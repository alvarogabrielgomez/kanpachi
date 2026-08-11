//go:build !windows && !linux

package main

// Fuera de Windows y de Linux no hay adaptador de firewall, y no lo va a haber:
// el de Windows son las reglas del Firewall de Windows más una sesión de WFP, y
// el de Linux es nftables.
//
// Existe para que `go build ./...` siga compilando en el job de Linux, que es
// el que corre los tests del dominio, del orden de arranque y del canal de la
// sala. Devuelve error en vez de un provisional que finja: un firewall que dice
// que purgó sin purgar hace la cuarentena inverificable, que es la razón de que
// `sinimplementar` falle en todo.

import (
	"context"
	"fmt"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/core/port"
	"github.com/accentiostudios/kanpachi/daemon/paths"
)

// sistemaDeCuarentena queda en cero, que es lo que `validate` rechaza con su
// propio mensaje. Es lo correcto: acá no hay cuarentena que escribir, y elegir
// una de las dos listas sería fingir que sí.
const sistemaDeCuarentena domain.QuarantineSystem = 0

// defaultDataDir sale de [paths.Data], que contesta la ruta de Unix aunque acá
// no arranque nada. El porqué vive allá; quien de verdad se niega es
// [realFirewall].
func defaultDataDir() string { return paths.Data() }

// engineExe no lo alcanza nadie acá: [realFirewall] falla antes. Se declara
// porque `main.go` lo nombra sin preguntar en qué sistema corre.
const engineExe = "kanpachi-engine"

// builtinCatalogDir contesta la ruta de Unix aunque acá no arranque nada, por lo
// mismo que [defaultDataDir].
func builtinCatalogDir() string { return "/usr/share/kanpachi" }

// packageRemovesData no lo alcanza nadie acá: [realFirewall] falla antes.
const packageRemovesData = false

func realFirewall(string, port.Logger, port.ExposureAudit) (
	port.FirewallPort, port.ExposureAudit, func() error, error) {

	return nil, nil, nil, fmt.Errorf("el firewall de Kanpachi son las reglas del Firewall " +
		"de Windows y una sesión de WFP, así que este binario solo sirve en Windows y en Linux")
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
