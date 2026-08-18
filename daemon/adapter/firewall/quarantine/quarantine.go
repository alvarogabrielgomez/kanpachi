// Package quarantine is what the two systems share about the base quarantine.
//
// # Why it is smaller than "everything about the quarantine"
//
// Because the two halves that LOOK shareable are not. Each system translates
// [domain.QuarantineRule] into its own target: Windows into a COM rule with
// profiles and interfaces, Linux into a line of nftables text. Those targets
// have nothing in common, so a translator that served both would be a switch on
// the system wearing a shared name.
//
// What IS shared is everything BEFORE the translation: whether a rule can be
// written at all, how a port range reads, and which ports have to be left alone
// because something is already listening on them. Those three used to live
// twice, once per system, which is how they drifted: Windows validated the port
// range and Linux did not, Linux skipped ports in use and Windows did not.
//
// # Pure on purpose, and it is what makes the skip testable
//
// No build tags, no syscalls, no files. The Linux CI runs it, and so does a
// Windows machine. [SplitInUse] used to sit behind a `//go:build linux` for no
// reason other than where it was written, so the arithmetic that decides which
// of the user's services survive could only be exercised on Linux.
package quarantine

import (
	"fmt"
	"sort"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// Validate says whether a rule can be written at all.
//
// It is the precondition BOTH translators need and neither of them can skip: a
// protocol the system cannot name, or a range that runs backwards, produces a
// rule that either fails at the API or, worse, is written meaning something
// else.
//
// It fails rather than repairing. A quarantine rule that got silently corrected
// is a port the user believes is closed on the strength of a guess.
func Validate(r domain.QuarantineRule) error {
	if _, err := ProtoName(r.Proto); err != nil {
		return fmt.Errorf("quarantine rule %q: %w", r.Name, err)
	}
	if r.From == 0 || r.To == 0 || r.From > r.To {
		return fmt.Errorf("quarantine rule %q: bad port range %d-%d", r.Name, r.From, r.To)
	}
	return nil
}

// ProtoName is the protocol as both systems spell it.
//
// The two happen to agree on "tcp" and "udp", and that agreement is why this is
// one function. The default case is not laziness: the quarantine is written per
// concrete protocol, and [domain.ProtoBoth] reaching here means somebody built
// the rule list wrong rather than that this should expand it.
func ProtoName(p domain.Proto) (string, error) {
	switch p {
	case domain.ProtoTCP:
		return "tcp", nil
	case domain.ProtoUDP:
		return "udp", nil
	default:
		return "", fmt.Errorf("protocol %v: the quarantine is written per concrete protocol", p)
	}
}

// PortRange renders a range the way both systems read it.
//
// A single port prints as one number and not as `445-445`, which is the form a
// person reads twice before understanding it is one port. Windows wants this
// string for `LocalPorts` and nftables wants it after `dport`, and they want it
// identical, so it is written once.
func PortRange(from, to uint16) string {
	if from == to {
		return fmt.Sprintf("%d", from)
	}
	return fmt.Sprintf("%d-%d", from, to)
}

// SplitInUse separates the ports that must be left open from the rules that can
// be written.
//
// # Why a port with a listener is not closed
//
// Because closing it would take away something the machine is already doing,
// and the quarantine outlives the daemon: the user would be left without a
// service they were using, with nothing on the machine explaining why. The one
// case that motivated it is an operator who moved sshd to another port on the
// list and would have locked themselves out of their own server.
//
// # It returns PORTS and RULES, and the asymmetry is the point
//
// Each port produces four rules, two protocols by two directions. What has to
// be reported to a person is the port, once; what has to be written is the
// rules that are left. Returning the same shape twice would force the caller to
// collapse the four back into one, which is the step that gets forgotten.
//
// The skipped ports come out sorted so two runs over an unchanged machine
// produce the same line.
func SplitInUse(
	rules []domain.QuarantineRule, listening map[uint16]bool,
) (skipped []uint16, remaining []domain.QuarantineRule) {
	seen := map[uint16]bool{}
	remaining = make([]domain.QuarantineRule, 0, len(rules))

	for _, r := range rules {
		if !listening[r.From] {
			remaining = append(remaining, r)
			continue
		}
		if !seen[r.From] {
			seen[r.From] = true
			skipped = append(skipped, r.From)
		}
	}
	sort.Slice(skipped, func(i, j int) bool { return skipped[i] < skipped[j] })
	return skipped, remaining
}
