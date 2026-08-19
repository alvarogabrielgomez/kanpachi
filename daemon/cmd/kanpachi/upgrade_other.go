//go:build !linux

package main

import (
	"context"
	"errors"
)

// aptInstall no existe fuera de Linux, y no lo alcanza nadie: quien se niega
// antes es [sePuedeActualizarAcá], que mira `runtime.GOOS` y explica que en
// Windows la actualización va por el instalador.
//
// Se declara igual para que `go build ./...` compile en Windows, que es donde se
// desarrolla, y devuelve error en vez de fingir: un `upgrade` que dijera que
// instaló sin instalar es peor que uno que no existe.
func aptInstall(context.Context, string, bool) error {
	return errors.New("installing a .deb is a Linux thing")
}
