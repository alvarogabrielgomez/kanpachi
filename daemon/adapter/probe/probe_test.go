package probe

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/core/timing"
)

func ctx() context.Context { return context.Background() }

func TestUnPuertoConOyenteContesta(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()

	at := netip.MustParseAddrPort(l.Addr().String())
	out, rtt := New().Probe(ctx(), at)

	if out != domain.ProbeAnswered {
		t.Fatalf("resultado = %v, se esperaba ProbeAnswered", out)
	}
	if rtt <= 0 {
		t.Error("el tiempo de respuesta llegó en cero")
	}
}

// Un puerto cerrado en la propia máquina rebota con RST, en los dos sistemas.
//
// Por loopback y no por un adaptador de red, que es lo que hace que este test
// mida algo: el modo sigiloso del Firewall de Windows, que es lo que convierte
// un rebote en silencio, actúa sobre las interfaces de red y no sobre 127.0.0.1.
//
// Envenenar la rama del 10061 de [refused] rompe este test EN WINDOWS, y eso es
// la prueba de que esa constante escrita a mano hace falta: el rechazo real
// llega con el número de Winsock y no con el `syscall.ECONNREFUSED` de Go.
func TestUnPuertoSinOyenteNoContesta(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	at := netip.MustParseAddrPort(l.Addr().String())
	// Se cierra para quedarse con un puerto que se sabe libre.
	l.Close()

	out, _ := New().Probe(ctx(), at)
	if out != domain.ProbeRefused && out != domain.ProbeSilent {
		t.Fatalf("resultado = %v, se esperaba rebote o silencio", out)
	}
	if out == domain.ProbeAnswered {
		t.Fatal("contestó un puerto sin oyente")
	}
}

// El sondeo no manda ni un byte. Si mandara algo, sería un cliente de cualquier
// protocolo que haya del otro lado, y esto es un botón de diagnóstico.
func TestElSondeoNoMandaNiUnByte(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	leídos := make(chan int, 1)
	go func() {
		c, err := l.Accept()
		if err != nil {
			leídos <- -1
			return
		}
		defer c.Close()
		_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
		var buf [64]byte
		n, _ := c.Read(buf[:])
		leídos <- n
	}()

	at := netip.MustParseAddrPort(l.Addr().String())
	if out, _ := New().Probe(ctx(), at); out != domain.ProbeAnswered {
		t.Fatalf("resultado = %v", out)
	}
	if n := <-leídos; n > 0 {
		t.Fatalf("el sondeo mandó %d bytes", n)
	}
}

// Los cuatro caminos de la clasificación, sin red.
//
// Van así y no con direcciones inalcanzables de verdad porque un test que
// dependa de cómo enruta la máquina de CI mide la máquina y no el código.
func TestLaClasificacionDeCadaError(t *testing.T) {
	casos := []struct {
		nombre string
		err    error
		quiere domain.ProbeOutcome
	}{
		{"rechazado en unix", opErr(syscall.ECONNREFUSED), domain.ProbeRefused},
		{"rechazado en windows", opErr(wsaeconnrefused), domain.ProbeRefused},
		{"plazo vencido", context.DeadlineExceeded, domain.ProbeSilent},
		{"tiempo agotado de red", opErr(errTiempo{}), domain.ProbeSilent},
		{"sin ruta", opErr(syscall.EHOSTUNREACH), domain.ProbeFailed},
		{"red inalcanzable", opErr(syscall.ENETUNREACH), domain.ProbeFailed},
		{"cualquier otra cosa", errors.New("vaya"), domain.ProbeFailed},
	}
	for _, c := range casos {
		if got := classify(ctx(), c.err); got != c.quiere {
			t.Errorf("%s: resultado = %v, se esperaba %v", c.nombre, got, c.quiere)
		}
	}
}

// El WSAECONNREFUSED de Winsock es el caso que un errors.Is contra
// syscall.ECONNREFUSED NO caza en Windows, y es el motivo de que la constante
// esté escrita a mano en el fichero portable.
func TestElRechazoDeWindowsNoSeEscapa(t *testing.T) {
	err := opErr(wsaeconnrefused)
	if errors.Is(err, syscall.ECONNREFUSED) && wsaeconnrefused != syscall.ECONNREFUSED {
		t.Skip("en este sistema los dos números coinciden, no hay nada que distinguir")
	}
	if !refused(err) {
		t.Fatal("el rechazo de Winsock no se reconoció. En Windows todo puerto " +
			"rebotado se leería como silencio, o sea como cerrado")
	}
}

// Que quien llama se rinda NO es una medición del otro extremo.
func TestElContextoCanceladoNoEsSilencio(t *testing.T) {
	c, cancel := context.WithCancel(ctx())
	cancel()

	if got := classify(c, context.Canceled); got != domain.ProbeFailed {
		t.Fatalf("resultado = %v, se esperaba ProbeFailed", got)
	}
	// Y ni siquiera un rechazo del otro lado cambia eso: con el contexto de
	// quien llama muerto, lo que se sabe del otro extremo es nada.
	if got := classify(c, opErr(syscall.ECONNREFUSED)); got != domain.ProbeFailed {
		t.Fatalf("con el contexto cancelado, resultado = %v", got)
	}
}

func TestElPlazoPorDefectoEsElDelDominio(t *testing.T) {
	if got := New().deadline; got != timing.ProbeDeadline {
		t.Fatalf("plazo = %v, se esperaba %v", got, timing.ProbeDeadline)
	}
}

// opErr envuelve como lo hace net: un *net.OpError con un *os.SyscallError
// dentro. Clasificar el error pelado sería más fácil y no probaría nada, porque
// lo que llega de un dial de verdad viene con las dos capas encima.
func opErr(err error) error {
	return &net.OpError{
		Op:   "dial",
		Net:  "tcp",
		Addr: &net.TCPAddr{IP: net.IPv4(100, 64, 1, 1), Port: 445},
		Err:  &os.SyscallError{Syscall: "connectex", Err: err},
	}
}

// errTiempo es un error de red que dice que se agotó el tiempo, que es la otra
// forma en que aparece el silencio.
type errTiempo struct{}

func (errTiempo) Error() string   { return "i/o timeout" }
func (errTiempo) Timeout() bool   { return true }
func (errTiempo) Temporary() bool { return true }

var _ net.Error = errTiempo{}
