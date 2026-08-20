package arch

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/accentiostudios/kanpachi/internal/brand"
)

// El canal de actualización vive en UN sitio por lenguaje, y estos dos tests
// son lo que impide que vuelva a estar en cinco.
//
// # Qué pasó, que es de dónde salen
//
// El nombre del repositorio estaba escrito cuatro veces: la constante de Go, el
// JavaScript de la página, el `Documentation=` de las dos units de systemd, y el
// `install.sh`. Un fork que editara la constante de Go seguía publicando una
// página y unas units que apuntaban al repositorio original, y nada fallaba: las
// URL resuelven perfectamente, solo que al proyecto de otro.

// marcaDart es dónde lo espeja la interfaz, que no puede importar Go.
const marcaDart = "../../ui/lib/core/brand.dart"

// nombraElRepo casa con el repositorio y NO con `…/kanpachi-engine`, que es otro
// repositorio, el del motor, y nombrarlo es correcto en todas partes.
var nombraElRepo = regexp.MustCompile(regexp.QuoteMeta(brand.Repo) + `([^-\w]|$)`)

// dóndeSePuedeNombrar son los sitios donde la cadena del repositorio SÍ puede
// aparecer, cada uno con su motivo.
var dóndeSePuedeNombrar = map[string]string{
	// La fuente.
	filepath.FromSlash("internal/brand/brand.go"): "es la constante",
	// El espejo de Dart, atado por el lockstep de abajo.
	filepath.FromSlash("ui/lib/core/brand.dart"): "es el espejo de Dart, con lockstep",
	// Se descarga por una URL que CONTIENE el repositorio, así que quien lo
	// bajó ya eligió cuál. No hay forma de que se entere de otro modo.
	filepath.FromSlash("seed/install.sh"): "se baja por una URL que ya lo lleva dentro",

	// El mismo caso que `install.sh`, y por el mismo motivo: un YAML que
	// alguien copia no tiene forma de saber de qué repositorio salió, así que
	// el nombre de la imagen tiene que estar escrito. La diferencia con
	// `install.sh` es que acá son cinco ficheros, y por eso están enumerados
	// uno por uno en vez de con un prefijo: un fichero nuevo bajo `docker/`
	// que nombre el repositorio tiene que aparecer en esta lista a mano, o
	// sea que agregarlo es una decisión y no un descuido.
	filepath.FromSlash("docker/Dockerfile"):                          "arma la imagen, y se construye desde un clon que ya eligió cuál",
	filepath.FromSlash("docker/templates/compose.yml"):               "nombra la imagen publicada, y un YAML no puede deducirla",
	filepath.FromSlash("docker/templates/compose.sidecar.yml"):       "nombra la imagen publicada, y un YAML no puede deducirla",
	filepath.FromSlash("docker/templates/compose.custom-ports.yml"):  "nombra la imagen publicada, y un YAML no puede deducirla",
	filepath.FromSlash("docker/templates/compose.seed-password.yml"): "nombra la imagen publicada, y un YAML no puede deducirla",
}

// TestElRepositorioEstaEnUnSoloSitioPorLenguaje.
func TestElRepositorioEstaEnUnSoloSitioPorLenguaje(t *testing.T) {
	// Extensiones que se revisan. El resto del árbol lleva binarios, iconos y
	// ficheros generados, donde una coincidencia no significaría nada.
	revisables := map[string]bool{
		".go": true, ".dart": true, ".html": true, ".sh": true,
		".yml": true, ".yaml": true, ".iss": true, ".ps1": true, ".toml": true,
	}

	err := filepath.WalkDir("../..", func(ruta string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel("../..", ruta)
		if d.IsDir() {
			// `dist` es artefacto y está en .gitignore; `target` y `build` son
			// de Rust y Flutter. Lo que hay dentro es el RESULTADO de compilar
			// estos ficheros, así que revisarlo sería marcar dos veces lo mismo.
			// `scratch` es lo desechable, también en .gitignore y tampoco se
			// publica: sin esto, un compose de prueba nombrando el repo dejaba
			// rojo el `go test ./...` de quien lo tenga en el disco. El otro
			// recorrido de este mismo fichero ya lo saltaba.
			switch d.Name() {
			case ".git", "dist", "target", "build", "node_modules", ".dart_tool", "scratch":
				return filepath.SkipDir
			}
			return nil
		}
		if !revisables[strings.ToLower(filepath.Ext(ruta))] {
			return nil
		}
		if _, vale := dóndeSePuedeNombrar[rel]; vale {
			return nil
		}
		// Los propios guardianes lo nombran para poder compararlo.
		if strings.HasSuffix(ruta, "_test.go") {
			return nil
		}
		crudo, err := os.ReadFile(ruta)
		if err != nil {
			return err
		}
		// Sin guion detrás, para que `alvarogabrielgomez/kanpachi-engine` no
		// cuente: es OTRO repositorio, el del motor, y nombrarlo es correcto.
		if nombraElRepo.Match(crudo) {
			t.Errorf("%s lleva %q escrito.\n"+
				"  El canal de actualización vive en internal/brand, y las caras que no pueden\n"+
				"  importarlo lo reciben: la página por el hueco de estado del servidor, y la\n"+
				"  interfaz por su espejo con lockstep. Copiarlo acá deja un fork publicando\n"+
				"  algo que apunta al repositorio original.", rel, brand.Repo)
		}
		// Y la API, que es de dónde sale la respuesta. Solo `selfupdate` la
		// arma, para que el interruptor de [brand.UpdatesEnabled] no se pueda
		// esquivar con una petición escrita en otro sitio.
		// Con esquema, que es lo que hace falta para pedir de verdad. Sin él, la
		// comprobación cazaba los comentarios que EXPLICAN por qué la página no
		// le pregunta a GitHub, o sea justo la prosa que documenta la regla.
		if strings.Contains(string(crudo), "https://api.github.com") &&
			!strings.HasPrefix(rel, filepath.FromSlash("internal/selfupdate")) &&
			rel != filepath.FromSlash("ui/lib/core/brand.dart") {
			t.Errorf("%s nombra api.github.com: la consulta de versiones vive en "+
				"internal/selfupdate, que es donde se mira el interruptor", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("recorriendo el árbol: %v", err)
	}
}

// TestLaMarcaDeDartCoincideConLaDeGo es el lockstep de las dos copias.
//
// Dart no puede importar una constante de Go, así que hay dos, y un comentario
// de "mantener en sync" no es un candado: es una nota que alguien lee después de
// haber roto la cosa. Este test compara los valores.
func TestLaMarcaDeDartCoincideConLaDeGo(t *testing.T) {
	crudo, err := os.ReadFile(marcaDart)
	if err != nil {
		t.Fatalf("leyendo %s: %v", marcaDart, err)
	}
	dart := string(crudo)

	repo := regexp.MustCompile(`static const String repo = '([^']*)'`).FindStringSubmatch(dart)
	if repo == nil {
		t.Fatalf("%s no declara `static const String repo`", marcaDart)
	}
	if repo[1] != brand.Repo {
		t.Errorf("el repositorio no coincide:\n  Go   %q\n  Dart %q", brand.Repo, repo[1])
	}

	activo := regexp.MustCompile(`static const bool updatesEnabled = (true|false)`).FindStringSubmatch(dart)
	if activo == nil {
		t.Fatalf("%s no declara `static const bool updatesEnabled`", marcaDart)
	}
	// Comparado como texto, que es lo que hay del otro lado. Un `false` en un
	// solo lenguaje deja la mitad del producto preguntando y la otra callada,
	// que es peor que las dos preguntando.
	quiero := "false"
	if brand.UpdatesEnabled {
		quiero = "true"
	}
	if activo[1] != quiero {
		t.Errorf("el interruptor de actualizaciones no coincide:\n  Go   %s\n  Dart %s",
			quiero, activo[1])
	}
}

// forkRef casa con cualquier referencia al fork de EasyTier: el tag de hoy y las
// formas versionadas que tuvo antes.
//
// Va por PATRÓN y no por la cadena de hoy a propósito. Un guardián que hubiera
// que editar cada vez que la referencia cambia es un guardián que se queda viejo
// junto con lo que vigila, que es exactamente el fallo que viene a impedir.
var forkRef = regexp.MustCompile(`v\d+\.\d+\.\d+-kanpachi[\w.]*`)

// canonicalDoc es el único documento que puede nombrarla.
const canonicalDoc = "02-decisiones-de-diseno.md"

// TestTheForkRefIsNamedInOneDocumentOnly.
//
// # Qué pasó, que es de dónde sale
//
// La referencia al fork estaba escrita en CINCO documentos, y dos de ellos
// decían `v2.6.4-kanpachi.1` cuando el motor ya compilaba contra `.2`. Nadie lo
// nota leyendo: las dos cadenas son plausibles, ninguna herramienta las resuelve
// y no fallan en ningún sitio, porque son prosa.
//
// Los documentos no se generan, así que no hay constante que interpolar acá. El
// equivalente documental de una constante es UN sitio canónico y el resto
// apuntando a él, y esto es lo único que lo sostiene.
func TestTheForkRefIsNamedInOneDocumentOnly(t *testing.T) {
	const dir = "../../docs"
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("leyendo %s: %v", dir, err)
	}

	var seen int
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("leyendo %s: %v", e.Name(), err)
		}
		found := forkRef.FindAllString(string(raw), -1)
		if len(found) == 0 {
			continue
		}
		if e.Name() == canonicalDoc {
			seen += len(found)
			continue
		}
		t.Errorf("docs/%s nombra la referencia del fork %q.\n"+
			"  Va en docs/%s y en ningún otro documento. Escrita en cinco sitios se\n"+
			"  desincronizó, y dos decían `.1` con el motor compilando contra `.2`.\n"+
			"  Apunta a la decisión 1 en vez de repetir el valor",
			e.Name(), found, canonicalDoc)
	}

	// El candado del candado. Sin esto, borrar el párrafo canónico deja el test
	// en verde y a los docs sin ningún sitio que diga contra qué se compila.
	if seen == 0 {
		t.Errorf("docs/%s no nombra ninguna referencia del fork.\n"+
			"  Es el sitio canónico: si dejó de decirlo, no queda ninguno que lo diga",
			canonicalDoc)
	}
}

// laAPIDeGitHub casa con la URL que se PIDE, y no con nombrarla.
//
// El esquema es el discriminador, y es honesto: tres comentarios de este
// repositorio explican por qué NO se llama a `api.github.com`, y un guardián que
// los marcara enseñaría a borrar las explicaciones para poner el test en verde.
// Lo que importa es una URL que alguien va a buscar.
var laAPIDeGitHub = regexp.MustCompile(`https://api\.github\.com`)

// dóndeSePuedePedir son los dos ficheros de marca, uno por lenguaje.
//
// Dos y no uno porque un lenguaje no puede importar la constante del otro. Que
// sean EXACTAMENTE los mismos que ya guarda el test del repositorio no es
// casualidad: quien publica y a quién se le pregunta por la versión son la misma
// decisión, y repartirla en dos sitios es cómo un fork termina preguntándole al
// repositorio de otro.
var dóndeSePuedePedir = map[string]string{
	filepath.FromSlash("internal/selfupdate/selfupdate.go"): "es la constante de Go",
	filepath.FromSlash("ui/lib/core/brand.dart"):            "es el espejo de Dart",
}

// TestTheGitHubAPIIsCalledFromOnePlacePerLanguage.
//
// # Por qué existe
//
// El canal de actualización pasó de colgar del seed a preguntarle a GitHub, y
// esa consulta manda la IP de la máquina a un tercero y gasta una cuota de
// sesenta por hora que se comparte con toda la red de esa persona. Una segunda
// copia de la URL es un segundo sitio desde el que eso puede empezar a pasar sin
// que nadie lo lea, y la página es donde más caro sale, porque ahí la cuota es
// la del visitante.
func TestTheGitHubAPIIsCalledFromOnePlacePerLanguage(t *testing.T) {
	checkable := map[string]bool{
		".go": true, ".dart": true, ".html": true, ".sh": true,
		".yml": true, ".yaml": true, ".iss": true, ".ps1": true, ".toml": true,
	}

	var seen int
	err := filepath.WalkDir("../..", func(ruta string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel("../..", ruta)
		if d.IsDir() {
			switch d.Name() {
			case ".git", "dist", "target", "build", "node_modules", ".dart_tool", "scratch":
				return filepath.SkipDir
			}
			return nil
		}
		if !checkable[strings.ToLower(filepath.Ext(ruta))] || strings.HasSuffix(ruta, "_test.go") {
			return nil
		}
		raw, err := os.ReadFile(ruta)
		if err != nil {
			return err
		}
		if !laAPIDeGitHub.Match(raw) {
			return nil
		}
		if _, vale := dóndeSePuedePedir[rel]; vale {
			seen++
			return nil
		}
		t.Errorf("%s pide https://api.github.com.\n"+
			"  Va en %s y en ningún otro sitio: una segunda copia es un segundo sitio\n"+
			"  desde el que se le manda la IP de alguien a un tercero sin que nadie lo lea",
			rel, sitios())
		return nil
	})
	if err != nil {
		t.Fatalf("recorriendo el árbol: %v", err)
	}

	// El candado del candado, por lenguaje: si uno de los dos deja de pedirla,
	// esa mitad del producto se quedó sin comprobar si hay versión nueva.
	if seen != len(dóndeSePuedePedir) {
		t.Errorf("solo %d de %d ficheros de marca piden la API de GitHub.\n"+
			"  Los dos tienen que hacerlo: si uno deja de preguntar, esa cara del\n"+
			"  producto se queda callada mientras la otra avisa", seen, len(dóndeSePuedePedir))
	}
}

func sitios() string {
	var nombres []string
	for ruta := range dóndeSePuedePedir {
		nombres = append(nombres, filepath.ToSlash(ruta))
	}
	sort.Strings(nombres)
	return strings.Join(nombres, " y ")
}
