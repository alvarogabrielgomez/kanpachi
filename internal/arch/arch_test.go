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
// Dos viven bajo daemon/ y están acá por el mismo motivo que justifica la regla
// entera. El supervisor solo habla con puertos declarados en core, y el
// protocolo se define APARTE de su transporte, así que los dos son Go puro por
// construcción y corren en el job de Linux junto a core.
//
// El día que alguien meta una llamada a Windows en el supervisor, ese job deja
// de correrlo y el bucle que hace vencer el contador de veinte minutos se queda
// sin pruebas. El día que la meta en el protocolo, la API local deja de poder
// reusarse sobre un socket Unix, que es la única razón por la que está
// separada del pipe. Y el día que la meta en el canal de la sala, el código que
// más revisión merece del proyecto, el que corre como SYSTEM parseando mensajes
// de gente de la sala, se queda sin sus tests en CI.
//
// `firewall/gate` es el cuarto de daemon/ y el más reciente. Ahí se decide qué
// bloquea y qué abre la compuerta, y lo usan LOS DOS sistemas: WFP en Windows y
// nftables en Linux. Una llamada a un sistema concreto ahí dentro convierte la
// decisión compartida en la decisión de uno solo, y el otro se entera cuando ya
// no compila.
var puros = []string{
	"../../core",
	"../../daemon/service",
	"../../daemon/transport/protocol",
	"../../daemon/transport/wire",
	"../../daemon/transport/control",
	"../../daemon/adapter/firewall/gate",
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

// adaptadoresPartidos son los que separan lo que DECIDE de lo que llama al
// sistema, con el archivo sin etiqueta de compilación como mitad pura.
//
// Es el corte que hace que las decisiones caras se prueben en el job de Linux.
// En `windows/wfp` lo que se decide sin etiqueta es cómo se traduce una
// condición y cómo se deriva la clave de una posición; en `windows/netfw`, qué
// dice cada regla; en `netcfg`, qué rutas se ponen y cuál se borra; en `routes`,
// qué prefijos se descartan al elegir la subred de la sala. Ninguna de esas
// necesita Windows para comprobarse, y el día que la mitad pura importe Windows
// dejan de comprobarse todas a la vez, en silencio: el paquete sigue compilando
// en la máquina donde se programa.
//
// `firewall/gate` NO está acá y es a propósito: no tiene mitad de Windows, es
// puro entero, así que lo vigila la lista de directorios puros. La distinción
// importa porque en `gate` un fichero `_windows.go` sería el fallo, y en
// `windows/wfp` es lo normal.
var adaptadoresPartidos = []string{
	"../../daemon/adapter/firewall",
	"../../daemon/adapter/firewall/windows/wfp",
	"../../daemon/adapter/firewall/windows/netfw",
	"../../daemon/adapter/netcfg",
	"../../daemon/adapter/routes",
	"../../daemon/adapter/engine/kanpachi",
}

// deWindows son los imports que atan un archivo a Windows.
//
// Es una lista MÁS CORTA que `prohibidos` a propósito: la mitad pura de un
// adaptador sí puede leer y escribir ficheros, que es lo que hace el libro de
// ajustes. Lo que no puede es hablar con Windows, porque eso es exactamente lo
// que decide si el job de Linux la puede correr.
var deWindows = []string{
	"syscall",
	"os/exec",
	"golang.org/x/sys",
	"github.com/go-ole/go-ole",
}

// TestLaMitadPuraDeLosAdaptadoresNoConoceWindows.
//
// Se mira por ARCHIVO y no por paquete, que es lo que distingue este guardián
// del de pureza de core: acá el paquete entero SÍ conoce Windows, y lo que se
// vigila es que la frontera esté donde dice estar. Un archivo sin sufijo
// `_windows` es la mitad que el CI de Linux compila y prueba.
func TestLaMitadPuraDeLosAdaptadoresNoConoceWindows(t *testing.T) {
	for _, dir := range adaptadoresPartidos {
		t.Run(dir, func(t *testing.T) {
			revisados := 0
			fset := token.NewFileSet()
			entradas, err := os.ReadDir(dir)
			if err != nil {
				t.Fatalf("no se encuentra %s: %v", dir, err)
			}
			for _, e := range entradas {
				nombre := e.Name()
				if e.IsDir() || !strings.HasSuffix(nombre, ".go") ||
					strings.HasSuffix(nombre, "_test.go") ||
					strings.HasSuffix(nombre, "_windows.go") ||
					strings.HasSuffix(nombre, "_other.go") {
					continue
				}
				ruta := filepath.Join(dir, nombre)
				archivo, err := parser.ParseFile(fset, ruta, nil, parser.ImportsOnly)
				if err != nil {
					t.Errorf("no se pudo parsear %s: %v", ruta, err)
					continue
				}
				revisados++
				for _, imp := range archivo.Imports {
					ruteo := strings.Trim(imp.Path.Value, `"`)
					for _, p := range deWindows {
						if ruteo == p || strings.HasPrefix(ruteo, p+"/") {
							t.Errorf("%s importa %q.\n"+
								"  Es la mitad PURA de un adaptador partido, o sea la que el job de Linux\n"+
								"  compila y prueba. Con este import deja de correr ahí, y lo que se queda\n"+
								"  sin pruebas son las decisiones caras del adaptador, en silencio: el\n"+
								"  paquete sigue compilando en la máquina donde se programa.\n"+
								"  Lo que habla con Windows va en un archivo _windows.go.", ruta, ruteo)
						}
					}
				}
			}
			// Sin esto, renombrar los archivos dejaría el guardián mirando un
			// conjunto vacío y pasando en verde para siempre.
			if revisados == 0 {
				t.Fatalf("no se revisó ni un archivo puro en %s: o el paquete dejó de estar "+
					"partido, o este guardián dejó de mirar donde vive", dir)
			}
		})
	}
}
