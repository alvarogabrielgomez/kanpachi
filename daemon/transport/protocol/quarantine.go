package protocol

// The door's side of the base-quarantine question. The switch itself lives in
// the consent use case; what this file owns is WHEN a room refuses to open
// over it, and the sentence the refusal carries.

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// quarantineGate runs before a room opens or gets entered.
//
// The refusal is OPT-IN by the caller, and that inversion is what keeps the
// two faces honest without the daemon guessing who is talking: the CLI sends
// "ask" always, so in the terminal the choice is mandatory; the window sends
// nothing and never blocks, because a modal before playing is what this
// product promised not to do. A script driving the pipe raw and sending
// nothing gets the window's deal: nothing changes on its machine, and the
// question stays owed.
//
// "on" and "off" ARE the decision, recorded and made true before the room
// opens. A failed operation does not stop the room: the word got recorded,
// the sweep keeps measuring what is actually in place, and the faces read
// that measurement, so proceeding is honest and dying here would cost a room
// over a protection the person can retry.
func (s *Server) quarantineGate(ctx context.Context, choice string) *Error {
	switch choice {
	case "":
		return nil
	case "ask":
		decision, ports := s.api.QuarantineQuestion()
		if decision != domain.QuarantineUndecided {
			return nil
		}
		return &Error{Code: CodeQuarantineUndecided, Message: quarantineQuestion(ports)}
	case "on", "off":
		if _, err := s.api.DecideQuarantine(ctx, choice == "on"); err != nil {
			s.log.Warn("la decisión de la cuarentena quedó anotada y la operación falló",
				"pedido", choice, "error", err)
		}
		return nil
	default:
		return badRequest(`quarantine tiene que ser "ask", "on" u "off"`)
	}
}

// quarantineQuestion is the sentence the CLI prints before asking. The ports
// come from the domain through the session, so what is offered and what gets
// closed cannot disagree.
func quarantineQuestion(ports []uint16) string {
	list := make([]string, len(ports))
	for i, p := range ports {
		list[i] = strconv.Itoa(int(p))
	}
	return fmt.Sprintf("this machine has not answered Kanpachi's one security question: "+
		"whether to close its risky server ports (%s) on every network it joins, until you say "+
		"otherwise. Those ports are how other machines reach THIS PC's shared folders, Remote "+
		"Desktop, remote administration and printer discovery. Closing them is recommended, and "+
		"it never affects your rooms, your games, or reaching other machines' shares. Say no if "+
		"you share folders from this PC or enter it over Remote Desktop.",
		strings.Join(list, ", "))
}
