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

// puros son los directorios que NO pueden conocer el sistema operativo.
//
// Son relativos al directorio de este paquete. `go test` fija el directorio de
// trabajo al del paquete bajo prueba, así que las rutas son estables sin
// importar desde dónde se invoque.
//
// daemon/service está acá aunque viva bajo daemon/, y el motivo es el mismo que
// justifica la regla entera: el supervisor solo habla con puertos declarados en
// core, así que corre en el job de Linux junto a core. El día que alguien meta
// una llamada a Windows ahí dentro, ese job deja de correrlo y el bucle que
// hace vencer el contador de veinte minutos se queda sin pruebas.
var puros = []string{
	"../../core",
	"../../daemon/service",
}

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
	for _, dir := range puros {
		t.Run(dir, func(t *testing.T) { revisaPureza(t, dir) })
	}
}

func revisaPureza(t *testing.T, dir string) {
	t.Helper()
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("no se encuentra %s: %v", dir, err)
	}

	var revisados int
	fset := token.NewFileSet()

	err := filepath.WalkDir(dir, func(ruta string, d os.DirEntry, err error) error {
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
					"%s importa %q, que está prohibido acá\n"+
						"  esta capa no puede conocer el sistema operativo: si necesitas esto, "+
						"declara un puerto en core/port y ponlo en un adaptador dentro de daemon/adapter",
					ruta, ruteoImport)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("recorriendo %s: %v", dir, err)
	}

	// Sin esta comprobación el test pasaría feliz el día que alguien mueva un
	// paquete de sitio o rompa la ruta relativa, dando una garantía falsa.
	if revisados == 0 {
		t.Fatalf("no se revisó ningún archivo .go en %s: la ruta está mal y el test no está vigilando nada", dir)
	}
	t.Logf("%d archivos de %s revisados, sin imports prohibidos", revisados, dir)
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
