package usecase

import (
	"errors"
	"strings"
	"testing"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// Leaving a room applies the empty rule set, and nothing checked the result.
// A purge that failed quietly left the game's ports open with the UI showing
// no room, which is the worst shape a failure can take: the promise is broken
// and the screen says everything is fine.

func TestLeavingVerifiesNothingSurvivedThePurge(t *testing.T) {
	b := nuevoBanco(t)
	if _, err := b.session.CreateRoom(ctx(), nick(t, "alvaro"), "Los panas", false, false); err != nil {
		t.Fatal(err)
	}

	// The firewall claims success and leaves a rule behind anyway. That is the
	// exact failure this guards: a silent purge failure.
	b.audit.overrideRules = true
	b.audit.puestas = []domain.AppliedRule{{
		Name:    "kanpachi-udp-16261",
		Layer:   domain.LayerFirewallRules,
		Enabled: true,
	}}

	st := b.session.LeaveRoom(ctx())

	if !tieneAlerta(st, domain.AlertKickIncomplete) {
		t.Fatalf("no avisó de que quedaron reglas puestas al salir: %+v", st.Alerts)
	}
	var detalle string
	for _, a := range st.Alerts {
		if a.Kind == domain.AlertKickIncomplete {
			detalle = a.Detail
		}
	}
	if !strings.Contains(detalle, "kanpachi-udp-16261") {
		t.Errorf("la alerta no nombra la regla que quedó: %q\n"+
			"  Sin el nombre, el usuario no puede ir a mirarla ni quitarla a mano", detalle)
	}
	if !domain.AlertKickIncomplete.Sticky() {
		t.Error("la alerta tiene que ser pegajosa: si el daemon se apaga tras salir, " +
			"nadie vuelve a medir nunca")
	}
}

func TestLeavingCleanlySaysNothing(t *testing.T) {
	b := nuevoBanco(t)
	if _, err := b.session.CreateRoom(ctx(), nick(t, "alvaro"), "Los panas", false, false); err != nil {
		t.Fatal(err)
	}

	st := b.session.LeaveRoom(ctx())

	if tieneAlerta(st, domain.AlertKickIncomplete) {
		t.Errorf("avisó sin que quedara nada: %+v", st.Alerts)
	}
	if tieneAlerta(st, domain.AlertAuditFailed) {
		t.Errorf("dijo que no pudo comprobar cuando sí pudo: %+v", st.Alerts)
	}
}

func TestNotBeingAbleToVerifyIsReported(t *testing.T) {
	b := nuevoBanco(t)
	if _, err := b.session.CreateRoom(ctx(), nick(t, "alvaro"), "Los panas", false, false); err != nil {
		t.Fatal(err)
	}

	// Blindness right where it matters most. Assuming the purge worked is the
	// one thing that must not happen here.
	b.audit.errIntactas = errors.New("el firewall no contestó")

	st := b.session.LeaveRoom(ctx())

	if !tieneAlerta(st, domain.AlertAuditFailed) {
		t.Fatalf("no avisó de que no pudo comprobar: %+v", st.Alerts)
	}
}
