//go:build !windows

package main

// El firewall de Kanpachi es el Firewall de Windows más una sesión de WFP, así
// que fuera de Windows no hay adaptador posible ni lo va a haber.
//
// Existe para que `go build ./...` siga compilando en el job de Linux, que es
// el que corre los tests del dominio, del orden de arranque y del canal de la
// sala. Devuelve error en vez de un provisional que finja: un firewall que dice
// que purgó sin purgar hace la cuarentena inverificable, que es la razón de que
// `sinimplementar` falle en todo.

import (
	"context"
	"fmt"

	"github.com/accentiostudios/kanpachi/core/port"
)

func realFirewall(string, port.Logger, port.ExposureAudit) (
	port.FirewallPort, port.ExposureAudit, func() error, error) {

	return nil, nil, nil, fmt.Errorf("el firewall de Kanpachi son las reglas del Firewall " +
		"de Windows y una sesión de WFP, así que este binario solo sirve en Windows")
}

// Fuera de Windows no hay cuarentena que quitar, porque no hay firewall donde
// ponerla. Ver la cabecera.
func quitarCuarentenaDeBase(context.Context, string, port.Logger) error {
	return fmt.Errorf("la cuarentena de Kanpachi vive en el Firewall de Windows, " +
		"así que este binario solo la puede quitar en Windows")
}

// protegerFichero no hace nada fuera de Windows, y no es un provisional que
// finja: acá el modo 0600 con el que se escribe la llave YA es la protección,
// porque en Unix el modo es lo que gobierna. Lo que no gobierna es en Windows,
// que es donde vive el adaptador de verdad.
func protegerFichero(string) error { return nil }
