// Package service es el ORDEN de arranque y apagado del daemon.
//
// # Qué vive acá y qué no
//
// Acá vive en qué orden pasan las cosas. La ELECCIÓN de qué adaptador concreto
// se usa vive en `cmd/kanpachid`, que es el único sitio que conoce a la vez el
// dominio y Windows.
//
// La frontera no es de gusto: este paquete es Go PURO y corre en el job de
// Linux junto a core, y eso es lo que hace que el orden de arranque tenga
// tests. El guardián de pureza parsea con `parser.ImportsOnly` y no mira
// etiquetas de compilación, así que un `main.go` con `//go:build windows` acá
// dentro rompería ese job. Ver decisión 3.
//
// Por eso las dependencias entran como INTERFACES mínimas y no como los tipos
// de verdad: `Entrada` es el named pipe sin nombrarlo, `Bucle` es el supervisor
// sin importarlo.
package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/core/port"
)

// Entrada es la superficie por la que le hablan al daemon. La implementa el
// named pipe.
type Entrada interface {
	Serve(ctx context.Context) error
	// Close es IDEMPOTENTE: lo llama el camino de error del arranque, que puede
	// correr antes de que se haya abierto nada.
	Close() error
}

// Bucle is what runs for as long as the daemon lives. The supervisor
// implements it.
//
// Run must close `ready` once the loop is actually up, and before doing any
// work that can block. That signal is the whole reason this is not a bare
// `Run(ctx)`: the entry point is the only surface reachable from outside the
// process, and it must not open until the supervisor is running.
//
// Without it there was a real window, not a theoretical one. Start launched
// both goroutines and returned, so the scheduler decided the order: the UI
// could connect and ask to join a room while the firewall purge had not run
// yet. A flaky ordering test is what surfaced it.
//
// Closing `ready` twice panics, so close it exactly once. An implementation
// that returns early on error must close it on the way out, or Start blocks
// forever.
type Bucle interface {
	Run(ctx context.Context, ready chan<- struct{}) error
}

// Sala es la sala vista desde el arranque y el apagado. La implementa la
// sesión.
type Sala interface {
	LeaveRoomOnShutdown(ctx context.Context) error

	// PendingRoom is the room left by the previous run, if there is one.
	PendingRoom() (domain.PersistedRoom, bool)
	// ResumeRoom reopens it with the SAME code and the SAME network.
	ResumeRoom(ctx context.Context) (domain.RoomState, error)
}

// Deps son las piezas YA CONSTRUIDAS.
//
// Que lleguen construidas es lo que mantiene este paquete puro, y de paso lo
// que hace que la purga del firewall no se pueda saltar: quien construye la
// sesión ya pasó por [usecase.NewSession], que purga antes de devolverla. La
// dependencia está en los tipos y no en un comentario que pide acordarse.
type Deps struct {
	Bucle   Bucle
	Entrada Entrada
	Sala    Sala
	Log     port.Logger

	// ApagadoMax es lo que se espera a que todo cierre antes de rendirse. Un
	// apagado que no termina deja al Administrador de servicios matando el
	// proceso, y ahí sí quedan reglas huérfanas.
	ApagadoMax time.Duration
}

// ApagadoPorDefecto son veinte segundos.
//
// El Administrador de servicios de Windows da treinta antes de matar el
// proceso. Rendirse a los veinte deja diez para anotar por qué, que es la
// diferencia entre un log que explica y un proceso que desapareció.
const ApagadoPorDefecto = 20 * time.Second

// Runtime es el daemon corriendo.
type Runtime struct {
	deps  Deps
	fin   chan struct{}
	una   sync.Once
	errIn error
	// reabriendo cuenta la reapertura de la sala del arranque anterior, que
	// corre en segundo plano. El apagado la espera: ver [Runtime.apagar].
	reabriendo sync.WaitGroup
}

// Start arranca en el orden que importa.
//
// # Por qué la entrada va ÚLTIMA
//
// El pipe es la única superficie alcanzable desde fuera del proceso. Abrirlo al
// final significa que ninguna orden puede llegar antes de que la máquina esté
// en cuarentena: la purga del firewall ya corrió al construir la sesión, y el
// supervisor ya está escuchando al motor.
//
// Al revés, existe una ventana, corta y real, en la que la UI puede pedir
// "entrar a esta sala" mientras el firewall todavía tiene las reglas de la
// ejecución anterior puestas.
//
// Y por eso `cmd/kanpachid` reporta SERVICE_RUNNING después de esto y no antes.
func Start(ctx context.Context, d Deps) (*Runtime, error) {
	if d.Bucle == nil || d.Entrada == nil || d.Log == nil {
		return nil, errors.New("service: faltan piezas del arranque")
	}
	if d.ApagadoMax <= 0 {
		d.ApagadoMax = ApagadoPorDefecto
	}

	r := &Runtime{deps: d, fin: make(chan struct{})}

	// The loop first, and the entry only once the loop says it is up. The
	// order is the point: see the Bucle doc.
	ready := make(chan struct{})
	go func() {
		if err := d.Bucle.Run(ctx, ready); err != nil && !errors.Is(err, context.Canceled) {
			d.Log.Error("el supervisor se detuvo", "error", err)
		}
	}()

	select {
	case <-ready:
	case <-ctx.Done():
		// Cancelled before the loop came up. Nothing was opened, so there is
		// nothing to unwind.
		return nil, ctx.Err()
	}

	go func() {
		defer close(r.fin)
		err := d.Entrada.Serve(ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			r.errIn = err
			d.Log.Error("la API local se detuvo", "error", err)
		}
	}()

	r.reabrirLaSala(ctx)

	d.Log.Info("el daemon está en marcha")
	return r, nil
}

// reabrirLaSala vuelve a abrir la que quedó del arranque anterior, si la hay.
//
// # Por qué en segundo plano, y por qué después de abrir la entrada
//
// Because it takes about a minute: two adapters have to come up, the credential
// has to be exchanged and the MTU measured. Doing it before opening the entry
// would delay systemd's READY by the same amount, and a `Type=notify` unit that
// takes a minute to report looks hung. Worse on Windows, where the service
// manager kills whatever does not report in time.
//
// So the entry opens first and this runs behind it. During that minute the
// daemon answers `status` saying it is connecting, and `progress` shows the
// steps, which is exactly what creating a room by hand does.
//
// # Por qué se reabre sola, si nada de fuera surte efecto sin confirmar
//
// Because that invariant is about what arrives FROM OUTSIDE, and what arrives
// from outside is an invite link somebody clicked in their browser. Reopening
// your own room, the one that was never closed, arrives from nowhere: this very
// machine opened it and it is still theirs. Closing it is a button, and whoever
// presses it deletes the file, so this finds nothing to reopen.
//
// **Worth saying out loud:** reopening the room also reopens the game ports of
// whatever profile was active. That is what a server wants, and it is what the
// gate and the quarantine bound.
func (r *Runtime) reabrirLaSala(ctx context.Context) {
	if r.deps.Sala == nil {
		return
	}
	pendiente, hay := r.deps.Sala.PendingRoom()
	if !hay {
		return
	}

	r.deps.Log.Info("hay una sala del arranque anterior, se reabre",
		"código", pendiente.Room.InviteID.String())

	r.reabriendo.Add(1)
	go func() {
		defer r.reabriendo.Done()
		if _, err := r.deps.Sala.ResumeRoom(ctx); err != nil {
			// Not fatal, and that is why it does not bring the startup down: the
			// daemon stays alive without a room, and whoever looks at `status`
			// sees why. Bringing it down would trade a room that does not come
			// back for a machine without Kanpachi.
			if errors.Is(err, context.Canceled) {
				r.deps.Log.Info("reabrir la sala se cortó porque el daemon se apagaba")
				return
			}
			r.deps.Log.Error("no se pudo reabrir la sala del arranque anterior", "error", err)
			return
		}
		r.deps.Log.Info("la sala del arranque anterior está abierta otra vez")
	}()
}

// Wait espera a que la entrada termine.
func (r *Runtime) Wait() error {
	<-r.fin
	return r.errIn
}

// esperarReapertura espera a que la sala termine de reabrirse, o al plazo.
//
// Returns false when the deadline ran out, so whoever shuts down can SAY it
// instead of carrying on in silence: if that happens there is a half-finished
// reopen racing the close, and the log is all that will be left.
func (r *Runtime) esperarReapertura(ctx context.Context) bool {
	hecho := make(chan struct{})
	go func() {
		r.reabriendo.Wait()
		close(hecho)
	}()
	select {
	case <-hecho:
		return true
	case <-ctx.Done():
		return false
	}
}

// Shutdown cierra en el orden inverso y con su PROPIO contexto.
//
// # Los dos detalles que lo hacen funcionar
//
// **El orden inverso.** Primero se corta la entrada, para que no lleguen
// órdenes nuevas mientras se cierra. Después el supervisor, para que no
// reaplique nada de lo que se está quitando. Y al final se sale de la sala, que
// es lo que cierra los puertos.
//
// **El contexto propio.** `context.WithoutCancel` sobre el de arriba: al apagar,
// el contexto del servicio ya viene cancelado, y con él cada cierre de puerto
// sería un no-op. O sea que el apagado limpio no limpiaría nada, y las reglas
// del firewall se quedarían puestas hasta el arranque siguiente. El patrón ya
// está en el repo, en el canal de la sala.
//
// Es IDEMPOTENTE.
func (r *Runtime) Shutdown(padre context.Context) error {
	var err error
	r.una.Do(func() { err = r.apagar(padre) })
	return err
}

func (r *Runtime) apagar(padre context.Context) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(padre), r.deps.ApagadoMax)
	defer cancel()

	r.deps.Log.Info("apagando el daemon")

	// 1. La puerta. Que no entren órdenes nuevas mientras se cierra.
	if err := r.deps.Entrada.Close(); err != nil {
		r.deps.Log.Warn("no se pudo cerrar la API local", "error", err)
	}

	// 1b. La reapertura, si todavía está corriendo.
	//
	// **It is waited for BEFORE closing the room, and without this what is left
	// is the worst of both.** Reopening raises adapters and writes rules;
	// closing removes them. Running at the same time, the close can remove what
	// the reopen has not put yet, and whatever stays put nobody removes,
	// because the process dies right after.
	//
	// The service context already comes cancelled on the normal path, so the
	// reopen cuts itself short. The deadline covers the other path, the shutdown
	// order arriving over the local channel: there nobody cancelled anything,
	// and waiting forever would leave the service never stopping.
	if !r.esperarReapertura(ctx) {
		r.deps.Log.Warn("la reapertura de la sala no terminó a tiempo, se cierra igual")
	}

	// 2. La sala. Es lo que cierra los puertos y revierte los ajustes, así que
	// es lo que de verdad importa que corra.
	if r.deps.Sala != nil {
		hecho := make(chan error, 1)
		go func() { hecho <- r.deps.Sala.LeaveRoomOnShutdown(ctx) }()

		select {
		case err := <-hecho:
			if err != nil {
				// Se anota y no se aborta: quedan cosas por cerrar detrás.
				r.deps.Log.Error("no se pudo cerrar la sala al apagar", "error", err)
			}
		case <-ctx.Done():
			return fmt.Errorf("service: el apagado no terminó en %s, quedan reglas puestas", r.deps.ApagadoMax)
		}
	}

	r.deps.Log.Info("el daemon se apagó")
	return nil
}
