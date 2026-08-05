package arch

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// El guardián de cómo se arranca el motor.
//
// # Lo que cambió, y por qué había que reescribirlo entero
//
// Este fichero estuvo en VERDE sin comprobar nada del motor. Tres defectos, cada
// uno suficiente por su cuenta:
//
//  1. Localizaba el paquete buscando el literal `"easytier-core"`. El motor es
//     un binario propio, `kanpachi-engine.exe`, así que el `t.Skip` duraba para
//     siempre. Un skip eterno es un test que miente.
//  2. Exigía `--rpc-portal 127.0.0.1:` en el argv. El motor propio no recibe
//     argumentos, y el campo `rpc_portal` **desapareció del TOML en la v2.5.0**:
//     hoy es imposible de satisfacer y además inútil.
//  3. Buscaba las prohibidas con sus guiones, `--enable-exit-node`. En una
//     configuración se escriben sin ellos, `enable_exit_node`, así que el
//     barrido pasaba en verde por encima de lo que existe para impedir.
//
// # Dónde vive ahora cada garantía, dicho sin adornos
//
// El diseño movió una parte de la responsabilidad al repositorio del motor, y
// este guardián NO puede alcanzarla. Conviene que esté escrito para que nadie
// crea que sigue cubierta desde acá:
//
//   - **Que el arranque lleve `disable_upnp` en verdadero y las prohibidas en
//     falso**: eso lo decide `src/config.rs` del motor, en Rust y con setters
//     tipados, y lo vigila su propio CI. Desde Go no hay nada que leer.
//   - **Que el motor no abra ningún puerto**: lo mide su test de invariante de
//     sockets, arrancando con la configuración real y el TUN levantado.
//
// # Lo que este guardián SÍ puede afirmar, y afirma
//
//   - Que en `daemon/` no aparece ninguna capacidad prohibida, **en las dos
//     grafías**, con guiones y sin ellos.
//   - Que el paquete del motor no nombra `rpc_portal` de ninguna forma.
//   - Que el adaptador **no escucha**: habla con el motor, nunca por él.
//   - Que le arma el entorno al hijo en vez de heredarlo.
//   - Que existe un test que evalúa lo que la construcción GENERA.

// prohibidas son capacidades que el producto no tiene.
//
// **Cada una en sus dos grafías, y eso no es celo de más.** El cliente dejó de
// usar argv: el motor recibe su configuración por el tubo y la traduce a claves
// sin guiones. Un guardián que solo buscara `--enable-exit-node` pasaría en
// verde por encima de un `enable_exit_node` puesto en cualquier parte del
// daemon.
var prohibidas = map[string]string{
	"--enable-exit-node": "jamás exit node",
	"enable_exit_node":   "jamás exit node",
	"--exit-nodes":       "jamás exit node",
	"exit_nodes":         "jamás exit node",
	"--proxy-networks":   "jamás subnet routing",
	"proxy_network":      "jamás subnet routing",
	"--vpn-portal":       "nada escucha en público",
	"vpn_portal_config":  "nada escucha en público",
	"--socks5":           "superficie sin razón",
	"socks5_proxy":       "superficie sin razón",
	"--accept-dns":       "el DNS de la máquina no se toca, y magic DNS abre un puerto de loopback",
	"accept_dns":         "el DNS de la máquina no se toca, y magic DNS abre un puerto de loopback",
	"--listeners": "deshace --no-listener, que es la invariante de que el cliente " +
		"jamás escucha en un puerto público. Solo el seed escucha",
	"--enable-udp-broadcast-relay": "es capturar el tráfico de la red de casa del usuario " +
		"con un driver de captura de paquetes",
	"enable_udp_broadcast_relay": "es capturar el tráfico de la red de casa del usuario " +
		"con un driver de captura de paquetes",
	"--config-server": "haría que el motor se traiga su configuración de un servidor remoto, " +
		"o sea que lo que corre en la máquina del usuario deja de decidirlo Kanpachi",
	"config_server": "el motor dejaría de tomar su configuración de Kanpachi",
	"--external-node": "su ayuda dice `use a public shared node to discover peers`, " +
		"y Kanpachi apunta a su propio seed",
	"--port-forward": "reenvía un puerto local hacia la red virtual POR DEBAJO del cálculo de " +
		"reglas, así que abriría algo que el módulo de exposición no ve ni puede auditar",
	"port_forward":       "abriría un puerto que el módulo de exposición no ve ni puede auditar",
	"--mapped-listeners": "publica direcciones alcanzables desde fuera",
	"mapped_listeners":   "publica direcciones alcanzables desde fuera",
}

// escuchas son las formas de abrir un canal de entrada.
//
// **El adaptador del motor no puede tener ninguna.** El motor tiene UNA sola
// entrada de órdenes, el tubo del proceso hijo, y ese tubo es anónimo: no tiene
// nombre, no tiene ruta, y no existe la operación de conectarse a él. Un segundo
// canal añadido de buena fe reconstruye el portal 15888 con otro nombre.
//
// Las banderas prohibidas vigilan lo que el motor HACE. Esto vigila por dónde se
// le puede MANDAR, que no lo miraba nadie.
var escuchas = []string{
	"net.Listen",
	"net.ListenPacket",
	"ListenPipe",
	"CreateNamedPipe",
	"ListenConfig",
}

// nodosPúblicos son los peers compartidos de EasyTier. Vigila NUESTRO código.
//
// No sirve, y conviene recordarlo, contra lo que manda un desconocido: un código
// de invitación puede nombrar cualquier host. Eso lo acota que la UI resalte un
// seed que no es el por defecto, más `domain.CheckSeedAddr` sobre lo resuelto.
var nodosPúblicos = []string{"easytier.top", "public.easytier", "easytier.cn"}

// TestNoForbiddenCapabilityAppearsInTheDaemon barre `daemon/` entero, así que no
// hay dónde esconder el adaptador del motor.
func TestNoForbiddenCapabilityAppearsInTheDaemon(t *testing.T) {
	archivos := literalesPorArchivo(t, "../../daemon")
	if len(archivos) == 0 {
		t.Fatal("no se leyó ni un archivo de daemon/, así que este test no probaría nada")
	}

	for ruta, literales := range archivos {
		for bandera, motivo := range prohibidas {
			if lit, ok := buscaLiteral(literales, bandera); ok {
				t.Errorf("%s: apareció %s en %q: %s", ruta, bandera, lit, motivo)
			}
		}
		visto := map[string]bool{}
		for _, nodo := range nodosPúblicos {
			if lit, ok := buscaLiteral(literales, nodo); ok && !visto[lit] {
				visto[lit] = true
				t.Errorf("%s: apareció un peer público de EasyTier en %q. Kanpachi apunta a su propio seed", ruta, lit)
			}
		}
	}
}

// TestTheEngineAdapterExistsAndDoesNotListen.
//
// Falla, y no salta, cuando no encuentra el paquete. La versión anterior saltaba
// y por eso pasó meses sin comprobar nada: si el adaptador cambia de nombre, lo
// correcto es que este test se rompa a gritos y no que se apague solo.
func TestTheEngineAdapterExistsAndDoesNotListen(t *testing.T) {
	paquete := buscaElPaqueteDelMotor(t, "../../daemon")
	if paquete == "" {
		t.Fatal("ningún paquete de daemon/ ejecuta `kanpachi-engine`.\n" +
			"  O el adaptador del motor desapareció, o cambió el nombre del binario y este " +
			"guardián dejó de encontrarlo.\n" +
			"  Las dos cosas hay que mirarlas: un guardián que no encuentra a quién vigilar " +
			"no es un guardián.")
	}

	literales := literalesDe(t, paquete)

	// El portal RPC no se configura, no existe. `ApiRpcServer::new` se construye
	// en un solo sitio del árbol de EasyTier, dentro de su binario de línea de
	// comandos, y el arranque por librería no lo nombra. Además el campo salió
	// del TOML en la v2.5.0 y `Config` no lleva `deny_unknown_fields`, así que
	// escribirlo no da error: lo ignora en silencio. Nombrarlo acá solo puede
	// significar que alguien creyó estar acotándolo.
	for _, aguja := range []string{"rpc_portal", "rpc-portal", "15888"} {
		if lit, ok := buscaLiteral(literales, aguja); ok {
			t.Errorf("%s nombra %q en %q.\n"+
				"  El motor propio NO abre el portal, y el campo ya no existe en la "+
				"configuración: ponerlo se ignora sin avisar.", paquete, aguja, lit)
		}
	}

	// Lo que este guardián puede ver de la ÚNICA entrada de órdenes.
	for _, l := range escuchas {
		if hayLlamada(t, paquete, l) {
			t.Errorf("%s construye %s.\n"+
				"  El motor tiene una sola entrada de órdenes, el tubo del proceso hijo, y es "+
				"anónimo: no tiene nombre ni dirección, así que no existe la operación de "+
				"conectarse a él.\n"+
				"  Un segundo canal reconstruye el portal sin autenticación con otro nombre.", paquete, l)
		}
	}

	// El entorno, que es la vía que no se ve leyendo argv.
	//
	// Cada bandera de EasyTier tiene gemela por variable de entorno:
	// `--config-server` es `ET_CONFIG_SERVER`, `--port-forward` es
	// `ET_PORT_FORWARD`. Un hijo que HEREDA el entorno acepta en silencio
	// cualquiera de ellas, y la lista de prohibidas de arriba no se entera.
	// `--disable-env-parsing` no lo tapa: su ayuda dice `in config file`, o sea
	// la interpolación dentro del fichero, que es otra cosa.
	if !asignaEntorno(t, paquete) {
		t.Errorf("%s arranca el motor sin fijarle el entorno.\n"+
			"  Asigna cmd.Env con una lista explícita, aunque sea vacía. Una lista vacía "+
			"cuenta, y es lo correcto acá.", paquete)
	}

	// Y la garantía de valor de verdad, que este guardián no puede dar porque lee
	// cadenas literales: vive en el test del propio adaptador, que evalúa lo que
	// la construcción GENERA. Mismo patrón que registry/setup/setup_test.go.
	if !hayTestDeArgumentos(t, paquete) {
		t.Errorf("%s no tiene un test de lo que le manda al motor.\n"+
			"  Este guardián lee literales y no puede afirmar el valor de nada en el caso "+
			"general.\n"+
			"  El adaptador tiene que construir la orden en Go PURO, sin build tags, y "+
			"afirmarla sobre lo que devuelve, uno por cada camino de arranque.", paquete)
	}
}

// buscaElPaqueteDelMotor devuelve el directorio del adaptador del motor,
// encontrado por lo que ES y no por dónde está ni por lo que nombra.
//
// # Por qué no vale buscar el nombre del binario
//
// La versión anterior buscaba un literal y se quedaba con el directorio más
// corto que lo tuviera. Con `main.go` nombrando la ruta del ejecutable para
// cablear el adaptador, eso apunta al paquete del `main`, que no es quien
// conduce el motor. El test entonces exige cosas donde no tienen que estar y
// deja sin mirar el sitio que importa, que es la peor forma de fallar: ruidosa y
// en el sitio equivocado.
//
// # El criterio que sí distingue
//
// Dos hechos a la vez, y ninguno de los dos solo:
//
//   - **Declara los métodos de `port.EnginePort`.** Eso lo cumple también
//     `sinimplementar`, que lo implementa devolviendo error en todo.
//   - **Lanza un proceso.** Eso es lo que separa al adaptador de verdad del
//     provisional, y de cualquier otro que aparezca mañana.
//
// Juntos identifican una cosa: el paquete que implementa el motor ejecutándolo.
func buscaElPaqueteDelMotor(t *testing.T, raíz string) string {
	t.Helper()

	implementa := map[string]bool{}
	lanza := map[string]bool{}

	porArchivo(t, raíz, func(ruta string, archivo *ast.File) {
		dir := filepath.Dir(ruta)
		for _, d := range archivo.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Recv == nil {
				continue
			}
			// `JoinWithCredential` es el método más distintivo de EnginePort:
			// no existe en ningún otro puerto del proyecto.
			if fn.Name.Name == "JoinWithCredential" {
				implementa[dir] = true
			}
		}
		ast.Inspect(archivo, func(n ast.Node) bool {
			llamada, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if strings.HasPrefix(expresión(llamada.Fun), "exec.Command") {
				lanza[dir] = true
			}
			return true
		})
	})

	for dir := range implementa {
		if lanza[dir] {
			return dir
		}
	}
	return ""
}

// hayLlamada dice si el paquete llama a algo cuyo nombre termine en `aguja`.
//
// Por AST y sobre la EXPRESIÓN llamada, no por texto: así un comentario que diga
// "acá jamás un net.Listen" no lo dispara, y una cadena que lo nombre tampoco.
func hayLlamada(t *testing.T, dir, aguja string) bool {
	t.Helper()
	var encontrado bool

	porArchivo(t, dir, func(_ string, archivo *ast.File) {
		ast.Inspect(archivo, func(n ast.Node) bool {
			llamada, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if strings.HasSuffix(expresión(llamada.Fun), aguja) {
				encontrado = true
			}
			return true
		})
	})
	return encontrado
}

// expresión rearma el nombre de lo que se llama, `net.Listen` o `Listen`.
func expresión(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.SelectorExpr:
		return expresión(v.X) + "." + v.Sel.Name
	default:
		return ""
	}
}

// asignaEntorno dice si el paquete le fija el entorno a algo.
//
// Por AST y no por texto: mira que el lado IZQUIERDO de una asignación sea un
// campo llamado `Env`, que es como se le pone el entorno a un `exec.Cmd`. Una
// lista vacía cuenta, y es justo lo que hay que poner cuando el hijo no necesita
// ninguna variable.
func asignaEntorno(t *testing.T, dir string) bool {
	t.Helper()
	var encontrado bool

	porArchivo(t, dir, func(_ string, archivo *ast.File) {
		ast.Inspect(archivo, func(n ast.Node) bool {
			asig, ok := n.(*ast.AssignStmt)
			if !ok {
				return true
			}
			for _, izq := range asig.Lhs {
				if sel, ok := izq.(*ast.SelectorExpr); ok && sel.Sel.Name == "Env" {
					encontrado = true
				}
			}
			return true
		})
		// También vale construirlo de una, con `exec.Cmd{..., Env: ...}`.
		ast.Inspect(archivo, func(n ast.Node) bool {
			kv, ok := n.(*ast.KeyValueExpr)
			if !ok {
				return true
			}
			if id, ok := kv.Key.(*ast.Ident); ok && id.Name == "Env" {
				encontrado = true
			}
			return true
		})
	})
	return encontrado
}

// hayTestDeArgumentos dice si el paquete tiene un test que mire lo que se manda.
func hayTestDeArgumentos(t *testing.T, dir string) bool {
	t.Helper()
	entradas, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entradas {
		n := strings.ToLower(e.Name())
		if strings.HasSuffix(n, "_test.go") && (strings.Contains(n, "arg") || strings.Contains(n, "spec")) {
			return true
		}
	}
	return false
}

// literalesDe junta las cadenas literales de un directorio, sin sus tests.
func literalesDe(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	for _, ls := range literalesPorArchivo(t, dir) {
		out = append(out, ls...)
	}
	return out
}

// literalesPorArchivo lee el árbol y devuelve las cadenas literales de cada
// archivo que no sea un test.
//
// Por AST y no por texto, para que un comentario que diga "jamás --socks5" no lo
// dispare. Los tests quedan fuera por lo mismo: uno que afirme la ausencia de
// una bandera tiene que poder nombrarla.
func literalesPorArchivo(t *testing.T, raíz string) map[string][]string {
	t.Helper()
	out := map[string][]string{}

	porArchivo(t, raíz, func(ruta string, archivo *ast.File) {
		var ls []string
		ast.Inspect(archivo, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if ok && lit.Kind == token.STRING {
				ls = append(ls, strings.ToLower(lit.Value))
			}
			return true
		})
		if len(ls) > 0 {
			out[filepath.ToSlash(ruta)] = ls
		}
	})
	return out
}

func porArchivo(t *testing.T, raíz string, fn func(ruta string, archivo *ast.File)) {
	t.Helper()
	if _, err := os.Stat(raíz); os.IsNotExist(err) {
		return
	}
	err := filepath.WalkDir(raíz, func(ruta string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if !strings.HasSuffix(ruta, ".go") || strings.HasSuffix(ruta, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		archivo, err := parser.ParseFile(fset, ruta, nil, 0)
		if err != nil {
			return err
		}
		fn(ruta, archivo)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func algunoContiene(literales []string, aguja string) bool {
	_, ok := buscaLiteral(literales, aguja)
	return ok
}

func buscaLiteral(literales []string, aguja string) (string, bool) {
	for _, l := range literales {
		if strings.Contains(l, aguja) {
			return l, true
		}
	}
	return "", false
}
