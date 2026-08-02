package domain

import (
	"errors"
	"fmt"
	"net/netip"
	"sort"
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
// forma de expresar "cualquiera" en Remote, porque el cero de este campo
// significa que la regla no debe existir. Las tres ausencias son invariantes,
// y lo que no existe en el tipo no se puede pedir por error.
type FirewallRule struct {
	// Name es determinista a partir del perfil y el rango, para que el diff
	// declarativo empareje la regla deseada con la aplicada sin guardar ids.
	Name string

	// Proto nunca es ProtoBoth acá: una regla de Windows tiene un protocolo y
	// solo uno, así que [BuildRuleSet] expande el rango en dos reglas.
	Proto Proto
	From  uint16
	To    uint16

	// Local es la IP del adaptador kanpachi0. El alcance va por dirección y no
	// por adaptador porque la API de firewall de Windows no filtra por nombre
	// de interfaz.
	Local netip.Addr
	// Remote son las IPs virtuales de los miembros presentes. Nunca vacío en
	// una regla emitida.
	Remote []netip.Addr
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
func (rs RuleSet) IsEmpty() bool { return len(rs.Rules) == 0 }

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
