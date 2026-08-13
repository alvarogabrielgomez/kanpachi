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
// **Hay un caso en que el job no la puede meter dentro**, y es el bundle
// portable: la UI nace en un job ajeno y a un proceso que ya está en uno no se
// le puede meter en otro. Ahí la invariante la sostiene código de cierre, que
// es peor y es lo único que hay. Ver [Host.closeHandles].
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

	// ResumeFlag es la bandera con la que se le dice a la ventana que el daemon
	// está reabriendo la sala del arranque anterior AHORA MISMO.
	//
	// # Qué hueco cierra
	//
	// El daemon sabe que hay sala que reabrir antes de lanzar la ventana: lo lee
	// del disco. La ventana no sabe nada hasta que conecta y llega el primer
	// estado, y reabrir tarda: levantar dos redes, tomar direcciones, medir el
	// MTU. Durante todo ese rato la ventana enseñaba la PORTADA, que dice
	// exactamente lo contrario de lo que está pasando, y después saltaba a la
	// sala de golpe.
	//
	// Con la bandera, el primer fotograma ya es la espera correcta, y el latido
	// la rellena con los pasos de verdad en cuanto conecta. No se le manda
	// ningún dato de la sala: solo QUE hay una en camino. Lo que se enseñe sale
	// del daemon como todo lo demás.
	ResumeFlag string

	// Resuming contesta si en ESTE instante hay una reapertura en marcha.
	//
	// Función y no bandera fija porque la ventana se lanza más de una vez: al
	// arrancar, al pedirla desde la bandeja, y al relanzarla si se cayó. Con un
	// valor fijo, la de la bandeja pediría esperar por una sala que lleva media
	// hora abierta. Preguntado en cada lanzamiento, sale bien en los tres.
	//
	// Nula significa que no. Un lanzador que no sepa contestarlo tiene que
	// fallar hacia el lado que no promete nada, igual que [ShowFlag].
	Resuming func() bool

	// LogDir es dónde la interfaz deja SU log, y viaja en la línea de comandos.
	//
	// **Se le DICE, y eso va contra la doctrina del marcador** que defiende
	// `ui/lib/features/session/infra/daemon/pipe/pipe_names.dart`: los dos lados
	// deducen del disco en vez de contárselo, para que nada se pierda por el
	// camino en una sola dirección. La regla sigue en pie para lo que deduce, y
	// esto es otra pregunta.
	//
	// El marcador contesta "qué producto soy", que los dos pueden mirar. Esto
	// contesta "desde qué carpeta me abrieron", que la interfaz NO puede
	// deducir: en el bundle portable su ejecutable vive en un directorio
	// temporal que se borra al salir, así que un log deducido junto a ella
	// muere justo cuando alguien lo iba a mandar. La carpeta la sabe solo el
	// bundle, y ya se la pasa al daemon por la misma vía, con `--log`.
	//
	// Vacío significa "que lo deduzca", que es lo correcto en el producto
	// instalado: ahí el directorio de datos es de ProgramData y no se borra.
	//
	// Llega por argv a un proceso SIN elevar, así que lo peor que consigue
	// alguien pasándola a mano es escribir donde ya podía escribir.
	LogDir string

	// OnGiveUp se llama cuando la interfaz se cae una y otra vez y ya no se
	// intenta más. Lo correcto entonces es apagar todo, porque un daemon vivo
	// sin forma de mostrarse es justo lo que la invariante prohíbe.
	OnGiveUp func()

	Log port.Logger
}

// Los topes del relanzamiento.
//
// Sin ellos, una interfaz que revienta al arrancar deja al daemon relanzándola
// para siempre. Con ellos, una cadena de caídas rápidas se lee como "esto no va
// a arrancar" y se apaga todo, que es lo que la invariante pide.
//
// **Se rinde en la CUARTA caída, no en la tercera**, y conviene decirlo porque
// el número engaña: `maxRelaunches` son los relanzamientos que se conceden, y
// [relanzador.murió] compara `seguidas > maxRelaunches`. O sea que tres caídas
// gastan los tres relanzamientos y la cuarta ya no tiene ninguno. Es lo que fija
// `TestCuatroCaídasRápidasApaganElDaemon`, y el texto que se le enseña al
// usuario decía "tres" hasta el 2026-08-11.
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

// relanzador cuenta caídas RÁPIDAS SEGUIDAS de la interfaz.
//
// Vive aparte del vigilante, y en un fichero sin `//go:build windows`, porque
// es la única vía del programa por la que la muerte de un HIJO apaga al PADRE:
// pasado el tope, `OnGiveUp` cierra el daemon entero, y con él la sala, la red
// virtual y el firewall. Una regla con esa consecuencia tiene que poder
// comprobarse sin arrancar un proceso ni estar en Windows.
//
// Al revés no existe: el daemon no tiene ningún camino que lo cierre porque el
// MOTOR se muera. El watchdog reintenta y, si se rinde, cierra la SALA y sigue
// vivo. Ver `supervisor.rendirse`.
type relanzador struct{ seguidas int }

// murió registra una muerte de una interfaz que vivió `vivió`.
//
// Devuelve qué número de intento es y si hay que rendirse. El orden importa: la
// vida larga limpia la cuenta ANTES de sumar, así que una interfaz que aguantó
// y se cerró vuelve a tener las tres oportunidades enteras.
func (r *relanzador) murió(vivió time.Duration) (intento int, rendirse bool) {
	if vivió > quickDeath {
		r.seguidas = 0
	}
	r.seguidas++
	return r.seguidas, r.seguidas > maxRelaunches
}

// Host lanza la interfaz y la mantiene viva mientras el daemon lo esté.
type Host struct {
	deps Deps

	mu sync.Mutex
	// job es el Job Object con KILL_ON_JOB_CLOSE. Cero antes de abrirlo.
	job uintptr
	// proc es el handle del proceso persistente, el que lleva la bandeja. Cero
	// cuando no hay ninguno vivo.
	proc uintptr
	// heldByJob dice si a ESE proceso lo sujeta [Host.job].
	//
	// Falso cuando `AssignProcessToJobObject` no pudo, que es lo que pasa
	// SIEMPRE en el bundle portable: la interfaz nace dentro de un job que no
	// es nuestro y a un proceso que ya está en uno no se le puede meter en
	// otro. Ver [Host.closeHandles], que es donde la diferencia se paga.
	heldByJob bool

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

// Stop puts a message on the user desktop and WAITS for it to be acknowledged.
//
// For the messages that ANNOUNCE Kanpachi shutting down, which today are two:
// this machine cannot build a virtual adapter, and the interface will not stay
// up. The caller shuts Kanpachi down right after, and that is exactly why this
// one waits: a box that appears at the same instant everything closes reads as
// a crash, not as an explanation. The wait is also the only time left to a game
// still in progress.
//
// The wait is bounded, because nobody may be at the machine. See the
// implementation.
func Stop(text string) { avisarYEsperarEnSesión("Kanpachi", text) }

// Close se lleva a la interfaz por delante.
//
// Es IDEMPOTENTE y no espera a nadie: lo llama el camino de apagado, que no
// puede quedarse colgado. Lo normal es que la mate el kernel al cerrar el job;
// cuando la interfaz quedó fuera del job hay que matarla a mano, y eso lo
// resuelve [Host.closeHandles].
func (h *Host) Close() error {
	h.once.Do(func() {
		close(h.stop)
		h.signalWake()
		h.watching.Wait()
		h.closeHandles()
	})
	return nil
}
