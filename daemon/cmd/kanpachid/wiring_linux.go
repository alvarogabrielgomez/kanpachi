//go:build linux

package main

// Con qué se compone el firewall en Linux.
//
// Es el gemelo de `wiring_windows.go`, y las diferencias son las del sistema:
// la compuerta es nftables en vez de WFP, la capa de permisos no abre nada
// porque no hay nada que abrir, y proteger un fichero es su modo y no una ACL.

import (
	"context"
	"fmt"
	"os"

	"github.com/accentiostudios/kanpachi/core/port"
	"github.com/accentiostudios/kanpachi/daemon/adapter/firewall/linux/nftpermits"
	"github.com/accentiostudios/kanpachi/daemon/paths"
)

// defaultDataDir sale de [paths.Data], que es el paquete que los DOS binarios
// importan.
//
// Vivía acá, como constante exportada, con un comentario que decía que el `.deb`
// y el CLI tenían que nombrar exactamente esta ruta. Exportarla desde
// `package main` no dejaba que nadie la usara, así que la promesa la sostenía
// que dos ficheros dijeran lo mismo.
func defaultDataDir() string { return paths.Data() }

// engineExe es cómo se llama el motor al lado de este binario.
//
// Sin `.exe`, que es lo obvio, y hace falta decirlo igual: el nombre se compara
// con `/proc/<pid>/exe` para matar motores huérfanos, así que un nombre que no
// sea el del fichero de verdad no mata nada y el síntoma es una sala que conecta
// a ratos. Ver `killOrphans` en el adaptador del motor.
const engineExe = "kanpachi-engine"

// CatalogDir es dónde vive el catálogo que vino con el paquete.
//
// NO al lado del binario, que es lo que hace Windows: acá los ejecutables van a
// `/usr/libexec/kanpachi` y esto es un fichero de datos que no depende de la
// arquitectura, o sea `/usr/share`. Es lo que dice el estándar de jerarquía de
// ficheros y es lo que un empaquetador de Debian espera encontrar.
//
// Exportada por lo mismo que [DataDir]: el `.deb` tiene que poner el fichero
// exactamente acá, y dos constantes que se copian se desincronizan.
const CatalogDir = "/usr/share/kanpachi"

func builtinCatalogDir() string { return CatalogDir }

// packageRemovesData dice si el empaquetador se lleva el directorio de datos.
//
// Acá sí, y por eso el desinstalador NO borra la llave: `apt purge` se lleva
// `/var/lib/kanpachi` entero, y `apt remove` tiene que conservarlo. Esa es la
// distinción que dpkg hace y que el usuario espera; romperla convierte quitar y
// reinstalar en cambiar de máquina para todos los que ya jugaron con esta.
const packageRemovesData = true

// quitarCuarentenaDeBase es el único camino del repositorio que la borra.
//
// Abre su PROPIO adaptador y no usa el del producto por lo mismo que en
// Windows: el puerto del producto no puede quitarla a propósito, y un guardián
// de `internal/arch` comprueba que esa capacidad no exista en la interfaz.
// Quitarla es del desinstalador, que no habla por ese puerto.
func quitarCuarentenaDeBase(ctx context.Context, _ string, log port.Logger) error {
	if err := nftpermits.RemoveBaseQuarantineForUninstall(ctx, nftpermits.QuarantineFile); err != nil {
		return err
	}
	log.Info("cuarentena de base quitada", "tabla", nftpermits.QuarantineTable)
	return nil
}

// protegerFichero deja la llave legible solo por su dueño.
//
// En Windows esto es una ACL entera y acá es una llamada. La diferencia no es
// que Linux proteja menos: es que el modo ES el mecanismo, y `safewrite` ya
// escribe con 0600. Esto lo vuelve a poner porque un fichero que venía de una
// versión anterior pudo quedar con otro modo, y arreglarlo cuesta una llamada.
func protegerFichero(ruta string) error {
	if err := os.Chmod(ruta, 0o600); err != nil {
		return fmt.Errorf("ajustando el modo de %s: %w", ruta, err)
	}
	return nil
}
