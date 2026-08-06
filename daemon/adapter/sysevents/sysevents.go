// Package sysevents avisa cuando le pasa algo a la MÁQUINA que invalida lo que
// Kanpachi dejó puesto.
//
// No son eventos del motor ni de la sala: son de Windows, y llegan de tres
// subsistemas que no se conocen entre sí. Por eso son tres canales y no uno con
// un enum, tal como pide [port.SystemEvents]: una suscripción muerta se VE,
// porque su canal deja de emitir mientras los otros dos siguen.
//
// # Ninguno de los tres es fiable, y eso está asumido arriba
//
// El supervisor reaplica los ajustes del adaptador cada tantos latidos aunque
// no llegue ningún evento. Esto no reemplaza ese respaldo: lo adelanta. La
// diferencia medida es que reponer la métrica tras un cambio de red pase de
// esperar al siguiente latido a ocurrir en el momento.
//
// # Por qué ninguno usa una bomba de mensajes
//
// Las tres suscripciones se hicieron con la variante que SEÑALA UN EVENTO en
// vez de la que llama a una función:
//
//   - El visor de eventos admite un `HANDLE` de evento en vez de un callback.
//   - Los cambios de dirección tienen `NotifyAddrChange` con `OVERLAPPED`.
//   - La energía es la excepción y sí es un callback, con la regla de que ese
//     callback no bloquee nunca.
//
// Con eventos, cada suscripción es una goroutine esperando un handle, que es
// código Go normal y se apaga cerrando su evento de parada. Con una bomba de
// mensajes haría falta una ventana oculta, un hilo fijado con
// `runtime.LockOSThread`, y un `DispatchMessage` del que hay que salir sin
// dejar la ventana viva. Es más código y más formas de colgar el apagado del
// servicio.
package sysevents

import (
	"sync"

	"github.com/accentiostudios/kanpachi/core/port"
)

// Events son las tres suscripciones.
type Events struct {
	identified chan struct{}
	resumed    chan struct{}
	changed    chan struct{}

	// stop lo cierra [Events.Close] y es lo que despierta a las tres
	// goroutines. Cerrar un canal es la única señal de parada que todas las
	// esperas de Go entienden sin coordinación.
	stop chan struct{}

	// undo son los cierres que cada suscripción deja apuntados: handles del
	// sistema, registros que hay que dar de baja. Los llena la mitad de este
	// paquete que sabe de Windows.
	undo []func()

	waiting sync.WaitGroup
	once    sync.Once
	log     port.Logger
}

// New abre las tres suscripciones.
//
// **Nunca devuelve error, y es deliberado.** [port.SystemEvents] no tiene por
// dónde devolverlo, y no lo necesita: una suscripción que no se pudo abrir deja
// su canal mudo, que es exactamente lo que este adaptador tiene que decir. Lo
// que sí hace es dejarlo escrito en el log, porque un canal mudo por un fallo y
// uno mudo porque no pasa nada se ven igual desde arriba.
func New(log port.Logger) *Events {
	e := &Events{
		// Capacidad 1 y envío que no bloquea. Estas señales son "algo cambió,
		// vuelve a mirar", así que dos seguidas valen lo mismo que una, y lo
		// único que no se puede perder es la última. Sin el búfer, una señal
		// que llegue mientras el supervisor está ocupado se perdería entera.
		identified: make(chan struct{}, 1),
		resumed:    make(chan struct{}, 1),
		changed:    make(chan struct{}, 1),
		stop:       make(chan struct{}),
		log:        log,
	}
	e.subscribe()
	return e
}

// signal deja una señal sin bloquear jamás.
//
// Lo llaman goroutines de espera y, en el caso de la energía, un callback del
// sistema. Un envío que bloqueara en un callback de Windows colgaría el hilo
// del sistema que lo invocó, que es de las peores cosas que se pueden hacer en
// esta plataforma.
func signal(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

func (e *Events) NetworkIdentified() <-chan struct{} { return e.identified }
func (e *Events) Resumed() <-chan struct{}           { return e.resumed }
func (e *Events) NetworkChanged() <-chan struct{}    { return e.changed }

// Close corta las suscripciones y cierra los tres canales.
//
// Es IDEMPOTENTE y NUNCA espera a que alguien lea, que es lo que
// [port.SystemEvents] exige: un Close que bloqueara esperando lector colgaría
// el apagado del servicio, y ahí es donde quedan reglas huérfanas de verdad.
//
// El orden importa. Primero se despierta a las goroutines, luego se espera a
// que salgan, y solo entonces se sueltan los handles: soltarlos antes dejaría a
// una goroutine esperando sobre un handle cerrado, que en Windows es
// comportamiento indefinido y no un error.
func (e *Events) Close() error {
	e.once.Do(func() {
		close(e.stop)
		e.waiting.Wait()
		for i := len(e.undo) - 1; i >= 0; i-- {
			e.undo[i]()
		}
		close(e.identified)
		close(e.resumed)
		close(e.changed)
	})
	return nil
}

var _ port.SystemEvents = (*Events)(nil)
