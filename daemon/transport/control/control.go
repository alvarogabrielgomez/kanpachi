// Package control es el canal de la sala de la decisión 23.
//
// **Solo escucha en la máquina del host.** Los invitados marcan hacia afuera y
// nunca abren un puerto, así que su deny-all queda literalmente intacto y la
// superficie se concentra en una sola máquina, la que ya acepta más exposición
// por definición.
//
// Es el código que más revisión merece del proyecto: corre como SYSTEM y parsea
// mensajes de gente que está en la sala. Un fallo acá es ejecución remota como
// SYSTEM en la máquina del host. De ahí las reglas, y ninguna es opcional:
//
//  1. Tope de tamaño ANTES de deserializar, y pasarse cierra la conexión.
//  2. Tabla de mensajes cerrada, comprobada contra un mapa, jamás por reflexión.
//  3. Esquema estricto: un campo de más rechaza el mensaje entero.
//  4. Dos alcances. Por la puerta solo se admite un pedido de credencial; por la
//     sala solo se admite a las IPs de los miembros presentes.
//  5. Los tipos del dominio se reconstruyen por sus parsers, jamás se creen.
//
// No conoce Windows: son sockets y bytes, así que corre y se prueba en el job de
// Linux junto a core.
package control

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/core/port"
)

// maxDoorConns es el tope de conexiones simultáneas en la puerta.
//
// La puerta acepta desconocidos por definición, así que es la única superficie
// que alguien con el código puede intentar agotar. Dieciséis es holgado para una
// sala de cinco y ridículo como recurso.
const maxDoorConns = 16

// outBuffer es cuánto se amortigua cada canal de salida.
//
// Amortiguados y con emisión no bloqueante: el caso de uso llama a Close con el
// candado de la sesión tomado, así que una emisión que esperara lector podría
// abrazarse con él. Ver el contrato de [port.ControlChannel].
const outBuffer = 16

// Issuer es lo ÚNICO que este adaptador le puede pedir al núcleo.
//
// Una interfaz de un método, declarada acá y satisfecha por *usecase.Session,
// por lo mismo que en el supervisor y en el protocolo: lo que no está en esta
// lista no se puede llamar desde el código que atiende entrada de la sala.
// Emitir una credencial es política y vive en el caso de uso, que es quien sabe
// si esta máquina puede emitir, qué dirección le toca al que entra y cuánto
// vale.
type Issuer interface {
	IssueCredentialFor(ctx context.Context, req domain.CredentialRequest) (domain.Credential, error)
}

// ErrNotAttached es que falta el emisor. Servir la puerta sin él sería aceptar
// conexiones para no poder contestarlas.
var ErrNotAttached = errors.New("control: falta el emisor de credenciales")

// ErrNotDialed es pedir una credencial sin haber marcado.
var ErrNotDialed = errors.New("control: no hay conexión con el host")

// Identity is this installation's long-term key, used to sign what the host
// answers at the door.
//
// It is injected rather than loaded here for the usual reason: `identity.key` is
// CREATED by one package and consumed by everything else. A second loader is how
// two writers happen, and with them a key silently regenerated, which would cost
// this machine the face the people who already played with it know it by.
//
// Its zero means **do not sign**: the console daemon with no data directory and
// the tests have no reason to hold an identity, and an unsigned answer is a case
// the guest already knows how to treat.
type Identity struct {
	Public []byte
	Sign   func(msg []byte) []byte
}

// Signs says whether this identity can actually sign.
func (i Identity) Signs() bool { return i.Sign != nil && len(i.Public) > 0 }

// Deps son las piezas del canal.
type Deps struct {
	Clock port.Clock
	Log   port.Logger

	// Identity signs what the host hands out at the door. See [Identity].
	Identity Identity

	// Listen y Dial existen para el test y para nada más.
	//
	// El test necesita conexiones con direcciones virtuales que no existen en la
	// máquina donde corre CI. En producción quedan en cero y se usa la red de
	// verdad: nadie fuera de este paquete los arma, igual que las cadencias del
	// supervisor.
	Listen func(ctx context.Context, at netip.AddrPort) (net.Listener, error)
	Dial   func(ctx context.Context, to netip.AddrPort) (net.Conn, error)
}

// Channel implementa [port.ControlChannel].
type Channel struct {
	deps Deps

	mu     sync.Mutex
	issuer Issuer
	srv    *server
	cli    *client
	// llaves son el par efímero de ESTA SESIÓN, no de esta conexión.
	//
	// De la sesión y no de la conexión porque entrar son dos conexiones, la de
	// la puerta y la de la sala, y el host sella el código nuevo contra la llave
	// que recibió en el pedido de credencial, o sea contra la de la primera. Un
	// par nuevo al marcar a la sala haría que el código repartido no lo pudiera
	// abrir nadie.
	//
	// Se descarta al salir: Close las borra, así que la sala siguiente estrena
	// llave. No tocan el disco y no identifican a nadie entre salas.
	llaves keyPair

	// Los canales de salida se crean UNA vez y no se cierran jamás.
	//
	// Close para el oyente y el marcador, y deja los canales vivos: la sesión
	// llama a Close en cada salida de sala y vuelve a marcar al entrar a la
	// siguiente. Cerrarlos dejaría al supervisor tratando la segunda sala como
	// una suscripción muerta, que es lo que hace con un canal cerrado.
	presence  chan bool
	announces chan domain.RoomAnnounce
	notices   chan domain.RoomNotice
	codes     chan domain.Room
	// canaryReqs es lo que el HOST le pide a este invitado, y canaryReports lo
	// que los invitados le contestan a este host. Nunca están los dos vivos en
	// la misma máquina, y van igual como dos canales: uno solo obligaría a
	// mirar el rol para saber qué llegó.
	canaryReqs    chan domain.CanaryRequest
	canaryReports chan domain.CanaryReport
	// joins es el lado del HOST: qué miembro acaba de abrir su canal.
	//
	// Es el único sitio del programa que sabe ese instante, y hay una cosa que
	// solo se puede hacer entonces: hablarle. Ver [Channel.MemberChannels].
	joins chan netip.Addr
}

// New arma el canal. Todavía no escucha ni marca nada.
func New(deps Deps) *Channel {
	if deps.Listen == nil {
		deps.Listen = listenTCP
	}
	if deps.Dial == nil {
		deps.Dial = dialTCP
	}
	return &Channel{
		deps:      deps,
		presence:  make(chan bool, outBuffer),
		announces: make(chan domain.RoomAnnounce, outBuffer),
		notices:   make(chan domain.RoomNotice, outBuffer),
		codes:     make(chan domain.Room, outBuffer),

		canaryReqs:    make(chan domain.CanaryRequest, outBuffer),
		canaryReports: make(chan domain.CanaryReport, outBuffer),
		joins:         make(chan netip.Addr, outBuffer),
	}
}

// Attach conecta el emisor de credenciales.
//
// Va aparte del constructor porque la dependencia es circular por naturaleza: la
// sesión recibe este canal por su Deps, y este canal necesita a la sesión para
// contestar la puerta. El cableado construye uno, después el otro, y los une.
func (c *Channel) Attach(i Issuer) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.issuer = i
}

func (c *Channel) HostPresence() <-chan bool                 { return c.presence }
func (c *Channel) Announcements() <-chan domain.RoomAnnounce { return c.announces }
func (c *Channel) Notices() <-chan domain.RoomNotice         { return c.notices }
func (c *Channel) Codes() <-chan domain.Room                 { return c.codes }

// CanaryRequests es el lado del INVITADO: lo que el host le pide marcar.
//
// Cada pedido llega con la dirección del host YA PUESTA por el adaptador, desde
// la conexión. En el cable no viaja ninguna dirección, así que quien lea de este
// canal no puede recibir un destino que el host no sea. Ver
// [domain.CanaryRequest].
func (c *Channel) CanaryRequests() <-chan domain.CanaryRequest { return c.canaryReqs }

// CanaryReports es el lado del HOST: lo que contestan los miembros.
//
// Igual que arriba, el remitente sale de la conexión. Es una PISTA y no una
// prueba: lo que el host da por cierto es lo que vio su propio canario.
func (c *Channel) CanaryReports() <-chan domain.CanaryReport { return c.canaryReports }

// MemberChannels avisa, en el HOST, de que un miembro acaba de abrir su canal.
//
// # Para qué hace falta un evento y no basta con preguntar cada tanto
//
// Porque lo que se hace con él tiene una ventana, y fuera de la ventana falla.
// Cuando el host reabre su sala tras reiniciar, el motor le pone a todos los
// miembros en la tabla en el mismo segundo, ANTES de que ninguno haya vuelto a
// marcar el canal de control; el host quiere avisarles de que perdió sus
// credenciales, y en ese instante no tiene por dónde. Medido el 2026-08-11: el
// primer intento falla siempre.
//
// Sin este evento eso queda dependiendo de cuándo vuelva a correr el latido, que
// es cada quince segundos, y de la suerte de que algún evento del motor lo
// adelante. En dos mediciones del mismo caso dio 6 y 29 segundos. Con el evento
// es en cuanto se puede, que es el único momento correcto.
//
// Lleva la dirección y no la conexión: el que lo consume razona sobre miembros.
func (c *Channel) MemberChannels() <-chan netip.Addr { return c.joins }

func (c *Channel) log() port.Logger { return c.deps.Log }
func (c *Channel) now() time.Time   { return c.deps.Clock.Now() }
func (c *Channel) dial(ctx context.Context, to netip.AddrPort) (net.Conn, error) {
	return c.deps.Dial(ctx, to)
}

// Close corta el oyente y el marcador. Es IDEMPOTENTE y **nunca espera a que
// alguien lea los canales de salida**.
//
// No es higiene, es lo que evita un abrazo mortal real: el caso de uso lo llama
// con el candado de la sesión tomado, y si esperara a una goroutine que a su vez
// estuviera bloqueada escribiendo en HostPresence, las dos se esperarían para
// siempre y el daemon quedaría colgado con la sala a medio cerrar. Por eso las
// emisiones son no bloqueantes y esto no espera a nadie.
func (c *Channel) Close() error {
	c.mu.Lock()
	srv, cli := c.srv, c.cli
	c.srv, c.cli = nil, nil
	c.llaves = keyPair{}
	c.mu.Unlock()

	if srv != nil {
		srv.stop()
	}
	if cli != nil {
		cli.stop()
	}
	return nil
}

// sessionKeys devuelve el par de esta sesión, generándolo la primera vez.
func (c *Channel) sessionKeys() (keyPair, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.llaves.priv == nil {
		k, err := newKeyPair()
		if err != nil {
			return keyPair{}, err
		}
		c.llaves = k
	}
	return c.llaves, nil
}

// emitir manda por un canal de salida sin bloquear jamás.
//
// Descartar y registrar es peor que entregar y mejor que colgarse: el supervisor
// drena estos canales en un bucle apretado, así que un búfer lleno significa que
// el despachador está trabado, y en ese caso lo último que ayuda es sumarle un
// escritor bloqueado.
func emitir[T any](c *Channel, ch chan T, v T, qué string) {
	select {
	case ch <- v:
	default:
		c.log().Warn("se descartó un mensaje del canal de control, nadie lo estaba leyendo", "tipo", qué)
	}
}

func listenTCP(ctx context.Context, at netip.AddrPort) (net.Listener, error) {
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", at.String())
	if err != nil {
		return nil, fmt.Errorf("escuchando en %s: %w", at, err)
	}
	return ln, nil
}

func dialTCP(ctx context.Context, to netip.AddrPort) (net.Conn, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", to.String())
	if err != nil {
		return nil, fmt.Errorf("marcando a %s: %w", to, err)
	}
	return conn, nil
}

// addrOf saca la dirección virtual del otro extremo.
//
// Del socket y jamás de un campo del mensaje: la dirección es lo único de esta
// conversación que el que habla no elige, porque se la asignó el host dentro de
// la credencial y el motor la impone en el overlay.
func addrOf(conn net.Conn) (netip.Addr, bool) {
	if conn == nil || conn.RemoteAddr() == nil {
		return netip.Addr{}, false
	}
	ap, err := netip.ParseAddrPort(conn.RemoteAddr().String())
	if err != nil {
		return netip.Addr{}, false
	}
	return ap.Addr().Unmap(), true
}
