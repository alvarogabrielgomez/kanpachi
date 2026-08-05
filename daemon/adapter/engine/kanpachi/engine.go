package kanpachi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/core/port"
	"github.com/accentiostudios/kanpachi/core/usecase"
	"github.com/accentiostudios/kanpachi/daemon/transport/wire"
)

// maxLine es el tope de una línea del motor.
//
// Con enmarcado por líneas, un mensaje que no cupo deja el flujo
// desincronizado: lo que sigue es la cola de algo que nunca se leyó entero. Por
// eso pasarse es terminal para el proceso y no un aviso.
const maxLine = 1 << 20

// callTimeout es lo que se espera una respuesta antes de darla por perdida.
//
// Existe porque el motor podría no contestar nunca, y sin plazo el llamador se
// queda colgado con la sala a medio abrir. Es holgado a propósito: `host` crea
// un adaptador virtual, que en una máquina cargada tarda segundos.
const callTimeout = 45 * time.Second

// child es el proceso hijo, detrás de una interfaz para que este fichero sea
// Go puro y lo pruebe el CI de Linux.
type child interface {
	Stdin() io.WriteCloser
	Stdout() io.Reader
	// Wait espera a que muera. Vuelve una sola vez.
	Wait() error
	// Kill lo mata sin pedir permiso, y con él a todo lo que haya arrancado.
	Kill() error
}

// spawner lanza el motor. En Windows lo implementa child_windows.go.
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

	// last es la ÚLTIMA orden de arranque, guardada para Restart.
	//
	// Vive acá y no vuelve a core, y por eso Restart existe en vez de repetir
	// HostNetwork: el secreto de la red real se queda en el único sitio del
	// programa que lo necesita.
	last *request
}

// New arma el adaptador. No lanza nada todavía: el proceso arranca con la
// primera orden que lo necesite.
func New(deps Deps) (*Engine, error) {
	if deps.Exe == "" {
		return nil, errors.New("el adaptador del motor necesita la ruta del ejecutable")
	}
	if deps.Resolve == nil {
		deps.Resolve = resolveWithNet
	}
	if deps.spawn == nil {
		deps.spawn = spawn
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Engine{
		deps:       deps,
		pending:    map[uint64]chan response{},
		events:     make(chan domain.EngineEvent, 32),
		procCtx:    ctx,
		procCancel: cancel,
	}, nil
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

// ensureLocked deja el proceso vivo, arrancándolo si hace falta.
//
// El contexto que recibe es el de la LLAMADA y solo acota el arranque. El que
// gobierna la vida del proceso es [Engine.procCtx], por el motivo escrito en
// ese campo.
func (e *Engine) ensureLocked(context.Context) error {
	if e.proc != nil {
		return nil
	}
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
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
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
func (e *Engine) HostNetwork(ctx context.Context, spec domain.HostSpec) error {
	uris, err := seedURIs(spec.Seeds, e.deps.Resolve)
	if err != nil {
		return err
	}
	return e.startCall(ctx, func(id uint64) request { return hostRequest(id, spec, uris) })
}

// JoinRendezvous entra al vestíbulo. NO se guarda para Restart: reiniciar el
// motor tiene que devolver la SALA, y el vestíbulo es un paso de paso.
func (e *Engine) JoinRendezvous(ctx context.Context, spec domain.RendezvousSpec) error {
	uris, err := seedURIs(spec.Seeds, e.deps.Resolve)
	if err != nil {
		return err
	}
	_, err = e.call(ctx, func(id uint64) request { return lobbyRequest(id, spec, uris) })
	return err
}

func (e *Engine) LeaveRendezvous(ctx context.Context) error {
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
	return e.startCall(ctx, func(id uint64) request { return guestRequest(id, spec, uris) })
}

// Leave sale de todo, y es IDEMPOTENTE.
//
// Lo llama el camino de error de crear y de entrar, que puede dispararse antes
// de que el motor haya arrancado. Sin motor no hay de qué salir, y devolver
// error ahí convertiría un fallo en dos.
func (e *Engine) Leave(ctx context.Context) error {
	e.mu.Lock()
	arrancado := e.proc != nil
	e.last = nil
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
			TTLSeconds: int64(usecase.CredentialTTL / time.Second),
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
func (e *Engine) Restart(ctx context.Context) error {
	e.mu.Lock()
	last := e.last
	if last == nil {
		e.mu.Unlock()
		return errors.New("no hay ninguna sala que reiniciar: el motor nunca arrancó")
	}
	_ = e.stopLocked()
	e.mu.Unlock()

	_, err := e.call(ctx, func(id uint64) request {
		copia := *last
		copia.ID = id
		return copia
	})
	return err
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
