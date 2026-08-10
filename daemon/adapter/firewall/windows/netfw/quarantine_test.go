package netfw

import (
	"reflect"
	"strings"
	"testing"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// The base quarantine, translated.
//
// What is asserted here is the ASYMMETRY with a permit. A permit and a block are
// not mirror images, and every field where they differ is a field where copying
// the permit path would open a hole.

func TestQuarantineRulesAreBlocksInTheBaseGroup(t *testing.T) {
	specs, err := QuarantineSpecs(domain.BaseQuarantine())
	if err != nil {
		t.Fatalf("translating the quarantine: %v", err)
	}
	if len(specs) == 0 {
		t.Fatal("the quarantine translated to nothing")
	}

	for _, s := range specs {
		if s.Action != actionBlock {
			t.Errorf("rule %q is not a block. Every rule of this group is a block by "+
				"construction of domain.QuarantineRule, so an allow here means the "+
				"translation invented one", s.Name)
		}
		if s.Group != domain.FirewallGroupBase {
			t.Errorf("rule %q carries group %q. In the room group it would be wiped by "+
				"the startup purge, and the failure would be invisible: everything "+
				"keeps working, only unprotected", s.Name, s.Group)
		}
		if !s.Enabled {
			t.Errorf("rule %q is written disabled, which opens the port it exists to shut", s.Name)
		}
		if s.Profiles != profileAll {
			t.Errorf("rule %q is not in all three profiles. The product does not depend "+
				"on Windows classifying the network correctly", s.Name)
		}
	}
}

// A block that is scoped is a hole with a schedule.
//
// Scoping a PERMIT to the virtual adapter is correct and specFor does it: a
// permit that stops matching CLOSES. A block that stops matching OPENS, and the
// virtual adapter only exists while a room is open, so a quarantine scoped to it
// would protect nothing for most of the machine's life.
func TestQuarantineRulesAreNotScoped(t *testing.T) {
	specs, err := QuarantineSpecs(domain.BaseQuarantine())
	if err != nil {
		t.Fatalf("translating the quarantine: %v", err)
	}

	for _, s := range specs {
		if s.LocalAddresses != "" {
			t.Errorf("rule %q is scoped to local address %q. A block that stops "+
				"matching opens", s.Name, s.LocalAddresses)
		}
		if len(s.Interfaces) != 0 {
			t.Errorf("rule %q is scoped to interface %v. The virtual adapter only "+
				"exists while a room is open", s.Name, s.Interfaces)
		}
		// Empty RemoteAddresses is what Windows reads as "*". specFor REFUSES to
		// produce it, because on a permit it is the worst possible meaning. Here
		// it is the only correct one: "nobody reaches my SMB" is not a statement
		// about who.
		if s.RemoteAddresses != "" {
			t.Errorf("rule %q limits who it blocks, to %q. The quarantine is against "+
				"anyone", s.Name, s.RemoteAddresses)
		}
	}
}

// THE ONE THAT KEEPS THE QUARANTINE FROM BREAKING THE MACHINE.
//
// The port is the LOCAL one in both directions, so these rules shut this PC's
// own services. They do NOT stop it from being a client: mounting a network
// drive, RDP-ing out, or git over SSH all leave from an ephemeral local port
// towards the OTHER machine's 445, 3389 or 22.
//
// Blocking by REMOTE port would break all three, permanently and with no visible
// cause, because the quarantine outlives the daemon. ruleSpec has no
// RemotePorts field at all, which is why this is asserted on the type.
func TestQuarantineBlocksTheLocalPortAndNeverTheRemoteOne(t *testing.T) {
	specs, err := QuarantineSpecs(domain.BaseQuarantine())
	if err != nil {
		t.Fatalf("translating the quarantine: %v", err)
	}

	for _, s := range specs {
		if s.LocalPorts == "" {
			t.Fatalf("rule %q has no local port, so it would block the whole protocol", s.Name)
		}
	}

	// The structural half. A RemotePorts field appearing on ruleSpec is the
	// change that would let someone write the version that breaks the machine.
	tipo := reflect.TypeOf(ruleSpec{})
	for i := 0; i < tipo.NumField(); i++ {
		if strings.EqualFold(tipo.Field(i).Name, "RemotePorts") {
			t.Error("ruleSpec grew a RemotePorts field. On the quarantine that would " +
				"stop this PC from REACHING anyone else's 445, 3389 or 22, for good, " +
				"because these rules outlive the daemon")
		}
	}
}

// Both directions, and neither is decoration.
//
// Inbound is the protection: nobody reaches my file sharing. Outbound shuts the
// same service on the other side, so it cannot answer or call out from that
// port either.
func TestQuarantineCoversBothDirections(t *testing.T) {
	specs, err := QuarantineSpecs(domain.BaseQuarantine())
	if err != nil {
		t.Fatalf("translating the quarantine: %v", err)
	}

	seen := map[string]map[int32]bool{}
	for _, s := range specs {
		key := s.LocalPorts + "/" + itoa(s.Protocol)
		if seen[key] == nil {
			seen[key] = map[int32]bool{}
		}
		seen[key][s.Direction] = true
	}

	for key, dirs := range seen {
		if !dirs[dirIn] || !dirs[dirOut] {
			t.Errorf("%s is not blocked in both directions: in=%v out=%v",
				key, dirs[dirIn], dirs[dirOut])
		}
	}
}

// A bad rule fails the whole translation rather than being skipped.
//
// A quarantine applied with one rule quietly dropped is worse than one that
// failed outright: the daemon would report the machine protected with a hole in
// it, and nothing on screen would say which port.
func TestOneBadQuarantineRuleFailsTheWholeTranslation(t *testing.T) {
	bad := []domain.QuarantineRule{
		{Name: "fine", Proto: domain.ProtoTCP, From: 445, To: 445, In: true},
		{Name: "no port", Proto: domain.ProtoTCP, From: 0, To: 0, In: true},
	}
	if _, err := QuarantineSpecs(bad); err == nil {
		t.Fatal("a rule with no port translated fine. It would be dropped and the " +
			"quarantine would report success with a hole in it")
	}

	both := []domain.QuarantineRule{{Name: "both", Proto: domain.ProtoBoth, From: 445, To: 445}}
	if _, err := QuarantineSpecs(both); err == nil {
		t.Fatal("proto both translated fine. A Windows rule carries one protocol, so " +
			"it would silently cover only one of the two")
	}
}

func itoa(n int32) string {
	if n == protoTCP {
		return "tcp"
	}
	return "udp"
}
