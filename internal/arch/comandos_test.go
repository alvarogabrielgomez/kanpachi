package arch

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"testing"
)

// El guardián de que la ayuda del CLI no se quede atrás de la tabla de comandos.
//
// # El hueco que cierra, que ya se coló entero en una versión publicada
//
// `daemon/cmd/kanpachi` lleva DOS listas de comandos: `commands`, que es la
// tabla que despacha, y `groups`, que es de dónde `kanpachi help` saca lo que
// imprime. Un comando que está en la primera y no en la segunda **funciona y no
// existe**: se puede escribir, hace lo suyo, y no aparece en ningún sitio donde
// alguien pudiera enterarse de que está.
//
// Le pasó a `quarantine`. Salió en 0.5.0 con sus tres bocas —comando, entrada
// del asistente e interruptor en la ventana—, con su documentación de diseño y
// con su copy escrito, y `kanpachi help` no lo nombró ni una vez hasta el
// 2026-08-18. Nada estaba roto: los tests pasaban, el comando contestaba, y la
// única forma de descubrirlo era leer el fuente.
//
// # Por qué se lee el FUENTE y no se importa la lista
//
// Porque las dos viven en `package main`, que no se puede importar. Se parsean
// con el AST, que además es lo correcto por otro motivo: lo que hay que
// comparar es lo que está escrito, no lo que un binario decida construir.
//
// # Falla por las dos vías, y las dos importan
//
// Un comando sin grupo es invisible. Un grupo que nombra un comando que no
// existe imprime una línea vacía en la ayuda, que es peor que no imprimirla:
// dice que hay algo que no hay.
func TestTodoComandoDelCLIApareceEnLaAyuda(t *testing.T) {
	const archivo = "../../daemon/cmd/kanpachi/commands.go"

	fset := token.NewFileSet()
	fuente, err := parser.ParseFile(fset, archivo, nil, 0)
	if err != nil {
		t.Fatalf("no se pudo parsear %s: %v", archivo, err)
	}

	enLaTabla := clavesDeLaTabla(t, fuente)
	if len(enLaTabla) == 0 {
		t.Fatalf("no se encontró la tabla `commands` en %s: la forma cambió y este test no vigila nada", archivo)
	}
	enLaAyuda := nombresDeLosGrupos(t, fuente)
	if len(enLaAyuda) == 0 {
		t.Fatalf("no se encontró la lista `groups` en %s: la forma cambió y este test no vigila nada", archivo)
	}

	for _, nombre := range enLaTabla {
		if !enLaAyuda[nombre] {
			t.Errorf("el comando %q existe y `kanpachi help` no lo nombra: "+
				"agregalo a `groups`, o nadie va a enterarse de que está", nombre)
		}
	}

	tabla := map[string]bool{}
	for _, n := range enLaTabla {
		tabla[n] = true
	}
	for nombre := range enLaAyuda {
		if !tabla[nombre] {
			t.Errorf("`groups` nombra %q, que no está en la tabla de comandos: "+
				"la ayuda imprimiría una línea vacía", nombre)
		}
	}
}

// clavesDeLaTabla saca los nombres de comando de `commands = map[string]command{…}`.
//
// La tabla se rellena en `init` y no en la declaración —las entradas se nombran
// entre ellas y Go no admite ciclos de inicialización—, así que se busca la
// ASIGNACIÓN y no una `var`.
func clavesDeLaTabla(t *testing.T, fuente *ast.File) []string {
	t.Helper()

	var out []string
	ast.Inspect(fuente, func(n ast.Node) bool {
		asig, ok := n.(*ast.AssignStmt)
		if !ok || len(asig.Lhs) != 1 || len(asig.Rhs) != 1 {
			return true
		}
		if id, ok := asig.Lhs[0].(*ast.Ident); !ok || id.Name != "commands" {
			return true
		}
		lit, ok := asig.Rhs[0].(*ast.CompositeLit)
		if !ok {
			return true
		}
		for _, elt := range lit.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			if s, hay := textoDe(kv.Key); hay {
				out = append(out, s)
			}
		}
		return false
	})
	return out
}

// nombresDeLosGrupos saca los comandos que la ayuda imprime.
//
// Cada grupo es `{"Título:", []string{…}}`, así que lo que se recoge es el
// SEGUNDO elemento de cada uno. Tomar todas las cadenas del literal metería los
// títulos, y un título no es un comando.
func nombresDeLosGrupos(t *testing.T, fuente *ast.File) map[string]bool {
	t.Helper()

	out := map[string]bool{}
	for _, decl := range fuente.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR {
			continue
		}
		for _, spec := range gen.Specs {
			valor, ok := spec.(*ast.ValueSpec)
			if !ok || len(valor.Names) != 1 || valor.Names[0].Name != "groups" {
				continue
			}
			for _, v := range valor.Values {
				lista, ok := v.(*ast.CompositeLit)
				if !ok {
					continue
				}
				for _, elt := range lista.Elts {
					grupo, ok := elt.(*ast.CompositeLit)
					if !ok || len(grupo.Elts) != 2 {
						continue
					}
					nombres, ok := grupo.Elts[1].(*ast.CompositeLit)
					if !ok {
						continue
					}
					for _, n := range nombres.Elts {
						if s, hay := textoDe(n); hay {
							out[s] = true
						}
					}
				}
			}
		}
	}
	return out
}

// textoDe devuelve el contenido de una cadena literal.
func textoDe(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return s, true
}
