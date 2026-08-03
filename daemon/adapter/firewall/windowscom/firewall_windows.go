//go:build windows

package windowscom

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/core/port"
)

// Firewall implements port.FirewallPort against INetFwPolicy2.
type Firewall struct {
	ap *apartment
	// adapter is the friendly name of the virtual adapter. Empty until the
	// engine has created it, and an empty name means the permits go out without
	// interface scope rather than scoped to a name that does not exist.
	adapter string
	// suspendPath is where the previous state of somebody else's rules is
	// written before any of them is disabled. It lives on disk and not in
	// memory because the caller that needs it most is the NEXT start of the
	// service, after a dirty exit.
	suspendPath string
	log         Logger
}

// SuspendedRulesFile is the name of that record, inside the data directory.
const SuspendedRulesFile = "suspended-rules.json"

// Logger is the slice of the daemon log this adapter needs.
type Logger interface {
	Info(msg string, kv ...any)
	Warn(msg string, kv ...any)
	Error(msg string, kv ...any)
}

// New opens the apartment. Close it to release the thread.
//
// dataDir is where suspended-rules.json goes. It is required: without it the
// adapter could disable somebody's rules and have no way to put them back.
func New(dataDir, adapter string, log Logger) (*Firewall, error) {
	if dataDir == "" {
		return nil, fmt.Errorf("the firewall adapter needs a data directory to record suspended rules")
	}
	ap, err := newApartment()
	if err != nil {
		return nil, err
	}
	return &Firewall{
		ap:          ap,
		adapter:     adapter,
		suspendPath: filepath.Join(dataDir, SuspendedRulesFile),
		log:         log,
	}, nil
}

// The check that this still fits the port. If a signature in core changes, the
// error comes out here and not where it gets wired.
var _ port.FirewallPort = (*Firewall)(nil)

func (f *Firewall) Close() error { return f.ap.Close() }

// SetAdapter records the adapter name once the engine has created it.
//
// It exists because the adapter is born after the daemon: the first Apply of a
// session may legitimately run with no name, and rescoping later must not
// require rebuilding the whole adapter.
func (f *Firewall) SetAdapter(name string) { f.adapter = name }

// Apply brings the firewall to the desired set.
//
// The diff runs against the rules ENUMERATED FROM THE SYSTEM on every call, not
// against a remembered set. Two things depend on that and both are functional:
// Apply is idempotent, and reapplying the same set REPAIRS whatever someone
// deleted or added behind our back. With a remembered set, reapplying would be
// a no-op and the self-repair of the exposure module would not exist.
func (f *Firewall) Apply(ctx context.Context, desired domain.RuleSet) error {
	want, err := SpecsFor(desired, f.adapter)
	if err != nil {
		return err
	}

	all, err := f.liveRules(ctx)
	if err != nil {
		return err
	}
	live, others := ownSpecs(all), foreignNames(all)

	return f.ap.do(ctx, func(policy *ole.IDispatch) error {
		rules, err := rulesOf(policy)
		if err != nil {
			return err
		}
		defer rules.Release()

		wanted := make(map[string]ruleSpec, len(want))
		for _, s := range want {
			wanted[s.Name] = s
		}

		// Retire what nobody asked for, and what drifted. A drifted rule is
		// retired and rewritten rather than patched field by field: patching
		// leaves a window where the rule is half old and half new, and that
		// window is on the wire.
		//
		// retired tracks what actually went away. A rule that could not be
		// retired must NOT be rewritten, or every Apply adds another copy of
		// it and the store grows without bound.
		retired := map[string]bool{}
		for name := range live {
			s, keep := wanted[name]
			if keep && live[name].sameScope(s) {
				continue
			}
			gone, err := f.retire(rules, name, others[name])
			if err != nil {
				return err
			}
			retired[name] = gone
		}

		for _, s := range want {
			if l, ok := live[s.Name]; ok {
				if l.sameScope(s) {
					continue
				}
				if !retired[s.Name] {
					// Could not be retired, so it is still there under that
					// name. Writing another one would duplicate it forever.
					continue
				}
			}
			if err := addRule(rules, s); err != nil {
				return fmt.Errorf("writing rule %q: %w", s.Name, err)
			}
		}
		return nil
	})
}

// retire takes one of our rules out of service, and says whether it is GONE.
//
// # Why this is not just Remove
//
// INetFwPolicy2.Rules.Remove takes a NAME, and Windows lets unrelated rules
// share one. Measured the hard way on a real machine: removing a stale
// "easytier-core" rule by name also took out a live rule carrying the same
// name, because the API offers no way to say which one.
//
// So when every rule under that name is OURS, Remove is safe and the rule is
// gone. When somebody else carries the name too, deleting would be a coin flip
// on their configuration, so instead our own rule object is DISABLED in place.
// Enumerating hands us that exact object, which is the one precise handle the
// API gives.
//
// `others` is how many rules with this name are not ours, and not how many
// exist. Two rules of our own group sharing a name are both ours to delete.
//
// A disabled rule opens nothing, so the port is shut either way. What differs is
// that it is still there under that name, and the caller must not write another
// one on top: that is what the returned bool is for.
//
// Returning false is a functional failure -- the game's port will not open --
// and it is the right trade. Kanpachi failing to open a port is visible and
// recoverable; Kanpachi silently deleting a rule from a machine it does not own
// is neither.
func (f *Firewall) retire(rules *ole.IDispatch, name string, others int) (bool, error) {
	if others == 0 {
		if _, err := oleutil.CallMethod(rules, "Remove", name); err != nil {
			return false, fmt.Errorf("removing rule %q: %w", name, err)
		}
		return true, nil
	}

	disabled := false
	err := eachRule(rules, func(rule *ole.IDispatch) (bool, error) {
		r := propReader{rule: rule}
		ruleName, group := r.str("Name"), r.str("Grouping")
		if err := r.Err(); err != nil {
			return false, err
		}
		if ruleName != name || group != domain.FirewallGroup {
			return true, nil
		}
		if _, err := oleutil.PutProperty(rule, "Enabled", false); err != nil {
			return false, fmt.Errorf("disabling rule %q: %w", name, err)
		}
		disabled = true
		return true, nil
	})
	if err != nil {
		return false, err
	}

	f.log.Warn("una regla se desactivó en vez de borrarse porque su nombre lo usa alguien más",
		"regla", name, "reglas-ajenas-con-ese-nombre", others, "desactivada", disabled)
	return false, nil
}

func addRule(rules *ole.IDispatch, s ruleSpec) error {
	unknown, err := oleutil.CreateObject("HNetCfg.FWRule")
	if err != nil {
		return fmt.Errorf("creating the rule object: %w", err)
	}
	defer unknown.Release()

	rule, err := unknown.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		return err
	}
	defer rule.Release()

	set := func(prop string, val any) error {
		if _, err := oleutil.PutProperty(rule, prop, val); err != nil {
			return fmt.Errorf("setting %s: %w", prop, err)
		}
		return nil
	}
	for _, kv := range []struct {
		prop string
		val  any
	}{
		{"Name", s.Name},
		{"Grouping", s.Group},
		{"Direction", s.Direction},
		{"Action", s.Action},
		{"Protocol", s.Protocol},
		{"LocalPorts", s.LocalPorts},
		{"LocalAddresses", s.LocalAddresses},
		{"RemoteAddresses", s.RemoteAddresses},
		{"Profiles", s.Profiles},
		{"Enabled", s.Enabled},
	} {
		if err := set(kv.prop, kv.val); err != nil {
			return err
		}
	}
	// Interfaces last, and only when there is one. Writing an empty array
	// scopes the rule to no interface at all on some builds, which turns a
	// permit into a rule that opens nothing.
	if len(s.Interfaces) > 0 {
		if err := set("Interfaces", s.Interfaces); err != nil {
			return err
		}
	}

	if _, err := oleutil.CallMethod(rules, "Add", rule); err != nil {
		return fmt.Errorf("adding the rule to the store: %w", err)
	}
	return nil
}

// PurgeOwned deletes everything tagged with our group.
//
// It runs when the service starts, before applying anything: a dirty death of
// the daemon never leaves orphaned ports open.
//
// It NEVER touches the base group. That is the installer's quarantine and the
// only thing protecting the machine while the daemon is not running. If the
// purge took it, every service restart would disarm the protection, and the
// failure would be invisible because everything else would keep working.
//
// Comparison is by exact equality: "Kanpachi" is a prefix of "Kanpachi-base",
// so a prefix match here deletes the quarantine.
func (f *Firewall) PurgeOwned(ctx context.Context) error {
	all, err := f.liveRules(ctx)
	if err != nil {
		return err
	}
	// The same two reads Apply uses, and for the same reason: the count of
	// rules that are NOT the daemon's is what decides whether deleting by name
	// is safe. This used to call Remove blindly for every name in our group,
	// which is the same coin flip on someone else's configuration.
	live, others := ownSpecs(all), foreignNames(all)

	return f.ap.do(ctx, func(policy *ole.IDispatch) error {
		rules, err := rulesOf(policy)
		if err != nil {
			return err
		}
		defer rules.Release()

		for name := range live {
			if _, err := f.retire(rules, name, others[name]); err != nil {
				return err
			}
		}
		if len(live) > 0 {
			f.log.Info("reglas propias purgadas", "cantidad", len(live))
		}
		return nil
	})
}
