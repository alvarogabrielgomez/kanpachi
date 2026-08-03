package arch

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// El guardián de cómo se arranca el motor.
//
// # Por qué existe, y por qué existe ANTES que el adaptador
//
// `01-que-es-kanpachi.md`, la decisión 1 y `docs/CLAUDE.md` prometen que todo
// arranque de `easytier-core` lleva `--disable-upnp true` y que hay un test que
// falla si alguien lo saca. Esa frase era cierta **solo para el seed**, que sí lo
// comprueba en `registry/setup`. Para el cliente no había nada.
//
// # Lo que este guardián puede y lo que NO puede
//
// Puede afirmar que en `daemon/` no aparece ninguna bandera prohibida. Eso vale
// para todo el árbol y no depende de dónde aterrice el adaptador, que fue el
// primer agujero de la versión anterior: buscaba en una ruta fija, así que un
// adaptador en un directorio hermano lo dejaba saltando para siempre con el CI
// en verde.
//
// **No puede afirmar el valor de una bandera en el caso general.** Lee cadenas
// literales, y `--disable-upnp` seguido de `"false"` en dos literales distintos
// era exactamente igual de verde que el arranque correcto. Se cubre lo que sí se
// puede: dentro de un literal de slice, que es como se arma un argv, se miran los
// pares en ORDEN. Y para lo demás, la garantía de valor vive donde tiene que
// vivir, en el test del propio adaptador, que este guardián exige que exista.
//
// La revisión adversarial que encontró todo esto también dejó claro para qué NO
// sirve la lista de nodos públicos: vigila lo que escribimos nosotros, jamás lo
// que manda un desconocido. Un código de invitación puede nombrar cualquier
// host, `public.easytier.cn` incluido, y contra eso no hay lista que sirva. Lo
// que acota ese caso es que la UI resalta un seed que no es el por defecto y la
// confirmación de la decisión 17.

// banderasProhibidas expresan capacidades que el producto no tiene.
//
// La lista salió de leer la struct `Cli` del fuente de la v2.6.4, que es la
// versión fijada, y no del texto de ayuda. Ahí aparecieron cuatro que este
// guardián no vigilaba y que rompen invariantes igual de fuerte que las que sí.
var banderasProhibidas = map[string]string{
	"--enable-exit-node": "jamás exit node",
	"--exit-nodes":       "jamás exit node",
	"--proxy-networks":   "jamás subnet routing",
	"--vpn-portal":       "nada escucha en público",
	"--socks5":           "nada escucha en público",
	"--accept-dns":       "el DNS de la máquina no se toca",
	"--listeners": "deshace --no-listener, que es la invariante de que el cliente " +
		"jamás escucha en un puerto público. Solo el seed escucha",
	"--enable-udp-broadcast-relay": "es capturar el tráfico de la red de casa del usuario " +
		"con un driver de captura de paquetes. La decisión 1 lo difiere hasta que exista un juego que lo pida",

	// Las cuatro que faltaban.
	"--config-server": "haría que el motor se traiga su configuración de un servidor remoto, " +
		"o sea que lo que corre en la máquina del usuario deja de decidirlo Kanpachi",
	"--external-node": "su propia ayuda dice `use a public shared node to discover peers`, " +
		"y Kanpachi apunta a su propio seed y jamás a un nodo público",
	"--port-forward": "reenvía un puerto local hacia la red virtual POR DEBAJO del cálculo de " +
		"reglas, así que abriría algo que el módulo de exposición no ve ni puede auditar",
	"--mapped-listeners": "publica direcciones alcanzables desde fuera, que es lo mismo que " +
		"escuchar en público con otro nombre",
}

// paresProhibidos son banderas que pierden todo su sentido con el valor
// equivocado. Se comprueban por posición dentro de un literal de slice.
var paresProhibidos = map[string]string{
	"--disable-upnp": "false",
	"--no-listener":  "false",
}

// paresObligatorios exigen que el VALOR de una bandera contenga algo.
//
// Existe por un agujero medido, no imaginado. `banderasObligatorias` pedía el
// literal `127.0.0.1` en algún lugar del paquete, y eso pasaba en verde con
// `--rpc-portal 15888`, que es exactamente el arranque malo:
//
//	// easytier/src/rpc_service/api.rs:178-181, en el tag v2.6.4
//	if let Some(Ok(port)) = rpc_portal.as_ref().map(|s| s.parse::<u16>()) {
//	    Ok(SocketAddr::from(([0, 0, 0, 0], port)))
//
// Un puerto suelto escucha en TODAS las interfaces. Comprobado con netstat
// contra el binario fijado, con un fichero de configuración que no nombra el
// portal en ninguna parte: `TCP 0.0.0.0:15888 LISTENING`. El texto de ayuda
// oficial dice `localhost` y el binario hace otra cosa.
var paresObligatorios = map[string]string{
	"--rpc-portal": "127.0.0.1:",
}

// banderasObligatorias son las que no pueden faltar donde se arma el arranque.
var banderasObligatorias = map[string]string{
	"--disable-upnp": "EasyTier mapea puertos en el router del usuario por defecto, " +
		"y la invariante dice que el router no se toca nunca",
	"--no-listener": "el cliente jamás escucha en un puerto público. Solo el seed escucha",
	"--rpc-portal": "sin la bandera el motor arma `0.0.0.0:0` y busca puerto libre entre 15888 " +
		"y 15900, o sea que el panel de control del motor queda expuesto a la LAN de casa",
}

// nodosPúblicos son los peers compartidos de EasyTier. Vigila NUESTRO código.
var nodosPúblicos = []string{"easytier.top", "public.easytier", "easytier.cn"}

// TestEnElDaemonNoApareceNingunaBanderaProhibida.
//
// Barre `daemon/` entero, así que no hay dónde esconder el adaptador del motor.
func TestEnElDaemonNoApareceNingunaBanderaProhibida(t *testing.T) {
	archivos := literalesPorArchivo(t, "../../daemon")
	if len(archivos) == 0 {
		t.Fatal("no se leyó ni un archivo de daemon/, así que este test no probaría nada")
	}

	for ruta, literales := range archivos {
		for bandera, motivo := range banderasProhibidas {
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

// TestElArranqueDelMotorLlevaLoQueTieneQueLlevar.
//
// Encuentra el paquete del motor por su CONTENIDO y no por una ruta fija: el que
// mencione `easytier-core` es el que arranca el motor, viva donde viva.
func TestElArranqueDelMotorLlevaLoQueTieneQueLlevar(t *testing.T) {
	paquete := buscaElPaqueteDelMotor(t, "../../daemon")
	if paquete == "" {
		t.Skip("todavía no hay ningún paquete que mencione easytier-core. El guardián está " +
			"escrito antes a propósito, y el barrido de banderas prohibidas ya cubre daemon/ entero")
	}

	literales := literalesDe(t, paquete)
	for bandera, motivo := range banderasObligatorias {
		if !algunoContiene(literales, bandera) {
			t.Errorf("%s: falta %s en el arranque del motor: %s", paquete, bandera, motivo)
		}
	}

	// El valor, donde se puede verlo: los pares en orden dentro de un argv.
	pares := paresEnOrden(t, paquete)
	for _, par := range pares {
		if malo, vigilada := paresProhibidos[par.bandera]; vigilada && par.valor == malo {
			t.Errorf("%s: %s va con %q, que es lo mismo que no ponerla", paquete, par.bandera, par.valor)
		}
		if quiere, vigilada := paresObligatorios[par.bandera]; vigilada && !strings.Contains(par.valor, quiere) {
			t.Errorf("%s: %s va con %q, y tiene que contener %q.\n"+
				"  Un puerto suelto NO es loopback: el motor lo convierte en 0.0.0.0:<puerto> "+
				"y escucha en todas las interfaces", paquete, par.bandera, par.valor, quiere)
		}
	}

	// El entorno, que es la vía que este guardián no puede ver leyendo argv.
	//
	// Cada bandera de EasyTier tiene una gemela por variable de entorno:
	// `--config-server` es `ET_CONFIG_SERVER`, `--port-forward` es
	// `ET_PORT_FORWARD`, y así con todas. Un proceso hijo que HEREDA el entorno
	// acepta en silencio cualquiera de ellas, y la lista de banderas prohibidas
	// de arriba no se entera. `--disable-env-parsing` no lo tapa: su ayuda dice
	// `disable environment variable parsing in config file`, o sea la
	// interpolación dentro del fichero, que es otra cosa.
	//
	// La única defensa es armar el entorno del hijo a mano.
	if !asignaEntorno(t, paquete) {
		t.Errorf("%s arranca el motor sin fijarle el entorno.\n"+
			"  Cada bandera de EasyTier tiene gemela por variable de entorno, así que un hijo "+
			"que hereda el entorno acepta ET_CONFIG_SERVER, ET_PORT_FORWARD y las demás sin "+
			"que nadie las escriba en el argv.\n"+
			"  Asigna cmd.Env con una lista explícita, aunque sea vacía.", paquete)
	}

	// Y la garantía de valor de verdad, que este guardián no puede dar, vive en
	// el test del propio adaptador. Igual que el del seed, que evalúa lo que
	// GENERA la función en vez de mirar el fuente. Ver registry/setup/setup_test.go.
	if !hayTestDeArgumentos(t, paquete) {
		t.Errorf("%s no tiene un test de los argumentos del motor.\n"+
			"  Este guardián lee cadenas literales y no puede afirmar el valor de una bandera "+
			"en el caso general.\n"+
			"  El adaptador tiene que exponer la construcción del argv en Go PURO, sin build tags, "+
			"y afirmarla sobre el slice que devuelve, uno por cada camino de arranque.", paquete)
	}
}

// buscaElPaqueteDelMotor devuelve el directorio del paquete que arranca el
// motor, encontrado por lo que dice y no por dónde está.
func buscaElPaqueteDelMotor(t *testing.T, raíz string) string {
	t.Helper()
	var encontrado string

	for ruta, literales := range literalesPorArchivo(t, raíz) {
		if algunoContiene(literales, "easytier-core") {
			dir := filepath.Dir(ruta)
			if encontrado == "" || len(dir) < len(encontrado) {
				encontrado = dir
			}
		}
	}
	return encontrado
}

type parDeArgumentos struct{ bandera, valor string }

// paresEnOrden saca las parejas consecutivas de un literal de slice, que es como
// se arma una lista de argumentos.
func paresEnOrden(t *testing.T, dir string) []parDeArgumentos {
	t.Helper()
	var out []parDeArgumentos

	porArchivo(t, dir, func(_ string, archivo *ast.File) {
		ast.Inspect(archivo, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			cadenas := make([]string, 0, len(lit.Elts))
			for _, e := range lit.Elts {
				b, ok := e.(*ast.BasicLit)
				if !ok || b.Kind != token.STRING {
					cadenas = append(cadenas, "")
					continue
				}
				v, err := strconv.Unquote(b.Value)
				if err != nil {
					v = b.Value
				}
				cadenas = append(cadenas, v)
			}
			for i := 0; i+1 < len(cadenas); i++ {
				out = append(out, parDeArgumentos{bandera: cadenas[i], valor: cadenas[i+1]})
			}
			return true
		})
	})
	return out
}

// asignaEntorno dice si el paquete le fija el entorno a algo, o sea si en algún
// lado hay un `algo.Env = ...`.
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

// hayTestDeArgumentos dice si el paquete tiene un test que mire los argumentos.
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
// dispare. Los tests quedan fuera por lo mismo: uno que afirme la ausencia de una
// bandera tiene que poder nombrarla.
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
