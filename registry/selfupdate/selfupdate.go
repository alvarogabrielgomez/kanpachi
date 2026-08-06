// Package selfupdate consulta los releases de GitHub y baja la carga del seed.
//
// Es la mitad "bajar y verificar" de `kanpseed upgrade`. La mitad "aplicar" vive
// en `registry/cli`, porque aplicar es tocar systemd y eso no es una descarga.
//
// **Baja el binario Y la página.** Son dos artefactos con vidas separadas a
// propósito (ver `install.sh`), así que una actualización que solo cambie el
// binario dejaría sirviendo un `index.html` de la versión anterior, y ese es
// justo el fallo que no se ve: la página se sirve a desconocidos y nadie mira
// su fecha.
package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
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

// Nombres de los artefactos del release, tal como los publica
// `.github/workflows/release-seed.yml`.
const (
	// SumsFile es el manifiesto de la carga de LINUX. El instalador de Windows
	// publica el suyo aparte: los dos workflows escriben en el mismo release, y
	// un nombre compartido significaba que el último en subir pisaba al otro.
	SumsFile = "SHA256SUMS-linux"
	PageFile = "index.html"
)

const apiLatest = "https://api.github.com/repos/" + Repo + "/releases/latest"

// BinaryFor es el nombre del binario publicado para una arquitectura.
func BinaryFor(arch string) string { return "kanpseed-linux-" + arch }

// Latest devuelve el tag del release publicado más reciente, por ejemplo
// "v0.1.0".
//
// Usa `releases/latest`, que es el mismo criterio que sigue `install.sh`: lo
// que instala una máquina nueva y lo que instala un `upgrade` tienen que ser la
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
	defer res.Body.Close()
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

// Bundle es una carga descargada Y VERIFICADA. Que exista este valor significa
// que los dos SHA256 dieron bien; no hay forma de construirlo sin eso.
type Bundle struct {
	Tag    string
	Binary []byte
	Page   []byte
}

// Download baja la carga de Linux del tag indicado y la verifica contra
// SHA256SUMS-linux.
//
// Se verifican los DOS archivos, no solo el binario, por el mismo motivo que en
// `install.sh`: la página se sirve a desconocidos y es igual de sustituible en
// tránsito que el ejecutable.
func Download(ctx context.Context, tag string, logf func(string, ...any)) (*Bundle, error) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	binario := BinaryFor(runtime.GOARCH)
	base := "https://github.com/" + Repo + "/releases/download/" + tag

	logf("bajando %s (%s)", binario, tag)
	bin, err := get(ctx, base+"/"+binario, 200<<20)
	if err != nil {
		return nil, err
	}
	pagina, err := get(ctx, base+"/"+PageFile, 8<<20)
	if err != nil {
		return nil, err
	}
	sumas, err := get(ctx, base+"/"+SumsFile, 1<<20)
	if err != nil {
		return nil, fmt.Errorf("no se pudo bajar %s: sin él no se verifica nada y no se instala nada: %w", SumsFile, err)
	}

	for _, a := range []struct {
		nombre string
		datos  []byte
	}{{binario, bin}, {PageFile, pagina}} {
		if err := verificar(string(sumas), a.nombre, a.datos); err != nil {
			return nil, err
		}
	}
	logf("SHA256 verificado")

	return &Bundle{Tag: tag, Binary: bin, Page: pagina}, nil
}

func verificar(sumas, nombre string, datos []byte) error {
	quiero := ChecksumFor(sumas, nombre)
	if quiero == "" {
		return fmt.Errorf("%s no menciona %s", SumsFile, nombre)
	}
	suma := sha256.Sum256(datos)
	tengo := hex.EncodeToString(suma[:])
	if tengo != quiero {
		return fmt.Errorf("el SHA256 de %s no coincide\n  esperado %s\n  obtenido %s\n"+
			"  no se instala nada: esto corre como servicio en un servidor público", nombre, quiero, tengo)
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

func get(ctx context.Context, url string, limite int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, res.Status)
	}
	return io.ReadAll(io.LimitReader(res.Body, limite))
}

// Timeout es el plazo total de un `upgrade`. Generoso porque incluye bajar un
// binario de decenas de MB por la red de un droplet.
const Timeout = 5 * time.Minute

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
