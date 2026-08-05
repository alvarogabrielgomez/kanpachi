// Package netcfg keeps the virtual adapter's settings against Windows's will.
//
// # Why "keeps" and not "applies"
//
// Windows reverts the adapter metric, the network category and the routes on
// every network identification event, which fires when an address is added or
// removed, when an adapter connects or disconnects, and on DHCP events.
// Applying this once at install time does not hold. The supervisor subscribes to
// NetworkProfile/Operational and reapplies the whole thing, and also reapplies
// it every few beats regardless, because that subscription is not reliable
// either. Without that fallback, a dead subscription reads as "it worked
// yesterday" a month later.
//
// # Why this file is pure
//
// What decides lives here and is tested on Linux: which routes a profile asked
// for, what MTU to write, and what has to be written down so it can be undone.
// The file with `//go:build windows` only makes the calls.
//
// # What outlives the process, and what does not
//
// Most of this dies on its own. The adapter is created by the engine per room
// and disappears with it, taking its address, metric, MTU and routes along.
// Exactly two settings are machine-wide and survive a reboot, the IPv6 prefix
// policy and DirectPlay, and those are the ones the book in `tweaks.go` records
// with their previous value.
package netcfg

import (
	"net/netip"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// Broadcast and multicast are the two routes a game profile can ask for.
//
// They are constants and not something a profile can spell out, which is the
// point: a profile declares an intent that the catalogue understands, and this
// layer decides what that intent is allowed to become. A profile cannot ask for
// a route to somewhere of its choosing.
var (
	// BroadcastRoute is classic LAN discovery, the DirectPlay era.
	BroadcastRoute = netip.MustParsePrefix("255.255.255.255/32")
	// MulticastRoute covers mDNS, SSDP and in-game server browsers.
	MulticastRoute = netip.MustParsePrefix("224.0.0.0/4")
)

// wantedRoutes is the set of extra routes the active profile asked for.
//
// The order is fixed so that two calls with the same state produce the same
// slice, which is what lets the caller diff instead of rewriting.
func wantedRoutes(want domain.AdapterState) []netip.Prefix {
	out := make([]netip.Prefix, 0, 2)
	if want.BroadcastRoute {
		out = append(out, BroadcastRoute)
	}
	if want.MulticastRoute {
		out = append(out, MulticastRoute)
	}
	return out
}

// isDefaultRoute reports whether a route would send the machine's traffic into
// the room.
//
// **Kanpachi never routes the internet.** Neither `0.0.0.0/0` nor `::/0` may
// exist on the virtual adapter, and if one appears it gets deleted. It is not
// something Kanpachi writes: EasyTier can install routes it learned from the
// network, so this is about what OTHERS may put on our adapter.
func isDefaultRoute(p netip.Prefix) bool { return p.IsValid() && p.Bits() == 0 }

// mtuFor decides what to write on the adapter, or zero to leave it alone.
//
// Zero means the path was never probed. Writing a zero would take the interface
// down, and writing the maximum blindly is exactly the PMTU black hole this
// exists to avoid, so "leave what is there" is the only correct third answer.
func mtuFor(want domain.AdapterState) int { return domain.TunnelMTU(want.MTU) }
