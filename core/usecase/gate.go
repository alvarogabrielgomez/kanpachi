package usecase

import (
	"context"
	"fmt"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// ErrWouldDisplace is entering somewhere while something else is in the way, and
// nobody having said it is fine.
//
// It carries WHAT is in the way, which is the whole point: a face that gets this
// can name the room it is about to leave, or say that closing means everybody
// inside drops, without working any of it out for itself.
//
// It answers to [ErrBusy] so the callers that only ever wanted "not now" keep
// working unchanged.
type ErrWouldDisplace struct {
	What domain.Displacement
}

func (e ErrWouldDisplace) Error() string {
	switch e.What.Kind {
	case domain.DisplaceCloseRoom:
		return fmt.Sprintf("ya hospedas la sala %s, y entrar a otra la cerraría",
			e.What.Room.InviteID)
	case domain.DisplaceStopReturning:
		return fmt.Sprintf("estás volviendo a la sala %s, y entrar a otra lo cancelaría",
			e.What.Room.InviteID)
	default:
		return fmt.Sprintf("ya estás en la sala %s, y entrar a otra te sacaría de ella",
			e.What.Room.InviteID)
	}
}

// Is makes this answer to [ErrBusy].
func (e ErrWouldDisplace) Is(target error) bool { return target == ErrBusy }

// Displacement says what entering a room would cost right now, without doing
// anything about it.
//
// It is what a face calls BEFORE showing its confirmation, so the question on
// screen already names what is at stake. The window gets it inside
// [Session.PeekInvite], which it already calls before building its dialog.
func (s *Session) Displacement() domain.Displacement {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.displacementLocked()
}

// displacementLocked is the rule, in ONE place.
//
// The order matters and is not arbitrary: being in a room wins over being on the
// way back, because they cannot both be true and the first is the one that costs
// somebody else something.
//
// Asume el candado tomado.
func (s *Session) displacementLocked() domain.Displacement {
	if s.state.Conn.InRoom() {
		kind := domain.DisplaceLeaveRoom
		otros := 0
		if s.state.IsHost() {
			kind = domain.DisplaceCloseRoom
			for _, p := range s.state.Peers {
				if !p.Self {
					otros++
				}
			}
		}
		return domain.Displacement{
			Kind:    kind,
			Room:    s.state.Room,
			Name:    s.state.Name,
			Members: otros,
		}
	}
	if r := s.returningLocked(); r.Returning() {
		return domain.Displacement{
			Kind: domain.DisplaceStopReturning,
			Room: r.Room,
			Name: r.Name,
		}
	}
	return domain.Displacement{}
}

// clearTheWayLocked is the gate every way into a room passes through.
//
// # Why it enforces rather than advises
//
// Because it is reachable from three faces plus the pipe, and a check each of
// them performs is a check any of them can skip. This one cannot be skipped:
// [Session.JoinRoom] and [Session.CreateRoom] call it before anything else, so
// the answer to "can I enter" is the same wherever the question comes from.
//
// # Why the leaving happens HERE, inside the same lock
//
// Because "leave, then join" as two calls leaves a gap, and the clock that
// retries going back fits through it. What comes out the other side is somebody
// dialling twice, or landing in a room they did not ask for. Under one lock
// there is no gap to fit through.
//
// Asume el candado tomado.
func (s *Session) clearTheWayLocked(ctx context.Context, replace bool) error {
	d := s.displacementLocked()
	if !d.Any() {
		return nil
	}
	if !replace {
		return ErrWouldDisplace{What: d}
	}

	switch d.Kind {
	case domain.DisplaceStopReturning:
		s.stopReturningLocked()
	default:
		s.deps.Log.Info("se sale de la sala anterior para entrar a otra",
			"código", d.Room.InviteID.String(), "cierra", d.Kind == domain.DisplaceCloseRoom)
		s.leaveRoomLocked(ctx)
	}
	return nil
}
