package main

// Confirming trust in a registry, which is the same act at both moments where it
// comes up.
//
// # Why asking is needed, now that there is no default seed
//
// Because the registry stopped being a compiled constant and became anybody's.
// When CREATING it comes from whatever this machine has configured; when
// ENTERING it is chosen by whoever wrote the code they pasted you. In both cases
// it is a third party's machine that this process is going to talk to, and that
// sees your public IP, so the decision belongs to the person and not to the
// program.
//
// # Why it lives in the CLIENT and not in the daemon
//
// Because the daemon has nobody to ask. It is a service, and the product's
// invariant is that nothing from outside takes effect without a confirmation
// INSIDE the app: the place where that holds is each face, the window and this
// terminal. Both show the same thing, and neither one saves the answer.

import (
	"encoding/json"
	"fmt"

	"github.com/AlecAivazis/survey/v2"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/daemon/transport/protocol"
)

// takeYes pulls `--yes` out of the arguments and says whether it was there.
//
// It gets pulled out rather than read in passing because `host` joins what is
// left into the room's NAME: without this, `kanpachi host my game --yes` opens a
// room called "my game --yes".
func takeYes(args []string) ([]string, bool) {
	rest := make([]string, 0, len(args))
	yes := false
	for _, a := range args {
		switch a {
		case "--yes", "-yes", "-y":
			yes = true
		default:
			rest = append(rest, a)
		}
	}
	return rest, yes
}

// trustTheRegistry asks before talking to a registry, and refuses when there is
// nobody to ask.
//
// # The three ways out, and why the middle one is a security matter
//
//  1. With `--yes`, it goes on. Whoever typed it already said so.
//  2. **With no terminal and no `--yes`, it REFUSES.** This is the case that
//     gets overlooked: inside a script there is nowhere to confirm, and reading
//     that absence as a yes would make the confirmation disappear exactly where
//     nobody is watching. It is the shape the invariant takes on Linux, that
//     nothing from outside takes effect without a confirmation inside.
//  3. With a terminal, it asks, and the default is NO.
//
// The refusal leaves through the error path, so with `--json` standard output
// stays clean: what a script consumes is stdout, and this explanation goes to
// stderr with its exit code.
func trustTheRegistry(op options, seed, moment string, noQuestions bool) error {
	if noQuestions {
		// It gets said anyway, and that is not redundant: whoever automates also
		// wants to see in the log which machine their script talked to. With
		// `--json` it stays quiet, because what is on the other side there is a
		// parser.
		if !op.json {
			fmt.Printf("Using registry %s.\n", seed)
		}
		return nil
	}
	if !hasTerminal() {
		return refuse("this needs you to confirm the registry, and there is no terminal to ask in.\n"+
			"  %s would talk to %s.\n"+
			"  If that is what you want, say so on purpose:  --yes", moment, seed)
	}

	fmt.Println()
	fmt.Printf("  %s would use the registry %s\n", moment, seed)
	fmt.Println()
	fmt.Println("  That machine is the meeting point: it sees your public IP and the")
	fmt.Println("  invite ID, and it is what everyone in the room connects through.")
	fmt.Println("  Only go on if you trust whoever runs it.")
	fmt.Println()

	var yes bool
	if err := ask(&survey.Confirm{
		Message: fmt.Sprintf("Trust %s?", seed),
		Default: false,
	}, &yes); err != nil {
		return err
	}
	if !yes {
		return refuse("nothing was done, because you did not trust %s.\n"+
			"  To point this machine somewhere else:  kanpachi seed <host>", seed)
	}
	return nil
}

// registryOfThisMachine reads the configured registry, so it can be shown before
// creating.
//
// Empty is not a failure of the daemon: it is an install that has not chosen one
// yet, and what is needed there is saying how one gets chosen.
func registryOfThisMachine(op options) (string, error) {
	c, err := dial(op)
	if err != nil {
		return "", err
	}
	defer func() { _ = c.Close() }()

	var v struct {
		Seed      string `json:"seed"`
		Suggested string `json:"suggested"`
	}
	raw, err := c.Call(protocol.MethodOwnSeed, nil)
	if err != nil {
		return "", err
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", fmt.Errorf("reading the configured registry: %w", err)
	}
	if v.Seed != "" {
		return v.Seed, nil
	}
	if v.Suggested != "" {
		return "", refuse("this machine has no registry configured, so there is nowhere to open a room.\n"+
			"  The last room you entered was on %s.\n"+
			"  To use that one:  kanpachi seed %s", v.Suggested, v.Suggested)
	}
	return "", refuse("this machine has no registry configured, so there is nowhere to open a room.\n" +
		"  To set one:  kanpachi seed seed.example.com")
}

// registryFromCode pulls the registry out of what they pasted, ONLY to show it.
//
// The text still travels to the daemon exactly as it came, which is the hostile
// input boundary and the only place where whether a code is a code gets decided.
// It gets looked at here so the machine can be named in the question, and text
// that does not parse returns empty rather than an error: whoever has to explain
// what that code is missing is the daemon, with its message, and not this
// function.
func registryFromCode(text string) string {
	room, err := domain.ParseRoom(text)
	if err != nil {
		return ""
	}
	return room.Seed
}
