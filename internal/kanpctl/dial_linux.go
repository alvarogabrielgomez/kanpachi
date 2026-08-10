//go:build linux

package main

import (
	"net"
	"time"
)

// dial abre el socket Unix del daemon.
//
// El mismo plazo que en Windows y por lo mismo: conectarse a un canal local es
// inmediato o no va a pasar, y esperar más solo alarga el rato en que la
// herramienta parece colgada cuando lo que hay es un daemon que no arrancó.
//
// No hace falta comprobar nada del otro lado desde acá. Quien decide si esta
// conexión vale es el daemon: el modo del socket ya impide abrirlo a quien no
// sea de casa, y encima pregunta al kernel por el uid. Ver `pipe_linux.go`.
func dial(nombre string) (net.Conn, error) {
	return net.DialTimeout("unix", nombre, 5*time.Second)
}
