package nftpermits

// Los DOS caminos que borran la cuarentena de base, y no hay tercero.
//
// # Por qué viven en su propio fichero y con estos nombres
//
// Porque un guardián de `internal/arch` los permite POR NOMBRE y con una lista
// de llamadores cada uno: el del desinstalador lo llama solo `cmd/kanpachid`
// detrás de `--uninstall-cleanup`, y la retirada a pedido viaja por
// [port.FirewallPort] y la llama solo el caso de uso del consentimiento. Los
// nombres largos son lo que permite distinguir "una persona la quitó" de
// "alguien la quitó", que es toda la doctrina: quitar la cuarentena es siempre
// el acto de una persona, jamás de un camino automático.

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

// RemoveBaseQuarantineAtUserRequest takes the quarantine down because the
// PERSON said so: the second of the two closed names, reached only through
// [port.FirewallPort] from the consent use case.
//
// The body repeats the uninstaller's on purpose — a shared helper would be a
// third function doing what only two may do, and the guardian would redden
// it. Table AND file, in that order, same as up there: the table is what has
// effect now, the file is what would resurrect it on the next boot, and a
// removal the user asked for that comes back by itself is the worst way to
// disobey a person.
//
// Idempotent: neither being there is success, the intention is already true.
func (p *Permits) RemoveBaseQuarantineAtUserRequest(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	var fallos []string

	if _, err := exec.LookPath("nft"); err != nil {
		fallos = append(fallos, fmt.Sprintf("no está `nft`, así que la tabla %s no se pudo "+
			"quitar: %v", QuarantineTable, err))
	} else {
		salida, err := exec.CommandContext(ctx, "nft", "delete", "table", "inet", QuarantineTable).
			CombinedOutput()
		texto := strings.TrimSpace(string(salida))
		// `No such file or directory` es lo que contesta nftables cuando la tabla
		// no está, y acá es el usuario apagando dos veces el mismo interruptor.
		if err != nil && !strings.Contains(texto, "No such file or directory") {
			fallos = append(fallos, fmt.Sprintf("quitando la tabla %s: %v: %s",
				QuarantineTable, err, texto))
		}
	}

	if err := os.Remove(p.file); err != nil && !os.IsNotExist(err) {
		fallos = append(fallos, fmt.Sprintf("borrando %s: %v", p.file, err))
	}

	if len(fallos) > 0 {
		return fmt.Errorf("la cuarentena de base quedó a medio quitar: %s",
			strings.Join(fallos, "; "))
	}
	p.log.Info("cuarentena de base retirada a pedido del usuario", "fichero", p.file)
	return nil
}
