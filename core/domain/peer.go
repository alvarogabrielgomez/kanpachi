package domain

import (
	"encoding/hex"
	"fmt"
	"io"
	"net/netip"
	"time"
)

// PathKind dice por dónde llega el tráfico hasta un peer.
type PathKind uint8

const (
	// PathDirect es lo normal y lo deseable: túnel WireGuard peer a peer.
	PathDirect PathKind = iota + 1
	// PathRelay va por el seed. Funciona igual, va más lento, y se dice para
	// que nadie culpe al juego de una latencia que es de la red.
	PathRelay
	// PathSelf es uno mismo. No hay ruta que describir.
	PathSelf
	// PathUnconfirmed es quien el host admitió y tiene el canal de control
	// abierto, y a quien el motor todavía no reporta en su tabla de rutas.
	//
	// El camino NO se inventa. Lo que prueba su presencia es el socket del canal
	// de control, y un socket no dice si el tráfico va directo o por relay: eso
	// solo lo sabe el motor. Elegir "directo" pintaría la sala de verde sobre
	// una medición que nadie hizo, y elegir "relay" la acusaría de lenta sin
	// motivo. Ver [RoomState.AnyRelay], que por eso no cuenta este camino.
	PathUnconfirmed
)

func (p PathKind) String() string {
	switch p {
	case PathDirect:
		return "directo"
	case PathRelay:
		return "relay"
	case PathSelf:
		return ""
	case PathUnconfirmed:
		return "sin confirmar"
	default:
		return "desconocido"
	}
}

// Peer es alguien presente en la red virtual, tal como lo reporta el motor.
//
// El nombre no es único, no se verifica y no es una cuenta: es una etiqueta
// para que un humano reconozca a otro en una lista corta. Ver decisión 21.
type Peer struct {
	VirtualIP netip.Addr
	Name      Nickname
	Path      PathKind
	RTT       time.Duration

	// Self marca la propia máquina. El motor la reporta como un peer más y la
	// UI necesita distinguirla, porque expulsarse a uno mismo no es una
	// operación que deba poder pedirse.
	Self bool
	// Host marca a quien declaró hospedar. Sale del canje de credencial, no de
	// que el peer lo diga: un miembro que se declare host no cambia nada.
	Host bool
}

// NetCheck es el diagnóstico de red, y no es adorno: convierte "no conecta" en
// "tu router hace NAT simétrico, vas por relay".
type NetCheck struct {
	// NATKind sale del motor: cone, symmetric, cgnat, y demás.
	NATKind    string
	UDPBlocked bool

	// SeedRTT llega SIEMPRE VACÍO hoy, y no significa que ningún seed conteste.
	//
	// El motor no lo cuenta: su respuesta de diagnóstico lleva NAT, UDP,
	// direcciones públicas, dirección virtual y MTU, y nada del seed. El
	// adaptador rellena lo que hay, así que este mapa nunca se puebla. **Es un
	// campo a la espera de que el motor lo mande, no una medición.**
	//
	// Está dicho acá porque el mapa vacío se lee como "ningún seed contestó", y
	// una sonda lo dio por bueno y acusó de caído a un seed sano. Quien quiera
	// la respuesta de verdad la mide él: resolver el nombre y abrir un TCP al
	// puerto del seed es la misma prueba que haría el motor.
	SeedRTT map[string]time.Duration

	// MTU efectivo del camino, sondeado con ping de no fragmentar. Existe
	// porque el síntoma de un MTU mal puesto es cruel para un juego: el túnel
	// levanta, el ping anda, la partida conecta, y el mundo no termina de
	// cargar. Ver 03-arquitectura.md.
	MTU int

	// Subnet y SubnetReason son el /24 elegido y por qué. Un conflicto CGNAT
	// tiene que ser diagnosticable en un renglón.
	Subnet       netip.Prefix
	SubnetReason string
}

// EngineEventKind es el conjunto cerrado de cosas que el motor puede avisar.
//
// Cerrado a propósito: el supervisor hace un switch sobre esto, y un evento
// nuevo que nadie maneja sería un cambio de red que se pierde en silencio.
type EngineEventKind uint8

const (
	// EngineConnected: el motor entró a la red y hay al menos un peer.
	EngineConnected EngineEventKind = iota + 1
	// EnginePeersChanged: alguien entró o salió. Dispara el recálculo del
	// RuleSet, que es la misma operación que ejecuta un cambio de juego.
	EnginePeersChanged
	// EngineDegraded: la conexión sigue en pie y va peor de lo que iba.
	EngineDegraded
	// EngineDisconnected: se cayó. El watchdog decide si reintenta.
	EngineDisconnected
	// EngineDied: el proceso hijo se murió. Distinto de desconectado: acá no
	// hay motor con el que hablar y hay que arrancar otro.
	EngineDied
)

func (k EngineEventKind) String() string {
	switch k {
	case EngineConnected:
		return "conectado"
	case EnginePeersChanged:
		return "cambio de miembros"
	case EngineDegraded:
		return "degradado"
	case EngineDisconnected:
		return "desconectado"
	case EngineDied:
		return "el motor murió"
	default:
		return "evento desconocido"
	}
}

// EngineEvent es lo que el motor empuja por su canal.
//
// Lleva el motivo en texto porque cada transición queda en el log con su
// causa, y "desconectado" sin causa es una línea de log que no sirve para
// nada seis meses después.
type EngineEvent struct {
	Kind   EngineEventKind
	Reason string
}

// CredentialID identifica una credencial emitida, para poder revocarla.
type CredentialID string

// Credential es el permiso temporal de estar en la red REAL de una sala.
//
// Es lo que abre la sala, y el invite ID no. Expulsar es revocar esto, y por
// eso expulsar no es cooperativo: no hay mensaje que el expulsado pueda
// ignorar, es el motor cerrándole la sesión. Ver decisión 22.
//
// LO QUE NO LLEVA, y es la propiedad central de la decisión 2: el secreto de
// la red real. Se verificó contra los binarios de la v2.6.4, no contra la
// documentación: el handshake de un nodo temporal lleva `secret_digest` en
// ceros y `client_secret_proof: None`. Ese hecho es lo que hace que revocar
// sirva de verdad, porque quien entró nunca tuvo con qué volver por su cuenta.
// Una credencial con un campo de secreto lo tiraría todo abajo, así que el
// campo no existe.
//
// Se emite UNA POR MIEMBRO, no una por código. El código y las credenciales
// son los dos controles independientes del host: renovar el código cierra la
// puerta a quien está fuera sin tocar a los presentes, y revocar una
// credencial saca a uno solo sin tocar el código. Ver decisión 22.
type Credential struct {
	ID CredentialID
	// Token es lo que el motor recibe para entrar. Es opaco para el dominio y
	// no deriva de nada que el portador pueda recalcular.
	Token string
	// NetworkName es el nombre de la red REAL. Solo el nombre: es lo que hace
	// falta para entrar con credencial, y no alcanza para entrar sin ella.
	NetworkName string

	Name      Nickname
	VirtualIP netip.Addr
	Subnet    netip.Prefix

	IssuedAt  time.Time
	ExpiresAt time.Time
}

// String redacta el token. Una credencial en un mensaje de error terminaría en
// los logs, y los logs se copian al portapapeles y se pegan en el grupo.
func (c Credential) String() string {
	return fmt.Sprintf("Credential{%s, %s, %s, token:REDACTADO}", c.ID, c.Name, c.VirtualIP)
}

// Expired dice si la credencial ya no vale. Recibe el ahora por parámetro
// porque el dominio no lee el reloj: eso entra por [port.Clock], y sin ese
// corte no habría forma de testear el vencimiento sin dormir.
func (c Credential) Expired(now time.Time) bool {
	return !c.ExpiresAt.IsZero() && !now.Before(c.ExpiresAt)
}

// CredentialRequest es lo que un invitado le manda al host por el canal de
// control: quién dice ser y con qué llave.
//
// Va firmado contra la llave del solicitante y viaja por el vestíbulo, que es
// observable por definición: cualquiera con el invite ID puede derivar la
// identidad de encuentro, el seed incluido. Ver decisión 25.
type CredentialRequest struct {
	Name      Nickname
	PublicKey []byte
}

// HostSpec es todo lo que el motor necesita para levantar la red como nodo
// admin, que es el único que conoce el secreto de la red real y el único que
// puede emitir credenciales.
type HostSpec struct {
	// Rendezvous es el vestíbulo público, derivado del invite ID.
	Rendezvous Rendezvous
	// NetworkID y NetworkSecret son de la red REAL: 16 y 32 bytes aleatorios,
	// generados por el host, que no derivan de ningún string y no viven en
	// ningún servidor. El seed jamás los recibe.
	NetworkID     [16]byte
	NetworkSecret [32]byte

	Name   Nickname
	Subnet netip.Prefix
	Seeds  []string
}

// NO HAY campo de descubrimiento LAN, y la ausencia es la decisión 1.
//
// Encenderlo significa `--enable-udp-broadcast-relay`, o sea capturar el
// tráfico de la red de casa del usuario con un driver de captura de paquetes.
// Ningún juego del catálogo inicial lo necesita: el caso central es Project
// Zomboid, que es `direct_ip`. El perfil sí declara `lan_discovery`, porque el
// catálogo es la capa de conocimiento, y esta es la capa que decide qué se
// concede. El día que exista un juego que lo pida, se agrega con su propia
// entrada en 02-decisiones-de-diseno.md, no antes.

// RendezvousSpec es la entrada al VESTÍBULO, que es un paso aparte y no una
// variante de entrar a la sala.
//
// Es el paso 4 del flujo de conexión: el invitado entra a la red de encuentro,
// que cualquiera con el invite ID puede derivar, y lo único que hace ahí es
// pedirle la credencial al host. Después sale y entra a la red real.
//
// La subred la deriva cada sala de su código, y las dos máquinas llegan a la
// misma sin hablarse, porque el invitado necesita una dirección a la que marcar
// antes de tener nada del host. Ver [Rendezvous.LobbySubnet].
type RendezvousSpec struct {
	Rendezvous Rendezvous
	// Address es la dirección que este nodo toma en el vestíbulo. El host
	// siempre [Rendezvous.LobbyHostAddress], para que lo encuentren; el
	// invitado, cualquier otra.
	Address netip.Addr
	Name    Nickname
	Seeds   []string
}

// RendezvousGuestAddress reparte una dirección al que entra al vestíbulo,
// esquivando las que esta máquina ya tiene.
//
// Al azar dentro del /24 y sin coordinación con la otra punta. Un choque con
// OTRO invitado es posible y no importa: la estancia dura lo que tarda un canje
// de credencial, el motor resuelve el duplicado por su cuenta, y el precio de
// equivocarse es reintentar. Coordinarlo exigiría un canal, y el canal es lo que
// se está montando.
//
// # Por qué sí importa chocar consigo mismo
//
// Porque eso no lo resuelve nadie. Si la dirección sorteada ya existe en otra
// interfaz de ESTA máquina, el sistema no se la deja poner al adaptador virtual,
// y lo que se ve es el arranque colgado treinta segundos esperando una dirección
// que no va a llegar, sin nada que explique por qué. Se descarta acá, que es
// gratis y determinista, en vez de reintentar a ciegas.
//
// `local` son las rutas y direcciones de la máquina, las mismas que mira
// [LobbyOverlap]. Vacío es válido y significa no esquivar nada.
func RendezvousGuestAddress(r io.Reader, lobby netip.Prefix, local []netip.Prefix) (netip.Addr, error) {
	var b [1]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return netip.Addr{}, fmt.Errorf("domain: leyendo aleatoriedad para la dirección del vestíbulo: %w", err)
	}
	base := lobby.Addr().As4()
	// De la .2 a la .254: la .1 es del host, la .0 es la red y la .255 el
	// broadcast. Se arranca en el sorteo y se avanza en círculo, igual que
	// [pickSubnet]: así esquivar una ocupada no sesga el reparto.
	start := int(b[0]) % 253
	for i := 0; i < 253; i++ {
		base[3] = byte(2 + (start+i)%253)
		cand := netip.AddrFrom4(base)
		if !occupied(local, cand) {
			return cand, nil
		}
	}
	// Las 253 ocupadas significa que esta máquina tiene el /24 entero, que es el
	// conflicto que [LobbyOverlap] ya nombra. Se devuelve la sorteada para que
	// falle donde se entiende, con el aviso del conflicto ya puesto, en vez de
	// con un error de aritmética de direcciones.
	base[3] = byte(2 + start)
	return netip.AddrFrom4(base), nil
}

// occupied dice si alguna red local ya contiene esa dirección.
func occupied(local []netip.Prefix, a netip.Addr) bool {
	for _, p := range local {
		if p.IsValid() && p.Contains(a) {
			return true
		}
	}
	return false
}

// GuestSpec es lo que el motor necesita para entrar como nodo temporal a la
// red REAL. Nunca recibe el secreto: lo que trae es la credencial que emitió
// el host, y la subred y la IP vienen dentro de ella.
type GuestSpec struct {
	Credential Credential

	Name  Nickname
	Seeds []string
}

// RealNetworkName es el nombre de la red REAL, el que llevan las credenciales
// que el host emite.
//
// Deriva del NetworkID aleatorio y no del invite ID, que es justo la
// diferencia con [Rendezvous.NetworkName]: dos nombres con el mismo prefijo y
// dos orígenes que no tienen nada que ver. Quien tenga el invite ID puede
// calcular el del vestíbulo y jamás este.
func (h HostSpec) RealNetworkName() string {
	return "kanpachi-" + hex.EncodeToString(h.NetworkID[:])
}

// String redacta el secreto, por el mismo motivo que [Rendezvous.String]: un
// %+v en un mensaje de error terminaría en los logs locales, que se copian al
// portapapeles con el botón de diagnóstico y se pegan en el grupo. Acá el
// valor sí es sensible de verdad.
func (h HostSpec) String() string {
	return fmt.Sprintf("HostSpec{%s, red-real:%x, secret:REDACTADO, subnet:%s}",
		h.Rendezvous.NetworkName(), h.NetworkID[:4], h.Subnet)
}
