package opener

import (
	"net"
	"net/netip"
	"strconv"
	"testing"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/core/port"
)

// La afirmación que hace que este paquete valga: satisface el puerto. Si deja de
// hacerlo, esto no compila, que es antes y más barato que descubrirlo cableando.
var _ port.CanaryPort = Opener{}

type logMudo struct{}

func (logMudo) Info(string, ...any) {}
func (logMudo) Warn(string, ...any) {}

func nonceDePrueba() domain.CanaryNonce {
	var n domain.CanaryNonce
	for i := range n {
		n[i] = byte(i + 1)
	}
	return n
}

func TestElCanarioAbiertoSatisfaceElPuerto(t *testing.T) {
	o := New(logMudo{})

	c, err := o.Listen(netip.MustParseAddr("127.0.0.1"), nonceDePrueba(), 5*time.Second, nil)
	if err != nil {
		t.Fatalf("no se pudo abrir: %v", err)
	}
	defer func() { _ = c.Close() }()

	if c.Port() == 0 {
		t.Fatal("no dio puerto")
	}
	if c.WasTouched() {
		t.Fatal("nació tocado")
	}
	select {
	case <-c.Touched():
		t.Fatal("el canal de toque nació cerrado, así que toda ronda concluiría fuga")
	default:
	}
}

// El número tiene que llegar entero al otro lado de la traducción. Un nonce mal
// copiado convierte el eco de UDP en un datagrama que no cuadra, y esa mitad de
// la medición se perdería en silencio.
func TestElNumeroCruzaLaTraduccionIntacto(t *testing.T) {
	o := New(logMudo{})
	nonce := nonceDePrueba()

	c, err := o.Listen(netip.MustParseAddr("127.0.0.1"), nonce, 5*time.Second, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()

	conn, err := net.DialTimeout("udp", net.JoinHostPort("127.0.0.1", strconv.Itoa(int(c.Port()))), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))

	if _, err := conn.Write(nonce[:]); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, domain.CanaryNonceSize)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("no volvió el eco, así que el número no cruzó igual: %v", err)
	}
	if n != domain.CanaryNonceSize || domain.CanaryNonce(buf) != nonce {
		t.Fatalf("el eco volvió distinto: %x", buf[:n])
	}
}

// Un fallo tiene que devolver el nil de la INTERFAZ. Un puntero nulo envuelto en
// una interfaz no es igual a nil, y quien llame haría Close sobre algo que no
// existe.
func TestUnFalloDevuelveNilDeVerdad(t *testing.T) {
	o := New(logMudo{})

	c, err := o.Listen(netip.Addr{}, nonceDePrueba(), time.Second, nil)
	if err == nil {
		_ = c.Close()
		t.Fatal("ligó sin dirección")
	}
	if c != nil {
		t.Fatal("devolvió una interfaz no nula con un puntero nulo adentro: " +
			"comprobar `if c != nil` mentiría y el Close siguiente entraría en pánico")
	}
}
