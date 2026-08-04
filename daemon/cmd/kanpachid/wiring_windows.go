//go:build windows

package main

// Los adaptadores de verdad que ya existen.
//
// Este archivo es la mitad de la elección que hace este binario. La otra mitad,
// la de los que todavía no existen, está en `main.go` y usa `sinimplementar`,
// que falla en todo a propósito.

import (
	"context"
	"fmt"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/core/port"
	"github.com/accentiostudios/kanpachi/daemon/adapter/firewall"
)

// realFirewall abre las dos capas de contención y devuelve las tres caras que
// el cableado necesita: el puerto que escribe, el que mide, y el cierre.
//
// # Por qué la auditoría se compone acá
//
// [port.ExposureAudit] tiene tres preguntas y el firewall solo puede contestar
// dos. `RouterMappings` le habla al ROUTER del usuario por IGD, que es otro
// protocolo sobre otra red, y es el único punto de todo el producto donde se
// mira hacia afuera de la máquina.
//
// El adaptador del firewall se niega a implementar el puerto entero, y esa
// negativa es deliberada: contestar `nil, nil` a la pregunta del router haría
// que "no hay mapeos" y "nadie miró" fueran indistinguibles, en la única
// pantalla cuyo trabajo es distinguir esas dos cosas. Así que se compone acá,
// que es donde este binario decide con qué.
func realFirewall(dataDir string, log port.Logger, router port.ExposureAudit) (
	port.FirewallPort, port.ExposureAudit, func() error, error) {

	fw, close, err := firewall.NewWindows(dataDir, log)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("%w.\n"+
			"  Escribir en el firewall exige administrador, así que en modo consola hay "+
			"que abrir la terminal elevada", err)
	}
	return fw, exposure{fw: fw, router: router}, close, nil
}

// exposure junta las dos mitades de la auditoría SIN embeberlas.
//
// Explícito y no por embebido, aunque salgan tres líneas más: embeber el
// firewall promovería `Apply` y `PurgeOwned` sobre el objeto que solo debería
// medir, y una auditoría con métodos que modifican es lo contrario de una
// auditoría.
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
