//go:build !windows && !linux

package kanpachi

import (
	"context"
	"errors"
)

// Kanpachi corre en Windows y en Linux, y este fichero existe para que el resto
// del paquete siga compilando en cualquier otro sistema.
//
// Es lo que hace que `spec.go` y `engine.go`, que son donde vive todo lo que se
// puede equivocar caro, no dependan de ningún sistema para leerse ni para
// probarse. Devolver un error acá y no un provisional que arranca algo es
// deliberado: un provisional que dice que sí haría inverificable la única prueba
// que importa.
func spawn(context.Context, string) (child, error) {
	return nil, errors.New("el motor de Kanpachi corre en Windows y en Linux, " +
		"y este binario no es de ninguno de los dos")
}

// Sin motor no hay motores que matar, y cero es la respuesta honesta: no es un
// fallo, es que no hay ninguno.
func killOrphans(string) int { return 0 }
