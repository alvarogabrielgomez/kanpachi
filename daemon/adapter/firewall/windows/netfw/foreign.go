package netfw

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/daemon/adapter/safewrite"
)

// This file is the part of the foreign-rule handling that has no COM in it, so
// it compiles and is tested on Linux. Everything decided here is decided the
// same way on every machine; the _windows file only reads properties and writes
// one boolean back.

// SuspendedRulesFile is the name of the record of somebody else's rules, inside
// the data directory.
//
// It lives in this file and not next to the struct that holds its path because
// that one carries the //go:build windows tag, and the test that covers the
// record's round trip does not. With the constant behind the tag, that test did
// not compile on Linux and the whole file's coverage was silently skipped
// there, which is the opposite of why this half exists.
const SuspendedRulesFile = "suspended-rules.json"

// liveRule is one rule as read from the store, before anything has been decided
// about it.
//
// The whole store is read into these and every decision is then taken over the
// slice, in this file, which is what puts the dangerous ones under the Linux CI
// instead of under a machine somebody has to remember to test on.
type liveRule struct {
	Name            string
	Group           string
	Application     string
	Direction       int32
	Action          int32
	Protocol        int32
	LocalPorts      string
	LocalAddresses  string
	RemoteAddresses string
	Interfaces      []string
	Profiles        int32
	Enabled         bool
}

// spec is this rule in the shape the desired set is written in, so the two can
// be compared field by field.
func (c liveRule) spec() ruleSpec {
	return ruleSpec{
		Name:            c.Name,
		Group:           c.Group,
		Direction:       c.Direction,
		Action:          c.Action,
		Protocol:        c.Protocol,
		LocalPorts:      c.LocalPorts,
		LocalAddresses:  c.LocalAddresses,
		RemoteAddresses: c.RemoteAddresses,
		Interfaces:      c.Interfaces,
		Profiles:        c.Profiles,
		Enabled:         c.Enabled,
	}
}

// roomGroup says whether the daemon may DELETE this rule.
//
// # This is not the same question as [liveRule.ours], and confusing them is a
// # security bug in either direction
//
// `ours` means Kanpachi owns it, in either group, and it answers "leave this
// alone" for the foreign-rule paths.
//
// `roomGroup` means the DAEMON wrote it, and it answers "this may be deleted".
// The quarantine group is ours and is NOT the daemon's: it belongs to the
// installer and it is the only thing protecting the machine while the service
// is stopped.
//
// Merging them one way lets a purge take the quarantine; the other way makes
// the audit report our own rules as somebody else's exposure.
func (c liveRule) roomGroup() bool { return c.Group == domain.FirewallGroup }

// ownSpecs are the rules the daemon wrote, by name.
func ownSpecs(all []liveRule) map[string]ruleSpec {
	out := make(map[string]ruleSpec)
	for _, c := range all {
		if c.roomGroup() {
			out[c.Name] = c.spec()
		}
	}
	return out
}

// foreignNames counts, per name, how many rules under it are NOT the daemon's.
//
// # Why this exists, measured the hard way
//
// INetFwPolicy2.Rules.Remove takes a NAME and Windows lets unrelated rules
// share one. Deleting a stale "easytier-core" rule by name on a real machine
// also took out a live rule carrying the same name, because the API offers no
// way to say which one. So a name may only be deleted when every rule under it
// is the daemon's.
//
// It counts what is NOT ours rather than what exists, and the difference cuts
// both ways: two rules of our own group sharing a name would block a deletion
// that is perfectly safe, and they would then be disabled forever and pile up;
// while a rule of the QUARANTINE group under a shared name has to block it, and
// a naive "count everything but the room group" would still catch that one
// correctly only because the quarantine is not the room group. That is the case
// that matters most, so it has a test.
func foreignNames(all []liveRule) map[string]int {
	out := make(map[string]int)
	for _, c := range all {
		if !c.roomGroup() {
			out[c.Name]++
		}
	}
	return out
}

// ours says whether the rule belongs to Kanpachi, in either group.
//
// Both groups are excluded from every foreign path, and the quarantine one is
// the reason this exists: it is the installer's, the only thing protecting the
// machine while the daemon is not running. Suspending a rule of that group
// would disarm the protection, and the caller asking for it would get success.
//
// The comparison lives in the domain and is not written here as an `||`,
// because the daemon must not NAME the quarantine group even to skip it. See
// [domain.IsOwnFirewallGroup].
func (c liveRule) ours() bool {
	return domain.IsOwnFirewallGroup(c.Group)
}

// foreign decides whether this live rule is worth reporting to the user.
//
// # What is filtered out, and why each one matters
//
// Ours, because Kanpachi's own rules are not foreign.
//
// Outbound, because an outbound permit lets the program reach out and lets
// nobody in. The exposure this audit is about is inbound.
//
// Blocks, because a block exposes nothing.
//
// DISABLED rules, and this one is not obvious. [domain.ForeignRule.Blocking]
// looks only at the class, so reporting a disabled Parsec rule would make the
// room permanently unopenable: the user would be asked to resolve something
// that is already resolved, with nothing left to turn off. A disabled rule
// opens no port, so it is not exposure.
func (c liveRule) foreign(gameExes []string) (domain.ForeignRule, bool) {
	if c.ours() || c.Direction != dirIn || c.Action != actionAllow || !c.Enabled {
		return domain.ForeignRule{}, false
	}
	class := domain.ClassifyForeignAgainst(c.Application, gameExes)
	// El alcance gana sobre el ejecutable, porque puede no haber ejecutable.
	//
	// Es la red permanente contra lo que motivó el fork: EasyTier escribía un
	// permiso de CUALQUIER protocolo sobre `kanpachi0`, sin puerto, sin origen y
	// sin aplicación. Por el camino del ejecutable no se clasifica, así que caía
	// en `ClassOther` y no se reportaba nunca. El fork lo quitó; esto es lo que
	// lo hace visible si alguien lo vuelve a poner, sea quien sea.
	if c.onOurAdapter() {
		class = domain.ClassOnOurAdapter
	}
	if class == domain.ClassOther {
		return domain.ForeignRule{}, false
	}
	return domain.ForeignRule{
		Name:       c.Name,
		Executable: c.Application,
		Profiles:   profilesOf(c.Profiles),
		Class:      class,
		WasEnabled: c.Enabled,
	}, true
}

// onOurAdapter says whether this rule is scoped to one of Kanpachi's virtual
// adapters.
//
// # Why prefix and not equality, here of all places
//
// Everywhere else in this codebase a group is compared by exact equality,
// because "Kanpachi" is a prefix of "Kanpachi-base" and a prefix match there
// deletes the quarantine. This is the opposite situation and the reasoning
// inverts with it: the adapters are named `kanpachi0` and `kanpachi1` today, a
// future one would be `kanpachi2`, and this decides what to REPORT, never what
// to delete. Reporting one adapter too many is a row on a screen; missing one is
// a permit nobody sees on a network the product claims to contain.
//
// Case folded because Windows hands the interface name back with whatever
// casing it was written with.
func (c liveRule) onOurAdapter() bool {
	for _, i := range c.Interfaces {
		if len(i) >= len(adapterPrefix) && strings.EqualFold(i[:len(adapterPrefix)], adapterPrefix) {
			return true
		}
	}
	return false
}

// adapterPrefix is what every Kanpachi virtual adapter's name starts with. It
// is derived from the domain constant rather than written out, so renaming the
// adapter cannot leave this silently looking for a name that no longer exists.
var adapterPrefix = strings.TrimRight(domain.AdapterName, "0123456789")

// matches says whether this live rule is the one the caller asked to suspend.
//
// Name AND executable, both of them, and that pair is the whole point.
// INetFwPolicy2.Rules.Remove takes a name and Windows lets unrelated rules
// share one, which is how a live rule got deleted on a real machine. Here the
// name alone would let a request to suspend "Parsec" turn off somebody else's
// rule that happens to carry that name.
//
// Case folded because a rule's ApplicationName comes back with whatever casing
// it was written with, and Windows paths do not care.
func (c liveRule) matches(r domain.ForeignRule) bool {
	if c.ours() {
		return false
	}
	return strings.EqualFold(c.Name, r.Name) && strings.EqualFold(c.Application, r.Executable)
}

// profilesOf turns the profile bitmask into the domain's list.
//
// NET_FW_PROFILE2_ALL is 0x7FFFFFFF and not 0x7, so the bits are tested one by
// one instead of comparing against a constant.
func profilesOf(mask int32) []domain.FirewallProfile {
	var out []domain.FirewallProfile
	if mask&profileDomain != 0 {
		out = append(out, domain.ProfileDomain)
	}
	if mask&profilePrivate != 0 {
		out = append(out, domain.ProfilePrivate)
	}
	if mask&profilePublic != 0 {
		out = append(out, domain.ProfilePublic)
	}
	return out
}

// sortForeign puts the dangerous ones first.
//
// The order is not cosmetic. The UI shows this list and the remote-control ones
// are BLOCKING: they have to be resolved before the room opens. Buried under
// twelve rules a game installer left, the one that hands over keyboard and
// files reads like one more row.
func sortForeign(rules []domain.ForeignRule) {
	sort.SliceStable(rules, func(i, j int) bool {
		a, b := rules[i], rules[j]
		if a.Blocking() != b.Blocking() {
			return a.Blocking()
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.Executable < b.Executable
	})
}

// fingerprint identifies one rule precisely enough to find it again after a
// reboot, which is what restoring after a dirty exit needs.
type fingerprint struct {
	Name        string `json:"name"`
	Application string `json:"application"`
	Direction   int32  `json:"direction"`
	Protocol    int32  `json:"protocol"`
	LocalPorts  string `json:"local_ports"`
	Profiles    int32  `json:"profiles"`
}

func (c liveRule) fingerprint() fingerprint {
	return fingerprint{
		Name:        c.Name,
		Application: c.Application,
		Direction:   c.Direction,
		Protocol:    c.Protocol,
		LocalPorts:  c.LocalPorts,
		Profiles:    c.Profiles,
	}
}

// key is the whole fingerprint flattened, for comparing and for ordering.
func (f fingerprint) key() string {
	return fmt.Sprintf("%s\x00%s\x00%d\x00%d\x00%s\x00%d",
		strings.ToLower(f.Name), strings.ToLower(f.Application),
		f.Direction, f.Protocol, f.LocalPorts, f.Profiles)
}

// loose is the pair that identifies the PROGRAM and the rule name, without the
// scope. Used only as a fallback when restoring; see [matchRecord].
func (f fingerprint) loose() string {
	return strings.ToLower(f.Name) + "\x00" + strings.ToLower(f.Application)
}

// suspendedRule is one line of suspended-rules.json.
type suspendedRule struct {
	Rule fingerprint `json:"rule"`
	// WasEnabled is what the rule was BEFORE Kanpachi touched it. It is written
	// to disk before anything is disabled, so a power cut in the middle leaves
	// the record and not the guesswork.
	WasEnabled bool `json:"was_enabled"`
}

const suspendedVersion = 1

type suspendedFile struct {
	Version int             `json:"version"`
	Rules   []suspendedRule `json:"rules"`
}

// matchRecord picks which live rule a saved record refers to.
//
// # Why there are two levels and not one
//
// An exact fingerprint match is what we want: it can only be the rule we
// disabled. It fails in one real case, which is the user editing that rule's
// ports or profiles while it was suspended. With only the exact match, that
// rule would stay off forever and the user would be told it came back.
//
// So there is a fallback on name plus executable, which is the same pair
// [liveRule.matches] uses. It cannot reach an unrelated program: a
// different rule of the SAME program with the SAME name is, for this purpose,
// the rule we disabled.
//
// It never falls back to the name alone. That is the ambiguity that deleted a
// live rule on a real machine.
func matchRecord(rec suspendedRule, live []liveRule) (int, bool) {
	want := rec.Rule.key()
	for i, c := range live {
		if c.ours() {
			continue
		}
		if c.fingerprint().key() == want {
			return i, true
		}
	}
	wantLoose := rec.Rule.loose()
	for i, c := range live {
		if c.ours() {
			continue
		}
		if c.fingerprint().loose() == wantLoose {
			return i, true
		}
	}
	return 0, false
}

// mergeSuspended adds records without ever overwriting an older one.
//
// The older one wins, and that is the whole reason this is not an append. A
// second SuspendForeign over a rule that is already suspended reads it as
// disabled, so writing that would record WasEnabled=false and restoring it
// would leave the user's rule off forever, with the record gone and nothing to
// show for it.
//
// Sorted by fingerprint so the file on disk is stable between runs.
func mergeSuspended(existing, fresh []suspendedRule) []suspendedRule {
	byKey := make(map[string]suspendedRule, len(existing)+len(fresh))
	for _, r := range existing {
		byKey[r.Rule.key()] = r
	}
	for _, r := range fresh {
		if _, already := byKey[r.Rule.key()]; already {
			continue
		}
		byKey[r.Rule.key()] = r
	}

	out := make([]suspendedRule, 0, len(byKey))
	for _, r := range byKey {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Rule.key() < out[j].Rule.key() })
	return out
}

func encodeSuspended(rules []suspendedRule) ([]byte, error) {
	raw, err := json.MarshalIndent(suspendedFile{Version: suspendedVersion, Rules: rules}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encoding the suspended rules: %w", err)
	}
	return append(raw, '\n'), nil
}

// decodeSuspended reads the file strictly.
//
// Unknown fields are refused rather than ignored: this file is the only record
// of what has to be turned back on, and a field this build does not understand
// means it was written by a build that knew something more. Restoring from a
// half-understood record is how a rule gets left in the wrong state.
func decodeSuspended(raw []byte) ([]suspendedRule, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()

	var f suspendedFile
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("reading the suspended rules: %w", err)
	}
	if f.Version != suspendedVersion {
		return nil, fmt.Errorf("suspended rules written by another version: %d, this build understands %d",
			f.Version, suspendedVersion)
	}
	return f.Rules, nil
}

// loadSuspended reads the file. Missing is NOT an error: it is what a clean
// install and every clean exit look like.
func loadSuspended(path string) ([]suspendedRule, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	return decodeSuspended(raw)
}

// saveSuspended writes the file atomically, and DELETES it when there is
// nothing left to remember.
//
// Deleting rather than writing an empty list matters at startup: an empty file
// and a missing file both mean "nothing suspended", and leaving an empty one
// around invites reading it as "the record was lost".
func saveSuspended(path string, rules []suspendedRule) error {
	if len(rules) == 0 {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("removing %s: %w", path, err)
		}
		return nil
	}
	raw, err := encodeSuspended(rules)
	if err != nil {
		return err
	}
	return safewrite.File(path, raw, 0o600)
}
