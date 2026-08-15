// Command kanpachi es el cliente: abre salas, entra, mira y mide.
//
// Es lo que se instala en `/usr/bin/kanpachi`, y en la variante headless es el
// ÚNICO cliente que hay. No hace nada por su cuenta: todo lo que ejecuta se lo
// pide al daemon por el canal local, con la misma lista cerrada de métodos que
// usa la interfaz de Windows. Un CLI que abriera un puerto por su cuenta sería
// una segunda puerta a la máquina, y la superficie chica es la mitigación
// principal de este producto.
//
// # Con subcomando es guionable; sin nada es un asistente
//
//	kanpachi status              qué hay ahora
//	kanpachi host "Mi sala"      abrir una
//	kanpachi join VA3B-SF5L      entrar
//	kanpachi                     el asistente, con flechas
//
// # Por qué pide `sudo` en Linux y no en Windows
//
// Porque el token vive en el directorio de datos y ahí los permisos son
// distintos a propósito. En Windows `ProgramData\Kanpachi` deja leer a los
// usuarios de la máquina, porque la interfaz corre sin elevar y necesita hablar.
// En Linux no hay interfaz que atender, así que el directorio es de root y el
// socket es 0600: quien administra Kanpachi ya puede ser root en ese servidor, y
// una vía que no pase por `sudo` solo quitaría el registro de quién hizo qué.
// Ver `daemon/paths` y `pipe_linux.go`.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/accentiostudios/kanpachi/daemon/paths"
	"github.com/accentiostudios/kanpachi/daemon/transport/client"
	"github.com/accentiostudios/kanpachi/daemon/transport/pipe"
	"github.com/accentiostudios/kanpachi/daemon/transport/protocol"
)

// opciones son los flags globales ya resueltos.
type opciones struct {
	datos string
	canal string
	nick  string
	// json pide la respuesta del daemon SIN pintar.
	//
	// Es lo que hace que esto sirva dentro de un script: lo pintado cambia cuando
	// alguien mejora una pantalla, y la forma de cable no, porque es un contrato
	// con la interfaz de Windows que además tiene candados.
	json bool
	// plazo es lo que se escribió en `--timeout`, y espera es ya interpretado.
	//
	// Los dos, y no solo el segundo: el texto crudo se conserva porque el error de
	// un valor que no vale tiene que poder citarlo tal como lo escribieron. Cero
	// en `espera` significa que nadie lo pidió y vale [client.DefaultTimeout].
	plazo  string
	espera time.Duration
}

func main() {
	// La interrupción se atiende, y hace falta decir qué NO hace: cortar el CLI
	// no cierra la sala. La sala vive en el daemon, que sigue corriendo; esto
	// solo suelta la conexión. Es la diferencia con `roomprobe`, que ES la
	// sesión y por eso tiene que apagarla al salir.
	ctx, parar := signal.NotifyContext(context.Background(), os.Interrupt)
	defer parar()

	code := correr(ctx, os.Args[1:])
	os.Exit(code)
}

func correr(ctx context.Context, args []string) int {
	op, resto, err := leerFlags(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "kanpachi:", err)
		return 2
	}

	// Sin subcomando, el asistente. Es lo que hace que esto se pueda usar sin
	// haber leído nada, que en un servidor recién instalado es el caso normal.
	if len(resto) == 0 {
		if err := assistant(ctx, op); err != nil {
			return informar(op, err)
		}
		return 0
	}

	cmd, ok := comandos[resto[0]]
	if !ok {
		fmt.Fprintf(os.Stderr, "kanpachi: there is no %q command\n\n", resto[0])
		ayuda(os.Stderr)
		return 2
	}
	if err := cmd.correr(ctx, op, resto[1:]); err != nil {
		return informar(op, err)
	}
	return 0
}

// informar cuenta el fallo, en la forma que corresponda, y elige con qué código
// de salida se termina. Los códigos los reparte [clasificar].
//
// Las DOS formas salen por sitios distintos, y eso es el contrato: la prosa por
// stderr para quien lee, el JSON por stdout para quien parsea. Nunca las dos.
func informar(op opciones, err error) int {
	if errors.Is(err, errInterrumpido) || errors.Is(err, context.Canceled) {
		// Sin mensaje: quien pulsa Ctrl+C ya sabe lo que hizo, y el 130 es lo que
		// todo el mundo usa para "lo mató una señal".
		return 130
	}
	código, salida := clasificar(err)
	if op.json {
		// **Con `--json`, un fallo sale como CÓDIGO y nada más.**
		//
		// La prosa se queda fuera, y no por estética. Este es el sitio donde
		// "nunca texto en claro con el motivo" se puede romper sin que nadie
		// mire: la salida de un script no la lee una persona, así que un mensaje
		// de más ahí no molesta a nadie y se queda para siempre. Con el registro
		// pidiendo password, el mensaje que llegaba hasta acá podía distinguir
		// un token vencido de uno inválido, que es justo la distinción que la
		// API se molesta en no hacer.
		//
		// Va a STDOUT y no a stderr, y con eso `--json` pasa a contestar un solo
		// documento JSON siempre, pase lo que pase: un script hace un parse y no
		// dos comprobaciones.
		fmt.Printf("{\"error\":{\"code\":%q}}\n", código)
		return salida
	}
	fmt.Fprintln(os.Stderr, "kanpachi:", err)
	if arreglo := arreglaEsto(código); arreglo != "" {
		fmt.Fprintf(os.Stderr, "  %s\n", arreglo)
	}
	return salida
}

// arreglaEsto es el comando que resuelve ese fallo, o vacío.
//
// # Por qué acá y no en cada subcomando
//
// Porque los dos fallos que tienen arreglo salen de VARIOS comandos: hospedar,
// renovar el código y renombrar la sala pasan todos por el registro. Repetir el
// consejo en cada uno daría tres sitios donde envejece, y el que se olvida es
// siempre el que menos se usa.
//
// **No se imprime con `--json`**, por la misma regla que la prosa: ahí sale el
// código y quien parsea decide qué hacer con él. Por eso vive en la rama de
// abajo de [informar] y no antes.
func arreglaEsto(código protocol.Code) string {
	switch código {
	case protocol.CodeNoOwnSeed:
		return "This machine has no registry yet:  kanpachi seed <host>"
	case protocol.CodeSeedPassword:
		return "That registry asks for a password to host:  kanpachi password"
	}
	return ""
}

// clasificar dice con qué código sale un fallo, y con qué número.
//
// # Por qué hay tres códigos de salida y no uno
//
// Porque un script tiene que poder distinguir "el daemon dijo que no" de "no
// hay daemon". El primero es una respuesta del producto y el script puede
// seguir; el segundo significa que nada de lo que venga después va a funcionar.
//
// El código de texto es el del PROTOCOLO cuando lo hay, así que un script mira
// la misma palabra que mira la interfaz. Los dos que no vienen del daemon
// llevan nombre propio para que tampoco haya que adivinarlos por el número.
func clasificar(err error) (protocol.Code, int) {
	if c := client.Code(err); c != "" {
		return c, 1
	}
	var mal *errDeUso
	if errors.As(err, &mal) {
		return "usage", 2
	}
	var no *errNegativa
	if errors.As(err, &no) {
		return "refused", 1
	}
	return "no_daemon", 3
}

// errDeUso es un comando mal escrito, que es culpa de quien lo escribió y no del
// daemon. Tiene tipo propio para que [informar] pueda darle su código.
type errDeUso struct{ msg string }

func (e *errDeUso) Error() string { return e.msg }

func uso(format string, args ...any) error {
	return &errDeUso{msg: fmt.Sprintf(format, args...)}
}

// errNegativa es el producto diciendo que no, decidido de este lado.
//
// # Por qué no vale devolver un error a secas
//
// Porque el error pelado cae en el 3, que significa "no se pudo hablar con el
// daemon", y eso sería mentira: el daemon contestó perfectamente y lo que dijo
// es que no hay sala. Un script que mire el código para decidir si reintentar
// reintentaría para siempre. Lo encontró correr `kanpachi link` sin sala, que
// salía con 3.
type errNegativa struct{ msg string }

func (e *errNegativa) Error() string { return e.msg }

func negativa(format string, args ...any) error {
	return &errNegativa{msg: fmt.Sprintf(format, args...)}
}

// errInterrumpido es que alguien pulsó Ctrl+C dentro de una pregunta.
//
// Es una RESPUESTA, no un fallo, y por eso tiene centinela propio. Dentro de una
// pregunta interactiva la interrupción no llega como señal: survey deja la
// consola en un modo donde lo único que ocurre es que la pregunta devuelve esto.
var errInterrumpido = errors.New("interrumpido")

// leerFlags parte los argumentos en flags globales y el resto.
//
// A mano y no con `flag`, y el motivo es el orden: `flag.Parse` deja de leer en
// el primer argumento que no es un flag, así que `kanpachi host --nick pepe` le
// pasaría `--nick` al subcomando en vez de a la sesión. Con esto los flags
// globales valen en cualquier posición, que es como se escriben de verdad.
func leerFlags(args []string) (opciones, []string, error) {
	op := opciones{datos: paths.Data(), canal: pipe.Name}
	var resto []string

	for i := 0; i < len(args); i++ {
		a := args[i]
		// Después del primer subcomando, un `--algo` puede ser suyo. Solo se
		// interceptan los globales, que están en esta lista y en ninguna otra.
		valor := func(nombre string) (string, error) {
			if i+1 >= len(args) {
				return "", uso("%s is missing its value", nombre)
			}
			i++
			return args[i], nil
		}
		var err error
		switch a {
		case "--data", "-data":
			op.datos, err = valor(a)
		case "--pipe", "-pipe", "--socket":
			op.canal, err = valor(a)
		case "--nick", "-nick", "--nickname":
			op.nick, err = valor(a)
		case "--timeout", "-timeout":
			op.plazo, err = valor(a)
		case "--json", "-json":
			op.json = true
		case "--help", "-help", "-h":
			resto = append(resto, "help")
		default:
			resto = append(resto, a)
		}
		if err != nil {
			return op, nil, err
		}
	}

	// The deadline is parsed HERE and not where it gets used, and that ordering
	// is the point: it is a flag value, so a bad one has to be rejected without
	// talking to anybody. It was validated inside `abrir`, after the connection
	// was already open, so `kanpachi --timeout abc status` with the daemon down
	// reported the daemon being down. Two wrong things at once, and the one it
	// named was the one the person had not caused.
	if op.plazo != "" {
		d, err := tiempo(op.plazo)
		if err != nil {
			return op, nil, err
		}
		op.espera = d
	}
	return op, resto, nil
}

// abrir se conecta y saluda.
//
// El error lleva pegado qué hacer, y no es adorno: el fallo más frecuente de
// este binario va a ser que no está corriendo el servicio o que falta `sudo`, y
// los dos se ven igual desde acá —un socket que no abre— así que el mensaje
// nombra los dos.
func abrir(op opciones) (*client.Client, error) {
	c, err := client.Open(op.canal, op.datos)
	if err != nil {
		return nil, fmt.Errorf("%w\n%s", err, pistaDeConexión(op))
	}
	if op.espera > 0 {
		c.Plazo = op.espera
	}
	return c, nil
}
