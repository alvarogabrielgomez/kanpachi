package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// El orden de arranque y de apagado es lo ÚNICO que este paquete decide, así
// que es lo único que estos tests afirman. Corren en Linux, que es la razón por
// la que el paquete es Go puro.

func TestLaEntradaSeAbreDespuesDelBucle(t *testing.T) {
	// El pipe es la única superficie alcanzable desde fuera del proceso. Si se
	// abriera antes, existiría una ventana en la que la UI puede pedir "entrar a
	// esta sala" con el firewall todavía a medio arreglar.
	orden := &registro{}
	d := deps(orden)

	r, err := Start(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Shutdown(context.Background()) })

	orden.espera(t, 2)
	if got := orden.lista(); got[0] != "bucle" {
		t.Errorf("arrancó %q antes que el bucle: la entrada no puede abrirse primero", got[0])
	}
}

func TestElApagadoVaAlReves(t *testing.T) {
	// Primero la puerta, para que no lleguen órdenes nuevas mientras se cierra.
	// Después la sala, que es lo que cierra los puertos de verdad.
	orden := &registro{}
	d := deps(orden)

	r, err := Start(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	orden.espera(t, 2)
	orden.limpia()

	if err := r.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}

	got := orden.lista()
	if len(got) < 2 || got[0] != "cerrar-entrada" || got[1] != "salir-sala" {
		t.Errorf("el apagado fue %v, se esperaba cerrar la entrada y después salir de la sala", got)
	}
}

func TestElApagadoCorreConElContextoYaCancelado(t *testing.T) {
	// Es el caso REAL: al apagar, el contexto del servicio ya viene cancelado.
	// Sin context.WithoutCancel, cada cierre de puerto sería un no-op, o sea que
	// el apagado limpio no limpiaría nada y las reglas del firewall se quedarían
	// puestas hasta el arranque siguiente.
	orden := &registro{}
	d := deps(orden)

	ctx, cancel := context.WithCancel(context.Background())
	r, err := Start(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	orden.espera(t, 2)
	orden.limpia()

	cancel()
	if err := r.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}

	if !contiene(orden.lista(), "salir-sala") {
		t.Error("con el contexto cancelado no se salió de la sala: las reglas del firewall " +
			"se quedarían puestas hasta el arranque siguiente")
	}
	if d.Sala.(*salaFalsa).ctxCancelado {
		t.Error("la sala recibió un contexto ya cancelado, así que sus cierres son no-op")
	}
}

func TestApagarDosVecesNoRompe(t *testing.T) {
	orden := &registro{}
	r, err := Start(context.Background(), deps(orden))
	if err != nil {
		t.Fatal(err)
	}
	orden.espera(t, 2)

	if err := r.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := r.Shutdown(context.Background()); err != nil {
		t.Errorf("el segundo apagado devolvió error: %v", err)
	}
	// Y no cerró la sala dos veces.
	if n := cuenta(orden.lista(), "salir-sala"); n != 1 {
		t.Errorf("se salió de la sala %d veces", n)
	}
}

func TestUnaSalaQueNoCierraNoCuelgaElApagadoParaSiempre(t *testing.T) {
	// El Administrador de servicios de Windows mata el proceso a los treinta
	// segundos. Colgarse aquí significa que lo mata él, y ahí sí quedan reglas
	// huérfanas sin que nadie lo anote.
	orden := &registro{}
	d := deps(orden)
	d.ApagadoMax = 150 * time.Millisecond
	d.Sala = &salaFalsa{orden: orden, cuelga: true}

	r, err := Start(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	orden.espera(t, 2)

	hecho := make(chan error, 1)
	go func() { hecho <- r.Shutdown(context.Background()) }()

	select {
	case err := <-hecho:
		if err == nil {
			t.Error("una sala que no cierra devolvió apagado limpio")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("el apagado se colgó pasado su propio plazo")
	}
}

func TestArrancarNoEsperaAQueLaSalaSeReabra(t *testing.T) {
	// Reopening takes about a minute: two adapters, the credential exchange and
	// the MTU probe. If `Start` waited for it, systemd's READY would be a minute
	// late, and a `Type=notify` unit that takes a minute to report looks hung.
	// On Windows the service manager just kills it.
	//
	// Lo que se afirma es que `Start` VUELVE, con la reapertura colgada a
	// propósito. No el orden entre "entrada" y "reabrir-sala": aquello lo anota
	// una gorrutina, así que compararlos es una carrera que pasa por suerte, y
	// además no es lo que importa. Lo que decide el READY es cuándo vuelve esto.
	orden := &registro{}
	d := deps(orden)
	d.ApagadoMax = 2 * time.Second
	d.Sala = &salaFalsa{orden: orden, pendiente: true, reabrirCuelga: true}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	vuelto := make(chan *Runtime, 1)
	go func() {
		r, err := Start(ctx, d)
		if err != nil {
			t.Error(err)
			vuelto <- nil
			return
		}
		vuelto <- r
	}()

	select {
	case r := <-vuelto:
		if r == nil {
			t.Fatal("el arranque falló")
		}
		t.Cleanup(func() { cancel(); _ = r.Shutdown(context.Background()) })
	case <-time.After(2 * time.Second):
		t.Fatal("el arranque se quedó esperando a que la sala se reabriera, " +
			"así que el READY del servicio llega tarde")
	}

	// Y la reapertura sí arrancó: sin esto el test pasaría con la reapertura
	// borrada, que es la otra forma de que `Start` vuelva rápido.
	orden.espera(t, 3)
	if !contiene(orden.lista(), "reabrir-sala") {
		t.Errorf("no se reabrió la sala pendiente: %v", orden.lista())
	}
}

func TestElApagadoEsperaALaReaperturaAntesDeCerrarLaSala(t *testing.T) {
	// Reopening raises adapters and writes rules; closing removes them. Running
	// at the same time, the close can remove what the reopen has not put yet,
	// and whatever is left nobody removes: the process dies right after.
	orden := &registro{}
	d := deps(orden)
	d.ApagadoMax = 2 * time.Second
	d.Sala = &salaFalsa{orden: orden, pendiente: true, reabrirCuelga: true}

	ctx, cancel := context.WithCancel(context.Background())
	r, err := Start(ctx, d)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	orden.espera(t, 3)

	// Se cancela el contexto del servicio, que es lo que pasa en el apagado de
	// verdad, y eso es lo que corta la reapertura colgada.
	cancel()
	if err := r.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}

	lista := orden.lista()
	cortado, salir := -1, -1
	for i, paso := range lista {
		switch paso {
		case "reabrir-cortado":
			cortado = i
		case "salir-sala":
			salir = i
		}
	}
	if cortado == -1 {
		t.Fatalf("la reapertura no llegó a cortarse: %v", lista)
	}
	if salir == -1 || salir < cortado {
		t.Errorf("se cerró la sala con la reapertura todavía corriendo: %v", lista)
	}
}

func TestSinPiezasNoArranca(t *testing.T) {
	// Un daemon a medio cablear arrancaría feliz y fallaría media hora después,
	// dentro de una operación del usuario.
	if _, err := Start(context.Background(), Deps{}); err == nil {
		t.Fatal("arrancó sin piezas")
	}
}

// El banco.

func deps(o *registro) Deps {
	return Deps{
		Bucle:      &bucleFalso{orden: o},
		Entrada:    &entradaFalsa{orden: o},
		Sala:       &salaFalsa{orden: o},
		Log:        logMudo{},
		ApagadoMax: 2 * time.Second,
	}
}

type registro struct {
	mu    sync.Mutex
	pasos []string
}

func (r *registro) anota(p string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pasos = append(r.pasos, p)
}

func (r *registro) lista() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.pasos...)
}

func (r *registro) limpia() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pasos = nil
}

// espera a que haya al menos n pasos, sin dormir a ciegas.
func (r *registro) espera(t *testing.T, n int) {
	t.Helper()
	hasta := time.Now().Add(2 * time.Second)
	for time.Now().Before(hasta) {
		if len(r.lista()) >= n {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("no se llegó a %d pasos: %v", n, r.lista())
}

type bucleFalso struct{ orden *registro }

func (b *bucleFalso) Run(ctx context.Context, ready chan<- struct{}) error {
	b.orden.anota("bucle")
	close(ready)
	<-ctx.Done()
	return ctx.Err()
}

type entradaFalsa struct {
	orden *registro
	una   sync.Once
	fin   chan struct{}
}

func (e *entradaFalsa) Serve(ctx context.Context) error {
	e.fin = make(chan struct{})
	e.orden.anota("entrada")
	select {
	case <-e.fin:
	case <-ctx.Done():
	}
	return nil
}

func (e *entradaFalsa) Close() error {
	e.orden.anota("cerrar-entrada")
	e.una.Do(func() {
		if e.fin != nil {
			close(e.fin)
		}
	})
	return nil
}

type salaFalsa struct {
	orden        *registro
	cuelga       bool
	ctxCancelado bool

	// pendiente dice si hay sala del arranque anterior. En falso, que es lo que
	// vale por omisión, este falso no reabre nada y los tests del ORDEN de
	// arranque miden solo el orden, que es lo suyo.
	pendiente bool
	// reabrirCuelga bloquea la reapertura hasta que se cancele el contexto, para
	// poder medir que el apagado la espera antes de cerrar la sala.
	reabrirCuelga bool
	errReabrir    error
}

func (s *salaFalsa) LeaveRoomOnShutdown(ctx context.Context) error {
	s.orden.anota("salir-sala")
	s.ctxCancelado = ctx.Err() != nil
	if s.cuelga {
		<-ctx.Done()
		return errors.New("no cerró")
	}
	return nil
}

func (s *salaFalsa) PendingRoom() (domain.HostedRoom, bool) {
	return domain.HostedRoom{}, s.pendiente
}

func (s *salaFalsa) ResumeRoom(ctx context.Context) (domain.RoomState, error) {
	s.orden.anota("reabrir-sala")
	if s.reabrirCuelga {
		<-ctx.Done()
		s.orden.anota("reabrir-cortado")
		return domain.RoomState{}, ctx.Err()
	}
	return domain.RoomState{}, s.errReabrir
}

type logMudo struct{}

func (logMudo) Info(string, ...any)  {}
func (logMudo) Warn(string, ...any)  {}
func (logMudo) Error(string, ...any) {}

func contiene(xs []string, x string) bool { return cuenta(xs, x) > 0 }

func cuenta(xs []string, x string) int {
	n := 0
	for _, v := range xs {
		if v == x {
			n++
		}
	}
	return n
}
