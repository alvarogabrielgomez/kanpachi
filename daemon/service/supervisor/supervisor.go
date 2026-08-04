// Package supervisor es lo que hace correr al daemon.
//
// Drena los canales de los adaptadores, late para que los plazos venzan, y
// reinicia el motor cuando se muere. Es el ÚNICO sitio del proyecto con
// goroutines de larga vida.
//
// No conoce Windows. Solo habla con los puertos declarados en core, así que
// compila y se prueba en Linux junto a core, que es la métrica de
// docs/CLAUDE.md. Los eventos del sistema entran por [port.SystemEvents], cuya
// implementación vive en un adaptador.
package supervisor

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/core/port"
)

// Room es lo que el supervisor necesita de la sesión.
//
// Es una interfaz y no *usecase.Session por dos motivos, y los dos importan. El
// test prueba el BUCLE y no la sesión, que ya tiene los suyos. Y lo que no está
// acá no se puede llamar por error: el supervisor no puede crear una sala, ni
// expulsar, ni emitir credenciales.
type Room interface {
	Status() domain.RoomState
	Tick(ctx context.Context) domain.RoomState
	RefreshAlerts(ctx context.Context) domain.RoomState
	OnEngineEvent(ctx context.Context, ev domain.EngineEvent) (domain.RoomState, error)
	OnEngineGaveUp(ctx context.Context, reason string) domain.RoomState
	OnPeersChanged(ctx context.Context) (domain.RoomState, error)
	OnRoomAnnounce(ctx context.Context, a domain.RoomAnnounce) (domain.RoomState, error)
	OnRoomNotice(ctx context.Context, n domain.RoomNotice) domain.RoomState
	OnCodeRotated(ctx context.Context, r domain.Room) domain.RoomState
	SetHostPresent(present bool) domain.RoomState
	ReapplyAdapter(ctx context.Context) error

	// CanaryDue avisa de que se acaba de aplicar la protección y toca
	// comprobarla. La sesión no lanza goroutines propias: quién corre la ronda
	// es política de ciclo de vida, y eso vive acá.
	CanaryDue() <-chan struct{}
	// RunCanaryRound comprueba desde la red que la compuerta contiene. Tarda
	// hasta CanaryRoundDeadline, así que NO puede correr dentro del despachador.
	RunCanaryRound(ctx context.Context, afterApply bool) domain.CanaryCheck
	// OnCanaryRequest es el lado del invitado: marca al host y contesta. Tarda
	// hasta seis segundos, por lo mismo.
	OnCanaryRequest(ctx context.Context, req domain.CanaryRequest) error
}

// EngineSource es lo único que el supervisor le puede pedir al motor.
type EngineSource interface {
	Events() <-chan domain.EngineEvent
	Restart(ctx context.Context) error
}

// ControlSource son los canales de entrada del canal de control.
//
// Sin Serve, sin Dial y sin Close: el ciclo de vida del canal es de los casos
// de uso, y un supervisor que pudiera cerrarlo sería un supervisor capaz de
// cortar la sala por su cuenta.
type ControlSource interface {
	HostPresence() <-chan bool
	Announcements() <-chan domain.RoomAnnounce
	Notices() <-chan domain.RoomNotice
	Codes() <-chan domain.Room
	// CanaryRequests son los pedidos del host, que solo le llegan a un invitado.
	//
	// CanaryReports NO está acá y es deliberado: un informe solo significa algo
	// dentro de una ronda abierta, correlacionado por puerto. Drenado por el
	// supervisor habría que aparcarlo en algún sitio hasta que una ronda lo
	// pidiera, que es un segundo estado con su propia caducidad. La ronda lee ese
	// canal directo durante sus diez segundos, y fuera de una ronda el emisor del
	// adaptador descarta con aviso en vez de bloquear, que es lo correcto: un
	// informe sin ronda abierta es el de un canario ya cerrado.
	CanaryRequests() <-chan domain.CanaryRequest
}

// ErrNotWired es que falta algún puerto. Un supervisor a medias no arranca:
// uno sin System no reaplicaría el adaptador, y el síntoma sería "ayer
// funcionaba" un mes después.
var ErrNotWired = errors.New("supervisor: el cableado no está completo, faltan")

// Deps son las piezas del supervisor.
type Deps struct {
	Room    Room
	Engine  EngineSource
	Control ControlSource
	System  port.SystemEvents
	Log     port.Logger
}

func (d Deps) validate() error {
	faltan := make([]string, 0, 5)
	nombrar := func(n string, ok bool) {
		if !ok {
			faltan = append(faltan, n)
		}
	}
	nombrar("Room", d.Room != nil)
	nombrar("Engine", d.Engine != nil)
	nombrar("Control", d.Control != nil)
	nombrar("System", d.System != nil)
	nombrar("Log", d.Log != nil)

	if len(faltan) > 0 {
		return fmt.Errorf("%w: %v", ErrNotWired, faltan)
	}
	return nil
}

// Las cadencias. Son constantes de compilación y NO se configuran desde fuera:
// el corte por ausencia del host de la decisión 20 no puede depender de un
// valor que un agente externo pueda cambiar para quedarse conectado siempre.
const (
	// Beat es cada cuánto se evalúan los vencimientos. Quince segundos es la
	// granularidad de los treinta de KickGrace, y son veinticuatro muestras
	// dentro del plazo más corto que se vigila, los seis minutos de silencio.
	Beat = 15 * time.Second
	// Sweep es cada cuánto corre el módulo de exposición. Va aparte del latido
	// porque hace siempre tres llamadas al sistema, una al IGD del router, que
	// en la mayoría de los routers termina en timeout. Un latido para el
	// tiempo, un barrido para el mundo.
	Sweep = 60 * time.Second
	// AdapterReapplyEvery es cada cuántos latidos se reaplican los ajustes del
	// adaptador aunque Windows no haya avisado nada.
	//
	// Es el respaldo de la suscripción al Event ID 10000. Ocho latidos son dos
	// minutos, y sin esto una suscripción muerta se traduce exactamente en
	// "ayer funcionaba".
	AdapterReapplyEvery = 8
)

// backoff es la escalera del watchdog del motor.
//
// Arranca corto porque el caso común es que el motor se muera por un adaptador
// que Windows quitó y vuelve en segundos, y se aplana en un minuto porque más
// allá de ahí reintentar no es lo que arregla nada.
//
// Sin jitter: hay una máquina reiniciando un hijo local, no hay manada que
// dispersar.
var backoff = [...]time.Duration{
	1 * time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second,
	20 * time.Second, 40 * time.Second, 60 * time.Second, 60 * time.Second,
}

// MaxRestarts es cuántas veces se reintenta antes de rendirse.
//
// La suma de la escalera son 198 segundos, tres minutos y dieciocho, que entra
// holgada dentro de [domain.ReconnectLimit]. Ese orden importa y no es
// casualidad: el plazo de core es el RESPALDO de este watchdog, no su
// competidor, y si se cruzaran la sala se cerraría a mitad de un reintento que
// iba a funcionar.
const MaxRestarts = len(backoff)

// tag identifica de dónde vino un item, para el log y para el test.
type tag string

const (
	tagEngine   tag = "motor"
	tagPresence tag = "presencia"
	tagAnnounce tag = "anuncio"
	tagNotice   tag = "aviso"
	tagCode     tag = "código"
	tagNetID    tag = "identificación-de-red"
	tagResume   tag = "despertar"
	tagNetChg   tag = "cambio-de-red"
	tagBeat     tag = "latido"
	tagSweep    tag = "barrido"
	tagRestart  tag = "reinicio"

	// tagCanaryDue es "se aplicó la protección, toca comprobarla".
	tagCanaryDue tag = "canario-tras-aplicar"
	// tagCanaryAsk es el host pidiéndole a este invitado que marque.
	tagCanaryAsk tag = "canario-pedido"
	// tagCanaryDone vuelve por el canal de trabajo cuando la ronda termina.
	//
	// Vuelve por acá y no libera la bandera desde su propia goroutine para que
	// canarioVivo lo siga tocando SOLO el despachador, igual que intentos y
	// latidos. Así el supervisor sigue siendo de un solo hilo para su estado.
	tagCanaryDone tag = "canario-terminado"
)

type item struct {
	tag   tag
	value any
	// closed marca que el canal de esa fuente se cerró, o sea que el adaptador
	// se apagó. Es distinto de que no llegue nada: eso último no es nada.
	closed bool
}

// Supervisor es el bucle.
type Supervisor struct {
	deps Deps

	// work junta todo lo que hay que atender. Amortiguado a propósito: es una
	// de las tres defensas contra el abrazo mortal de cerrar el canal de
	// control con el candado de la sesión tomado. Ver Run.
	work chan item

	// beats y sweeps son los relojes. Se inyectan solo desde los tests de este
	// paquete, y no hay forma de cambiarlos desde fuera.
	beats  <-chan time.Time
	sweeps <-chan time.Time
	stop   func()

	// after es time.AfterFunc, cambiable solo desde los tests.
	after func(time.Duration, func()) *time.Timer

	// Todo lo de abajo lo toca SOLO la goroutine despachadora. El supervisor no
	// tiene candado propio, y esa ausencia es la garantía: no hay ningún orden
	// de adquisición que razonar contra el de la sesión.
	intentos int
	latidos  int
	fuentes  map[tag]any

	// canarioVivo es el single-flight de la ronda.
	//
	// Se comparte entre la ronda del host y la atención del pedido en el
	// invitado, y alcanza con una: una máquina no es host e invitado de la misma
	// sala. Descartar un pedido mientras hay algo en vuelo es lo correcto, porque
	// el host se queda sin informe de ese miembro y lo cuenta como sin confirmar,
	// que es el lado seguro.
	canarioVivo bool
}

// New arma el supervisor con sus relojes de verdad.
//
// No recibe intervalos. Las cadencias son constantes de compilación por lo
// mismo que los plazos de core: si se pudieran configurar, un proceso local
// podría poner el latido en un siglo y quedarse en una sala para siempre.
func New(d Deps) (*Supervisor, error) {
	if err := d.validate(); err != nil {
		return nil, err
	}
	latido := time.NewTicker(Beat)
	barrido := time.NewTicker(Sweep)
	s := nuevo(d, latido.C, barrido.C)
	s.stop = func() {
		latido.Stop()
		barrido.Stop()
	}
	return s, nil
}

func nuevo(d Deps, beats, sweeps <-chan time.Time) *Supervisor {
	return &Supervisor{
		deps:    d,
		work:    make(chan item, 64),
		beats:   beats,
		sweeps:  sweeps,
		stop:    func() {},
		after:   time.AfterFunc,
		fuentes: map[tag]any{},
	}
}

// Run corre hasta que se cancele el contexto.
//
// # La forma, y por qué es esta
//
// N drenajes y UN despachador. Cada canal tiene su goroutine de drenaje, que no
// contiene código de usuario y por lo tanto no puede entrar en pánico; empuja
// items al canal de trabajo. El despachador los consume y llama al manejador
// dentro de un recover por item.
//
// Un despachador y no varios, a propósito: todos los manejadores toman el
// candado de la sesión de todas formas, así que despachar en paralelo solo
// agregaría contención y reordenamiento. Con uno, el supervisor entero es de un
// solo hilo desde el punto de vista del análisis.
//
// # El abrazo mortal que esta forma evita
//
// Salir de la sala llama a Close del canal de control CON el candado de la
// sesión tomado. Si ese Close esperara a su goroutine emisora, y esa goroutine
// estuviera bloqueada escribiendo en HostPresence, las dos se esperarían para
// siempre. Hacen falta las tres defensas y ninguna es opcional: el canal de
// trabajo amortiguado, que los drenajes sigan drenando mientras un manejador
// corre, y la frase de contrato en el puerto que dice que Close no espera
// lector.
//
// Nunca devuelve por una fuente muerta. El usuario puede crear otra sala y el
// bucle tiene que seguir ahí.
func (s *Supervisor) Run(ctx context.Context, ready chan<- struct{}) error {
	defer s.stop()

	s.resuscribir(ctx)
	s.arrancarRelojes(ctx)

	// Up: subscriptions and timers are live, so an order arriving now gets
	// handled. The entry point opens on the back of this, never before. See
	// the Bucle contract in daemon/service.
	close(ready)

	for {
		// El despachador se relanza tras un pánico. Diez por minuto de tope,
		// para que un pánico en bucle no queme la CPU en vez de fallar.
		reintentos := 0
		for {
			if s.despachar(ctx) {
				return ctx.Err()
			}
			reintentos++
			if reintentos > 10 {
				s.deps.Log.Error("el despachador entró en pánico demasiadas veces seguidas")
				break
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

// despachar consume el canal de trabajo. Devuelve true si terminó por
// cancelación, y false si volvió de un pánico y hay que relanzarlo.
func (s *Supervisor) despachar(ctx context.Context) (terminó bool) {
	defer func() {
		if r := recover(); r != nil {
			s.deps.Log.Error("pánico en el despachador del supervisor",
				"pánico", fmt.Sprint(r), "pila", string(debug.Stack()))
			terminó = false
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return true
		case it := <-s.work:
			s.atender(ctx, it)
		}
	}
}

// atender ejecuta un manejador con su propio recover.
//
// Por item y no por bucle: un mensaje malformado que haga entrar en pánico a un
// manejador cuesta UN evento perdido, y no puede llevarse por delante el latido
// que hace vencer el contador de veinte minutos.
func (s *Supervisor) atender(ctx context.Context, it item) {
	defer func() {
		if r := recover(); r != nil {
			s.deps.Log.Error("pánico atendiendo un evento, el bucle sigue",
				"fuente", string(it.tag), "pánico", fmt.Sprint(r), "pila", string(debug.Stack()))
		}
	}()
	s.manejar(ctx, it)
}

func (s *Supervisor) manejar(ctx context.Context, it item) {
	if it.closed {
		s.fuenteCerrada(ctx, it.tag)
		return
	}

	switch it.tag {
	case tagEngine:
		ev, _ := it.value.(domain.EngineEvent)
		s.evento(ctx, ev)

	case tagPresence:
		presente, _ := it.value.(bool)
		s.deps.Room.SetHostPresent(presente)

	case tagAnnounce:
		a, _ := it.value.(domain.RoomAnnounce)
		if _, err := s.deps.Room.OnRoomAnnounce(ctx, a); err != nil {
			s.deps.Log.Warn("no se pudo aplicar el anuncio del host", "error", err)
		}

	case tagNotice:
		n, _ := it.value.(domain.RoomNotice)
		s.deps.Room.OnRoomNotice(ctx, n)

	case tagCode:
		r, _ := it.value.(domain.Room)
		s.deps.Room.OnCodeRotated(ctx, r)

	case tagNetID:
		// Windows acaba de identificar una red, o sea que acaba de revertir la
		// métrica, la categoría y las rutas del adaptador.
		s.reaplicarAdaptador(ctx, "identificación de red")

	case tagResume, tagNetChg:
		s.revalidar(ctx, it.tag)

	case tagBeat:
		s.latido(ctx)

	case tagSweep:
		s.deps.Room.RefreshAlerts(ctx)
		s.rondaCanario(ctx, false)

	case tagCanaryDue:
		s.rondaCanario(ctx, true)

	case tagCanaryAsk:
		req, _ := it.value.(domain.CanaryRequest)
		s.atenderPedidoCanario(ctx, req)

	case tagCanaryDone:
		s.canarioVivo = false

	case tagRestart:
		s.reiniciarMotor(ctx)
	}
}

// evento pasa lo del motor a la sesión y lleva la cuenta del watchdog.
func (s *Supervisor) evento(ctx context.Context, ev domain.EngineEvent) {
	if ev.Kind == domain.EngineConnected {
		// Una conexión buena reinicia la escalera. Sin esto, un motor que se
		// cae una vez por hora agotaría los intentos en una tarde.
		s.intentos = 0
	}
	if _, err := s.deps.Room.OnEngineEvent(ctx, ev); err != nil {
		s.deps.Log.Warn("no se pudo aplicar un evento del motor", "tipo", ev.Kind.String(), "error", err)
	}
	if ev.Kind == domain.EngineDied {
		s.programarReinicio(ctx)
	}
}

// latido evalúa los plazos y hace el mantenimiento periódico.
func (s *Supervisor) latido(ctx context.Context) {
	s.latidos++
	s.deps.Room.Tick(ctx)

	// Reaplicar el adaptador cada tantos latidos es el respaldo de la
	// suscripción a los eventos de Windows, que puede morirse sin avisar.
	if s.latidos%AdapterReapplyEvery == 0 {
		s.reaplicarAdaptador(ctx, "respaldo periódico")
	}
	// Y volver a suscribirse es el respaldo del cableado. Es el requisito
	// aplicado al propio supervisor: nada depende de que la capa anterior haya
	// funcionado, ni siquiera esto.
	s.resuscribir(ctx)
}

// rondaCanario lanza la comprobación FUERA del despachador.
//
// # Por qué fuera, y no es estilo
//
// Una ronda dura hasta diez segundos y el despachador es de un solo hilo.
// Corriéndola dentro, el latido de quince segundos que hace vencer el corte de
// los veinte minutos se quedaría esperando a la red. Un latido perdido es
// exactamente lo que el corte automático no se puede permitir.
//
// # El estado sigue siendo de un solo hilo
//
// `canarioVivo` lo lee y lo escribe SOLO el despachador: se pone acá, y se quita
// con un [tagCanaryDone] que vuelve por el mismo canal de trabajo que todo lo
// demás. Nadie toca ese campo desde la goroutine lanzada.
//
// # El recover es propio y no sobra
//
// El de [Supervisor.atender] cubre la llamada a esto, no a la goroutine que esto
// lanza. Un pánico ahí dentro se llevaría el proceso entero, que corre como
// SYSTEM.
func (s *Supervisor) rondaCanario(ctx context.Context, trasAplicar bool) {
	if s.canarioVivo {
		return
	}
	s.canarioVivo = true

	go func() {
		defer func() {
			if r := recover(); r != nil {
				s.deps.Log.Error("pánico en la ronda del canario, el bucle sigue",
					"pánico", fmt.Sprint(r), "pila", string(debug.Stack()))
			}
			// Y se libera SIEMPRE, por el canal de trabajo. Un pánico que dejara
			// la bandera puesta apagaría la comprobación para el resto de la vida
			// del proceso, en silencio.
			s.terminóElCanario(ctx)
		}()
		s.deps.Room.RunCanaryRound(ctx, trasAplicar)
	}()
}

// atenderPedidoCanario marca al host y contesta, también fuera del despachador.
//
// Son hasta seis segundos de red en la máquina del invitado, o sea el mismo
// motivo que arriba. Comparte el single-flight: una máquina no es host e
// invitado de la misma sala, así que una bandera alcanza.
func (s *Supervisor) atenderPedidoCanario(ctx context.Context, req domain.CanaryRequest) {
	if s.canarioVivo {
		// El host se queda sin informe de este miembro y lo cuenta como sin
		// confirmar, que es el lado seguro de equivocarse.
		s.deps.Log.Info("se descarta un pedido de canario porque ya hay uno en curso")
		return
	}
	s.canarioVivo = true

	go func() {
		defer func() {
			if r := recover(); r != nil {
				s.deps.Log.Error("pánico atendiendo un pedido de canario, el bucle sigue",
					"pánico", fmt.Sprint(r), "pila", string(debug.Stack()))
			}
			s.terminóElCanario(ctx)
		}()
		if err := s.deps.Room.OnCanaryRequest(ctx, req); err != nil {
			s.deps.Log.Warn("no se pudo contestar el pedido de canario", "error", err)
		}
	}()
}

// terminóElCanario devuelve la liberación de la bandera al despachador.
//
// Sin bloquear contra el apagado: si el bucle ya se está yendo, no hay nada que
// liberar y quedarse esperando dejaría una goroutine colgada.
func (s *Supervisor) terminóElCanario(ctx context.Context) {
	select {
	case s.work <- item{tag: tagCanaryDone}:
	case <-ctx.Done():
	}
}

func (s *Supervisor) reaplicarAdaptador(ctx context.Context, motivo string) {
	if err := s.deps.Room.ReapplyAdapter(ctx); err != nil {
		s.deps.Log.Warn("no se pudieron reaplicar los ajustes del adaptador",
			"motivo", motivo, "error", err)
	}
}

// revalidar es lo que hace falta tras despertar o tras cambiar de red.
//
// Fast Startup y suspender dejan endpoints muertos y sesiones colgadas, y una
// IP pública nueva obliga a renegociar. Los dos casos terminan en lo mismo:
// reponer lo que Windows deshizo, empujar al motor si no hay túnel, y releer
// los miembros.
//
// El latido va primero y no último: despertar significa que el reloj de pared
// saltó, y puede haber contadores ya vencidos que no tiene sentido arrastrar un
// cuarto de minuto más.
func (s *Supervisor) revalidar(ctx context.Context, de tag) {
	s.deps.Room.Tick(ctx)
	s.reaplicarAdaptador(ctx, string(de))

	if s.deps.Room.Status().TunnelDown() {
		s.reiniciarMotor(ctx)
	}
	if _, err := s.deps.Room.OnPeersChanged(ctx); err != nil {
		s.deps.Log.Warn("no se pudo releer la lista de miembros", "motivo", string(de), "error", err)
	}
}

// programarReinicio pone el siguiente intento en la escalera.
//
// La espera NO es un Sleep en el despachador, que pararía todo lo demás: es un
// temporizador que empuja un item más al canal de trabajo.
func (s *Supervisor) programarReinicio(ctx context.Context) {
	if s.intentos >= MaxRestarts {
		s.rendirse(ctx)
		return
	}
	espera := backoff[s.intentos]
	s.intentos++
	s.deps.Log.Warn("el motor murió, se reintenta",
		"intento", s.intentos, "de", MaxRestarts, "espera", espera)

	s.after(espera, func() {
		select {
		case s.work <- item{tag: tagRestart}:
		case <-ctx.Done():
		}
	})
}

func (s *Supervisor) reiniciarMotor(ctx context.Context) {
	if err := s.deps.Engine.Restart(ctx); err != nil {
		s.deps.Log.Error("no se pudo reiniciar el motor", "intento", s.intentos, "error", err)
		s.programarReinicio(ctx)
		return
	}
	// El canal de eventos es del proceso, así que tras reiniciar hay uno nuevo
	// y el anterior está cerrado.
	s.resuscribir(ctx)
}

// rendirse cierra la sala y sigue vivo.
//
// Sigue vivo a propósito: el usuario puede crear otra sala, y un daemon que se
// apaga porque el motor falló ocho veces obliga a reiniciar el servicio a mano.
func (s *Supervisor) rendirse(ctx context.Context) {
	motivo := fmt.Sprintf("el motor no volvió tras %d intentos", MaxRestarts)
	s.deps.Log.Error("el watchdog se rinde, se cierra la sala", "intentos", MaxRestarts)
	s.deps.Room.OnEngineGaveUp(ctx, motivo)
	s.intentos = 0
}

// fuenteCerrada reacciona a que un adaptador apagó uno de sus canales.
//
// Cerrado no es lo mismo que ocioso. Ocioso no es nada, y el latido es lo que
// convierte "no pasó nada" en una decisión. Cerrado es información.
func (s *Supervisor) fuenteCerrada(ctx context.Context, de tag) {
	delete(s.fuentes, de)

	switch de {
	case tagEngine:
		// Sin canal de eventos el motor está muerto, se haya dicho o no. Se
		// sintetiza el evento y pasa por el manejador normal, así el watchdog
		// toma el mando sin un camino especial.
		s.deps.Log.Warn("el canal de eventos del motor se cerró")
		s.evento(ctx, domain.EngineEvent{
			Kind:   domain.EngineDied,
			Reason: "el canal de eventos del motor se cerró",
		})

	case tagPresence:
		// Un último "no está" y a esperar. Reconectar el canal no es asunto del
		// supervisor: lo recrean CreateRoom y JoinRoom.
		s.deps.Log.Warn("el canal de presencia del host se cerró")
		s.deps.Room.SetHostPresent(false)

	case tagCanaryDue:
		// Perderlo significa que el canario deja de comprobar justo después de
		// aplicar, que es cuando más importa. El barrido de sesenta segundos
		// sigue siendo el respaldo, y este aviso es lo que lo hace visible.
		s.deps.Log.Warn("se cerró el aviso de comprobar la protección: " +
			"a partir de ahora solo la comprueba el barrido")

	default:
		// Los demás se pierden sin más. El silencio pasa a medirlo
		// HostSilenceLimit, que es el respaldo que existe justo para esto, y
		// los del sistema los cubre la reaplicación periódica del adaptador.
		s.deps.Log.Warn("se cerró un canal de entrada", "fuente", string(de))
	}
}

// resuscribir arranca un drenaje por cada canal que todavía no tenga uno.
//
// Compara por IDENTIDAD de canal, que en Go es una comparación válida. Corre en
// cada latido y no una sola vez al arrancar, porque un Restart del motor
// produce un canal de eventos nuevo, y porque es el requisito aplicado al
// propio supervisor: nada depende de que la capa anterior haya funcionado, ni
// siquiera el cableado.
func (s *Supervisor) resuscribir(ctx context.Context) {
	drenarSi(s, ctx, tagEngine, s.deps.Engine.Events())
	drenarSi(s, ctx, tagPresence, s.deps.Control.HostPresence())
	drenarSi(s, ctx, tagAnnounce, s.deps.Control.Announcements())
	drenarSi(s, ctx, tagNotice, s.deps.Control.Notices())
	drenarSi(s, ctx, tagCode, s.deps.Control.Codes())
	drenarSi(s, ctx, tagCanaryDue, s.deps.Room.CanaryDue())
	drenarSi(s, ctx, tagCanaryAsk, s.deps.Control.CanaryRequests())
	drenarSi(s, ctx, tagNetID, s.deps.System.NetworkIdentified())
	drenarSi(s, ctx, tagResume, s.deps.System.Resumed())
	drenarSi(s, ctx, tagNetChg, s.deps.System.NetworkChanged())
}

// drenarSi arranca el drenaje de un canal si es uno que no se estaba drenando.
func drenarSi[T any](s *Supervisor, ctx context.Context, de tag, ch <-chan T) {
	if ch == nil {
		return
	}
	if visto, hay := s.fuentes[de]; hay && visto == any(ch) {
		return
	}
	s.fuentes[de] = any(ch)
	go drenar(ctx, s.work, de, ch)
}

// drenar mueve un canal al canal de trabajo.
//
// Deliberadamente TONTO. No llama a la sesión, no toma candados y no tiene
// lógica: si un drenaje llamara a un método de la sesión podría bloquearse en
// su candado, y esa contrapresión llegaría hasta el adaptador que está
// sosteniendo el candado del otro lado.
func drenar[T any](ctx context.Context, out chan<- item, de tag, ch <-chan T) {
	for {
		select {
		case <-ctx.Done():
			return
		case v, abierto := <-ch:
			it := item{tag: de, value: any(v), closed: !abierto}
			select {
			case out <- it:
			case <-ctx.Done():
				return
			}
			if !abierto {
				return
			}
		}
	}
}

// arrancarRelojes convierte los tics en items, para que pasen por la misma
// puerta que todo lo demás y hereden la contención de pánico.
func (s *Supervisor) arrancarRelojes(ctx context.Context) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-s.beats:
				select {
				case s.work <- item{tag: tagBeat}:
				case <-ctx.Done():
					return
				}
			case <-s.sweeps:
				select {
				case s.work <- item{tag: tagSweep}:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
}
