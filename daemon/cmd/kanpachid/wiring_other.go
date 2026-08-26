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
	"os"

	"github.com/accentiostudios/kanpachi/core/port"
	"github.com/accentiostudios/kanpachi/daemon/paths"
)

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

// prepararCarpetaDeLogDeLaUI acá solo crea la carpeta.
//
// No hay ventana fuera de Windows todavía, y el día que la haya el permiso no
// se arregla con esto: `/var/lib/kanpachi` es de root en 0700 y el canal local
// solo acepta a root o al uid del daemon, así que lo PRIMERO que hay que
// decidir es si ese canal admite al usuario del escritorio. Ver
// `daemon/transport/pipe/pipe_linux.go`.
func prepararCarpetaDeLogDeLaUI(ruta string) error {
	return os.MkdirAll(ruta, 0o700)
}
