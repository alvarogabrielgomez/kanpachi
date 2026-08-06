//go:build !windows

package inspector

import (
	"context"
	"fmt"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// Snapshot fuera de Windows no mira nada.
//
// Existe para que el paquete compile en el job de Linux. Acá no hay nada
// portable que salvar, a diferencia del lector de Steam: leer una tabla de
// sockets del kernel es la definición de específico del sistema.
func (s *Sockets) Snapshot(context.Context, domain.ProcessRef) ([]domain.Listener, error) {
	return nil, fmt.Errorf("la foto de sockets solo está escrita para Windows")
}
