package arch

import (
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"strings"
	"testing"
)

// Los dos candados del password del registro, que son lo que impide que se
// escape por donde nadie mira.
//
// # Por qué hacen falta si hoy no se escapa
//
// Porque hoy no se escapa por OMISIÓN, no por construcción. Nada del transporte
// anota los parámetros de una petición y `SeedPassword` no llama al diario, y
// las dos cosas son ciertas hasta que alguien depure un fallo raro agregando un
// `Log.Debug` con los parámetros dentro. Eso es exactamente lo que ya pasó con
// el log del motor, y falla callado: el fichero que se manda por chat cuando
// alguien pide ayuda es el mismo que llevaría el password.
//
// Los tres sitios donde se comparte, y por eso los tres que se vigilan: el
// diario de progreso que la ventana pinta, el log del daemon que se copia al
// portapapeles, y la salida del CLI.

// dondeVivelPassword son los ficheros por los que pasa el password en claro.
//
// Son tres y no más, y esa cortedad es el diseño: el valor se convierte en
// [domain.SeedAuthProof] en cuanto se puede, y de ahí en adelante lo que viaja
// ya es un hash con el host dentro.
var dondeVivelPassword = []string{
	"../../core/usecase/seedpassword.go",
	"../../daemon/transport/protocol/server.go",
	"../../daemon/cmd/kanpachi/password.go",
	"../../registry/cli/password.go",
}

// lasQueCuentan son las llamadas que dejan rastro en algún sitio que sobrevive
// a la operación, o que alguien copia y pega.
//
// `Fprintf` y compañía entran porque el CLI escribe con ellas, y `Sprintf`
// porque el resultado casi siempre acaba en una de las otras.
var lasQueCuentan = regexp.MustCompile(
	`^(Info|Warn|Error|Debug|Log|Step|Begin|Note|Print|Printf|Println|` +
		`Fprint|Fprintf|Fprintln|Sprint|Sprintf|Sprintln|Errorf)$`)

// TestElPasswordNoLlegaANingunRastro recorre los ficheros que lo tocan y falla
// si el valor entra como argumento de algo que escribe.
func TestElPasswordNoLlegaANingunRastro(t *testing.T) {
	for _, ruta := range dondeVivelPassword {
		fset := token.NewFileSet()
		archivo, err := parser.ParseFile(fset, ruta, nil, 0)
		if err != nil {
			t.Fatalf("no se pudo leer %s: %v", ruta, err)
		}

		ast.Inspect(archivo, func(n ast.Node) bool {
			llamada, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if !lasQueCuentan.MatchString(nombreDeLaLlamada(llamada.Fun)) {
				return true
			}
			for _, arg := range llamada.Args {
				if secreto := elSecretoDentro(arg); secreto != "" {
					t.Errorf("%s:%d: %s(… %s …) deja el password del registro en algo "+
						"que se escribe. El log del daemon se manda por chat, el diario "+
						"se pinta en pantalla y la salida del CLI acaba en un fichero",
						ruta, fset.Position(llamada.Pos()).Line,
						nombreDeLaLlamada(llamada.Fun), secreto)
				}
			}
			return true
		})
	}
}

// TestSeVigilanLosFicherosCorrectos es el candado del candado.
//
// Sin esto, mover o renombrar cualquiera de los tres ficheros deja el test de
// arriba recorriendo una lista vacía y pasando en verde para siempre. Ya pasó en
// este repositorio con un guardián que apuntaba a una ruta que había cambiado.
func TestSeVigilanLosFicherosCorrectos(t *testing.T) {
	for _, ruta := range dondeVivelPassword {
		fset := token.NewFileSet()
		archivo, err := parser.ParseFile(fset, ruta, nil, 0)
		if err != nil {
			t.Fatalf("no se pudo leer %s: %v", ruta, err)
		}
		encontrado := false
		ast.Inspect(archivo, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && esNombreDeSecreto(id.Name) {
				encontrado = true
			}
			return true
		})
		if !encontrado {
			t.Errorf("%s ya no nombra ningún password, así que o se movió el código "+
				"o esta lista quedó vigilando un fichero que no vigila nada", ruta)
		}
	}
}

// nombreDeLaLlamada saca el identificador final: `x.Log.Info` da `Info`.
func nombreDeLaLlamada(fun ast.Expr) string {
	switch f := fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		return f.Sel.Name
	}
	return ""
}

// elSecretoDentro devuelve el nombre del identificador sospechoso que aparezca
// dentro de la expresión, o vacío.
//
// Mira el subárbol entero y no solo la raíz, porque el argumento peligroso rara
// vez es la variable pelada: es `"pw="+pw`, o un `fmt.Sprintf` con ella dentro.
func elSecretoDentro(e ast.Expr) string {
	var hallado string
	ast.Inspect(e, func(n ast.Node) bool {
		if hallado != "" {
			return false
		}
		switch v := n.(type) {
		case *ast.Ident:
			if esNombreDeSecreto(v.Name) {
				hallado = v.Name
			}
		case *ast.SelectorExpr:
			if esNombreDeSecreto(v.Sel.Name) {
				hallado = v.Sel.Name
			}
		}
		return true
	})
	return hallado
}

// esNombreDeSecreto reconoce cómo se llama el valor en los cuatro ficheros.
//
// Por nombre y no por tipo, y hay que decir por qué: el tipo es `string`, igual
// que el nombre de la sala y el host del registro, así que un candado por tipo
// no distinguiría nada. Lo que se vigila es la CONVENCIÓN, y por eso el otro
// test comprueba que la convención sigue viva en cada fichero.
//
// `proof` NO está en la lista a propósito: es el hash, con el host dentro, y
// registrarlo no entrega ningún password que nadie reuse. Lo que entrega es una
// credencial de ese seed, así que tampoco conviene, y para eso está el tope de
// arriba sobre lo que sí es el password.
func esNombreDeSecreto(nombre string) bool {
	switch nombre {
	case "password", "pw", "Password":
		return true
	}
	return false
}

// TestConJSONElFalloSaleSinProsa vigila el orden de [report].
//
// # Qué regresión atrapa
//
// La de mover el `Fprintln(os.Stderr, …)` por encima de la rama de `--json`, o
// borrar el `return` de esa rama. Las dos dejan la prosa saliendo en el modo que
// existe para que no salga, y ninguna de las dos rompe nada más: el JSON sigue
// apareciendo, así que un script sigue funcionando y nadie mira el stderr que
// nadie lee.
//
// Es lo que la fase 3C pide por nombre: `--json` es una superficie de seguridad
// y no una comodidad, porque es donde "nunca texto en claro con el motivo" se
// puede romper sin que nadie mire.
func TestConJSONElFalloSaleSinProsa(t *testing.T) {
	const ruta = "../../daemon/cmd/kanpachi/main.go"
	fset := token.NewFileSet()
	archivo, err := parser.ParseFile(fset, ruta, nil, 0)
	if err != nil {
		t.Fatalf("no se pudo leer %s: %v", ruta, err)
	}

	cuerpo := cuerpoDeLaFuncion(archivo, "report")
	if cuerpo == nil {
		t.Fatal("no se encontró func report: el candado dejó de vigilar nada")
	}

	corte := -1
	for i, sentencia := range cuerpo.List {
		si, ok := sentencia.(*ast.IfStmt)
		if !ok || elSecretoDentro(si.Cond) != "" {
			continue
		}
		if !mencionaJSON(si.Cond) || !terminaEnReturn(si.Body) {
			continue
		}
		corte = i
		break
	}
	if corte < 0 {
		t.Fatal("report ya no tiene una rama `if op.json { … return }`: con --json " +
			"el fallo vuelve a salir como prosa, que es lo que 3C prohíbe")
	}

	for i, sentencia := range cuerpo.List {
		if i > corte {
			continue
		}
		ast.Inspect(sentencia, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if ok && sel.Sel.Name == "Stderr" {
				t.Errorf("%s:%d: se escribe en stderr ANTES de la rama de --json, "+
					"así que la prosa del fallo sale igual con --json puesto",
					ruta, fset.Position(sel.Pos()).Line)
			}
			return true
		})
	}
}

func cuerpoDeLaFuncion(archivo *ast.File, nombre string) *ast.BlockStmt {
	for _, decl := range archivo.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Name.Name == nombre {
			return fn.Body
		}
	}
	return nil
}

func mencionaJSON(e ast.Expr) bool {
	encontrado := false
	ast.Inspect(e, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && strings.EqualFold(id.Name, "json") {
			encontrado = true
		}
		return true
	})
	return encontrado
}

func terminaEnReturn(b *ast.BlockStmt) bool {
	if len(b.List) == 0 {
		return false
	}
	_, ok := b.List[len(b.List)-1].(*ast.ReturnStmt)
	return ok
}
