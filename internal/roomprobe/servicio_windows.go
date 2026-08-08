//go:build windows

package main

import (
	"errors"
	"fmt"

	"github.com/accentiostudios/kanpachi/core/port"
	"golang.org/x/sys/windows"
)

// ServicioDelDaemon es como el instalador registra el daemon.
//
// Está escrito otra vez acá y no importado porque la constante vive en
// `daemon/cmd/kanpachid`, que es `package main`. La fuente de verdad es
// `servicio_windows.go` de ese paquete: si cambia allá, cambia acá.
const ServicioDelDaemon = "kanpachi-daemon"

// comprobarServicio se niega a arrancar si el daemon instalado está vivo.
//
// # Por qué esto no es una cortesía
//
// Construir la sesión llama a `Firewall.PurgeOwned` dentro del constructor, o
// sea que **borra las reglas que el daemon instalado tiene puestas ahora
// mismo**, con la sala de alguien abierta detrás. Y encima los dos procesos se
// pelean por el motor, por los adaptadores virtuales y por la misma sesión de
// WFP. Es la clase de destrozo que se descubre media hora después, cuando a
// alguien se le cayó la partida.
//
// Con `-force` se sigue igualmente, porque a veces es justo lo que se quiere
// medir. Se avisa, se anota y cuenta como fallo.
func comprobarServicio(log port.Logger, force bool) error {
	corriendo, err := servicioCorriendo()
	if err != nil {
		// No saberlo no impide medir: la mayoría de las máquinas donde corre
		// esta herramienta no tienen Kanpachi instalado.
		log.Warn("no se pudo saber si el servicio del daemon está corriendo", "error", err)
		return nil
	}
	if !corriendo {
		log.Info("el servicio " + ServicioDelDaemon + " no está corriendo, vía libre")
		return nil
	}

	const aviso = "el servicio " + ServicioDelDaemon + " está CORRIENDO.\n\n" +
		"  Los dos procesos se pelean por el motor, por el adaptador virtual y por la\n" +
		"  sesión de WFP. Y peor: arrancar roomprobe purga las reglas de firewall que\n" +
		"  ese daemon tiene puestas AHORA, con la sala de alguien abierta detrás.\n\n" +
		"    Páralo antes:  Stop-Service " + ServicioDelDaemon + "\n" +
		"    Y al terminar: Start-Service " + ServicioDelDaemon

	if !force {
		log.Error("no se arranca: el servicio del daemon está corriendo")
		return fmt.Errorf("%s\n\n  Con -force esto se salta, y pasa lo de arriba", aviso)
	}
	log.Warn("se arranca con -force y el servicio del daemon corriendo: sus reglas se van a purgar")
	fmt.Println("\n  AVISO:", aviso)
	fmt.Println("\n  Se sigue porque pasaste -force.")
	return nil
}

// servicioCorriendo pregunta al gestor de servicios.
//
// Se abre con los permisos MÍNIMOS —`SC_MANAGER_CONNECT` y
// `SERVICE_QUERY_STATUS`— y no con `mgr.Connect`, que pide
// `SC_MANAGER_ALL_ACCESS`. Mismo camino que
// `daemon/cmd/kanpachid/lanzador_windows.go`.
//
// Que el servicio NO exista no es un error: es una máquina sin Kanpachi
// instalado, que es el caso normal de esta herramienta.
func servicioCorriendo() (bool, error) {
	gestor, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return false, fmt.Errorf("abriendo el gestor de servicios: %w", err)
	}
	defer func() { _ = windows.CloseServiceHandle(gestor) }()

	nombre, err := windows.UTF16PtrFromString(ServicioDelDaemon)
	if err != nil {
		return false, err
	}
	h, err := windows.OpenService(gestor, nombre, windows.SERVICE_QUERY_STATUS)
	if err != nil {
		if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
			return false, nil
		}
		return false, fmt.Errorf("abriendo el servicio %s: %w", ServicioDelDaemon, err)
	}
	defer func() { _ = windows.CloseServiceHandle(h) }()

	var estado windows.SERVICE_STATUS
	if err := windows.QueryServiceStatus(h, &estado); err != nil {
		return false, fmt.Errorf("consultando el estado de %s: %w", ServicioDelDaemon, err)
	}
	return estado.CurrentState != windows.SERVICE_STOPPED, nil
}
