// Package wfp decide QUÉ filtros pone la compuerta, sin tocar Windows.
//
// # Qué es la compuerta y por qué existe
//
// Las reglas del Firewall de Windows son las que ABREN, y no pueden expresar
// "denegar todo salvo esto": los bloqueos explícitos ganan sobre cualquier
// permiso en conflicto, sin desempate por especificidad, así que un bloqueo
// total taparía también los permisos del propio Kanpachi. La lista de lo que se
// abre queda ADITIVA, y mientras lo sea, una regla permisiva ajena de escritorio
// remoto alcanza al usuario por la red virtual.
//
// La compuerta es la segunda capa: un bloqueo de todo lo entrante acotado al
// adaptador virtual, más permisos espejo del mismo conjunto. En WFP un Block es
// HARD por defecto y un Permit es SOFT, y esa asimetría es lo que hace que
// nuestro bloqueo anule la regla ajena sin tocarla, conservando a la vez el veto
// del usuario. Ver decisión 27, con las cuatro mediciones que la sostienen.
//
// # Por qué este fichero es puro
//
// Todo lo que se puede equivocar de forma cara se decide acá y lo prueba el CI
// de Linux. La parte de Windows solo copia estos campos a una estructura de la
// API. El fallo más caro posible de esta capa es un filtro SIN ALCANCE: no falla
// en ningún test funcional, aplica a todos los adaptadores de la máquina, y con
// un bloqueo duro deja al usuario sin su red de casa. No se ve leyendo un diff.
package wfp

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"net/netip"
	"sort"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// Action es qué hace un filtro cuando casa.
type Action uint8

const (
	// Block es HARD por defecto en WFP: no lo puede permitir otro sublayer.
	Block Action = iota + 1
	// Permit es SOFT por defecto: lo puede bloquear otro sublayer, que es lo
	// que conserva el veto del usuario.
	Permit
)

func (a Action) String() string {
	switch a {
	case Block:
		return "bloqueo"
	case Permit:
		return "permiso"
	default:
		return "acción-inválida"
	}
}

// Layer es dónde se instala el filtro.
//
// SOLO capas de recepción. `ALE_AUTH_CONNECT` no aparece en este tipo a
// propósito: bloquear la salida impediría que un invitado marque al puerto del
// juego del host, que es el caso central del producto. Lo que no existe en el
// tipo no se puede pedir por error.
type Layer uint8

const (
	RecvAcceptV4 Layer = iota + 1
	RecvAcceptV6
)

func (l Layer) String() string {
	switch l {
	case RecvAcceptV4:
		return "entrada IPv4"
	case RecvAcceptV6:
		return "entrada IPv6"
	default:
		return "capa-inválida"
	}
}

// Pesos DENTRO del sublayer propio.
//
// El permiso tiene que pesar más que el bloqueo de todo, o el bloqueo se lo
// lleva por delante y no habría forma de abrir el puerto del juego. Medido
// funcionando el 2026-08-04 con estos dos valores.
const (
	WeightBlockAll uint64 = 100
	WeightPermit   uint64 = 200
)

// Scope es DÓNDE aplica un filtro, y ningún filtro puede salir sin él.
//
// # Por qué son dos cosas y no una
//
// El LUID del adaptador es lo preciso. El prefijo de la sala es el respaldo, y
// existe por un riesgo concreto: si la condición de interfaz llegara vacía al
// reautorizar un flujo ya establecido, tras reiniciar el servicio o al cambiar
// de adaptador, el bloqueo dejaría de casar EN SILENCIO y la pantalla diría
// verde. Emitiendo el bloqueo por las dos vías, ninguna es el único asidero.
//
// El prefijo no puede pisar la red de casa porque la `/24` de la sala se elige
// en tiempo de ejecución contra las redes que la máquina ya tiene. Ver decisión
// 10.
type Scope struct {
	// LUID del adaptador virtual, tal como lo entiende WFP. Cero significa sin
	// acotar, y eso está prohibido.
	LUID uint64
	// Net es el rango de la sala sobre ese adaptador.
	Net netip.Prefix
}

// Valid dice si el alcance sirve para acotar de verdad.
func (s Scope) Valid() bool { return s.LUID != 0 && s.Net.IsValid() }

// Conditions son las condiciones de un filtro.
//
// Los ceros significan "sin condición", que para un bloqueo es lo peligroso y
// para un permiso es lo inútil. Por eso nadie construye esto a mano fuera de
// este fichero: lo vigila un guardián de internal/arch.
type Conditions struct {
	// LUID acota por adaptador. Cero es sin acotar.
	LUID uint64
	// LocalNet acota por dirección local. Inválido es sin acotar.
	LocalNet netip.Prefix
	// LocalAddr es la IP del adaptador, en los permisos.
	LocalAddr netip.Addr
	// LocalPort cero significa cualquier puerto, que es lo que convierte un
	// bloqueo en un bloqueo de todo.
	LocalPort uint16
	Proto     domain.Proto
	// Remote son los miembros presentes.
	Remote []netip.Addr
	// RemoteNets es el alcance remoto por prefijo. Solo lo usa la puerta del
	// vestíbulo, igual que en [domain.FirewallRule].
	RemoteNets []netip.Prefix
}

// scoped dice si estas condiciones acotan a ALGO local.
//
// Es la comprobación que separa un filtro correcto de uno que aplica a todos los
// adaptadores de la máquina.
func (c Conditions) scoped() bool {
	return c.LUID != 0 || c.LocalNet.IsValid() || c.LocalAddr.IsValid()
}

// FilterSpec es un filtro decidido, listo para que la capa de Windows lo copie.
type FilterSpec struct {
	// Key es determinista a partir de Label, para que la limpieza encuentre lo
	// que dejó una ejecución anterior sin recordar nada entre arranques.
	Key   [16]byte
	Label string
	Layer Layer
	// Action y Weight deciden el arbitraje dentro del sublayer propio.
	Action     Action
	Weight     uint64
	Conditions Conditions
}

// Validate es la última puerta antes de que esto llegue a la API de Windows.
//
// Un filtro sin alcance no falla en ningún test funcional: aplica a TODOS los
// adaptadores, y con un bloqueo duro deja al usuario sin la red de casa. Por eso
// se comprueba acá además de en el guardián de arquitectura: uno vigila cómo se
// escribe el código y este vigila lo que de verdad se va a instalar.
func (f FilterSpec) Validate() error {
	if f.Layer != RecvAcceptV4 && f.Layer != RecvAcceptV6 {
		return fmt.Errorf("filtro %q: capa inválida %d", f.Label, f.Layer)
	}
	if f.Action != Block && f.Action != Permit {
		return fmt.Errorf("filtro %q: acción inválida %d", f.Label, f.Action)
	}
	if !f.Conditions.scoped() {
		return fmt.Errorf("filtro %q: SIN ALCANCE. Aplicaría a todos los adaptadores "+
			"de la máquina, y siendo un %s eso deja al usuario sin su red de casa",
			f.Label, f.Action)
	}
	if f.Action == Permit && f.Weight <= WeightBlockAll {
		return fmt.Errorf("filtro %q: un permiso con peso %d no le gana al bloqueo de "+
			"todo, que pesa %d, así que el puerto del juego no se abriría",
			f.Label, f.Weight, WeightBlockAll)
	}
	return nil
}

// keyFor deriva la GUID de un filtro a partir de su etiqueta.
//
// Determinista a propósito: al arrancar hay que poder borrar lo que dejó la
// ejecución anterior, y recordar las claves en disco añadiría un fichero que
// puede desincronizarse del sistema. Enumerar el sublayer sería la alternativa,
// y trae bastante más superficie de API por la misma respuesta.
func keyFor(label string) [16]byte {
	const namespace = "kanpachi/wfp/gate/v1\x00"
	sum := sha256.Sum256([]byte(namespace + label))

	var key [16]byte
	copy(key[:], sum[:16])
	// Se marcan los bits de versión y variante de UUID v4 para que sea una GUID
	// bien formada. Windows no lo exige y las herramientas que la muestren sí lo
	// esperan.
	key[6] = (key[6] & 0x0f) | 0x40
	key[8] = (key[8] & 0x3f) | 0x80
	return key
}

// SpecsFor traduce el conjunto deseado a los filtros de la compuerta.
//
// # Lo que emite, y en qué orden importa
//
// Dos bloqueos de todo, uno por LUID y otro por prefijo de la sala, en IPv4. Ver
// [Scope] para el porqué de los dos.
//
// Un bloqueo de todo por LUID en IPv6, SIN permisos espejo. Kanpachi direcciona
// en IPv4 dentro de `100.64.0.0/10`, así que cualquier cosa que llegue por IPv6
// a ese adaptador no es nuestra, y dejarla pasar sería un agujero con la puerta
// de al lado cerrada.
//
// Un permiso espejo por cada regla deseada, con más peso que los bloqueos.
//
// Falla en la PRIMERA regla mala en vez de saltársela, por lo mismo que
// `SpecsFor` del adaptador COM: un conjunto aplicado con una regla caída en
// silencio deja al usuario creyendo que la sala está configurada mientras un
// jugador no puede entrar, sin nada en pantalla que lo explique.
func SpecsFor(desired domain.RuleSet, scope Scope) ([]FilterSpec, error) {
	if !scope.Valid() {
		return nil, fmt.Errorf("la compuerta necesita adaptador Y rango de la sala, y "+
			"llegó LUID=0x%X rango=%v. Sin alcance, el bloqueo de todo dejaría al "+
			"usuario sin su red de casa", scope.LUID, scope.Net)
	}

	out := []FilterSpec{
		newSpec("bloqueo de todo, por adaptador", RecvAcceptV4, Block, WeightBlockAll,
			Conditions{LUID: scope.LUID}),
		newSpec("bloqueo de todo, por rango de la sala", RecvAcceptV4, Block, WeightBlockAll,
			Conditions{LocalNet: scope.Net}),
		newSpec("bloqueo de todo IPv6, por adaptador", RecvAcceptV6, Block, WeightBlockAll,
			Conditions{LUID: scope.LUID}),
	}

	for _, r := range desired.Rules {
		s, err := permitFor(r, scope)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}

	for _, s := range out {
		if err := s.Validate(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// permitFor traduce una regla del dominio a su permiso espejo.
//
// El permiso lleva el MISMO alcance que la regla del Firewall de Windows, más el
// adaptador. Que sean espejo es lo que hace que la compuerta no abra nada que
// los permisos visibles no abran ya: quien audite con sus herramientas de
// siempre ve la lista completa de lo que está abierto.
func permitFor(r domain.FirewallRule, scope Scope) (FilterSpec, error) {
	if !r.Local.IsValid() {
		return FilterSpec{}, fmt.Errorf("regla %q: sin dirección local", r.Name)
	}
	if len(r.Remote) == 0 && len(r.Nets) == 0 {
		// El dominio ya lo prohíbe. Se recomprueba porque un permiso sin alcance
		// remoto en la compuerta abriría el puerto a cualquiera que alcance el
		// adaptador, que es justo lo que la compuerta existe para impedir.
		return FilterSpec{}, fmt.Errorf("regla %q: sin alcance remoto, y eso en la "+
			"compuerta se lee como cualquiera", r.Name)
	}
	if r.Proto == domain.ProtoBoth {
		return FilterSpec{}, fmt.Errorf("regla %q: proto both no llega hasta acá, "+
			"[domain.BuildRuleSet] lo expande en dos reglas", r.Name)
	}
	if r.From != r.To {
		return FilterSpec{}, fmt.Errorf("regla %q: la compuerta emite un filtro por "+
			"PUERTO y este es un rango %d-%d. Hay que expandirlo antes", r.Name, r.From, r.To)
	}

	remote := append([]netip.Addr(nil), r.Remote...)
	sort.Slice(remote, func(i, j int) bool { return remote[i].Less(remote[j]) })
	nets := append([]netip.Prefix(nil), r.Nets...)
	sort.Slice(nets, func(i, j int) bool { return nets[i].String() < nets[j].String() })

	return newSpec("permiso espejo: "+r.Name, RecvAcceptV4, Permit, WeightPermit, Conditions{
		LUID:       scope.LUID,
		LocalAddr:  r.Local,
		LocalPort:  r.From,
		Proto:      r.Proto,
		Remote:     remote,
		RemoteNets: nets,
	}), nil
}

// newSpec es el ÚNICO constructor de filtros.
//
// Existe para que el guardián de arquitectura tenga algo concreto que vigilar:
// un literal `FilterSpec{...}` fuera de este fichero puede olvidarse el alcance
// y compilar igual.
func newSpec(label string, layer Layer, action Action, weight uint64, c Conditions) FilterSpec {
	return FilterSpec{
		Key:        keyFor(label),
		Label:      label,
		Layer:      layer,
		Action:     action,
		Weight:     weight,
		Conditions: c,
	}
}

// KeysOf son las claves de un conjunto, para poder borrarlas.
func KeysOf(specs []FilterSpec) [][16]byte {
	out := make([][16]byte, 0, len(specs))
	for _, s := range specs {
		out = append(out, s.Key)
	}
	return out
}

// unused mantiene honesto el import mientras la capa de Windows no aterriza.
var _ = binary.LittleEndian
