// Command engineprobe arranca el motor por el adaptador de verdad y espera.
//
// # Para qué existe
//
// Para probar el Job Object, que es lo único del motor que no se puede
// comprobar de ninguna otra forma. La promesa es que un daemon que muere de
// forma SUCIA, sin correr un solo `defer`, se lleva al motor con él; sin eso
// queda un motor vivo con la red virtual arriba y el firewall ya purgado.
//
// Nada del producto llega a ese camino a propósito, así que hay que fabricarlo:
// este binario hace de daemon, arranca el motor, imprime los dos PID, y se
// queda quieto para que alguien lo mate desde fuera.
//
// **No pasa por `CreateRoom`.** Crear una sala lee la tabla de rutas y
// configura el adaptador, y esos dos adaptadores todavía no existen. Este
// programa llama al motor directamente, que es justo lo que hace falta para
// aislar la pregunta del Job Object de todo lo demás.
//
// Necesita consola elevada: el motor crea un adaptador virtual.
package main

import (
	"context"
	"crypto/rand"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
	kanpachiengine "github.com/accentiostudios/kanpachi/daemon/adapter/engine/kanpachi"
)

func main() {
	stage := flag.String("stage", `C:\kt\stage`, "directorio con kanpachi-engine.exe")
	seed := flag.String("seed", "kanpachi.accentio.dev", "nombre del seed")
	hold := flag.Duration("hold", 5*time.Minute, "cuánto quedarse vivo esperando que lo maten")
	flag.Parse()

	if err := run(*stage, *seed, *hold); err != nil {
		fmt.Fprintln(os.Stderr, "engineprobe:", err)
		os.Exit(1)
	}
}

func run(stage, seed string, hold time.Duration) error {
	eng, err := kanpachiengine.New(kanpachiengine.Deps{
		Exe: filepath.Join(stage, "kanpachi-engine.exe"),
		Log: logStderr{},
	})
	if err != nil {
		return err
	}
	// El cierre NO se pone con defer a propósito: la gracia de esta prueba es
	// que a este proceso lo maten sin que corra ningún camino de limpieza.

	var spec domain.HostSpec
	if _, err := rand.Read(spec.NetworkID[:]); err != nil {
		return err
	}
	if _, err := rand.Read(spec.NetworkSecret[:]); err != nil {
		return err
	}
	nick, err := domain.ParseNickname("prueba")
	if err != nil {
		return err
	}
	spec.Name = nick
	spec.Subnet = domain.SharedSpace
	spec.Seeds = []string{seed}

	ctx := context.Background()
	fmt.Printf("engineprobe pid=%d\n", os.Getpid())
	if err := eng.HostNetwork(ctx, spec); err != nil {
		return fmt.Errorf("arrancando la red: %w", err)
	}
	fmt.Println("red arriba. Mata este proceso a lo bruto y comprueba que el motor muere con él.")

	// Los eventos se van imprimiendo para que se vea que la red está viva y no
	// solo que el proceso existe.
	go func() {
		for ev := range eng.Events() {
			fmt.Printf("evento %s: %s\n", ev.Kind, ev.Reason)
		}
	}()

	time.Sleep(hold)
	fmt.Println("se acabó la espera, nadie lo mató")
	return eng.Close()
}

type logStderr struct{}

func (logStderr) Info(msg string, kv ...any)  { fmt.Fprintln(os.Stderr, "info ", msg, kv) }
func (logStderr) Warn(msg string, kv ...any)  { fmt.Fprintln(os.Stderr, "aviso", msg, kv) }
func (logStderr) Error(msg string, kv ...any) { fmt.Fprintln(os.Stderr, "error", msg, kv) }
