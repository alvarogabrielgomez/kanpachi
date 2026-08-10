//go:build linux

package steam

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// steamRoot busca dónde está instalado Steam.
//
// # La MISMA trampa que en Windows, con otro nombre
//
// En Windows el error es leer `HKEY_CURRENT_USER`: el daemon corre como SYSTEM,
// y para SYSTEM esa raíz es el perfil de SYSTEM y no el de quien está sentado
// delante. Acá el error idéntico es mirar `$HOME`: el daemon corre como root, y
// el `$HOME` de root es `/root`, donde no hay Steam, nunca lo va a haber, y la
// consulta no falla. Devuelve "no está", que es una respuesta creíble y falsa.
//
// Así que se miran los directorios de todos los usuarios de la máquina, que es
// el equivalente de leer `HKEY_LOCAL_MACHINE`.
//
// # Las cuatro formas en que Steam aterriza en Linux
//
// Las cuatro son reales y ninguna es rara: el paquete de siempre deja un enlace
// en `~/.steam/steam` y los datos en `~/.local/share/Steam`, el de Flatpak
// aterriza bajo `~/.var/app`, y el de Snap bajo `~/snap`. Mirar solo la primera
// deja fuera a media distribución moderna.
func steamRoot() (string, error) {
	relativas := []string{
		".steam/steam",
		".local/share/Steam",
		".var/app/com.valvesoftware.Steam/data/Steam",
		"snap/steam/common/.local/share/Steam",
	}

	for _, casa := range homeDirs() {
		for _, rel := range relativas {
			ruta := filepath.Join(casa, rel)
			// Que el directorio exista no prueba que sea una instalación de
			// Steam: `~/.steam` sobrevive a desinstalar el paquete. Lo que se
			// comprueba es la biblioteca, que es lo que este paquete va a leer.
			if _, err := os.Stat(filepath.Join(ruta, "steamapps")); err != nil {
				continue
			}
			return ruta, nil
		}
	}
	return "", fmt.Errorf("no hay una instalación de Steam legible en los directorios de los " +
		"usuarios de esta máquina")
}

// homeDirs son los directorios donde puede haber un Steam, en orden.
//
// # Por qué se lee /etc/passwd y no se usa os/user
//
// Porque la pregunta no es "¿cuál es mi casa?", que es lo que contesta esa
// librería, es "¿cuáles son TODAS?". Y el fichero es la respuesta directa: una
// línea por cuenta, con el directorio en el sexto campo.
//
// `SUDO_USER` va PRIMERO cuando está. Alguien que corre esto con `sudo` desde su
// sesión es el caso más probable de tener Steam, y probar su casa antes ahorra
// recorrer las demás.
//
// Las cuentas de sistema se saltan por UID. El corte en mil es la convención de
// Debian y de Ubuntu, que es a lo que apunta este producto, y sirve para lo que
// hace falta: no recorrer las cuarenta cuentas de servicio de un servidor
// buscando una biblioteca de juegos.
func homeDirs() []string {
	var out []string
	visto := map[string]bool{}
	agregar := func(d string) {
		if d == "" || visto[d] {
			return
		}
		visto[d] = true
		out = append(out, d)
	}

	if u := os.Getenv("SUDO_USER"); u != "" {
		agregar(filepath.Join("/home", u))
	}

	f, err := os.Open("/etc/passwd")
	if err != nil {
		// Sin el fichero queda lo que se haya podido deducir. No es un fallo de
		// esta función: quien la llama distingue "no hay Steam" de un error, y
		// no encontrarlo es una respuesta válida.
		return out
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		// nombre:x:uid:gid:comentario:casa:shell
		campos := strings.Split(sc.Text(), ":")
		if len(campos) < 6 {
			continue
		}
		uid, err := strconv.Atoi(campos[2])
		if err != nil || uid < 1000 || uid == 65534 {
			// 65534 es `nobody`, que tiene UID alto y no es una persona.
			continue
		}
		agregar(campos[5])
	}
	return out
}
