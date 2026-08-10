// Package gate decide QUÉ bloquea y qué abre la compuerta, sin tocar ningún
// sistema operativo.
//
// # Qué es la compuerta y por qué existe
//
// La compuerta es «denegar todo lo que entre por el adaptador virtual, salvo
// esto». Es lo que convierte la lista de lo que se abre de ADITIVA en COMPLETA,
// y mientras sea aditiva una regla permisiva ajena de escritorio remoto alcanza
// al usuario por la red virtual. Ver decisión 27, con las cuatro mediciones que
// la sostienen.
//
// # Este paquete no sabe cómo se instala lo que decide
//
// Emite [Spec], y el adaptador de cada sistema lo traduce:
//
//   - Windows lo copia a filtros de WFP, donde hizo falta una segunda capa
//     porque las reglas del Firewall de Windows no pueden expresar «denegar
//     salvo esto» por sí solas: sus bloqueos explícitos ganan sobre cualquier
//     permiso en conflicto, así que un bloqueo total taparía también los
//     permisos propios. En WFP un Block es HARD y un Permit es SOFT, y esa
//     asimetría es la que anula la regla ajena conservando el veto del usuario.
//   - Linux lo copia a reglas de nftables, donde una tabla propia ya lo expresa
//     en una sola capa: un `drop` corta la evaluación de todas las cadenas del
//     hook y un `accept` no es final.
//
// Los dos necesitan la misma decisión, y por eso se toma acá una sola vez.
//
// # Por qué este paquete es puro
//
// Todo lo que se puede equivocar de forma cara se decide acá y lo prueba el CI
// de Linux. El fallo más caro posible de esta capa es un filtro SIN ALCANCE, y
// es el mismo fallo en los dos sistemas: en WFP aplica a todos los adaptadores
// de la máquina, y una regla de nftables sin interfaz ni dirección local tira
// todo el tráfico de entrada. No falla en ningún test funcional, deja al usuario
// sin su red de casa, y no se ve leyendo un diff.
package gate

import (
	"fmt"
	"net/netip"
	"sort"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// Action es qué hace un filtro cuando casa.
type Action uint8

const (
	// Block cierra, y en los dos sistemas es la acción fuerte: en WFP es HARD,
	// o sea que no lo puede permitir otro sublayer, y en nftables un `drop` corta
	// la evaluación de todas las cadenas del hook.
	Block Action = iota + 1
	// Permit abre, y en los dos sistemas es la acción débil: en WFP es SOFT, o
	// sea que lo puede bloquear otro sublayer, y en nftables un `accept` deja
	// que el paquete siga a las cadenas de prioridad posterior.
	//
	// Que sea débil es lo que conserva el veto del usuario. También es el motivo
	// por el que en Linux Kanpachi no puede abrir por encima del bloqueo de un
	// ufw o un firewalld: se audita y se informa, no se pisa.
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
// SOLO entrada, y las dos familias por separado. La salida no aparece en este
// tipo a propósito: bloquearla impediría que un invitado marque al puerto del
// juego del host, que es el caso central del producto. Lo que no existe en el
// tipo no se puede pedir por error.
//
// Windows los mapea a `ALE_AUTH_RECV_ACCEPT_V4` y `_V6`; Linux, al hook `input`
// con la familia correspondiente.
type Layer uint8

const (
	InboundV4 Layer = iota + 1
	InboundV6
)

func (l Layer) String() string {
	switch l {
	case InboundV4:
		return "entrada IPv4"
	case InboundV6:
		return "entrada IPv6"
	default:
		return "capa-inválida"
	}
}

// Pesos, que son la PRECEDENCIA entre los filtros propios.
//
// El permiso tiene que pesar más que el bloqueo de todo, o el bloqueo se lo
// lleva por delante y no habría forma de abrir el puerto del juego. Medido
// funcionando el 2026-08-04 con estos dos valores.
//
// Windows se los pasa a WFP, que arbitra por peso dentro del sublayer propio.
// Linux los ordena: nftables resuelve por PRIMERA COINCIDENCIA dentro de la
// cadena, así que más peso significa ir antes.
const (
	WeightBlockAll uint64 = 100
	WeightPermit   uint64 = 200
)

// Scope es DÓNDE aplica un filtro, y ningún filtro puede salir sin él.
//
// # Por qué son dos cosas y no una
//
// La interfaz es lo preciso. El prefijo de la sala es el respaldo, y existe por
// un riesgo concreto: si la condición de interfaz llegara vacía al reautorizar
// un flujo ya establecido, tras reiniciar el servicio o al cambiar de adaptador,
// el bloqueo dejaría de casar EN SILENCIO y la pantalla diría verde. Emitiendo
// el bloqueo por las dos vías, ninguna es el único asidero.
//
// El prefijo no puede pisar la red de casa porque la `/24` de la sala se elige
// en tiempo de ejecución contra las redes que la máquina ya tiene. Ver decisión
// 10.
type Scope struct {
	// Iface identifica el adaptador virtual de la SALA, con el número que use
	// cada sistema: el LUID en Windows, el `ifindex` en Linux.
	//
	// **Cero significa sin acotar y está prohibido**, y en los dos sistemas por
	// el mismo motivo: WFP lee el LUID cero como TODAS las interfaces, y una
	// regla de nftables sin `iifindex` casa con todas también. Quien lo resuelve
	// devuelve error y jamás cero. Ver `firewall.IfaceResolver`.
	Iface uint64
	// Net es el rango de la sala sobre ese adaptador.
	Net netip.Prefix
	// Lobby identifica el adaptador del VESTÍBULO, y cero cuando no lo hay.
	//
	// # Por qué el vestíbulo no trae su rango
	//
	// Porque no es suyo: el vestíbulo vive siempre en [domain.RendezvousSubnet],
	// igual para todas las salas de todo el mundo. Un campo por el que pasarlo
	// sería un campo por el que ensancharlo, y este es el adaptador donde llega
	// gente que TODAVÍA NO ES MIEMBRO, o sea el que menos puede permitírselo.
	//
	// Cero es un estado normal y frecuente: el invitado suelta el vestíbulo al
	// entrar, y el host lo tiene solo mientras acepta gente.
	Lobby uint64
}

// LobbyNet es el rango del vestíbulo, y no se pide: es constante.
func (s Scope) LobbyNet() netip.Prefix { return domain.RendezvousSubnet }

// HasLobby dice si el alcance cubre también el adaptador del vestíbulo.
func (s Scope) HasLobby() bool { return s.Lobby != 0 }

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
	if s.Iface == 0 {
		return fmt.Errorf("la compuerta necesita el adaptador, y llegó cero, que los dos " +
			"sistemas leen como TODAS las interfaces")
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
	if s.Lobby != 0 && s.Lobby == s.Iface {
		// Los dos adaptadores son dos redes distintas del motor. El mismo número
		// para las dos significa que alguien resolvió mal un nombre, y el
		// resultado sería un bloqueo del vestíbulo puesto sobre la sala y el
		// vestíbulo sin compuerta: descubierto justo el adaptador donde llega
		// gente que todavía no es miembro.
		return fmt.Errorf("la sala y el vestíbulo llegaron con el mismo adaptador (%d), "+
			"y son dos redes distintas", s.Iface)
	}
	return nil
}

// Conditions son las condiciones de un filtro.
//
// Los ceros significan "sin condición", que para un bloqueo es lo peligroso y
// para un permiso es lo inútil. Por eso nadie construye esto a mano fuera de
// este fichero: lo vigila un guardián de internal/arch.
type Conditions struct {
	// Iface acota por adaptador, con el número de [Scope.Iface]. Cero es sin
	// acotar.
	Iface uint64
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
	// que el dominio acepta. Los dos sistemas tienen condición de rango de
	// puertos y es lo que corresponde usar.
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
	if c.Iface != 0 || c.LocalAddr.IsValid() {
		return true
	}
	// Un prefijo de cero bits está puesto y no acota nada: casa con toda
	// dirección local de la máquina. Contarlo como alcance sería contar como
	// protección justo el caso que hay que impedir.
	return c.LocalNet.IsValid() && c.LocalNet.Bits() > 0
}

// Las ranuras FIJAS, tres por adaptador.
//
// # Por qué son posiciones fijas y no un contador
//
// Porque la clave de un filtro se deriva de su ranura, así que la ranura ES la
// identidad. Fijándolas, medir si la compuerta está puesta es preguntar por una
// clave conocida sin enumerar nada, y barrer de 0 a [MaxFilters] borra todo lo
// que la compuerta pueda haber puesto, sin recordar nada entre arranques.
//
// Las del vestíbulo quedan reservadas aunque no haya vestíbulo. Corriendo los
// permisos hacia arriba cuando falta, un permiso ocuparía la ranura de un
// bloqueo del vestíbulo, y la limpieza siguiente lo borraría creyendo que barre
// un bloqueo que ya no aplica: un puerto que se cierra solo, sin nada que lo
// explique.
const (
	SlotRoomIface = iota
	SlotRoomNet
	SlotRoomV6
	SlotLobbyIface
	SlotLobbyNet
	SlotLobbyV6
	// FirstPermitSlot es donde empiezan los permisos espejo, siempre.
	FirstPermitSlot
)

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

// Spec es un filtro decidido, listo para que el adaptador de cada sistema lo
// traduzca.
//
// **No lleva identidad de backend**, y eso es deliberado. Cómo se reconoce un
// filtro puesto es problema de quien lo pone, y los dos sistemas lo resuelven
// distinto: Windows deriva una GUID estable de [Spec.Slot], porque tiene que
// encontrar lo que dejó la ejecución anterior sin recordar nada entre arranques;
// Linux reconstruye la cadena entera en una transacción, así que la identidad
// por posición es implícita y no hace falta nombrar nada.
type Spec struct {
	// Slot es la POSICIÓN fija que ocupa este filtro, y por eso es su identidad.
	//
	// # Por qué la posición, que no es lo obvio
	//
	// La alternativa era derivar la identidad de la etiqueta, y ahí un permiso
	// espejo lleva dentro el nombre de la regla, o sea el juego: cambiar de juego
	// cambia las etiquetas, cambia las identidades, y los filtros del juego
	// anterior quedan HUÉRFANOS. Un permiso huérfano deja abierto un puerto que
	// ya nadie pidió, y no se ve, porque un filtro de WFP no aparece en `wf.msc`
	// ni en `Get-NetFirewallRule`.
	//
	// Con la posición, el conjunto de identidades posibles es fijo y conocido:
	// barrer de 0 a [MaxFilters] borra todo lo que la compuerta pueda haber
	// puesto alguna vez, sin enumerar nada.
	Slot int
	// Label explica. Viaja al nombre visible del filtro en Windows, que es lo que
	// se lee en `netsh wfp show filters`, y al `comment` de la regla en Linux,
	// que es lo que se lee en `nft list ruleset`. La posición identifica y esto
	// explica.
	Label string
	// Rule es el nombre de la regla del dominio que este filtro espeja, y va
	// vacío en los bloqueos. Existe para que la medición pueda nombrar lo que
	// encontró sin recortar la etiqueta con un corte de cadena.
	Rule  string
	Layer Layer
	// Action y Weight deciden la precedencia entre los filtros propios. Ver los
	// pesos, arriba.
	Action     Action
	Weight     uint64
	Conditions Conditions
}

// Validate es la última puerta antes de que esto llegue a la API del sistema.
//
// Un filtro sin alcance no falla en ningún test funcional: aplica a TODOS los
// adaptadores, y con un bloqueo duro deja al usuario sin la red de casa. Por eso
// se comprueba acá además de en el guardián de arquitectura: uno vigila cómo se
// escribe el código y este vigila lo que de verdad se va a instalar.
func (f Spec) Validate() error {
	if f.Layer != InboundV4 && f.Layer != InboundV6 {
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
	if f.Layer == InboundV6 {
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

// SpecsFor traduce el conjunto deseado a los filtros de la compuerta.
//
// # Lo que emite, y en qué orden importa
//
// Dos bloqueos de todo, uno por adaptador y otro por prefijo de la sala, en
// IPv4. Ver [Scope] para el porqué de los dos.
//
// Un bloqueo de todo por adaptador en IPv6, SIN permisos espejo. Kanpachi direcciona
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
func SpecsFor(desired domain.RuleSet, scope Scope) ([]Spec, error) {
	if err := scope.Valid(); err != nil {
		return nil, fmt.Errorf("sin alcance el bloqueo de todo dejaría al usuario sin su "+
			"red de casa: %w", err)
	}

	// Las SEIS primeras ranuras son fijas, tres por adaptador. Que el bloqueo
	// por adaptador de la sala sea siempre la cero es lo que permite medir si la
	// compuerta está puesta preguntando por UNA clave conocida, sin enumerar, y
	// las del vestíbulo se miden igual por sus propias ranuras.
	out := []Spec{
		newSpec(SlotRoomIface, "bloqueo de todo, por adaptador de la sala", InboundV4, Block, WeightBlockAll,
			Conditions{Iface: scope.Iface}),
		newSpec(SlotRoomNet, "bloqueo de todo, por rango de la sala", InboundV4, Block, WeightBlockAll,
			Conditions{LocalNet: scope.Net}),
		newSpec(SlotRoomV6, "bloqueo de todo IPv6, por adaptador de la sala", InboundV6, Block, WeightBlockAll,
			Conditions{Iface: scope.Iface}),
	}

	// El vestíbulo se cubre igual que la sala, y NO es un extra. Es el adaptador
	// al que llega gente que todavía no es miembro: dejarlo sin compuerta es
	// dejar la lista de permitidos en ADITIVA justo donde puede tocar cualquiera
	// que tenga el código, que es el caso que la compuerta existe para cerrar.
	if scope.HasLobby() {
		out = append(out,
			newSpec(SlotLobbyIface, "bloqueo de todo, por adaptador del vestíbulo", InboundV4, Block, WeightBlockAll,
				Conditions{Iface: scope.Lobby}),
			newSpec(SlotLobbyNet, "bloqueo de todo, por rango del vestíbulo", InboundV4, Block, WeightBlockAll,
				Conditions{LocalNet: scope.LobbyNet()}),
			newSpec(SlotLobbyV6, "bloqueo de todo IPv6, por adaptador del vestíbulo", InboundV6, Block, WeightBlockAll,
				Conditions{Iface: scope.Lobby}),
		)
	}

	// Los permisos arrancan SIEMPRE en la misma ranura, haya vestíbulo o no. Con
	// las ranuras corridas, una sala sin vestíbulo pondría un permiso en la
	// ranura del bloqueo del vestíbulo, y la limpieza siguiente lo borraría
	// creyendo que barre un bloqueo que ya no aplica.
	slot := FirstPermitSlot
	for _, r := range desired.Rules {
		s, err := permitFor(slot, r, scope)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
		slot++
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
// El permiso lleva el MISMO alcance que la regla de la capa que abre, más el
// adaptador. Que sean espejo es lo que hace que la compuerta no abra nada que
// los permisos visibles no abran ya: quien audite con sus herramientas de
// siempre ve la lista completa de lo que está abierto.
//
// # Cuál de los dos adaptadores
//
// Lo dice la dirección LOCAL de la regla, y no un campo que alguien rellena:
// una dirección dentro de [domain.RendezvousSubnet] solo puede estar en el
// adaptador del vestíbulo, y cualquier otra en el de la sala. Deducirlo de la
// dirección es lo que impide que el permiso de la puerta acabe acotado al
// adaptador equivocado, donde no casaría con nada y dejaría la puerta cerrada
// con la compuerta diciendo que está puesta.
func permitFor(slot int, r domain.FirewallRule, scope Scope) (Spec, error) {
	if !r.Local.IsValid() {
		return Spec{}, fmt.Errorf("regla %q: sin dirección local", r.Name)
	}

	iface := scope.Iface
	if domain.RendezvousSubnet.Contains(r.Local) {
		if !scope.HasLobby() {
			// Fallar es lo correcto: acotarla a la sala pondría el permiso donde
			// no casa, y emitirla sin adaptador la pondría en TODOS. El primero
			// deja la puerta cerrada en silencio, el segundo abre el puerto del
			// canal en la red de casa del usuario.
			return Spec{}, fmt.Errorf("regla %q: está en %v, o sea en el vestíbulo, "+
				"y el alcance llegó sin adaptador de vestíbulo", r.Name, domain.RendezvousSubnet)
		}
		iface = scope.Lobby
	}
	if len(r.Remote) == 0 && len(r.Nets) == 0 {
		// El dominio ya lo prohíbe. Se recomprueba porque un permiso sin alcance
		// remoto en la compuerta abriría el puerto a cualquiera que alcance el
		// adaptador, que es justo lo que la compuerta existe para impedir.
		return Spec{}, fmt.Errorf("regla %q: sin alcance remoto, y eso en la "+
			"compuerta se lee como cualquiera", r.Name)
	}
	if r.Proto == domain.ProtoBoth {
		return Spec{}, fmt.Errorf("regla %q: proto both no llega hasta acá, "+
			"[domain.BuildRuleSet] lo expande en dos reglas", r.Name)
	}
	if r.From == 0 || r.To == 0 {
		// Un permiso sin puerto abriría el adaptador entero para esos miembros,
		// que es exactamente lo que la compuerta existe para impedir.
		return Spec{}, fmt.Errorf("regla %q: rango de puertos %d-%d, y el cero "+
			"en la compuerta se lee como cualquier puerto", r.Name, r.From, r.To)
	}
	if r.From > r.To {
		return Spec{}, fmt.Errorf("regla %q: rango invertido %d-%d", r.Name, r.From, r.To)
	}

	remote := append([]netip.Addr(nil), r.Remote...)
	sort.Slice(remote, func(i, j int) bool { return remote[i].Less(remote[j]) })
	nets := append([]netip.Prefix(nil), r.Nets...)
	sort.Slice(nets, func(i, j int) bool { return nets[i].String() < nets[j].String() })

	s := newSpec(slot, "permiso espejo: "+r.Name, InboundV4, Permit, WeightPermit, Conditions{
		Iface:         iface,
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
// devuelve una copia en vez de mutar: un [Spec] a medio construir que se
// escapara sería justo el tipo de cosa que el guardián del alcance existe para
// impedir.
func (f Spec) mirroring(rule string) Spec {
	f.Rule = rule
	return f
}

// newSpec es el ÚNICO constructor de filtros.
//
// Existe para que el guardián de arquitectura tenga algo concreto que vigilar:
// un literal `Spec{...}` fuera de este fichero puede olvidarse el alcance
// y compilar igual.
func newSpec(slot int, label string, layer Layer, action Action, weight uint64, c Conditions) Spec {
	return Spec{
		Slot:       slot,
		Label:      label,
		Layer:      layer,
		Action:     action,
		Weight:     weight,
		Conditions: c,
	}
}
