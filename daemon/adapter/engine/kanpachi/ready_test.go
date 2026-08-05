package kanpachi

import (
	"context"
	"errors"
	"net/netip"
	"sync/atomic"
	"testing"
	"time"
)

func addr(s string) netip.Addr { return netip.MustParseAddr(s) }

// TestWaitForAddressReturnsOnceTheAddressAppears is the whole point: the
// adapter does not exist when a start command is acknowledged, and "no such
// network interface" is the NORMAL answer for the first attempts.
func TestWaitForAddressReturnsOnceTheAddressAppears(t *testing.T) {
	var intentos atomic.Int32
	lista := func(string) ([]netip.Addr, error) {
		switch intentos.Add(1) {
		case 1:
			return nil, errors.New("no such network interface")
		case 2:
			// El adaptador ya existe y todavía no tomó dirección. Es un estado
			// real y no un error, y esperarlo es justo lo que hacía falta.
			return nil, nil
		default:
			return []netip.Addr{addr("fe80::1"), addr("100.127.255.1")}, nil
		}
	}

	if err := waitForAddress(context.Background(), lista,
		LobbyDevice, addr("100.127.255.1"), 5*time.Second); err != nil {
		t.Fatalf("tenía que encontrarla al tercer intento: %v", err)
	}
	if n := intentos.Load(); n != 3 {
		t.Errorf("se esperaban 3 intentos y hubo %d", n)
	}
}

// TestWaitForAddressFailsWhenTheAdapterNeverTakesIt covers the direction that
// matters for safety: a room reported as open on an adapter that never appeared
// is worse than one that failed to open.
func TestWaitForAddressFailsWhenTheAdapterNeverTakesIt(t *testing.T) {
	// El adaptador existe con OTRA dirección, que es el caso traicionero: el
	// nombre está, la interfaz responde, y ligar ahí falla igual.
	lista := func(string) ([]netip.Addr, error) {
		return []netip.Addr{addr("100.99.0.5")}, nil
	}

	err := waitForAddress(context.Background(), lista,
		RoomDevice, addr("100.87.4.1"), 600*time.Millisecond)
	if err == nil {
		t.Fatal("sin la dirección pedida tenía que fallar")
	}
	// El mensaje lleva lo que SÍ había. Sin eso, diagnosticar esto obliga a
	// reproducirlo con el adaptador delante.
	if got := err.Error(); !contiene(got, "100.99.0.5") || !contiene(got, RoomDevice) {
		t.Errorf("el error tiene que decir qué adaptador y qué había: %q", got)
	}
}

func TestWaitForAddressHonoursTheContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := waitForAddress(ctx, func(string) ([]netip.Addr, error) { return nil, nil },
		RoomDevice, addr("100.87.4.1"), time.Minute)
	if err == nil {
		t.Fatal("con el contexto cancelado tenía que volver enseguida")
	}
}

// TestWaitForAddressRejectsAnEmptyTarget guards against waiting for nothing.
// A zero address would never match, so this would burn the whole deadline and
// then blame the adapter for a mistake made by the caller.
func TestWaitForAddressRejectsAnEmptyTarget(t *testing.T) {
	llamadas := 0
	err := waitForAddress(context.Background(),
		func(string) ([]netip.Addr, error) { llamadas++; return nil, nil },
		RoomDevice, netip.Addr{}, time.Minute)
	if err == nil {
		t.Fatal("esperar a una dirección vacía tenía que fallar")
	}
	if llamadas != 0 {
		t.Errorf("no tenía que mirar el sistema ni una vez, y miró %d", llamadas)
	}
}

func contiene(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
