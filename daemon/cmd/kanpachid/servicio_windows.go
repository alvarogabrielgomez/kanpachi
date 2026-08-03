//go:build windows

package main

import (
	"context"
	"fmt"

	"golang.org/x/sys/windows/svc"
)

// ServiceName es como se registra en Windows.
const ServiceName = "kanpachi-daemon"

// EnServicio dice si este proceso lo arrancó el Administrador de servicios.
//
// Se pregunta y no se deduce de una bandera: arrancar como servicio y arrancar
// a mano se distinguen por cómo entró el proceso, no por lo que alguien
// escribió en la línea de comandos.
func EnServicio() (bool, error) { return svc.IsWindowsService() }

// CorrerComoServicio le entrega el control al Administrador de servicios.
//
// `arrancar` tiene que devolver cuando el daemon esté LISTO, y `esperar` bloquea
// hasta que se caiga solo. Esa separación es lo que hace que SERVICE_RUNNING se
// reporte en el momento correcto: después de que el pipe esté abierto y no
// antes. Al revés, Windows daría el servicio por arrancado mientras todavía no
// hay quien atienda, y el primer intento de la UI fallaría sin motivo visible.
func CorrerComoServicio(arrancar func(context.Context) (esperar func() error, apagar func(), err error)) error {
	return svc.Run(ServiceName, &manejador{arrancar: arrancar})
}

type manejador struct {
	arrancar func(context.Context) (func() error, func(), error)
}

func (m *manejador) Execute(_ []string, pide <-chan svc.ChangeRequest, estado chan<- svc.Status) (bool, uint32) {
	// StartPending mientras se purga el firewall y se levanta el motor. Windows
	// espera con esto puesto en vez de dar el arranque por fallido.
	estado <- svc.Status{State: svc.StartPending}

	ctx, cancelar := context.WithCancel(context.Background())
	defer cancelar()

	esperar, apagar, err := m.arrancar(ctx)
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
