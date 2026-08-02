package domain

import (
	"net/netip"
	"testing"
	"time"
)

func TestTransicionesLegales(t *testing.T) {
	casos := []struct {
		de, a ConnState
		legal bool
	}{
		{StateIdle, StateResolving, true},
		{StateIdle, StateConnected, false}, // no se salta el canje de credencial
		{StateResolving, StateConnecting, true},
		{StateConnecting, StateConnected, true},
		{StateConnected, StateDegraded, true},
		{StateDegraded, StateConnected, true},
		{StateDegraded, StateReconnecting, true},
		{StateReconnecting, StateConnected, true},
		{StateReconnecting, StateResolving, false}, // reconectar no vuelve a resolver
		{StateConnected, StateConnected, true},     // los eventos del motor llegan repetidos
	}
	for _, c := range casos {
		if got := c.de.CanGoTo(c.a); got != c.legal {
			t.Errorf("%s → %s = %v, se esperaba %v", c.de, c.a, got, c.legal)
		}
	}
}

// TestSiempreSePuedeSalir: salir es una acción del usuario y tiene que
// funcionar desde cualquier estado, incluso a mitad de un intento de conexión
// que no responde.
func TestSiempreSePuedeSalir(t *testing.T) {
	for _, s := range []ConnState{
		StateIdle, StateResolving, StateConnecting, StateConnected, StateDegraded, StateReconnecting,
	} {
		if !s.CanGoTo(StateIdle) {
			t.Errorf("desde %s no se puede salir de la sala", s)
		}
	}
}

func TestVolverAIdleLimpiaLaSala(t *testing.T) {
	r := RoomState{
		Conn:        StateConnected,
		Role:        RoleHost,
		Room:        Room{Seed: DefaultSeedHost},
		Name:        "La sala",
		Peers:       []Peer{{Name: Nickname{value: "alvaro"}}},
		HostPresent: true,
		Game:        perfilEstrella(),
		LocalIP:     ipLocal,
		Subnet:      netip.MustParsePrefix("100.87.3.0/24"),
	}
	if err := r.Transition(StateIdle, "el usuario salió"); err != nil {
		t.Fatal(err)
	}
	if r.Role != 0 || r.Name != "" || r.Peers != nil || !r.Game.IsZero() || r.LocalIP.IsValid() {
		t.Fatalf("quedaron restos de la sala anterior: %+v", r)
	}
	// La subred describe la máquina y no la sala: se conserva a propósito para
	// que el diagnóstico siga diciendo qué rango se eligió y por qué.
	if !r.Subnet.IsValid() {
		t.Error("la subred se borró, y no es de la sala sino de la máquina")
	}
}

func TestTransiciónImposibleDiceCuál(t *testing.T) {
	r := RoomState{Conn: StateIdle}
	err := r.Transition(StateConnected, "un evento inesperado")
	if err == nil {
		t.Fatal("se aceptó saltar de Idle a Connected")
	}
	var bad ErrBadTransition
	if !asBadTransition(err, &bad) || bad.From != StateIdle || bad.To != StateConnected {
		t.Fatalf("el error no dice de dónde a dónde: %v", err)
	}
	if r.Conn != StateIdle {
		t.Error("la transición ilegal movió el estado igual")
	}
}

func asBadTransition(err error, out *ErrBadTransition) bool {
	e, ok := err.(ErrBadTransition)
	if ok {
		*out = e
	}
	return ok
}

// TestElContadorDeAusenciaCuentaDesdeLaPrimeraCaída.
//
// Es el bug que se comería la decisión 20 entera: si cada latido fallido
// reiniciara la marca, el contador quedaría en cero para siempre y la salida
// automática no ocurriría nunca.
func TestElContadorDeAusenciaCuentaDesdeLaPrimeraCaída(t *testing.T) {
	t0 := time.Date(2026, 8, 2, 20, 0, 0, 0, time.UTC)
	r := RoomState{Conn: StateConnected, Role: RoleGuest, HostPresent: true}

	r.SetHostPresent(false, t0)
	r.SetHostPresent(false, t0.Add(19*time.Minute))
	r.SetHostPresent(false, t0.Add(19*time.Minute+59*time.Second))

	if r.ShouldLeaveForHostAbsence(t0.Add(19 * time.Minute)) {
		t.Error("salió antes de tiempo")
	}
	if !r.ShouldLeaveForHostAbsence(t0.Add(HostAbsenceLimit)) {
		t.Fatal("no salió a los veinte minutos: la marca se reinició con cada latido")
	}
}

func TestElHostVuelveYElContadorSePara(t *testing.T) {
	t0 := time.Date(2026, 8, 2, 20, 0, 0, 0, time.UTC)
	r := RoomState{Conn: StateConnected, Role: RoleGuest, HostPresent: true}

	r.SetHostPresent(false, t0)
	r.SetHostPresent(true, t0.Add(5*time.Minute))

	if r.ShouldLeaveForHostAbsence(t0.Add(time.Hour)) {
		t.Fatal("salió de una sala cuyo host volvió hace rato")
	}
	if !r.HostGoneSince.IsZero() {
		t.Error("la marca de ausencia no se limpió al volver el host")
	}
}

// TestElHostNoSeEchaDeSuPropiaSala: si el campo se rellenara por error en un
// host, el resultado sería que el host se echa de la sala que hospeda.
func TestElHostNoSeEchaDeSuPropiaSala(t *testing.T) {
	t0 := time.Date(2026, 8, 2, 20, 0, 0, 0, time.UTC)
	r := RoomState{Conn: StateConnected, Role: RoleHost, HostGoneSince: t0}
	if r.ShouldLeaveForHostAbsence(t0.Add(2 * time.Hour)) {
		t.Fatal("el host se echó de su propia sala")
	}
}

func TestEstarEnLaSalaNoEsEstarConectado(t *testing.T) {
	if !StateReconnecting.InRoom() || !StateDegraded.InRoom() {
		t.Error("reconectar y degradado siguen siendo estar en la sala")
	}
	if StateIdle.InRoom() {
		t.Error("Idle no es estar en una sala")
	}
}
