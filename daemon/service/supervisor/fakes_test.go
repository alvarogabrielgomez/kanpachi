package supervisor

import (
	"context"
	"net/netip"
	"sync"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// Los dobles del supervisor.
//
// Son chicos porque el supervisor depende de interfaces estrechas y no de la
// sesión entera: lo que se prueba acá es el BUCLE, y la sesión ya tiene sus
// propios tests en core.

type salaFalsa struct {
	mu sync.Mutex

	llamadas []string
	estado   domain.RoomState
	// pánicoEn hace entrar en pánico al manejador de esa llamada, para probar
	// que un manejador roto no se lleva al resto por delante.
	pánicoEn string
	pánicos  int
	máximos  int
	// errReacotar hace fallar el reacotado tras un reinicio.
	errReacotar error

	// Lo del canario.
	canarioDue     chan struct{}
	rondas         []bool // trasAplicar de cada ronda
	pedidosCanario []domain.CanaryRequest
	// bloquear detiene la ronda hasta que se cierre, para probar que el latido
	// sigue corriendo mientras tanto. Lo comparten el canario y el reingreso,
	// que son los dos trabajos largos que corren fuera del despachador.
	bloquear chan struct{}

	// Lo del reingreso del invitado.
	reingresoToca bool
	reingresos    int
}

func (r *salaFalsa) anota(s string) {
	r.mu.Lock()
	pánico := s == r.pánicoEn && (r.máximos == 0 || r.pánicos < r.máximos)
	if pánico {
		r.pánicos++
	}
	r.llamadas = append(r.llamadas, s)
	r.mu.Unlock()

	if pánico {
		panic("pánico de prueba en " + s)
	}
}

func (r *salaFalsa) veces(s string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, l := range r.llamadas {
		if l == s {
			n++
		}
	}
	return n
}

func (r *salaFalsa) ponEstado(st domain.RoomState) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.estado = st
}

func (r *salaFalsa) Status() domain.RoomState {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.estado
}

func (r *salaFalsa) Tick(context.Context) domain.RoomState {
	r.anota("tick")
	return r.Status()
}

func (r *salaFalsa) RefreshAlerts(context.Context) domain.RoomState {
	r.anota("alertas")
	return r.Status()
}

func (r *salaFalsa) OnEngineEvent(_ context.Context, ev domain.EngineEvent) (domain.RoomState, error) {
	r.anota("motor:" + ev.Kind.String())
	return r.Status(), nil
}

func (r *salaFalsa) OnEngineGaveUp(context.Context, string) domain.RoomState {
	r.anota("rendirse")
	return r.Status()
}

// OnEngineRestarted anota, y de eso depende poder afirmar que el reacotado
// ocurre DESPUÉS de reiniciar y no antes: antes no hay adaptadores que acotar.
func (r *salaFalsa) OnEngineRestarted(context.Context) error {
	r.anota("reacotar")
	return r.errReacotar
}

func (r *salaFalsa) OnPeersChanged(context.Context) (domain.RoomState, error) {
	r.anota("miembros")
	return r.Status(), nil
}

func (r *salaFalsa) OnRoomAnnounce(context.Context, domain.RoomAnnounce) (domain.RoomState, error) {
	r.anota("anuncio")
	return r.Status(), nil
}

func (r *salaFalsa) OnRoomNotice(context.Context, domain.RoomNotice) domain.RoomState {
	r.anota("aviso")
	return r.Status()
}

func (r *salaFalsa) OnCodeRotated(context.Context, domain.Room) domain.RoomState {
	r.anota("código")
	return r.Status()
}

func (r *salaFalsa) OnMemberChannelUp(context.Context, netip.Addr) domain.RoomState {
	r.anota("canal-de-miembro")
	return r.Status()
}

func (r *salaFalsa) SetHostPresent(present bool) domain.RoomState {
	if present {
		r.anota("presente")
	} else {
		r.anota("ausente")
	}
	return r.Status()
}

func (r *salaFalsa) ReapplyAdapter(context.Context) error {
	r.anota("adaptador")
	return nil
}

func (r *salaFalsa) CanaryDue() <-chan struct{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.canarioDue == nil {
		r.canarioDue = make(chan struct{}, 1)
	}
	return r.canarioDue
}

func (r *salaFalsa) RunCanaryRound(_ context.Context, trasAplicar bool) domain.CanaryCheck {
	r.mu.Lock()
	bloquear := r.bloquear
	r.rondas = append(r.rondas, trasAplicar)
	r.mu.Unlock()

	r.anota("canario")
	if bloquear != nil {
		<-bloquear
	}
	return domain.CanaryCheck{}
}

func (r *salaFalsa) RejoinDue() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.reingresoToca
}

func (r *salaFalsa) Rejoin(_ context.Context) error {
	r.mu.Lock()
	bloquear := r.bloquear
	r.reingresos++
	r.mu.Unlock()

	r.anota("reingreso")
	if bloquear != nil {
		<-bloquear
	}
	return nil
}

func (r *salaFalsa) OnCanaryRequest(_ context.Context, req domain.CanaryRequest) error {
	r.mu.Lock()
	bloquear := r.bloquear
	r.pedidosCanario = append(r.pedidosCanario, req)
	r.mu.Unlock()

	r.anota("canario:pedido")
	if bloquear != nil {
		<-bloquear
	}
	return nil
}

func (r *salaFalsa) rondasCorridas() []bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]bool(nil), r.rondas...)
}

func (r *salaFalsa) pedidosDeCanario() []domain.CanaryRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]domain.CanaryRequest(nil), r.pedidosCanario...)
}

type motorFalso struct {
	mu        sync.Mutex
	eventos   chan domain.EngineEvent
	reinicios int
	err       error
}

func nuevoMotor() *motorFalso {
	return &motorFalso{eventos: make(chan domain.EngineEvent, 8)}
}

func (m *motorFalso) Events() <-chan domain.EngineEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.eventos
}

func (m *motorFalso) Restart(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reinicios++
	return m.err
}

func (m *motorFalso) veces() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.reinicios
}

// canalNuevo simula lo que hace un Restart de verdad: el canal del proceso
// anterior se cierra y aparece otro.
func (m *motorFalso) canalNuevo() chan domain.EngineEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	close(m.eventos)
	m.eventos = make(chan domain.EngineEvent, 8)
	return m.eventos
}

type controlFalso struct {
	presencia chan bool
	anuncios  chan domain.RoomAnnounce
	avisos    chan domain.RoomNotice
	códigos   chan domain.Room
	pedidos   chan domain.CanaryRequest
	miembros  chan netip.Addr
}

func nuevoControl() *controlFalso {
	return &controlFalso{
		presencia: make(chan bool, 4),
		anuncios:  make(chan domain.RoomAnnounce, 4),
		avisos:    make(chan domain.RoomNotice, 4),
		códigos:   make(chan domain.Room, 4),
		pedidos:   make(chan domain.CanaryRequest, 4),
		miembros:  make(chan netip.Addr, 4),
	}
}

func (c *controlFalso) HostPresence() <-chan bool                 { return c.presencia }
func (c *controlFalso) Announcements() <-chan domain.RoomAnnounce { return c.anuncios }
func (c *controlFalso) Notices() <-chan domain.RoomNotice         { return c.avisos }
func (c *controlFalso) Codes() <-chan domain.Room                 { return c.códigos }

func (c *controlFalso) CanaryRequests() <-chan domain.CanaryRequest { return c.pedidos }

func (c *controlFalso) MemberChannels() <-chan netip.Addr { return c.miembros }

type sistemaFalso struct {
	red      chan struct{}
	despertó chan struct{}
	cambio   chan struct{}
}

func nuevoSistema() *sistemaFalso {
	return &sistemaFalso{
		red:      make(chan struct{}, 4),
		despertó: make(chan struct{}, 4),
		cambio:   make(chan struct{}, 4),
	}
}

func (s *sistemaFalso) NetworkIdentified() <-chan struct{} { return s.red }
func (s *sistemaFalso) Resumed() <-chan struct{}           { return s.despertó }
func (s *sistemaFalso) NetworkChanged() <-chan struct{}    { return s.cambio }
func (s *sistemaFalso) Close() error                       { return nil }

type logMudo struct{}

func (logMudo) Info(string, ...any)  {}
func (logMudo) Warn(string, ...any)  {}
func (logMudo) Error(string, ...any) {}

// banco es el escenario montado, con los relojes en la mano.
type banco struct {
	sala     *salaFalsa
	motor    *motorFalso
	control  *controlFalso
	sistema  *sistemaFalso
	latidos  chan time.Time
	barridos chan time.Time
	sup      *Supervisor

	// esperas son los temporizadores del watchdog que quedaron pendientes, para
	// dispararlos a mano sin que el test duerma. demoras guarda con cuánto se
	// programó cada uno, que es como se observa la escalera del backoff sin
	// leerle los campos al supervisor.
	mu      sync.Mutex
	esperas []func()
	demoras []time.Duration
}

func nuevoBanco() *banco {
	b := &banco{
		sala:     &salaFalsa{},
		motor:    nuevoMotor(),
		control:  nuevoControl(),
		sistema:  nuevoSistema(),
		latidos:  make(chan time.Time, 4),
		barridos: make(chan time.Time, 4),
	}
	b.sup = nuevo(Deps{
		Room:    b.sala,
		Engine:  b.motor,
		Control: b.control,
		System:  b.sistema,
		Log:     logMudo{},
	}, b.latidos, b.barridos)

	// El temporizador del watchdog se reemplaza por una cola que el test
	// dispara cuando quiere. Ningún test de este paquete duerme.
	b.sup.after = func(d time.Duration, f func()) *time.Timer {
		b.mu.Lock()
		defer b.mu.Unlock()
		b.esperas = append(b.esperas, f)
		b.demoras = append(b.demoras, d)
		return nil
	}
	return b
}

// disparaEsperas ejecuta los temporizadores pendientes del watchdog.
func (b *banco) disparaEsperas() int {
	b.mu.Lock()
	pendientes := b.esperas
	b.esperas = nil
	b.mu.Unlock()

	for _, f := range pendientes {
		f()
	}
	return len(pendientes)
}

// pendientes son los reintentos programados y todavía sin disparar.
func (b *banco) pendientes() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.esperas)
}

// últimaDemora es con cuánto se programó el último reintento. Es la forma de
// mirar la escalera del backoff sin tocarle los campos al supervisor, que solo
// su goroutine despachadora puede leer sin carrera.
func (b *banco) últimaDemora() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.demoras) == 0 {
		return 0
	}
	return b.demoras[len(b.demoras)-1]
}

// esperaA sondea hasta que la condición se cumpla. Sondear y no dormir un
// tiempo fijo: el test no tiene que saber cuánto tarda el bucle.
func esperaA(t interface {
	Helper()
	Fatalf(string, ...any)
}, qué string, cond func() bool) {
	t.Helper()
	limite := time.Now().Add(2 * time.Second)
	for time.Now().Before(limite) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("no ocurrió a tiempo: %s", qué)
}
