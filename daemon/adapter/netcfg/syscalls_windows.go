//go:build windows

package netcfg

import (
	"context"
	"fmt"
	"net/netip"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/accentiostudios/kanpachi/daemon/adapter/firewall/wfp"
)

// luidOf resolves an adapter name to the LUID the routing APIs speak.
//
// It reuses the firewall gate's resolver rather than growing a second one. That
// function refuses to return zero, which matters just as much here: a zero LUID
// on a route entry is not "no adapter", it is a different adapter.
func luidOf(name string) (uint64, error) { return wfp.LUIDOf(name) }

// routesOn lists the route destinations installed on one adapter.
func routesOn(luid uint64) ([]netip.Prefix, error) {
	var table *windows.MibIpForwardTable2
	if err := windows.GetIpForwardTable2(windows.AF_UNSPEC, &table); err != nil {
		return nil, fmt.Errorf("leyendo la tabla de rutas: %w", err)
	}
	defer windows.FreeMibTable(unsafe.Pointer(table))

	var out []netip.Prefix
	for _, r := range table.Rows() {
		if r.InterfaceLuid != luid {
			continue
		}
		if p, ok := prefixOf(&r.DestinationPrefix); ok {
			out = append(out, p)
		}
	}
	return out, nil
}

func addRoute(luid uint64, p netip.Prefix) error {
	row := forwardRow(luid, p)
	r, _, _ := procCreateIpForwardEntry.Call(uintptr(unsafe.Pointer(&row)))
	// Already there is success. Applying is declarative and idempotent, and the
	// supervisor reapplies every couple of minutes.
	if r != 0 && r != uintptr(windows.ERROR_OBJECT_ALREADY_EXISTS) {
		return fmt.Errorf("CreateIpForwardEntry2 devolvió %d", r)
	}
	return nil
}

func deleteRoute(luid uint64, p netip.Prefix) error {
	row := forwardRow(luid, p)
	r, _, _ := procDeleteIpForwardEntry.Call(uintptr(unsafe.Pointer(&row)))
	// Not there is success, for the same reason.
	if r != 0 && r != uintptr(windows.ERROR_NOT_FOUND) {
		return fmt.Errorf("DeleteIpForwardEntry2 devolvió %d", r)
	}
	return nil
}

// forwardRow builds an ON-LINK route: reachable through the adapter itself,
// with no gateway. The next hop is the unspecified address of the family, which
// is how Windows spells "on link".
//
// # Why it starts from InitializeIpForwardEntry
//
// Because a zeroed row is not a valid one, and the way it fails is quiet.
// `ValidLifetime` and `PreferredLifetime` default to INFINITE, and this code
// used to leave them at zero: the route was created and immediately expired,
// which reads as "the call succeeded and the route is not there". Letting
// Windows fill its own defaults and overwriting only what this package decides
// is the documented way, and it does not depend on knowing every field.
func forwardRow(luid uint64, p netip.Prefix) windows.MibIpForwardRow2 {
	var row windows.MibIpForwardRow2
	procInitIpForwardEntry.Call(uintptr(unsafe.Pointer(&row)))

	row.InterfaceLuid = luid
	row.DestinationPrefix = windows.IpAddressPrefix{
		Prefix:       sockaddrInet(p.Masked().Addr()),
		PrefixLength: uint8(p.Bits()),
	}
	// A low metric so the route is preferred over anything the system may have
	// for the same destination on another adapter, which is the whole point of
	// asking for it.
	row.Metric = 1
	row.Protocol = 3 // MIB_IPPROTO_NETMGMT: written by an administrator

	if p.Addr().Is4() {
		row.NextHop = sockaddrInet(netip.IPv4Unspecified())
	} else {
		row.NextHop = sockaddrInet(netip.IPv6Unspecified())
	}
	return row
}

// sockaddrInet fills the union the routing APIs take by value.
func sockaddrInet(a netip.Addr) windows.RawSockaddrInet {
	var raw windows.RawSockaddrInet
	if a.Is4() {
		p := (*windows.RawSockaddrInet4)(unsafe.Pointer(&raw))
		p.Family = windows.AF_INET
		p.Addr = a.As4()
		return raw
	}
	p := (*windows.RawSockaddrInet6)(unsafe.Pointer(&raw))
	p.Family = windows.AF_INET6
	p.Addr = a.As16()
	return raw
}

// prefixOf reads the address union. A family this does not know returns false
// rather than a guess: reading an IPv6 blob as IPv4 gives a plausible fiction.
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

// directPlayEnabled asks DISM whether the legacy component is installed.
func directPlayEnabled(ctx context.Context) (bool, error) {
	args := []string{"/online", "/get-featureinfo", "/featurename:DirectPlay", "/english"}
	out, err := silentCommand(ctx, "dism.exe", args...).CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("dism %v: %w (%s)", args, err, out)
	}
	// `/english` is passed so this does not depend on the machine's language,
	// which is exactly the kind of thing that works here and fails on a
	// friend's Windows.
	texto := string(out)
	for _, línea := range strings.Split(texto, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(línea), "State :") {
			continue
		}
		return strings.Contains(línea, "Enabled"), nil
	}
	return false, fmt.Errorf("dism no dijo el estado de DirectPlay: %s", texto)
}
