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
	"crypto/rand"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/accentiostudios/kanpachi/core/usecase"
	catalogstore "github.com/accentiostudios/kanpachi/daemon/adapter/catalog/jsonfile"
	"github.com/accentiostudios/kanpachi/daemon/adapter/sinimplementar"
	statestore "github.com/accentiostudios/kanpachi/daemon/adapter/state/jsonfile"
	"github.com/accentiostudios/kanpachi/daemon/service"
	"github.com/accentiostudios/kanpachi/daemon/service/supervisor"
	"github.com/accentiostudios/kanpachi/daemon/transport/control"
	"github.com/accentiostudios/kanpachi/daemon/transport/pipe"
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

	ctx, parar := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer parar()

	log := logConsola{}

	// El firewall ANTES que nada, y no por orden de lectura: construir la sesión
	// purga las reglas de la ejecución anterior, así que si el firewall no se
	// puede abrir hay que enterarse acá y no a mitad del arranque.
	fw, audit, cerrarFirewall, err := realFirewall(datos, log, sinimplementar.Audit{})
	if err != nil {
		return err
	}
	defer func() { _ = cerrarFirewall() }()

	eventos := sinimplementar.NewEvents()
	defer func() { _ = eventos.Close() }()

	canal := control.New(control.Deps{Clock: relojReal{}, Log: log})
	motor := sinimplementar.Engine{}

	// NewSession PURGA el firewall antes de devolver, así que a partir de acá la
	// máquina está en el estado que este arranque decidió y no en el que dejó el
	// anterior. Que la purga esté dentro del constructor y no en una llamada
	// aparte es lo que hace que no se pueda saltar.
	sesion, err := usecase.NewSession(ctx, usecase.Deps{
		Engine:    motor,
		Firewall:  fw,
		NetCfg:    sinimplementar.NetConfig{},
		Routes:    sinimplementar.Routing{},
		Store:     catalogstore.New(dirDelBinario(), datos, log),
		State:     statestore.New(datos),
		Library:   sinimplementar.Library{},
		Directory: sinimplementar.Directory{},
		Control:   canal,
		Audit:     audit,
		Inspector: sinimplementar.Inspector{},
		Clock:     relojReal{},
		Log:       log,
		Rand:      rand.Reader,
	})
	if err != nil {
		return err
	}

	bucle, err := supervisor.New(supervisor.Deps{
		Room:    sesion,
		Engine:  motor,
		Control: canal,
		System:  eventos,
		Log:     log,
	})
	if err != nil {
		return err
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

	ln, err := pipe.Listen(pipe.Deps{
		API:   sesion,
		Token: token,
		Clock: relojReal{},
		Log:   log,
		Name:  nombre,
	})
	if err != nil {
		return err
	}

	rt, err := service.Start(ctx, service.Deps{
		Bucle:   bucle,
		Entrada: ln,
		Sala:    sesion,
		Log:     log,
	})
	if err != nil {
		_ = ln.Close()
		return err
	}

	fmt.Printf("kanpachid en modo consola\n  pipe:  %s\n  token: %s\n  datos: %s\n\n"+
		"Ctrl+C para salir. Prueba con:  go run ./internal/kanpctl -data %q status\n\n",
		nombre, token, datos, datos)

	go func() {
		<-ctx.Done()
		// El apagado tiene su PROPIO contexto dentro de service, porque el de
		// acá ya viene cancelado y con él cada cierre de puerto sería un no-op.
		if err := rt.Shutdown(ctx); err != nil {
			log.Error("el apagado no terminó bien", "error", err)
		}
	}()
	return rt.Wait()
}

// dirDelBinario es donde vive el catálogo que trajo el instalador.
//
// Junto al ejecutable y no en el directorio de datos: ese archivo es del
// instalador, se actualiza con la app, y el daemon no lo escribe jamás. Ver
// [catalogstore.Store] para por qué son dos rutas y no una.
func dirDelBinario() string {
	exe, err := os.Executable()
	if err != nil {
		// Sin ruta del ejecutable el catálogo que vino con la app no se lee, y
		// el producto sigue funcionando con el del usuario. Se devuelve el
		// directorio actual, que es lo que hace que `go run` encuentre uno si
		// lo hay al lado.
		return "."
	}
	return filepath.Dir(exe)
}

type relojReal struct{}

func (relojReal) Now() time.Time { return time.Now() }

type logConsola struct{}

func (logConsola) Info(msg string, kv ...any)  { fmt.Println("info ", msg, kv) }
func (logConsola) Warn(msg string, kv ...any)  { fmt.Println("aviso", msg, kv) }
func (logConsola) Error(msg string, kv ...any) { fmt.Println("error", msg, kv) }
