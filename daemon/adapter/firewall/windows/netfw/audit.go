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

// quarantineTally counts what is wrong with the base quarantine as it stands.
//
// It ONLY counts; the verdict over the counts is [domain.MeasuredQuarantine],
// which is domain and needs no firewall. Pure and tested on Linux for the same
// reason as appliedRules above: a bug here does not break anything visible, it
// paints the screen green.
//
// The drifted count is the case that until now went to ONE log line, in
// ApplyBaseQuarantine, and nowhere a person looks: a rule of ours that
// somebody edited until it stopped matching still counts as present to
// anything counting by name, and it blocks nothing. Disabled is checked
// before drift on purpose — sameScope compares Enabled too, so without the
// order a disabled rule would tally as drifted and the repair on offer would
// be the wrong one.
func quarantineTally(all []liveRule, want []ruleSpec) (missing, disabled, drifted int) {
	live := make(map[string]liveRule, len(all))
	for _, c := range all {
		if c.Group == domain.FirewallGroupBase {
			live[c.Name] = c
		}
	}
	for _, s := range want {
		l, ok := live[s.Name]
		switch {
		case !ok:
			missing++
		case !l.Enabled:
			disabled++
		case !l.spec().sameScope(s):
			drifted++
		}
	}
	return missing, disabled, drifted
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
