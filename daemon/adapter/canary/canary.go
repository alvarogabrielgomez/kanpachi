// Package canary abre un oyente que existe PARA SER BLOQUEADO.
//
// # Por qué hace falta algo tan raro
//
// Porque en Windows un puerto callado no dice nada. Está medido: con el firewall
// encendido, un puerto PERMITIDO y sin nadie escuchando contesta lo mismo que un
// puerto bloqueado, que es silencio. Es el modo sigiloso, y arruina la única
// comprobación que atraviesa la red de verdad.
//
// El canario quita esa ambigüedad por el único camino que queda: **poniendo a
// alguien detrás de la puerta a propósito**. Si se sabe con certeza que hay un
// oyente, el silencio deja de tener dos lecturas y pasa a significar una sola,
// que el firewall lo bloqueó.
//
//	el invitado marca y NO contesta   la compuerta bloquea. Prueba de verdad
//	el invitado marca y SÍ contesta   la compuerta no está bloqueando. Alarma
//
// # Por qué un puerto prueba TODOS
//
// Porque la compuerta no es una regla por puerto: es un bloqueo del adaptador
// entero, con permisos espejo para lo que el juego pidió. Un puerto que no está
// en esos permisos y que aun así queda callado demuestra que el bloqueo está
// vivo, y ese bloqueo es el mismo para todos los puertos que nadie pidió.
//
// Por eso acá no hay lista de puertos peligrosos que recorrer. Enumerar
// amenazas es una lotería: Parsec, Sunshine y RustDesk escuchan donde el usuario
// les diga. Comprobar el bloqueo general las cubre a todas, incluidas las que no
// conocemos.
//
// # El radio de explosión es exactamente lo que se mide
//
// El oyente se liga **solo a la dirección de la sala**, jamás a `0.0.0.0`. O sea
// que ese socket existe únicamente en el adaptador virtual, que es justo el
// adaptador que la compuerta bloquea:
//
//   - con la compuerta viva, ese socket es inalcanzable para todo el mundo;
//   - con la compuerta muerta, lo alcanzan los de la sala, que es lo que se
//     quería averiguar.
//
// En ningún caso abre nada en la red de casa del usuario.
//
// # Y no hace nada cuando lo tocan
//
// Por TCP acepta y cierra, sin leer un byte. Por UDP lee un número fijo y
// devuelve el mismo si coincide. No hay parser, no hay buffer que crezca, no hay
// estado. Es muchísima menos superficie que el canal de la sala, que sí parsea
// mensajes de gente de la sala corriendo como SYSTEM.
package canary

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sync"
	"time"
)

// NonceSize es el largo del número que ata la pregunta con la respuesta.
//
// Fijo, y ese detalle es de seguridad y no de estilo: por UDP se lee EXACTAMENTE
// esta cantidad y se descarta el resto, así que no hay ningún camino por el que
// un paquete de fuera decida cuánta memoria se reserva.
const NonceSize = 16

// Nonce ata la pregunta con la respuesta.
//
// Existe por UDP, que no tiene conexión: sin él, cualquier paquete suelto que
// llegara al invitado en ese momento se leería como que el canario contestó, o
// sea como una fuga que no existe. Por TCP no hace falta, porque el apretón de
// manos ya prueba que se llegó a ESE socket.
type Nonce [NonceSize]byte

// TTLMax es lo máximo que el canario puede quedar abierto.
//
// Existe como tope duro y no como sugerencia: el oyente lo abre el daemon, que
// corre como SYSTEM, y un plazo que dependa de que alguien llame a Close deja el
// socket vivo cuando quien tenía que cerrarlo se murió. Con esto, el peor caso
// es medio minuto.
const TTLMax = 30 * time.Second

// Logger es lo que el canario necesita contar. Se declara acá y no se importa
// de core para que este paquete no dependa de nada del dominio.
type Logger interface {
	Info(msg string, kv ...any)
	Warn(msg string, kv ...any)
}

// Canary es el oyente abierto.
type Canary struct {
	port  uint16
	nonce Nonce

	tcp net.Listener
	udp net.PacketConn

	closeOnce sync.Once
	timer     *time.Timer
	log       Logger

	mu      sync.Mutex
	touched bool
}

// Listen abre el canario en `at`, en un puerto libre EN LOS DOS PROTOCOLOS.
//
// # Por qué los dos a la vez, y en el mismo número
//
// Porque la compuerta no mira el protocolo: sus bloqueos llevan condición de
// adaptador y de rango, y ninguna de protocolo. Comprobar los dos con una sola
// pregunta es lo que hace que el resultado valga también para UDP, que es por
// donde habla justamente la herramienta que más preocupa.
//
// TCP y UDP tienen espacios de puertos separados, así que un número libre en uno
// puede estar tomado en el otro. Se busca uno libre en ambos, y ligar de verdad
// es la única comprobación que sirve: en Windows hay rangos que el sistema
// RESERVA y que cambian en cada arranque, así que un número que parece libre
// puede no serlo.
//
// `ttl` se recorta a [TTLMax]. En cero vale [TTLMax].
func Listen(at netip.Addr, nonce Nonce, ttl time.Duration, log Logger) (*Canary, error) {
	if !at.IsValid() {
		return nil, errors.New("canary: sin dirección de la sala no hay dónde ligar, y ligar " +
			"en todas las interfaces abriría un puerto en la red de casa del usuario")
	}
	if at.IsUnspecified() {
		// El cero es `0.0.0.0`, o sea todas las interfaces. Sería exactamente lo
		// que este paquete promete no hacer.
		return nil, fmt.Errorf("canary: se pidió ligar en %s, que es todas las interfaces", at)
	}
	if ttl <= 0 || ttl > TTLMax {
		ttl = TTLMax
	}

	tcp, udp, port, err := bindBoth(at)
	if err != nil {
		return nil, err
	}

	c := &Canary{port: port, nonce: nonce, tcp: tcp, udp: udp, log: log}
	go c.serveTCP()
	go c.serveUDP()

	// El plazo duro. Va con AfterFunc y no con un contexto porque tiene que
	// cerrar los sockets aunque nadie esté esperando: el que lo abrió puede
	// haberse muerto.
	//
	// El temporizador se guarda BAJO EL CANDADO, y no es ceremonia: puede
	// dispararse antes de que esta línea termine, y entonces su Close leería el
	// campo mientras se escribe. Lo cazó el detector de carreras con un plazo
	// corto, que es el caso realista.
	t := time.AfterFunc(ttl, func() {
		_ = c.Close()
	})
	c.mu.Lock()
	c.timer = t
	c.mu.Unlock()

	log.Info("canario abierto", "en", netip.AddrPortFrom(at, port), "plazo", ttl)
	return c, nil
}

// Port es el puerto que quedó, para poder decírselo al invitado.
func (c *Canary) Port() uint16 { return c.port }

// Touched dice si ALGUIEN llegó hasta el socket.
//
// Es la segunda mitad de la medición y no sobra, porque el invitado puede
// mentir. Lo que ve el host acá es un hecho propio: si el canario fue tocado, el
// paquete cruzó la compuerta, lo diga quien lo diga.
//
// Ojo con la asimetría, que es la parte importante: que NO haya sido tocado no
// prueba que la compuerta funcione, porque a lo mejor el invitado nunca marcó.
// Sirve para desmentir un "no contestó" falso, no para confirmarlo.
func (c *Canary) Touched() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.touched
}

// Close es IDEMPOTENTE. Lo llama el camino normal y también el plazo duro, y los
// dos pueden ganar la carrera.
func (c *Canary) Close() error {
	var err error
	c.closeOnce.Do(func() {
		// Se saca bajo candado y se para fuera: parar el temporizador desde
		// dentro de su propia función devolvería false y no haría nada, y
		// sostener el candado hasta el log haría que Touched se abrace consigo
		// mismo.
		c.mu.Lock()
		t := c.timer
		c.mu.Unlock()
		if t != nil {
			t.Stop()
		}
		err = errors.Join(c.tcp.Close(), c.udp.Close())
		c.log.Info("canario cerrado", "puerto", c.port, "lo tocaron", c.Touched())
	})
	return err
}

func (c *Canary) mark() {
	c.mu.Lock()
	c.touched = true
	c.mu.Unlock()
}

// serveTCP acepta y cierra, sin leer NI UN BYTE.
//
// Un `Read` acá esperaría a un saludo que el sondeo no manda, y de paso
// convertiría este socket en un lector de lo que le mande cualquiera. El apretón
// de manos ya terminó cuando Accept devuelve, así que la respuesta ya está dada.
func (c *Canary) serveTCP() {
	for {
		conn, err := c.tcp.Accept()
		if err != nil {
			return
		}
		c.mark()
		_ = conn.Close()
	}
}

// serveUDP devuelve el mismo número que le manden, y SOLO si coincide.
//
// El buffer es de tamaño fijo y se lee una sola vez: un datagrama más largo se
// trunca y no coincide, así que se descarta. No hay forma de que lo que llegue
// de fuera decida cuánta memoria se toca.
//
// Un datagrama que no coincide se ignora en silencio y **no cuenta como toque**.
// Contarlo convertiría cualquier paquete perdido en una alarma.
func (c *Canary) serveUDP() {
	buf := make([]byte, NonceSize)
	for {
		n, from, err := c.udp.ReadFrom(buf)
		if err != nil {
			return
		}
		if n != NonceSize || [NonceSize]byte(buf) != c.nonce {
			continue
		}
		c.mark()
		// La respuesta va a quien preguntó y a nadie más. Que el destino salga
		// del propio datagrama es lo que impide que esto sirva para rebotar
		// tráfico hacia un tercero.
		if _, err := c.udp.WriteTo(c.nonce[:], from); err != nil {
			c.log.Warn("el canario no pudo contestar", "a", from, "error", err)
		}
	}
}

// bindBoth busca un puerto libre en TCP y en UDP a la vez, en esa dirección.
//
// Se pide un efímero al sistema en TCP y se intenta el mismo número en UDP. Que
// el sistema elija evita inventar números, y reintentar cubre el caso de que ese
// número esté tomado en UDP.
func bindBoth(at netip.Addr) (net.Listener, net.PacketConn, uint16, error) {
	host := at.String()

	const intentos = 20
	// Se guarda el ÚLTIMO error de UDP para poder contarlo. Un mensaje que diga
	// "20 intentos" y no diga por qué falló cada uno no sirve para nada el día
	// que falle en la máquina de un usuario.
	var último error

	for i := 0; i < intentos; i++ {
		tcp, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
		if err != nil {
			return nil, nil, 0, fmt.Errorf("canary: no se pudo ligar TCP en %s: %w", host, err)
		}

		port := uint16(tcp.Addr().(*net.TCPAddr).Port)
		udp, err := net.ListenPacket("udp", net.JoinHostPort(host, fmt.Sprint(port)))
		if err == nil {
			return tcp, udp, port, nil
		}
		último = err
		_ = tcp.Close()
	}
	return nil, nil, 0, fmt.Errorf("canary: no salió ningún puerto libre en TCP y UDP a la vez "+
		"en %s tras %d intentos. Lo último que dijo UDP: %w", host, intentos, último)
}
