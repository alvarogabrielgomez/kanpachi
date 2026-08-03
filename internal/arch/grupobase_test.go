package arch

import (
	"go/ast"
	"go/token"
	"strings"
	"testing"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// El guardián de la cuarentena de base.
//
// # Qué protege
//
// El firewall tiene DOS grupos y el reparto es la mitad de la decisión: el
// instalador pone la cuarentena, el daemon pone la sala, y ninguno toca lo del
// otro. Lo que hace valiosa a la cuarentena es justo que sobrevive al daemon:
// sigue puesta con el servicio detenido, deshabilitado o a medio desinstalar.
//
// # Las dos formas de romperlo, y las dos se ven acá
//
// La primera es que el daemon la escriba o la borre. Basta con que nombre el
// grupo, porque nombrarlo es lo único que hace falta para tocarlo.
//
// La segunda es más silenciosa: `FirewallGroup` es PREFIJO de
// `FirewallGroupBase`, así que una purga escrita con HasPrefix en vez de
// igualdad se lleva la cuarentena por delante. Nadie lo notaría, porque con la
// cuarentena borrada todo sigue funcionando igual, solo que la máquina queda
// expuesta. Ese es el peor modo de fallo posible en este proyecto: silencioso y
// con la pantalla en verde.
//
// # Por qué existe antes que el adaptador
//
// Igual que el guardián del motor: el adaptador del firewall todavía no está
// escrito, y este test es lo que hace que la regla exista el día que alguien lo
// escriba, en vez de ser un párrafo que nadie releyó.

// TestElDaemonNoNombraElGrupoBase.
//
// Barre `daemon/` entero, literales e identificadores. El literal solo no
// alcanza: `domain.FirewallGroupBase` toca el mismo grupo sin escribir la cadena.
func TestElDaemonNoNombraElGrupoBase(t *testing.T) {
	const identificador = "FirewallGroupBase"
	literal := strings.ToLower(domain.FirewallGroupBase)

	archivos := literalesPorArchivo(t, "../../daemon")
	if len(archivos) == 0 {
		t.Fatal("no se leyó ni un archivo de daemon/, así que este test no probaría nada")
	}
	for ruta, literales := range archivos {
		if lit, ok := buscaLiteral(literales, literal); ok {
			t.Errorf("%s: aparece el grupo de la cuarentena de base en %q.\n"+
				"  Ese grupo lo pone el instalador y el daemon jamás lo toca: es lo único que\n"+
				"  protege la máquina mientras el servicio no corre.", ruta, lit)
		}
	}

	for _, ruta := range identificadoresEn(t, "../../daemon", identificador) {
		t.Errorf("%s: usa domain.%s.\n"+
			"  El daemon solo escribe el grupo de la sala. La cuarentena de base es del instalador.",
			ruta, identificador)
	}
}

// TestNadieComparaElGrupoPorPrefijo.
//
// `strings.HasPrefix(algo, domain.FirewallGroup)` es verdadero para las reglas
// del grupo base, así que una purga escrita así borra la cuarentena. La
// comparación tiene que ser por igualdad.
//
// Se vigila en TODO el repo y no solo en `daemon/`: el error es igual de caro
// venga de donde venga.
func TestNadieComparaElGrupoPorPrefijo(t *testing.T) {
	// Se comprueba primero que la trampa sigue existiendo. El día que los dos
	// grupos dejen de compartir prefijo, este test sobra y hay que decirlo en vez
	// de dejarlo pasando por una razón que ya no es cierta.
	if !strings.HasPrefix(domain.FirewallGroupBase, domain.FirewallGroup) {
		t.Fatalf("%q ya no empieza por %q: la confusión que este test vigila dejó de ser posible,"+
			" así que el test miente sobre lo que protege",
			domain.FirewallGroupBase, domain.FirewallGroup)
	}

	for _, dir := range árbolDelProducto {
		for _, ruta := range llamadasConGrupo(t, dir) {
			t.Errorf("%s: compara el grupo del firewall por prefijo.\n"+
				"  %q es prefijo de %q, así que esto se lleva por delante la cuarentena de base.\n"+
				"  La comparación va por igualdad exacta.",
				ruta, domain.FirewallGroup, domain.FirewallGroupBase)
		}
	}
}

// árbolDelProducto es todo el Go que este repo escribe.
//
// Se nombra en vez de recorrer la raíz, y no es por velocidad: `.agents/` y
// `third_party/` están en .gitignore y el primero trae ejemplos .go de terceros
// con imports rotos, según dice el propio .gitignore. Un guardián que se cae por
// el código de otro no falla donde tiene que fallar.
var árbolDelProducto = []string{"../../core", "../../daemon", "../../registry", "../../internal"}

// identificadoresEn devuelve los archivos que mencionan un identificador.
//
// Por AST y no por texto, para que un comentario que explique por qué NO se usa
// no dispare el guardián. Es el mismo criterio que el barrido de banderas del
// motor.
func identificadoresEn(t *testing.T, raíz, nombre string) []string {
	t.Helper()
	var out []string

	porArchivo(t, raíz, func(ruta string, archivo *ast.File) {
		encontrado := false
		ast.Inspect(archivo, func(n ast.Node) bool {
			if encontrado {
				return false
			}
			if id, ok := n.(*ast.Ident); ok && id.Name == nombre {
				encontrado = true
				return false
			}
			return true
		})
		if encontrado {
			out = append(out, ruta)
		}
	})
	return out
}

// llamadasConGrupo busca comparaciones por prefijo o sufijo sobre el grupo del
// firewall.
//
// Se miran las dos: HasSuffix no rompe la cuarentena hoy, y la rompería el día
// que alguien renombre los grupos al revés. Sale más barato vigilarla ahora que
// descubrirlo entonces.
func llamadasConGrupo(t *testing.T, raíz string) []string {
	t.Helper()
	sospechosas := map[string]bool{"HasPrefix": true, "HasSuffix": true, "Contains": true}
	var out []string

	// No hace falta tallar una excepción para este archivo, que compara los dos
	// grupos a propósito unas líneas más arriba: porArchivo ya deja fuera los
	// _test.go, por lo mismo que en el resto de los guardianes. Un test que
	// afirme que algo NO se hace tiene que poder escribirlo.
	porArchivo(t, raíz, func(ruta string, archivo *ast.File) {
		ast.Inspect(archivo, func(n ast.Node) bool {
			llamada, ok := n.(*ast.CallExpr)
			if !ok || len(llamada.Args) < 2 {
				return true
			}
			sel, ok := llamada.Fun.(*ast.SelectorExpr)
			if !ok || !sospechosas[sel.Sel.Name] {
				return true
			}
			if paquete, ok := sel.X.(*ast.Ident); !ok || paquete.Name != "strings" {
				return true
			}
			// El segundo argumento es la aguja: lo que se busca DENTRO del
			// primero. Que la aguja sea el grupo es lo que convierte la llamada
			// en una comparación de grupos.
			if nombraElGrupo(llamada.Args[1]) {
				out = append(out, ruta)
				return false
			}
			return true
		})
	})
	return out
}

// nombraElGrupo dice si la expresión es el grupo del firewall, escrito como
// constante del dominio o como literal.
func nombraElGrupo(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.SelectorExpr:
		return v.Sel.Name == "FirewallGroup"
	case *ast.Ident:
		return v.Name == "FirewallGroup"
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return false
		}
		return strings.EqualFold(strings.Trim(v.Value, `"`), domain.FirewallGroup)
	}
	return false
}
