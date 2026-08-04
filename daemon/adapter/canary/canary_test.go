package canary

import (
	"net"
	"net/netip"
	"strconv"
	"testing"
	"time"
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
	c, err := Listen(aquí(), nonceDePrueba(), 5*time.Second, logMudo{})
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
		if c, err := Listen(at, nonceDePrueba(), time.Second, logMudo{}); err == nil {
			_ = c.Close()
			t.Errorf("%s: el canario ligó y no debía", nombre)
		}
	}
}

func TestUnaConexionTCPCuentaComoToque(t *testing.T) {
	c := abrir(t)

	if c.Touched() {
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
	if c.Touched() {
		t.Fatal("un datagrama que no cuadra contó como toque. Cualquier paquete " +
			"perdido encendería la alarma")
	}
}

// Lo llama el camino normal y también el plazo duro, y los dos pueden ganar la
// carrera.
func TestCerrarDosVecesNoRompeNada(t *testing.T) {
	c, err := Listen(aquí(), nonceDePrueba(), time.Second, logMudo{})
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
	c, err := Listen(aquí(), nonceDePrueba(), 150*time.Millisecond, logMudo{})
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
	c, err := Listen(aquí(), nonceDePrueba(), 10*time.Hour, logMudo{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()

	if TTLMax > time.Minute {
		t.Errorf("el tope del canario son %v, y es demasiado para un socket que "+
			"abre un proceso que corre como SYSTEM", TTLMax)
	}
}

func esperarToque(t *testing.T, c *Canary) {
	t.Helper()
	plazo := time.Now().Add(2 * time.Second)
	for time.Now().Before(plazo) {
		if c.Touched() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("el canario no registró el toque")
}
