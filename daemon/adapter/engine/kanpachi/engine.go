package kanpachi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/core/port"
	"github.com/accentiostudios/kanpachi/core/timing"
	"github.com/accentiostudios/kanpachi/daemon/transport/wire"
)

// maxLine es el tope de una línea del motor.
//
// Con enmarcado por líneas, un mensaje que no cupo deja el flujo
// desincronizado: lo que sigue es la cola de algo que nunca se leyó entero. Por
// eso pasarse es terminal para el proceso y no un aviso.
const maxLine = 1 << 20

// child es el proceso hijo, detrás de una interfaz para que este fichero sea Go
// puro y lo prueben los dos CI sin lanzar un proceso de verdad.
type child interface {
	Stdin() io.WriteCloser
	Stdout() io.Reader
	// Wait espera a que muera. Vuelve una sola vez.
	Wait() error
	// Kill lo mata sin pedir permiso, y con él a todo lo que haya arrancado.
	Kill() error
}

// spawner lanza el motor. Lo implementan `child_windows.go` y `child_linux.go`.
type spawner func(ctx context.Context, exe string) (child, error)

// Deps es lo que hay que darle al adaptador.
type Deps struct {
	// Exe es la ruta del motor. La decide quien cablea, y no se busca en el
	// PATH: un PATH que alguien pueda escribir es una forma de que el daemon,
	// que corre como SYSTEM, ejecute otro binario.
	Exe string
	Log port.Logger
	// Resolve es el resolvedor de nombres. Se inyecta para poder probar el
	// rechazo de un seed que apunta a la LAN sin depender del DNS de nadie.
	Resolve func(string) ([]netip.Addr, error)
	// Addrs lee las direcciones de un adaptador. Se inyecta por lo mismo que
	// Resolve: la espera a que la red esté usable se prueba en Linux, donde
	// ninguno de estos adaptadores existe.
	Addrs func(adapter string) ([]netip.Addr, error)

	// Preflight comprueba que la máquina PUEDA construir un adaptador virtual.
	//
	// Se inyecta por lo mismo que Addrs: lo que hace de verdad es privativo de
	// Windows, y este fichero tiene que compilar y probarse en los dos sitios.
	// Fuera de Windows la implementación no comprueba nada, y ahí eso es la
	// respuesta correcta. Ver `preflight_other.go`.
	Preflight func(engineDir string) error

	// OnFatal avisa de que esta MÁQUINA no sirve, y de nada más.
	//
	// Hoy lo dispara una sola cosa, [ErrPreflight], y esa es toda su
	// competencia: no es un canal de errores. Un fallo al abrir una sala vuelve
	// por donde vino y lo enseña quien la pidió; esto es para el caso en que no
	// hay sala posible en esta máquina y seguir abierto solo sirve para que la
	// persona lo intente otras seis veces.
	//
	// Se llama en una goroutine y a propósito: quien lo implementa levanta un
	// cuadro de diálogo que no vuelve hasta que alguien pulsa Aceptar, y esta
	// llamada sale de debajo del mutex del adaptador.
	//
	// Nil vale, y es lo que hace la variante sin ventana: en un servidor no hay
	// a quién enseñárselo, así que el error vuelve por el canal y ya está.
	OnFatal func(error)

	// LogDir es dónde el MOTOR escribe su propio log, y viaja en cada orden de
	// arranque.
	//
	// Vacío significa que no escribe nada, que es como se comportó hasta ahora.
	// Se le dice desde acá porque el motor no puede deducirlo: en el bundle
	// portable vive en un directorio temporal que se borra al salir, así que un
	// log junto a su ejecutable moriría justo cuando alguien lo iba a mandar.
	LogDir string

	// Progress es el diario de la operación en curso, cuando la hay.
	//
	// Lo escribe este adaptador porque es el único que sabe dos cosas que
	// desde arriba no se ven: cuándo se arranca el PROCESO del motor, que solo
	// pasa la primera vez, y cuánto tardó el adaptador virtual en quedar
	// utilizable. Esa espera es la parte larga de crear una sala, y sin
	// contarla la pantalla se queda quieta durante segundos sin decir en qué.
	//
	// Nil vale: se sustituye por [port.NoProgress] en [New].
	Progress port.ProgressSink

	spawn spawner
}

// Engine implementa port.EnginePort conduciendo el motor propio.
type Engine struct {
	deps Deps

	mu   sync.Mutex
	proc child
	w    *wire.Writer
	// writeMu serializa las escrituras al tubo, y es OTRO mutex a propósito.
	//
	// Escribir con `mu` tomado es un interbloqueo latente: si el búfer de
	// entrada del hijo se llenara, `Write` se quedaría esperando con `mu` en la
	// mano, y el lector, que necesita `mu` para repartir las respuestas, no
	// podría vaciar nada. Los dos esperando al otro.
	//
	// En la práctica los mensajes son cortos y el hijo lee sin parar, así que
	// no pasaría casi nunca. Casi nunca es exactamente cuándo aparecen estos, y
	// el síntoma sería una sala congelada sin ningún error en ningún log.
	//
	// Hace falta uno igual porque `wire.Writer` lleva un `bufio.Writer` dentro,
	// que no admite dos escritores a la vez.
	writeMu sync.Mutex
	nextID  uint64
	pending map[uint64]chan response

	// events vive lo que vive el adaptador, y se cierra SOLO en Close.
	//
	// # El fallo que esto arregla, medido con el daemon de verdad
	//
	// La primera versión creaba un canal por proceso y devolvía uno CERRADO
	// mientras no hubiera ninguno. El supervisor lee un canal cerrado como
	// muerte, y lo dice en su propio código: *"Sin canal de eventos el motor
	// está muerto, se haya dicho o no"*. O sea que "todavía no arrancó" y "se
	// murió" eran el mismo hecho para quien escucha.
	//
	// El resultado no fue sutil. Con el daemon recién arrancado y sin ninguna
	// sala, el watchdog gastó sus ocho intentos, cada uno fallando con "no hay
	// ninguna sala que reiniciar", y terminó cerrando una sala que el usuario
	// nunca llegó a crear.
	//
	// Con un solo canal, "no arrancó" es silencio y "murió" es un
	// [domain.EngineDied] explícito. Son dos hechos distintos y ahora se ven
	// distintos.
	events chan domain.EngineEvent
	// closed evita mandar por un canal ya cerrado, que sería un pánico dentro
	// del daemon.
	closed bool

	// procCtx acota la vida del proceso hijo a la vida del ADAPTADOR.
	//
	// # El fallo que esto arregla, y por qué no se veía
	//
	// La primera versión le pasaba a `spawn` el contexto de la llamada. Ese
	// contexto lleva un plazo y un `defer cancel()`, y `exec.CommandContext`
	// mata el proceso cuando su contexto termina. O sea que el motor moría en
	// cuanto contestaba a la orden que lo había arrancado.
	//
	// Nada lo delataba: `HostNetwork` devolvía éxito, porque la respuesta
	// llegaba antes de que el `cancel` corriera. Lo que quedaba era una red que
	// se levantaba y se caía sola un instante después, con un `exit status 1`
	// que no dice nada.
	procCtx    context.Context
	procCancel context.CancelFunc

	// last es la orden que levantó la SALA, guardada para Restart.
	//
	// Vive acá y no vuelve a core, y por eso Restart existe en vez de repetir
	// HostNetwork: el secreto de la red real se queda en el único sitio del
	// programa que lo necesita.
	last *request

	// lastLobby es la orden que levantó el VESTÍBULO, o nil.
	//
	// # Por qué son dos campos y no uno
	//
	// Porque el motor conduce DOS redes y `Restart` tiene que devolver las dos.
	// Con un solo campo, la última orden pisaba a la anterior, así que tras un
	// reinicio del watchdog volvía una red y la otra no.
	//
	// Medido con el producto entero: al matar el motor a lo bruto con una sala
	// de host abierta, volvía `kanpachi0` y NO volvía `kanpachi1`, y a partir de
	// ahí la regla de la puerta no se podía escribir porque su adaptador ya no
	// existía. La sala seguía en pie con la puerta cerrada para siempre.
	//
	// El comentario que había decía que el vestíbulo es "un paso de paso", y eso
	// es cierto para el INVITADO, que lo suelta al entrar. Para el host es su
	// puerta, y dura lo que dura la sala. Por eso lo que decide no es una regla
	// escrita acá: es si el vestíbulo sigue levantado, que es justo lo que este
	// campo dice.
	lastLobby *request

	// tunFail late cerrado cuando el motor reportó que un adaptador virtual
	// FALLÓ, y tunFailMsg lleva su motivo.
	//
	// # El fallo que esto corta, medido
	//
	// El motor emite `TunDeviceError` con la causa exacta en el primer segundo
	// —`Could not install driver ... (0x000000CB)` en la máquina de un invitado
	// el 2026-08-11— y [Engine.awaitAddress] se quedaba los treinta segundos
	// enteros mirando una dirección que no iba a llegar, para terminar en un
	// mensaje sobre direcciones. El pestillo es lo que deja que la espera se
	// corte con la causa de verdad.
	//
	// # Por qué es un PESTILLO y no un canal de paso
	//
	// Porque el evento puede llegar ANTES de que la espera empiece: entre la
	// respuesta a la orden de arranque y el primer sondeo hay una ventana, y en
	// el caso medido el fallo cayó dentro. Un canal cerrado se observa después
	// de cerrado; un mensaje mandado a nadie se pierde. Se rearma en cada orden
	// de arranque, bajo `mu`, para que el fallo de un intento anterior no mate
	// el intento nuevo.
	tunFail    chan struct{}
	tunFailMsg string

	// preflightRan y preflightErr recuerdan el sondeo de la máquina.
	//
	// Se guarda el resultado, y el fallo TAMBIÉN, porque las dos respuestas son
	// estables: un driver que no se deja instalar no se va a dejar en el intento
	// siguiente, y el sondeo cuesta un par de segundos y crea un dispositivo. Sin
	// esto, cada reintento del watchdog pagaría el sondeo entero para volver a
	// enterarse de lo mismo.
	//
	// Los dos campos van bajo `mu`, que es el que tiene tomado [Engine.ensureLocked].
	preflightRan bool
	preflightErr error
}

// ErrPreflight marca que esta MÁQUINA no puede construir un adaptador virtual.
//
// Va aparte de cualquier otro fallo del arranque porque se responde distinto: no
// es una sala que salió mal ni un motor que se cayó, es que acá no hay red
// virtual posible y no la va a haber reintentando. Quien lo recibe lo enseña y
// apaga, en vez de mandar al watchdog a reintentar ocho veces algo que no
// depende de nosotros.
var ErrPreflight = errors.New("esta máquina no puede crear el adaptador virtual")

// preflightError lleva el texto LARGO y contesta que sí a [ErrPreflight].
//
// Son las dos cosas a la vez y por eso no basta un `fmt.Errorf("%w: %w")`: ese
// texto va derecho a un cuadro de diálogo que lee alguien que quería jugar, y
// anteponerle una frase de clasificación lo empeora. Quien decide qué hacer
// pregunta con `errors.Is`, que es una pregunta y no una lectura.
type preflightError struct{ err error }

func (p preflightError) Error() string        { return p.err.Error() }
func (p preflightError) Unwrap() error        { return p.err }
func (p preflightError) Is(target error) bool { return target == ErrPreflight }

// New arma el adaptador. No lanza nada todavía: el proceso arranca con la
// primera orden que lo necesite.
func New(deps Deps) (*Engine, error) {
	if deps.Exe == "" {
		return nil, errors.New("el adaptador del motor necesita la ruta del ejecutable")
	}
	if deps.Resolve == nil {
		deps.Resolve = resolveWithNet
	}
	if deps.Addrs == nil {
		deps.Addrs = addressesOf
	}
	// El sondeo de la máquina va PEGADO al lanzador de verdad, y no es
	// organización: lo que mide es la máquina donde ese lanzador va a correr.
	// Quien sustituye el lanzador no tiene máquina que medir —su hijo es un par
	// de tuberías— y sondear la de verdad ahí haría que un test del protocolo
	// dependiera de que el anfitrión tenga wintun. `spawn` no se exporta, así que
	// ese "quien" solo puede ser un test de este paquete: en producción son
	// siempre los dos reales, y no hay forma de pedir uno sin el otro.
	if deps.spawn == nil {
		deps.spawn = spawn
		if deps.Preflight == nil {
			deps.Preflight = Preflight
		}
	}
	if deps.Preflight == nil {
		deps.Preflight = func(string) error { return nil }
	}
	// Un sumidero que no anota es mejor que un nil comprobado en cada llamada:
	// esa comprobación es la que un día falta en el camino nuevo.
	if deps.Progress == nil {
		deps.Progress = port.NoProgress{}
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Engine{
		deps:       deps,
		pending:    map[uint64]chan response{},
		events:     make(chan domain.EngineEvent, 32),
		tunFail:    make(chan struct{}),
		procCtx:    ctx,
		procCancel: cancel,
	}, nil
}

// tunFailPrefix es cómo empieza el motivo del evento con el que el motor cuenta
// que un adaptador virtual falló.
//
// Es la otra mitad de un contrato entre repos: la cadena la arma `pump` en
// `kanpachi-engine/src/engine.rs`, sobre `TunDeviceError`. Casar por prefijo y
// no por igualdad deja que el motor añada detalle detrás sin romper esto, y el
// coste de que la cadena cambie entera es volver al comportamiento de antes,
// esperar el plazo completo: peor mensaje, jamás un fallo nuevo.
const tunFailPrefix = "the virtual adapter failed"

// armTunLatch deja el pestillo listo para el intento que empieza.
func (e *Engine) armTunLatch() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.tunFail = make(chan struct{})
	e.tunFailMsg = ""
}

// tunFailed cierra el pestillo, una sola vez por armado.
func (e *Engine) tunFailed(reason string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	select {
	case <-e.tunFail:
		// Ya estaba cerrado: se conserva el PRIMER motivo, que es el del fallo
		// original y no el de un eco posterior.
	default:
		e.tunFailMsg = reason
		close(e.tunFail)
	}
}

// tunFailState devuelve el canal y el motivo actuales, para observarlos sin
// carreras con el rearmado.
func (e *Engine) tunFailState() (<-chan struct{}, func() string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	ch := e.tunFail
	return ch, func() string {
		e.mu.Lock()
		defer e.mu.Unlock()
		return e.tunFailMsg
	}
}

func resolveWithNet(host string) ([]netip.Addr, error) {
	return net.DefaultResolver.LookupNetIP(context.Background(), "ip", host)
}

// Close apaga el motor y cierra el canal de eventos. Es idempotente.
//
// Es el ÚNICO sitio que cierra ese canal, y por eso el supervisor puede leer un
// cierre como el fin del adaptador y no como una muerte más del proceso.
func (e *Engine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	err := e.stopLocked()
	// Después de parar el proceso, no antes: cancelar primero haría que
	// `CommandContext` lo matara sin darle la salida limpia por stdin.
	e.procCancel()
	if !e.closed {
		e.closed = true
		close(e.events)
	}
	return err
}

// KillOrphans mata motores que dejó otra ejecución, y devuelve cuántos.
//
// # Por qué está expuesto, si arrancar ya lo hace
//
// Porque arrancar necesita arrancar. El hard reset existe justo para la máquina
// donde el daemon YA NO LEVANTA, y ahí un motor huérfano es lo primero que hay
// que quitar: mientras siga vivo la red virtual sigue arriba, así que purgar el
// firewall antes dejaría un adaptador con tráfico y sin nada conteniéndolo.
//
// Se compara por RUTA COMPLETA y jamás por nombre. Esto corre como SYSTEM en
// Windows y como root en Linux, o sea que matar cualquier proceso llamado
// `kanpachi-engine` sería matar el de otra instalación, y podría.
func (e *Engine) KillOrphans() int {
	abs, err := filepath.Abs(e.deps.Exe)
	if err != nil {
		// Sin ruta absoluta no se puede comparar por ruta completa, y comparar
		// por nombre está prohibido. No matar nada es la respuesta correcta.
		e.warn("no se pudo resolver la ruta del motor, así que no se buscaron huérfanos",
			"exe", e.deps.Exe, "error", err)
		return 0
	}
	return killOrphans(abs)
}

// stopLocked mata el proceso SIN tocar el canal de eventos.
func (e *Engine) stopLocked() error {
	if e.proc == nil {
		return nil
	}
	// Cerrar stdin es la salida limpia: el motor la lee como fin de órdenes,
	// baja las dos redes y termina solo.
	_ = e.proc.Stdin().Close()
	err := e.proc.Kill()
	e.proc = nil
	e.w = nil
	for id, ch := range e.pending {
		close(ch)
		delete(e.pending, id)
	}
	return err
}

// emitLocked manda un evento si queda alguien a quien mandárselo.
//
// Se descarta cuando el búfer está lleno en vez de bloquear: quien llama tiene
// el mutex, y bloquearse acá dejaría al adaptador entero parado por un aviso.
func (e *Engine) emitLocked(ev domain.EngineEvent) {
	if e.closed {
		return
	}
	select {
	case e.events <- ev:
	default:
	}
}

// preflightLocked sondea la máquina UNA vez y recuerda lo que contestó.
//
// # Por qué se recuerda también el fallo
//
// Porque las dos respuestas son estables. Un almacén de drivers que rechaza el
// `.inf` lo va a rechazar igual dentro de un minuto, y el sondeo no es gratis:
// crea un dispositivo de verdad, medido en 678 ms y 952 ms en esta máquina. Sin
// recordar el fallo, cada reintento del watchdog volvería a pagarlo entero para
// enterarse de lo mismo.
//
// # Por qué acá y no al arrancar el daemon
//
// Porque el daemon arranca con la máquina, antes de que haya nadie delante, y un
// sondeo ahí crearía un adaptador en cada encendido para una sala que quizá
// nadie abra. Acá corre en la primera orden que necesita el motor, que es
// exactamente cuando la respuesta importa y hay alguien mirando.
func (e *Engine) preflightLocked() error {
	if e.preflightRan {
		return e.preflightErr
	}
	e.preflightRan = true

	e.deps.Progress.Step(domain.ScopeEngine, "comprobando el adaptador virtual")
	err := e.deps.Preflight(filepath.Dir(e.deps.Exe))
	if err != nil {
		// Se anota acá además de devolverlo: quien llama lo va a enseñar en una
		// ventana, y una ventana no se puede adjuntar a un reporte.
		e.warn("la máquina no puede crear un adaptador virtual", "error", err)
		e.preflightErr = preflightError{err}
		if e.deps.OnFatal != nil {
			// En una goroutine porque acá está tomado `mu` y quien escucha abre
			// una ventana modal: hacerlo en línea dejaría el adaptador entero
			// bloqueado hasta que alguien pulse Aceptar, y el apagado que viene
			// después necesita ese mismo mutex.
			go e.deps.OnFatal(e.preflightErr)
		}
	}
	return e.preflightErr
}

// CheckMachine corre el sondeo AHORA, en vez de esperar a la primera sala.
//
// # Por qué existe si [Engine.ensureLocked] ya lo hace
//
// Porque cuándo se sabe es la mitad del valor. `ensureLocked` corre en la
// primera orden que necesita el motor, o sea cuando alguien ya eligió un juego,
// escribió un código de ocho caracteres y está esperando. Y lo que el sondeo
// mide no depende de esa sala ni de ninguna: depende de la máquina, que estaba
// disponible desde el primer segundo.
//
// Lo llama el arranque del daemon. La comprobación de `ensureLocked` se queda
// igual y no es redundante: cubre a quien construya el adaptador sin pasar por
// ese arranque, y con la caché no cuesta nada.
//
// El resultado queda cacheado, así que la sala siguiente no vuelve a pagarlo.
// Llamarlo es adelantar el sondeo, nunca añadir uno.
func (e *Engine) CheckMachine() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.preflightLocked()
}

// ensureLocked deja el proceso vivo, arrancándolo si hace falta.
//
// El contexto que recibe es el de la LLAMADA y solo acota el arranque. El que
// gobierna la vida del proceso es [Engine.procCtx], por el motivo escrito en
// ese campo.
func (e *Engine) ensureLocked(context.Context) error {
	if e.proc != nil {
		return nil
	}
	// Antes de lanzar nada: si esta máquina no puede construir un adaptador
	// virtual, el motor arrancaría igual, aceptaría la orden, y el fallo llegaría
	// treinta segundos después disfrazado de problema de direcciones. Ver
	// [Engine.preflightLocked].
	if err := e.preflightLocked(); err != nil {
		return err
	}
	// Solo la PRIMERA vez deja rastro, que es justo lo que hace útil el paso:
	// las llamadas siguientes reusan el proceso, y verlo aparecer dos veces en
	// la misma operación diría que el motor se cayó por el camino.
	// El nombre sale de [Deps.Exe] y no está escrito acá: en Linux el fichero se
	// llama `kanpachi-engine`, sin `.exe`, y esto lo LEE el usuario mientras
	// espera a que abra su sala. Un binario que no existe con ese nombre es
	// exactamente lo que uno buscaría al diagnosticar, y no estaría.
	e.deps.Progress.Step(domain.ScopeEngine, "arrancando "+filepath.Base(e.deps.Exe))
	p, err := e.deps.spawn(e.procCtx, e.deps.Exe)
	if err != nil {
		return fmt.Errorf("arrancando el motor: %w", err)
	}
	e.proc = p
	e.w = wire.NewWriter(p.Stdin(), maxLine)

	go e.read(p)
	go e.reap(p)
	return nil
}

// read reparte lo que llega del motor: respuestas por id, eventos al canal.
func (e *Engine) read(p child) {
	r := wire.NewReader(p.Stdout(), maxLine)
	for {
		line, err := r.ReadLine()
		if err != nil {
			return
		}
		var in incoming
		if err := json.Unmarshal(line, &in); err != nil {
			e.warn("el motor mandó una línea ilegible", "error", err)
			continue
		}

		// La ausencia de id es lo que hace que un evento sea un evento.
		if in.ID == nil {
			kind, err := toEventKind(in.Event)
			if err != nil {
				e.warn("evento del motor descartado", "error", err)
				continue
			}
			// El pestillo va ANTES de emitir, y aunque el búfer esté lleno: la
			// espera del adaptador tiene que cortarse aunque el supervisor vaya
			// atrasado leyendo eventos.
			if kind == domain.EngineDisconnected && strings.HasPrefix(in.Reason, tunFailPrefix) {
				e.tunFailed(in.Reason)
			}
			// Si el búfer está lleno se descarta, y no se bloquea el lector:
			// bloquearlo dejaría de repartir también las RESPUESTAS, y con eso
			// la sala entera se cuelga por un aviso.
			e.mu.Lock()
			e.emitLocked(domain.EngineEvent{Kind: kind, Reason: in.Reason})
			e.mu.Unlock()
			continue
		}

		e.mu.Lock()
		ch, ok := e.pending[*in.ID]
		delete(e.pending, *in.ID)
		e.mu.Unlock()
		if !ok {
			e.warn("el motor contestó a una orden que ya no espera nadie", "id", *in.ID)
			continue
		}
		ch <- response{ID: *in.ID, OK: in.OK, Error: in.Error, Data: in.Data}
		close(ch)
	}
}

// reap avisa cuando el proceso muere.
//
// Es el ÚNICO sitio de donde sale [domain.EngineDied], porque un motor muerto
// no puede reportar su propia muerte. El supervisor lo distingue de
// "desconectado" a propósito: acá no hay con quién hablar y hay que arrancar
// otro.
func (e *Engine) reap(p child) {
	err := p.Wait()

	e.mu.Lock()
	defer e.mu.Unlock()

	// Si ya no es el proceso actual, lo reemplazó un Restart y su muerte no es
	// noticia: la sala está viva en el proceso nuevo.
	if e.proc != p {
		return
	}
	e.proc = nil
	e.w = nil
	for id, ch := range e.pending {
		close(ch)
		delete(e.pending, id)
	}

	// Solo se avisa de una muerte si había algo que morir. Sin arranque previo,
	// el proceso terminó porque nadie llegó a usarlo, y anunciarlo como muerte
	// haría que el watchdog gastara sus intentos contra una sala inexistente.
	if e.last == nil {
		return
	}
	razon := "el proceso del motor terminó"
	if err != nil {
		razon = fmt.Sprintf("el proceso del motor terminó: %v", err)
	}
	e.emitLocked(domain.EngineEvent{Kind: domain.EngineDied, Reason: razon})
}

func (e *Engine) warn(msg string, kv ...any) {
	if e.deps.Log != nil {
		e.deps.Log.Warn(msg, kv...)
	}
}

// call manda una orden y espera su respuesta.
func (e *Engine) call(ctx context.Context, build func(id uint64) request) (*responseData, error) {
	ctx, cancel := context.WithTimeout(ctx, timing.EngineCallTimeout)
	defer cancel()

	e.mu.Lock()
	if err := e.ensureLocked(ctx); err != nil {
		e.mu.Unlock()
		return nil, err
	}
	e.nextID++
	id := e.nextID
	req := build(id)
	ch := make(chan response, 1)
	e.pending[id] = ch
	w := e.w
	e.mu.Unlock()

	// FUERA de `mu`, ver el comentario de writeMu.
	e.writeMu.Lock()
	err := w.Write(req)
	e.writeMu.Unlock()

	if err != nil {
		e.mu.Lock()
		delete(e.pending, id)
		e.mu.Unlock()
		return nil, fmt.Errorf("mandándole una orden al motor: %w", err)
	}

	select {
	case <-ctx.Done():
		e.mu.Lock()
		delete(e.pending, id)
		e.mu.Unlock()
		return nil, fmt.Errorf("el motor no contestó a tiempo: %w", ctx.Err())
	case res, abierto := <-ch:
		if !abierto {
			return nil, errors.New("el motor murió antes de contestar")
		}
		if !res.OK {
			return nil, fmt.Errorf("el motor rechazó la orden: %s", res.Error)
		}
		return res.Data, nil
	}
}

// startCall es una orden de arranque: la guarda para Restart si sale bien.
func (e *Engine) startCall(ctx context.Context, build func(id uint64) request) error {
	if _, err := e.call(ctx, build); err != nil {
		return err
	}
	e.mu.Lock()
	req := build(0)
	e.last = &req
	e.mu.Unlock()
	return nil
}

// HostNetwork arranca la red como nodo admin.
//
// No vuelve hasta que el adaptador tiene su dirección. Ver [waitForAddress]:
// contestar antes dejaba a todo lo que va después ligando a una IP que todavía
// no existía.
func (e *Engine) HostNetwork(ctx context.Context, spec domain.HostSpec) error {
	uris, err := seedURIs(spec.Seeds, e.deps.Resolve)
	if err != nil {
		return err
	}
	// Antes de mandar la orden, no antes de esperar: el fallo del adaptador
	// puede llegar entre la respuesta y el primer sondeo. Ver [Engine.tunFail].
	e.armTunLatch()
	if err := e.startCall(ctx, func(id uint64) request { return hostRequest(id, spec, uris, e.deps.LogDir) }); err != nil {
		return err
	}
	return e.awaitAddress(ctx, RoomDevice, domain.HostAddress(spec.Subnet))
}

// JoinRendezvous entra al vestíbulo, y SÍ se guarda para Restart.
//
// Antes no se guardaba, con el argumento de que el vestíbulo es un paso de
// paso. Eso es cierto para el invitado, que lo suelta al entrar, y falso para el
// host: es su puerta, y dura lo que dura la sala. Lo que lo resuelve sin una
// regla por rol es que `LeaveRendezvous` lo olvide, así que se replica
// exactamente mientras siga levantado.
func (e *Engine) JoinRendezvous(ctx context.Context, spec domain.RendezvousSpec) error {
	uris, err := seedURIs(spec.Seeds, e.deps.Resolve)
	if err != nil {
		return err
	}
	// Antes de mandar la orden, no antes de esperar. Ver [Engine.tunFail]. Este
	// es además el camino donde el pestillo pagó su costo de entrada: para un
	// invitado el vestíbulo es la PRIMERA orden, y su fallo era el que se
	// tiraba.
	e.armTunLatch()
	if _, err := e.call(ctx, func(id uint64) request { return lobbyRequest(id, spec, uris, e.deps.LogDir) }); err != nil {
		return err
	}
	e.mu.Lock()
	req := lobbyRequest(0, spec, uris, e.deps.LogDir)
	e.lastLobby = &req
	e.mu.Unlock()
	// La del vestíbulo es la que se midió fallando: el canal de control liga
	// justo acá, en la IP del host dentro del vestíbulo, y Windows contestaba
	// "The requested address is not valid in its context".
	return e.awaitAddress(ctx, LobbyDevice, spec.Address)
}

// awaitAddress espera a que la red esté usable de verdad, y lo anota.
//
// El aviso vale porque una sala que tarda quince segundos en abrir y una que
// falla se ven igual desde fuera mientras pasa.
//
// # La espera se corta si el motor ya dijo por qué no va a llegar
//
// El sondeo de direcciones solo puede distinguir "todavía no" de "nunca"
// agotando el plazo. El motor sí lo sabe antes: cuando wintun no puede crear el
// adaptador emite `TunDeviceError` con la causa exacta en el primer segundo, y
// esperar los treinta enteros para contestar con un mensaje sobre direcciones
// es justo lo que costó dos días de diagnóstico el 2026-08-11. Ver
// [Engine.tunFail].
func (e *Engine) awaitAddress(ctx context.Context, adapter string, want netip.Addr) error {
	inicio := time.Now()
	// Antes de esperar y no después: es el paso más largo de crear una sala, y
	// anotarlo al terminar dejaría la pantalla quieta justo mientras dura.
	e.deps.Progress.Step(domain.ScopeEngine,
		fmt.Sprintf("esperando a que %s tome la dirección %s", adapter, want))

	fallo, motivo := e.tunFailState()
	ctxEspera, cancelar := context.WithCancel(ctx)
	defer cancelar()
	// La goroutine muere sola en los dos finales: si gana la espera, el
	// resultado ya está en el canal con búfer; si gana el pestillo, `cancelar`
	// la hace volver y el canal con búfer recibe su error sin lector.
	res := make(chan error, 1)
	go func() {
		res <- waitForAddress(ctxEspera, e.deps.Addrs, adapter, want, timing.AddressDeadline)
	}()

	select {
	case err := <-res:
		if err != nil {
			return fmt.Errorf("la red arrancó y el adaptador no quedó utilizable: %w", err)
		}
	case <-fallo:
		cancelar()
		e.deps.Progress.Step(domain.ScopeEngine, "el motor reportó que el adaptador falló")
		return fmt.Errorf("el motor no pudo crear el adaptador %q: %s", adapter, motivo())
	}
	tardó := time.Since(inicio).Round(time.Millisecond)
	e.deps.Log.Info("adaptador virtual listo",
		"adaptador", adapter, "dirección", want.String(), "tardó", tardó.String())
	e.deps.Progress.Step(domain.ScopeEngine, fmt.Sprintf("%s listo con %s, tardó %s", adapter, want, tardó))
	return nil
}

// LeaveRendezvous suelta el vestíbulo, y lo OLVIDA para Restart.
//
// Olvidarlo es la mitad que hace que guardarlo sea seguro: el invitado sale del
// vestíbulo antes de entrar a la sala, a propósito, y un reinicio que lo
// levantara otra vez lo devolvería a una red observable por cualquiera que
// tenga el código.
func (e *Engine) LeaveRendezvous(ctx context.Context) error {
	e.mu.Lock()
	e.lastLobby = nil
	e.mu.Unlock()
	_, err := e.call(ctx, func(id uint64) request {
		return request{ID: id, Cmd: command{LeaveRendezvous: &emptyArgs{}}}
	})
	return err
}

func (e *Engine) JoinWithCredential(ctx context.Context, spec domain.GuestSpec) error {
	uris, err := seedURIs(spec.Seeds, e.deps.Resolve)
	if err != nil {
		return err
	}
	// Antes de mandar la orden, no antes de esperar. Ver [Engine.tunFail].
	e.armTunLatch()
	if err := e.startCall(ctx, func(id uint64) request { return guestRequest(id, spec, uris, e.deps.LogDir) }); err != nil {
		return err
	}
	// La dirección del invitado la asignó el host dentro de la credencial, y
	// ahora se le MANDA al motor. Esperarla es lo que hace que marcar al host no
	// salga desde una interfaz que aún no tiene IP.
	//
	// Este comentario decía "y el motor la toma por DHCP de la red", afirmando
	// las dos cosas a la vez. Eran dos algoritmos calculando lo mismo por
	// separado, y coincidían solo mientras nadie se reconectara. Ver
	// `guestAddress` y el `set_dhcp(false)` del motor.
	return e.awaitAddress(ctx, RoomDevice, spec.Credential.VirtualIP)
}

// Leave sale de todo, y es IDEMPOTENTE.
//
// Lo llama el camino de error de crear y de entrar, que puede dispararse antes
// de que el motor haya arrancado. Sin motor no hay de qué salir, y devolver
// error ahí convertiría un fallo en dos.
func (e *Engine) Leave(ctx context.Context) error {
	e.mu.Lock()
	arrancado := e.proc != nil
	e.last, e.lastLobby = nil, nil
	e.mu.Unlock()
	if !arrancado {
		return nil
	}
	_, err := e.call(ctx, func(id uint64) request {
		return request{ID: id, Cmd: command{Leave: &emptyArgs{}}}
	})
	return err
}

func (e *Engine) IssueCredential(ctx context.Context, req domain.CredentialRequest) (domain.Credential, error) {
	data, err := e.call(ctx, func(id uint64) request {
		return request{ID: id, Cmd: command{IssueCredential: &issueArgs{
			TTLSeconds: int64(timing.CredentialTTL / time.Second),
		}}}
	})
	if err != nil {
		return domain.Credential{}, err
	}
	if data == nil || data.Credential == nil {
		return domain.Credential{}, errors.New("el motor dijo que sí y no mandó la credencial")
	}
	return domain.Credential{
		ID:    domain.CredentialID(data.Credential.CredentialID),
		Token: data.Credential.CredentialSecret,
		Name:  req.Name,
	}, nil
}

// RenewCredential empuja el vencimiento conservando la llave. Ver el contrato
// en [port.EnginePort].
//
// El plazo sale de la MISMA constante que usa emitir. Que fueran dos números
// sería la forma de que renovar dejara credenciales con una vida distinta de la
// que el producto declara, sin que nadie lo notara hasta que alguien se cae.
func (e *Engine) RenewCredential(ctx context.Context, id domain.CredentialID, ttl time.Duration) (time.Time, error) {
	if id == "" {
		return time.Time{}, fmt.Errorf("no se puede renovar una credencial sin id")
	}
	data, err := e.call(ctx, func(reqID uint64) request {
		return request{ID: reqID, Cmd: command{RenewCredential: &renewArgs{
			CredentialID: string(id),
			TTLSeconds:   int64(ttl / time.Second),
		}}}
	})
	if err != nil {
		return time.Time{}, err
	}
	if data == nil || data.Renewed == nil {
		return time.Time{}, fmt.Errorf("el motor renovó %s y no dijo hasta cuándo", id)
	}
	return time.Unix(data.Renewed.ExpiryUnix, 0), nil
}

func (e *Engine) RevokeCredential(ctx context.Context, id domain.CredentialID) error {
	_, err := e.call(ctx, func(reqID uint64) request {
		return request{ID: reqID, Cmd: command{RevokeCredential: &revokeArgs{CredentialID: string(id)}}}
	})
	return err
}

func (e *Engine) ListCredentials(ctx context.Context) ([]domain.Credential, error) {
	data, err := e.call(ctx, func(id uint64) request {
		return request{ID: id, Cmd: command{ListCredentials: &emptyArgs{}}}
	})
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, nil
	}
	out := make([]domain.Credential, 0, len(data.Credentials))
	for _, c := range data.Credentials {
		out = append(out, domain.Credential{
			ID:        domain.CredentialID(c.CredentialID),
			ExpiresAt: time.Unix(c.ExpiryUnix, 0),
		})
	}
	return out, nil
}

// Restart vuelve a levantar el motor con la ÚLTIMA especificación.
//
// Es el MECANISMO del watchdog. Cuántas veces y cada cuánto es POLÍTICA y vive
// en el supervisor: este adaptador sabe cómo arrancarlo, no sabe cuándo hay que
// rendirse.
//
// Sin arranque previo devuelve error y no inventa ninguna sala.
// Restart vuelve a levantar LAS DOS redes que estaban arriba.
//
// Las dos, y en ese orden: la sala primero, que es lo que la gente está jugando,
// y después el vestíbulo, que es la puerta. Reponer solo una dejaba la sala en
// pie con la puerta cerrada para siempre, sin nada en pantalla que lo dijera.
func (e *Engine) Restart(ctx context.Context) error {
	e.mu.Lock()
	last, lobby := e.last, e.lastLobby
	if last == nil {
		e.mu.Unlock()
		return errors.New("no hay ninguna sala que reiniciar: el motor nunca arrancó")
	}
	_ = e.stopLocked()
	e.mu.Unlock()

	// Cada orden se ESPERA hasta que su adaptador tenga dirección, igual que en
	// `HostNetwork` y `JoinRendezvous`. Sin eso, `Restart` devolvía éxito con
	// las redes todavía a medio levantar, y quien reacciona a la vuelta del
	// túnel se encontraba adaptadores que aún no existían.
	repetir := func(r *request) error {
		// Un pestillo por orden y no uno para las dos: el `TunDeviceError` de la
		// sala no puede cortar la espera del vestíbulo que viene después.
		e.armTunLatch()
		if _, err := e.call(ctx, func(id uint64) request {
			copia := *r
			copia.ID = id
			return copia
		}); err != nil {
			return err
		}
		adaptador, dir, ok := adapterOf(*r)
		if !ok {
			return nil
		}
		return e.awaitAddress(ctx, adaptador, dir)
	}
	if err := repetir(last); err != nil {
		return err
	}
	if lobby == nil {
		return nil
	}
	// Que falle el vestíbulo NO tumba el reinicio. La sala ya está arriba y la
	// gente que está dentro sigue jugando; lo que se pierde es que entre alguien
	// nuevo, y eso es mucho menos que cerrarlo todo. Queda dicho en el log.
	if err := repetir(lobby); err != nil {
		e.warn("la sala volvió y la puerta no, así que no puede entrar nadie nuevo", "error", err)
	}
	return nil
}

func (e *Engine) Peers(ctx context.Context) ([]domain.Peer, error) {
	data, err := e.call(ctx, func(id uint64) request {
		return request{ID: id, Cmd: command{Peers: &emptyArgs{}}}
	})
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, nil
	}
	return toPeers(data.Peers)
}

// Events devuelve el canal del adaptador, que es SIEMPRE el mismo.
//
// Se cierra únicamente en [Engine.Close]. Un cierre significa que el adaptador
// terminó, jamás que murió un proceso: la muerte de un proceso viaja como
// [domain.EngineDied], que es un hecho distinto y ahora se ve distinto. Ver el
// comentario del campo `events`.
func (e *Engine) Events() <-chan domain.EngineEvent {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.events
}

func (e *Engine) Diagnostics(ctx context.Context) (domain.NetCheck, error) {
	data, err := e.call(ctx, func(id uint64) request {
		return request{ID: id, Cmd: command{Diagnostics: &emptyArgs{}}}
	})
	if err != nil {
		return domain.NetCheck{}, err
	}
	if data == nil || data.Diagnostics == nil {
		return domain.NetCheck{}, errors.New("el motor dijo que sí y no mandó el diagnóstico")
	}
	d := data.Diagnostics
	return domain.NetCheck{
		NATKind:    d.NATKind,
		UDPBlocked: d.UDPBlocked,
		MTU:        int(d.MTU),
	}, nil
}
