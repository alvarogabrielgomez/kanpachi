package arch

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// El guardián de que la lista de alertas no se quede atrás del enum.
//
// # Por qué hace falta un guardián y no basta con acordarse
//
// `domain.AllAlertKinds` es lo que vuelve enumerable un conjunto cerrado, y de
// ella cuelgan las dos promesas que hacen que una alerta llegue a la pantalla:
// que tenga nombre en la API local y que la UI tenga un texto para ella.
//
// El problema es que una lista escrita a mano no se queja cuando le falta algo.
// Agregar una constante al enum y olvidar la lista deja los dos tests de
// cobertura pasando en verde, porque los dos recorren la lista: la alerta nueva
// nunca entra al recorrido, así que nadie comprueba que exista.
//
// Este test es el que cierra el círculo. Cuenta las constantes del enum EN EL
// FUENTE, con el AST, y las compara contra la lista. Es la única forma de que
// la lista no pueda mentir sobre sí misma.
//
// Ya pasó una vez con AlertAuditFailed, sin lista y sin este guardián: la
// alerta viajaba a la UI como "unknown".

// TestLaListaDeAlertasTieneTodasLasDelEnum.
func TestLaListaDeAlertasTieneTodasLasDelEnum(t *testing.T) {
	const archivo = "../../core/domain/exposure.go"

	enElFuente := constantesConPrefijo(t, archivo, "Alert")
	if len(enElFuente) == 0 {
		t.Fatalf("no se encontró ninguna constante Alert* en %s: la ruta cambió y este test no vigila nada", archivo)
	}

	enLaLista := map[domain.AlertKind]bool{}
	for _, k := range domain.AllAlertKinds() {
		if enLaLista[k] {
			t.Errorf("la alerta %d aparece dos veces en AllAlertKinds", k)
		}
		enLaLista[k] = true
	}

	if len(enElFuente) != len(domain.AllAlertKinds()) {
		t.Errorf("el enum declara %d alertas y AllAlertKinds devuelve %d.\n"+
			"  Constantes en %s: %s\n"+
			"  Una alerta fuera de la lista no la recorre ningún test de cobertura, así que\n"+
			"  puede llegar a la UI como \"unknown\" sin que nada falle.",
			len(enElFuente), len(domain.AllAlertKinds()), archivo, strings.Join(enElFuente, ", "))
	}

	// Y los valores tienen que ser los del iota, consecutivos desde 1. Un hueco
	// significa que la lista trae un valor que el enum no declara, que es el
	// mismo fallo por el otro lado.
	for i := range enElFuente {
		k := domain.AlertKind(i + 1)
		if !enLaLista[k] {
			t.Errorf("el valor %d del enum no está en AllAlertKinds", k)
		}
	}
}

// TestLaListaDeClasesTieneTodasLasDelEnum es el mismo guardián para
// [domain.RuleClass], y el precio de que falte es peor que en las alertas.
//
// Una alerta que llega como "unknown" no se pinta y se pierde un aviso. Una
// CLASE que llega como "unknown" se pinta igual, como una fila más de la lista
// de reglas ajenas, y si esa clase era bloqueante la sala se abre con el agujero
// puesto: es la diferencia entre "el juego dejó una regla de más" y "cualquiera
// de la sala tiene tu teclado y tus ficheros".
func TestLaListaDeClasesTieneTodasLasDelEnum(t *testing.T) {
	const archivo = "../../core/domain/exposure.go"

	enElFuente := constantesConPrefijo(t, archivo, "Class")
	if len(enElFuente) == 0 {
		t.Fatalf("no se encontró ninguna constante Class* en %s: la ruta cambió y este test no vigila nada", archivo)
	}

	enLaLista := map[domain.RuleClass]bool{}
	for _, c := range domain.AllRuleClasses() {
		if enLaLista[c] {
			t.Errorf("la clase %d aparece dos veces en AllRuleClasses", c)
		}
		enLaLista[c] = true
	}

	if len(enElFuente) != len(domain.AllRuleClasses()) {
		t.Errorf("el enum declara %d clases y AllRuleClasses devuelve %d.\n"+
			"  Constantes en %s: %s\n"+
			"  Una clase fuera de la lista no la recorre ningún test de cobertura, así que\n"+
			"  puede llegar a la UI como \"unknown\" y pintarse como si fuera inofensiva.",
			len(enElFuente), len(domain.AllRuleClasses()), archivo, strings.Join(enElFuente, ", "))
	}

	for i := range enElFuente {
		c := domain.RuleClass(i + 1)
		if !enLaLista[c] {
			t.Errorf("el valor %d del enum no está en AllRuleClasses", c)
		}
	}
}

// constantesConPrefijo devuelve los nombres de las constantes de un archivo que
// empiezan con el prefijo dado.
//
// Por AST y no por texto: un comentario que mencione AlertLoQueSea no cuenta
// como declaración, y acá contar de más es tan malo como contar de menos.
func constantesConPrefijo(t *testing.T, ruta, prefijo string) []string {
	t.Helper()

	fset := token.NewFileSet()
	archivo, err := parser.ParseFile(fset, ruta, nil, 0)
	if err != nil {
		t.Fatalf("no se pudo parsear %s: %v", ruta, err)
	}

	var out []string
	for _, decl := range archivo.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			valor, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, nombre := range valor.Names {
				if strings.HasPrefix(nombre.Name, prefijo) && nombre.Name != prefijo {
					out = append(out, nombre.Name)
				}
			}
		}
	}
	return out
}
