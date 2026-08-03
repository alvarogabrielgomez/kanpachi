// Command kanpachid es el daemon.
//
// Acá vive la ELECCIÓN de adaptadores concretos, que es lo único que este
// binario decide. El ORDEN vive en `daemon/service`, que es Go puro y corre en
// el job de Linux. El `main` está acá y no allá porque el guardián de pureza no
// mira etiquetas de compilación: un main con `//go:build windows` dentro de
// service rompería ese job. Mismo reparto que `registry/cmd/kanpseed`.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/daemon/adapter/sinimplementar"
	"github.com/accentiostudios/kanpachi/daemon/transport/pipe"
	"github.com/accentiostudios/kanpachi/daemon/transport/protocol"
)

func main() {
	consola := flag.Bool("console", false, "correr como aplicación de consola en vez de servicio")
	datos := flag.String("data", "", "directorio de datos. Vacío usa ProgramData\\Kanpachi")
	// El nombre del pipe se puede cambiar SOLO en modo consola, y existe por una
	// razón concreta: el de producción vive bajo ProtectedPrefix\Administrators,
	// que Windows no deja crear sin elevar. Sin esta bandera, probar el saludo,
	// el token y los topes exigiría un UAC cada vez.
	//
	// No es un agujero: en modo servicio no se lee, y un binario con
	// provisionales no arranca como servicio.
	nombre := flag.String("pipe", "", "nombre del pipe. Solo con --console. Vacío usa el protegido")
	flag.Parse()

	if err := correr(*consola, *datos, *nombre); err != nil {
		fmt.Fprintln(os.Stderr, "kanpachid:", err)
		os.Exit(1)
	}
}

func correr(consola bool, datos, nombre string) error {
	// **Un binario con provisionales NO se instala como servicio.** El riesgo
	// real nunca fue que fallen: es que uno con un firewall que dice que purgó
	// termine corriendo en la máquina de alguien.
	if sinimplementar.Presente && !consola {
		return fmt.Errorf("este binario lleva adaptadores provisionales dentro, así que solo " +
			"arranca con --console.\n" +
			"  Un provisional que devuelve éxito hace la cuarentena inverificable, y eso " +
			"instalado es peor que no tener daemon")
	}
	if !consola {
		return fmt.Errorf("el host del servicio de Windows todavía no está escrito. Usa --console")
	}

	if nombre == "" {
		nombre = pipe.ConsoleName
	}

	if datos == "" {
		datos = filepath.Join(os.Getenv("ProgramData"), "Kanpachi")
	}
	// El directorio lo crea el INSTALADOR con una ACL propia, y esa ACL es la
	// mitad de la protección del token. Crearlo acá la perdería en silencio.
	if _, err := os.Stat(datos); err != nil {
		return fmt.Errorf("el directorio de datos %s no está.\n"+
			"  Lo crea el instalador con su ACL. Para probar, créalo a mano o pasa --data", datos)
	}

	// El token rota una vez por vida del proceso y se borra en TODO camino de
	// salida: uno que sobreviva al proceso no abre nada y solo es un secreto
	// muerto en disco.
	token, err := pipe.NewToken()
	if err != nil {
		return err
	}
	if err := pipe.WriteToken(datos, token); err != nil {
		return err
	}
	defer func() { _ = pipe.RemoveToken(datos) }()

	log := logConsola{}
	ln, err := pipe.Listen(pipe.Deps{
		API:   apiConsola{},
		Token: token,
		Clock: relojReal{},
		Log:   log,
		Name:  nombre,
	})
	if err != nil {
		return err
	}
	defer func() { _ = ln.Close() }()

	ctx, parar := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer parar()

	fmt.Printf("kanpachid en modo consola\n  pipe:  %s\n  token: %s\n  datos: %s\n\n"+
		"Ctrl+C para salir. Prueba con:  go run ./internal/kanpctl -data %q status\n\n",
		nombre, token, datos, datos)

	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()
	return ln.Serve(ctx)
}

// apiConsola es lo que se puede atender hoy.
//
// Embebe la interfaz en vez de implementarla entera: lo que no está escrito
// entra en pánico, y el pipe lo recoge cortando SOLO esa conexión. Es lo que
// hace que probar el transporte no dependa de que exista el resto del daemon.
type apiConsola struct{ protocol.API }

func (apiConsola) Status() domain.RoomState { return domain.RoomState{Conn: domain.StateIdle} }
func (apiConsola) MissingGame() string      { return "" }

type relojReal struct{}

func (relojReal) Now() time.Time { return time.Now() }

type logConsola struct{}

func (logConsola) Info(msg string, kv ...any)  { fmt.Println("info ", msg, kv) }
func (logConsola) Warn(msg string, kv ...any)  { fmt.Println("aviso", msg, kv) }
func (logConsola) Error(msg string, kv ...any) { fmt.Println("error", msg, kv) }
