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

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/core/port"
	"github.com/accentiostudios/kanpachi/daemon/adapter/firewall"
	"github.com/accentiostudios/kanpachi/daemon/adapter/firewall/linux/nftpermits"
)

// sistemaDeCuarentena es qué lista de puertos cierra la cuarentena de base.
//
// Se declara acá, en el fichero del sistema, y no se deduce en `main.go` con un
// `runtime.GOOS`. La diferencia importa: así el día que haya un tercer sistema,
// no compilar su `wiring_*.go` es un error de enlazado en vez de un `default`
// que aplica la lista de otro.
//
// Y la lista de Linux no es la de Windows por una razón cara: allá el 22 es un
// servicio opcional, acá es el canal por el que el operador administra su
// servidor. Ver [domain.QuarantineSystem].
const sistemaDeCuarentena = domain.QuarantineLinux

// realFirewall compone las dos capas de Linux.
func realFirewall(_ string, log port.Logger, router port.ExposureAudit) (
	port.FirewallPort, port.ExposureAudit, func() error, error) {

	// La cuarentena va a su ruta de siempre y no al directorio de datos: la
	// unidad de systemd que la carga al arrancar apunta ahí, y las dos tienen que
	// decir lo mismo o la cuarentena se pierde en el próximo reinicio sin que
	// nada lo diga.
	fw, close, err := firewall.NewLinux(nftpermits.QuarantineFile, log)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("%w.\n"+
			"  Escribir en nftables exige CAP_NET_ADMIN, así que corriendo a mano hay que "+
			"usar sudo. El servicio lo trae en su unidad", err)
	}
	return fw, exposure{fw: fw, router: router}, close, nil
}

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
