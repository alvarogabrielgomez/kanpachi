//go:build linux

package client

import (
	"net"

	"github.com/accentiostudios/kanpachi/core/timing"
)

// Dial abre el socket Unix del daemon.
//
// No hace falta comprobar nada del otro lado desde acá. Quien decide si esta
// conexión vale es el daemon: el modo del socket ya impide abrirlo a quien no
// sea de casa, y encima le pregunta al kernel por el uid del que se conectó. Ver
// `checkPeer` en `pipe_linux.go`.
func Dial(nombre string) (net.Conn, error) {
	return net.DialTimeout("unix", nombre, timing.PipeDialTimeout)
}
