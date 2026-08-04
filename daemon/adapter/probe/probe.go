// Package probe marca un puerto TCP de otra máquina y dice qué contestó.
//
// Es el adaptador más chico del proyecto y el único que sale a la red por su
// cuenta. Todo lo que hace es abrir una conexión y cerrarla: no manda un byte,
// no lee ninguno, y no sabe qué significa lo que midió. Eso último vive en
// [domain.ProbeReport.Verdict], que se testea sin red.
//
// # Por qué no tiene fichero _windows.go
//
// Porque no hay nada específico de Windows que hacer: `net.Dialer` es
// suficiente. Lo único que cambia entre sistemas es el número del error de
// conexión rechazada, y eso se resuelve con una comprobación que se puede
// probar EN LINUX, que es donde corre CI. Un fichero por sistema habría dejado
// el camino de Windows sin ningún test.
package probe

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"syscall"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// wsaeconnrefused es el WSAECONNREFUSED de Winsock.
//
// Está a mano y con su número porque en Windows `syscall.ECONNREFUSED` NO es
// este valor: es una constante inventada del bloque APPLICATION_ERROR, y el
// error que devuelve un dial rechazado de verdad trae el 10061 de Winsock. Un
// `errors.Is(err, syscall.ECONNREFUSED)` a secas, que es lo que uno escribe sin
// pensar, no casa en Windows.
//
// Que esté en el fichero portable en vez de en uno con etiqueta de compilación
// es a propósito: así el camino de Windows tiene test y corre en Linux.
const wsaeconnrefused = syscall.Errno(10061)

// Prober es la implementación de [port.Prober].
type Prober struct {
	// deadline es cuánto se espera a cada puerto. Se puede fijar para los
	// tests; en cero vale [domain.ProbeDeadline].
	deadline time.Duration
}

// New construye la sonda con el plazo del dominio.
func New() *Prober { return &Prober{deadline: domain.ProbeDeadline} }

// Probe marca y clasifica.
//
// El cierre va con defer y sin leer ni escribir nada. Un `conn.Read` acá
// esperaría a un saludo que no todos los protocolos mandan, y convertiría un
// puerto abierto en un silencio.
func (p *Prober) Probe(ctx context.Context, at netip.AddrPort) (domain.ProbeOutcome, time.Duration) {
	deadline := p.deadline
	if deadline <= 0 {
		deadline = domain.ProbeDeadline
	}

	inner, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()

	var d net.Dialer
	start := time.Now()
	conn, err := d.DialContext(inner, "tcp", at.String())
	elapsed := time.Since(start)

	if err == nil {
		// Se cierra en cuanto se sabe. La respuesta ya está dada por el apretón
		// de manos y quedarse conectado sería hablar con un programa ajeno.
		_ = conn.Close()
		return domain.ProbeAnswered, elapsed
	}
	return classify(ctx, err), elapsed
}

// classify traduce el error del dial a lo que significa para el sondeo.
//
// La distinción que importa es entre "el otro extremo calló" y "esta máquina no
// pudo ni preguntar", porque las dos dicen lo contrario la una de la otra: el
// silencio es lo que hace una máquina blindada, y el fallo local no mide nada
// del otro lado.
func classify(parent context.Context, err error) domain.ProbeOutcome {
	// El contexto de QUIEN LLAMA vencido gana sobre todo lo demás: es esta
	// máquina la que se rindió, no la otra la que calló.
	if parent.Err() != nil {
		return domain.ProbeFailed
	}
	if refused(err) {
		return domain.ProbeRefused
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return domain.ProbeSilent
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return domain.ProbeSilent
	}
	// Sin ruta, red inalcanzable y demás son fallos de ESTA máquina para
	// mandar el paquete. Leerlos como "cerrado" pintaría de verde un adaptador
	// caído, que es el caso en el que menos se sabe.
	return domain.ProbeFailed
}

// refused dice si el otro extremo rebotó el paquete con un RST.
//
// En Windows con el firewall encendido esto casi no pasa, y está MEDIDO: el
// modo sigiloso calla en vez de rebotar, incluso con una regla que permite el
// puerto y sin nadie escuchando. Se distingue igual porque cuando ocurre
// significa que el paquete LLEGÓ, que es justo lo que el veredicto mide.
func refused(err error) bool {
	if errors.Is(err, syscall.ECONNREFUSED) {
		return true
	}
	var errno syscall.Errno
	return errors.As(err, &errno) && errno == wsaeconnrefused
}
