package supervisor

import (
	"context"
	"testing"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// salaBloqueante simula un manejador que se queda esperando algo de la red,
// que es lo que haría una ronda de canario colgada de un evento de la sesión.
type salaBloqueante struct {
	*salaFalsa
	entró  chan struct{}
	suelta chan struct{}
}

func (r *salaBloqueante) OnEngineEvent(ctx context.Context, ev domain.EngineEvent) (domain.RoomState, error) {
	close(r.entró)
	<-r.suelta
	return r.salaFalsa.OnEngineEvent(ctx, ev)
}

// TestClaimUnManejadorQueEsperaCongelaElBucle.
//
// El despachador es UNO solo y todos los manejadores pasan por él. Un manejador
// que espera un informe de otra máquina no retrasa solo a ese evento: para el
// latido, que es lo que hace vencer el corte por ausencia del host.
//
// Y el corolario que importa para el diseño del canario: si el informe que se
// espera tuviera que entrar por este mismo despachador, no llegaría NUNCA.
func TestClaimUnManejadorQueEsperaCongelaElBucle(t *testing.T) {
	base := &salaFalsa{}
	sala := &salaBloqueante{
		salaFalsa: base,
		entró:     make(chan struct{}),
		suelta:    make(chan struct{}),
	}
	b := &banco{
		sala:     base,
		motor:    nuevoMotor(),
		control:  nuevoControl(),
		sistema:  nuevoSistema(),
		latidos:  make(chan time.Time, 4),
		barridos: make(chan time.Time, 4),
	}
	b.sup = nuevo(Deps{
		Room:    sala,
		Engine:  b.motor,
		Control: b.control,
		System:  b.sistema,
		Log:     logMudo{},
	}, b.latidos, b.barridos)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan struct{})
	go func() { _ = b.sup.Run(ctx, ready) }()
	<-ready

	// Un cambio de miembros entra al manejador y se queda esperando.
	b.motor.eventos <- domain.EngineEvent{Kind: domain.EnginePeersChanged}
	<-sala.entró

	// Ahora late el supervisor. El item entra al canal de trabajo y ahí se queda.
	b.latidos <- time.Now()
	time.Sleep(300 * time.Millisecond)

	if n := base.veces("tick"); n != 0 {
		t.Fatalf("REFUTADO: el latido corrió (%d) con un manejador esperando", n)
	}
	t.Log("con un manejador esperando, el latido NO corrió: el bucle entero está parado")

	close(sala.suelta)
	esperaA(t, "el latido corrió al soltar", func() bool { return base.veces("tick") >= 1 })
	t.Log("soltado el manejador, el latido corre")
}
