package netfw

import (
	"fmt"
	"sort"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// The pure half of the exposure audit. What gets MEASURED is decided here and
// tested on Linux; the _windows file only reads it off COM.
//
// The split matters more here than anywhere else in this package: this is the
// code that tells the user whether the promise still holds, and a bug in it does
// not break anything visible. It paints the screen green.

// appliedRules is what the firewall layer of [domain.Enforcement] carries.
//
// Only the DAEMON's own group. The quarantine is not in here on purpose: the
// diff compares this against the desired set of the active room, and the
// quarantine is in neither, so including it would report every one of its rules
// as Extra, which reads as "somebody tampered with your firewall".
//
// Sorted so two sweeps over an unchanged machine produce the same alert text.
func appliedRules(all []liveRule) []domain.AppliedRule {
	out := make([]domain.AppliedRule, 0, len(all))
	for _, c := range all {
		if !c.roomGroup() {
			continue
		}
		out = append(out, domain.AppliedRule{
			Name:    c.Name,
			Layer:   domain.LayerFirewallRules,
			Enabled: c.Enabled,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return !out[i].Enabled && out[j].Enabled
	})
	return out
}

// profileMask is the NET_FW_PROFILE_TYPE2 value of one domain profile.
//
// It exists as its own function, with an error rather than a default, because
// getting it wrong is silent: asking Windows whether the firewall is on for the
// wrong profile still answers, and the answer looks fine.
func profileMask(p domain.FirewallProfile) (int32, error) {
	switch p {
	case domain.ProfileDomain:
		return profileDomain, nil
	case domain.ProfilePrivate:
		return profilePrivate, nil
	case domain.ProfilePublic:
		return profilePublic, nil
	default:
		return 0, fmt.Errorf("unknown firewall profile %v", p)
	}
}
