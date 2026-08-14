//go:build windows

package main

import (
	"context"
	"fmt"

	"github.com/accentiostudios/kanpachi/daemon/preflight"
	"golang.org/x/sys/windows/svc"
)

// ServiceName es como se registra en Windows.
// ServiceName sale de [preflight.DaemonService]: es la MISMA cadena que las
// herramientas usan para preguntar si este servicio está vivo, y dos copias de
// un nombre de servicio son dos servicios el día que una cambie.
const ServiceName = preflight.DaemonService

// EnServicio dice si este proceso lo arrancó el Administrador de servicios.
//
// Se pregunta y no se deduce de una bandera: arrancar como servicio y arrancar
// a mano se distinguen por cómo entró el proceso, no por lo que alguien
// escribió en la línea de comandos.
func EnServicio() (bool, error) { return svc.IsWindowsService() }

// ArgShow es el argumento con el que se pide arrancar el servicio Y abrir la
// ventana de la interfaz.
//
// Lo manda quien arranca el servicio a mano —el lanzador, vía `StartService`— y
// NO lo manda el arranque automático de Windows. Esa es toda la diferencia
// entre encender la PC, que deja solo el icono en la bandeja, y hacer doble
// clic, que abre Kanpachi.
//
// Es un argumento del SERVICIO y no una bandera de la línea de comandos de este
// proceso: quien lo arranca no es una consola, es el Administrador de
// servicios, y esta es la única vía que tiene para pasarle algo.
//
// Se escribe igual que la bandera del lanzador y que la de la interfaz, y eso
// es a propósito: `--show` significa lo mismo en los tres sitios, o sea enseña
// la ventana. El silencio es el default en todos.
const ArgShow = "--show"

// tiene dice si el argumento está en la lista.
func tiene(args []string, quéBusco string) bool {
	for _, a := range args {
		if a == quéBusco {
			return true
		}
	}
	return false
}

// CorrerComoServicio le entrega el control al Administrador de servicios.
//
// `arrancar` tiene que devolver cuando el daemon esté LISTO, y `esperar` bloquea
// hasta que se caiga solo. Esa separación es lo que hace que SERVICE_RUNNING se
// reporte en el momento correcto: después de que el pipe esté abierto y no
// antes. Al revés, Windows daría el servicio por arrancado mientras todavía no
// hay quien atienda, y el primer intento de la UI fallaría sin motivo visible.
//
// Recibe también los argumentos con los que se arrancó el servicio. Ver
// [ArgShowUI].
func CorrerComoServicio(arrancar func(context.Context, []string) (esperar func() error, apagar func(), err error)) error {
	return svc.Run(ServiceName, &manejador{arrancar: arrancar})
}

type manejador struct {
	arrancar func(context.Context, []string) (func() error, func(), error)
}

func (m *manejador) Execute(args []string, pide <-chan svc.ChangeRequest, estado chan<- svc.Status) (bool, uint32) {
	// StartPending mientras se purga el firewall y se levanta el motor. Windows
	// espera con esto puesto en vez de dar el arranque por fallido.
	estado <- svc.Status{State: svc.StartPending}

	ctx, cancelar := context.WithCancel(context.Background())
	defer cancelar()

	// `args[0]` es el nombre del servicio, que Windows pone siempre. Lo que
	// alguien pasó en `StartService` empieza en el uno.
	esperar, apagar, err := m.arrancar(ctx, args)
	if err != nil {
		// Un código distinto de cero es lo que hace que la política de
		// recuperación del servicio reintente. Devolver éxito acá dejaría un
		// servicio "arrancado" que no atiende a nadie.
		return false, 1
	}

	// **Recién ahora.** El daemon está listo, o sea el firewall purgado y el
	// pipe abierto.
	estado <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}

	fin := make(chan error, 1)
	go func() { fin <- esperar() }()

	for {
		select {
		case c := <-pide:
			switch c.Cmd {
			case svc.Interrogate:
				estado <- c.CurrentStatus

			case svc.Stop, svc.Shutdown:
				// StopPending antes de empezar, para que Windows espere en vez
				// de matar el proceso a mitad del cierre. Ahí es cuando quedan
				// reglas huérfanas de verdad.
				estado <- svc.Status{State: svc.StopPending}
				cancelar()
				apagar()
				return false, 0
			}

		case <-fin:
			// La entrada se cayó sola. Se cierra igual de limpio.
			estado <- svc.Status{State: svc.StopPending}
			cancelar()
			apagar()
			return false, 0
		}
	}
}

// ErrSinHost es lo que se devuelve si alguien llega acá sin poder ser servicio.
var ErrSinHost = fmt.Errorf("kanpachid: este proceso no lo arrancó el Administrador de servicios")
