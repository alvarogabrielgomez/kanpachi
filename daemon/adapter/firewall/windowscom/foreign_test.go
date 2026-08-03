package windowscom

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/accentiostudios/kanpachi/core/domain"
)

func liveParsec() liveRule {
	return liveRule{
		Name:        "Parsec",
		Group:       "",
		Application: `C:\Program Files\Parsec\parsecd.exe`,
		Direction:   dirIn,
		Action:      actionAllow,
		Protocol:    protoUDP,
		LocalPorts:  "9000-9100",
		Profiles:    profileAll,
		Enabled:     true,
	}
}

var gameExes = []string{`D:\Steam\steamapps\common\Zomboid\zomboid.exe`}

func TestRemoteControlIsReportedAndIsBlocking(t *testing.T) {
	fr, ok := liveParsec().foreign(gameExes)
	if !ok {
		t.Fatal("a live inbound permit for Parsec was not reported")
	}
	if fr.Class != domain.ClassRemoteControl {
		t.Errorf("class = %v, want remote control", fr.Class)
	}
	if !fr.Blocking() {
		t.Error("a remote-control rule came back non blocking, so the room would " +
			"open with the host's keyboard and files reachable")
	}
	if len(fr.Profiles) != 3 {
		t.Errorf("profiles = %v, want the three: NET_FW_PROFILE2_ALL is 0x7FFFFFFF "+
			"and not 0x7, so the mask has to be tested bit by bit", fr.Profiles)
	}
}

func TestADisabledRuleIsNotExposure(t *testing.T) {
	// This one is not obvious and it is the reason it has a test.
	// domain.ForeignRule.Blocking looks only at the class, so reporting a
	// disabled Parsec rule would make the room permanently unopenable: the user
	// would be asked to turn off something that is already off.
	c := liveParsec()
	c.Enabled = false

	if _, ok := c.foreign(gameExes); ok {
		t.Fatal("a disabled rule was reported as exposure")
	}
}

func TestAnOutboundPermitIsNotExposure(t *testing.T) {
	c := liveParsec()
	c.Direction = dirOut

	if _, ok := c.foreign(gameExes); ok {
		t.Fatal("an outbound permit was reported: it lets the program reach out " +
			"and lets nobody in")
	}
}

func TestABlockIsNotExposure(t *testing.T) {
	c := liveParsec()
	c.Action = actionBlock

	if _, ok := c.foreign(gameExes); ok {
		t.Fatal("a block rule was reported as exposure")
	}
}

func TestOurOwnRulesAreNeverForeign(t *testing.T) {
	for _, group := range []string{domain.FirewallGroup, domain.FirewallGroupBase} {
		c := liveParsec()
		c.Group = group
		if _, ok := c.foreign(gameExes); ok {
			t.Errorf("a rule of group %q was reported as foreign", group)
		}
		if c.matches(domain.ForeignRule{Name: c.Name, Executable: c.Application}) {
			t.Errorf("a rule of group %q matched a suspend request; disabling the "+
				"base group would disarm the installer's quarantine", group)
		}
	}
}

func TestTheGameIsReportedButDoesNotBlock(t *testing.T) {
	c := liveParsec()
	c.Name = "Project Zomboid"
	c.Application = gameExes[0]

	fr, ok := c.foreign(gameExes)
	if !ok {
		t.Fatal("a permissive rule left by the game installer was not reported")
	}
	if fr.Class != domain.ClassGame {
		t.Errorf("class = %v, want game", fr.Class)
	}
	if fr.Blocking() {
		t.Error("a rule for the game blocked the room from opening")
	}
}

func TestAnUnrelatedProgramIsNotReported(t *testing.T) {
	c := liveParsec()
	c.Name = "Spotify"
	c.Application = `C:\Users\a\AppData\Roaming\Spotify\spotify.exe`

	if _, ok := c.foreign(gameExes); ok {
		t.Fatal("an unrelated program was reported, which would drown the list " +
			"the user has to read")
	}
}

func TestThePathShapesThatBrokeTheOtherHeuristic(t *testing.T) {
	// Measured on a real machine while cleaning up 1027 rules: deciding by "does
	// the file exist" gives false positives on rules whose ApplicationName is
	// literally "System", and on paths carrying environment variables that are
	// only expanded by the firewall service. Matching by BASENAME is immune to
	// both, and this test is what says so.
	cases := []struct {
		exe  string
		want domain.RuleClass
	}{
		{`%ProgramFiles%\Parsec\parsecd.exe`, domain.ClassRemoteControl},
		{`%SystemRoot%\system32\svchost.exe`, domain.ClassOther},
		{"System", domain.ClassOther},
		{"", domain.ClassOther},
		{`C:/Program Files/Parsec/PARSECD.EXE`, domain.ClassRemoteControl},
	}
	for _, c := range cases {
		cand := liveParsec()
		cand.Application = c.exe
		got := domain.ClassifyForeignAgainst(c.exe, gameExes)
		if got != c.want {
			t.Errorf("%q classified as %v, want %v", c.exe, got, c.want)
		}
	}
}

func TestSuspendNeedsTheNameAndTheProgram(t *testing.T) {
	// The measured trap: Rules.Remove takes a NAME and Windows lets unrelated
	// rules share one, which is how a live rule got deleted on a real machine.
	// Matching a suspend request by name alone repeats it with Enabled.
	c := liveParsec()

	same := domain.ForeignRule{Name: "Parsec", Executable: c.Application}
	if !c.matches(same) {
		t.Fatal("the rule did not match its own name and program")
	}

	impostor := domain.ForeignRule{Name: "Parsec", Executable: `C:\evil\other.exe`}
	if c.matches(impostor) {
		t.Error("a request naming a different program matched, so suspending would " +
			"turn off somebody else's rule that happens to share the name")
	}

	cased := domain.ForeignRule{Name: "parsec", Executable: strings.ToUpper(c.Application)}
	if !c.matches(cased) {
		t.Error("a casing difference stopped the match, and Windows hands paths " +
			"back with whatever casing they were written with")
	}
}

func TestMergeKeepsTheOlderRecord(t *testing.T) {
	// The second suspend over an already suspended rule reads it as disabled.
	// Letting that overwrite would record WasEnabled=false, and restoring would
	// leave the user's rule off forever with the record gone.
	fp := liveParsec().fingerprint()
	old := []suspendedRule{{Rule: fp, WasEnabled: true}}
	newer := []suspendedRule{{Rule: fp, WasEnabled: false}}

	got := mergeSuspended(old, newer)
	if len(got) != 1 {
		t.Fatalf("merge produced %d records, want 1", len(got))
	}
	if !got[0].WasEnabled {
		t.Error("the newer record won, so the rule would never come back on")
	}
}

func TestMergeIsStableOnDisk(t *testing.T) {
	a := liveParsec().fingerprint()
	b := liveParsec()
	b.Name = "AnyDesk"
	b.Application = `C:\Program Files\AnyDesk\anydesk.exe`

	one := mergeSuspended(nil, []suspendedRule{{Rule: a}, {Rule: b.fingerprint()}})
	two := mergeSuspended(nil, []suspendedRule{{Rule: b.fingerprint()}, {Rule: a}})

	if one[0].Rule.key() != two[0].Rule.key() || one[1].Rule.key() != two[1].Rule.key() {
		t.Error("the order depends on the input, so the file would churn between runs")
	}
}

func TestMatchRecordFallsBackToTheProgramButNeverToTheNameAlone(t *testing.T) {
	rec := suspendedRule{Rule: liveParsec().fingerprint(), WasEnabled: true}

	exact := liveParsec()
	if i, ok := matchRecord(rec, []liveRule{exact}); !ok || i != 0 {
		t.Fatal("the exact fingerprint did not match itself")
	}

	// Edited while suspended: same rule, different ports. Without the fallback
	// it would stay off forever and the user would be told it came back.
	edited := liveParsec()
	edited.LocalPorts = "9000-9200"
	if _, ok := matchRecord(rec, []liveRule{edited}); !ok {
		t.Error("a rule edited while suspended stopped matching, so it would " +
			"never be restored")
	}

	// Same name, different program. This must NOT match at either level.
	impostor := liveParsec()
	impostor.Application = `C:\evil\other.exe`
	if _, ok := matchRecord(rec, []liveRule{impostor}); ok {
		t.Error("the fallback reached a different program, which is the ambiguity " +
			"that deleted a live rule on a real machine")
	}

	// Ours is skipped even if it somehow carried the same fingerprint.
	mine := liveParsec()
	mine.Group = domain.FirewallGroupBase
	if _, ok := matchRecord(rec, []liveRule{mine}); ok {
		t.Error("a rule of the base quarantine was picked as a restore target")
	}
}

func TestTheRecordFileRoundTrips(t *testing.T) {
	want := []suspendedRule{{Rule: liveParsec().fingerprint(), WasEnabled: true}}

	raw, err := encodeSuspended(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeSuspended(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Rule.key() != want[0].Rule.key() || !got[0].WasEnabled {
		t.Fatalf("round trip lost data: %+v", got)
	}
}

func TestTheRecordFileIsReadStrictly(t *testing.T) {
	// This file is the only record of what has to be turned back on. A field
	// this build does not understand means it was written by one that knew more,
	// and restoring from a half-understood record leaves rules in the wrong state.
	if _, err := decodeSuspended([]byte(`{"version":1,"rules":[],"extra":true}`)); err == nil {
		t.Error("an unknown field was accepted")
	}
	if _, err := decodeSuspended([]byte(`{"version":2,"rules":[]}`)); err == nil {
		t.Error("a record from another version was accepted")
	}
}

func TestSavingNothingRemovesTheFile(t *testing.T) {
	// An empty file and a missing file both mean "nothing suspended". Leaving an
	// empty one invites reading it at startup as "the record was lost".
	path := filepath.Join(t.TempDir(), SuspendedRulesFile)

	if err := saveSuspended(path, []suspendedRule{{Rule: liveParsec().fingerprint(), WasEnabled: true}}); err != nil {
		t.Fatal(err)
	}
	got, err := loadSuspended(path)
	if err != nil || len(got) != 1 {
		t.Fatalf("load after save gave %v, %v", got, err)
	}

	if err := saveSuspended(path, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("the file survived being cleared: %v", err)
	}

	// And a missing file is not an error: it is what every clean exit looks like.
	got, err = loadSuspended(path)
	if err != nil {
		t.Errorf("a missing record file was reported as an error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("a missing record file produced %d records", len(got))
	}
}

// named builds a live rule with a given name and group, which is all the
// removal-safety decision looks at.
func named(name, group string) liveRule {
	r := liveParsec()
	r.Name = name
	r.Group = group
	return r
}

func TestTheQuarantineIsOursButNotTheDaemonsToDelete(t *testing.T) {
	// Two questions that look like one, and merging them is a security bug in
	// either direction. Kanpachi OWNS the quarantine, so the foreign-rule paths
	// leave it alone; the DAEMON did not write it, so the purge must not delete
	// it. It is the only thing protecting the machine with the service stopped.
	base := named("whatever", domain.FirewallGroupBase)
	if !base.ours() {
		t.Error("the quarantine came back as somebody else's rule, so the audit " +
			"would report it as exposure and offer to disable it")
	}
	if base.roomGroup() {
		t.Error("the quarantine came back as the daemon's, so a purge would delete " +
			"it and every service restart would disarm the protection")
	}

	room := named("whatever", domain.FirewallGroup)
	if !room.ours() || !room.roomGroup() {
		t.Error("a rule the daemon wrote came back as neither ours nor the daemon's")
	}
}

func TestWhenDeletingByNameIsSafe(t *testing.T) {
	// The measured accident: Rules.Remove takes a NAME, Windows lets unrelated
	// rules share one, and deleting a stale "easytier-core" rule by name took
	// out a live rule that happened to carry it. A name may only be deleted
	// when every rule under it is the daemon's.
	cases := []struct {
		nombre string
		store  []liveRule
		others int
		why    string
	}{
		{
			nombre: "only ours",
			store:  []liveRule{named("kanpachi-udp-16261", domain.FirewallGroup)},
			others: 0,
		},
		{
			nombre: "two of ours under one name",
			store: []liveRule{
				named("kanpachi-udp-16261", domain.FirewallGroup),
				named("kanpachi-udp-16261", domain.FirewallGroup),
			},
			others: 0,
			why: "both are ours to delete; counting the whole store instead would " +
				"refuse, disable them, and let them pile up forever",
		},
		{
			nombre: "the quarantine carries the same name",
			store: []liveRule{
				named("kanpachi-udp-16261", domain.FirewallGroup),
				named("kanpachi-udp-16261", domain.FirewallGroupBase),
			},
			others: 1,
			why:    "deleting by that name would take the installer's quarantine with it",
		},
		{
			nombre: "somebody else carries the same name",
			store: []liveRule{
				named("Parsec", domain.FirewallGroup),
				named("Parsec", ""),
			},
			others: 1,
			why:    "this is the exact shape that deleted a live rule on a real machine",
		},
	}

	for _, c := range cases {
		t.Run(c.nombre, func(t *testing.T) {
			got := foreignNames(c.store)[c.store[0].Name]
			if got != c.others {
				t.Errorf("foreignNames = %d, want %d. %s", got, c.others, c.why)
			}
		})
	}
}

func TestOwnSpecsTakesOnlyWhatTheDaemonWrote(t *testing.T) {
	store := []liveRule{
		named("kanpachi-udp-16261", domain.FirewallGroup),
		named("Kanpachi-base: bloqueo 3389", domain.FirewallGroupBase),
		named("Parsec", ""),
	}

	own := ownSpecs(store)
	if len(own) != 1 {
		t.Fatalf("ownSpecs took %d rules, want only the daemon's: %v", len(own), own)
	}
	if _, ok := own["kanpachi-udp-16261"]; !ok {
		t.Error("the daemon's own rule is not in the set it diffs against, so Apply " +
			"would write a second copy of it on every heartbeat")
	}
}

func TestSortPutsTheDangerousOnesFirst(t *testing.T) {
	rules := []domain.ForeignRule{
		{Name: "Zomboid", Class: domain.ClassGame},
		{Name: "AnyDesk", Class: domain.ClassRemoteControl},
		{Name: "Alpha", Class: domain.ClassGame},
	}
	sortForeign(rules)

	if !rules[0].Blocking() {
		t.Fatalf("the list starts with %q, and the blocking one has to be first: "+
			"buried under the game's rules it reads like one more row", rules[0].Name)
	}
	if rules[1].Name != "Alpha" || rules[2].Name != "Zomboid" {
		t.Errorf("the rest is not sorted by name: %v", rules)
	}
}
