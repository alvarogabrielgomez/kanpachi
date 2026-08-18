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
// # Qué protege, y qué cambió, dos veces
//
// Antes esto decía "el daemon jamás nombra el grupo base", porque la cuarentena
// la iba a poner un instalador. **No hay instalador**, así que la escribe y la
// repone el daemon, y para eso tiene que nombrarlo.
//
// Después decía "el daemon jamás la BORRA", con un solo borrador legítimo, el
// desinstalador. Eso también cambió: la cuarentena pasó a ser una decisión del
// usuario (la decisión nueva de docs/02), y decir que no ES quitarla. La
// doctrina vigente es esta:
//
// **Quitar la cuarentena es siempre el acto de una persona, jamás de un camino
// automático.** Hay exactamente DOS actos de persona: desinstalar, y la
// decisión explícita del usuario de no tenerla. Un barrido, un reinicio del
// servicio, un `--reset`, un evento del motor: ninguno puede quitarla, hoy ni
// mañana, y este archivo es lo que lo mantiene cierto. Cada capacidad tiene UN
// nombre cerrado y UNA lista cerrada de llamadores, y las listas son la
// comprobación.
//
// Y la mitad que no cambió nunca: **jamás se compara el grupo por prefijo.**
// Lo que hace valiosa a la cuarentena sigue siendo que se queda puesta con el
// servicio detenido, deshabilitado o a medio desinstalar, salvo que una
// persona diga lo contrario.
//
// # Las tres formas de romperlo, y las tres se ven acá
//
// La primera es que el grupo se nombre desde cualquier sitio del daemon. Se
// permite en DOS paquetes, uno por sistema, y en ninguno más, así que el radio
// de lo que puede tocarlo se lee en una línea y el guardián conserva los
// dientes en todo el resto.
//
// La segunda es un borrado fuera de los dos nombres cerrados: una llamada
// destructiva con el grupo entre sus argumentos, una función que nombre el
// grupo y borre sin llamarse como una de las dos, o un llamador que no esté en
// la lista de esa capacidad. Se busca por AST.
//
// La tercera es más silenciosa: `FirewallGroup` es PREFIJO de
// `FirewallGroupBase`, así que una purga escrita con HasPrefix en vez de
// igualdad se lleva la cuarentena por delante. Nadie lo notaría, porque con la
// cuarentena borrada todo sigue funcionando igual, solo que la máquina queda
// expuesta. Ese es el peor modo de fallo posible en este proyecto: silencioso y
// con la pantalla en verde.

// paquetesDeLaCuarentena son los ÚNICOS de daemon/ que pueden nombrar el grupo
// base: uno por sistema, y ninguno más.
//
// Son dos y no uno desde que existe Linux, y la lista sigue siendo del mismo
// tamaño que implementaciones hay. Cada entrada es un prefijo de ruta, así que
// cubre el archivo puro y sus hermanos con etiqueta sin cubrir nada más.
// Ampliar esto es una decisión que se ve en el diff, y lo que hay que
// preguntarse al ampliarlo es si el paquete nuevo IMPLEMENTA la cuarentena o
// solo quiere leerla: para escribirla está `ApplyBaseQuarantine` del puerto, y
// para comprobar si está puesta, `ExposureAudit.QuarantineState`, que la mide
// del sistema en las dos plataformas.
var paquetesDeLaCuarentena = []string{
	"daemon/adapter/firewall/windows/netfw/",
	"daemon/adapter/firewall/linux/nftpermits/",
}

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
				"  Solo %s pueden nombrarlo. Nombrarlo es todo lo que hace falta para tocarlo,\n"+
				"  y la cuarentena es lo único que protege la máquina con el servicio parado.",
				ruta, lit, strings.Join(paquetesDeLaCuarentena, " y "))
		}
	}

	for _, ruta := range identificadoresEn(t, "../../daemon", identificador) {
		if permitido(ruta) {
			continue
		}
		t.Errorf("%s: usa domain.%s.\n"+
			"  Solo %s pueden nombrarlo.", ruta, identificador, strings.Join(paquetesDeLaCuarentena, " y "))
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

// TestLaInterfazQuitaLaCuarentenaDeUnaSolaForma vigila la CAPACIDAD, no el uso.
//
// Un barrido de llamadas caza a quien la borre hoy; esto caza a quien haga
// posible borrarla mañana por otro nombre. La capacidad vive en la interfaz:
// lo que `port.FirewallPort` no declara, ningún adaptador puede ofrecer y
// ningún caso de uso puede llamar.
//
// # Qué compraba antes, y qué compra ahora
//
// Antes exigía que NINGÚN método juntara un verbo destructivo con la
// cuarentena, y su doctrina era "lo que no existe en la interfaz no se puede
// llamar por error". Desde que la cuarentena es una decisión del usuario, el
// método tiene que existir: decir que no ES quitarla. Lo que se exige ahora es
// que exista DE UNA SOLA FORMA, con el nombre exacto de la retirada a pedido,
// y que ningún otro método de la interfaz pueda quitarla. El error por el que
// existía el guardián sigue siendo imposible: un `PurgeBase` nuevo, con otro
// nombre y otros llamadores, se pone rojo acá.
//
// La existencia del método no se exige todavía: el nombre se fija ANTES que la
// capacidad a propósito, porque primero se decide qué se permite y después se
// escribe lo permitido. El día que el frente de la decisión lo declare, este
// test lo cubre sin tocarse.
func TestLaInterfazQuitaLaCuarentenaDeUnaSolaForma(t *testing.T) {
	métodos := métodosDeInterfaz(t, "../../core/port", "FirewallPort")
	if len(métodos) == 0 {
		t.Fatal("no se encontró la interfaz FirewallPort, así que este test no probaría nada")
	}

	for _, m := range métodos {
		bajo := strings.ToLower(m)
		if !strings.Contains(bajo, "base") && !strings.Contains(bajo, "quarantine") {
			continue
		}
		destructivo := false
		for _, verbo := range verbosDestructivos {
			if strings.Contains(bajo, strings.ToLower(verbo)) {
				destructivo = true
			}
		}
		if !destructivo || m == funciónDeLaRetiradaAPedido {
			continue
		}
		t.Errorf("FirewallPort declara %q.\n"+
			"  Quitar la cuarentena por este puerto existe de UNA sola forma, %s,\n"+
			"  que es la decisión explícita del usuario y la llama solo su caso de uso.\n"+
			"  Un segundo método con otro nombre es otra puerta con otros llamadores,\n"+
			"  y por ahí es como la cuarentena se cae sin que una persona lo pida.",
			m, funciónDeLaRetiradaAPedido)
	}
}

// verbosDestructivos son las formas de quitar algo, en el inglés que usa el
// código de adaptadores.
var verbosDestructivos = []string{"Remove", "RemoveAll", "Delete", "Purge", "Disable", "Clear", "Drop", "Reset"}

func permitido(ruta string) bool {
	limpia := filepath.ToSlash(ruta)
	for _, p := range paquetesDeLaCuarentena {
		if strings.Contains(limpia, p) {
			return true
		}
	}
	return false
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

// Las DOS funciones que pueden borrar la cuarentena de base, y no hay tercera.
//
// Una por acto de persona: desinstalar, y la decisión explícita del usuario de
// no tenerla. Los nombres son largos a propósito: aparecen enteros en
// cualquier búsqueda y en cualquier diff, así que ampliar este permiso no se
// puede hacer sin que se vea. Y cada una tiene su PROPIA lista de llamadores,
// abajo, porque compartirla sería dejar que el desinstalador se llame desde un
// caso de uso o la retirada desde el cableado.
const (
	funciónDelDesinstalador = "RemoveBaseQuarantineForUninstall"
	// funciónDeLaRetiradaAPedido no existe todavía: la trae el frente de la
	// decisión del usuario, y el nombre se fija acá ANTES que la capacidad.
	// Es el mismo nombre en el puerto, en el compuesto y en los adaptadores,
	// para que la búsqueda de quién puede quitar la cuarentena sea UNA.
	funciónDeLaRetiradaAPedido = "RemoveBaseQuarantineAtUserRequest"
)

// TestSoloLasDosRetiradasBorranLaCuarentena, y este guardián existe porque el
// de las llamadas destructivas NO alcanzaba.
//
// # El agujero que tenía el guardián de al lado
//
// `TestElDaemonNoBorraLaCuarentenaDeBase` busca llamadas cuyo NOMBRE sea un
// verbo destructivo y que lleven el grupo base entre sus ARGUMENTOS. Ninguna de
// las dos cosas pasa en el código real: dentro de `netfw` se borra llamando
// a `retire`, que es un helper propio, y el grupo no viaja como argumento, se
// compara contra el campo de una regla enumerada. O sea que una función nueva
// que se llevara la cuarentena pasaba en verde.
//
// Se comprobó escribiendo la función del desinstalador: el guardián no dijo
// nada. Un guardián que no muerde es peor que ninguno, porque se lee como una
// garantía.
//
// # Lo que vigila este
//
// La forma REAL: dentro del paquete permitido, una función que a la vez NOMBRE
// el grupo base y llame a algo que borra tiene que llamarse como una de las DOS
// retiradas, y nada más. Es una condición sobre la función entera y no sobre
// una llamada suelta, que es como se escribe de verdad un borrado por aquí. La
// lista de dos ES la comprobación: la tercera forma de quitar la cuarentena no
// puede escribirse sin ponerla roja.
//
// La existencia solo se exige de la del desinstalador, que ya está cableada.
// La retirada a pedido todavía no existe, y exigirla pondría rojo el árbol
// antes del frente que la trae.
func TestSoloLasDosRetiradasBorranLaCuarentena(t *testing.T) {
	borra := map[string]bool{"retire": true}
	for _, v := range verbosDestructivos {
		borra[v] = true
	}
	retiradas := map[string]bool{
		funciónDelDesinstalador:    true,
		funciónDeLaRetiradaAPedido: true,
	}

	visto := false
	porArchivo(t, "../../daemon", func(ruta string, archivo *ast.File) {
		if !permitido(ruta) {
			// Fuera del paquete permitido no se puede ni nombrar el grupo, y de
			// eso se encarga el otro guardián.
			return
		}
		for _, decl := range archivo.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			nombra, borrando := false, ""
			ast.Inspect(fn, func(n ast.Node) bool {
				if nombraElGrupoBase(exprDe(n)) {
					nombra = true
				}
				if llamada, ok := n.(*ast.CallExpr); ok {
					if v := nombreLlamado(llamada.Fun); borra[v] {
						borrando = v
					}
				}
				return true
			})
			if !nombra || borrando == "" {
				continue
			}
			if retiradas[fn.Name.Name] {
				if fn.Name.Name == funciónDelDesinstalador {
					visto = true
				}
				continue
			}
			t.Errorf("%s: la función %s nombra la cuarentena de base y llama a %s.\n"+
				"  Quitar la cuarentena es siempre el acto de una persona, y hay DOS:\n"+
				"  %s, que llama solo --uninstall-cleanup, y\n"+
				"  %s, que llama solo el caso de uso de la decisión del usuario.\n"+
				"  Una función con otro nombre es una tercera puerta, y no hay tercera.",
				ruta, fn.Name.Name, borrando, funciónDelDesinstalador, funciónDeLaRetiradaAPedido)
		}
	})

	if !visto {
		t.Errorf("no se encontró %s nombrando el grupo y borrando.\n"+
			"  O se renombró, o este guardián dejó de mirar donde vive, y en los dos casos\n"+
			"  está pasando en verde sobre algo que ya no comprueba.", funciónDelDesinstalador)
	}
}

// TestElDesinstaladorNoLoLlamaNadieMás: la función existe, y el radio de quien
// puede invocarla se lee en una línea.
//
// La capacidad del desinstalador sigue fuera de `port.FirewallPort`, así que
// ningún caso de uso puede pedirla. Esto cierra la otra mitad: que nadie del
// daemon la llame por la puerta de atrás, saltándose el puerto.
func TestElDesinstaladorNoLoLlamaNadieMás(t *testing.T) {
	const permitidoLlamar = "daemon/cmd/kanpachid/"

	llamantes := llamadoresDe(t, funciónDelDesinstalador, "../../daemon")
	if len(llamantes) == 0 {
		t.Fatalf("nadie llama a %s, así que este test no probaría nada.\n"+
			"  O se dejó de cablear el desinstalador, o se renombró.", funciónDelDesinstalador)
	}
	for ruta := range llamantes {
		if strings.Contains(ruta, permitidoLlamar) {
			continue
		}
		t.Errorf("%s: llama a %s.\n"+
			"  Solo el cableado de %s puede, y solo detrás de --uninstall-cleanup.\n"+
			"  Quitar la cuarentena por desinstalar no es una operación del producto.",
			ruta, funciónDelDesinstalador, permitidoLlamar)
	}
}

// TestLaRetiradaAPedidoSoloLaLlamaElConsentimiento es la mitad nueva del
// guardián de llamadores, y vigila justo lo que la capacidad nueva NO puede
// volverse: automática.
//
// La retirada a pedido tiene dos llamadores legítimos y ninguno más: el caso
// de uso del consentimiento, que es donde vive la decisión de la persona, y el
// compuesto del firewall, que es el único camino del puerto al adaptador. En
// particular NO puede llamarla el barrido de alertas, ningún arranque, ningún
// `--reset` y ningún evento del motor: si uno de esos la llama, la cuarentena
// se quita sola, y "se quita sola" es exactamente lo que este archivo existe
// para que no pase.
//
// Se barre `core` además de `daemon` a propósito: los caminos automáticos
// viven en `core/usecase`, y un guardián que no los mira no vigila nada.
//
// Cero llamadores hoy es verde: la capacidad todavía no existe, y el guardián
// se escribe ANTES que ella. El día que exista, la lista de abajo es la que
// decide desde dónde.
func TestLaRetiradaAPedidoSoloLaLlamaElConsentimiento(t *testing.T) {
	permitidos := []string{
		// El caso de uso del consentimiento, que es UN archivo y no el paquete:
		// en este mismo paquete viven el barrido y los arranques, o sea los
		// caminos automáticos que tienen prohibido llamarla.
		"core/usecase/quarantine.go",
		// El compuesto que enruta el puerto al adaptador de cada sistema.
		"daemon/adapter/firewall/hybrid.go",
	}

	for ruta := range llamadoresDe(t, funciónDeLaRetiradaAPedido, "../../core", "../../daemon") {
		ok := false
		for _, p := range permitidos {
			if strings.HasSuffix(ruta, p) {
				ok = true
			}
		}
		if ok {
			continue
		}
		t.Errorf("%s: llama a %s.\n"+
			"  Quitar la cuarentena a pedido es una decisión de la PERSONA, y sus dos\n"+
			"  únicos llamadores son %s y %s.\n"+
			"  Desde cualquier otro sitio, y en especial desde un barrido o un arranque,\n"+
			"  la cuarentena se estaría quitando sola.",
			ruta, funciónDeLaRetiradaAPedido, permitidos[0], permitidos[1])
	}
}

// llamadoresDe devuelve los archivos que llaman a una función, por nombre, en
// los árboles que se le den.
func llamadoresDe(t *testing.T, nombre string, raíces ...string) map[string]bool {
	t.Helper()
	llamantes := map[string]bool{}
	for _, raíz := range raíces {
		porArchivo(t, raíz, func(ruta string, archivo *ast.File) {
			ast.Inspect(archivo, func(n ast.Node) bool {
				llamada, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				if nombreLlamado(llamada.Fun) == nombre {
					llamantes[filepath.ToSlash(ruta)] = true
				}
				return true
			})
		})
	}
	return llamantes
}

// exprDe saca la expresión de un nodo, para poder reusar [nombraElGrupoBase]
// tanto sobre argumentos como sobre comparaciones.
func exprDe(n ast.Node) ast.Expr {
	e, _ := n.(ast.Expr)
	return e
}
