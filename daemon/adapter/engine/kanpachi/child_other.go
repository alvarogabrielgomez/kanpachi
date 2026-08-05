//go:build !windows

package kanpachi

import (
	"context"
	"errors"
)

// El cliente de Kanpachi es solo Windows, y este fichero existe para que el
// resto del paquete siga compilando y probándose en el CI de Linux.
//
// Es lo que hace que `spec.go` y `engine.go`, que son donde vive todo lo que se
// puede equivocar caro, corran sin admin, sin red y sin Windows. Devolver un
// error acá y no un provisional que arranca algo es deliberado: un provisional
// que dice que sí haría inverificable la única prueba que importa.
func spawn(context.Context, string) (child, error) {
	return nil, errors.New("el motor de Kanpachi solo corre en Windows")
}

// Sin Windows no hay motores que matar, y cero es la respuesta honesta: no es
// un fallo, es que no hay ninguno.
func killOrphans(string) int { return 0 }
