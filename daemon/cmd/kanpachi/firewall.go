package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/AlecAivazis/survey/v2"
	"github.com/accentiostudios/kanpachi/daemon/transport/protocol"
)

// conSalaConsintiendo runs a host/join and drives the two consents the daemon
// may refuse it over: the base-quarantine question, and a foreign firewall
// blocking the room's inbound. Each refusal is shown, answered, and retried.
//
// # Why they are error-driven while displacement is pre-flight
//
// Displacement rides in the status every face already reads. These two are
// the daemon's to refuse at the moment of entry, and each refusal already
// carries its whole sentence — the exact ports, the exact commands — so a
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
// the CLI always asks to be refused while the question is owed — in the
// terminal the choice is mandatory — and with no terminal it refuses naming
// the flag. There is no default answer: both answers are legitimate here, and
// pressing Enter must not pick one.
func conSalaConsintiendo(
	op opciones, m protocol.Method, sinPreguntar bool, cuarentena string,
	params func(allowFirewall bool, quarantine string) any,
) error {
	allow := sinPreguntar
	q := cuarentena
	if q == "" {
		q = "ask"
	}
	for {
		err := conSala(op, m, params(allow, q))
		var pe *protocol.Error
		if !errors.As(err, &pe) {
			return err
		}
		switch pe.Code {
		case protocol.CodeQuarantineUndecided:
			elegido, cErr := decidirCuarentena(pe.Message)
			if cErr != nil {
				return cErr
			}
			q = elegido
		case protocol.CodeFirewallBlocks:
			if allow {
				return err
			}
			if cErr := consentirFirewall(pe.Message); cErr != nil {
				return cErr
			}
			allow = true
		default:
			return err
		}
	}
}

// decidirCuarentena shows the daemon's question and demands an answer.
func decidirCuarentena(pregunta string) (string, error) {
	if !hayTerminal() {
		return "", negativa("%s\n"+
			"  There is no terminal to answer in, and this is answered once per machine.\n"+
			"  Choose on purpose:  --quarantine on   or   --quarantine off", pregunta)
	}
	fmt.Println()
	fmt.Println("  " + pregunta)
	fmt.Println()
	const (
		cerrar = "Close them (recommended)"
		dejar  = "Leave them open (this PC shares folders, or gets entered over Remote Desktop)"
	)
	elegido, err := elegir("Close those ports?", []string{cerrar, dejar})
	if err != nil {
		return "", err
	}
	if elegido == cerrar {
		return "on", nil
	}
	return "off", nil
}

// sacarCuarentena splits `--quarantine on|off` out of the arguments.
func sacarCuarentena(args []string) ([]string, string, error) {
	resto := make([]string, 0, len(args))
	valor := ""
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--quarantine":
			if i+1 >= len(args) {
				return nil, "", uso("--quarantine needs a value: on or off")
			}
			i++
			valor = args[i]
		case strings.HasPrefix(a, "--quarantine="):
			valor = strings.TrimPrefix(a, "--quarantine=")
		default:
			resto = append(resto, a)
			continue
		}
		if valor != "on" && valor != "off" {
			return nil, "", uso("--quarantine only takes on or off")
		}
	}
	return resto, valor, nil
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
