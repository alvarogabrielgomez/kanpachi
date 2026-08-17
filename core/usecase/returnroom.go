package usecase

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/core/port"
	"github.com/accentiostudios/kanpachi/core/timing"
)

// loadLast reads the saved last room into memory at startup.
//
// Not having one is the ordinary case and not an error: it means this machine has
// never been a guest, or was a host the whole time. One that does not decode is
// not an error either, and it is DISCARDED rather than repaired: a last room is a
// pointer to somewhere, and half a pointer is not worth acting on.
func (s *Session) loadLast() {
	raw, err := s.deps.State.LoadLast()
	if err != nil {
		return
	}
	last, err := domain.DecodeLastRoom(raw)
	if err != nil {
		s.deps.Log.Warn("la última sala guardada no se pudo interpretar y se ignora", "error", err)
		return
	}
	s.last, s.hasLast = last, true
	if last.AutoReturn {
		s.deps.Log.Info("hay una sala a la que volver",
			"código", last.Room.InviteID.String(), "seed", last.Room.Seed)
	}
}

// forgetLastRoomLocked erases the saved room, on disk and in memory.
//
// # Forgetting is not the same as switching off, and only one thing forgets
//
// Leaving a room on purpose, and being kicked, turn `AutoReturn` off and **keep
// the file**: it is the LAST room and not the current one, which is what still
// offers going back by hand, and what makes kicking not the same as banning.
//
// This runs for one reason only: **the room stopped existing**. Then the pointer
// points at nothing, and keeping it would be offering a way back into a dead
// room. Which of the ways it ended does not reach here and does not matter —
// renewed code, closed room, swept entry all arrive as the same answer, on
// purpose.
//
// Asume el candado tomado.
func (s *Session) forgetLastRoomLocked(why string) {
	if !s.hasLast {
		return
	}
	code := s.last.Room.InviteID.String()
	s.last, s.hasLast = domain.LastRoom{}, false
	s.returnNextAt, s.returnAttempts, s.returnReason = time.Time{}, 0, ""
	if err := s.deps.State.ClearLast(); err != nil {
		s.deps.Log.Warn("no se pudo borrar la última sala guardada", "error", err)
	}
	s.deps.Log.Info("se olvida la última sala", "código", code, "motivo", why)
}

// stopReturningLocked is the formal request to stop going back.
//
// It rewrites the saved room with `AutoReturn` off and cuts whatever attempt is
// in flight. **The file stays**, with the room still named in it: this is the LAST
// room and not the current one, so going back by hand is still on offer. It is
// the same reason being kicked does not take the room away — kicking is not
// banning.
//
// Reached from every face, and from every way of saying it: pressing leave,
// pasting a different code, and opening a room of one's own.
//
// Asume el candado tomado.
func (s *Session) stopReturningLocked() {
	if s.returnCancel != nil {
		s.returnCancel()
	}
	s.returnNextAt, s.returnAttempts, s.returnReason = time.Time{}, 0, ""
	if !s.hasLast || !s.last.AutoReturn {
		return
	}
	s.last.AutoReturn = false
	raw, err := s.last.Encode()
	if err != nil {
		s.deps.Log.Warn("no se pudo serializar la última sala al dejar de volver", "error", err)
		return
	}
	if err := s.deps.State.SaveLast(raw); err != nil {
		s.deps.Log.Warn("no se pudo guardar que ya no se vuelve a la última sala", "error", err)
	}
	s.deps.Log.Info("se deja de volver a la última sala",
		"código", s.last.Room.InviteID.String())
}

// returningLocked derives whether this machine is on its way back, and where.
//
// Three facts, all already true somewhere else: not being in a room, having a
// saved last room, and its `AutoReturn` being on. Nothing to latch and nothing to
// clear — entering a room makes it false by itself, and so does leaving on
// purpose, because that writes the flag off.
//
// Asume el candado tomado.
func (s *Session) returningLocked() domain.ReturnAttempt {
	if s.state.Conn.InRoom() || !s.hasLast || !s.last.AutoReturn {
		return domain.ReturnAttempt{}
	}
	return domain.ReturnAttempt{
		Room:     s.last.Room,
		Name:     s.last.Name,
		NextAt:   s.returnNextAt,
		Attempts: s.returnAttempts,
		Reason:   s.returnReason,
	}
}

// ReturnDue says whether it is time to try getting back in.
//
// BARATO a propósito, igual que [Session.RejoinDue]: solo mira relojes y estado.
// Lo caro es [Session.Return], que corre fuera del despachador del supervisor.
func (s *Session) ReturnDue() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.returnDueLocked(s.deps.Clock.Now())
}

// returnDueLocked es la condición, en un solo sitio porque la miran los dos: el
// supervisor antes de lanzar la gorrutina, y [Session.Return] otra vez con el
// candado ya tomado.
//
// The zero `returnNextAt` is due NOW, which is what makes the first attempt of a
// daemon that just started immediate.
//
// Asume el candado tomado.
func (s *Session) returnDueLocked(now time.Time) bool {
	if s.returnRunning || !s.returningLocked().Returning() {
		return false
	}
	return !now.Before(s.returnNextAt)
}

// Return makes ONE attempt at getting back into the saved room.
//
// It is [Session.JoinRoom] with the saved code and nick, and there is no separate
// way in on purpose: coming back does the same credential exchange as the first
// time, with the host reissuing and seeing who arrives. That is what keeps
// revocation meaningful, and it is why a copied `last-room.json` grants nothing.
//
// # Why the lock is released across the attempt
//
// Because [Session.JoinRoom] takes it, and it holds it for about a minute. What
// the lock protects here is the turn: `returnRunning` is claimed before letting
// go, so a second caller finds nothing to do rather than dialling twice.
//
// # Cancelling
//
// The attempt runs under a context this session can cut, and [Session.JoinRoom]
// with `replace` cuts it. Whatever somebody just asked for beats whatever a clock
// decided.
func (s *Session) Return(ctx context.Context) {
	s.mu.Lock()
	if !s.returnDueLocked(s.deps.Clock.Now()) {
		s.mu.Unlock()
		return
	}
	last := s.last
	s.returnRunning = true
	s.returnAttempts++
	s.returnNextAt = time.Time{}
	ctx, cancel := context.WithCancel(ctx)
	s.returnCancel = cancel
	intento := s.returnAttempts
	s.snapshot()
	s.mu.Unlock()

	defer cancel()
	code := last.Room.InviteID.String() + "@" + last.Room.Seed
	// `replace` is false, and firmly. An automatic return never displaces
	// anything: if somebody is in a room by now, this attempt is the one that
	// gives way. It comes back as [ErrBusy] and gets no turn.
	//
	// `allowFirewall` is false for the same reason: an automatic path cannot
	// consent to touching the operator's firewall. If one blocks, the refusal
	// lands in the default branch below and the return keeps trying at its own
	// pace, which is right: the block is transient the moment the operator
	// opens it, and the sweep's [domain.AlertForeignRule] says why meanwhile.
	_, err := s.JoinRoom(ctx, code, last.Nick, false, false)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.returnRunning = false
	s.returnCancel = nil

	switch {
	case err == nil:
		s.returnAttempts, s.returnReason = 0, ""
		s.deps.Log.Info("de vuelta en la última sala", "código", code, "intentos", intento)

	// **The registry saying the room does not exist is the end of it**, and it is
	// the only answer that is. It reaches here as one sentinel for three different
	// endings —the code was renewed, the host closed the room, the entry was
	// swept— and telling them apart is neither possible nor wanted.
	case errors.Is(err, ErrNoSuchRoom):
		s.forgetLastRoomLocked("el registro dice que esa sala ya no existe")

	// Neither of these is a failure and neither gets a turn.
	//
	// Cancelled: the daemon is shutting down, or somebody asked for something
	// that supersedes this. Busy: somebody got there first between the beat
	// asking and this dialling — pasting another code, opening their own room, or
	// the host's own room finishing its reopen. Both are somebody else's business
	// going right, and writing either on a screen would be noise.
	case errors.Is(err, context.Canceled), errors.Is(err, ErrBusy):

	default:
		// **Everything else keeps trying, and the silence of the registry is
		// firmly in this branch.** A seed that does not answer, a name that does
		// not resolve, a throttle, a timeout: none of them is a room that ended.
		// Treating them as one would mean that losing `rooms.json` on the server
		// makes every guest of every room forget it at once, and that does not
		// come back.
		s.returnReason = err.Error()
		s.returnNextAt = s.deps.Clock.Now().Add(s.nextReturnWaitLocked(err))
		s.deps.Log.Info("no se pudo volver a la última sala todavía",
			"código", code, "intento", intento, "error", err,
			"próximo intento", s.returnNextAt)
	}
	s.snapshot()
}

// nextReturnWaitLocked spreads the next attempt inside its window.
//
// # The throttle gets pushed further out, and that is not politeness
//
// The registry's rate limit counts what FAILS too, so a client that retries into
// a throttle is a client that keeps its own door shut. Its error even says so.
// One guest on a five-minute cadence cannot cause a throttle by itself —the
// budget is thirty a minute— but the global one can be on for other reasons, and
// the right answer to being throttled is never to insist at the same rate.
//
// Sale de [port.Rand] y no de math/rand por lo mismo que el canario: hay una sola
// fuente de aleatoriedad en la sesión, es la que los tests pueden fijar.
//
// Asume el candado tomado.
func (s *Session) nextReturnWaitLocked(err error) time.Duration {
	base := timing.ReturnInterval
	if errors.Is(err, port.ErrSeedThrottled) {
		base *= 4
	}
	var b [1]byte
	if _, e := io.ReadFull(s.deps.Rand, b[:]); e != nil {
		// Sin aleatoriedad se espera el máximo, que es el lado seguro de
		// equivocarse: la manada queda sin dispersar, y más separada en vez de
		// más apretada.
		return base + timing.ReturnJitter
	}
	return base + time.Duration(b[0])*timing.ReturnJitter/255
}
