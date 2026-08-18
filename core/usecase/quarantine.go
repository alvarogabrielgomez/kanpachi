package usecase

// The consent use case: the user's quarantine decision, made true.
//
// This file is load-bearing BY NAME. The guardian in internal/arch allows the
// call to RemoveBaseQuarantineAtUserRequest from core/usecase/quarantine.go
// and from nowhere else in core, so the operation below cannot migrate into a
// sweep, a start or a reset without going red. Keep the decision here.

import (
	"context"
	"fmt"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// QuarantineDecision says what this machine's user has answered, touching
// nothing. Undecided is what makes the faces ask exactly once.
func (s *Session) QuarantineDecision() domain.QuarantineDecision {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.quarantineDecision
}

// QuarantineQuestion is what the room's door needs in ONE read: whether the
// question is still owed, and what closing would close. The ports come from
// the domain so what gets offered and what gets closed cannot disagree.
func (s *Session) QuarantineQuestion() (domain.QuarantineDecision, []uint16) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.quarantineDecision, domain.QuarantinePorts(s.deps.Quarantine)
}

// DecideQuarantine records the user's answer and makes it true, in whichever
// direction: accepting writes the base quarantine, declining removes it.
//
// # The decision IS the operation
//
// There is no preference stored on one side and rules standing on the other.
// Saying yes twice repairs, saying no twice stays removed — both halves are
// idempotent, so the switch every face draws can be pressed freely.
//
// # The word is persisted FIRST, the rules second
//
// Losing the user's answer over a failed rule write would ask them again for
// something they already said. The other order fails safe too, and the tie
// goes to the person: a recorded "no" whose removal failed leaves rules up
// that the next press takes down, and the state below says so meanwhile.
//
// The state returned is MEASURED after the operation, never assumed from it:
// what the screen says next has to be what the machine has.
func (s *Session) DecideQuarantine(ctx context.Context, wanted bool) (domain.QuarantineState, error) {
	decision := domain.QuarantineDeclined
	if wanted {
		decision = domain.QuarantineAccepted
	}
	raw, err := domain.EncodeQuarantineDecision(decision, s.deps.Clock.Now())
	if err != nil {
		return domain.QuarantineState{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.deps.State.SaveQuarantineDecision(raw); err != nil {
		return domain.QuarantineState{}, fmt.Errorf("guardando la decisión de la cuarentena: %w", err)
	}
	s.quarantineDecision = decision

	var opErr error
	if wanted {
		opErr = s.deps.Firewall.ApplyBaseQuarantine(ctx, domain.BaseQuarantineFor(s.deps.Quarantine))
		if opErr != nil {
			opErr = fmt.Errorf("escribiendo la cuarentena de base: %w", opErr)
		}
	} else {
		opErr = s.deps.Firewall.RemoveBaseQuarantineAtUserRequest(ctx)
		if opErr != nil {
			opErr = fmt.Errorf("retirando la cuarentena de base: %w", opErr)
		}
	}

	// Measured even when the operation failed, and stored where the sweep
	// stores it: the faces read ONE truth about the quarantine, and it is
	// never this function's intention.
	if q, err := s.deps.Audit.QuarantineState(ctx); err != nil {
		s.deps.Log.Warn("no se pudo medir la cuarentena tras la decisión", "error", err)
		s.state.Quarantine = domain.QuarantineState{}
	} else {
		s.state.Quarantine = q
	}
	s.snapshot()

	return s.state.Quarantine, opErr
}

// loadQuarantineDecision reads the persisted answer at start.
//
// Absent IS undecided, which is the normal first boot. Malformed decodes to
// undecided WITH a log line, never to an invented answer: a made-up "no"
// would silently strip the machine of what the user asked for, and a made-up
// "yes" would close ports nobody agreed to close. Undecided just means the
// question gets asked again, which is the cheapest of the three mistakes.
func (s *Session) loadQuarantineDecision() domain.QuarantineDecision {
	raw, err := s.deps.State.LoadQuarantineDecision()
	if err != nil {
		s.deps.Log.Info("sin decisión de cuarentena guardada: se preguntará", "detalle", err)
		return domain.QuarantineUndecided
	}
	decision, _, err := domain.DecodeQuarantineDecision(raw)
	if err != nil {
		s.deps.Log.Warn("la decisión de la cuarentena no se pudo leer, así que se pregunta otra vez",
			"error", err)
		return domain.QuarantineUndecided
	}
	return decision
}
