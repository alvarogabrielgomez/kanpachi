package control

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/accentiostudios/kanpachi/core/timing"
)

// TestCadaReintentoDeMarcadoLlevaSuPropioPlazo.
//
// El marcado inicial lo tiene y este no lo tenía, y la asimetría es cara contra
// un `drop`. Sin plazo propio, cada intento se come el presupuesto de SYN
// entero del núcleo, que en Linux son alrededor de 130 segundos, así que la
// escalera anunciada de 1/2/5/10/20/30 degeneraba en un intento cada dos
// minutos. Quien esperaba volver a la sala esperaba veinte veces más de lo que
// dice el diseño, y ninguna línea de log lo delataba.
func TestCadaReintentoDeMarcadoLlevaSuPropioPlazo(t *testing.T) {
	var mu sync.Mutex
	var plazos []time.Duration
	llamado := make(chan struct{}, 4)

	ch := New(Deps{
		Clock: &relojFalso{},
		Log:   logMudo{},
		Dial: func(ctx context.Context, _ netip.AddrPort) (net.Conn, error) {
			plazo, hay := ctx.Deadline()
			mu.Lock()
			if hay {
				plazos = append(plazos, time.Until(plazo))
			} else {
				plazos = append(plazos, 0)
			}
			mu.Unlock()
			select {
			case llamado <- struct{}{}:
			default:
			}
			return nil, errors.New("no contesta")
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &client{ch: ch, at: netip.MustParseAddrPort("100.93.137.1:57623"), ctx: ctx}
	go c.redial()

	select {
	case <-llamado:
	case <-time.After(5 * time.Second):
		t.Fatal("el remarcado no llegó a intentar")
	}
	cancel()

	mu.Lock()
	defer mu.Unlock()
	if len(plazos) == 0 {
		t.Fatal("no se registró ningún intento")
	}
	if plazos[0] == 0 {
		t.Fatal("el intento salió sin plazo: contra un drop se come el presupuesto de SYN entero")
	}
	if plazos[0] > timing.InitialRoomDialWait {
		t.Fatalf("el plazo del intento fue %v, más que el del marcado inicial (%v)",
			plazos[0], timing.InitialRoomDialWait)
	}
}
