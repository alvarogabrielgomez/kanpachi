package usecase

import (
	"errors"
	"testing"
)

func TestExposureShowsTheGamePortsThatAreReallyOpen(t *testing.T) {
	b := nuevoBanco(t)
	if _, err := b.sesión.CreateRoom(ctx(), nick(t, "alvaro"), "Los panas"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.sesión.ActivateProfile(ctx(), "project-zomboid"); err != nil {
		t.Fatal(err)
	}

	r := b.sesión.Exposure(ctx())
	if r.Blind() {
		t.Fatal("la medición se declaró ciega con la auditoría funcionando")
	}
	if len(r.Ports) == 0 {
		t.Fatal("con un juego activo no se informó ningún puerto")
	}
	for _, p := range r.Ports {
		if !p.Applied {
			t.Errorf("el puerto %s se informó como no aplicado, y el firewall lo tiene", p.Name)
		}
		if len(p.Members) == 0 && len(p.Nets) == 0 {
			t.Errorf("el puerto %s se informó sin decir para quién está abierto", p.Name)
		}
	}
}

func TestExposureGoesBlindInsteadOfLooking(t *testing.T) {
	// Lo que no se pudo medir se dice. Rellenar con la última lista buena sería
	// enseñar una pantalla verde sobre una medición que no ocurrió.
	b := nuevoBanco(t)
	if _, err := b.sesión.CreateRoom(ctx(), nick(t, "alvaro"), "Los panas"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.sesión.ActivateProfile(ctx(), "project-zomboid"); err != nil {
		t.Fatal(err)
	}
	if r := b.sesión.Exposure(ctx()); len(r.Ports) == 0 {
		t.Fatal("este test no prueba nada: no había nada que ocultar")
	}

	b.auditoría.errIntactas = errors.New("no se pudo enumerar")

	r := b.sesión.Exposure(ctx())
	if !r.Blind() {
		t.Fatal("una medición caída se informó como buena")
	}
	if len(r.Ports) != 0 {
		t.Errorf("el informe ciego enseñó %d puertos de la lectura anterior", len(r.Ports))
	}
}

func TestExposureWithNoRoomDoesNotDemandTheGate(t *testing.T) {
	// Sin sala no hay adaptador virtual, así que exigir la compuerta dejaría una
	// alerta encendida en reposo.
	b := nuevoBanco(t)

	r := b.sesión.Exposure(ctx())
	if r.Blind() {
		t.Fatal("medir en reposo se declaró ciego")
	}
	if len(r.Ports) != 0 {
		t.Errorf("sin sala se informaron %d puertos abiertos", len(r.Ports))
	}
}

func TestExposureReportsRulesNobodyAskedFor(t *testing.T) {
	b := nuevoBanco(t)
	if _, err := b.sesión.CreateRoom(ctx(), nick(t, "alvaro"), "Los panas"); err != nil {
		t.Fatal(err)
	}
	b.auditoría.tamper()

	r := b.sesión.Exposure(ctx())
	if len(r.Unexpected) == 0 {
		t.Fatal("una regla del grupo propio que nadie pidió no se informó")
	}
}
