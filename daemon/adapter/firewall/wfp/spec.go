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
	"fmt"
	"net/netip"
	"sort"
	"strconv"

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

// Valid dice si el alcance acota de verdad, y no solo si está relleno.
//
// # Por qué no basta con que el prefijo sea válido
//
// `0.0.0.0/0` es un prefijo perfectamente válido, y como alcance de un bloqueo
// duro es la catástrofe entera: casa con TODA dirección local de la máquina, o
// sea que deja al usuario sin la entrada de su red de casa. Y no se ve, porque
// el campo está puesto, el tipo es correcto, y el código que lo lee dice
// "acotado por rango de la sala".
//
// Así que se exige además el tamaño de una sala y el espacio donde las salas
// viven. Un /16 dentro del espacio compartido tampoco vale: bloquearía 255 salas
// ajenas de las que esta máquina no sabe nada.
func (s Scope) Valid() error {
	if s.LUID == 0 {
		return fmt.Errorf("la compuerta necesita el adaptador, y llegó LUID=0, que WFP " +
			"lee como TODAS las interfaces")
	}
	if !s.Net.IsValid() {
		return fmt.Errorf("la compuerta necesita el rango de la sala, y llegó vacío")
	}
	if s.Net.Bits() != domain.RoomPrefixBits {
		return fmt.Errorf("el rango de la sala es %v, de %d bits, y una sala es un /%d. "+
			"Un prefijo más ancho convierte el bloqueo de todo en un bloqueo de una red "+
			"que no es nuestra", s.Net, s.Net.Bits(), domain.RoomPrefixBits)
	}
	if !domain.SharedSpace.Contains(s.Net.Addr()) && !domain.FallbackSpace.Contains(s.Net.Addr()) {
		return fmt.Errorf("el rango de la sala es %v, fuera de %v y de %v, que es donde "+
			"viven las salas", s.Net, domain.SharedSpace, domain.FallbackSpace)
	}
	return nil
}

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
	// LocalPortFrom y LocalPortTo son el rango de puertos, ambos incluidos.
	//
	// Los dos a cero significan CUALQUIER puerto, que es lo que convierte un
	// bloqueo en un bloqueo de todo. Iguales entre sí es un puerto suelto.
	//
	// Son dos campos y no uno porque el catálogo no pone tope a la amplitud de
	// un rango, así que un perfil puede pedir `27000-27100` legítimamente.
	// Expandir eso a cien filtros sería absurdo, y rechazarlo rompería perfiles
	// que el dominio acepta. WFP tiene condición de rango y es lo que
	// corresponde usar.
	LocalPortFrom uint16
	LocalPortTo   uint16
	Proto         domain.Proto
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
	if c.LUID != 0 || c.LocalAddr.IsValid() {
		return true
	}
	// Un prefijo de cero bits está puesto y no acota nada: casa con toda
	// dirección local de la máquina. Contarlo como alcance sería contar como
	// protección justo el caso que hay que impedir.
	return c.LocalNet.IsValid() && c.LocalNet.Bits() > 0
}

// MaxFilters es cuántos filtros puede tener puestos la compuerta a la vez, y de
// paso cuántas ranuras barre la limpieza.
//
// # Por qué hay un tope y por qué este número
//
// Hoy el máximo real son 21: los tres bloqueos, más [domain.MaxPortRanges]
// rangos por perfil que `both` puede duplicar, más los dos huecos del canal de
// control. El tope está por encima para que un perfil legítimo jamás lo toque, y
// lo bastante bajo para que barrer las ranuras enteras sea barato.
//
// Emitir de más se rechaza en vez de recortarse: un conjunto aplicado a medias
// deja al usuario creyendo que la sala está configurada mientras un jugador no
// puede entrar.
const MaxFilters = 40

// FilterSpec es un filtro decidido, listo para que la capa de Windows lo copie.
type FilterSpec struct {
	// Key sale de la RANURA que ocupa el filtro, y no de su etiqueta.
	//
	// # Por qué de la ranura, que no es lo obvio
	//
	// La limpieza tiene que encontrar lo que dejó la ejecución anterior sin
	// recordar nada entre arranques. Derivando la clave de la etiqueta, un
	// permiso espejo lleva dentro el nombre de la regla, o sea el juego: cambiar
	// de juego cambia las etiquetas, cambia las claves, y los filtros del juego
	// anterior quedan HUÉRFANOS. Un permiso huérfano deja abierto un puerto que
	// ya nadie pidió, y no se ve, porque un filtro de WFP no aparece en `wf.msc`
	// ni en `Get-NetFirewallRule`.
	//
	// Con la clave por ranura, el conjunto de claves posibles es fijo y conocido:
	// barrer de 0 a [MaxFilters] borra todo lo que la compuerta pueda haber
	// puesto alguna vez, sin enumerar nada.
	//
	// La etiqueta sigue siendo descriptiva y viaja al nombre visible del filtro,
	// que es lo que se lee en `netsh wfp show filters`. Una identifica y la otra
	// explica.
	Key  [16]byte
	Slot int
	// Label explica. Viaja al nombre visible del filtro, que es lo que se lee en
	// `netsh wfp show filters`.
	Label string
	// Rule es el nombre de la regla del dominio que este filtro espeja, y va
	// vacío en los bloqueos. Existe para que la medición pueda nombrar lo que
	// encontró sin recortar la etiqueta con un corte de cadena.
	Rule  string
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
	if f.Conditions.LocalNet.IsValid() && f.Conditions.LocalAddr.IsValid() {
		// WFP une con O las condiciones del MISMO campo y con Y las de campos
		// distintos. La red local y la dirección local son el mismo campo, así
		// que pedir las dos no acota más: ENSANCHA. En un permiso eso abre el
		// puerto a todo el rango de la sala en vez de a la IP del host, y el
		// filtro se ve perfectamente razonable leyéndolo.
		return fmt.Errorf("filtro %q: lleva red local %v Y dirección local %v, y WFP "+
			"une por O las condiciones del mismo campo, así que pedir las dos ENSANCHA "+
			"el filtro en vez de acotarlo", f.Label, f.Conditions.LocalNet, f.Conditions.LocalAddr)
	}
	if f.Layer == RecvAcceptV6 {
		// Un filtro de la capa IPv6 con condiciones de dirección IPv4 no casa con
		// nada, y no falla: el bloqueo de IPv6 quedaría puesto sin bloquear nada.
		if f.Conditions.LocalNet.IsValid() || f.Conditions.LocalAddr.IsValid() ||
			len(f.Conditions.Remote) > 0 || len(f.Conditions.RemoteNets) > 0 {
			return fmt.Errorf("filtro %q: está en la capa IPv6 y lleva condiciones de "+
				"dirección IPv4, que ahí no casan con nada. El bloqueo quedaría puesto "+
				"sin bloquear", f.Label)
		}
	}
	return nil
}

// keyForSlot deriva la GUID de un filtro a partir de la ranura que ocupa.
//
// Determinista a propósito: al arrancar hay que poder borrar lo que dejó la
// ejecución anterior, y recordar las claves en disco añadiría un fichero que
// puede desincronizarse del sistema. Enumerar el sublayer sería la alternativa,
// y trae bastante más superficie de API por la misma respuesta.
//
// El espacio de nombres lleva versión: el día que cambie qué ocupa cada ranura,
// subirla evita que una limpieza borre por clave un filtro que significaba otra
// cosa.
func keyForSlot(slot int) [16]byte {
	const namespace = "kanpachi/wfp/gate/v1\x00slot "
	sum := sha256.Sum256([]byte(namespace + strconv.Itoa(slot)))

	var key [16]byte
	copy(key[:], sum[:16])
	// Se marcan los bits de versión y variante de UUID v4 para que sea una GUID
	// bien formada. Windows no lo exige y las herramientas que la muestren sí lo
	// esperan.
	key[6] = (key[6] & 0x0f) | 0x40
	key[8] = (key[8] & 0x3f) | 0x80
	return key
}

// AllKeys son las claves de TODAS las ranuras, ocupadas o no.
//
// Es lo que barre la limpieza al arrancar, y por eso no depende del conjunto
// deseado: lo que hay que borrar es justo lo que sobró de un conjunto que ya no
// se recuerda.
func AllKeys() [][16]byte {
	out := make([][16]byte, 0, MaxFilters)
	for i := 0; i < MaxFilters; i++ {
		out = append(out, keyForSlot(i))
	}
	return out
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
	if err := scope.Valid(); err != nil {
		return nil, fmt.Errorf("sin alcance el bloqueo de todo dejaría al usuario sin su "+
			"red de casa: %w", err)
	}

	// Las tres primeras ranuras son fijas. Que el bloqueo por adaptador sea
	// siempre la cero es lo que permite medir si la compuerta está puesta
	// preguntando por UNA clave conocida, sin enumerar.
	out := []FilterSpec{
		newSpec(0, "bloqueo de todo, por adaptador", RecvAcceptV4, Block, WeightBlockAll,
			Conditions{LUID: scope.LUID}),
		newSpec(1, "bloqueo de todo, por rango de la sala", RecvAcceptV4, Block, WeightBlockAll,
			Conditions{LocalNet: scope.Net}),
		newSpec(2, "bloqueo de todo IPv6, por adaptador", RecvAcceptV6, Block, WeightBlockAll,
			Conditions{LUID: scope.LUID}),
	}

	for _, r := range desired.Rules {
		s, err := permitFor(len(out), r, scope)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}

	if len(out) > MaxFilters {
		// Recortar sería peor que fallar: la sala quedaría configurada a medias
		// y el jugador que no entra no tendría nada que mirar.
		return nil, fmt.Errorf("la compuerta necesita %d filtros y el tope son %d. "+
			"Con %d reglas deseadas, alguien subió [domain.MaxPortRanges] sin subir "+
			"este tope", len(out), MaxFilters, len(desired.Rules))
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
func permitFor(slot int, r domain.FirewallRule, scope Scope) (FilterSpec, error) {
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
	if r.From == 0 || r.To == 0 {
		// Un permiso sin puerto abriría el adaptador entero para esos miembros,
		// que es exactamente lo que la compuerta existe para impedir.
		return FilterSpec{}, fmt.Errorf("regla %q: rango de puertos %d-%d, y el cero "+
			"en la compuerta se lee como cualquier puerto", r.Name, r.From, r.To)
	}
	if r.From > r.To {
		return FilterSpec{}, fmt.Errorf("regla %q: rango invertido %d-%d", r.Name, r.From, r.To)
	}

	remote := append([]netip.Addr(nil), r.Remote...)
	sort.Slice(remote, func(i, j int) bool { return remote[i].Less(remote[j]) })
	nets := append([]netip.Prefix(nil), r.Nets...)
	sort.Slice(nets, func(i, j int) bool { return nets[i].String() < nets[j].String() })

	s := newSpec(slot, "permiso espejo: "+r.Name, RecvAcceptV4, Permit, WeightPermit, Conditions{
		LUID:          scope.LUID,
		LocalAddr:     r.Local,
		LocalPortFrom: r.From,
		LocalPortTo:   r.To,
		Proto:         r.Proto,
		Remote:        remote,
		RemoteNets:    nets,
	})
	return s.mirroring(r.Name), nil
}

// mirroring anota de qué regla del dominio es espejo este filtro.
//
// Va aparte del constructor para que este siga teniendo una firma corta, y
// devuelve una copia en vez de mutar: un [FilterSpec] a medio construir que se
// escapara sería justo el tipo de cosa que el guardián del alcance existe para
// impedir.
func (f FilterSpec) mirroring(rule string) FilterSpec {
	f.Rule = rule
	return f
}

// newSpec es el ÚNICO constructor de filtros.
//
// Existe para que el guardián de arquitectura tenga algo concreto que vigilar:
// un literal `FilterSpec{...}` fuera de este fichero puede olvidarse el alcance
// y compilar igual.
func newSpec(slot int, label string, layer Layer, action Action, weight uint64, c Conditions) FilterSpec {
	return FilterSpec{
		Key:        keyForSlot(slot),
		Slot:       slot,
		Label:      label,
		Layer:      layer,
		Action:     action,
		Weight:     weight,
		Conditions: c,
	}
}

// GateKey es la clave por la que se pregunta si la compuerta está puesta.
//
// Es la ranura cero, o sea el bloqueo de todo por adaptador. Preguntar por una
// clave conocida evita enumerar, y la ranura cero es la correcta porque es el
// filtro sin el cual la compuerta no contiene nada: con los permisos espejo
// puestos y el bloqueo ausente, la lista vuelve a ser aditiva.
func GateKey() [16]byte { return keyForSlot(0) }
