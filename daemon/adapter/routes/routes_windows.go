//go:build windows

package routes

import (
	"context"
	"fmt"
	"net/netip"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/accentiostudios/kanpachi/core/port"
)

// Table reads this machine's addresses and routes.
//
// It holds no state and caches nothing, on purpose. A laptop's network changes
// between home and the office, so an answer is only true at the moment it is
// asked. [port.RoutingTable] is consulted when opening or joining a room, never
// at install time, for the same reason.
type Table struct{}

func New() *Table { return &Table{} }

// The check that this still fits the port. If a signature in core changes, the
// error comes out here and not where it gets wired.
var _ port.RoutingTable = (*Table)(nil)

// LocalPrefixes answers with both the addresses configured on every adapter and
// the destinations in the routing table.
//
// Both, and not just one. Addresses alone miss a static route to a network this
// machine can reach through a gateway, which is a range a room must still avoid.
// The routing table alone reports its rows in forms that say less about which
// adapter owns them. Together they are the question the address planner asks.
func (t *Table) LocalPrefixes(ctx context.Context) ([]netip.Prefix, error) {
	// Two synchronous calls into iphlpapi, on the order of milliseconds. There
	// is nothing to interrupt halfway, so the context is honoured once at the
	// door rather than pretending to be cancellable.
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	names, addrs, err := adapterAddresses()
	if err != nil {
		return nil, err
	}
	rows, err := routeTable(names)
	if err != nil {
		return nil, err
	}
	return usable(append(addrs, rows...)), nil
}

// adapterAddresses returns the index-to-name map and one entry per unicast
// address.
//
// The map exists because the routing table identifies an adapter by index and
// this package excludes Kanpachi's own adapters by NAME. Without it, an open
// room's routes would look like somebody else's network.
func adapterAddresses() (map[uint32]string, []entry, error) {
	const flags = windows.GAA_FLAG_SKIP_ANYCAST |
		windows.GAA_FLAG_SKIP_MULTICAST |
		windows.GAA_FLAG_SKIP_DNS_SERVER

	// The documented way to size this call is to let it fail once and retry
	// with the size it asks for. The starting value is the 15 kB Microsoft
	// recommends, and the loop is bounded because the adapter list can grow
	// between the two calls and a machine that keeps growing it must not hang
	// the daemon.
	size := uint32(15000)
	var buf []byte
	for tries := 0; ; tries++ {
		buf = make([]byte, size)
		err := windows.GetAdaptersAddresses(windows.AF_UNSPEC, flags, 0,
			(*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0])), &size)
		if err == nil {
			break
		}
		if err != windows.ERROR_BUFFER_OVERFLOW || tries >= 3 {
			return nil, nil, fmt.Errorf("enumerando los adaptadores de red: %w", err)
		}
	}

	names := make(map[uint32]string, 8)
	var out []entry

	for a := (*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0])); a != nil; a = a.Next {
		name := windows.UTF16PtrToString(a.FriendlyName)

		// Both indices are recorded. They are usually the same number, and
		// nothing guarantees it: an adapter with IPv6 disabled reports zero in
		// one of them, and a route would then land on an adapter nobody named.
		if a.IfIndex != 0 {
			names[a.IfIndex] = name
		}
		if a.Ipv6IfIndex != 0 {
			names[a.Ipv6IfIndex] = name
		}

		for u := a.FirstUnicastAddress; u != nil; u = u.Next {
			ip := u.Address.IP()
			if ip == nil {
				continue
			}
			addr, ok := netip.AddrFromSlice(ip)
			if !ok {
				continue
			}
			// PrefixFrom returns the zero value when the length does not fit
			// the family, and the pure half drops invalid prefixes, so a
			// surprising row costs one entry rather than the whole answer.
			out = append(out, entry{
				adapter: name,
				prefix:  netip.PrefixFrom(addr.Unmap(), int(u.OnLinkPrefixLength)),
			})
		}
	}
	return names, out, nil
}

// routeTable returns one entry per route destination.
func routeTable(names map[uint32]string) ([]entry, error) {
	var table *windows.MibIpForwardTable2
	if err := windows.GetIpForwardTable2(windows.AF_UNSPEC, &table); err != nil {
		return nil, fmt.Errorf("leyendo la tabla de rutas del sistema: %w", err)
	}
	// The table is allocated by the system and freed by name, never by the Go
	// allocator. Leaking it would leak in every room that gets opened.
	defer windows.FreeMibTable(unsafe.Pointer(table))

	rows := table.Rows()
	out := make([]entry, 0, len(rows))
	for i := range rows {
		p, ok := prefixOf(&rows[i].DestinationPrefix)
		if !ok {
			continue
		}
		// A route on an adapter that is not in the map keeps an empty name,
		// which only means it cannot be excluded as one of ours.
		out = append(out, entry{adapter: names[rows[i].InterfaceIndex], prefix: p})
	}
	return out, nil
}

// prefixOf reads the address union that WFP and the routing table both use.
//
// It is a union, so which struct it really is depends on the family field. A
// family this does not know returns false instead of a guess: reading an IPv6
// blob as IPv4 would produce a prefix that looks plausible and is fiction.
func prefixOf(p *windows.IpAddressPrefix) (netip.Prefix, bool) {
	switch p.Prefix.Family {
	case windows.AF_INET:
		raw := (*windows.RawSockaddrInet4)(unsafe.Pointer(&p.Prefix))
		return netip.PrefixFrom(netip.AddrFrom4(raw.Addr), int(p.PrefixLength)), true
	case windows.AF_INET6:
		raw := (*windows.RawSockaddrInet6)(unsafe.Pointer(&p.Prefix))
		return netip.PrefixFrom(netip.AddrFrom16(raw.Addr).Unmap(), int(p.PrefixLength)), true
	}
	return netip.Prefix{}, false
}
