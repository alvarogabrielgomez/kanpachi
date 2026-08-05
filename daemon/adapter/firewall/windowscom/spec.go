// Package windowscom implements port.FirewallPort against INetFwPolicy2.
//
// It never shells out to netsh. Two reasons, and the second is the one that
// matters: netsh output is localised, so parsing it breaks on a Spanish or
// Portuguese Windows, and building netsh command lines from room data would put
// string interpolation between an attacker-influenced value and a process that
// runs as SYSTEM.
//
// This file is PURE Go with no build tags, so it compiles and is tested on the
// Linux CI job. Everything that decides WHAT a rule says lives here; the COM
// calls that put it there live in the _windows file. The split is deliberate:
// the interesting failures are in the translation, and a translation that can
// only be tested on a developer's Windows box is a translation nobody tests.
package windowscom

import (
	"fmt"
	"net/netip"
	"sort"
	"strings"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// Windows protocol numbers, as INetFwRule.Protocol wants them.
const (
	protoTCP = 6
	protoUDP = 17
	// protoICMPv4 is only used by the base quarantine, which the installer
	// writes. The daemon never emits it.
	protoICMPv4 = 1
)

// Profile bits of NET_FW_PROFILE_TYPE2. They are a mask, not an enum.
const (
	profileDomain  = 0x1
	profilePrivate = 0x2
	profilePublic  = 0x4
	// profileAll is what every Kanpachi rule carries. Not because the adapter
	// can land anywhere, which it also can, but because the product does not
	// depend on Windows classifying the network correctly. See decision 15.
	profileAll = profileDomain | profilePrivate | profilePublic
)

// Direction values of NET_FW_RULE_DIRECTION.
const (
	dirIn  = 1
	dirOut = 2
)

// Action values of NET_FW_ACTION.
const (
	actionBlock = 0
	actionAllow = 1
)

// ruleSpec is one Windows firewall rule, fully decided, ready to be written.
//
// It is a plain struct on purpose: the COM side does nothing but copy these
// fields onto an INetFwRule, so anything worth arguing about is decided here
// and asserted by a test that runs anywhere.
type ruleSpec struct {
	Name      string
	Group     string
	Direction int32
	Action    int32
	Protocol  int32
	// LocalPorts is empty for protocols that have no ports.
	LocalPorts string
	// LocalAddresses is the address of the virtual adapter.
	LocalAddresses string
	// RemoteAddresses is who may reach it. Never "*": the empty case means the
	// rule must not exist at all, and domain.RuleSet already refuses to express
	// "anyone".
	RemoteAddresses string
	// Interfaces scopes the rule to the virtual adapter by its friendly name.
	//
	// This is only set on PERMITS. On a block it would be a hole: when a scope
	// stops matching, a permit that stops applying closes, and a block that
	// stops applying opens. Same reasoning as core/domain/policy.go.
	Interfaces []string
	Profiles   int32
	Enabled    bool
}

// specFor translates one domain rule into the Windows shape.
//
// `adapter` is the friendly name of the ROOM's virtual adapter. Empty means "do
// not scope by interface", which is what the tests of the pure layer use and
// what a caller that cannot resolve the name yet must pass rather than inventing
// one.
//
// # The lobby rule goes on the LOBBY's adapter
//
// The host lives on two networks at once, and the door where people who are not
// members yet knock is on the second one. Pinning every permit to the room's
// adapter left that rule scoped to an interface its own local address does not
// live on, so it matched nothing: the door was shut at the Windows layer while
// everything reported green. Measured on a real room, where the rule read back
// `local=100.127.255.1 ifaces=kanpachi0`.
//
// Which adapter is read off the rule's LOCAL ADDRESS, exactly like the gate does
// it one layer up. An address inside [domain.RendezvousSubnet] can only be on
// the lobby adapter.
func specFor(r domain.FirewallRule, adapter string) (ruleSpec, error) {
	proto, err := protocolOf(r.Proto)
	if err != nil {
		return ruleSpec{}, fmt.Errorf("rule %q: %w", r.Name, err)
	}
	remote, err := remoteOf(r)
	if err != nil {
		return ruleSpec{}, fmt.Errorf("rule %q: %w", r.Name, err)
	}
	if !r.Local.IsValid() {
		return ruleSpec{}, fmt.Errorf("rule %q: no local address, so it would apply to every address on the machine", r.Name)
	}

	s := ruleSpec{
		Name:            r.Name,
		Group:           domain.FirewallGroup,
		Direction:       dirIn,
		Action:          actionAllow,
		Protocol:        proto,
		LocalPorts:      portsOf(r),
		LocalAddresses:  r.Local.String(),
		RemoteAddresses: remote,
		Profiles:        profileAll,
		Enabled:         true,
	}
	if adapter != "" {
		if domain.RendezvousSubnet.Contains(r.Local) {
			s.Interfaces = []string{domain.LobbyAdapterName}
		} else {
			s.Interfaces = []string{adapter}
		}
	}
	return s, nil
}

// SpecsFor translates a whole desired set.
//
// It fails on the FIRST bad rule instead of skipping it. A set that is applied
// with one rule quietly dropped is worse than one that is not applied at all:
// the caller believes the room is configured and one player cannot connect,
// with nothing on screen to explain it.
func SpecsFor(rs domain.RuleSet, adapter string) ([]ruleSpec, error) {
	out := make([]ruleSpec, 0, len(rs.Rules))
	for _, r := range rs.Rules {
		s, err := specFor(r, adapter)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

func protocolOf(p domain.Proto) (int32, error) {
	switch p {
	case domain.ProtoTCP:
		return protoTCP, nil
	case domain.ProtoUDP:
		return protoUDP, nil
	case domain.ProtoBoth:
		// BuildRuleSet expands this into two rules, so reaching here means the
		// expansion was skipped and the rule would silently cover one protocol.
		return 0, fmt.Errorf("proto both never reaches the firewall: it must be expanded into two rules")
	default:
		return 0, fmt.Errorf("unknown protocol %v", p)
	}
}

// portsOf renders the range the way Windows wants it: a single port on its own,
// a range with a dash.
func portsOf(r domain.FirewallRule) string { return portRange(r.From, r.To) }

// remoteOf renders who may reach the rule.
//
// Addresses and prefixes both land in the same comma separated list, which is
// what INetFwRule.RemoteAddresses takes. The order is sorted so that two
// identical desired sets produce identical strings: the diff compares what is
// live against what is wanted, and an unstable string would rewrite the
// firewall on every heartbeat.
func remoteOf(r domain.FirewallRule) (string, error) {
	parts := make([]string, 0, len(r.Remote)+len(r.Nets))
	for _, a := range r.Remote {
		if !a.IsValid() {
			return "", fmt.Errorf("an invalid member address would widen the rule to everyone")
		}
		parts = append(parts, a.String())
	}
	for _, n := range r.Nets {
		if !n.IsValid() {
			return "", fmt.Errorf("an invalid prefix would widen the rule to everyone")
		}
		parts = append(parts, n.String())
	}
	if len(parts) == 0 {
		// Windows reads an empty RemoteAddresses as "*". The domain says an
		// empty scope means the rule must not exist, so producing it here would
		// invert the meaning into the worst possible one.
		return "", fmt.Errorf("empty remote scope: Windows would read that as anyone")
	}
	sort.Strings(parts)
	return strings.Join(parts, ","), nil
}

// normalizeAddresses puts an address list in the shape Windows gives it back.
//
// # Why this is not cosmetic
//
// Windows does not store what you write. It stores an equivalent, and hands the
// equivalent back on the next read. Measured on a real machine:
//
//	written "10.99.7.1"       read back "10.99.7.1/255.255.255.255"
//	written "10.99.7.0/24"    read back "10.99.7.0/255.255.255.0"
//	written ""                read back "*"
//
// Comparing the two strings raw makes EVERY rule look altered, forever, and the
// two consequences are both real. The base quarantine logged a drift warning for
// all 48 of its rules on every start, which is the surest way to guarantee
// nobody reads the log. And `Apply` retires and rewrites every rule on every
// heartbeat: the code goes out of its way to avoid a rule being half old and
// half new precisely because that window is on the wire, and this reopened it on
// a timer.
//
// The list order is sorted for the same reason `remoteOf` sorts: Windows is free
// to hand the members back in another order.
func normalizeAddresses(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || s == "*" {
		// The two spellings of "any". Kanpachi writes the empty one and Windows
		// reads back the star.
		return "*"
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, normalizeAddress(strings.TrimSpace(p)))
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}

// normalizeAddress renders one entry as address plus DOTTED mask, which is what
// Windows returns.
//
// Anything it cannot parse comes back untouched. Windows accepts shapes this
// does not model, ranges among them, and turning one of those into a guess would
// make two different scopes compare equal, which is worse than reporting a
// difference that is not one.
func normalizeAddress(s string) string {
	if a, err := netip.ParseAddr(s); err == nil {
		if a.Is4() {
			return s + "/255.255.255.255"
		}
		return s
	}
	p, err := netip.ParsePrefix(s)
	if err != nil || !p.Addr().Is4() {
		return s
	}
	var mask [4]byte
	for i := 0; i < p.Bits(); i++ {
		mask[i/8] |= 1 << (7 - i%8)
	}
	return fmt.Sprintf("%s/%d.%d.%d.%d", p.Addr(), mask[0], mask[1], mask[2], mask[3])
}

// sameScope says whether a live rule still matches what was asked for.
//
// Only the fields Kanpachi sets are compared. Windows fills in a pile of others
// with defaults, and demanding equality on those would report every rule as
// altered forever.
//
// The addresses go through [normalizeAddresses] because Windows stores an
// equivalent of what it was given, not the same string.
func (s ruleSpec) sameScope(other ruleSpec) bool {
	if s.Name != other.Name || s.Protocol != other.Protocol ||
		s.LocalPorts != other.LocalPorts ||
		normalizeAddresses(s.LocalAddresses) != normalizeAddresses(other.LocalAddresses) ||
		normalizeAddresses(s.RemoteAddresses) != normalizeAddresses(other.RemoteAddresses) ||
		s.Action != other.Action ||
		s.Direction != other.Direction || s.Profiles != other.Profiles ||
		s.Enabled != other.Enabled {
		return false
	}
	if len(s.Interfaces) != len(other.Interfaces) {
		return false
	}
	for i := range s.Interfaces {
		if !strings.EqualFold(s.Interfaces[i], other.Interfaces[i]) {
			return false
		}
	}
	return true
}

// unused keeps the ICMP constant honest until the installer spec lands.
var _ = protoICMPv4

// unusedAddr keeps the import honest.
var _ = netip.Addr{}
