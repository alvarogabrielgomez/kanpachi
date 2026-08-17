package usecase

import (
	"errors"
	"strings"
	"testing"

	"github.com/accentiostudios/kanpachi/core/domain"
)

func TestShuttingDownKeepsTheRoomToReopenIt(t *testing.T) {
	// Shutting down is not the room ending. An upgrade, a reboot or a
	// `systemctl restart` stop the process while the room is still its owner's,
	// so the file stays and the next start reopens it with the SAME code, which
	// is what keeps the links already handed out working.
	b := nuevoBanco(t)
	if _, err := b.session.CreateRoom(ctx(), nick(t, "alvaro"), "Los panas", false, false); err != nil {
		t.Fatal(err)
	}
	if len(b.state.salaGuardada()) == 0 {
		t.Fatal("este test no prueba nada: la sala no llegó a guardarse")
	}

	if err := b.session.LeaveRoomOnShutdown(ctx()); err != nil {
		t.Fatalf("un apagado limpio informó un problema: %v", err)
	}

	if st := b.session.Status(); st.Conn != domain.StateIdle {
		t.Errorf("al apagar el estado quedó en %v", st.Conn)
	}
	if len(b.state.salaGuardada()) == 0 {
		t.Error("el apagado se llevó la sala guardada, así que el arranque siguiente " +
			"no la puede reabrir y el código repartido deja de valer")
	}
}

func TestClosingTheRoomDoesClearIt(t *testing.T) {
	// La otra mitad de la decisión, y sin ella la de arriba sería que el fichero
	// no se borra nunca: cerrar la sala SÍ se lo lleva, porque ahí la sala se
	// acabó de verdad. Es lo único que lo borra.
	b := nuevoBanco(t)
	if _, err := b.session.CreateRoom(ctx(), nick(t, "alvaro"), "Los panas", false, false); err != nil {
		t.Fatal(err)
	}
	if len(b.state.salaGuardada()) == 0 {
		t.Fatal("este test no prueba nada: la sala no llegó a guardarse")
	}

	b.session.LeaveRoom(ctx())

	if len(b.state.salaGuardada()) != 0 {
		t.Error("cerrar la sala dejó su fichero, así que el arranque siguiente " +
			"reabriría una sala que el usuario cerró a propósito")
	}
}

func TestShuttingDownDoesNotTellTheMembersTheRoomClosed(t *testing.T) {
	// The half that breaks in silence if anybody merges the two paths again.
	// `LeaveRoom` sends NoticeRoomClosed and is right to: the room is over.
	// Sending it from the shutdown would be a lie AND expensive, because every
	// guest would leave and stop reconnecting, which is exactly what keeping the
	// room exists to allow. What they see is the host absent, and the
	// twenty-minute counter already covers that.
	b := nuevoBanco(t)
	if _, err := b.session.CreateRoom(ctx(), nick(t, "alvaro"), "Los panas", false, false); err != nil {
		t.Fatal(err)
	}
	b.control.avisos = nil

	if err := b.session.LeaveRoomOnShutdown(ctx()); err != nil {
		t.Fatal(err)
	}

	for _, a := range b.control.avisos {
		if a.n.Kind == domain.NoticeRoomClosed {
			t.Error("el apagado avisó de que la sala se cierra: cada invitado sale y " +
				"deja de reconectar, que es lo contrario de conservarla")
		}
	}
}

func TestShuttingDownReportsRulesThatSurvived(t *testing.T) {
	// Acá no hay nadie mirando la pantalla. LeaveRoom anota lo que sale mal como
	// una alerta del estado, y el proceso se muere justo después: una alerta
	// añadida a un estado que nadie va a leer no es un informe.
	b := nuevoBanco(t)
	if _, err := b.session.CreateRoom(ctx(), nick(t, "alvaro"), "Los panas", false, false); err != nil {
		t.Fatal(err)
	}
	b.audit.tamper()

	err := b.session.LeaveRoomOnShutdown(ctx())
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
	if _, err := b.session.CreateRoom(ctx(), nick(t, "alvaro"), "Los panas", false, false); err != nil {
		t.Fatal(err)
	}
	b.audit.errIntactas = errors.New("no se pudo enumerar")

	err := b.session.LeaveRoomOnShutdown(ctx())
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

	if err := b.session.LeaveRoomOnShutdown(ctx()); err != nil {
		t.Fatalf("apagar sin sala abierta informó un problema: %v", err)
	}
	if _, err := b.session.CreateRoom(ctx(), nick(t, "alvaro"), "Los panas", false, false); err != nil {
		t.Fatal(err)
	}
	if err := b.session.LeaveRoomOnShutdown(ctx()); err != nil {
		t.Fatal(err)
	}
	if err := b.session.LeaveRoomOnShutdown(ctx()); err != nil {
		t.Fatalf("el segundo apagado informó un problema: %v", err)
	}
}

func TestShuttingDownClosesThePortsBeforeMeasuring(t *testing.T) {
	// El orden importa: medir antes de cerrar encontraría las reglas de la sala
	// todavía puestas y llamaría a eso un apagado sucio, en cada apagado.
	b := nuevoBanco(t)
	if _, err := b.session.CreateRoom(ctx(), nick(t, "alvaro"), "Los panas", false, false); err != nil {
		t.Fatal(err)
	}
	if _, err := b.session.ActivateProfile(ctx(), "project-zomboid"); err != nil {
		t.Fatal(err)
	}
	if len(b.firewall.estado().Rules) == 0 {
		t.Fatal("este test no prueba nada: no llegó a abrirse ningún puerto")
	}

	if err := b.session.LeaveRoomOnShutdown(ctx()); err != nil {
		t.Fatal(err)
	}
	if n := len(b.firewall.estado().Rules); n != 0 {
		t.Errorf("quedaron %d reglas puestas después de apagar", n)
	}
}
