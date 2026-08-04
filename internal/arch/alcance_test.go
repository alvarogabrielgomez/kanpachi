package arch

import (
	"go/ast"
	"strings"
	"testing"
)

// El guardián del alcance de los filtros de la compuerta.
//
// # Qué protege, y por qué no basta con acordarse
//
// Un filtro de WFP sin condición de alcance aplica a TODOS los adaptadores de la
// máquina. Con un bloqueo duro, eso deja al usuario sin la entrada de su red de
// casa: sin impresoras, sin compartir archivos, sin nada que reciba conexiones.
//
// Y es el peor modo de fallo posible porque **no falla en ningún sitio**. El
// código compila, los tests funcionales de la sala pasan, la pantalla pinta
// verde, y el destrozo está en una máquina ajena. Leyendo el diff tampoco se ve:
// la diferencia entre el filtro correcto y el catastrófico es un campo que NO
// está.
//
// # Por qué existe antes que el adaptador de Windows
//
// Igual que el guardián del grupo base y el de las banderas del motor. La parte
// de WFP que llama a la API todavía no está escrita, y este test es lo que hace
// que la regla exista el día que alguien la escriba, en vez de ser un párrafo
// que nadie releyó.
//
// # Cómo lo comprueba
//
// `wfp.FilterSpec` se construye en UN solo sitio, `newSpec`, que exige las
// condiciones como parámetro. Un literal `FilterSpec{...}` en cualquier otro
// lado puede omitir el alcance y compilar. Así que se prohíben los literales
// fuera del fichero que los define.
//
// Es una comprobación de FORMA, y por eso no viaja sola: `FilterSpec.Validate`
// comprueba el CONTENIDO justo antes de instalar. Uno vigila cómo se escribe el
// código y el otro vigila lo que de verdad se va a poner en el sistema.
//
// Los `_test.go` quedan fuera porque `porArchivo` los salta, igual que en el
// resto de los guardianes: el test que comprueba que `Validate` caza un filtro
// sin alcance necesita construir uno a mano, y un guardián que se lo impidiera
// dejaría sin probar justo la comprobación que más importa.

// dondeSeConstruyenLosFiltros es el único fichero autorizado a escribir un
// literal de FilterSpec.
//
// Va la RUTA y no el nombre suelto, y eso costó una corrida: con `spec.go` a
// secas quedaban exentos todos los ficheros llamados así, y en `daemon/` hay
// varios. El guardián pasaba en verde con el literal prohibido puesto delante,
// que es peor que no tenerlo, porque además tranquiliza.
const dondeSeConstruyenLosFiltros = "firewall/wfp/spec.go"

// esElConstructor compara la ruta sin depender del separador del sistema.
func esElConstructor(ruta string) bool {
	return strings.HasSuffix(strings.ReplaceAll(ruta, `\`, "/"), dondeSeConstruyenLosFiltros)
}

// TestNadieConstruyeUnFiltroFueraDelConstructor.
func TestNadieConstruyeUnFiltroFueraDelConstructor(t *testing.T) {
	const raíz = "../../daemon"

	vistos := 0
	var infractores []string

	porArchivo(t, raíz, func(ruta string, archivo *ast.File) {
		vistos++
		autorizado := esElConstructor(ruta)

		ast.Inspect(archivo, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			if !nombraElTipoDeFiltro(lit.Type) {
				return true
			}
			if autorizado {
				return true
			}
			infractores = append(infractores, ruta)
			return false
		})
	})

	if vistos == 0 {
		t.Fatal("no se leyó ni un archivo de daemon/, así que este test no probaría nada")
	}
	for _, ruta := range infractores {
		t.Errorf("%s: construye un wfp.FilterSpec a mano.\n"+
			"  Un literal puede omitir el alcance y compilar igual, y un filtro sin alcance\n"+
			"  aplica a TODOS los adaptadores: con un bloqueo duro eso deja al usuario sin\n"+
			"  la entrada de su red de casa, sin que falle nada ni se vea en el diff.\n"+
			"  Usa el constructor de %s, que exige las condiciones.", ruta, dondeSeConstruyenLosFiltros)
	}
}

// nombraElTipoDeFiltro reconoce `FilterSpec{...}` y `wfp.FilterSpec{...}`.
func nombraElTipoDeFiltro(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name == "FilterSpec"
	case *ast.SelectorExpr:
		return v.Sel.Name == "FilterSpec"
	}
	return false
}

// TestLaCompuertaJamasSeInstalaEnLaCapaDeSalida.
//
// Bloquear la salida del adaptador virtual impediría que un invitado marque al
// puerto del juego del host, que es el caso central del producto. El tipo `Layer`
// no tiene un valor para eso a propósito, y esto vigila que nadie nombre la capa
// de conexión por su nombre de Windows para saltárselo.
func TestLaCompuertaJamasSeInstalaEnLaCapaDeSalida(t *testing.T) {
	prohibidas := []string{
		"ALE_AUTH_CONNECT",
		"AUTH_CONNECT",
	}

	for _, dir := range []string{"../../daemon", "../../core"} {
		for ruta, literales := range literalesPorArchivo(t, dir) {
			for _, prohibida := range prohibidas {
				if lit, ok := buscaLiteral(literales, strings.ToLower(prohibida)); ok {
					t.Errorf("%s: nombra la capa de salida en %q.\n"+
						"  La compuerta va SOLO en las capas de recepción. Bloquear la salida\n"+
						"  impide que un invitado marque al puerto del juego del host, que es\n"+
						"  el caso central del producto. Ver decisión 27.", ruta, lit)
				}
			}
		}
	}
}
