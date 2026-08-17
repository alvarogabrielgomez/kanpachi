package domain

import "strings"

// FirewallBlock is a foreign firewall about to swallow the whole room.
//
// # What it describes, exactly
//
// A firewall manager belonging to the OPERATOR (ufw, firewalld) that is
// active, postured to deny inbound, and without a pass for Kanpachi's virtual
// adapters. With that in place, the SYNs arriving at `kanpachi0`/`kanpachi1`
// die without an RST: the room assembles perfectly, everything reads green,
// and nobody gets in. Measured (2026-08-16, ufw on the droplet), and that
// silence is the defect this type exists to name BEFORE opening a room, not
// after losing the afternoon.
//
// # Why it carries the commands inside
//
// Because the product's promise is that foreign things are touched only with
// consent, and consent has to be about something CONCRETE: the person accepts
// these commands, not a general idea of "fixing the firewall". What is shown
// and what is executed come from the same place, so they cannot disagree.
type FirewallBlock struct {
	// Manager is the manager's name as the operator knows it: "ufw",
	// "firewalld". It comes from a closed list in the adapter, never from
	// parsing foreign text.
	Manager string
	// Adapters are the virtual adapters without an inbound pass, by name
	// ([AdapterName], [LobbyAdapterName]).
	Adapters []string
	// Fix are the exact commands that open, one per adapter, in the order of
	// Adapters. They are always shown; they run only with consent.
	Fix []string
}

// String paints the block the way it is told to a person: who blocks, and
// which adapters are deaf. It is the sentence whoever tries to open a room
// gets to read.
func (b FirewallBlock) String() string {
	return b.Manager + " denies inbound on " + strings.Join(b.Adapters, " and ")
}
