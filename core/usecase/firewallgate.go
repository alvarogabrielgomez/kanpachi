package usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// ErrFirewallBlocked is the sentinel the faces and the pipe match on: a
// foreign firewall would swallow the room's inbound and nobody consented to
// opening it. The rich version, with WHAT blocks and the exact commands, is
// [ErrFirewallBlocks]; this bare one exists for errors.Is and the protocol
// code, same split as ErrBusy and ErrWouldDisplace.
var ErrFirewallBlocked = errors.New("a foreign firewall blocks the room's inbound")

// ErrFirewallBlocks is the refusal that names the blocker.
//
// It carries the blocks so a face can show who blocks, which adapters are
// deaf, and the exact commands that would open them — the person consents to
// something concrete, never to a general idea of "fixing the firewall".
//
// The message spells all of it out because on the wire only {code, message}
// travels: whatever the face needs to ask a good question has to be in the
// sentence itself.
type ErrFirewallBlocks struct {
	Blocks []domain.FirewallBlock
}

func (e ErrFirewallBlocks) Error() string {
	names := make([]string, 0, len(e.Blocks))
	var fixes []string
	for _, b := range e.Blocks {
		names = append(names, b.String())
		fixes = append(fixes, b.Fix...)
	}
	return fmt.Sprintf("%s: the room would assemble and nobody would get in. "+
		"What would open it (undone when the room closes): %s",
		strings.Join(names, "; "), strings.Join(fixes, " && "))
}

// Is makes this answer to [ErrFirewallBlocked].
func (e ErrFirewallBlocks) Is(target error) bool { return target == ErrFirewallBlocked }

// firewallGateLocked is the foreign-firewall precondition of hosting and
// joining.
//
// # Why it runs BEFORE the state machine moves
//
// Because the defect it prevents was measured (2026-08-16, ufw on the
// droplet): with a foreign manager denying inbound, the room assembles
// perfectly, every check reads green, and nobody gets in. Refusing in the
// first second, naming the manager and both adapters, is the whole point;
// mounting half a room first would repeat the original afternoon.
//
// # What consent means here
//
// `allow` is the person having seen the exact commands and said yes — the
// same shape as `replace` in [Session.clearTheWayLocked]. With it, the
// adapter runs those commands and books every addition; the book is paid on
// leave and on service start ([Session.teardown], [NewSession]).
//
// # The automatic paths do not pass through here, and that is deliberate
//
// ResumeRoom reopens the saved room and Rejoin recovers a live one: neither
// can ask anybody anything. Gating them would turn a reboot into a room that
// never comes back. They proceed, and the sweep raises
// [domain.AlertForeignRule] so the screen says why nobody is arriving. The
// asymmetry is decision 36's.
//
// Asume el candado tomado.
func (s *Session) firewallGateLocked(ctx context.Context, allow bool) error {
	blocks, err := s.deps.Firewall.InboundBlocked(ctx)
	if err != nil {
		// No inventing "clear": a gate that cannot read refuses, it does not
		// wave through. Same doctrine as the adapter's.
		return fmt.Errorf("could not read whether a foreign firewall blocks the room: %w", err)
	}
	if len(blocks) == 0 {
		return nil
	}
	if !allow {
		return ErrFirewallBlocks{Blocks: blocks}
	}
	if err := s.deps.Firewall.AllowAdapters(ctx, blocks); err != nil {
		return fmt.Errorf("opening the foreign firewall with the given consent: %w", err)
	}
	return nil
}
