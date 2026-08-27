package supervisor

import (
	"context"
	"errors"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// motorDeMalla contesta lo que el test le ponga, y cuenta los sondeos.
type motorDeMalla struct {
	mu      sync.Mutex
	peers   []domain.Peer
	err     error
	sondeos int
}

func (m *motorDeMalla) Peers(context.Context) ([]domain.Peer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sondeos++
	return m.peers, m.err
}

func (m *motorDeMalla) veces() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sondeos
}

func (m *motorDeMalla) pon(peers []domain.Peer, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.peers, m.err = peers, err
}

func ip(t *testing.T, s string) netip.Addr {
	t.Helper()
	a, err := netip.ParseAddr(s)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// TestElVigiaAvisaDeCadaCambioYNoDeCadaTic.
//
// Las dos mitades importan. Avisar de cada cambio es lo que releía la lista de
// miembros cuando la tabla del motor ya convergió, que es lo que faltaba el
// 2026-08-25. No avisar de cada tic es lo que impide que esto sea un latido a
// un aviso por segundo contra el candado de la sesión.
//
// Vaciarse también avisa, y es deliberado: es el cambio más gordo que hay, y
// hasta ahora solo escribía un Warn.
func TestElVigiaAvisaDeCadaCambioYNoDeCadaTic(t *testing.T) {
	motor := &motorDeMalla{err: errors.New("there is no room running")}
	tics := make(chan time.Time)
	vigía := &VigiaDeMalla{Motor: motor, Log: logMudo{}}
	vigía.tics = tics
	cambios := vigía.Cambios()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go vigía.Correr(ctx)

	// tic empuja una vuelta del bucle y espera a que la haya dado.
	tic := func() {
		antes := motor.veces()
		tics <- time.Time{}
		esperaA(t, "el vigía dio la vuelta", func() bool { return motor.veces() > antes })
	}

	// Sin sala no hay malla, y el arranque no cuenta como cambio.
	tic()
	tic()
	select {
	case <-cambios:
		t.Fatal("avisó de un cambio con el motor diciendo que no hay sala")
	default:
	}

	// Entra alguien.
	motor.pon([]domain.Peer{
		{VirtualIP: ip(t, "100.93.137.1"), Self: true},
		{VirtualIP: ip(t, "100.93.137.3"), Path: domain.PathRelay},
	}, nil)
	tic()
	if !avisó(cambios) {
		t.Fatal("no avisó de que entró alguien")
	}

	// La misma malla, tic tras tic, no vuelve a avisar.
	tic()
	tic()
	if avisó(cambios) {
		t.Fatal("avisó sin que la malla cambiara: esto sería un latido")
	}

	// Cambia el camino: de relay a directo es un cambio que interesa.
	motor.pon([]domain.Peer{
		{VirtualIP: ip(t, "100.93.137.1"), Self: true},
		{VirtualIP: ip(t, "100.93.137.3"), Path: domain.PathDirect},
	}, nil)
	tic()
	if !avisó(cambios) {
		t.Fatal("no avisó del cambio de camino")
	}

	// Y vaciarse avisa.
	motor.pon(nil, nil)
	tic()
	if !avisó(cambios) {
		t.Fatal("no avisó de que la malla se quedó vacía")
	}
}

// avisó consume un aviso si lo hay. No espera: cada `tic` del test ya esperó a
// que la vuelta del bucle terminara, así que el aviso o está o no se mandó.
func avisó(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}
