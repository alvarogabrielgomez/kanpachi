package arch

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// El guardián del corte automático.
//
// # Qué protege, y por qué merece un test propio
//
// El requisito no es "que exista un contador". Es que **ningún agente externo
// pueda cambiar una configuración y quedarse conectado para siempre**. Un
// proceso corriendo como el usuario, un archivo de ProgramData editado a mano,
// una clave de registro: nada de eso puede alargar ni desactivar el corte.
//
// Lo que lo garantiza son tres ausencias, y las tres se comprueban acá porque
// una ausencia no se ve leyendo el código:
//
//  1. Los plazos son constantes de COMPILACIÓN. Una `var` se puede reasignar
//     desde cualquier init del programa; una `const` no existe en tiempo de
//     ejecución.
//  2. No hay ningún método exportado que los ponga, los apague ni los salte.
//     Lo que no está en la superficie no se puede llamar por error, y tampoco
//     a propósito desde el named pipe.
//  3. No son estado por sala. Un campo en RoomState sería un valor que entra
//     por el archivo de sala guardada, o sea justo el camino que el esquema de
//     ese archivo existe para cerrar.

// plazosVigilados son las constantes que sostienen los cortes automáticos, con
// el archivo donde tienen que vivir.
var plazosVigilados = map[string]string{
	"HostAbsenceLimit": "../../core/domain/state.go",
	"HostSilenceLimit": "../../core/domain/state.go",
	"ReconnectLimit":   "../../core/domain/state.go",
	"AnnounceInterval": "../../core/usecase/announce.go",
	"KickGrace":        "../../core/usecase/kickmember.go",
	"CredentialTTL":    "../../core/usecase/issuecredential.go",
	"Beat":             "../../daemon/service/supervisor/supervisor.go",
	"Sweep":            "../../daemon/service/supervisor/supervisor.go",
}

// TestLosPlazosSonConstantesDeCompilación.
func TestLosPlazosSonConstantesDeCompilación(t *testing.T) {
	for nombre, archivo := range plazosVigilados {
		t.Run(nombre, func(t *testing.T) {
			decl := buscaDeclaración(t, archivo, nombre)
			if decl == nil {
				t.Fatalf("%s ya no está declarado en %s: si se movió, mueve también esta vigilancia",
					nombre, archivo)
			}
			if decl.Tok != token.CONST {
				t.Fatalf("%s dejó de ser const en %s.\n"+
					"  Una var se puede reasignar desde cualquier init del programa, y con eso el corte "+
					"automático se apaga sin tocar una línea de la lógica.", nombre, archivo)
			}
		})
	}
}

// TestNingúnPlazoSaleDeUnValorCalculado.
//
// Una const que valga otro identificador es una puerta entreabierta: hoy apunta
// a un literal y mañana a algo que sale de un archivo. El valor tiene que
// leerse en el sitio donde está la constante.
func TestNingúnPlazoSaleDeUnValorCalculado(t *testing.T) {
	for nombre, archivo := range plazosVigilados {
		t.Run(nombre, func(t *testing.T) {
			decl := buscaDeclaración(t, archivo, nombre)
			if decl == nil {
				t.Fatalf("%s ya no está en %s", nombre, archivo)
			}
			for _, spec := range decl.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, id := range vs.Names {
					if id.Name != nombre || i >= len(vs.Values) {
						continue
					}
					if !esLiteralDeTiempo(vs.Values[i]) {
						t.Fatalf("%s no sale de un literal en %s: su valor viene de otro sitio", nombre, archivo)
					}
				}
			}
		})
	}
}

// setterProhibido empareja cualquier método que pudiera cambiar un plazo o la
// cadencia que los evalúa.
var setterProhibido = regexp.MustCompile(`(?i)^(Set|Disable|Configure|Override|Skip).*(Absence|Silence|Timeout|Limit|Tick|Beat|Deadline|Grace)`)

// TestNoHayFormaDeApagarElCorteDesdeFuera.
func TestNoHayFormaDeApagarElCorteDesdeFuera(t *testing.T) {
	for _, dir := range []string{"../../core", "../../daemon"} {
		recorreFuentes(t, dir, func(ruta string, archivo *ast.File) {
			for _, d := range archivo.Decls {
				fn, ok := d.(*ast.FuncDecl)
				if !ok || !fn.Name.IsExported() {
					continue
				}
				if setterProhibido.MatchString(fn.Name.Name) {
					t.Errorf("%s expone %s.\n"+
						"  El corte automático no se configura desde fuera: si esto entra, un proceso local "+
						"puede quedarse en una sala para siempre.", ruta, fn.Name.Name)
				}
			}
		})
	}
}

// TestElPlazoNoEsEstadoPorSala.
//
// Un campo con Limit o Timeout en el nombre dentro de RoomState sería un valor
// que entra por el archivo de sala guardada, y ese archivo lleva identidad y
// referencias, jamás política.
func TestElPlazoNoEsEstadoPorSala(t *testing.T) {
	const archivo = "../../core/domain/state.go"
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, archivo, nil, 0)
	if err != nil {
		t.Fatalf("no se pudo parsear %s: %v", archivo, err)
	}

	visto := false
	ast.Inspect(f, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if !ok || ts.Name.Name != "RoomState" {
			return true
		}
		visto = true
		st, ok := ts.Type.(*ast.StructType)
		if !ok {
			return false
		}
		for _, campo := range st.Fields.List {
			for _, nombre := range campo.Names {
				bajo := strings.ToLower(nombre.Name)
				if strings.Contains(bajo, "limit") || strings.Contains(bajo, "timeout") {
					t.Errorf("RoomState tiene el campo %s.\n"+
						"  Un plazo por sala se carga desde el archivo de sala guardada, y ese archivo "+
						"no puede expresar política.", nombre.Name)
				}
			}
		}
		return false
	})
	if !visto {
		t.Fatal("no se encontró RoomState: la vigilancia no está vigilando nada")
	}
}

// TestLaVigilanciaDelCorteDetectaUnaViolación comprueba el detector contra
// casos conocidos. Un guardián que nunca se probó no es un guardián.
func TestLaVigilanciaDelCorteDetectaUnaViolación(t *testing.T) {
	casos := []struct {
		nombre string
		quiero bool
	}{
		{"SetHostAbsenceLimit", true},
		{"DisableTickers", true},
		{"DisableAbsenceCheck", true},
		{"SkipDeadlines", true},
		{"OverrideBeat", true},
		{"ConfigureTimeouts", true},
		{"SetHostPresent", false},
		{"SetDirectPlay", false},
		{"SetTunnelDown", false},
		{"Tick", false},
	}
	for _, c := range casos {
		if got := setterProhibido.MatchString(c.nombre); got != c.quiero {
			t.Errorf("setterProhibido(%q) = %v, se esperaba %v", c.nombre, got, c.quiero)
		}
	}
}

// buscaDeclaración devuelve el bloque donde se declara un identificador.
func buscaDeclaración(t *testing.T, archivo, nombre string) *ast.GenDecl {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, archivo, nil, 0)
	if err != nil {
		t.Fatalf("no se pudo parsear %s: %v", archivo, err)
	}
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, id := range vs.Names {
				if id.Name == nombre {
					return gd
				}
			}
		}
	}
	return nil
}

// esLiteralDeTiempo acepta las formas en que se escribe una duración fija:
// `20 * time.Minute`, `time.Minute`, y un entero pelado.
func esLiteralDeTiempo(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.BasicLit:
		return true
	case *ast.SelectorExpr:
		paquete, ok := v.X.(*ast.Ident)
		return ok && paquete.Name == "time"
	case *ast.BinaryExpr:
		return esLiteralDeTiempo(v.X) && esLiteralDeTiempo(v.Y)
	case *ast.ParenExpr:
		return esLiteralDeTiempo(v.X)
	default:
		return false
	}
}

// recorreFuentes aplica f a cada .go de producción bajo dir.
func recorreFuentes(t *testing.T, dir string, f func(ruta string, archivo *ast.File)) {
	t.Helper()
	fset := token.NewFileSet()
	err := filepath.WalkDir(dir, func(ruta string, d os.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case d.IsDir(), !strings.HasSuffix(ruta, ".go"), strings.HasSuffix(ruta, "_test.go"):
			return nil
		}
		archivo, err := parser.ParseFile(fset, ruta, nil, 0)
		if err != nil {
			t.Errorf("no se pudo parsear %s: %v", ruta, err)
			return nil
		}
		f(ruta, archivo)
		return nil
	})
	if err != nil {
		t.Fatalf("recorriendo %s: %v", dir, err)
	}
}
