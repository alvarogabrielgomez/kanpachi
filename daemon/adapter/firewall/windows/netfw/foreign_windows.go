//go:build windows

package netfw

import (
	"context"
	"fmt"

	"github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// candidateOf reads the handful of properties this package cares about off a
// live rule.
//
// A failed read is an ERROR and never a zero value, and in this function that
// is not pedantry: Action 0 is NET_FW_ACTION_BLOCK, so a swallowed failure
// would turn a live permissive rule into a block, drop it from the audit, and
// leave the screen saying there is nothing to see.
func candidateOf(rule *ole.IDispatch) (liveRule, error) {
	r := propReader{rule: rule}
	c := liveRule{
		Name:            r.str("Name"),
		Group:           r.str("Grouping"),
		Application:     r.str("ApplicationName"),
		Direction:       r.int32("Direction"),
		Action:          r.int32("Action"),
		Protocol:        r.int32("Protocol"),
		LocalPorts:      r.str("LocalPorts"),
		LocalAddresses:  r.str("LocalAddresses"),
		RemoteAddresses: r.str("RemoteAddresses"),
		Interfaces:      r.strs("Interfaces"),
		Profiles:        r.int32("Profiles"),
		Enabled:         r.bool("Enabled"),
	}
	return c, r.Err()
}

// liveRules copies the whole store out as plain values.
//
// Reading everything first and deciding afterwards is the point: every decision
// about foreign rules then happens in pure code that the Linux CI runs, and
// this function has nothing left to get wrong except reading a property name.
func (f *Firewall) liveRules(ctx context.Context) ([]liveRule, error) {
	var out []liveRule
	err := f.ap.do(ctx, func(policy *ole.IDispatch) error {
		rules, err := rulesOf(policy)
		if err != nil {
			return err
		}
		defer rules.Release()

		return eachRule(rules, func(rule *ole.IDispatch) (bool, error) {
			c, err := candidateOf(rule)
			if err != nil {
				return false, err
			}
			out = append(out, c)
			return true, nil
		})
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// AuditForeign looks for permissive rules Kanpachi did not create.
//
// It asks the RULE STORE, by executable path. It does not enumerate processes
// and does not know whether the program is running, which is the right shape:
// the rule is on disk whether or not there is a game on.
func (f *Firewall) AuditForeign(ctx context.Context, p domain.GameProfile) ([]domain.ForeignRule, error) {
	live, err := f.liveRules(ctx)
	if err != nil {
		return nil, err
	}

	var out []domain.ForeignRule
	for _, c := range live {
		if fr, ok := c.foreign(p.Detect.Executables); ok {
			out = append(out, fr)
		}
	}
	sortForeign(out)

	// How many rules were SWEPT, and not only how many were found.
	//
	// "Nothing found" and "nothing looked at" produce the same empty slice, and
	// on this particular audit the second one means the one hole that hands
	// somebody keyboard, screen and files went unreported. A sweep that returns
	// zero rules on a real Windows machine is impossible: the store ships with
	// hundreds. So the count is what tells the two apart, and it belongs in the
	// log rather than in a return value nobody would check.
	f.log.Info("reglas ajenas auditadas", "revisadas", len(live), "halladas", len(out))
	if len(live) == 0 {
		return nil, fmt.Errorf("el almacén de reglas devolvió CERO reglas, y una máquina " +
			"Windows real trae cientos. Eso no es una auditoría limpia, es una auditoría " +
			"que no miró nada")
	}
	return out, nil
}

// SuspendForeign disables the rules the user agreed to disable. It never
// deletes one.
//
// # The order is the design
//
// The record goes to DISK FIRST, and only then is anything disabled. The other
// order loses the original state on a power cut: the rules stay off and nothing
// left on the machine knows they were ever on.
//
// # It is all or nothing
//
// A rule the caller asked for and that is not in the store aborts the whole
// call before anything is written or touched. Returning success with one of
// them still enabled is the dangerous direction: the user would open the room
// believing the remote-control tool was off.
func (f *Firewall) SuspendForeign(ctx context.Context, rules []domain.ForeignRule) error {
	if len(rules) == 0 {
		return nil
	}

	live, err := f.liveRules(ctx)
	if err != nil {
		return err
	}

	fresh := make([]suspendedRule, 0, len(rules))
	want := make(map[string]bool, len(rules))
	for _, r := range rules {
		found := 0
		for _, c := range live {
			if !c.matches(r) {
				continue
			}
			found++
			fresh = append(fresh, suspendedRule{Rule: c.fingerprint(), WasEnabled: c.Enabled})
			want[c.fingerprint().key()] = false
		}
		if found == 0 {
			return fmt.Errorf("the rule %q of %q is not in the firewall, so nothing was suspended",
				r.Name, r.Executable)
		}
	}

	existing, err := loadSuspended(f.suspendPath)
	if err != nil {
		return err
	}
	if err := saveSuspended(f.suspendPath, mergeSuspended(existing, fresh)); err != nil {
		return err
	}

	changed, err := f.setEnabled(ctx, want)
	if err != nil {
		return err
	}
	f.log.Info("reglas ajenas desactivadas", "pedidas", len(rules), "cambiadas", changed)
	return nil
}

// RestoreForeign puts them back the way they were.
//
// It runs when the room ends and also when the service starts, in case a dirty
// exit left something off. Both callers can run it with nothing suspended, and
// that is not an error: a missing file is what a clean install and every clean
// exit look like.
//
// The record file is cleared LAST, and only if every write succeeded. A failure
// halfway leaves the file untouched so the next start tries again, and writing
// Enabled=true over a rule that is already enabled costs nothing.
func (f *Firewall) RestoreForeign(ctx context.Context) error {
	recs, err := loadSuspended(f.suspendPath)
	if err != nil {
		return err
	}
	if len(recs) == 0 {
		return nil
	}

	live, err := f.liveRules(ctx)
	if err != nil {
		return err
	}

	want := make(map[string]bool, len(recs))
	var gone []string
	for _, rec := range recs {
		i, ok := matchRecord(rec, live)
		if !ok {
			gone = append(gone, rec.Rule.Name)
			continue
		}
		want[live[i].fingerprint().key()] = rec.WasEnabled
	}

	changed, err := f.setEnabled(ctx, want)
	if err != nil {
		return err
	}
	if len(gone) > 0 {
		// Warned and dropped rather than kept: the rule was deleted while it
		// was suspended, so there is nothing left to restore and keeping the
		// record would warn about it on every start, forever.
		f.log.Warn("había reglas suspendidas que ya no existen en el firewall",
			"reglas", gone)
	}
	f.log.Info("reglas ajenas restauradas", "anotadas", len(recs), "cambiadas", changed)
	return saveSuspended(f.suspendPath, nil)
}

// InboundBlocked answers empty on Windows, and that is a stated cut, not a
// measurement.
//
// The defect this method exists for was measured on LINUX (ufw swallowing the
// adapters' SYNs, 2026-08-16). On Windows the inbound default-block belongs to
// the Windows Firewall, and OUR permits layer already opens it per rule, so the
// measured case does not apply as-is; a third-party suite blocking the virtual
// adapters is plausible and UNMEASURED. Per the plan's rule, the Windows
// adapter gets written after the case is measured, not before: answering a
// guess here would be the exact lie InboundBlocked exists not to tell, so the
// honest answer today is "nothing detected", said out loud in this comment.
func (f *Firewall) InboundBlocked(context.Context) ([]domain.FirewallBlock, error) {
	return nil, nil
}

// AllowAdapters has nothing to open while InboundBlocked reports nothing.
func (f *Firewall) AllowAdapters(context.Context, []domain.FirewallBlock) error { return nil }

// WithdrawAdapters has no book on Windows yet; nothing pending is the truth.
func (f *Firewall) WithdrawAdapters(context.Context) error { return nil }

// setEnabled writes Enabled on the rules whose fingerprint is in want.
//
// Rules of our own groups are skipped even if a key somehow matched. Disabling
// a rule of the base group would disarm the installer's quarantine, and the
// caller would be told it worked.
func (f *Firewall) setEnabled(ctx context.Context, want map[string]bool) (int, error) {
	if len(want) == 0 {
		return 0, nil
	}

	changed := 0
	err := f.ap.do(ctx, func(policy *ole.IDispatch) error {
		rules, err := rulesOf(policy)
		if err != nil {
			return err
		}
		defer rules.Release()

		return eachRule(rules, func(rule *ole.IDispatch) (bool, error) {
			c, err := candidateOf(rule)
			if err != nil {
				return false, err
			}
			if c.ours() {
				return true, nil
			}
			enabled, asked := want[c.fingerprint().key()]
			if !asked || c.Enabled == enabled {
				return true, nil
			}
			if _, err := oleutil.PutProperty(rule, "Enabled", enabled); err != nil {
				return false, fmt.Errorf("setting Enabled=%t on rule %q: %w", enabled, c.Name, err)
			}
			changed++
			return true, nil
		})
	})
	return changed, err
}
