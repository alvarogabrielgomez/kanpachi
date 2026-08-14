//go:build windows

package main

import (
	"fmt"

	"github.com/accentiostudios/kanpachi/core/port"
	"github.com/accentiostudios/kanpachi/daemon/preflight"
)

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
	corriendo, err := preflight.DaemonServiceRunning()
	if err != nil {
		// No saberlo no impide medir: la mayoría de las máquinas donde corre
		// esta herramienta no tienen Kanpachi instalado.
		log.Warn("no se pudo saber si el servicio del daemon está corriendo", "error", err)
		return nil
	}
	if !corriendo {
		log.Info("el servicio " + preflight.DaemonService + " no está corriendo, vía libre")
		return nil
	}

	const aviso = "el servicio " + preflight.DaemonService + " está CORRIENDO.\n\n" +
		"  Los dos procesos se pelean por el motor, por el adaptador virtual y por la\n" +
		"  sesión de WFP. Y peor: arrancar roomprobe purga las reglas de firewall que\n" +
		"  ese daemon tiene puestas AHORA, con la sala de alguien abierta detrás.\n\n" +
		"    Páralo antes:  Stop-Service " + preflight.DaemonService + "\n" +
		"    Y al terminar: Start-Service " + preflight.DaemonService

	if !force {
		log.Error("no se arranca: el servicio del daemon está corriendo")
		return fmt.Errorf("%s\n\n  Con -force esto se salta, y pasa lo de arriba", aviso)
	}
	log.Warn("se arranca con -force y el servicio del daemon corriendo: sus reglas se van a purgar")
	fmt.Println("\n  AVISO:", aviso)
	fmt.Println("\n  Se sigue porque pasaste -force.")
	return nil
}
