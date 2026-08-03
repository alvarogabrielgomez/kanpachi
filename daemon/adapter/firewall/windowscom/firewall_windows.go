//go:build windows

package windowscom

import (
	"context"
	"fmt"

	"github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// Firewall implements port.FirewallPort against INetFwPolicy2.
type Firewall struct {
	ap *apartment
	// adapter is the friendly name of the virtual adapter. Empty until the
	// engine has created it, and an empty name means the permits go out without
	// interface scope rather than scoped to a name that does not exist.
	adapter string
	log     Logger
}

// Logger is the slice of the daemon log this adapter needs.
type Logger interface {
	Info(msg string, kv ...any)
	Warn(msg string, kv ...any)
	Error(msg string, kv ...any)
}

// New opens the apartment. Close it to release the thread.
func New(adapter string, log Logger) (*Firewall, error) {
	ap, err := newApartment()
	if err != nil {
		return nil, err
	}
	return &Firewall{ap: ap, adapter: adapter, log: log}, nil
}

func (f *Firewall) Close() error { return f.ap.Close() }

// SetAdapter records the adapter name once the engine has created it.
//
// It exists because the adapter is born after the daemon: the first Apply of a
// session may legitimately run with no name, and rescoping later must not
// require rebuilding the whole adapter.
func (f *Firewall) SetAdapter(name string) { f.adapter = name }

// liveRule is one rule of our own group as read back from the system.
type liveRule struct {
	spec  ruleSpec
	group string
}

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

	return f.ap.do(ctx, func(policy *ole.IDispatch) error {
		rules, err := rulesOf(policy)
		if err != nil {
			return err
		}
		defer rules.Release()

		live, byName, err := readOwn(rules)
		if err != nil {
			return err
		}

		wanted := make(map[string]ruleSpec, len(want))
		for _, s := range want {
			wanted[s.Name] = s
		}

		// Remove what nobody asked for, and what drifted. A drifted rule is
		// removed and rewritten rather than patched: patching field by field
		// leaves a window where the rule is half old and half new, and that
		// window is on the wire.
		for name, l := range live {
			s, keep := wanted[name]
			if keep && l.spec.sameScope(s) {
				continue
			}
			if err := f.removeOwn(rules, name, byName); err != nil {
				return err
			}
		}

		for _, s := range want {
			if l, ok := live[s.Name]; ok && l.spec.sameScope(s) {
				continue
			}
			if err := addRule(rules, s); err != nil {
				return fmt.Errorf("writing rule %q: %w", s.Name, err)
			}
		}
		return nil
	})
}

// removeOwn deletes one of our rules by name, refusing when the name is shared.
//
// # Why the refusal is not paranoia
//
// INetFwPolicy2.Rules.Remove takes a NAME, and Windows lets unrelated rules
// share one. Measured the hard way on a real machine: removing a stale
// "easytier-core" rule by name also took out a live one that happened to carry
// the same name, because the API gives no way to say which.
//
// PurgeOwned survives that because it deletes the whole group and the ambiguity
// does not matter. Here it does, so the rule is: if any rule with this name
// belongs to someone else, do not touch it. Leaving one of ours behind is a
// stale allow that the next sweep reports; deleting someone else's is silent
// damage to a machine Kanpachi does not own.
func (f *Firewall) removeOwn(rules *ole.IDispatch, name string, byName map[string]int) error {
	if byName[name] > 1 {
		f.log.Warn("a rule was left in place because its name is shared with rules that are not ours",
			"regla", name, "coincidencias", byName[name])
		return nil
	}
	if _, err := oleutil.CallMethod(rules, "Remove", name); err != nil {
		return fmt.Errorf("removing rule %q: %w", name, err)
	}
	return nil
}

// readOwn returns our group's rules, and how many rules carry each name across
// the WHOLE store, which is what makes removal safe.
func readOwn(rules *ole.IDispatch) (map[string]liveRule, map[string]int, error) {
	own := map[string]liveRule{}
	byName := map[string]int{}

	err := eachRule(rules, func(rule *ole.IDispatch) (bool, error) {
		name := strProp(rule, "Name")
		byName[name]++

		// Exact equality on the group, never a prefix: "Kanpachi" is a prefix
		// of "Kanpachi-base", and the base group is the installer's quarantine,
		// which the daemon must never touch.
		if strProp(rule, "Grouping") != domain.FirewallGroup {
			return true, nil
		}
		own[name] = liveRule{
			group: domain.FirewallGroup,
			spec: ruleSpec{
				Name:            name,
				Group:           domain.FirewallGroup,
				Direction:       intProp(rule, "Direction"),
				Action:          intProp(rule, "Action"),
				Protocol:        intProp(rule, "Protocol"),
				LocalPorts:      strProp(rule, "LocalPorts"),
				LocalAddresses:  strProp(rule, "LocalAddresses"),
				RemoteAddresses: strProp(rule, "RemoteAddresses"),
				Interfaces:      interfacesOf(rule),
				Profiles:        intProp(rule, "Profiles"),
				Enabled:         boolProp(rule, "Enabled"),
			},
		}
		return true, nil
	})
	return own, byName, err
}

// interfacesOf reads the interface scope, which arrives as a VARIANT holding an
// array of names and is empty on most rules.
func interfacesOf(rule *ole.IDispatch) []string {
	v, err := oleutil.GetProperty(rule, "Interfaces")
	if err != nil {
		return nil
	}
	defer func() { _ = v.Clear() }()
	arr := v.ToArray()
	if arr == nil {
		return nil
	}
	vals := arr.ToStringArray()
	if len(vals) == 0 {
		return nil
	}
	return vals
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
	return f.ap.do(ctx, func(policy *ole.IDispatch) error {
		rules, err := rulesOf(policy)
		if err != nil {
			return err
		}
		defer rules.Release()

		var names []string
		err = eachRule(rules, func(rule *ole.IDispatch) (bool, error) {
			if strProp(rule, "Grouping") == domain.FirewallGroup {
				names = append(names, strProp(rule, "Name"))
			}
			return true, nil
		})
		if err != nil {
			return err
		}

		for _, n := range names {
			if _, err := oleutil.CallMethod(rules, "Remove", n); err != nil {
				return fmt.Errorf("purging rule %q: %w", n, err)
			}
		}
		if len(names) > 0 {
			f.log.Info("reglas propias purgadas", "cantidad", len(names))
		}
		return nil
	})
}
