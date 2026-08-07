//go:build windows

package wfp

// La parte que habla con Windows. Todo lo que se puede equivocar de forma cara
// ya se decidió en spec.go, que es puro y lo prueba el CI de Linux: acá solo se
// copian esos campos a las estructuras de la API.
//
// # La sesión: NO dinámica y NO persistente
//
// Las dos mitades son deliberadas y por motivos distintos.
//
// No dinámica, porque los filtros de una sesión dinámica se borran cuando el
// proceso muere. Eso suena bien y quita justo lo que la compuerta necesita: que
// una muerte sucia del daemon deje la sala CONTENIDA en vez de abierta, y que la
// limpieza al arrancar sea una operación de verdad y no un no-op. Es la misma
// doctrina que `PurgeOwned` del adaptador COM.
//
// No persistente, porque así reiniciar la máquina se lo lleva todo. Es la red de
// seguridad final: si el cierre falla y la limpieza falla, un reinicio deja la
// máquina como estaba.
//
// # Por qué todo va dentro de una transacción
//
// Aplicar es barrer las ranuras y volver a escribirlas enteras, no parchear
// filtro a filtro. Entre el borrado y la escritura hay una ventana donde el
// bloqueo no está, y esa ventana está en el cable. Con transacción no existe:
// WFP publica el conjunto entero de golpe o no publica nada.

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"runtime"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/accentiostudios/kanpachi/core/domain"
)

var (
	fwpuclnt = windows.NewLazySystemDLL("fwpuclnt.dll")
	iphlpapi = windows.NewLazySystemDLL("iphlpapi.dll")

	procEngineOpen          = fwpuclnt.NewProc("FwpmEngineOpen0")
	procEngineClose         = fwpuclnt.NewProc("FwpmEngineClose0")
	procTransactionBegin    = fwpuclnt.NewProc("FwpmTransactionBegin0")
	procTransactionCommit   = fwpuclnt.NewProc("FwpmTransactionCommit0")
	procTransactionAbort    = fwpuclnt.NewProc("FwpmTransactionAbort0")
	procSubLayerAdd         = fwpuclnt.NewProc("FwpmSubLayerAdd0")
	procSubLayerDeleteByKey = fwpuclnt.NewProc("FwpmSubLayerDeleteByKey0")
	procFilterAdd           = fwpuclnt.NewProc("FwpmFilterAdd0")
	procFilterDeleteByKey   = fwpuclnt.NewProc("FwpmFilterDeleteByKey0")
	procFilterGetByKey      = fwpuclnt.NewProc("FwpmFilterGetByKey0")
	procFreeMemory          = fwpuclnt.NewProc("FwpmFreeMemory0")

	procIndexToLuid = iphlpapi.NewProc("ConvertInterfaceIndexToLuid")
)

// Las GUID del sistema.
//
// Si alguna estuviera mal, la llamada devuelve un código y se ve. No hay forma
// de que esto se equivoque en silencio: una GUID inventada no es otra capa
// válida, es ninguna.
var (
	// FWPM_LAYER_ALE_AUTH_RECV_ACCEPT_V4. La capa donde se autoriza una conexión
	// ENTRANTE. Medida funcionando en las cuatro pruebas del 2026-08-04.
	layerRecvAcceptV4 = windows.GUID{
		Data1: 0xe1cd9fe7, Data2: 0xf4b5, Data3: 0x4273,
		Data4: [8]byte{0x96, 0xc0, 0x59, 0x2e, 0x48, 0x7b, 0x86, 0x50},
	}
	// FWPM_LAYER_ALE_AUTH_RECV_ACCEPT_V6. La gemela de la anterior. Esta NO se
	// midió en el spike, que fue entero IPv4: si estuviera mal, el alta del
	// bloqueo IPv6 falla con código y se ve al primer arranque.
	layerRecvAcceptV6 = windows.GUID{
		Data1: 0xa3b42c97, Data2: 0x9f04, Data3: 0x4672,
		Data4: [8]byte{0xb8, 0x7e, 0xce, 0xe9, 0xc4, 0x83, 0x25, 0x7f},
	}

	// FWPM_CONDITION_IP_LOCAL_INTERFACE. Toma un LUID de 64 bits POR PUNTERO.
	condLocalInterface = windows.GUID{
		Data1: 0x4cd62a49, Data2: 0x59c3, Data3: 0x4969,
		Data4: [8]byte{0xb7, 0xf3, 0xbd, 0xa5, 0xd3, 0x28, 0x90, 0xa4},
	}
	// FWPM_CONDITION_IP_LOCAL_ADDRESS
	condLocalAddress = windows.GUID{
		Data1: 0xd9ee00de, Data2: 0xc1ef, Data3: 0x4617,
		Data4: [8]byte{0xbf, 0xe3, 0xff, 0xd8, 0xf5, 0xa0, 0x89, 0x57},
	}
	// FWPM_CONDITION_IP_REMOTE_ADDRESS
	condRemoteAddress = windows.GUID{
		Data1: 0xb235ae9a, Data2: 0x1d64, Data3: 0x49b8,
		Data4: [8]byte{0xa4, 0x4c, 0x5f, 0xf3, 0xd9, 0x09, 0x50, 0x45},
	}
	// FWPM_CONDITION_IP_LOCAL_PORT
	condLocalPort = windows.GUID{
		Data1: 0x0c1ba1af, Data2: 0x5765, Data3: 0x453f,
		Data4: [8]byte{0xaf, 0x22, 0xa8, 0xf7, 0x91, 0xac, 0x77, 0x5b},
	}
	// FWPM_CONDITION_IP_PROTOCOL
	condProtocol = windows.GUID{
		Data1: 0x3971ef2b, Data2: 0x623e, Data3: 0x4f9a,
		Data4: [8]byte{0x8c, 0xb1, 0x6e, 0x79, 0xb8, 0x06, 0xb9, 0xa7},
	}

	// La nuestra. Fija y literal, para que la limpieza de un arranque encuentre
	// el sublayer que dejó el arranque anterior sin recordar nada.
	subLayerKey = windows.GUID{
		Data1: 0x6b1f2c3d, Data2: 0x9a4e, Data3: 0x4f10,
		Data4: [8]byte{0xb2, 0x71, 0x6b, 0x61, 0x6e, 0x70, 0x61, 0x63},
	}
)

// Los códigos de error de WFP que forman parte del camino normal.
//
// Estaban puestos de memoria en el spike y DOS de tres estaban mal: el de filtro
// no encontrado se creía 0x...08 y es 0x...03, y el de sublayer no encontrado se
// creía 0x...05 y es 0x...07. Salió a la primera corrida, porque un "no
// encontrado" que no se reconoce se reporta como fallo duro.
const (
	fwpErrFilterNotFound   = 0x80320003
	fwpErrSubLayerNotFound = 0x80320007
	fwpErrAlreadyExists    = 0x80320009

	// errAccessDenied es ERROR_ACCESS_DENIED, y llega CRUDO y no como HRESULT.
	// Es 0x5 y no 0x80070005, que es lo que se esperaría de una API que devuelve
	// HRESULT en todo lo demás. Medido, ver [Open].
	errAccessDenied = 0x5
)

// Tipos de dato de WFP, de FWP_DATA_TYPE.
const (
	fwpUint8      = 1
	fwpUint16     = 2
	fwpUint32     = 3
	fwpUint64     = 4
	fwpV4AddrMask = 0x100
	fwpRangeType  = 0x102
)

// Tipos de comparación, de FWP_MATCH_TYPE.
const (
	matchEqual = 0
	matchRange = 5
)

const (
	// FWP_ACTION_BLOCK | FWP_ACTION_FLAG_TERMINATING.
	//
	// Un BLOCK es HARD por defecto: "cannot be permitted at another sub-layer".
	// Esa asimetría es toda la apuesta del diseño, y quedó medida: le gana a un
	// permiso vivo del Firewall de Windows.
	actionBlock = 0x00001001

	// FWP_ACTION_PERMIT | FWP_ACTION_FLAG_TERMINATING.
	//
	// Un PERMIT es SOFT por defecto: "can be blocked at another sub-layer". Eso
	// es lo que conserva el veto del usuario, y también quedó medido: su bloqueo
	// tumba nuestro permiso espejo. Ver decisión 4.
	actionPermit = 0x00001002

	// El peso del sublayer. Alto para ganarle al de MPSSVC, y NO el máximo,
	// porque el usuario conserva el veto.
	subLayerWeight = 0x8000
)

// IPPROTO_TCP e IPPROTO_UDP, que es lo que espera la condición de protocolo.
const (
	ipprotoTCP = 6
	ipprotoUDP = 17
)

type fwpByteBlob struct {
	size uint32
	data *uint8
}

type fwpmDisplayData0 struct {
	name        *uint16
	description *uint16
}

type fwpValue0 struct {
	typ   uint32
	_     uint32
	value uintptr
}

type fwpConditionValue0 struct {
	typ   uint32
	_     uint32
	value uintptr
}

type fwpmFilterCondition0 struct {
	fieldKey       windows.GUID
	matchType      uint32
	_              uint32
	conditionValue fwpConditionValue0
}

type fwpmAction0 struct {
	typ    uint32
	filter windows.GUID
}

type fwpmSubLayer0 struct {
	subLayerKey  windows.GUID
	displayData  fwpmDisplayData0
	flags        uint32
	_            uint32
	providerKey  *windows.GUID
	providerData fwpByteBlob
	weight       uint16
	_            [6]byte
}

// fwpmFilter0 es FWPM_FILTER0 de fwpmtypes.h.
//
// Los rellenos explícitos no son adorno: en x64 la unión de rawContext y
// providerContextKey se alinea a 8 y Go no lo haría solo con un `[16]byte`, así
// que sin ese relleno todos los campos posteriores quedan corridos cuatro bytes.
// El de filterID y effectiveWeight sale por ahí, y ninguno de los dos se lee.
type fwpmFilter0 struct {
	filterKey           windows.GUID
	displayData         fwpmDisplayData0
	flags               uint32
	_                   uint32
	providerKey         *windows.GUID
	providerData        fwpByteBlob
	layerKey            windows.GUID
	subLayerKey         windows.GUID
	weight              fwpValue0
	numFilterConditions uint32
	_                   uint32
	filterCondition     *fwpmFilterCondition0
	action              fwpmAction0
	_                   [4]byte
	context             [16]byte
	reserved            *windows.GUID
	filterID            uint64
	effectiveWeight     fwpValue0
}

// fwpV4AddrAndMask es FWP_V4_ADDR_AND_MASK, en orden de host.
type fwpV4AddrAndMask struct {
	addr uint32
	mask uint32
}

// fwpRange0 es FWP_RANGE0, los dos extremos de un rango incluidos.
type fwpRange0 struct {
	low  fwpValue0
	high fwpValue0
}

// hr recorta el HRESULT a 32 bits.
//
// En x64 los Call devuelven uintptr y el HRESULT vuelve con el signo extendido:
// 0x80320003 llega como 0xFFFFFFFF80320003, así que comparar el uintptr crudo
// contra la constante NUNCA casa. Es el peor de los bugs que tuvo el spike: un
// "ya existe" o un "no encontrado" sin reconocer convierte un camino normal en
// un error.
func hr(r uintptr) uint32 { return uint32(r) }

// Logger es la rebanada del log del daemon que este adaptador necesita.
type Logger interface {
	Info(msg string, kv ...any)
	Warn(msg string, kv ...any)
	Error(msg string, kv ...any)
}

// Gate es la compuerta viva: la sesión contra el motor de filtrado.
//
// # El handle se tiene abierto, no se abre por llamada
//
// Abrir la sesión cuesta una llamada RPC y no hace falta repetirla. Lo que sí
// importa es que UNA sola sesión hace de las transacciones algo secuencial y
// comprobable: WFP admite una transacción por sesión, y el mutex de acá la
// serializa antes de que la API tenga que decirlo.
type Gate struct {
	mu  sync.Mutex
	h   windows.Handle
	log Logger
}

// Open abre la sesión contra el motor de filtrado.
//
// # Qué necesita elevación y qué no, MEDIDO el 2026-08-04
//
//	FwpmEngineOpen0        sin elevar, funciona
//	FwpmFilterGetByKey0    sin elevar, depende de si el filtro ESTÁ
//	FwpmTransactionBegin0  sin elevar, 0x5, o sea ERROR_ACCESS_DENIED
//
// El del medio tiene trampa y costó dos conclusiones equivocadas seguidas. Sin
// elevar y con el filtro ausente contesta FWP_E_FILTER_NOT_FOUND, así que la
// primera medición pareció decir que leer no exige administrador. Con el filtro
// PUESTO contesta 0x5. O sea que leer un filtro que existe también exige
// elevación, y lo anterior funcionaba solo porque no había nada que leer.
//
// # Por qué eso no produce un verde falso
//
// Porque los dos casos llegan con códigos DISTINTOS. Si "no está" y "está y no
// puedes verlo" compartieran código, una medición sin elevar informaría la
// compuerta como ausente teniéndola puesta. Como no lo comparten, [Gate.present]
// devuelve error en el segundo y [Gate.Measure] contesta SIN COMPROBAR, que es
// la verdad. Ver [domain.GateUnknown]: una es un hecho y la otra es ceguera.
//
// La consecuencia de producto es que la pantalla de exposición no puede medir la
// compuerta sin elevar. Quien la mide de verdad es el daemon, que corre como
// servicio del sistema.
func Open(log Logger) (*Gate, error) {
	if log == nil {
		return nil, fmt.Errorf("la compuerta necesita un log: sin él, un filtro que no se " +
			"pudo poner no deja rastro en ningún sitio")
	}

	var h windows.Handle
	// serverName nil, authnService RPC_C_AUTHN_DEFAULT, authIdentity nil, y
	// session nil, que es la sesión por defecto: NI dinámica NI persistente.
	// Ver el comentario de arriba del archivo para el porqué de las dos mitades.
	r, _, _ := procEngineOpen.Call(0, 0xFFFFFFFF, 0, 0, uintptr(unsafe.Pointer(&h)))
	if c := hr(r); c != 0 {
		return nil, fmt.Errorf("FwpmEngineOpen0 devolvió 0x%X", c)
	}
	return &Gate{h: h, log: log}, nil
}

// Close cierra la sesión.
//
// No se lleva los filtros por delante, que es justo lo que se quiere: la sesión
// no es dinámica, así que una muerte sucia del daemon deja la sala contenida y
// no abierta. Quien limpia es [Gate.Purge] en el arranque siguiente.
func (g *Gate) Close() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.h == 0 {
		return nil
	}
	r, _, _ := procEngineClose.Call(uintptr(g.h))
	g.h = 0
	if c := hr(r); c != 0 {
		return fmt.Errorf("FwpmEngineClose0 devolvió 0x%X", c)
	}
	return nil
}

// Apply deja la compuerta con exactamente estos filtros y ninguno más.
//
// # Se reescribe entero, no se parchea
//
// Barre las [MaxFilters] ranuras y vuelve a escribir las que toquen. Es más
// simple que diferenciar, y sobre todo es lo que hace que reaplicar el mismo
// conjunto REPARE lo que alguien haya borrado por fuera, igual que en el
// adaptador COM. Un filtro de WFP tampoco se puede editar en sitio: la API solo
// da alta y baja.
//
// Todo pasa dentro de una transacción, así que no existe el instante en que el
// bloqueo se fue y los permisos todavía no llegaron.
//
// El contexto se mira UNA vez, al entrar. Cancelar a mitad de una transacción no
// dejaría nada a medias, y tampoco serviría de nada: lo que sigue son unas pocas
// llamadas locales.
func (g *Gate) Apply(ctx context.Context, want []FilterSpec) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// La última puerta antes de la API. El guardián de arquitectura vigila cómo
	// se ESCRIBE el código y esto vigila lo que de verdad se va a instalar, que
	// no son la misma cosa: acá llega una rebanada que otro pudo recortar.
	for _, s := range want {
		if err := s.Validate(); err != nil {
			return err
		}
	}
	if len(want) > MaxFilters {
		return fmt.Errorf("llegaron %d filtros y la limpieza solo barre %d ranuras: los "+
			"de más quedarían puestos para siempre", len(want), MaxFilters)
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	return g.inTransaction(func() error {
		if err := g.addSubLayer(); err != nil {
			return err
		}
		if err := g.sweep(); err != nil {
			return err
		}
		for _, s := range want {
			if err := g.add(s); err != nil {
				return err
			}
		}
		return nil
	})
}

// Purge borra todo lo que la compuerta pueda haber puesto alguna vez.
//
// Se llama al arrancar el servicio, antes de aplicar nada, por lo mismo que
// `PurgeOwned`: una muerte sucia del daemon nunca deja filtros huérfanos. Un
// permiso huérfano deja abierto un puerto que ya nadie pidió, y no se ve, porque
// un filtro de WFP no sale ni en `wf.msc` ni en `Get-NetFirewallRule`.
//
// Barre RANURAS y no un conjunto recordado, que es de lo que depende que
// funcione sin memoria entre arranques. Ver [FilterSpec.Key].
//
// Es idempotente: que no haya nada que borrar es el resultado normal.
func (g *Gate) Purge(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	return g.inTransaction(func() error {
		if err := g.sweep(); err != nil {
			return err
		}
		return g.removeSubLayer()
	})
}

// Measure pregunta al sistema qué hay puesto de lo que se pidió.
//
// # Por qué recibe el conjunto deseado
//
// Porque una clave sola no dice qué significaba. Las ranuras son posiciones, no
// nombres: para poder decir "falta el permiso de tal regla" hay que saber qué
// regla ocupaba esa ranura. El sistema sigue siendo el que contesta si el filtro
// está; el conjunto deseado solo dice por cuáles preguntar.
//
// Un fallo al leer devuelve [domain.GateUnknown] y no ausente. La diferencia es
// el motivo entero de que ese estado exista: una es un hecho y la otra es
// ceguera, y una ceguera pintada de verde es peor que una alarma.
func (g *Gate) Measure(ctx context.Context, want []FilterSpec) (Measurement, error) {
	if err := ctx.Err(); err != nil {
		return Measurement{}, err
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	out := Measurement{Gate: domain.GateUnknown}

	// Se pregunta por la ranura de la sala SIEMPRE, y por la del vestíbulo solo
	// cuando se pidió.
	//
	// Esa condición es lo que separa "no aplica" de "falta". El vestíbulo no
	// está en una sala de invitado, que lo soltó al entrar a propósito: buscarlo
	// ahí y no encontrarlo diría que la compuerta está ausente cuando está
	// entera. Y al revés, con el vestíbulo pedido y su bloqueo caído, contestar
	// que está puesta sería pintar de verde media compuerta, justo en el
	// adaptador donde llega gente que todavía no es miembro.
	claves := [][16]byte{GateKey()}
	for _, s := range want {
		if s.Slot == SlotLobbyLUID {
			claves = append(claves, LobbyGateKey())
			break
		}
	}

	out.Gate = domain.GatePresent
	for _, k := range claves {
		present, err := g.present(guidOf(k))
		if err != nil {
			return Measurement{Gate: domain.GateUnknown}, err
		}
		if !present {
			out.Gate = domain.GateAbsent
			break
		}
	}

	for _, s := range want {
		if s.Rule == "" {
			continue
		}
		ok, err := g.present(guidOf(s.Key))
		if err != nil {
			// Medido a medias no se devuelve: el que llama no tiene cómo
			// distinguir "este permiso no está" de "no se pudo preguntar", y esa
			// confusión es la que produce un verde falso.
			return Measurement{Gate: domain.GateUnknown}, err
		}
		out.Rules = append(out.Rules, domain.AppliedRule{
			Name:    s.Rule,
			Layer:   domain.LayerPacketFilter,
			Enabled: ok,
		})
	}
	return out, nil
}

// inTransaction corre fn dentro de una transacción de WFP.
//
// # Por qué el hilo queda fijado
//
// Una transacción pertenece a la sesión, y la documentación no promete que se
// pueda confirmar desde un hilo distinto del que la abrió. Go mueve las
// goroutines entre hilos del sistema cuando le conviene, así que sin fijar el
// hilo esto funcionaría casi siempre, que es la peor de las opciones.
//
// El aborto va en defer y no en cada retorno: una transacción abierta y sin
// cerrar bloquea a la siguiente, y con varios retornos posibles olvidarse de uno
// es cuestión de tiempo.
func (g *Gate) inTransaction(fn func() error) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if r, _, _ := procTransactionBegin.Call(uintptr(g.h), 0); hr(r) != 0 {
		if hr(r) == errAccessDenied {
			// Es donde aparece la falta de elevación, y no al abrir la sesión.
			// Medido el 2026-08-04, ver [Open].
			return fmt.Errorf("FwpmTransactionBegin0 devolvió ERROR_ACCESS_DENIED. Este "+
				"proceso puede LEER los filtros y no escribirlos, así que hay que correrlo "+
				"como administrador (0x%X)", hr(r))
		}
		return fmt.Errorf("FwpmTransactionBegin0 devolvió 0x%X", hr(r))
	}

	committed := false
	defer func() {
		if committed {
			return
		}
		if r, _, _ := procTransactionAbort.Call(uintptr(g.h)); hr(r) != 0 {
			g.log.Error("no se pudo abortar la transacción de la compuerta, y hasta que "+
				"se cierre la sesión ninguna otra podrá abrirse", "codigo", fmt.Sprintf("0x%X", hr(r)))
		}
	}()

	if err := fn(); err != nil {
		return err
	}

	if r, _, _ := procTransactionCommit.Call(uintptr(g.h)); hr(r) != 0 {
		return fmt.Errorf("FwpmTransactionCommit0 devolvió 0x%X, así que no se aplicó "+
			"NADA de lo pedido", hr(r))
	}
	committed = true
	return nil
}

// addSubLayer crea el sublayer propio. Que ya exista NO es un error.
//
// Sin la bandera de persistente: un reinicio se lo lleva, que es la red de
// seguridad final.
func (g *Gate) addSubLayer() error {
	name, err := windows.UTF16PtrFromString("Kanpachi")
	if err != nil {
		return err
	}
	desc, err := windows.UTF16PtrFromString(
		"La compuerta de la red virtual de Kanpachi. No persistente: un reinicio se la lleva.")
	if err != nil {
		return err
	}

	sl := fwpmSubLayer0{
		subLayerKey: subLayerKey,
		displayData: fwpmDisplayData0{name: name, description: desc},
		weight:      subLayerWeight,
	}
	r, _, _ := procSubLayerAdd.Call(uintptr(g.h), uintptr(unsafe.Pointer(&sl)), 0)
	runtime.KeepAlive(&sl)

	if c := hr(r); c != 0 && c != fwpErrAlreadyExists {
		return fmt.Errorf("FwpmSubLayerAdd0 devolvió 0x%X", c)
	}
	return nil
}

func (g *Gate) removeSubLayer() error {
	key := subLayerKey
	r, _, _ := procSubLayerDeleteByKey.Call(uintptr(g.h), uintptr(unsafe.Pointer(&key)))
	runtime.KeepAlive(&key)

	if c := hr(r); c != 0 && c != fwpErrSubLayerNotFound {
		return fmt.Errorf("FwpmSubLayerDeleteByKey0 devolvió 0x%X", c)
	}
	return nil
}

// sweep borra las [MaxFilters] ranuras, ocupadas o no.
func (g *Gate) sweep() error {
	removed := 0
	for slot, k := range AllKeys() {
		key := guidOf(k)
		r, _, _ := procFilterDeleteByKey.Call(uintptr(g.h), uintptr(unsafe.Pointer(&key)))
		runtime.KeepAlive(&key)

		switch c := hr(r); c {
		case 0:
			removed++
		case fwpErrFilterNotFound:
			// El caso normal: la ranura estaba vacía.
		default:
			return fmt.Errorf("borrando la ranura %d: FwpmFilterDeleteByKey0 devolvió 0x%X", slot, c)
		}
	}
	// Cuántas ranuras se borraron no se anota. Barrer y reinstalar es cómo
	// funciona cada aplicación de reglas, así que el número es el mismo siempre
	// y sale varias veces por segundo mientras alguien entra. Lo que se anota
	// es el acotado de la compuerta, que es el hecho que cambia.
	_ = removed
	return nil
}

// add instala un filtro.
func (g *Gate) add(s FilterSpec) error {
	layer, err := layerGUID(s.Layer)
	if err != nil {
		return err
	}
	action, err := actionOf(s.Action)
	if err != nil {
		return err
	}
	cs, err := conditionsOf(s.Conditions)
	if err != nil {
		return fmt.Errorf("filtro %q: %w", s.Label, err)
	}
	if len(cs.conds) == 0 {
		// No debería llegar acá porque Validate lo caza antes, y se recomprueba
		// igual: un filtro sin condiciones aplica a TODOS los adaptadores de la
		// máquina, y siendo un bloqueo duro deja al usuario sin su red de casa.
		return fmt.Errorf("filtro %q: quedó sin ninguna condición", s.Label)
	}

	name, err := windows.UTF16PtrFromString("Kanpachi: " + s.Label)
	if err != nil {
		return err
	}

	key := guidOf(s.Key)
	weight := s.Weight
	f := fwpmFilter0{
		filterKey:           key,
		displayData:         fwpmDisplayData0{name: name},
		layerKey:            layer,
		subLayerKey:         subLayerKey,
		weight:              fwpValue0{typ: fwpUint64, value: uintptr(unsafe.Pointer(&weight))},
		numFilterConditions: uint32(len(cs.conds)),
		filterCondition:     &cs.conds[0],
		action:              fwpmAction0{typ: action},
	}

	var id uint64
	r, _, _ := procFilterAdd.Call(uintptr(g.h), uintptr(unsafe.Pointer(&f)), 0,
		uintptr(unsafe.Pointer(&id)))

	// Todo lo apuntado tiene que seguir vivo durante la llamada. El recolector no
	// ve punteros escondidos dentro de un uintptr.
	runtime.KeepAlive(&f)
	runtime.KeepAlive(&weight)
	runtime.KeepAlive(cs)

	if c := hr(r); c != 0 {
		return fmt.Errorf("FwpmFilterAdd0 de %q devolvió 0x%X", s.Label, c)
	}
	return nil
}

// present dice si un filtro está puesto AHORA.
//
// Cualquier código que no sea cero ni "no encontrado" es un ERROR y jamás un
// "no está". Sin elevar, un filtro que EXISTE contesta ERROR_ACCESS_DENIED, y
// leerlo como ausente sería informar la compuerta caída teniéndola puesta. Ver
// [Open] para lo que se midió.
func (g *Gate) present(key windows.GUID) (bool, error) {
	var p uintptr
	r, _, _ := procFilterGetByKey.Call(uintptr(g.h), uintptr(unsafe.Pointer(&key)),
		uintptr(unsafe.Pointer(&p)))
	runtime.KeepAlive(&key)

	switch c := hr(r); c {
	case 0:
		if p != 0 {
			procFreeMemory.Call(uintptr(unsafe.Pointer(&p)))
		}
		return true, nil
	case fwpErrFilterNotFound:
		return false, nil
	default:
		return false, fmt.Errorf("FwpmFilterGetByKey0 devolvió 0x%X", c)
	}
}

// guidOf traduce la clave de un filtro a una GUID de Windows.
//
// La clave sale de un hash y se trata como la cadena de bytes de un UUID de la
// RFC 4122, o sea con los tres primeros campos en orden de red. Windows los
// guarda en orden de host, así que la conversión no es una copia: hacerla a lo
// bruto daría una GUID válida pero distinta de la que imprime cualquier
// herramienta, y los bits de versión y variante aparecerían movidos de sitio.
func guidOf(k [16]byte) windows.GUID {
	return windows.GUID{
		Data1: binary.BigEndian.Uint32(k[0:4]),
		Data2: binary.BigEndian.Uint16(k[4:6]),
		Data3: binary.BigEndian.Uint16(k[6:8]),
		Data4: [8]byte{k[8], k[9], k[10], k[11], k[12], k[13], k[14], k[15]},
	}
}

func layerGUID(l Layer) (windows.GUID, error) {
	switch l {
	case RecvAcceptV4:
		return layerRecvAcceptV4, nil
	case RecvAcceptV6:
		return layerRecvAcceptV6, nil
	}
	return windows.GUID{}, fmt.Errorf("capa %d sin GUID", l)
}

func actionOf(a Action) (uint32, error) {
	switch a {
	case Block:
		return actionBlock, nil
	case Permit:
		return actionPermit, nil
	}
	return 0, fmt.Errorf("acción %d sin traducción", a)
}

// condSet junta las condiciones de un filtro y mantiene vivo lo que apuntan.
//
// El campo `pin` no es decorativo. Varias condiciones llevan el valor POR
// PUNTERO, y ese puntero viaja escondido dentro de un uintptr, donde el
// recolector no lo ve. Sin una referencia de verdad en algún sitio, el valor
// puede recogerse entre que se arma la condición y se llama a la API.
type condSet struct {
	conds []fwpmFilterCondition0
	pin   []any
}

func (cs *condSet) add(field windows.GUID, match, typ uint32, value uintptr) {
	cs.conds = append(cs.conds, fwpmFilterCondition0{
		fieldKey:       field,
		matchType:      match,
		conditionValue: fwpConditionValue0{typ: typ, value: value},
	})
}

func (cs *condSet) pinned(v any, p unsafe.Pointer) uintptr {
	cs.pin = append(cs.pin, v)
	return uintptr(p)
}

func (cs *condSet) u64(v uint64) uintptr {
	p := new(uint64)
	*p = v
	return cs.pinned(p, unsafe.Pointer(p))
}

func (cs *condSet) addrMask(addr, mask uint32) uintptr {
	p := &fwpV4AddrAndMask{addr: addr, mask: mask}
	return cs.pinned(p, unsafe.Pointer(p))
}

func (cs *condSet) portRange(from, to uint16) uintptr {
	p := &fwpRange0{
		low:  fwpValue0{typ: fwpUint16, value: uintptr(from)},
		high: fwpValue0{typ: fwpUint16, value: uintptr(to)},
	}
	return cs.pinned(p, unsafe.Pointer(p))
}

// conditionsOf copia las condiciones ya decididas a las estructuras de la API.
//
// Traduce y no decide: qué condiciones salen, con qué valores y de qué campo lo
// resuelve [Conditions.Expand], que es puro y lo prueba el CI de Linux.
func conditionsOf(c Conditions) (*condSet, error) {
	expanded, err := c.Expand()
	if err != nil {
		return nil, err
	}

	cs := &condSet{}
	for _, e := range expanded {
		field, err := fieldGUID(e.Field)
		if err != nil {
			return nil, err
		}
		match, err := matchOf(e.Match)
		if err != nil {
			return nil, err
		}

		switch e.Kind {
		case ValueNum:
			typ, err := widthType(e.Width)
			if err != nil {
				return nil, err
			}
			if e.Width == Width64 {
				// Los de 64 bits van POR PUNTERO, a diferencia de los estrechos
				// que van por valor. Es la clase de detalle que no falla
				// ruidosamente: metiendo el valor directo, la condición compara
				// basura y el filtro no aplica a nada, en silencio.
				cs.add(field, match, typ, cs.u64(e.Num))
			} else {
				cs.add(field, match, typ, uintptr(e.Num))
			}
		case ValueAddrMask:
			cs.add(field, match, fwpV4AddrMask, cs.addrMask(e.Addr, e.Mask))
		case ValuePortRange:
			cs.add(field, match, fwpRangeType, cs.portRange(e.From, e.To))
		default:
			return nil, fmt.Errorf("condición de %s con forma de valor %d, sin traducción",
				e.Field, e.Kind)
		}
	}
	return cs, nil
}

func fieldGUID(f Field) (windows.GUID, error) {
	switch f {
	case FieldLocalInterface:
		return condLocalInterface, nil
	case FieldLocalAddress:
		return condLocalAddress, nil
	case FieldLocalPort:
		return condLocalPort, nil
	case FieldProtocol:
		return condProtocol, nil
	case FieldRemoteAddress:
		return condRemoteAddress, nil
	}
	return windows.GUID{}, fmt.Errorf("campo %d sin GUID", f)
}

func matchOf(m Match) (uint32, error) {
	switch m {
	case MatchEqual:
		return matchEqual, nil
	case MatchRange:
		return matchRange, nil
	}
	return 0, fmt.Errorf("comparación %d sin traducción", m)
}

// widthType lleva el ancho del entero al tipo de dato de WFP.
//
// La API guarda el tipo DENTRO del valor, así que decirle 32 donde esperaba 64
// no falla: compara otra cosa.
func widthType(w Width) (uint32, error) {
	switch w {
	case Width8:
		return fwpUint8, nil
	case Width16:
		return fwpUint16, nil
	case Width32:
		return fwpUint32, nil
	case Width64:
		return fwpUint64, nil
	}
	return 0, fmt.Errorf("ancho de %d bits sin tipo de WFP", w)
}

// LUIDOf busca el LUID de un adaptador por su nombre.
//
// El LUID es lo que WFP entiende, y no el nombre ni el índice. Sin él no hay
// [Scope] posible, así que esto es lo que hace utilizable a la compuerta.
//
// Devolver cero jamás es una opción: WFP lee un LUID cero como TODAS las
// interfaces, que es lo contrario de acotar, y con un bloqueo duro eso deja al
// usuario sin la entrada de su red de casa.
func LUIDOf(name string) (uint64, error) {
	if name == "" {
		return 0, fmt.Errorf("no se puede acotar la compuerta a un adaptador sin nombre")
	}

	ifaces, err := net.Interfaces()
	if err != nil {
		return 0, err
	}
	for _, i := range ifaces {
		if !equalFoldASCII(i.Name, name) {
			continue
		}
		var luid uint64
		r, _, _ := procIndexToLuid.Call(uintptr(i.Index), uintptr(unsafe.Pointer(&luid)))
		runtime.KeepAlive(&luid)

		if r != 0 {
			return 0, fmt.Errorf("ConvertInterfaceIndexToLuid del adaptador %q devolvió %d", name, r)
		}
		if luid == 0 {
			return 0, fmt.Errorf("el adaptador %q dio un LUID cero, y WFP lee el cero como "+
				"TODAS las interfaces", name)
		}
		return luid, nil
	}
	return 0, fmt.Errorf("no hay ningún adaptador llamado %q", name)
}

// equalFoldASCII compara sin distinguir mayúsculas y sin tocar Unicode.
//
// Los nombres de adaptador de Windows son ASCII, y `strings.EqualFold` haría
// plegado Unicode, que puede igualar cosas que el sistema considera distintas.
func equalFoldASCII(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if 'A' <= ca && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if 'A' <= cb && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}
