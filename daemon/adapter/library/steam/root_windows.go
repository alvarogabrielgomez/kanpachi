//go:build windows

package steam

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows/registry"
)

// steamRoot busca dónde está instalado Steam.
//
// # Por qué NO se mira HKEY_CURRENT_USER
//
// Es la trampa entera de este archivo, y no se ve leyendo tutoriales: casi todo
// el mundo resuelve esto con `HKCU\Software\Valve\Steam\SteamPath`, que es la
// clave que Steam escribe y la más fiable... **para el proceso del usuario**.
// El daemon corre como SYSTEM, y para SYSTEM `HKEY_CURRENT_USER` es el perfil
// de SYSTEM, no el de quien está sentado delante. Ahí no hay Steam, nunca lo
// habrá, y la consulta no falla: devuelve "no está", que es una respuesta
// creíble y falsa.
//
// Así que se lee `HKEY_LOCAL_MACHINE`, que es de la máquina y por tanto igual
// para todos los procesos. Las dos vistas, porque Steam es de 32 bits y en un
// Windows de 64 aterriza bajo `WOW6432Node`; se prueban las dos por si alguna
// vez deja de serlo.
func steamRoot() (string, error) {
	claves := []struct {
		ruta   string
		acceso uint32
	}{
		{`SOFTWARE\Valve\Steam`, registry.QUERY_VALUE | registry.WOW64_32KEY},
		{`SOFTWARE\Valve\Steam`, registry.QUERY_VALUE | registry.WOW64_64KEY},
		{`SOFTWARE\WOW6432Node\Valve\Steam`, registry.QUERY_VALUE},
	}

	for _, c := range claves {
		k, err := registry.OpenKey(registry.LOCAL_MACHINE, c.ruta, c.acceso)
		if err != nil {
			continue
		}
		ruta, _, err := k.GetStringValue("InstallPath")
		_ = k.Close()
		if err != nil || ruta == "" {
			continue
		}
		// Que la clave exista no prueba que el directorio esté: desinstalar
		// Steam deja restos en el registro con más frecuencia de la que
		// gustaría. Se comprueba, así el que llama distingue "no hay Steam" de
		// "hay Steam y no se pudo leer".
		if _, err := os.Stat(ruta); err != nil {
			continue
		}
		return ruta, nil
	}
	return "", fmt.Errorf("no hay InstallPath de Steam legible en HKLM")
}
