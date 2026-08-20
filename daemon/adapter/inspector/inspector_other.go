//go:build !windows && !linux

package inspector

import (
	"context"
	"fmt"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// Snapshot fuera de Windows no mira nada.
//
// Existe para que el paquete compile en cualquier sistema. Acá no hay nada
// portable que salvar, a diferencia del lector de Steam: leer una tabla de
// sockets del kernel es la definición de específico del sistema.
// Listening tampoco, y por el mismo motivo.
func (s *Sockets) Listening(context.Context) ([]domain.Listener, error) {
	return nil, fmt.Errorf("la lista de puertos atados está escrita para Windows y para Linux")
}

func (s *Sockets) Snapshot(context.Context, domain.ProcessRef) ([]domain.Listener, error) {
	return nil, fmt.Errorf("la foto de sockets está escrita para Windows y para Linux")
}
