package domain

import "net/netip"

// FirewallProfile son los tres perfiles del Firewall de Windows.
//
// Las reglas se aplican a los tres, siempre. No porque el adaptador pueda
// estar en cualquiera, que también, sino porque Kanpachi NO depende de lograr
// clasificar la red como privada: un adaptador sin puerta de enlace queda como
// "Red no identificada" y Windows lo mete en Público. Ver decisión 15.
type FirewallProfile uint8

const (
	ProfileDomain FirewallProfile = iota + 1
	ProfilePrivate
	ProfilePublic
)

func (p FirewallProfile) String() string {
	switch p {
	case ProfileDomain:
		return "dominio"
	case ProfilePrivate:
		return "privado"
	case ProfilePublic:
		return "público"
	default:
		return "perfil-inválido"
	}
}

// AllFirewallProfiles es a lo que se aplica todo lo que Kanpachi crea.
func AllFirewallProfiles() []FirewallProfile {
	return []FirewallProfile{ProfileDomain, ProfilePrivate, ProfilePublic}
}

// FirewallProfileState es si un perfil del firewall está encendido.
type FirewallProfileState struct {
	Profile FirewallProfile
	Enabled bool
}

// ForeignRule es una regla de firewall que Kanpachi NO creó y que deja al
// juego alcanzable por fuera de su control.
//
// La deja el instalador del juego o un diálogo previo de Windows que el
// usuario despachó con "Permitir". Su efecto es que expulsar a alguien de la
// sala no lo tapa: sigue alcanzando el juego desde la LAN de casa.
//
// La consulta va contra el ALMACÉN DE REGLAS del firewall, buscando por ruta
// de ejecutable. No enumera procesos, no detecta si el juego está corriendo y
// no le importa: la regla existe en disco haya o no partida.
type ForeignRule struct {
	Name       string
	Executable string
	Profiles   []FirewallProfile
	// WasEnabled es el estado previo, para restaurar. Se persiste ANTES de
	// tocar nada, en suspended-rules.json, para poder deshacerlo tras una
	// salida sucia.
	WasEnabled bool
}

// PortMapping es un mapeo que existe en el router del usuario.
//
// Es SOLO LECTURA y alimenta el módulo de alertas de la decisión 19. No hay
// tipo ni puerto para crear ni borrar mapeos, y esa ausencia es deliberada: lo
// que no existe en la interfaz no se puede llamar por error. El router del
// usuario no se toca nunca.
type PortMapping struct {
	Proto        Proto
	ExternalPort uint16
	InternalIP   netip.Addr
	InternalPort uint16
	Description  string
}

// ControlScope es dónde escucha el host y a quién le acepta qué.
//
// Son DOS direcciones porque son dos conversaciones con dos modelos de
// confianza distintos, y meterlas en una sola sería regalar la de la sala a
// cualquiera que tenga el código:
//
//	Lobby  la dirección del host en el vestíbulo. Es la PUERTA: acepta a
//	       cualquiera que haya llegado hasta ahí, o sea a cualquiera con el
//	       invite ID, y lo único que admite es un pedido de credencial. Nada
//	       más. Un mensaje de otro tipo por acá se descarta sin interpretarse.
//	Room   su IP en la red real. Acepta SOLO a los miembros presentes, y es
//	       por donde va todo lo demás: expulsión, cierre de sala, y por su
//	       sola existencia la presencia del host.
//
// Los invitados no tienen ninguna de las dos: marcan hacia afuera y no abren
// un puerto, así que su deny-all queda literalmente intacto.
//
// Este es el código que más revisión merece del proyecto: corre como SYSTEM y
// parsea entrada de gente semi-confiable. Tope de tamaño antes de
// deserializar, esquema cerrado sin tipos arbitrarios, tope de conexiones por
// dirección, y en Room el rechazo de toda IP que no esté en Members.
type ControlScope struct {
	Lobby   netip.Addr
	Room    netip.Addr
	Members []netip.Addr
}

// AlertKind es el conjunto cerrado de cosas que Kanpachi no controla y que
// anulan su promesa si nadie las mira.
type AlertKind uint8

const (
	// AlertFirewallOff: un perfil del Firewall de Windows está apagado. Con el
	// firewall apagado, las reglas de Kanpachi no restringen nada.
	AlertFirewallOff AlertKind = iota + 1
	// AlertRulesTampered: las reglas del grupo Kanpachi no están como se
	// aplicaron. Algo o alguien las cambió.
	AlertRulesTampered
	// AlertRouterMapping: el router tiene un mapeo hacia esta máquina. Kanpachi
	// no lo puso y no lo va a quitar, lo dice.
	AlertRouterMapping
	// AlertForeignRule: el propio juego dejó una regla que lo hace alcanzable
	// sin pasar por Kanpachi.
	AlertForeignRule
	// AlertLobbyConflict: una red de esta máquina pisa el /24 fijo del
	// vestíbulo. Entrar a salas ajenas puede fallar, y sin este aviso el fallo
	// sería indiagnosticable. Ver "El vestíbulo tiene un /24 fijo" en
	// 03-arquitectura.md.
	AlertLobbyConflict
)

// Alert es un hallazgo del módulo de exposición.
//
// Viaja dentro de Status() y no por un canal aparte: el módulo publica su
// último resultado y Status() lo arrastra, así que una alerta nunca puede
// bloquear ni retrasar una respuesta. Ninguna comprobación devuelve error
// fatal: devuelven hallazgos.
type Alert struct {
	Kind   AlertKind
	Detail string
}
