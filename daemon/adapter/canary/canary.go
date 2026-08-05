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

	// touch se CIERRA en el primer toque y jamás se escribe.
	//
	// Cerrar es nivel y no flanco: quien llegue tarde a seleccionar lo ve igual,
	// así que la ronda no puede perderse un toque por no estar mirando todavía.
	// Un envío se perdería en ese caso, y un segundo toque dejaría bloqueada una
	// goroutine servidora dentro de un proceso que corre como SYSTEM.
	touch     chan struct{}
	touchOnce sync.Once

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
// # El puerto que NO sirve
//
// `avoid` dice qué números hay que descartar, y en nil no descarta ninguno.
// Existe porque un efímero que caiga dentro de un rango que el juego activo
// tiene abierto sería alcanzable A PROPÓSITO, y su respuesta se leería como que
// la compuerta dejó de contener. Ver `domain.RuleSet.Allows`.
//
// `ttl` se recorta a [TTLMax]. En cero vale [TTLMax].
func Listen(at netip.Addr, nonce Nonce, ttl time.Duration, avoid func(uint16) bool, log Logger) (*Canary, error) {
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

	tcp, udp, port, err := bindBoth(at, avoid)
	if err != nil {
		return nil, err
	}

	c := &Canary{
		port:  port,
		nonce: nonce,
		tcp:   tcp,
		udp:   udp,
		log:   log,
		touch: make(chan struct{}),
	}
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

// Touched es el AVISO de que alguien llegó hasta el socket, y se lee igual que
// [context.Context.Done]: se cierra una vez y queda cerrado.
//
// Existe para que una ronda pueda cortar en el primer toque en vez de esperar su
// plazo entero. Con la compuerta rota, cada segundo de más es un socket
// alcanzable de verdad por la sala.
//
// **Cerrar el canario NO cierra este canal**, y esa omisión es de seguridad: un
// canal cerrado se lee como listo, así que un [Canary.Close] que lo cerrara haría
// que todo canario intacto concluyera que hubo fuga.
func (c *Canary) Touched() <-chan struct{} { return c.touch }

// WasTouched es el HECHO, y se lee igual que [context.Context.Err]: vale también
// después de cerrar, que es justo cuando el host lo mira.
//
// Es la segunda mitad de la medición y no sobra, porque el invitado puede
// mentir. Lo que ve el host acá es un hecho propio: si el canario fue tocado, el
// paquete cruzó la compuerta, lo diga quien lo diga.
//
// Ojo con la asimetría, que es la parte importante: que NO haya sido tocado no
// prueba que la compuerta funcione, porque a lo mejor el invitado nunca marcó.
// Sirve para desmentir un "no contestó" falso, no para confirmarlo.
func (c *Canary) WasTouched() bool {
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
		// Los dos sockets y NADA MÁS. `c.touch` se queda como está, y esa
		// omisión es la que evita el peor fallo posible de este paquete: ver
		// [Canary.Touched].
		err = errors.Join(c.tcp.Close(), c.udp.Close())
		c.log.Info("canario cerrado", "puerto", c.port, "lo tocaron", c.WasTouched())
	})
	return err
}

func (c *Canary) mark() {
	c.mu.Lock()
	c.touched = true
	c.mu.Unlock()
	// Fuera del candado y con un Once, así dos toques a la vez no cierran dos
	// veces el mismo canal, que sería un pánico dentro de un proceso que corre
	// como SYSTEM.
	c.touchOnce.Do(func() { close(c.touch) })
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

// bindIntentos son los números que se prueban antes de rendirse.
//
// El número sale de una medición y no del aire. Ver [bindBoth].
//
// # Por qué 500, después de que 40 fallara
//
// La primera versión dimensionó esto midiendo SOLO los rangos reservados de
// UDP, que en la máquina de desarrollo daban un bloque contiguo de 400 puertos.
// Con 40 intentos el test de 2000 aperturas pasó, y al día siguiente falló en la
// apertura 1880, en `127.0.0.1:58586`.
//
// Ese puerto no está en ningún rango de UDP. Está en uno de TCP, que **no se
// habían mirado**:
//
//	TCP  5357  50000-50059  58540-58639  62952-63051  63052-63151
//	     63897-63996  64211-64310  64311-64410  64411-64510
//
// Los tres últimos son contiguos: **300 puertos seguidos**. Y hace falta un
// número libre en los DOS protocolos a la vez, así que los rangos de TCP cuentan
// igual que los de UDP aunque el número lo elija UDP.
//
// Windows entrega los efímeros de forma SECUENCIAL en este equipo, medido, así
// que N intentos cubren N puertos seguidos y nada más: 40 caben enteros dentro
// de un bloque de 300. 500 pasa el peor bloque medido con margen, y no cuesta
// nada porque el caso normal acierta al primer intento.
//
// La lección, escrita para que no se repita: **los rangos reservados hay que
// mirarlos en los dos protocolos.** Son listas distintas y cambian solas.
const bindIntentos = 500

// bindBoth busca un puerto libre en TCP y en UDP a la vez, en esa dirección, y
// que además `avoid` no rechace.
//
// # Por qué alterna quién elige el número, MEDIDO
//
// Windows RESERVA bloques del rango efímero. Los pone cualquier cosa que use
// Hyper-V por debajo (WSL, Docker Desktop) y cambian en cada arranque. En la
// máquina de desarrollo, el 2026-08-04:
//
//	netsh int ipv4 show excludedportrange protocol=udp
//	  50000-50059  50085-50184  50278-50377  50516-50615
//	  50616-50715  50716-50815  50816-50915  58804-58903
//
// Los cuatro del medio son CONTIGUOS: 50516 a 50915 son cuatrocientos puertos
// seguidos. Y el sistema entrega efímeros de forma SECUENCIAL, no aleatoria, así
// que pidiendo siempre por el mismo protocolo los intentos caminan en fila por
// dentro del bloque. Medido con tres mil aperturas de un solo intento: 16,7%
// falló, con una racha de 401 fallos SEGUIDOS, en puertos consecutivos.
//
// Esa es la causa de que una corrida de la suite fallara con
// `listen udp 127.0.0.1:58900: bind: WSAEACCES`, con el 58900 dentro de
// 58804-58903.
//
// # Cuánto cambia, con números
//
// Veinte mil aperturas completas de cada estrategia, misma máquina:
//
//	20 intentos, siempre TCP    60 fallaron (0,30%), peor caso los 20 agotados
//	40 intentos, alternando      0 fallaron,         peor caso 10 intentos
//
// Esa tabla se midió mirando solo los rangos de UDP, y por eso el 0 de la
// segunda fila envejeció mal: al día siguiente 40 no alcanzaba. Ver
// [bindIntentos].
//
// Un 0,30% con una ronda cada sesenta segundos son unas cuatro fallas por día y
// por máquina. No era teórico.
//
// Alternar funciona porque las listas de exclusión de TCP y de UDP son
// DISTINTAS: cuando le toca elegir a UDP, el sistema entrega un número que para
// UDP ya está libre, y el bloque deja de existir para ese intento. Por eso el
// peor caso baja a diez y no a veinte.
//
// El síntoma que esto quita es el peor posible: el canario no abre, la ronda
// devuelve ciego, y la Protección Kanpachi no corre NUNCA sin que nada explique
// por qué. Y le tocaría justo al público de este producto, que es gente con
// Windows y con WSL o Docker instalados.
func bindBoth(at netip.Addr, avoid func(uint16) bool) (net.Listener, net.PacketConn, uint16, error) {
	host := at.String()

	// Se guarda el ÚLTIMO error y cuántos se descartaron por chocar, para poder
	// contarlo. Un mensaje que diga cuántos intentos y no diga por qué falló
	// cada uno no sirve para nada el día que falle en la máquina de un usuario, y
	// las dos causas piden arreglos distintos.
	var último error
	descartados := 0

	for i := 0; i < bindIntentos; i++ {
		tcp, udp, port, chocó, err := unIntento(host, avoid, i%2 == 0)
		switch {
		case chocó:
			descartados++
		case err != nil:
			último = err
		default:
			return tcp, udp, port, nil
		}
	}

	if último == nil {
		return nil, nil, 0, fmt.Errorf("canary: los %d puertos que ofreció el sistema en %s "+
			"caían todos en un rango que el juego activo tiene abierto", bindIntentos, host)
	}
	return nil, nil, 0, fmt.Errorf("canary: no salió ningún puerto libre en TCP y UDP a la vez "+
		"en %s tras %d intentos, con %d descartados por chocar con el juego activo. "+
		"Si el error de abajo habla de permisos, son los rangos que Windows reserva: "+
		"`netsh int ipv4 show excludedportrange protocol=udp` los enseña. Lo último: %w",
		host, bindIntentos, descartados, último)
}

// unIntento consigue el mismo número en los dos protocolos, dejando ELEGIR al
// que se le diga.
//
// Devuelve `chocó` aparte del error porque son cosas distintas: chocar con un
// puerto del juego activo es lo normal y se reintenta sin más, y un error de
// ligar es información que hay que conservar para el mensaje final.
func unIntento(host string, avoid func(uint16) bool, desdeTCP bool) (
	net.Listener, net.PacketConn, uint16, bool, error) {

	var (
		tcp  net.Listener
		udp  net.PacketConn
		port uint16
		err  error
	)

	if desdeTCP {
		tcp, err = net.Listen("tcp", net.JoinHostPort(host, "0"))
		if err != nil {
			return nil, nil, 0, false, err
		}
		port = uint16(tcp.Addr().(*net.TCPAddr).Port)
	} else {
		udp, err = net.ListenPacket("udp", net.JoinHostPort(host, "0"))
		if err != nil {
			return nil, nil, 0, false, err
		}
		port = uint16(udp.LocalAddr().(*net.UDPAddr).Port)
	}

	cerrar := func() {
		if tcp != nil {
			_ = tcp.Close()
		}
		if udp != nil {
			_ = udp.Close()
		}
	}

	// El predicado se consulta con el número ya en la mano y ANTES de ligar el
	// segundo. Un número que el juego activo tiene abierto contestaría con toda
	// razón, y esa respuesta se leería como que la compuerta dejó de contener.
	if avoid != nil && avoid(port) {
		cerrar()
		return nil, nil, 0, true, nil
	}

	if desdeTCP {
		udp, err = net.ListenPacket("udp", net.JoinHostPort(host, fmt.Sprint(port)))
	} else {
		tcp, err = net.Listen("tcp", net.JoinHostPort(host, fmt.Sprint(port)))
	}
	if err != nil {
		cerrar()
		return nil, nil, 0, false, err
	}
	return tcp, udp, port, false, nil
}
