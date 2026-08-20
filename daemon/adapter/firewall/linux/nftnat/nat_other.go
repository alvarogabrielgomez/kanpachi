//go:build !linux

package nftnat

import (
	"context"
	"fmt"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// La contraparte que existe para que `go build ./...` siga funcionando fuera de
// Linux, igual que la de la compuerta.
//
// No es un adaptador provisional que haya que terminar: el desvío es del modo
// contenedor, que solo existe en Linux. Fuera de ahí nadie construye esto, y si
// alguien lo construyera, lo correcto es que lo diga en vez de fingir que
// desvió algo.

func (r *Redirect) Apply(context.Context, domain.RedirectSpec) error {
	return fmt.Errorf("el desvío hacia el juego está escrito para Linux")
}

func (r *Redirect) Clear(context.Context) error {
	return fmt.Errorf("el desvío hacia el juego está escrito para Linux")
}
