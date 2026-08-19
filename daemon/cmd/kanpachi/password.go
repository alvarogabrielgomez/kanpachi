package main

// `kanpachi password`: the password of the registry this machine hosts on.
//
// # Why there is no --password flag, and never will be
//
// On Linux any user reads /proc/<pid>/cmdline, and the shell keeps a history. A
// flag would put the password of somebody else's seed in two places that outlive
// the command. It is typed, masked, or it does not get sent.
//
// Reading it from stdin is allowed, and that is the door for a script: a file
// with mode 0600 piped in never appears in an argument list.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/AlecAivazis/survey/v2"
	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/daemon/transport/protocol"
)

func cmdPassword(ctx context.Context, op options, args []string) error {
	if len(args) > 0 {
		return badUsage("password takes no arguments. It is typed, never passed: " +
			"the argv of a process is world readable")
	}

	pw, err := readPassword(op)
	if err != nil {
		return err
	}

	c, err := dial(op)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	// The answer is empty on purpose, so there is nothing to print beyond the
	// fact that it worked. See [protocol.MethodSeedPassword].
	_, done, err := request[struct{}](c, op, protocol.MethodSeedPassword, struct {
		Password string `json:"password"`
	}{pw})
	if done || err != nil {
		return err
	}
	fmt.Println("The registry accepted it. This machine can host on it now.")
	return nil
}

// readPassword takes it from the terminal, or from input if that was redirected.
//
// # With no terminal it reads stdin instead of refusing
//
// It is the opposite of what confirming trust does, and for a concrete reason:
// there the absence of a terminal means NOBODY confirmed, and assuming a yes
// would be taking the decision away. Here redirected input carries the value
// itself, so there is nothing to assume. A 0600 file through the pipe is exactly
// the right way to automate this.
func readPassword(op options) (string, error) {
	if !hasTerminal() {
		raw, err := io.ReadAll(io.LimitReader(os.Stdin, 4096))
		if err != nil {
			return "", fmt.Errorf("could not read the password from standard input: %w", err)
		}
		pw := strings.TrimRight(string(raw), "\r\n")
		if pw == "" {
			return "", errors.New("there is no terminal and standard input was empty.\n" +
				"  To automate it:  sudo kanpachi password < /path/to/secret")
		}
		return pw, domain.ValidateSeedPassword(pw)
	}
	if op.json {
		// With `--json` the output belongs to a machine, and a machine does not
		// type. It gets said here instead of letting a prompt paint itself over
		// the JSON.
		return "", errors.New("with --json the password comes from standard input")
	}

	var pw string
	question := &survey.Password{
		Message: fmt.Sprintf("Password of the registry (%d-%d characters):",
			domain.MinSeedPasswordLen, domain.MaxSeedPasswordLen),
	}
	if err := survey.AskOne(question, &pw); err != nil {
		return "", err
	}
	// The rule gets checked HERE as well as in the daemon, and that is not
	// redundant: without this, a password that is too short travels over the pipe
	// before anybody looks at it, and asking for it again costs a round trip and
	// a message that arrives in the daemon's own words.
	return pw, domain.ValidateSeedPassword(pw)
}
