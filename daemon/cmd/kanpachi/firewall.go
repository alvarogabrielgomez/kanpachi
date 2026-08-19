package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/AlecAivazis/survey/v2"
	"github.com/accentiostudios/kanpachi/daemon/transport/protocol"
)

// withRoomConsenting runs a host/join and drives the two consents the daemon
// may refuse it over: the base-quarantine question, and a foreign firewall
// blocking the room's inbound. Each refusal is shown, answered, and retried.
//
// # Why they are error-driven while displacement is pre-flight
//
// Displacement rides in the status every face already reads. These two are
// the daemon's to refuse at the moment of entry, and each refusal already
// carries its whole sentence, the exact ports and the exact commands, so a
// second pre-flight method would be a copy of that answer, free to drift.
//
// # The outcomes, per consent
//
// The firewall keeps its three: `--yes` sends the consent in the FIRST
// request (so `--json` answers exactly once), no terminal and no `--yes`
// refuses, and a terminal shows the daemon's sentence and asks defaulting to
// NO.
//
// The quarantine is its own decision with its own flag, and `--yes` does NOT
// answer it ON PURPOSE: `--yes` already means "trust the registry" and "open
// the foreign firewall", and hanging a third, different security decision off
// the same word is how a consent loses its meaning. Without `--quarantine`,
// the CLI always asks to be refused while the question is owed, in the
// terminal the choice is mandatory, and with no terminal it refuses naming
// the flag. There is no default answer: both answers are legitimate here, and
// pressing Enter must not pick one.
func withRoomConsenting(
	op options, m protocol.Method, noQuestions bool, quarantine string,
	params func(allowFirewall bool, quarantine string) any,
) error {
	allow := noQuestions
	q := quarantine
	if q == "" {
		q = "ask"
	}
	for {
		err := withRoom(op, m, params(allow, q))
		var pe *protocol.Error
		if !errors.As(err, &pe) {
			return err
		}
		switch pe.Code {
		case protocol.CodeQuarantineUndecided:
			chosen, cErr := decideQuarantine(pe.Message)
			if cErr != nil {
				return cErr
			}
			q = chosen
		case protocol.CodeFirewallBlocks:
			if allow {
				return err
			}
			if cErr := consentFirewall(pe.Message); cErr != nil {
				return cErr
			}
			allow = true
		default:
			return err
		}
	}
}

// decideQuarantine shows the daemon's question and demands an answer.
func decideQuarantine(question string) (string, error) {
	if !hasTerminal() {
		return "", refuse("%s\n"+
			"  There is no terminal to answer in, and this is answered once per machine.\n"+
			"  Choose on purpose:  --quarantine on   or   --quarantine off", question)
	}
	fmt.Println()
	fmt.Println("  " + question)
	fmt.Println()
	const (
		close_ = "Close them (recommended)"
		leave  = "Leave them open (this PC shares folders, or gets entered over Remote Desktop)"
	)
	chosen, err := choose("Close those ports?", []string{close_, leave})
	if err != nil {
		return "", err
	}
	if chosen == close_ {
		return "on", nil
	}
	return "off", nil
}

// takeQuarantine splits `--quarantine on|off` out of the arguments.
func takeQuarantine(args []string) ([]string, string, error) {
	rest := make([]string, 0, len(args))
	value := ""
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--quarantine":
			if i+1 >= len(args) {
				return nil, "", badUsage("--quarantine needs a value: on or off")
			}
			i++
			value = args[i]
		case strings.HasPrefix(a, "--quarantine="):
			value = strings.TrimPrefix(a, "--quarantine=")
		default:
			rest = append(rest, a)
			continue
		}
		if value != "on" && value != "off" {
			return nil, "", badUsage("--quarantine only takes on or off")
		}
	}
	return rest, value, nil
}

// consentFirewall shows who blocks and asks. The person consents to the
// exact commands in the sentence, which the daemon composed from the same
// closed list it executes: what is shown and what runs cannot disagree.
func consentFirewall(block string) error {
	if !hasTerminal() {
		return refuse("%s\n"+
			"  There is no terminal to confirm opening it in.\n"+
			"  If that is what you want, say so on purpose:  --yes", block)
	}
	fmt.Println()
	fmt.Println("  " + block)
	fmt.Println()
	var yes bool
	if err := ask(&survey.Confirm{
		Message: "Open the firewall for the room? (undone when the room closes)",
		Default: false,
	}, &yes); err != nil {
		return err
	}
	if !yes {
		return refuse("nothing was done.")
	}
	return nil
}
