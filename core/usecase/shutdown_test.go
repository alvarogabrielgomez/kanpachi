package usecase

import (
	"errors"
	"strings"
	"testing"

	"github.com/accentiostudios/kanpachi/core/domain"
)

func TestShuttingDownLeavesTheRoomAndSaysSo(t *testing.T) {
	b := nuevoBanco(t)
	if _, err := b.sesión.CreateRoom(ctx(), nick(t, "alvaro"), "Los panas"); err != nil {
		t.Fatal(err)
	}
	if len(b.estado.salaGuardada()) == 0 {
		t.Fatal("este test no prueba nada: la sala no llegó a guardarse")
	}

	if err := b.sesión.LeaveRoomOnShutdown(ctx()); err != nil {
		t.Fatalf("un apagado limpio informó un problema: %v", err)
	}

	if st := b.sesión.Status(); st.Conn != domain.StateIdle {
		t.Errorf("al apagar el estado quedó en %v", st.Conn)
	}
	// La ausencia del archivo es lo que dice que la salida fue limpia, y no hay
	// bandera dentro que lo diga. Conservarlo haría que todo apagado se leyera
	// como una muerte sucia, y el aviso de "quedó una sala abierta" dejaría de
	// significar nada por salir siempre.
	if len(b.estado.salaGuardada()) != 0 {
		t.Error("el apagado limpio dejó la sala guardada, así que el arranque siguiente " +
			"la va a leer como una muerte sucia")
	}
}

func TestShuttingDownReportsRulesThatSurvived(t *testing.T) {
	// Acá no hay nadie mirando la pantalla. LeaveRoom anota lo que sale mal como
	// una alerta del estado, y el proceso se muere justo después: una alerta
	// añadida a un estado que nadie va a leer no es un informe.
	b := nuevoBanco(t)
	if _, err := b.sesión.CreateRoom(ctx(), nick(t, "alvaro"), "Los panas"); err != nil {
		t.Fatal(err)
	}
	b.auditoría.tamper()

	err := b.sesión.LeaveRoomOnShutdown(ctx())
	if err == nil {
		t.Fatal("el apagado dijo que todo bien con reglas puestas de más")
	}
	if !strings.Contains(err.Error(), "kanpachi-rule-nobody-asked-for") {
		t.Errorf("el error no nombra la regla que sobrevivió: %v", err)
	}
}

func TestShuttingDownReportsBlindness(t *testing.T) {
	// Ciego y limpio no son lo mismo, y confundirlos acá es el peor sitio
	// posible: es la última medición que se hace en la vida del proceso.
	b := nuevoBanco(t)
	if _, err := b.sesión.CreateRoom(ctx(), nick(t, "alvaro"), "Los panas"); err != nil {
		t.Fatal(err)
	}
	b.auditoría.errIntactas = errors.New("no se pudo enumerar")

	err := b.sesión.LeaveRoomOnShutdown(ctx())
	if err == nil {
		t.Fatal("una medición caída se informó como un apagado limpio")
	}
	if !strings.Contains(err.Error(), "no se pudo comprobar") {
		t.Errorf("el error no dice que fue ceguera: %v", err)
	}
}

func TestShuttingDownIsIdempotent(t *testing.T) {
	// Lo llama el usuario, lo llama el contador de veinte minutos y lo llama el
	// apagado. Que el segundo en llegar falle no le aporta nada a nadie.
	b := nuevoBanco(t)

	if err := b.sesión.LeaveRoomOnShutdown(ctx()); err != nil {
		t.Fatalf("apagar sin sala abierta informó un problema: %v", err)
	}
	if _, err := b.sesión.CreateRoom(ctx(), nick(t, "alvaro"), "Los panas"); err != nil {
		t.Fatal(err)
	}
	if err := b.sesión.LeaveRoomOnShutdown(ctx()); err != nil {
		t.Fatal(err)
	}
	if err := b.sesión.LeaveRoomOnShutdown(ctx()); err != nil {
		t.Fatalf("el segundo apagado informó un problema: %v", err)
	}
}

func TestShuttingDownClosesThePortsBeforeMeasuring(t *testing.T) {
	// El orden importa: medir antes de cerrar encontraría las reglas de la sala
	// todavía puestas y llamaría a eso un apagado sucio, en cada apagado.
	b := nuevoBanco(t)
	if _, err := b.sesión.CreateRoom(ctx(), nick(t, "alvaro"), "Los panas"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.sesión.ActivateProfile(ctx(), "project-zomboid"); err != nil {
		t.Fatal(err)
	}
	if len(b.firewall.estado().Rules) == 0 {
		t.Fatal("este test no prueba nada: no llegó a abrirse ningún puerto")
	}

	if err := b.sesión.LeaveRoomOnShutdown(ctx()); err != nil {
		t.Fatal(err)
	}
	if n := len(b.firewall.estado().Rules); n != 0 {
		t.Errorf("quedaron %d reglas puestas después de apagar", n)
	}
}
