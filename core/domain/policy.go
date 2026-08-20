package domain

import (
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"strings"
)

// ErrRuleForbiddenPort es el último cortafuegos entre un perfil y el Firewall
// de Windows.
//
// No debería poder dispararse: un GameProfile solo existe validado. Se
// comprueba igual porque este es el sitio donde un puerto se abre de verdad, y
// la invariante "puertos prohibidos siempre" tiene que vivir donde ocurre el
// acto, no solo donde se lee el archivo. Si salta, es un bug del programa, no
// un perfil malo.
var ErrRuleForbiddenPort = errors.New("policy: se intentó abrir un puerto prohibido")

// ErrRuleWideOpen es el guardián del único campo que acepta un prefijo.
//
// [FirewallRule.Nets] existe para la puerta del vestíbulo y para nada más. Sin
// esta comprobación, ese campo sería la forma de escribir "cualquiera" que el
// resto del tipo se esfuerza en no tener: un /0 ahí abriría el puerto del canal
// a toda la red de casa del usuario.
var ErrRuleWideOpen = errors.New("policy: el alcance de la regla es más ancho que el vestíbulo")

// Role es quién eres en la sala. Lo fija CreateRoom o JoinRoom y no cambia en
// toda la vida de la sala: nadie hereda el rol de host, no hay promoción
// automática ni elección. Ver decisión 20.
type Role uint8

const (
	RoleHost Role = iota + 1
	RoleGuest
)

func (r Role) String() string {
	switch r {
	case RoleHost:
		return "host"
	case RoleGuest:
		return "invitado"
	default:
		return "rol-inválido"
	}
}

// FirewallRule es una regla concreta, ya lista para el almacén de Windows.
//
// Solo entrantes: no hay campo para dirección porque no existe la salida. No
// hay campo para ejecutable porque jamás se permite por ejecutable. No hay
// forma de expresar "cualquiera" en el alcance remoto, porque las dos listas
// vacías significan que la regla no debe existir. Las tres ausencias son
// invariantes, y lo que no existe en el tipo no se puede pedir por error.
type FirewallRule struct {
	// Name es determinista a partir del perfil y el rango, para que el diff
	// declarativo empareje la regla deseada con la aplicada sin guardar ids.
	Name string

	// Proto nunca es ProtoBoth acá: una regla de Windows tiene un protocolo y
	// solo uno, así que [BuildRuleSet] expande el rango en dos reglas.
	Proto Proto
	From  uint16
	To    uint16

	// Local es la IP del adaptador kanpachi0.
	//
	// Acá decía que el alcance va por dirección y no por adaptador porque la
	// API de firewall de Windows no filtra por nombre de interfaz. **Es
	// falso.** `INetFwRule` tiene la propiedad `Interfaces`, y hay una regla
	// viva de Microsoft usándola sobre un adaptador virtual:
	//
	//	HNS Container Networking - DNS (UDP-In)
	//	Interfaces : vEthernet (WSL (Hyper-V firewall))
	//
	// La dirección se queda igual, y el nombre del adaptador se suma en el
	// adaptador de firewall cuando se escriba. Los dos a la vez, porque el
	// direccionamiento de Kanpachi sale de `100.64.0.0/10`, que es espacio
	// CGNAT: un router 4G que reparta `100.64.x` en la LAN de casa hace que
	// acotar solo por IP alcance también a la red física.
	//
	// El nombre del adaptador NO entra en este tipo, y esa ausencia es
	// deliberada: es un dato del entorno de la máquina, igual que la IP la pone
	// el motor. Acotar por interfaz vale SOLO para los permisos. En un bloqueo
	// abriría, porque un alcance que deja de casar convierte el bloqueo en
	// nada, y por eso la cuarentena de `FirewallGroupBase` no lo lleva.
	Local netip.Addr
	// Remote son las IPs virtuales de los miembros presentes.
	Remote []netip.Addr
	// Nets es el alcance remoto expresado como prefijo, y tiene UN uso: la
	// puerta del vestíbulo.
	//
	// Existe porque ahí no hay lista posible: quien está entrando todavía no es
	// miembro y su dirección en el vestíbulo es aleatoria, así que la regla
	// tiene que existir antes de saber quién va a llegar. Ningún perfil del
	// catálogo puede producir esto, y [ControlRules] es lo único que lo emite.
	//
	// Se valida contra [RendezvousSubnet], así que sigue sin existir forma de
	// escribir "cualquiera" en una regla de Kanpachi.
	Nets []netip.Prefix
}

// RuleSet es el estado DESEADO del firewall. Declarativo: FirewallPort recibe
// esto entero y calcula la diferencia contra lo aplicado. No hay "agregar
// regla" imperativo suelto, así que cada cambio de miembros o de juego
// regenera el conjunto completo y no puede quedar nada colgando de un cambio
// anterior.
type RuleSet struct {
	Rules []FirewallRule
}

// IsEmpty es el estado normal de una sala recién creada y de todo invitado en
// un juego de estrella: red cifrada, cero puertos abiertos, nadie alcanza a
// nadie.
//
// Deja de serlo en el host en cuanto entra alguien, porque ahí aparece el hueco
// del canal de control. Ver [ControlRules].
func (rs RuleSet) IsEmpty() bool { return len(rs.Rules) == 0 }

// Add suma reglas y conserva el orden estable.
//
// El orden importa por lo mismo que dentro de [BuildRuleSet]: el firewall
// calcula la diferencia contra lo que está vivo, y dos cálculos con la misma
// entrada tienen que producir el mismo conjunto para que no reescriba el
// firewall en cada latido.
func (rs *RuleSet) Add(rules ...FirewallRule) {
	if len(rules) == 0 {
		return
	}
	rs.Rules = append(rs.Rules, rules...)
	sort.Slice(rs.Rules, func(i, j int) bool { return rs.Rules[i].Name < rs.Rules[j].Name })
}

// Allows dice si este conjunto deja ese puerto alcanzable para ALGUIEN.
//
// Lo usa el canario para no ligarse a un puerto que la propia Kanpachi abrió: un
// oyente en un puerto permitido contesta con toda razón, y esa respuesta se
// leería como que la compuerta no contiene. Ver `daemon/adapter/canary`.
//
// # Por qué ignora el protocolo y a quién se le abrió
//
// A propósito, y por asimetría de costos. El canario liga los dos protocolos a
// la vez, y cualquier miembro de la sala puede ser el que marque, así que la
// lectura correcta es "permitido para alguien por algún protocolo".
//
// Descartar un puerto de más cuesta un efímero más, que el sistema tiene de
// sobra. Aceptar uno de menos cuesta **una alarma que grita con todo bien**, y
// una alarma que se enciende en falso enseña al usuario a ignorar la única
// comprobación que atraviesa la red de verdad.
func (rs RuleSet) Allows(port uint16) bool {
	for _, r := range rs.Rules {
		if r.From <= port && port <= r.To {
			return true
		}
	}
	return false
}

// BuildRuleSet traduce perfil + rol + miembros presentes al estado deseado.
//
// Es la capa de política, y su contraparte es el catálogo, que es la capa de
// conocimiento. El perfil dice qué necesita el juego; esta función decide qué
// es aceptable conceder.
//
// Devuelve el conjunto VACÍO, sin error, en los casos en que no hay nada que
// abrir, que son varios y todos normales:
//
//   - No hay juego activo. Es el estado por defecto al entrar a una sala.
//   - No hay ningún otro miembro presente. Sin RemoteAddresses no hay regla
//     posible, porque no existe forma de expresar "cualquiera".
//   - Eres invitado y el juego es de estrella, o sea client_ports vacío, que
//     es la enorme mayoría. Entre invitados no hay nada abierto.
func BuildRuleSet(p GameProfile, role Role, local netip.Addr, members []netip.Addr) (RuleSet, error) {
	if p.IsZero() {
		return RuleSet{}, nil
	}
	if !local.IsValid() {
		return RuleSet{}, fmt.Errorf("policy: el adaptador no tiene IP, no hay dónde anclar las reglas")
	}

	ranges := p.HostPorts
	if role == RoleGuest {
		ranges = p.ClientPorts
	}
	if len(ranges) == 0 {
		return RuleSet{}, nil
	}

	remote := normalizeMembers(local, members)
	if len(remote) == 0 {
		return RuleSet{}, nil
	}

	rules := make([]FirewallRule, 0, len(ranges)*2)
	for _, r := range ranges {
		if bad, hit := r.hitsForbidden(); hit {
			return RuleSet{}, fmt.Errorf("%w: %d, pedido por %s", ErrRuleForbiddenPort, bad, p.ID)
		}
		for _, proto := range r.Proto.expand() {
			rules = append(rules, FirewallRule{
				Name:   ruleName(p.ID, role, proto, r),
				Proto:  proto,
				From:   r.From,
				To:     r.To,
				Local:  local,
				Remote: remote,
			})
		}
	}

	// El orden es estable para que dos cálculos con la misma entrada produzcan
	// el mismo conjunto byte a byte. Sin eso, el diff declarativo vería
	// cambios donde no los hay y reescribiría el firewall en cada latido.
	sort.Slice(rules, func(i, j int) bool { return rules[i].Name < rules[j].Name })
	return RuleSet{Rules: rules}, nil
}

// ControlRules son los dos huecos del canal de la sala, y son los ÚNICOS que
// Kanpachi abre sin que ningún perfil los pida.
//
// Van aparte de [BuildRuleSet] y no dentro, y la razón es funcional: aquella
// devuelve el conjunto vacío cuando no hay juego activo, que es el estado normal
// de una sala recién creada, así que meter la regla del canal ahí la haría
// desaparecer justo en el caso más común. El host se quedaría escuchando detrás
// de su propio deny-all.
//
//	La puerta  Su IP en el vestíbulo, abierta al /24 fijo del vestíbulo entero.
//	           Acepta desconocidos por definición: quien toca todavía no es
//	           miembro. Lo que la acota no es esta regla, es que del otro lado
//	           solo se admite un pedido de credencial.
//	La sala    Su IP en kanpachi0, abierta SOLO a los miembros presentes. Con
//	           cero miembros no se emite: sin nadie a quien nombrar no hay regla
//	           posible, igual que en las reglas de juego.
//
// Solo el host. Un invitado no escucha nada, así que devuelve vacío y su
// deny-all queda intacto.
//
// Las direcciones en cero se saltan sin error: entrar a una sala pasa por
// estados donde el vestíbulo ya se dejó y la sala todavía no tiene IP.
func ControlRules(role Role, lobby, room netip.Addr, members []netip.Addr) ([]FirewallRule, error) {
	if role != RoleHost {
		return nil, nil
	}
	out := make([]FirewallRule, 0, 2)

	if lobby.IsValid() {
		if !LobbySpace.Contains(lobby) {
			// La puerta abre un /24 entero, así que anclarla a una dirección de
			// otra red sería abrir ese rango en un sitio donde no significa lo
			// que dice.
			//
			// Se comprueba contra el espacio y no contra un /24 concreto porque
			// cada sala deriva el suyo del código. Ver [Rendezvous.LobbySubnet].
			return nil, fmt.Errorf("%w: la puerta se pidió en %s, fuera de %s",
				ErrRuleWideOpen, lobby, LobbySpace)
		}
		// El /24 sale de la propia dirección en vez de recibirse aparte, y eso
		// hace imposible que se contradigan: la regla abre exactamente la red de
		// la dirección que ancla, no una que alguien dijo que era.
		out = append(out, FirewallRule{
			Name:  controlRuleName("puerta"),
			Proto: ProtoTCP,
			From:  ControlPort,
			To:    ControlPort,
			Local: lobby,
			Nets:  []netip.Prefix{netip.PrefixFrom(lobby, RoomPrefixBits).Masked()},
		})
	}

	if room.IsValid() {
		presentes := normalizeMembers(room, members)
		if len(presentes) > 0 {
			out = append(out, FirewallRule{
				Name:   controlRuleName("sala"),
				Proto:  ProtoTCP,
				From:   ControlPort,
				To:     ControlPort,
				Local:  room,
				Remote: presentes,
			})
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// controlRulePrefix distingue las reglas del canal de las de juego.
//
// Por el nombre y no por un campo nuevo, y es inequívoco: el id de un perfil
// solo admite minúsculas, dígitos y guiones, así que ningún juego del catálogo
// puede producir un nombre con espacios como este.
const controlRulePrefix = FirewallGroup + ": canal de control "

// controlRuleName lleva el grupo por delante, igual que las de juego, para que
// se lea en la consola del firewall de Windows.
func controlRuleName(dónde string) string { return controlRulePrefix + dónde }

// IsControl dice si la regla es del canal y no de un juego.
//
// Sirve para poder afirmar en un test lo que el producto promete: una sala sin
// juego activo no abre NINGÚN puerto de juego. El hueco del canal es otra cosa,
// tiene otro dueño y se abre siempre que el host esté en una sala.
func (r FirewallRule) IsControl() bool { return strings.HasPrefix(r.Name, controlRulePrefix) }

// GameRules son las reglas que pidió un perfil, sin el hueco del canal.
func (rs RuleSet) GameRules() []FirewallRule {
	out := make([]FirewallRule, 0, len(rs.Rules))
	for _, r := range rs.Rules {
		if !r.IsControl() {
			out = append(out, r)
		}
	}
	return out
}

// expand convierte ProtoBoth en las dos reglas que Windows necesita.
func (p Proto) expand() []Proto {
	if p == ProtoBoth {
		return []Proto{ProtoTCP, ProtoUDP}
	}
	return []Proto{p}
}

// ruleName lleva el grupo por delante para que se lea en la consola del
// firewall de Windows, y el rol dentro porque el mismo juego abre rangos
// distintos según se hospede o se entre.
// MirrorLocal duplica las reglas apuntando a OTRA dirección local de la misma
// máquina, y devuelve el conjunto con las dos.
//
// Existe por el desvío del modo contenedor y sería inútil sin él. El desvío
// reescribe el destino en `prerouting`, o sea ANTES de que la compuerta mire el
// paquete, así que lo que llega al filtro ya no va dirigido a la dirección de la
// sala sino a la del juego. Sin el espejo, la compuerta tiraría justo lo que el
// desvío acaba de encaminar bien, y las dos capas se estarían peleando en
// silencio. Ver [RedirectSpec].
//
// Es aditivo y conserva las originales: el desvío puede desaparecer entre dos
// mediciones, y mientras tanto lo que llega a la dirección de la sala tiene que
// seguir entrando.
//
// Con una dirección inválida o igual a la que ya tienen las reglas no hace
// nada, que es lo que corresponde: no hay dos destinos que cubrir.
func (s RuleSet) MirrorLocal(addr netip.Addr) RuleSet {
	if !addr.IsValid() || len(s.Rules) == 0 {
		return s
	}
	out := make([]FirewallRule, 0, len(s.Rules)*2)
	for _, r := range s.Rules {
		out = append(out, r)
		if r.Local == addr {
			continue
		}
		espejo := r
		espejo.Local = addr
		// El nombre lleva la dirección para que dos espejos de destinos
		// distintos no colisionen, y para que se lea en el firewall qué es esto
		// sin tener que adivinarlo.
		espejo.Name = r.Name + " -> " + addr.String()
		out = append(out, espejo)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return RuleSet{Rules: out}
}

func ruleName(profileID string, role Role, proto Proto, r PortRange) string {
	return fmt.Sprintf("%s: %s %s %s %s", FirewallGroup, profileID, role, proto, r.Spec())
}

// normalizeMembers ordena, deduplica y saca la propia IP.
//
// Sacar la propia importa: el motor reporta la máquina local como un peer más,
// y una regla que se autorizara a sí misma sería ruido en el mejor caso. El
// orden y la deduplicación son por el mismo motivo que el orden de las reglas,
// que el conjunto tiene que ser una función pura de la entrada.
func normalizeMembers(local netip.Addr, members []netip.Addr) []netip.Addr {
	seen := make(map[netip.Addr]bool, len(members))
	out := make([]netip.Addr, 0, len(members))
	for _, m := range members {
		if !m.IsValid() || m == local || seen[m] {
			continue
		}
		seen[m] = true
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Less(out[j]) })
	return out
}

// MemberIPs saca las IPs virtuales de una lista de peers, descartando la
// propia. Es lo que se le pasa a [BuildRuleSet] tras cada cambio de miembros.
func MemberIPs(peers []Peer) []netip.Addr {
	out := make([]netip.Addr, 0, len(peers))
	for _, p := range peers {
		if p.Self || !p.VirtualIP.IsValid() {
			continue
		}
		out = append(out, p.VirtualIP)
	}
	return out
}
