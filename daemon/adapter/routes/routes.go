// Package routes reads which IP ranges this machine already lives in.
//
// It implements [port.RoutingTable], whose single question decides where a room
// can be placed: [domain.PlanAddresses] needs the prefixes to avoid so a room
// never collides with the user's home network. Getting it wrong breaks
// connectivity the user already had, which is worse than failing to open a room.
//
// # Why this file is pure
//
// Everything that DECIDES lives here and is tested on Linux. The file with
// `//go:build windows` only reads the system and hands rows over. Same split as
// `adapter/firewall/wfp` and `adapter/engine/kanpachi`.
//
// The expensive mistake in this layer is not a missing prefix: it is one prefix
// too many. A single `0.0.0.0/0` in the answer overlaps every candidate, so the
// planner concludes both address spaces are full and no room can be opened
// anywhere. Every filter below exists because of a row the system really
// reports.
package routes

import (
	"net/netip"
	"slices"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// entry is one address or one route, as the system reported it.
//
// Addresses and routes arrive in the same shape on purpose. They answer the
// same question, "is this range already taken here", and merging them at the
// edge keeps the filtering in one place instead of two that can drift.
type entry struct {
	// adapter is the friendly name, the one Windows shows and the one
	// `net.Interfaces` returns. Empty when the row could not be attributed to
	// an adapter, which is not an error and only means it cannot be excluded
	// as one of ours.
	adapter string
	prefix  netip.Prefix
}

// usable reduces what the system reported to the prefixes a room must avoid.
//
// The result is masked, deduplicated and sorted, so two calls with the same
// machine state produce the same slice. That stability is what keeps the
// subnet chosen for a room from wobbling between two calls.
func usable(raw []entry) []netip.Prefix {
	seen := make(map[netip.Prefix]struct{}, len(raw))
	out := make([]netip.Prefix, 0, len(raw))

	for _, e := range raw {
		if !e.prefix.IsValid() {
			continue
		}
		if isOurs(e.adapter) {
			continue
		}
		if !worthAvoiding(e.prefix) {
			continue
		}
		p := e.prefix.Masked()
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}

	slices.SortFunc(out, func(a, b netip.Prefix) int {
		if c := a.Addr().Compare(b.Addr()); c != 0 {
			return c
		}
		return a.Bits() - b.Bits()
	})
	return out
}

// isOurs reports whether the row belongs to a Kanpachi adapter.
//
// Excluding them is not tidiness. A room that is up owns a /24 inside the very
// space the planner picks from, so counting it would make an open room veto the
// next one, and would make resuming a room after a dirty death fail because its
// own subnet looks occupied.
//
// The comparison is case-insensitive ASCII, like the one the firewall gate uses
// to resolve an adapter name to a LUID. Windows adapter names are ASCII, and
// `strings.EqualFold` would apply Unicode folding, which can equate names the
// system considers different.
func isOurs(adapter string) bool {
	return equalFoldASCII(adapter, domain.AdapterName) ||
		equalFoldASCII(adapter, domain.LobbyAdapterName)
}

// worthAvoiding reports whether a prefix says anything about ranges being taken.
func worthAvoiding(p netip.Prefix) bool {
	// The default route is the one that matters. Every routing table has one,
	// it overlaps EVERYTHING, and letting it through makes the planner believe
	// the shared space and the fallback space are both entirely occupied. The
	// symptom is not a bad subnet, it is that no room can ever be created.
	if p.Bits() == 0 {
		return false
	}

	a := p.Addr()
	switch {
	case a.IsUnspecified():
		return false
	case a.IsLoopback():
		// 127.0.0.0/8 and ::1. Nothing is routed to other machines through it.
		return false
	case a.IsLinkLocalUnicast():
		// 169.254.0.0/16 and fe80::/10. Present on any adapter that failed to
		// get an address, which is common and means nothing is taken.
		return false
	case a.IsMulticast(), a.IsLinkLocalMulticast(), a.IsInterfaceLocalMulticast():
		// 224.0.0.0/4 and ff00::/8. Every routing table carries these and they
		// are not addresses anybody holds.
		return false
	}
	return true
}

// equalFoldASCII compares without case and without touching Unicode.
func equalFoldASCII(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		x, y := a[i], b[i]
		if 'A' <= x && x <= 'Z' {
			x += 'a' - 'A'
		}
		if 'A' <= y && y <= 'Z' {
			y += 'a' - 'A'
		}
		if x != y {
			return false
		}
	}
	return true
}
