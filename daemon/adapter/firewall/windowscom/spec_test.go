package windowscom

import (
	"net/netip"
	"strings"
	"testing"

	"github.com/accentiostudios/kanpachi/core/domain"
)

func addr(s string) netip.Addr { return netip.MustParseAddr(s) }

func baseRule() domain.FirewallRule {
	return domain.FirewallRule{
		Name:   "kanpachi-udp-16261",
		Proto:  domain.ProtoUDP,
		From:   16261,
		To:     16261,
		Local:  addr("100.64.1.1"),
		Remote: []netip.Addr{addr("100.64.1.5")},
	}
}

func TestASingleRuleTranslatesWithEveryScopeSet(t *testing.T) {
	s, err := specFor(baseRule(), "kanpachi0")
	if err != nil {
		t.Fatal(err)
	}

	if s.Protocol != protoUDP {
		t.Errorf("protocol = %d, want %d", s.Protocol, protoUDP)
	}
	if s.LocalPorts != "16261" {
		t.Errorf("ports = %q, want a bare port with no dash", s.LocalPorts)
	}
	if s.LocalAddresses != "100.64.1.1" {
		t.Errorf("local = %q", s.LocalAddresses)
	}
	if s.RemoteAddresses != "100.64.1.5" {
		t.Errorf("remote = %q", s.RemoteAddresses)
	}
	if len(s.Interfaces) != 1 || s.Interfaces[0] != "kanpachi0" {
		t.Errorf("interfaces = %v, want the virtual adapter only", s.Interfaces)
	}
	if s.Direction != dirIn || s.Action != actionAllow {
		t.Errorf("dir=%d action=%d, want inbound allow", s.Direction, s.Action)
	}
	if s.Profiles != profileAll {
		t.Errorf("profiles = %d, want all three: the product does not rely on "+
			"Windows classifying the network correctly", s.Profiles)
	}
	if s.Group != domain.FirewallGroup {
		t.Errorf("group = %q, want %q", s.Group, domain.FirewallGroup)
	}
}

func TestAnEmptyRemoteScopeIsRefused(t *testing.T) {
	// Windows reads an empty RemoteAddresses as "*". The domain says an empty
	// scope means the rule must not exist, so emitting it would invert the
	// meaning into the worst one available.
	r := baseRule()
	r.Remote = nil

	if _, err := specFor(r, "kanpachi0"); err == nil {
		t.Fatal("an empty remote scope was accepted, which Windows reads as anyone")
	} else if !strings.Contains(err.Error(), "anyone") {
		t.Errorf("the error does not say why it matters: %v", err)
	}
}

func TestARuleWithNoLocalAddressIsRefused(t *testing.T) {
	// Without a local address the rule applies to every address on the machine,
	// including the home LAN one.
	r := baseRule()
	r.Local = netip.Addr{}

	if _, err := specFor(r, "kanpachi0"); err == nil {
		t.Fatal("a rule with no local address was accepted")
	}
}

func TestProtoBothNeverReachesTheFirewall(t *testing.T) {
	// BuildRuleSet expands it into two rules. Reaching here means the expansion
	// was skipped and the rule would silently cover one protocol.
	r := baseRule()
	r.Proto = domain.ProtoBoth

	if _, err := specFor(r, "kanpachi0"); err == nil {
		t.Fatal("proto both was translated instead of refused")
	}
}

func TestARangeKeepsTheDash(t *testing.T) {
	r := baseRule()
	r.To = 16262

	s, err := specFor(r, "kanpachi0")
	if err != nil {
		t.Fatal(err)
	}
	if s.LocalPorts != "16261-16262" {
		t.Errorf("ports = %q, want 16261-16262", s.LocalPorts)
	}
}

func TestTheRemoteScopeIsStable(t *testing.T) {
	// Apply diffs live rules against wanted ones. An unstable string would
	// rewrite the whole firewall on every heartbeat.
	r := baseRule()
	r.Remote = []netip.Addr{addr("100.64.1.9"), addr("100.64.1.2"), addr("100.64.1.5")}
	r.Nets = []netip.Prefix{netip.MustParsePrefix("100.64.9.0/24")}

	first, err := specFor(r, "kanpachi0")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		again, err := specFor(r, "kanpachi0")
		if err != nil {
			t.Fatal(err)
		}
		if again.RemoteAddresses != first.RemoteAddresses {
			t.Fatalf("run %d gave %q, first gave %q", i, again.RemoteAddresses, first.RemoteAddresses)
		}
	}
	if first.RemoteAddresses != "100.64.1.2,100.64.1.5,100.64.1.9,100.64.9.0/24" {
		t.Errorf("remote = %q, want them sorted and comma separated", first.RemoteAddresses)
	}
}

func TestNoAdapterMeansNoInterfaceScope(t *testing.T) {
	// A caller that cannot resolve the adapter name yet must pass empty rather
	// than invent one. Scoping to a name that does not exist would make the
	// permit apply to nothing, and the game would not connect.
	s, err := specFor(baseRule(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Interfaces) != 0 {
		t.Errorf("interfaces = %v, want none", s.Interfaces)
	}
}

func TestOneBadRuleFailsTheWholeSet(t *testing.T) {
	// Applying a set with one rule quietly dropped is worse than not applying
	// it: the caller believes the room is configured and one player cannot
	// connect, with nothing on screen to explain it.
	bad := baseRule()
	bad.Name = "kanpachi-tcp-7777"
	bad.Remote = nil

	var rs domain.RuleSet
	rs.Add(baseRule(), bad)

	if _, err := SpecsFor(rs, "kanpachi0"); err == nil {
		t.Fatal("the set translated with a broken rule in it")
	} else if !strings.Contains(err.Error(), "kanpachi-tcp-7777") {
		t.Errorf("the error does not name the offending rule: %v", err)
	}
}

func TestSameScopeIgnoresWhatWindowsFillsIn(t *testing.T) {
	a, err := specFor(baseRule(), "kanpachi0")
	if err != nil {
		t.Fatal(err)
	}
	b := a
	if !a.sameScope(b) {
		t.Fatal("identical specs compared as different")
	}

	// Windows hands the interface name back with its own casing.
	b.Interfaces = []string{"KANPACHI0"}
	if !a.sameScope(b) {
		t.Error("a case difference in the adapter name counted as tampering, " +
			"which would make Apply rewrite the rule on every heartbeat")
	}

	b = a
	b.RemoteAddresses = "100.64.1.5,100.64.1.6"
	if a.sameScope(b) {
		t.Error("an extra member in the remote scope went unnoticed")
	}

	b = a
	b.Enabled = false
	if a.sameScope(b) {
		t.Error("a disabled rule counted as intact, and a disabled rule leaves " +
			"the port shut just like a missing one")
	}
}

// Windows no guarda lo que se le escribe: guarda un equivalente, y devuelve el
// equivalente. Las cadenas de este test están MEDIDAS en una máquina real.
//
// Compararlas crudas hacía que toda regla pareciera alterada, para siempre, y
// eso tenía dos consecuencias de verdad. La cuarentena de base avisaba de deriva
// en sus 48 reglas en cada arranque, que es la forma más segura de garantizar
// que nadie lea el log. Y `Apply` retiraba y reescribía cada regla en cada
// latido: el código evita a propósito que una regla quede mitad vieja y mitad
// nueva porque esa ventana está en el cable, y esto la reabría con un
// temporizador.
func TestWindowsGivesTheAddressesBackInItsOwnShape(t *testing.T) {
	// Lo que Kanpachi escribe, y lo que Windows devolvió al leerlo.
	casos := []struct{ escrito, leído string }{
		{"100.127.255.1", "100.127.255.1/255.255.255.255"},
		{"100.127.255.0/24", "100.127.255.0/255.255.255.0"},
		{"", "*"},
		{"10.99.7.5,10.99.7.6", "10.99.7.6/255.255.255.255,10.99.7.5/255.255.255.255"},
	}
	for _, c := range casos {
		if normalizeAddresses(c.escrito) != normalizeAddresses(c.leído) {
			t.Errorf("escrito %q y leído %q se comparan distinto:\n  %q\n  %q",
				c.escrito, c.leído, normalizeAddresses(c.escrito), normalizeAddresses(c.leído))
		}
	}

	// Y lo que de verdad cambió sigue viéndose. Sin esto, normalizar sería
	// apagar el detector en vez de arreglarlo.
	distintos := []struct{ a, b string }{
		{"10.99.7.5", "10.99.7.6"},
		{"10.99.7.0/24", "10.99.7.0/16"},
		{"10.99.7.5", ""},
		{"10.99.7.5", "10.99.7.5,10.99.7.6"},
	}
	for _, c := range distintos {
		if normalizeAddresses(c.a) == normalizeAddresses(c.b) {
			t.Errorf("%q y %q se comparan iguales, y son alcances distintos", c.a, c.b)
		}
	}
}

// La puerta del vestíbulo va en el adaptador del VESTÍBULO.
//
// El host vive en dos redes a la vez, y la puerta donde toca quien todavía no es
// miembro está en la segunda. Anclando todos los permisos al adaptador de la
// sala, esa regla quedaba acotada a una interfaz donde su propia dirección local
// no vive, o sea que no casaba con nada: la puerta cerrada en la capa de
// Windows, con todo reportando verde. Medido en una sala real, donde la regla se
// leyó como `local=100.127.255.1 ifaces=kanpachi0`.
func TestTheLobbyDoorGoesOnTheLobbyAdapter(t *testing.T) {
	puerta := domain.FirewallRule{
		Name:  domain.FirewallGroup + ": canal de control puerta",
		Proto: domain.ProtoTCP,
		From:  domain.ControlPort,
		To:    domain.ControlPort,
		Local: domain.RendezvousSubnet.Addr().Next(),
		Nets:  []netip.Prefix{domain.RendezvousSubnet},
	}
	s, err := specFor(puerta, domain.AdapterName)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Interfaces) != 1 || s.Interfaces[0] != domain.LobbyAdapterName {
		t.Errorf("la puerta quedó en %v, y vive en %s", s.Interfaces, domain.LobbyAdapterName)
	}

	// Y una regla de la sala sigue en el adaptador de la sala.
	sala := puerta
	sala.Local = netip.MustParseAddr("10.99.7.1")
	sala.Nets = nil
	sala.Remote = []netip.Addr{netip.MustParseAddr("10.99.7.5")}
	s, err = specFor(sala, domain.AdapterName)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Interfaces) != 1 || s.Interfaces[0] != domain.AdapterName {
		t.Errorf("una regla de la sala quedó en %v", s.Interfaces)
	}
}
