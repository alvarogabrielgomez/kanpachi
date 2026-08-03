package usecase

import (
	"context"
	"strings"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// verifyClosedLocked measures the firewall right after a room ends and reports
// whatever survived the purge.
//
// # Why this exists
//
// Leaving a room applies the empty rule set, and the give-up path also calls
// PurgeOwned on top of that. Neither of them checked the result. A purge that
// failed quietly left the game's ports open with the UI showing no room, which
// is the worst shape a failure can take here: the promise is broken and the
// screen says everything is fine.
//
// The periodic sweep would not save it either. It demands a room to judge the
// gate, it runs on a timer, and if the daemon shuts down right after leaving --
// which is exactly what people do when they stop playing -- nobody measures
// again, ever.
//
// # Why the alert is sticky
//
// [domain.AlertRulesTampered] is not sticky, on the grounds that the next sweep
// recomputes it. That reasoning holds while there is a next sweep. Here there
// may not be one, so the finding is recorded as a kick-incomplete style alert:
// something happened, and nobody is going to re-measure it.
//
// # What it does NOT do
//
// It does not repair. Reapplying the empty set is what already failed, and
// doing it again in a loop would hammer the firewall over a call that is
// broken. It reports, and repair is the next sweep's job if the daemon lives
// long enough to run one.
//
// Assumes the lock is held.
func (s *Session) verifyClosedLocked(ctx context.Context) {
	e, err := s.deps.Audit.Enforcement(ctx)
	if err != nil {
		// Blindness right where it matters most. Say so rather than assume the
		// purge worked, which is the same doctrine as AlertAuditFailed.
		s.deps.Log.Warn("could not verify the firewall was left closed", "error", err)
		s.state.Alerts = append(s.state.Alerts, domain.Alert{
			Kind:   domain.AlertAuditFailed,
			Detail: "no se pudo comprobar que el firewall quedó cerrado al salir de la sala",
		})
		return
	}

	// The desired set with no room is the empty one, and the gate is not
	// demanded because there is no adapter to scope it to.
	d := e.Diff(domain.RuleSet{}, false)
	if len(d.Extra) == 0 {
		return
	}

	s.deps.Log.Error("rules survived leaving the room", "rules", d.Extra)
	s.state.Alerts = append(s.state.Alerts, domain.Alert{
		Kind: domain.AlertKickIncomplete,
		Detail: "al salir de la sala quedaron reglas puestas que Kanpachi no pudo quitar: " +
			strings.Join(d.Extra, ", "),
	})
}
