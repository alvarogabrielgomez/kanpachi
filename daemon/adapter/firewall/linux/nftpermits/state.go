package nftpermits

// The pure half of the quarantine measurement: what counts as present is
// decided here, with no build tag, and the _linux file only fetches the text.
// Same split as netfw's audit, and it earns its keep the same way — a bug in
// this code breaks nothing visible, it paints the screen green.

import (
	"fmt"
	"strings"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// quarantineTableLoaded says whether the quarantine table is in the ruleset.
//
// The trailing space is load-bearing: it keeps "kanpachi-base" from matching a
// hypothetical "kanpachi-base-something". Same match QuarantineLoaded uses.
func quarantineTableLoaded(ruleset string) bool {
	return strings.Contains(ruleset, "table inet "+QuarantineTable+" ")
}

// missingFromRuleset counts the quarantine rules the kernel does not have.
//
// Presence is judged by the rule's COMMENT, which carries the same
// deterministic name in the file, in the kernel, and in the Windows firewall
// console. It is the one part of an nft rule that survives the round trip
// verbatim: nft is free to reformat the match, and does.
//
// There is no disabled and no drifted tally here, and the asymmetry is the
// system's: an nftables rule cannot be switched off in place, and editing one
// is deleting it and adding another, so every way of breaking the quarantine
// shows up as missing. The ports that apply-time left open because something
// was listening on them count as missing too, which is correct — the question
// is what the machine HAS, not what the daemon meant.
func missingFromRuleset(ruleset string, rules []domain.QuarantineRule) int {
	missing := 0
	for _, r := range rules {
		if !strings.Contains(ruleset, fmt.Sprintf("comment %q", r.Name)) {
			missing++
		}
	}
	return missing
}
