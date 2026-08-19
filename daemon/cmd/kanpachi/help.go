package main

// The two help pages: the list of commands, and one page per command.
//
// # Why one page per command, when there was only a list
//
// Because the list has one line per command and a command has flags. `profile`
// takes four, `upgrade` takes four, `host` and `join` take two each, and none of
// them fit on a line next to a sentence saying what the command is for. What
// used to happen was that they were crammed in anyway: the `profile` row read
// `profile <id> --name <n> [--tcp|--udp l]`, which overflowed the column the
// descriptions line up on and broke the shape of the whole page for the one
// command whose flags most needed explaining.
//
// So the list went back to being a list, and everything that does not fit on a
// line lives in [command.usage], which is what `kanpachi <name> --help` prints.

import (
	"fmt"
	"io"
	"runtime"
	"strings"
	"time"

	"github.com/accentiostudios/kanpachi/daemon/transport/pipe"
)

// printHelp writes the list of commands.
//
// By hand and in order, not by walking the map: a Go map has no order, so the
// help would come out shuffled differently on every run. And the order is not
// alphabetical on purpose, it goes by what gets done first.
func printHelp(w io.Writer) {
	fmt.Fprintln(w, "kanpachi, the Kanpachi client.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "  With no arguments it opens the wizard, driven with arrow keys.")
	fmt.Fprintln(w)
	for _, g := range groups {
		fmt.Fprintf(w, "%s\n", g.title)
		for _, n := range g.names {
			c := commands[n]
			left := n
			if c.args != "" {
				left += " " + c.args
			}
			fmt.Fprintf(w, "  %-26s %s\n", left, c.brief)
		}
		fmt.Fprintln(w)
	}
	fmt.Fprintln(w, "Options, valid in any position:")
	fmt.Fprintln(w, "  --nick <name>              how the room sees you. Remembered")
	fmt.Fprintln(w, "  --json                     the daemon's raw answer, unrendered")
	fmt.Fprintln(w, "  --data <dir>               a different data directory")
	fmt.Fprintln(w, "  --pipe <path>              a different control channel")
	fmt.Fprintln(w, "  --timeout <duration>       how long to wait for an answer (90s default)")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "`kanpachi <command> --help` explains one command, its flags and its examples.")
}

// printCommandHelp writes one command's page.
//
// The caller has already checked that the name is in the table, which is what
// lets this one be total: an unknown command is answered with the list, and
// answering it here would be the same message written twice.
func printCommandHelp(w io.Writer, name string) {
	c := commands[name]

	line := "kanpachi " + name
	if full := c.usage; full != "" {
		line += " " + full
	} else if c.args != "" {
		line += " " + c.args
	}
	fmt.Fprintln(w, line)
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  %s\n", c.brief)

	if c.help != "" {
		fmt.Fprintln(w)
		for _, l := range strings.Split(strings.TrimRight(c.help, "\n"), "\n") {
			fmt.Fprintln(w, l)
		}
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "  `kanpachi help` lists every command. --json, --timeout, --nick, --data")
	fmt.Fprintln(w, "  and --pipe work here too, in any position.")
}

// connectionHint says what to look at when the channel will not open.
//
// # Why it has to say two things and not one
//
// Because the two frequent failures look the SAME from here: a socket that will
// not open is the same with the service stopped as with the service running and
// no `sudo`. Naming only one sends half the people to look where it is not.
//
// # Why this one does look at `runtime.GOOS` and the rest of the repository does
// not
//
// Because what changes here is the PROSE, not what the program does. The rule
// about splitting by file exists so that a system with no wiring is a link error
// instead of a silent `default` applying somebody else's decision; a help text
// that comes out generic on a new system breaks nothing, and splitting it would
// cost three files for three sentences.
func connectionHint(options) string {
	switch runtime.GOOS {
	case "linux":
		return "  Check both, they look the same from here:\n" +
			"    systemctl status kanpachid       is it running?\n" +
			"    sudo kanpachi ...                the channel and the token belong to root"
	case "windows":
		return fmt.Sprintf("  Is the Kanpachi service running?\n"+
			"    sc query kanpachi-daemon\n"+
			"  In console mode the channel is another one: --pipe %s", pipe.ConsoleName)
	default:
		return "  The local channel is written for Windows and for Linux."
	}
}

// parseDuration reads one of the durations Go accepts: `30s`, `2m`, `1h30m`.
func parseDuration(s string) (time.Duration, error) {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, badUsage("--timeout %q is not a duration: write it 30s, 2m, 1h30m", s)
	}
	if d <= 0 {
		return 0, badUsage("--timeout has to be positive, got %s", d)
	}
	return d, nil
}
