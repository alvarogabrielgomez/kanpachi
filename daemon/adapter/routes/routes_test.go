package routes

import (
	"crypto/rand"
	"net/netip"
	"slices"
	"testing"

	"github.com/accentiostudios/kanpachi/core/domain"
)

func pfx(s string) netip.Prefix { return netip.MustParsePrefix(s) }

// realWindowsTable is what these two calls actually report on a machine on a
// home network with a room already open. Every row is here because the system
// really produces it, and half of them have to be dropped.
func realWindowsTable() []entry {
	return []entry{
		{adapter: "Ethernet", prefix: pfx("0.0.0.0/0")},        // the default route
		{adapter: "Ethernet", prefix: pfx("192.168.1.37/24")},  // the home LAN
		{adapter: "Ethernet", prefix: pfx("192.168.1.37/32")},  // its own host route
		{adapter: "Ethernet", prefix: pfx("192.168.1.255/32")}, // its broadcast
		{adapter: "Loopback", prefix: pfx("127.0.0.0/8")},
		{adapter: "Loopback", prefix: pfx("127.0.0.1/32")},
		{adapter: "Wi-Fi", prefix: pfx("169.254.88.4/16")},       // APIPA, no address
		{adapter: "Ethernet", prefix: pfx("224.0.0.0/4")},        // multicast
		{adapter: "Ethernet", prefix: pfx("255.255.255.255/32")}, // limited broadcast
		{adapter: "Ethernet", prefix: pfx("fe80::1a2b/64")},      // IPv6 link-local
		{adapter: "Ethernet", prefix: pfx("2001:db8:1::5/64")},   // real IPv6
		{adapter: "Ethernet", prefix: pfx("::/0")},               // IPv6 default route
		{adapter: domain.AdapterName, prefix: pfx("100.87.4.1/24")},
		{adapter: domain.LobbyAdapterName, prefix: pfx("100.127.255.1/24")},
	}
}

func TestUsableKeepsWhatIsReallyTaken(t *testing.T) {
	got := usable(realWindowsTable())

	want := []netip.Prefix{
		pfx("192.168.1.0/24"),
		pfx("192.168.1.37/32"),
		pfx("192.168.1.255/32"),
		pfx("255.255.255.255/32"),
		pfx("2001:db8:1::/64"),
	}
	slices.SortFunc(want, func(a, b netip.Prefix) int {
		if c := a.Addr().Compare(b.Addr()); c != 0 {
			return c
		}
		return a.Bits() - b.Bits()
	})

	if !slices.Equal(got, want) {
		t.Errorf("prefijos filtrados distintos\n  quería %v\n  obtuvo %v", want, got)
	}
}

// TestDefaultRouteWouldMakeEveryRoomImpossible is the reason the filter exists,
// stated as its consequence rather than as a rule.
//
// The default route is in every routing table on earth. Letting it through does
// not produce a slightly worse subnet: it overlaps both address spaces
// completely, so the planner reports that no room can be opened anywhere.
func TestDefaultRouteWouldMakeEveryRoomImpossible(t *testing.T) {
	conElDefault := []netip.Prefix{pfx("0.0.0.0/0"), pfx("192.168.1.0/24")}
	if _, err := domain.PlanAddresses(conElDefault, rand.Reader); err == nil {
		t.Fatal("con 0.0.0.0/0 dentro, planear una subred debería fallar, y no falló. " +
			"Este test es el que le da sentido al filtro, así que si deja de fallar acá, " +
			"revisa PlanAddresses antes de tocar el filtro")
	}

	filtrado := usable([]entry{
		{adapter: "Ethernet", prefix: pfx("0.0.0.0/0")},
		{adapter: "Ethernet", prefix: pfx("192.168.1.0/24")},
	})
	plan, err := domain.PlanAddresses(filtrado, rand.Reader)
	if err != nil {
		t.Fatalf("filtrado, planear una subred tenía que funcionar: %v", err)
	}
	if !domain.SharedSpace.Contains(plan.Subnet.Addr()) {
		t.Errorf("sin conflictos la sala va en el espacio compartido, y fue a %v", plan.Subnet)
	}
}

// TestOurOwnAdaptersAreNotAConflict covers resuming a room after a dirty death.
//
// The room's own /24 lives inside the space the planner picks from, so counting
// it would make an open room veto the next one, and would make ResumeRoom
// reject the very subnet it is trying to restore.
func TestOurOwnAdaptersAreNotAConflict(t *testing.T) {
	sala := pfx("100.87.4.0/24")

	got := usable([]entry{
		{adapter: domain.AdapterName, prefix: pfx("100.87.4.1/24")},
		{adapter: domain.LobbyAdapterName, prefix: pfx("100.127.255.1/24")},
	})
	if len(got) != 0 {
		t.Fatalf("los adaptadores propios no son un conflicto, y quedaron %v", got)
	}

	for _, p := range got {
		if p.Overlaps(sala) {
			t.Errorf("%v pisa la subred de la sala %v", p, sala)
		}
	}
}

// TestOurAdaptersAreMatchedWithoutCase guards the comparison itself. Windows is
// not consistent about the case it reports adapter names in, and a miss here
// looks exactly like the previous test passing for the wrong reason.
func TestOurAdaptersAreMatchedWithoutCase(t *testing.T) {
	for _, name := range []string{"KANPACHI0", "Kanpachi0", "kanpachi1", "KanPachi1"} {
		if got := usable([]entry{{adapter: name, prefix: pfx("100.87.4.1/24")}}); len(got) != 0 {
			t.Errorf("%q es un adaptador nuestro y no se reconoció", name)
		}
	}
	// And the negative side: a name that merely starts the same is somebody
	// else's adapter. A HasPrefix here would silently ignore a real network.
	if got := usable([]entry{{adapter: "kanpachi0-vpn", prefix: pfx("192.168.5.1/24")}}); len(got) != 1 {
		t.Error("kanpachi0-vpn es un adaptador ajeno y sus rangos sí cuentan")
	}
}

func TestUsableIsStableAndDeduplicated(t *testing.T) {
	// The same range reported twice is normal: once as an address and once as
	// a route. Two calls have to produce the same slice, or the subnet chosen
	// for a room could wobble between them.
	raw := []entry{
		{adapter: "Ethernet", prefix: pfx("10.0.5.7/16")},
		{adapter: "Wi-Fi", prefix: pfx("10.0.0.0/16")},
		{adapter: "Ethernet", prefix: pfx("192.168.1.0/24")},
	}
	got := usable(raw)

	want := []netip.Prefix{pfx("10.0.0.0/16"), pfx("192.168.1.0/24")}
	if !slices.Equal(got, want) {
		t.Errorf("se esperaba enmascarado, sin repetidos y ordenado\n  quería %v\n  obtuvo %v", want, got)
	}
	if !slices.Equal(usable(raw), got) {
		t.Error("dos llamadas con la misma entrada dieron resultados distintos")
	}
}

func TestUsableDropsInvalidPrefixes(t *testing.T) {
	// PrefixFrom returns the zero value when the length does not fit the
	// family, which is how a surprising row from the system arrives here.
	got := usable([]entry{
		{adapter: "Ethernet", prefix: netip.Prefix{}},
		{adapter: "Ethernet", prefix: netip.PrefixFrom(netip.MustParseAddr("192.168.1.1"), 99)},
		{adapter: "Ethernet", prefix: pfx("192.168.1.0/24")},
	})
	if len(got) != 1 || got[0] != pfx("192.168.1.0/24") {
		t.Errorf("solo el prefijo válido tenía que sobrevivir, y quedó %v", got)
	}
}
