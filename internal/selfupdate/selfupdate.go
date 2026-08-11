// Package selfupdate es la mitad de actualizarse que NO depende de qué producto
// se actualiza: preguntar qué versión hay, comparar, bajar y verificar.
//
// # Por qué está en internal/ y no junto a uno de los dos
//
// Porque son dos productos y esto lo necesitan los dos. El seed vive en
// `registry/` y el cliente en `daemon/`, y ninguno puede importar al otro: son
// ejecutables distintos que se instalan por separado y se publican en workflows
// distintos. Copiar la comparación de versiones en los dos sitios significa que
// el día que alguien arregle el orden de los precandidatos, lo arregla en uno.
//
// `internal/` a secas es importable por todo el módulo, así que sirve para los
// dos y no lo puede importar nadie de fuera, que es exactamente lo que hace
// falta.
//
// # Lo que NO está acá, y es la parte que importa
//
// **Cómo se instala.** Y no es un olvido: es la diferencia real entre los dos
// productos. El seed reemplaza su binario en sitio, porque lo puso un
// `install.sh` que copia ficheros y no hay base de datos de paquetes a la que
// contradecir. El cliente de Linux viene de un `.deb`, y sobrescribir un fichero
// que dpkg cree suyo deja esa base de datos mintiendo: el siguiente
// `apt upgrade` lo pisa sin decir nada. Cada uno aplica lo suyo.
package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Repo es el repositorio de GitHub que publica los releases.
//
// OJO: no coincide con la ruta del módulo (`accentiostudios/kanpachi`). El
// módulo se nombró así antes de que el repositorio existiera, y quien los
// confunda escribe una URL que devuelve 404.
const Repo = "alvarogabrielgomez/kanpachi"

const apiLatest = "https://api.github.com/repos/" + Repo + "/releases/latest"

// Timeout es el plazo total de un `upgrade`. Generoso porque incluye bajar
// decenas de MB por la red de un servidor cualquiera.
const Timeout = 5 * time.Minute

// Base es la URL de donde cuelgan los artefactos de un tag.
func Base(tag string) string {
	return "https://github.com/" + Repo + "/releases/download/" + tag
}

// Latest devuelve el tag del release publicado más reciente, por ejemplo
// "v0.1.0".
//
// Usa `releases/latest`, que es el mismo criterio que sigue `install.sh`: lo que
// instala una máquina nueva y lo que instala un `upgrade` tienen que ser la
// misma cosa, o "actualizado" dejaría de significar nada.
func Latest(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiLatest, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub respondió %s", res.Status)
	}

	var cuerpo struct {
		Tag string `json:"tag_name"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&cuerpo); err != nil {
		return "", err
	}
	if cuerpo.Tag == "" {
		return "", fmt.Errorf("la respuesta de GitHub no traía tag_name")
	}
	return cuerpo.Tag, nil
}

// Get baja una URL con un tope de tamaño.
func Get(ctx context.Context, url string, limite int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, res.Status)
	}
	return io.ReadAll(io.LimitReader(res.Body, limite))
}

// Verificar compara lo bajado contra el manifiesto, y ese es su único trabajo.
//
// # Por qué el nombre del manifiesto es un parámetro
//
// Porque hay DOS manifiestos en la misma release y confundirlos es un fallo
// real: el del seed y el del cliente. Con un nombre compilado dentro, el seed
// pedía `SHA256SUMS-linux` y recibía el del cliente, no encontraba su propio
// nombre dentro, y cortaba. Cortar es lo correcto —falla del lado seguro, se
// niega a instalar en vez de instalar lo que no es— y aun así el mensaje tiene
// que poder nombrar el fichero que sí miró.
func Verificar(sumas, manifiesto, nombre string, datos []byte) error {
	quiero := ChecksumFor(sumas, nombre)
	if quiero == "" {
		return fmt.Errorf("%s no menciona %s", manifiesto, nombre)
	}
	suma := sha256.Sum256(datos)
	tengo := hex.EncodeToString(suma[:])
	if tengo != quiero {
		return fmt.Errorf("el SHA256 de %s no coincide\n  esperado %s\n  obtenido %s\n"+
			"  no se instala nada", nombre, quiero, tengo)
	}
	return nil
}

// ChecksumFor busca el hash de un archivo en un manifiesto de `sha256sum`.
//
// Tolera el asterisco del modo binario (`<hash> *nombre`), que es lo que emite
// `sha256sum -b` y lo que rompería una comparación literal del segundo campo.
func ChecksumFor(sumas, nombre string) string {
	for _, linea := range strings.Split(sumas, "\n") {
		campos := strings.Fields(linea)
		if len(campos) != 2 {
			continue
		}
		if strings.TrimPrefix(campos[1], "*") == nombre {
			return campos[0]
		}
	}
	return ""
}

// --- Comparación de versiones -----------------------------------------------
//
// Se implementa aquí en vez de traer `golang.org/x/mod/semver`. Son treinta
// líneas contra una dependencia entera, y las versiones que compara este
// proyecto salen de sus propios tags: `vX.Y.Z`, con un sufijo de precandidato
// como mucho.

type version struct {
	partes [3]int
	pre    string
}

func parse(s string) (version, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")
	if s == "" {
		return version{}, false
	}
	// El sufijo de build (`+algo`) no ordena, así que se descarta antes que nada.
	if i := strings.IndexByte(s, '+'); i >= 0 {
		s = s[:i]
	}
	var v version
	if i := strings.IndexByte(s, '-'); i >= 0 {
		v.pre = s[i+1:]
		s = s[:i]
		if v.pre == "" {
			return version{}, false
		}
	}
	campos := strings.Split(s, ".")
	if len(campos) != 3 {
		return version{}, false
	}
	for i, c := range campos {
		n, err := strconv.Atoi(c)
		if err != nil || n < 0 {
			return version{}, false
		}
		v.partes[i] = n
	}
	return v, true
}

// IsVersion informa si s es una versión de release de verdad. Un binario
// compilado a mano lleva "dev", y eso no es una versión.
func IsVersion(s string) bool {
	_, ok := parse(s)
	return ok
}

// Compare devuelve -1, 0 o +1 comparando dos versiones.
//
// Una versión CON precandidato es anterior a la misma sin él: v0.2.0-rc1 <
// v0.2.0. Sin esa regla, un rc1 se quedaría instalado para siempre porque el
// final "no es más nuevo".
func Compare(a, b string) int {
	va, oka := parse(a)
	vb, okb := parse(b)
	switch {
	case !oka && !okb:
		return 0
	case !oka:
		return -1
	case !okb:
		return 1
	}
	for i := range va.partes {
		if va.partes[i] != vb.partes[i] {
			if va.partes[i] < vb.partes[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case va.pre == vb.pre:
		return 0
	case va.pre == "":
		return 1
	case vb.pre == "":
		return -1
	case va.pre < vb.pre:
		return -1
	default:
		return 1
	}
}

// Outdated informa si latest es estrictamente más nueva que current.
//
// Un binario "dev" NUNCA se declara desactualizado: quien compiló a mano suele
// tener algo más nuevo que el último release, y empujarlo a "actualizar" le
// borraría su propio build.
func Outdated(current, latest string) bool {
	if !IsVersion(current) || !IsVersion(latest) {
		return false
	}
	return Compare(current, latest) < 0
}
