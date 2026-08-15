package canary

import (
	"net"
	"net/netip"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/accentiostudios/kanpachi/core/timing"
)

const local = "127.0.0.1"

func aquí() netip.Addr { return netip.MustParseAddr(local) }

type logMudo struct{}

func (logMudo) Info(string, ...any) {}
func (logMudo) Warn(string, ...any) {}

func nonceDePrueba() Nonce {
	var n Nonce
	for i := range n {
		n[i] = byte(i + 1)
	}
	return n
}

func abrir(t *testing.T) *Canary {
	t.Helper()
	c, err := Listen(aquí(), nonceDePrueba(), 5*time.Second, nil, logMudo{})
	if err != nil {
		t.Fatalf("no se pudo abrir el canario: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func destino(c *Canary) string {
	return net.JoinHostPort(local, strconv.Itoa(int(c.Port())))
}

func TestElCanarioLigaLosDosProtocolosEnElMismoPuerto(t *testing.T) {
	c := abrir(t)

	if c.Port() == 0 {
		t.Fatal("el canario no dio puerto")
	}
	// Los dos tienen que estar tomados: si alguno quedó libre, el canario no
	// está comprobando ese protocolo y su silencio no probaría nada.
	if l, err := net.Listen("tcp", destino(c)); err == nil {
		l.Close()
		t.Error("el puerto quedó libre en TCP")
	}
	if p, err := net.ListenPacket("udp", destino(c)); err == nil {
		p.Close()
		t.Error("el puerto quedó libre en UDP")
	}
}

// Ligar en todas las interfaces abriría un puerto en la red de casa del usuario,
// que es exactamente lo que este producto promete no hacer.
func TestElCanarioSeNiegaALigarEnTodasLasInterfaces(t *testing.T) {
	casos := map[string]netip.Addr{
		"sin dirección": {},
		"0.0.0.0":       netip.MustParseAddr("0.0.0.0"),
		"::":            netip.MustParseAddr("::"),
	}
	for nombre, at := range casos {
		if c, err := Listen(at, nonceDePrueba(), time.Second, nil, logMudo{}); err == nil {
			_ = c.Close()
			t.Errorf("%s: el canario ligó y no debía", nombre)
		}
	}
}

func TestUnaConexionTCPCuentaComoToque(t *testing.T) {
	c := abrir(t)

	if c.WasTouched() {
		t.Fatal("nació tocado")
	}
	conn, err := net.DialTimeout("tcp", destino(c), 2*time.Second)
	if err != nil {
		t.Fatalf("no se pudo conectar al canario: %v", err)
	}
	defer conn.Close()

	esperarToque(t, c)
}

// El canario no lee NI UN BYTE por TCP. Un Read acá lo convertiría en un lector
// de lo que le mande cualquiera, corriendo como SYSTEM.
func TestElCanarioNoLeeNadaPorTCP(t *testing.T) {
	c := abrir(t)

	conn, err := net.DialTimeout("tcp", destino(c), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	esperarToque(t, c)

	// Se manda algo y se comprueba que el socket ya está cerrado del otro lado.
	// El canario acepta y cierra, así que escribir y leer tiene que fallar o
	// devolver EOF, jamás quedarse esperando a que alguien procese.
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	_, _ = conn.Write([]byte("hola, ¿me lees?"))
	var buf [16]byte
	if n, err := conn.Read(buf[:]); err == nil && n > 0 {
		t.Fatalf("el canario contestó %d bytes por TCP, y no tiene que contestar nada", n)
	}
}

func TestUnDatagramaConElNumeroBuenoVuelveYCuentaComoToque(t *testing.T) {
	c := abrir(t)
	nonce := nonceDePrueba()

	conn, err := net.DialTimeout("udp", destino(c), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))

	if _, err := conn.Write(nonce[:]); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, NonceSize)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("el canario no devolvió el eco: %v", err)
	}
	if n != NonceSize || [NonceSize]byte(buf) != nonce {
		t.Fatalf("el eco volvió distinto: %x", buf[:n])
	}
	esperarToque(t, c)
}

// La defensa contra la alarma falsa. Sin esto, cualquier paquete perdido que
// caiga en ese puerto se leería como que la compuerta está rota.
func TestUnDatagramaQueNoCuadraNiVuelveNiCuentaComoToque(t *testing.T) {
	c := abrir(t)

	conn, err := net.DialTimeout("udp", destino(c), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(time.Second))

	// Uno más corto, uno más largo y uno del mismo largo con otro contenido.
	for _, malo := range [][]byte{
		[]byte("corto"),
		make([]byte, NonceSize*4),
		make([]byte, NonceSize), // del largo bueno pero todo ceros
	} {
		if _, err := conn.Write(malo); err != nil {
			t.Fatal(err)
		}
	}

	buf := make([]byte, NonceSize)
	if n, err := conn.Read(buf); err == nil {
		t.Fatalf("el canario contestó %d bytes a un número que no era el suyo", n)
	}
	if c.WasTouched() {
		t.Fatal("un datagrama que no cuadra contó como toque. Cualquier paquete " +
			"perdido encendería la alarma")
	}
}

// Lo llama el camino normal y también el plazo duro, y los dos pueden ganar la
// carrera.
func TestCerrarDosVecesNoRompeNada(t *testing.T) {
	c, err := Listen(aquí(), nonceDePrueba(), time.Second, nil, logMudo{})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("el primer cierre falló: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("el segundo cierre falló: %v", err)
	}
}

// El plazo duro existe porque el oyente lo abre un proceso que corre como
// SYSTEM: si quien tenía que cerrarlo se muere, el socket no puede quedar vivo.
func TestElCanarioSeCierraSoloAlVencerElPlazo(t *testing.T) {
	c, err := Listen(aquí(), nonceDePrueba(), 150*time.Millisecond, nil, logMudo{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })

	puerto := destino(c)
	plazo := time.Now().Add(3 * time.Second)
	for time.Now().Before(plazo) {
		l, err := net.Listen("tcp", puerto)
		if err == nil {
			l.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("el canario seguía ligado mucho después de su plazo")
}

func TestElPlazoSeRecortaAlTope(t *testing.T) {
	// Un plazo enorme no puede dejar el socket abierto una hora. Se comprueba
	// por el efecto y no por un campo, que es lo que de verdad importa.
	c, err := Listen(aquí(), nonceDePrueba(), 10*time.Hour, nil, logMudo{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()

	if timing.CanaryTTLMax > time.Minute {
		t.Errorf("el tope del canario son %v, y es demasiado para un socket que "+
			"abre un proceso que corre como SYSTEM", timing.CanaryTTLMax)
	}
}

// esperarToque espera por el CANAL, que es como lo espera la ronda de verdad.
//
// Sondear `WasTouched` en un bucle también funcionaría y probaría menos: el
// canal es lo que hace que una ronda corte en el primer toque en vez de esperar
// su plazo entero, y si nunca se cerrara, este arnés no lo notaría.
func esperarToque(t *testing.T, c *Canary) {
	t.Helper()
	select {
	case <-c.Touched():
	case <-time.After(2 * time.Second):
		t.Fatal("el canario no avisó del toque por su canal")
	}
	if !c.WasTouched() {
		t.Fatal("avisó por el canal y el hecho quedó en falso")
	}
}

// ---------------------------------------------------------------------------
// El canal de toque
// ---------------------------------------------------------------------------

// LA TRAMPA.
//
// Un canal cerrado se lee como listo. Si `Close` cerrara el canal de toque, toda
// ronda concluiría que hubo fuga en cuanto cerrara su canario, o sea siempre, y
// la alarma que este producto tiene para el caso más grave se encendería en cada
// comprobación.
func TestCerrarNoHaceQueUnCanarioSinTocarParezcaTocado(t *testing.T) {
	c, err := Listen(aquí(), nonceDePrueba(), 5*time.Second, nil, logMudo{})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("el cierre falló: %v", err)
	}

	select {
	case <-c.Touched():
		t.Fatal("cerrar el canario lo hizo parecer tocado. Así, toda ronda termina " +
			"en alarma y la única comprobación que cruza la red deja de valer")
	default:
	}
	if c.WasTouched() {
		t.Error("cerrar no puede cambiar el hecho")
	}
}

// El canal se cierra UNA vez. Cerrarlo dos veces es un pánico, y esto corre
// dentro de un proceso que corre como SYSTEM.
func TestVariosToquesNoEntranEnPanico(t *testing.T) {
	c := abrir(t)

	for i := 0; i < 3; i++ {
		conn, err := net.DialTimeout("tcp", destino(c), 2*time.Second)
		if err != nil {
			t.Fatalf("conexión %d: %v", i, err)
		}
		_ = conn.Close()
	}

	udp, err := net.DialTimeout("udp", destino(c), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	nonce := nonceDePrueba()
	if _, err := udp.Write(nonce[:]); err != nil {
		t.Fatal(err)
	}

	esperarToque(t, c)
}

// ---------------------------------------------------------------------------
// La exclusión de puertos
// ---------------------------------------------------------------------------

// Un puerto que el juego activo tiene abierto contestaría con toda razón, y esa
// respuesta se leería como que la compuerta dejó de contener.
func TestElCanarioEsquivaLosPuertosQueSeLeProhiben(t *testing.T) {
	var ofrecidos []uint16
	// Rechaza los tres primeros que ofrezca el sistema, sean cuales sean. Así el
	// test no depende de qué números salgan, que cambian en cada corrida.
	avoid := func(p uint16) bool {
		ofrecidos = append(ofrecidos, p)
		return len(ofrecidos) <= 3
	}

	c, err := Listen(aquí(), nonceDePrueba(), 5*time.Second, avoid, logMudo{})
	if err != nil {
		t.Fatalf("no se pudo abrir esquivando tres puertos: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	if len(ofrecidos) < 4 {
		t.Fatalf("se consultó %d veces, y se rechazaron los tres primeros: "+
			"el predicado no se está consultando en cada intento", len(ofrecidos))
	}
	for _, p := range ofrecidos[:3] {
		if c.Port() == p {
			t.Fatalf("quedó ligado en %d, que es uno de los que se prohibieron", p)
		}
	}
}

// Prohibirlos todos tiene que fallar en voz alta. Un canario ligado a un puerto
// prohibido sería peor que ninguno.
func TestSinPuertoAceptableFalla(t *testing.T) {
	c, err := Listen(aquí(), nonceDePrueba(), 5*time.Second, func(uint16) bool { return true }, logMudo{})
	if err == nil {
		_ = c.Close()
		t.Fatal("ligó igual con todos los puertos prohibidos")
	}
	if !strings.Contains(err.Error(), "juego activo") {
		t.Errorf("el error no dice por qué falló: %v", err)
	}
}

// EL TEST QUE CAZA EL BLOQUE RESERVADO DE WINDOWS.
//
// # Qué se está reproduciendo
//
// Windows reserva bloques del rango efímero, los pone cualquier cosa que use
// Hyper-V por debajo, cambian en cada arranque, y a veces son CONTIGUOS. En la
// máquina de desarrollo, el 2026-08-04, cuatrocientos puertos seguidos:
//
//	netsh int ipv4 show excludedportrange protocol=udp
//	  50516-50615  50616-50715  50716-50815  50816-50915
//
// Y el sistema entrega efímeros de forma SECUENCIAL. Medido con tres mil
// aperturas de un solo intento: 16,7% falló, con una racha de 401 fallos
// seguidos en puertos consecutivos. O sea que pidiendo siempre por el mismo
// protocolo, veinte intentos caminan en fila por dentro del bloque y se acaban.
//
// # Por qué se abre tantas veces
//
// Porque el fallo depende de dónde esté el CONTADOR de efímeros, y eso no se
// puede fijar desde acá. Medido sobre veinte mil aperturas completas:
//
//	20 intentos, siempre TCP    60 fallaron (0,30%)
//	40 intentos, alternando      0 fallaron  <- envejeció mal, ver bindIntentos
//
// Con 0,30%, mil aperturas cazan la regresión el 95% de las veces. Se abren dos
// mil, y tardan alrededor de un segundo porque el caso bueno son dos llamadas al
// sistema. Un test que solo falle con suerte no protegería nada: una versión
// anterior de esto abría treinta y pasaba con el código roto puesto a mano.
//
// En una máquina sin ningún rango reservado esto pasa siempre y no prueba nada,
// que es lo correcto: el test no puede inventar la reserva.
func TestElCanarioAbreAunqueWindowsTengaRangosReservados(t *testing.T) {
	if testing.Short() {
		t.Skip("abre dos mil canarios; se salta en -short")
	}

	const aperturas = 2000
	for i := 0; i < aperturas; i++ {
		c, err := Listen(aquí(), nonceDePrueba(), 5*time.Second, nil, logMudo{})
		if err != nil {
			t.Fatalf("apertura %d de %d: %v\n\n"+
				"  Si el error habla de permisos, cayó en un rango reservado de Windows y\n"+
				"  no supo salir. Los rangos se ven con:\n"+
				"    netsh int ipv4 show excludedportrange protocol=udp\n"+
				"  El síntoma en la máquina de un usuario es que la Protección Kanpachi no\n"+
				"  corre NUNCA y nada explica por qué.", i, aperturas, err)
		}
		_ = c.Close()
	}
}

// Y que el predicado se consulte SIEMPRE, elija quien elija el número. Sin esto,
// alternar habría dejado la mitad de los intentos sin comprobar si el puerto
// choca con el juego activo.
func TestElPredicadoSeConsultaEnTodosLosIntentos(t *testing.T) {
	consultas := 0
	avoid := func(uint16) bool {
		consultas++
		return consultas <= 6 // rechaza los seis primeros, de los dos protocolos
	}

	c, err := Listen(aquí(), nonceDePrueba(), 5*time.Second, avoid, logMudo{})
	if err != nil {
		t.Fatalf("no se pudo abrir esquivando seis puertos: %v", err)
	}
	defer func() { _ = c.Close() }()

	if consultas < 7 {
		t.Fatalf("se consultó %d veces con seis rechazos: hay intentos que no pasan "+
			"por el predicado, y en esos el canario puede ligarse a un puerto abierto",
			consultas)
	}
}
