package arch

import (
	"go/ast"
	"go/token"
	"path/filepath"
	"strings"
	"testing"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// El guardián de la cuarentena de base.
//
// # Qué protege, y qué cambió
//
// Antes esto decía "el daemon jamás nombra el grupo base", porque la cuarentena
// la iba a poner un instalador. **No hay instalador**, y una cuarentena que
// depende de un programa que no existe es una promesa apagada. Ahora la escribe
// y la repone el daemon en cada arranque.
//
// O sea que "jamás lo nombra" dejó de ser la regla que protege, porque el daemon
// tiene que nombrarlo para escribirlo. La que sí protege, y es la que valía
// desde el principio, es esta: **jamás lo BORRA, y jamás lo compara por
// prefijo.** Lo que hace valiosa a la cuarentena no es quién la pone: es que
// sigue puesta con el servicio detenido, deshabilitado o a medio desinstalar.
//
// # Las tres formas de romperlo, y las tres se ven acá
//
// La primera es que el grupo se nombre desde cualquier sitio del daemon. Se
// permite en UN paquete y en ninguno más, así que el radio de lo que puede
// tocarlo se lee en una línea y el guardián conserva los dientes en todo el
// resto.
//
// La segunda es una llamada destructiva apuntada al grupo base. Se busca por
// AST, por el verbo y por el argumento.
//
// La tercera es más silenciosa: `FirewallGroup` es PREFIJO de
// `FirewallGroupBase`, así que una purga escrita con HasPrefix en vez de
// igualdad se lleva la cuarentena por delante. Nadie lo notaría, porque con la
// cuarentena borrada todo sigue funcionando igual, solo que la máquina queda
// expuesta. Ese es el peor modo de fallo posible en este proyecto: silencioso y
// con la pantalla en verde.

// paqueteDeLaCuarentena es el ÚNICO de daemon/ que puede nombrar el grupo base.
//
// Uno solo y por prefijo de ruta, así que el permiso cubre el archivo puro y su
// hermano _windows sin cubrir nada más. Ampliar esta lista es una decisión que
// se ve en el diff.
const paqueteDeLaCuarentena = "daemon/adapter/firewall/windowscom/"

// TestSoloUnPaqueteDelDaemonNombraElGrupoBase.
//
// Barre `daemon/` entero, literales e identificadores. El literal solo no
// alcanza: `domain.FirewallGroupBase` toca el mismo grupo sin escribir la cadena.
func TestSoloUnPaqueteDelDaemonNombraElGrupoBase(t *testing.T) {
	const identificador = "FirewallGroupBase"
	literal := strings.ToLower(domain.FirewallGroupBase)

	archivos := literalesPorArchivo(t, "../../daemon")
	if len(archivos) == 0 {
		t.Fatal("no se leyó ni un archivo de daemon/, así que este test no probaría nada")
	}
	for ruta, literales := range archivos {
		if permitido(ruta) {
			continue
		}
		if lit, ok := buscaLiteral(literales, literal); ok {
			t.Errorf("%s: aparece el grupo de la cuarentena de base en %q.\n"+
				"  Solo %s puede nombrarlo. Nombrarlo es todo lo que hace falta para tocarlo,\n"+
				"  y la cuarentena es lo único que protege la máquina con el servicio parado.",
				ruta, lit, paqueteDeLaCuarentena)
		}
	}

	for _, ruta := range identificadoresEn(t, "../../daemon", identificador) {
		if permitido(ruta) {
			continue
		}
		t.Errorf("%s: usa domain.%s.\n"+
			"  Solo %s puede nombrarlo.", ruta, identificador, paqueteDeLaCuarentena)
	}
}

// TestElDaemonNoBorraLaCuarentenaDeBase.
//
// Es lo que compraba la lista blanca de antes. El daemon ya puede nombrar el
// grupo, así que lo que no puede existir es la ACCIÓN: ninguna llamada
// destructiva de ningún sitio de daemon/, incluido el paquete permitido, puede
// llevar el grupo base entre sus argumentos.
//
// El caso real que caza: alguien escribe la reposición de la cuarentena como un
// "purgo y vuelvo a escribir", que es la forma natural de reponer y la que
// convierte cada arranque del servicio en una ventana sin protección. Y si ese
// arranque falla a la mitad, la ventana no se cierra nunca.
func TestElDaemonNoBorraLaCuarentenaDeBase(t *testing.T) {
	rutas := destructivasConGrupoBase(t, "../../daemon")
	for _, r := range rutas {
		t.Errorf("%s: llama a %s con el grupo de la cuarentena de base.\n"+
			"  El daemon la escribe y la repone, y no la borra JAMÁS: lo que la hace valiosa\n"+
			"  es seguir puesta con el servicio detenido. Borrarla es del desinstalador.",
			r.ruta, r.verbo)
	}
}

// TestNoExisteUnMetodoParaBorrarLaCuarentena vigila la CAPACIDAD, no el uso.
//
// Es el reemplazo de lo que compraba la lista blanca. Un barrido de llamadas
// caza a quien la borre hoy; esto caza a quien haga posible borrarla mañana. La
// capacidad vive en la interfaz: lo que `port.FirewallPort` no declara, ningún
// adaptador puede ofrecer y ningún caso de uso puede llamar.
func TestNoExisteUnMetodoParaBorrarLaCuarentena(t *testing.T) {
	métodos := métodosDeInterfaz(t, "../../core/port", "FirewallPort")
	if len(métodos) == 0 {
		t.Fatal("no se encontró la interfaz FirewallPort, así que este test no probaría nada")
	}

	for _, m := range métodos {
		bajo := strings.ToLower(m)
		if !strings.Contains(bajo, "base") && !strings.Contains(bajo, "quarantine") {
			continue
		}
		for _, verbo := range verbosDestructivos {
			if strings.Contains(bajo, strings.ToLower(verbo)) {
				t.Errorf("FirewallPort declara %q.\n"+
					"  La cuarentena vale porque sigue puesta con el daemon parado, así que la\n"+
					"  capacidad de quitarla no puede vivir en la interfaz que el daemon usa.\n"+
					"  Quitarla es del desinstalador, que no habla por este puerto.", m)
			}
		}
	}
}

// verbosDestructivos son las formas de quitar algo, en el inglés que usa el
// código de adaptadores.
var verbosDestructivos = []string{"Remove", "RemoveAll", "Delete", "Purge", "Disable", "Clear", "Drop", "Reset"}

func permitido(ruta string) bool {
	return strings.Contains(filepath.ToSlash(ruta), paqueteDeLaCuarentena)
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

// llamadaDestructiva es un sitio donde se llama a un verbo destructivo con el
// grupo base entre los argumentos.
type llamadaDestructiva struct {
	ruta  string
	verbo string
}

// destructivasConGrupoBase busca por AST cualquier llamada a un verbo
// destructivo que lleve el grupo de la cuarentena como argumento.
//
// Mira los argumentos y no el receptor a propósito. Da igual sobre qué objeto se
// llame: lo que convierte la llamada en un borrado de la cuarentena es que el
// grupo base viaje dentro, y eso se ve igual en `rules.Remove(grupo)` que en
// `f.purge(ctx, domain.FirewallGroupBase)`.
func destructivasConGrupoBase(t *testing.T, raíz string) []llamadaDestructiva {
	t.Helper()
	verbo := make(map[string]bool, len(verbosDestructivos))
	for _, v := range verbosDestructivos {
		verbo[v] = true
	}

	var out []llamadaDestructiva
	porArchivo(t, raíz, func(ruta string, archivo *ast.File) {
		ast.Inspect(archivo, func(n ast.Node) bool {
			llamada, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			nombre := nombreLlamado(llamada.Fun)
			if !verbo[nombre] {
				return true
			}
			for _, arg := range llamada.Args {
				if nombraElGrupoBase(arg) {
					out = append(out, llamadaDestructiva{ruta: ruta, verbo: nombre})
					return false
				}
			}
			return true
		})
	})
	return out
}

func nombreLlamado(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.SelectorExpr:
		return v.Sel.Name
	case *ast.Ident:
		return v.Name
	}
	return ""
}

// nombraElGrupoBase dice si la expresión es el grupo de la cuarentena, como
// constante del dominio o como literal.
func nombraElGrupoBase(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.SelectorExpr:
		return v.Sel.Name == "FirewallGroupBase"
	case *ast.Ident:
		return v.Name == "FirewallGroupBase"
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return false
		}
		return strings.EqualFold(strings.Trim(v.Value, `"`), domain.FirewallGroupBase)
	}
	return false
}

// métodosDeInterfaz devuelve los nombres de los métodos que declara una interfaz.
func métodosDeInterfaz(t *testing.T, raíz, nombre string) []string {
	t.Helper()
	var out []string

	porArchivo(t, raíz, func(_ string, archivo *ast.File) {
		ast.Inspect(archivo, func(n ast.Node) bool {
			spec, ok := n.(*ast.TypeSpec)
			if !ok || spec.Name.Name != nombre {
				return true
			}
			iface, ok := spec.Type.(*ast.InterfaceType)
			if !ok {
				return true
			}
			for _, campo := range iface.Methods.List {
				for _, id := range campo.Names {
					out = append(out, id.Name)
				}
			}
			return false
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
