package arch

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// Los guardianes de los tres enums del sondeo, y por qué no se puede reusar
// [constantesConPrefijo] para ellos.
//
// Porque los tres viven en el mismo archivo y dos comparten prefijo:
// `ProbeReference` es una clase y `ProbeAnswered` es un resultado, y encima
// `ProbeDeadline` es una duración que no es de ningún enum. Contar por prefijo
// daría siete constantes donde hay tres y cuatro, y el guardián fallaría siempre
// o nunca, que para el caso es lo mismo.
//
// Lo que se hace en cambio es contar por TIPO, leyendo el bloque `const` del
// AST. Un enum es un bloque con su tipo declarado en el primer elemento, y eso
// es exactamente lo que hay que contar.

const archivoDelSondeo = "../../core/domain/probe.go"

func TestLaListaDeClasesDeSondeoTieneTodasLasDelEnum(t *testing.T) {
	enElFuente := constantesDelTipo(t, archivoDelSondeo, "ProbeKind")
	if len(enElFuente) == 0 {
		t.Fatalf("no se encontró ninguna constante de ProbeKind en %s: la ruta o el "+
			"tipo cambiaron y este test no vigila nada", archivoDelSondeo)
	}

	enLaLista := map[domain.ProbeKind]bool{}
	for _, k := range domain.AllProbeKinds() {
		if enLaLista[k] {
			t.Errorf("la clase %d aparece dos veces en AllProbeKinds", k)
		}
		enLaLista[k] = true
	}

	if len(enElFuente) != len(domain.AllProbeKinds()) {
		t.Errorf("el enum declara %d clases de sondeo y AllProbeKinds devuelve %d.\n"+
			"  Constantes: %s\n"+
			"  Una clase fuera de la lista no la recorre el test de cobertura de la API,\n"+
			"  así que puede viajar sin nombre sin que nada falle.",
			len(enElFuente), len(domain.AllProbeKinds()), strings.Join(enElFuente, ", "))
	}
	for i := range enElFuente {
		if k := domain.ProbeKind(i + 1); !enLaLista[k] {
			t.Errorf("el valor %d del enum no está en AllProbeKinds", k)
		}
	}
}

// El precio de que falte un resultado es concreto: la vista de cable manda lo
// desconocido como "failed", así que un resultado nuevo sin nombrar se leería
// como que no se pudo preguntar cuando quizá midió algo.
func TestLaListaDeResultadosDeSondeoTieneTodosLosDelEnum(t *testing.T) {
	enElFuente := constantesDelTipo(t, archivoDelSondeo, "ProbeOutcome")
	if len(enElFuente) == 0 {
		t.Fatalf("no se encontró ninguna constante de ProbeOutcome en %s", archivoDelSondeo)
	}

	enLaLista := map[domain.ProbeOutcome]bool{}
	for _, o := range domain.AllProbeOutcomes() {
		if enLaLista[o] {
			t.Errorf("el resultado %d aparece dos veces en AllProbeOutcomes", o)
		}
		enLaLista[o] = true
	}

	if len(enElFuente) != len(domain.AllProbeOutcomes()) {
		t.Errorf("el enum declara %d resultados y AllProbeOutcomes devuelve %d.\n"+
			"  Constantes: %s",
			len(enElFuente), len(domain.AllProbeOutcomes()), strings.Join(enElFuente, ", "))
	}
	for i := range enElFuente {
		if o := domain.ProbeOutcome(i + 1); !enLaLista[o] {
			t.Errorf("el valor %d del enum no está en AllProbeOutcomes", o)
		}
	}
}

// Este enum arranca en CERO y no en uno, y es a propósito: el cero es el ciego,
// así que un veredicto recién construido no puede leerse como "está todo bien".
func TestLaListaDeVeredictosTieneTodosLosDelEnum(t *testing.T) {
	enElFuente := constantesDelTipo(t, archivoDelSondeo, "ProbeVerdict")
	if len(enElFuente) == 0 {
		t.Fatalf("no se encontró ninguna constante de ProbeVerdict en %s", archivoDelSondeo)
	}

	enLaLista := map[domain.ProbeVerdict]bool{}
	for _, v := range domain.AllProbeVerdicts() {
		if enLaLista[v] {
			t.Errorf("el veredicto %d aparece dos veces en AllProbeVerdicts", v)
		}
		enLaLista[v] = true
	}

	if len(enElFuente) != len(domain.AllProbeVerdicts()) {
		t.Errorf("el enum declara %d veredictos y AllProbeVerdicts devuelve %d.\n"+
			"  Constantes: %s",
			len(enElFuente), len(domain.AllProbeVerdicts()), strings.Join(enElFuente, ", "))
	}
	for i := range enElFuente {
		if v := domain.ProbeVerdict(i); !enLaLista[v] {
			t.Errorf("el valor %d del enum no está en AllProbeVerdicts", v)
		}
	}
	if domain.VerdictBlind != 0 {
		t.Error("VerdictBlind dejó de ser el cero del enum, así que un ProbeReport " +
			"recién construido ya no se lee como sin medir")
	}
}

// constantesDelTipo devuelve las constantes de un bloque `const` cuyo tipo
// declarado sea el pedido.
//
// El tipo se declara UNA vez, en el primer elemento del bloque, y los demás lo
// heredan del iota. Por eso se mira el bloque entero y no elemento por elemento:
// buscar el tipo en cada uno devolvería una sola constante de cada enum.
func constantesDelTipo(t *testing.T, ruta, tipo string) []string {
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
		if !bloqueDelTipo(gen, tipo) {
			continue
		}
		for _, spec := range gen.Specs {
			valor, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			out = append(out, nombresDe(valor)...)
		}
	}
	return out
}

// bloqueDelTipo dice si el primer elemento del bloque declara el tipo buscado.
func bloqueDelTipo(gen *ast.GenDecl, tipo string) bool {
	for _, spec := range gen.Specs {
		valor, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		ident, ok := valor.Type.(*ast.Ident)
		return ok && ident.Name == tipo
	}
	return false
}

func nombresDe(valor *ast.ValueSpec) []string {
	out := make([]string, 0, len(valor.Names))
	for _, nombre := range valor.Names {
		out = append(out, nombre.Name)
	}
	return out
}
