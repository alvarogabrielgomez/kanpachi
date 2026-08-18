package netfw

import (
	"fmt"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/daemon/adapter/firewall/quarantine"
)

// The base quarantine, translated to the Windows shape.
//
// This file and its _windows sibling are the ONLY place in daemon/ allowed to
// name domain.FirewallGroupBase. A guardian in internal/arch enforces that, and
// it is deliberately a one-package allowance: the daemon has to write the
// quarantine because there is no installer, and everything else in daemon/ still
// must not be able to so much as mention it.
//
// What the guardian protects has therefore MOVED. It used to be "the daemon
// never names the group". It is now "the daemon can never DELETE it", which is
// the property that actually matters, and it is enforced on the interface: there
// is no method that could.

// quarantineSpecFor translates one quarantine rule into the Windows shape.
//
// # Every difference from specFor is the point
//
// A permit and a block are not mirror images, and reading this next to specFor
// is the fastest way to see why:
//
//   - Action is BLOCK, and it comes from the type rather than a field.
//     domain.QuarantineRule cannot express a permit at all.
//   - Direction comes from the rule. A permit is always inbound; the quarantine
//     shuts the local service in both directions.
//   - LocalAddresses is EMPTY. A permit is scoped to the virtual adapter's
//     address because a permit that stops matching closes. A block that stops
//     matching opens, so scoping one is a hole with a schedule.
//   - RemoteAddresses is EMPTY, and Windows reads that as "*". On a permit that
//     is the worst possible meaning and specFor refuses to produce it. On a
//     block it is the ONLY correct one: "nobody reaches my SMB" is not a
//     statement about who.
//   - Interfaces is nil, for the same reason as LocalAddresses.
//
// The port is always the LOCAL one, in both directions, and that is what keeps
// the quarantine from breaking the machine. Inbound local 445 is "nobody reaches
// MY file sharing". Outbound local 445 is "my file sharing does not talk out
// either". Neither stops this PC from being a CLIENT: mounting a network drive,
// or git over SSH, leaves from an ephemeral local port towards the OTHER
// machine's 445 or 22. Blocking by REMOTE port would break those, permanently,
// because the quarantine outlives the daemon.
func quarantineSpecFor(r domain.QuarantineRule) (ruleSpec, error) {
	// The precondition is shared with the Linux writer, and it has to be: this
	// file used to check the port range and the nftables one did not, which is
	// the drift that a rule list travelling to two translators invites.
	if err := quarantine.Validate(r); err != nil {
		return ruleSpec{}, err
	}
	proto, err := protocolOf(r.Proto)
	if err != nil {
		return ruleSpec{}, fmt.Errorf("quarantine rule %q: %w", r.Name, err)
	}

	direction := int32(dirOut)
	if r.In {
		direction = dirIn
	}

	return ruleSpec{
		Name:       r.Name,
		Group:      domain.FirewallGroupBase,
		Direction:  direction,
		Action:     actionBlock,
		Protocol:   proto,
		LocalPorts: quarantine.PortRange(r.From, r.To),
		Profiles:   profileAll,
		Enabled:    true,
	}, nil
}

// QuarantineSpecs translates the whole quarantine.
//
// Like SpecsFor, it fails on the FIRST bad rule instead of skipping it. A
// quarantine applied with one rule quietly dropped is worse than one that failed
// outright: the daemon would report the machine protected with a hole in it.
func QuarantineSpecs(rules []domain.QuarantineRule) ([]ruleSpec, error) {
	out := make([]ruleSpec, 0, len(rules))
	for _, r := range rules {
		s, err := quarantineSpecFor(r)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}
