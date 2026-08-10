package nftpermits

// El ÚNICO camino que borra la cuarentena de base.
//
// # Por qué vive en su propio fichero y con este nombre
//
// Porque un guardián de `internal/arch` comprueba dos cosas a la vez: que
// ninguna llamada destructiva de `daemon/` nombre el grupo base, y que exista
// exactamente esta función haciéndolo. El nombre largo es lo que permite
// distinguir "el desinstalador la quita" de "alguien la quitó".
//
// La capacidad no está en [port.FirewallPort] a propósito, y eso también lo
// vigila un guardián: lo que la interfaz no declara, ningún caso de uso puede
// llamar. La cuarentena vale porque sigue puesta con el daemon parado, así que
// quitarla no puede ser una operación del daemon.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// RemoveBaseQuarantineForUninstall se lleva la tabla Y el fichero.
//
// # Los dos, y en este orden
//
// La tabla es lo que está puesto ahora; el fichero es lo que la volvería a
// poner en el próximo arranque. Quitar solo la tabla deja una cuarentena que
// resucita después de desinstalar el producto, que es de las cosas que peor se
// diagnostican: el usuario ya no tiene Kanpachi y su SMB sigue sin contestar.
//
// La tabla primero porque es lo que tiene efecto. Si falla el borrado del
// fichero, queda anotado y la tabla ya no está.
//
// # Que no exista NO es un error
//
// Desinstalar dos veces, o desinstalar una instalación que nunca llegó a
// arrancar, tienen que terminar bien. Lo que no puede pasar es que un fallo de
// verdad se lea como "no estaba".
func RemoveBaseQuarantineForUninstall(ctx context.Context, file string) error {
	if file == "" {
		file = QuarantineFile
	}

	var fallos []string

	if _, err := exec.LookPath("nft"); err != nil {
		fallos = append(fallos, fmt.Sprintf("no está `nft`, así que la tabla %s no se pudo "+
			"quitar: %v", QuarantineTable, err))
	} else {
		salida, err := exec.CommandContext(ctx, "nft", "delete", "table", "inet", QuarantineTable).
			CombinedOutput()
		texto := strings.TrimSpace(string(salida))
		// `No such file or directory` es lo que contesta nftables cuando la tabla
		// no está, y es el caso normal de desinstalar dos veces.
		if err != nil && !strings.Contains(texto, "No such file or directory") {
			fallos = append(fallos, fmt.Sprintf("quitando la tabla %s: %v: %s",
				QuarantineTable, err, texto))
		}
	}

	if err := os.Remove(file); err != nil && !os.IsNotExist(err) {
		fallos = append(fallos, fmt.Sprintf("borrando %s: %v", file, err))
	}

	if len(fallos) > 0 {
		return fmt.Errorf("la cuarentena de base quedó a medio quitar: %s",
			strings.Join(fallos, "; "))
	}
	return nil
}
