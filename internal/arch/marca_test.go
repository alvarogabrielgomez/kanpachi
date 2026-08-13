package arch

import (
	"os"
	"path/filepath"
	"regexp"
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
			switch d.Name() {
			case ".git", "dist", "target", "build", "node_modules", ".dart_tool":
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
