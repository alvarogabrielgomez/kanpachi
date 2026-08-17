package main

import (
	"errors"
	"fmt"

	"github.com/AlecAivazis/survey/v2"
	"github.com/accentiostudios/kanpachi/daemon/transport/protocol"
)

// conSalaAbriendoFirewall runs a host/join and, when the daemon refuses
// because a foreign firewall blocks the room's inbound, asks and retries with
// consent.
//
// # Why it is error-driven while displacement is pre-flight
//
// Displacement rides in the status every face already reads. Whether a foreign
// firewall blocks is something only the daemon can measure at the moment of
// entry, and its refusal already carries who blocks and the exact commands
// that would open it: a second pre-flight method would be a copy of that
// answer, free to drift.
//
// # The same three outcomes as every consent
//
//  1. With `--yes` the consent goes in the FIRST request. That keeps the
//     `--json` face answering exactly once, instead of printing a refusal and
//     then a second answer for the same order.
//  2. With no terminal and no `--yes`, it REFUSES. The absence of a terminal
//     is never a yes, least of all for writing into the operator's firewall.
//  3. With a terminal it shows the daemon's sentence, commands included, and
//     asks defaulting to NO.
func conSalaAbriendoFirewall(
	op opciones, m protocol.Method, sinPreguntar bool, params func(allowFirewall bool) any,
) error {
	if sinPreguntar {
		return conSala(op, m, params(true))
	}
	err := conSala(op, m, params(false))
	var pe *protocol.Error
	if !errors.As(err, &pe) || pe.Code != protocol.CodeFirewallBlocks {
		return err
	}
	if cErr := consentirFirewall(pe.Message); cErr != nil {
		return cErr
	}
	return conSala(op, m, params(true))
}

// consentirFirewall shows who blocks and asks. The person consents to the
// exact commands in the sentence, which the daemon composed from the same
// closed list it executes: what is shown and what runs cannot disagree.
func consentirFirewall(bloqueo string) error {
	if !hayTerminal() {
		return negativa("%s\n"+
			"  There is no terminal to confirm opening it in.\n"+
			"  If that is what you want, say so on purpose:  --yes", bloqueo)
	}
	fmt.Println()
	fmt.Println("  " + bloqueo)
	fmt.Println()
	var sí bool
	if err := preguntar(&survey.Confirm{
		Message: "Open the firewall for the room? (undone when the room closes)",
		Default: false,
	}, &sí); err != nil {
		return err
	}
	if !sí {
		return negativa("nothing was done.")
	}
	return nil
}
