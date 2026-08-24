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
	"os"

	"golang.org/x/sys/windows"

	"github.com/accentiostudios/kanpachi/core/port"
	"github.com/accentiostudios/kanpachi/daemon/adapter/firewall/windows/netfw"
	"github.com/accentiostudios/kanpachi/daemon/paths"
)

// engineExe es cómo se llama el motor al lado de este binario.
const engineExe = "kanpachi-engine.exe"

// packageRemovesData dice si el empaquetador se lleva el directorio de datos.
//
// Acá no. El desinstalador quita Program Files y deja `ProgramData\Kanpachi`
// en pie, así que la llave de identidad hay que borrarla explícitamente o queda
// viva en una máquina donde Kanpachi ya no está. Ver [limpiar].
const packageRemovesData = false

// builtinCatalogDir es dónde vive el catálogo que vino con la app.
//
// Al lado del binario, o sea en Program Files, que es de solo lectura para el
// daemon y lo actualiza el instalador. Ver [catalogstore.Store].
func builtinCatalogDir() string { return dirDelBinario() }

// defaultDataDir sale de [paths.Data], que es el paquete que los DOS binarios
// importan. El porqué de la ruta y de su ACL vive allá.
func defaultDataDir() string { return paths.Data() }

// protegerFichero le pone a un fichero su ACL PROPIA: solo SYSTEM y
// Administradores, sin heredar nada.
//
// # Por qué hace falta, si ya se escribe con modo 0600
//
// Porque en Windows el modo de Go no gobierna nada: `os.Chmod` solo mueve el
// atributo de solo lectura, y quien decide quién puede leer un fichero es su
// ACL, que por omisión se hereda del directorio. La ACL de `ProgramData\Kanpachi`
// da lectura a todos los usuarios de la máquina a propósito, porque ahí viven
// ficheros que la UI necesita leer sin elevar.
//
// `identity.key` es la única excepción, y `docs/03-arquitectura.md` la nombra:
// es lo que impide que alguien se haga pasar por este equipo ante quienes ya
// jugaron con él, y a diferencia de una credencial de sala no vence ni se
// revoca sola.
//
// `PROTECTED_DACL_SECURITY_INFORMATION` es la mitad que se olvida: sin ella la
// ACL nueva se SUMA a la heredada, así que el permiso de lectura del directorio
// seguiría ahí y esto no habría servido de nada.
func protegerFichero(ruta string) error {
	sistema, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		return fmt.Errorf("resolviendo el SID de SYSTEM: %w", err)
	}
	admins, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return fmt.Errorf("resolviendo el SID de Administradores: %w", err)
	}

	entradas := []windows.EXPLICIT_ACCESS{
		{
			AccessPermissions: windows.GENERIC_ALL,
			AccessMode:        windows.GRANT_ACCESS,
			Inheritance:       windows.NO_INHERITANCE,
			Trustee: windows.TRUSTEE{
				TrusteeForm:  windows.TRUSTEE_IS_SID,
				TrusteeType:  windows.TRUSTEE_IS_USER,
				TrusteeValue: windows.TrusteeValueFromSID(sistema),
			},
		},
		{
			AccessPermissions: windows.GENERIC_ALL,
			AccessMode:        windows.GRANT_ACCESS,
			Inheritance:       windows.NO_INHERITANCE,
			Trustee: windows.TRUSTEE{
				TrusteeForm:  windows.TRUSTEE_IS_SID,
				TrusteeType:  windows.TRUSTEE_IS_GROUP,
				TrusteeValue: windows.TrusteeValueFromSID(admins),
			},
		},
	}
	dacl, err := windows.ACLFromEntries(entradas, nil)
	if err != nil {
		return fmt.Errorf("armando los permisos de %s: %w", ruta, err)
	}
	err = windows.SetNamedSecurityInfo(
		ruta,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, dacl, nil,
	)
	if err != nil {
		return fmt.Errorf("aplicando los permisos de %s: %w", ruta, err)
	}
	return nil
}

// quitarCuarentenaDeBase es el único camino del repositorio que la borra.
//
// # Por qué abre su PROPIO adaptador y no usa el del producto
//
// Porque el puerto del producto no puede quitarla, a propósito:
// `port.FirewallPort` no declara nada capaz de hacerlo, así que ningún caso de
// uso puede pedirlo aunque quiera. Colar la capacidad por el objeto compuesto
// sería devolverle por la ventana lo que la interfaz le niega por la puerta.
//
// Así que esto abre la capa de permisos a secas, hace la única cosa que tiene
// permitida, y cierra. Se llama SOLO desde `--uninstall-cleanup`.
func quitarCuarentenaDeBase(ctx context.Context, dataDir string, log port.Logger) error {
	permisos, err := netfw.New(dataDir, "", log)
	if err != nil {
		return fmt.Errorf("abriendo el firewall para quitar la cuarentena: %w", err)
	}
	defer func() { _ = permisos.Close() }()

	n, err := netfw.RemoveBaseQuarantineForUninstall(ctx, permisos)
	if err != nil {
		return fmt.Errorf("quitando la cuarentena de base: %w", err)
	}
	log.Info("cuarentena de base retirada", "cantidad", n)
	return nil
}

// prepararCarpetaDeLogDeLaUI crea la hoja del log de la ventana y le da
// escritura a los usuarios de la máquina.
//
// # Qué se concede, y qué NO
//
// **La ACL de la raíz no se toca**, y esa es la mitad del asunto: `api.token`
// sigue en solo lectura para los usuarios, que es media protección del token de
// la API local. Lo que se concede es escritura en una carpeta que no contiene
// otra cosa que el log de la interfaz. El log del daemon está en la carpeta de
// arriba y sigue sin poder tocarse, así que nadie puede forjar líneas en el
// registro que se pega en un reporte de fallo.
//
// Y no se corta la herencia, al revés que [protegerFichero]: SYSTEM y
// Administradores tienen que seguir pudiendo leer este archivo, que es el que
// alguien va a mandar cuando la ventana se muera.
//
// # Por qué lo hace el daemon y no el instalador
//
// Para que una instalación que se ACTUALIZA lo reciba sin reinstalar. El
// instalador crea el directorio de datos una vez, y esto tiene que existir en
// las máquinas donde Kanpachi ya estaba.
func prepararCarpetaDeLogDeLaUI(ruta string) error {
	if err := os.MkdirAll(ruta, 0o700); err != nil {
		return fmt.Errorf("creando %s: %w", ruta, err)
	}
	usuarios, err := windows.CreateWellKnownSid(windows.WinBuiltinUsersSid)
	if err != nil {
		return fmt.Errorf("resolviendo el SID de Usuarios: %w", err)
	}
	entradas := []windows.EXPLICIT_ACCESS{{
		AccessPermissions: windows.GENERIC_READ | windows.GENERIC_WRITE | windows.DELETE,
		AccessMode:        windows.GRANT_ACCESS,
		Inheritance:       windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_GROUP,
			TrusteeValue: windows.TrusteeValueFromSID(usuarios),
		},
	}}
	// El nil del segundo argumento es "sobre lo que ya hay", que es lo que hace
	// que esto SUME y no reemplace.
	dacl, err := windows.ACLFromEntries(entradas, nil)
	if err != nil {
		return fmt.Errorf("armando los permisos de %s: %w", ruta, err)
	}
	err = windows.SetNamedSecurityInfo(
		ruta,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION,
		nil, nil, dacl, nil,
	)
	if err != nil {
		return fmt.Errorf("aplicando los permisos de %s: %w", ruta, err)
	}
	return nil
}
