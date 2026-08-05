package arch

import (
	"go/ast"
	"path/filepath"
	"strings"
	"testing"
)

// El guardián del CABLEADO.
//
// # El fallo que existe para cazar, y que pasó DOS veces
//
// `control.Attach` y `firewall.SetScope` estaban escritos, probados y **sin
// llamar desde producción**. Los dos son métodos de unión: existen para juntar
// dos piezas que se construyen por separado, y sin la llamada las dos piezas
// funcionan, sus tests pasan, y el producto no.
//
// El de `Attach` costó una sesión entera de diagnóstico: `Serve` devolvía
// `ErrNotAttached` y crear una sala fallaba entera con el motor ya levantado. El
// propio comentario del método decía *"El cableado construye uno, después el
// otro, y los une"*, y el cableado no hacía la última parte.
//
// **Un test de paquete no lo puede ver, porque el test SÍ los llama.** Lo que lo
// ve es correr el producto, y eso no está automatizado ni lo va a estar del
// todo. Esto es lo que sí se puede automatizar: exigir que la llamada exista.
//
// # Qué cuenta como método de unión
//
// Los verbos de abajo, y son pocos a propósito. No es un barrido de "todo método
// exportado tiene que llamarse", que daría falsos positivos por todos lados: es
// la forma concreta que tuvieron los dos fallos, un método que no hace trabajo
// del producto sino que ENLAZA, y que por eso nadie echa de menos hasta que algo
// devuelve un error raro a mitad de camino.
var verbosDeUnión = []string{"Attach", "Bind", "Unbind", "SetScope"}

// sufijoDeMedición es la única forma de saltarse la regla, y se declara en el
// NOMBRE del método.
//
// `SetScopeForMeasurement` existe solo para `internal/fwprobe`, que mide la
// compuerta sobre un adaptador físico elegido a mano. Es legítimo que producción
// no lo llame, y decirlo en el nombre es lo que evita una lista de excepciones
// en este fichero, que es justo donde una excepción deja de leerse.
const sufijoDeMedición = "ForMeasurement"

// llamanDesdeProducción son los árboles donde una llamada CUENTA.
//
// `internal/` queda FUERA a propósito, y es la mitad importante de este
// guardián: `SetScope` sí se llamaba, desde `internal/fwprobe`, y eso era
// exactamente el estado del fallo. Una herramienta de medición que ejercita un
// método no prueba que el producto lo use.
var llamanDesdeProducción = []string{"../../core", "../../daemon"}

// dondeViveElCableado son los árboles donde se declaran los métodos de unión.
var dondeViveElCableado = []string{"../../daemon"}

func TestTodoMétodoDeUniónSeLlamaDesdeProducción(t *testing.T) {
	declarados := métodosDeUnión(t)
	if len(declarados) == 0 {
		t.Fatal("no se encontró ni un método de unión, así que este test no probaría nada.\n" +
			"  O se renombraron todos, o el barrido dejó de mirar donde viven.")
	}

	llamados := map[string]bool{}
	for _, raíz := range llamanDesdeProducción {
		porArchivo(t, raíz, func(ruta string, archivo *ast.File) {
			if strings.HasSuffix(ruta, "_test.go") {
				// Un test que llama al método es exactamente lo que hacía que el
				// fallo pasara inadvertido. No cuenta.
				return
			}
			ast.Inspect(archivo, func(n ast.Node) bool {
				if llamada, ok := n.(*ast.CallExpr); ok {
					llamados[nombreLlamado(llamada.Fun)] = true
				}
				return true
			})
		})
	}

	for nombre, dónde := range declarados {
		if llamados[nombre] {
			continue
		}
		t.Errorf("%s: declara %s y NADIE lo llama desde core/ ni daemon/.\n"+
			"  Es un método de unión: existe para juntar dos piezas que se construyen por\n"+
			"  separado. Sin la llamada, las dos piezas funcionan, sus tests pasan, y el\n"+
			"  producto no. Ya pasó dos veces, con Attach y con el alcance de la compuerta,\n"+
			"  y las dos se descubrieron corriendo el producto y no en el CI.\n"+
			"  Si de verdad solo lo usa una herramienta de medición, dilo en el nombre:\n"+
			"  un método terminado en %q se salta esta regla.",
			dónde, nombre, sufijoDeMedición)
	}
}

// métodosDeUnión devuelve nombre -> archivo donde se declara.
//
// Solo métodos con receptor y exportados: una función suelta no une nada, y una
// privada no la puede llamar el cableado.
func métodosDeUnión(t *testing.T) map[string]string {
	t.Helper()

	out := map[string]string{}
	for _, raíz := range dondeViveElCableado {
		porArchivo(t, raíz, func(ruta string, archivo *ast.File) {
			if strings.HasSuffix(ruta, "_test.go") {
				return
			}
			for _, decl := range archivo.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Recv == nil || !fn.Name.IsExported() {
					continue
				}
				nombre := fn.Name.Name
				if strings.HasSuffix(nombre, sufijoDeMedición) {
					continue
				}
				if !esVerboDeUnión(nombre) {
					continue
				}
				out[nombre] = filepath.ToSlash(ruta)
			}
		})
	}
	return out
}

// esVerboDeUnión compara por PREFIJO, así que `BindRoom` y `UnbindRoom` entran
// sin tener que enumerarlos. Lo que se vigila es la forma del método, no su
// nombre exacto, porque el nombre exacto es justo lo que cambia.
func esVerboDeUnión(nombre string) bool {
	for _, v := range verbosDeUnión {
		if strings.HasPrefix(nombre, v) {
			return true
		}
	}
	return false
}
