// Package uihost es el daemon lanzando su propia interfaz.
//
// # La invariante que sostiene
//
// **Hay bandeja si y solo si hay daemon.** Un túnel abierto sin nada en
// pantalla que lo explique tiene la forma exacta de un troyano, y ninguna
// cantidad de red correcta por debajo lo hace leerse de otra manera. Ver el
// modelo de procesos en `docs/03`.
//
// El icono no lo puede poner el daemon: corre en la sesión 0, que no tiene
// escritorio interactivo ni área de notificación, y los servicios interactivos
// los retiró Microsoft en Windows 10 1803. Así que lo pone el único proceso que
// puede, uno sin elevar en la sesión del usuario, y este paquete es cómo el
// daemon lo arranca y lo tiene sujeto.
//
// # Por qué el daemon es el PADRE
//
// Porque así la invariante la sostiene el kernel y no código de cierre. La UI
// entra en un Job Object con `KILL_ON_JOB_CLOSE`, el mismo mecanismo que ya
// usaba el motor: cualquier muerte del daemon, incluido un `TerminateProcess`
// desde el Administrador de tareas, se lleva la UI por delante. No hay camino
// de apagado lo bastante sucio como para dejar un túnel sin cara.
//
// Y de paso desaparece un problema. Detectar que a la UI la mataron
// necesitaría que ella declarara su PID en el saludo; siendo el padre, el
// daemon tiene su handle desde que nace y lo único que hace es esperar sobre él.
package uihost

import (
	"fmt"
	"sync"
	"time"

	"github.com/accentiostudios/kanpachi/core/port"
)

// Deps es lo que hace falta para lanzar.
type Deps struct {
	// Exe es la ruta de la interfaz. **Sale del directorio del propio binario
	// y jamás del estado, la configuración ni el pipe.** Esto corre como
	// SYSTEM: una ruta que alguien pueda influir es escalada de privilegios
	// directa.
	Exe string

	// ShowFlag es la bandera con la que se pide arrancar CON ventana.
	//
	// La bandera pide mostrar y no callar, y esa vuelta importa: sin ella, la
	// interfaz abre ventana. Una bandera que se pierde por el camino —un
	// argumento mal pasado, un lanzamiento nuevo que se olvida de ponerla— tiene
	// que fallar hacia el lado callado. Al revés, el fallo es una ventana
	// abriéndose sola encima de lo que estuvieras haciendo al encender la PC.
	ShowFlag string

	// OnGiveUp se llama cuando la interfaz se cae una y otra vez y ya no se
	// intenta más. Lo correcto entonces es apagar todo, porque un daemon vivo
	// sin forma de mostrarse es justo lo que la invariante prohíbe.
	OnGiveUp func()

	Log port.Logger
}

// Los topes del relanzamiento.
//
// Sin ellos, una interfaz que revienta al arrancar deja al daemon relanzándola
// para siempre. Con ellos, tres caídas rápidas se leen como "esto no va a
// arrancar" y se apaga todo, que es lo que la invariante pide.
const (
	maxRelaunches = 3
	// quickDeath es qué se considera "se cayó enseguida". Una interfaz que
	// vivió más que esto y se cerró es un caso normal, así que el contador se
	// reinicia y se vuelve a tener las tres oportunidades.
	quickDeath = 60 * time.Second
	// relaunchGrace es lo que se espera antes de volver a lanzarla.
	//
	// Corto: quien mira la bandeja la quiere de vuelta. Suficiente para que la
	// que acaba de morir suelte lo suyo, empezando por el evento con nombre de
	// la instancia única, que es lo que la nueva se toparía consigo misma.
	relaunchGrace = 1500 * time.Millisecond
)

// Host lanza la interfaz y la mantiene viva mientras el daemon lo esté.
type Host struct {
	deps Deps

	mu sync.Mutex
	// job es el Job Object con KILL_ON_JOB_CLOSE. Cero antes de abrirlo.
	job uintptr
	// proc es el handle del proceso persistente, el que lleva la bandeja. Cero
	// cuando no hay ninguno vivo.
	proc uintptr

	stop chan struct{}
	// wake despierta la espera del vigilante sin cerrar handles debajo suyo.
	wake     uintptr
	watching sync.WaitGroup
	once     sync.Once
}

// New abre el job y deja el host listo. NO lanza nada.
func New(deps Deps) (*Host, error) {
	if deps.Exe == "" {
		return nil, fmt.Errorf("uihost: sin ruta de la interfaz no hay nada que lanzar")
	}
	if deps.Log == nil {
		return nil, fmt.Errorf("uihost: hace falta un log")
	}
	h := &Host{deps: deps, stop: make(chan struct{})}
	if err := h.openJob(); err != nil {
		return nil, err
	}
	return h, nil
}

// Start lanza la interfaz y se queda vigilándola.
//
// `show` decide si abre ventana. Va en `true` cuando al daemon lo levantó
// alguien haciendo doble clic, y en `false` cuando lo levantó Windows al
// encender la máquina: ahí la bandeja aparece y la ventana no.
func (h *Host) Start(show bool) error {
	if err := h.launch(show, true); err != nil {
		// Say it on the user desktop. Without this, failing to launch is total
		// silence: no tray, no window, no error, and a daemon running with
		// nothing on screen — the shape the invariant forbids.
		//
		// The order of the sentences is the whole point, and the first version
		// got it wrong: it opened with a raw Go error, so the first thing the
		// reader met was a Windows API string in English. What somebody needs
		// from a box like this is, in order: what happened, whether it broke
		// anything, what to do, and only then the line to copy into a report.
		Warn("Kanpachi se está ejecutando y no consiguió abrir su ventana.\n\n" +
			"Nada se rompió: si tenías una sala abierta, sigue abierta. " +
			"Lo que falta es la ventana.\n\n" +
			"Qué hacer: vuelve a abrir Kanpachi desde su acceso directo. " +
			"Si sigue sin aparecer, reinicia la PC.\n\n" +
			"Detalle técnico, por si hay que reportarlo:\n" + err.Error())
		return err
	}
	h.watching.Add(1)
	go h.watch()
	return nil
}

// Show pide que la ventana se vea.
//
// No manda ninguna señal: **lanza la interfaz otra vez**. Si ya hay una
// corriendo, el proceso nuevo se topa con la instancia única, le dice a la
// primera que se muestre, y se muere. Esa maquinaria hace falta igual para los
// enlaces `kanpachi://`, así que se paga una vez y sirve para las dos cosas.
//
// Si no hay ninguna viva, esta pasa a ser la persistente.
func (h *Host) Show() error {
	h.mu.Lock()
	hayViva := h.proc != 0
	h.mu.Unlock()

	if hayViva {
		// Transitoria: no se guarda su handle ni entra en la vigilancia. Vive
		// lo que tarda en avisarle a la otra.
		return h.launch(true, false)
	}
	return h.launch(true, true)
}

// Warn puts a message on the user desktop, from session 0.
//
// Exported because the daemon needs it too, for the same reason this package
// needs it: a service that cannot show its interface has no other way to say
// so. See the implementation for why a plain MessageBox does not work here.
func Warn(text string) { avisarEnSesión("Kanpachi", text) }

// Close cierra el job, y con él se va la interfaz.
//
// Es IDEMPOTENTE y no espera a nadie: lo llama el camino de apagado, que no
// puede quedarse colgado. Cerrar el job es lo que mata a la interfaz, y lo hace
// el kernel.
func (h *Host) Close() error {
	h.once.Do(func() {
		close(h.stop)
		h.signalWake()
		h.watching.Wait()
		h.closeHandles()
	})
	return nil
}
