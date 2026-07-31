// Package arch contiene los tests que vigilan la arquitectura.
//
// Vive fuera de core/ a propósito. El test necesita recorrer el disco, o sea
// necesita os y path/filepath, y ponerlo dentro de core lo obligaría a
// saltarse a sí mismo con una excepción. Una regla con una excepción tallada
// para su propio guardián es una regla más débil.
package arch

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// coreDir es relativo al directorio de este paquete. `go test` fija el
// directorio de trabajo al del paquete bajo prueba, así que la ruta es estable
// sin importar desde dónde se invoque.
const coreDir = "../../core"

// prohibidos son los imports que no pueden aparecer en core.
//
// La lista sale de docs/CLAUDE.md. No es estética: si core toca cualquiera de
// estos, deja de correr en CI sin privilegios y sin Windows, y esa es la
// métrica que define si la arquitectura sigue sana.
var prohibidos = []string{
	"os",
	"os/exec",
	"syscall",
	"golang.org/x/sys",
	"net/http",
}

// permitidosConPrefijo evita falsos positivos por prefijo compartido.
//
// Sin esto, "os" haría match con "os/user" por accidente en una comparación
// laxa, y peor, un import legítimo como "golang.org/x/crypto/argon2" quedaría
// atrapado por "golang.org/x/sys" si alguien escribiera la comparación con
// HasPrefix sobre la raíz equivocada.
func estaProhibido(ruta string) bool {
	for _, p := range prohibidos {
		if ruta == p || strings.HasPrefix(ruta, p+"/") {
			return true
		}
	}
	return false
}

// TestCoreNoTieneDependenciasSucias es la regla de arquitectura convertida en
// algo que no se puede ignorar sin querer.
//
// Vale más que cualquier párrafo de documentación: un documento se lee una vez
// y se olvida, este test falla en el momento exacto en que alguien mete el
// sistema operativo dentro del dominio.
func TestCoreNoTieneDependenciasSucias(t *testing.T) {
	if _, err := os.Stat(coreDir); err != nil {
		t.Fatalf("no se encuentra %s: %v", coreDir, err)
	}

	var revisados int
	fset := token.NewFileSet()

	err := filepath.WalkDir(coreDir, func(ruta string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(ruta, ".go") {
			return nil
		}
		// Los archivos de test se saltan: no se compilan dentro del binario
		// que se distribuye, y un test puede necesitar os para armar su
		// escenario sin que eso ensucie el dominio.
		if strings.HasSuffix(ruta, "_test.go") {
			return nil
		}

		archivo, err := parser.ParseFile(fset, ruta, nil, parser.ImportsOnly)
		if err != nil {
			t.Errorf("no se pudo parsear %s: %v", ruta, err)
			return nil
		}
		revisados++

		for _, imp := range archivo.Imports {
			ruteoImport := strings.Trim(imp.Path.Value, `"`)
			if estaProhibido(ruteoImport) {
				t.Errorf(
					"%s importa %q, que está prohibido en core\n"+
						"  core no puede conocer el sistema operativo: si necesitas esto, "+
						"declara un puerto en core/port y ponlo en un adaptador dentro de daemon/",
					ruta, ruteoImport)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("recorriendo %s: %v", coreDir, err)
	}

	// Sin esta comprobación el test pasaría feliz el día que alguien mueva
	// core de sitio o rompa la ruta relativa, dando una garantía falsa.
	if revisados == 0 {
		t.Fatalf("no se revisó ningún archivo .go en %s: la ruta está mal y el test no está vigilando nada", coreDir)
	}
	t.Logf("%d archivos de core revisados, sin imports prohibidos", revisados)
}

// TestElTestDePurezaDetectaUnaViolacion comprueba el detector contra un caso
// conocido, para que un error en la propia lógica de detección no deje pasar
// todo en silencio. Un guardián que nunca se probó no es un guardián.
func TestElTestDePurezaDetectaUnaViolacion(t *testing.T) {
	casos := []struct {
		ruta   string
		quiero bool
	}{
		{"os", true},
		{"os/exec", true},
		{"syscall", true},
		{"golang.org/x/sys/windows", true},
		{"net/http", true},
		{"strings", false},
		{"errors", false},
		{"net/netip", false},
		{"golang.org/x/crypto/argon2", false},
		{"ostrich", false}, // empieza con "os", no debe hacer match
		{"net/httptest", false},
	}
	for _, c := range casos {
		if got := estaProhibido(c.ruta); got != c.quiero {
			t.Errorf("estaProhibido(%q) = %v, se esperaba %v", c.ruta, got, c.quiero)
		}
	}
}
