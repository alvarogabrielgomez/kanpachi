// Package selfupdate baja la carga DEL SEED y la verifica.
//
// Es la mitad "bajar y verificar" de `kanpseed upgrade`. La mitad "aplicar" vive
// en `registry/cli`, porque aplicar es tocar systemd y eso no es una descarga.
//
// # Qué queda acá y qué bajó a internal/selfupdate
//
// Acá queda lo que es DEL SEED: sus dos artefactos, el nombre de su manifiesto y
// el de su binario. Lo genérico —preguntar la última versión, comparar,
// verificar un SHA256 contra un manifiesto— vive en `internal/selfupdate`, que
// es lo que el cliente también usa. Antes estaba todo junto y el cliente habría
// tenido que copiarlo, con la comparación de versiones duplicada en dos sitios y
// arreglada en uno.
//
// **Baja el binario Y la página.** Son dos artefactos con vidas separadas a
// propósito (ver `install.sh`), así que una actualización que solo cambie el
// binario dejaría sirviendo un `index.html` de la versión anterior, y ese es
// justo el fallo que no se ve: la página se sirve a desconocidos y nadie mira su
// fecha.
package selfupdate

import (
	"context"
	"fmt"
	"runtime"

	compartido "github.com/accentiostudios/kanpachi/internal/selfupdate"
)

// Lo genérico se reexporta para no tocar a quien ya lo llamaba por acá. Son
// alias de verdad, no envoltorios: hay una sola implementación.
const (
	Repo    = compartido.Repo
	Timeout = compartido.Timeout
)

var (
	Latest      = compartido.Latest
	IsVersion   = compartido.IsVersion
	Compare     = compartido.Compare
	Outdated    = compartido.Outdated
	ChecksumFor = compartido.ChecksumFor
)

// Nombres de los artefactos del release, tal como los publica
// `.github/workflows/release-seed.yml`.
const (
	// SumsFile es el manifiesto de la carga del SEED. El cliente publica el suyo
	// aparte, y también el instalador de Windows: los tres workflows escriben en
	// el mismo release, y un nombre compartido significa que el último en subir
	// pisa a los otros.
	SumsFile = "SHA256SUMS-seed-linux"
	PageFile = "index.html"
)

// BinaryFor es el nombre del binario publicado para una arquitectura.
func BinaryFor(arch string) string { return "kanpseed-linux-" + arch }

// Bundle es una carga descargada Y VERIFICADA. Que exista este valor significa
// que los dos SHA256 dieron bien; no hay forma de construirlo sin eso.
type Bundle struct {
	Tag    string
	Binary []byte
	Page   []byte
}

// Download baja la carga de Linux del tag indicado y la verifica contra
// [SumsFile].
//
// Se verifican los DOS archivos, no solo el binario, por el mismo motivo que en
// `install.sh`: la página se sirve a desconocidos y es igual de sustituible en
// tránsito que el ejecutable.
func Download(ctx context.Context, tag string, logf func(string, ...any)) (*Bundle, error) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	binario := BinaryFor(runtime.GOARCH)
	base := compartido.Base(tag)

	logf("bajando %s (%s)", binario, tag)
	bin, err := compartido.Get(ctx, base+"/"+binario, 200<<20)
	if err != nil {
		return nil, err
	}
	pagina, err := compartido.Get(ctx, base+"/"+PageFile, 8<<20)
	if err != nil {
		return nil, err
	}
	sumas, err := compartido.Get(ctx, base+"/"+SumsFile, 1<<20)
	if err != nil {
		return nil, fmt.Errorf("no se pudo bajar %s: sin él no se verifica nada y no se "+
			"instala nada: %w", SumsFile, err)
	}

	for _, a := range []struct {
		nombre string
		datos  []byte
	}{{binario, bin}, {PageFile, pagina}} {
		if err := compartido.Verificar(string(sumas), SumsFile, a.nombre, a.datos); err != nil {
			return nil, fmt.Errorf("%w\n  esto corre como servicio en un servidor público", err)
		}
	}
	logf("SHA256 verificado")

	return &Bundle{Tag: tag, Binary: bin, Page: pagina}, nil
}
