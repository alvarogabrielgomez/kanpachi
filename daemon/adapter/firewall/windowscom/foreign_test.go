package windowscom

import (
	"net/netip"
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

func TestTheAuditMeasuresOnlyWhatTheDaemonWrote(t *testing.T) {
	// The quarantine must NOT be in here. The diff compares this against the
	// active room's desired set, and the quarantine is in neither, so every one
	// of its rules would come back as Extra: the screen would tell the user
	// somebody had tampered with their firewall, on every single sweep.
	store := []liveRule{
		named("kanpachi-udp-16261", domain.FirewallGroup),
		named("Kanpachi-base: bloqueo 445", domain.FirewallGroupBase),
		named("Parsec", ""),
	}

	got := appliedRules(store)
	if len(got) != 1 {
		t.Fatalf("measured %d rules, want only the daemon's: %+v", len(got), got)
	}
	if got[0].Name != "kanpachi-udp-16261" || got[0].Layer != domain.LayerFirewallRules {
		t.Errorf("measured %+v", got[0])
	}

	// And the whole point of measuring instead of judging: run it through the
	// domain's own diff and it has to come out intact.
	var want domain.RuleSet
	want.Add(domain.FirewallRule{
		Name:   "kanpachi-udp-16261",
		Proto:  domain.ProtoUDP,
		From:   16261,
		To:     16261,
		Local:  addr("100.64.1.1"),
		Remote: []netip.Addr{addr("100.64.1.5")},
	})
	if d := (domain.Enforcement{Rules: got}).Diff(want, false); !d.Intact() {
		t.Errorf("the measurement of an untouched machine came back altered: %+v", d)
	}
}

func TestADisabledOwnRuleIsMeasuredAsDisabled(t *testing.T) {
	// Missing and disabled both leave the port shut, and only one of them means
	// somebody was there. The domain needs to be able to tell them apart, so the
	// measurement has to carry it.
	off := named("kanpachi-udp-16261", domain.FirewallGroup)
	off.Enabled = false

	got := appliedRules([]liveRule{off})
	if len(got) != 1 || got[0].Enabled {
		t.Fatalf("a disabled rule was measured as %+v", got)
	}
}

func TestEveryProfileHasAMask(t *testing.T) {
	// Asking Windows about the wrong profile still answers, and the answer looks
	// fine, so a missing case here would be silent.
	seen := map[int32]bool{}
	for _, p := range domain.AllFirewallProfiles() {
		mask, err := profileMask(p)
		if err != nil {
			t.Fatalf("%v has no mask: %v", p, err)
		}
		if seen[mask] {
			t.Errorf("%v shares a mask with another profile", p)
		}
		seen[mask] = true
	}
	if _, err := profileMask(domain.FirewallProfile(0)); err == nil {
		t.Error("an invalid profile got a mask instead of an error")
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

// Un permiso ajeno SOBRE NUESTRO ADAPTADOR se reporta, tenga ejecutable o no.
//
// La regla de este test es la que EasyTier escribía de verdad al crear el
// adaptador, copiada de una máquina: cualquier protocolo sobre `kanpachi0`, sin
// puerto, sin origen y sin aplicación. Por el camino del ejecutable no se
// clasifica, así que caía en `ClassOther` y no se reportaba NUNCA.
//
// El fork la quitó de raíz. Esto es la red permanente: si alguien la vuelve a
// poner, sea quien sea, la pantalla de exposición lo dice. Y bloquea, porque un
// permiso ajeno sobre la red virtual deshace la promesa central en la misma capa
// que Kanpachi usa para conceder, y la compuerta no lo tapa: los dos son
// permisos, así que conviven.
func TestUnPermisoAjenoSobreNuestroAdaptadorSeVe(t *testing.T) {
	regla := liveRule{
		Name:      "EasyTier kanpachi0 - ALL Protocol (Inbound)",
		Group:     "EasyTier",
		Direction: dirIn,
		Action:    actionAllow,
		// Sin aplicación, sin puertos y sin origen: el caso real.
		Interfaces: []string{"kanpachi0"},
		Profiles:   profileAll,
		Enabled:    true,
	}

	got, ok := regla.foreign(nil)
	if !ok {
		t.Fatal("un permiso de cualquier protocolo sobre kanpachi0 no se reportó")
	}
	if got.Class != domain.ClassOnOurAdapter {
		t.Errorf("clase = %v, y lo que la hace peligrosa es DÓNDE está", got.Class)
	}
	if !got.Blocking() {
		t.Error("no bloquea, y la sala se abriría con el permiso ajeno puesto sobre la red virtual")
	}

	// El vestíbulo cuenta igual, y una mayúscula no lo esconde: Windows devuelve
	// el nombre de la interfaz con la caja con que se escribió.
	regla.Interfaces = []string{"KANPACHI1"}
	if got, ok := regla.foreign(nil); !ok || got.Class != domain.ClassOnOurAdapter {
		t.Error("un permiso sobre el vestíbulo, escrito en mayúsculas, no se reportó")
	}

	// Y una regla acotada a OTRO adaptador no es asunto nuestro: reportarla
	// convertiría la pantalla en una lista del firewall entero del usuario.
	regla.Interfaces = []string{"Ethernet"}
	if _, ok := regla.foreign(nil); ok {
		t.Error("se reportó un permiso sobre un adaptador que no es de Kanpachi")
	}
}

// Y las nuestras siguen sin ser ajenas, que es lo primero que rompería este
// cambio: todas las reglas de Kanpachi van acotadas a `kanpachi*`.
func TestNuestrasReglasSobreNuestroAdaptadorNoSonAjenas(t *testing.T) {
	for _, grupo := range []string{domain.FirewallGroup, domain.FirewallGroupBase} {
		regla := liveRule{
			Name:       grupo + ": una regla nuestra",
			Group:      grupo,
			Direction:  dirIn,
			Action:     actionAllow,
			Interfaces: []string{domain.AdapterName},
			Profiles:   profileAll,
			Enabled:    true,
		}
		if _, ok := regla.foreign(nil); ok {
			t.Errorf("una regla del grupo %q se reportó como ajena, y la pantalla de "+
				"exposición diría que Kanpachi se expone a sí mismo", grupo)
		}
	}
}
