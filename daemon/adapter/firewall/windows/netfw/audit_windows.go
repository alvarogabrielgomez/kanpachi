//go:build windows

package netfw

import (
	"context"
	"fmt"

	"github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// Audit answers two of the three questions of port.ExposureAudit.
//
// # Why it is not the whole port
//
// RouterMappings asks the user's ROUTER over IGD, which is a different protocol
// on a different network, and it belongs to its own adapter. Implementing it
// here as `return nil, nil` would be the worst option available: "no mappings
// found" and "nobody looked" would become indistinguishable, on the one screen
// whose entire job is telling those two apart.
//
// So this type deliberately does NOT satisfy the port. Whoever wires the daemon
// composes it with the router adapter. Until that exists, the sinimplementar
// version stays, and it fails, which is visible.
//
// It shares the [Firewall]'s apartment rather than opening its own. That gives
// it the locked thread for free and serialises the audit against Apply, so a
// sweep can never read the store while it is half rewritten.
type Audit struct {
	fw *Firewall
}

func NewAudit(fw *Firewall) *Audit { return &Audit{fw: fw} }

// FirewallEnabled asks whether the Windows Firewall is on, per profile.
//
// All three are asked, always. Kanpachi does not depend on Windows classifying
// the network correctly, so "the profile this adapter landed in" is not a
// question this code is allowed to guess at. See decision 15.
func (a *Audit) FirewallEnabled(ctx context.Context) ([]domain.FirewallProfileState, error) {
	profiles := domain.AllFirewallProfiles()
	out := make([]domain.FirewallProfileState, 0, len(profiles))

	err := a.fw.ap.do(ctx, func(policy *ole.IDispatch) error {
		for _, p := range profiles {
			mask, err := profileMask(p)
			if err != nil {
				return err
			}
			v, err := oleutil.GetProperty(policy, "FirewallEnabled", mask)
			if err != nil {
				return fmt.Errorf("asking whether the firewall is on for the %s profile: %w", p, err)
			}
			on := v.Value() == true
			_ = v.Clear()
			out = append(out, domain.FirewallProfileState{Profile: p, Enabled: on})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Enforcement returns what the system has in place RIGHT NOW.
//
// It MEASURES and never judges. Whether that is what was asked for is decided by
// [domain.Enforcement.Diff], which is domain and runs without Windows.
func (a *Audit) Enforcement(ctx context.Context) (domain.Enforcement, error) {
	all, err := a.fw.liveRules(ctx)
	if err != nil {
		return domain.Enforcement{}, err
	}
	return domain.Enforcement{
		Rules: appliedRules(all),
		Gate:  a.gate(),
	}, nil
}

// QuarantineState measures the base quarantine against what BaseQuarantine
// says it should be, on the live store.
//
// The desired set is recomputed here rather than remembered from the last
// ApplyBaseQuarantine, and that is the whole point of the method: a rule
// somebody deleted, disabled or edited has to show, and a memory cannot show
// it. Reading and counting are split so the counting runs under the Linux CI.
func (a *Audit) QuarantineState(ctx context.Context) (domain.QuarantineState, error) {
	want, err := QuarantineSpecs(domain.BaseQuarantine())
	if err != nil {
		return domain.QuarantineState{}, err
	}
	all, err := a.fw.liveRules(ctx)
	if err != nil {
		return domain.QuarantineState{}, err
	}
	missing, disabled, drifted := quarantineTally(all, want)
	return domain.MeasuredQuarantine(domain.QuarantineWindows, missing, disabled, drifted), nil
}

// gate reports the state of the packet-filter gate.
//
// # Read this before changing it
//
// It returns ABSENT and not UNKNOWN, and the difference is the whole point of
// [domain.GateState]. Unknown means "could not be measured", which would be a
// lie here: there is no code in this program that installs a gate, so its
// absence is not a failed measurement, it is a fact known by construction.
//
// [domain.EnforcementDiff] treats an absent gate as an ALERT and never an
// emergency, which is correct: with no gate, containment degrades exactly to
// the Windows Firewall rules, which is what the product had before the gate was
// designed. Nothing new is open.
//
// The gate now exists, and this function is deliberately NOT the place that
// measures it: the gate is a WFP session that this adapter does not own and
// cannot see. What overwrites this answer with the real measurement is the
// composition of the two layers, in the parent package. Reading this adapter on
// its own therefore reports a gate that is missing while it may be present: a
// false alarm, never a false green. That is the direction this is allowed to
// fail in.
func (a *Audit) gate() domain.GateState { return domain.GateAbsent }
