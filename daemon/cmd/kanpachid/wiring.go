package main

// Lo del cableado que no depende del sistema.
//
// Los `wiring_*.go` de cada sistema eligen los adaptadores concretos; acá vive
// lo que los dos componen igual. Empezó siendo un solo tipo, y está en su
// fichero desde que hay dos sistemas: duplicarlo en cada uno es cómo se acaba
// con dos auditorías que se comportan distinto sin que nadie lo haya decidido.

import (
	"context"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/core/port"
	"github.com/accentiostudios/kanpachi/daemon/adapter/firewall"
)

// exposure junta las dos mitades de la auditoría SIN embeberlas.
//
// Explícito y no por embebido, aunque salgan tres líneas más: embeber el
// firewall promovería `Apply` y `PurgeOwned` sobre el objeto que solo debería
// medir, y una auditoría con métodos que modifican es lo contrario de una
// auditoría.
//
// Existe porque el adaptador del firewall se niega a implementar el puerto
// entero: contestar `nil, nil` a la pregunta del router haría que "no hay
// mapeos" y "nadie miró" fueran indistinguibles, en la única pantalla cuyo
// trabajo es distinguir esas dos cosas.
type exposure struct {
	fw     *firewall.Firewall
	router port.ExposureAudit
}

func (e exposure) FirewallEnabled(ctx context.Context) ([]domain.FirewallProfileState, error) {
	return e.fw.FirewallEnabled(ctx)
}

func (e exposure) Enforcement(ctx context.Context) (domain.Enforcement, error) {
	return e.fw.Enforcement(ctx)
}

func (e exposure) RouterMappings(ctx context.Context) ([]domain.PortMapping, error) {
	return e.router.RouterMappings(ctx)
}
