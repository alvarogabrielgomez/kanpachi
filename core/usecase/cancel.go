package usecase

import (
	"context"
	"sync"
	"time"
)

// Cancelar una operación larga, que son dos: crear una sala y entrar a una.
//
// # Por qué hace falta
//
// Porque las dos tardan decenas de segundos con todo funcionando, y hay casos
// en los que quien está mirando ya sabe que no vale la pena esperar: pegó el
// código equivocado, el host todavía no abrió la sala, o se arrepintió. Sin
// esto la única salida es esperar al final o matar el proceso, y matar el
// proceso a mitad de una creación es justo el momento en el que hay una red
// virtual arriba y reglas de firewall escritas.
//
// # Qué garantiza, y qué no
//
// Garantiza que **lo que se alcanzó a abrir se cierra**. Cancelar no aborta la
// operación en el acto: cancela su contexto, y quien lo estuviera usando
// devuelve error en la primera llamada que lo mire. A partir de ahí corre el
// mismo camino de limpieza que un fallo cualquiera, que es el `teardown` que ya
// existía y ya estaba probado. Ver [Session.cleanupContext] para el detalle que
// hace que esa limpieza no se cancele a sí misma.
//
// No garantiza que sea instantáneo. Un adaptador metido en una llamada de
// Windows que no acepta contexto termina esa llamada antes de mirar.

// longOp es la operación larga en curso, si la hay.
//
// El candado es SUYO y no el de la sesión, y esa es toda su razón de existir:
// `CreateRoom` tiene tomado el de la sesión durante todo el minuto que dura, o
// sea justo el rato en el que alguien pulsa Cancelar. Con el candado compartido,
// cancelar esperaría a que terminara lo que viene a cortar.
type longOp struct {
	mu     sync.Mutex
	cancel context.CancelFunc
}

// arm deja anotado cómo cortar la operación que empieza.
//
// Reemplaza lo que hubiera. No puede haber dos: las dos operaciones largas
// toman el candado de la sesión, así que la segunda espera a la primera.
func (o *longOp) arm(cancel context.CancelFunc) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.cancel = cancel
}

// disarm olvida la operación que terminó.
//
// Sin esto, un Cancelar llegado después cancelaría un contexto ya vencido, que
// no hace nada, y contestaría que sí canceló algo, que sería mentira.
func (o *longOp) disarm() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.cancel = nil
}

// fire corta la operación en curso. Devuelve si había alguna.
func (o *longOp) fire() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.cancel == nil {
		return false
	}
	o.cancel()
	o.cancel = nil
	return true
}

// Cancel corta la operación larga que esté corriendo. Devuelve si había una.
//
// **No toma el candado de la sesión**, a diferencia de todo lo demás de este
// paquete. Es deliberado y es la única forma de que sirva: quien lo tiene
// tomado es exactamente la operación que se quiere cortar.
//
// Devolver "no había ninguna" no es un error. Pasa siempre que se pulse
// Cancelar en el instante en que la operación estaba terminando, y la pantalla
// ya se va a enterar por el resultado de la operación misma.
func (s *Session) Cancel() bool {
	cortada := s.inFlight.fire()
	if cortada {
		s.deps.Log.Info("el usuario canceló la operación en curso")
	}
	return cortada
}

// begin arranca una operación larga cancelable.
//
// Devuelve el contexto que hay que usar de ahí en adelante y el cierre que hay
// que diferir. El cierre hace las dos cosas que van juntas: soltar el contexto
// y borrar el cancelador, para que un Cancelar posterior no diga que cortó algo
// que ya había terminado.
func (s *Session) begin(ctx context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancel(ctx)
	s.inFlight.arm(cancel)
	return ctx, func() {
		s.inFlight.disarm()
		cancel()
	}
}

// cleanupGrace es cuánto se le da a la limpieza de una operación abortada.
//
// Es un tope y no una espera: lo normal es que cierre en milisegundos. Existe
// para que un adaptador colgado no deje el candado de la sesión tomado para
// siempre, que sería una app que no puede volver a intentar nada.
const cleanupGrace = 30 * time.Second

// cleanupContext da un contexto VIVO para deshacer lo que quedó a medias.
//
// # Por qué no vale el contexto de la operación
//
// Porque cuando esto hace falta, ese contexto está cancelado: es lo que acaba
// de abortar la operación. Pasárselo al `teardown` haría que cada paso de la
// limpieza devuelva "context canceled" sin llegar a la API de Windows, o sea
// que cancelar dejaría abierto exactamente lo que el botón promete cerrar: las
// reglas del firewall, la compuerta, los ajustes del adaptador y el motor.
//
// `WithoutCancel` conserva los valores del contexto padre y corta su
// cancelación, que es justo el reparto que hace falta. El plazo propio va
// encima para que la limpieza no pueda colgarse. Ver [cleanupGrace].
func cleanupContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), cleanupGrace)
}
