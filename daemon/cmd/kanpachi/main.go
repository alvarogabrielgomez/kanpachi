// Command kanpachi is the client: it opens rooms, enters them, watches and
// measures.
//
// It is what gets installed at `/usr/bin/kanpachi`, and on the headless variant
// it is the ONLY client there is. It does nothing on its own: everything it
// runs, it asks the daemon for over the local channel, with the same closed list
// of methods the Windows interface uses. A CLI that opened a port of its own
// would be a second door into the machine, and the small surface is this
// product's main mitigation.
//
// # With a subcommand it is scriptable; with nothing it is a wizard
//
//	kanpachi status              what there is now
//	kanpachi host "My room"      open one
//	kanpachi join VA3B-SF5L      enter
//	kanpachi                     the wizard, with arrow keys
//
// # Why it asks for `sudo` on Linux and not on Windows
//
// Because the token lives in the data directory and the permissions there are
// different on purpose. On Windows `ProgramData\Kanpachi` lets the machine's
// users read, because the interface runs unelevated and needs to talk. On Linux
// there is no interface to serve, so the directory belongs to root and the
// socket is 0600: whoever administers Kanpachi can already be root on that
// server, and a path that skipped `sudo` would only drop the record of who did
// what. See `daemon/paths` and `pipe_linux.go`.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/daemon/paths"
	"github.com/accentiostudios/kanpachi/daemon/transport/client"
	"github.com/accentiostudios/kanpachi/daemon/transport/pipe"
	"github.com/accentiostudios/kanpachi/daemon/transport/protocol"
)

// options are the global flags already resolved.
type options struct {
	data    string
	channel string
	nick    string
	// json asks for the daemon's answer WITHOUT rendering.
	//
	// It is what makes this usable inside a script: what gets rendered changes
	// whenever somebody improves a screen, and the wire shape does not, because
	// it is a contract with the Windows interface and it has guardians of its
	// own.
	json bool
	// help is `--help` in any position, and it names no command by itself.
	//
	// Which page it prints depends on what else was written: with a subcommand
	// it is that subcommand's page, with nothing it is the whole list. It is
	// read here and not turned into the `help` command, which is what used to
	// happen: `kanpachi game --help` became `kanpachi game help` and went off to
	// activate a game called "help".
	help bool
	// timeoutText is what was written in `--timeout`, and timeout is it parsed.
	//
	// Both, and not only the second: the raw text is kept because the error for
	// a value that does not work has to be able to quote it as they wrote it.
	// Zero in `timeout` means nobody asked for one, and
	// [timing.PipeDefaultTimeout] applies.
	timeoutText string
	timeout     time.Duration
}

func main() {
	// Interruption is handled, and it is worth saying what it does NOT do:
	// cutting the CLI off does not close the room. The room lives in the daemon,
	// which keeps running; this only drops the connection. That is the
	// difference with `roomprobe`, which IS the session and therefore has to
	// shut it down on the way out.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	code := run(ctx, os.Args[1:])
	os.Exit(code)
}

func run(ctx context.Context, args []string) int {
	op, rest, err := parseFlags(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "kanpachi:", err)
		return 2
	}

	// `--help` answers before anything else, and before opening any connection:
	// somebody reading a command's flags is not asking the daemon for anything,
	// and a help page that failed because the service was down would be the
	// worst possible answer to "how do I write this".
	if op.help {
		if len(rest) == 0 {
			printHelp(os.Stdout)
			return 0
		}
		if _, ok := commands[rest[0]]; !ok {
			fmt.Fprintf(os.Stderr, "kanpachi: there is no %q command\n\n", rest[0])
			printHelp(os.Stderr)
			return 2
		}
		printCommandHelp(os.Stdout, rest[0])
		return 0
	}

	// With no subcommand, the wizard. It is what makes this usable without
	// having read anything, which on a freshly installed server is the normal
	// case.
	if len(rest) == 0 {
		if err := assistant(ctx, op); err != nil {
			return report(op, err)
		}
		return 0
	}

	cmd, ok := commands[rest[0]]
	if !ok {
		fmt.Fprintf(os.Stderr, "kanpachi: there is no %q command\n\n", rest[0])
		printHelp(os.Stderr)
		return 2
	}
	if err := cmd.run(ctx, op, rest[1:]); err != nil {
		return report(op, err)
	}
	return 0
}

// report tells the failure, in whichever form fits, and picks the exit code to
// finish with. The codes are handed out by [classify].
//
// The TWO forms leave by different doors, and that is the contract: the prose
// on stderr for whoever reads, the JSON on stdout for whoever parses. Never
// both.
func report(op options, err error) int {
	if errors.Is(err, errInterrupted) || errors.Is(err, context.Canceled) {
		// No message: whoever pressed Ctrl+C already knows what they did, and
		// 130 is what everybody uses for "a signal killed it".
		return 130
	}
	code, exit := classify(err)
	if op.json {
		// **With `--json`, a failure comes out as a CODE and nothing else.**
		//
		// The prose stays out, and not for looks. This is the place where "never
		// plain text with the reason" can break without anybody watching: the
		// output of a script is not read by a person, so one extra message there
		// bothers nobody and stays forever. With the registry asking for a
		// password, the message that used to reach this far could tell an
		// expired token from an invalid one, which is exactly the distinction
		// the API goes out of its way not to make.
		//
		// It goes to STDOUT and not to stderr, and with that `--json` answers
		// with one JSON document always, whatever happens: a script does one
		// parse instead of two checks.
		fmt.Printf("{\"error\":{\"code\":%q}}\n", code)
		return exit
	}
	fmt.Fprintln(os.Stderr, "kanpachi:", err)
	if fix := fixFor(code); fix != "" {
		fmt.Fprintf(os.Stderr, "  %s\n", fix)
	}
	return exit
}

// fixFor is the command that resolves that failure, or empty.
//
// # Why here and not in each subcommand
//
// Because the two failures that have a fix come out of SEVERAL commands:
// hosting, renewing the code and renaming the room all go through the registry.
// Repeating the advice in each one would give three places for it to age, and
// the one that gets forgotten is always the least used.
//
// **It does not print with `--json`**, by the same rule as the prose: there the
// code comes out and whoever parses decides what to do with it. That is why it
// lives in the lower branch of [report] and not before it.
func fixFor(code protocol.Code) string {
	switch code {
	case protocol.CodeNoOwnSeed:
		return "This machine has no registry yet:  kanpachi seed <host>"
	case protocol.CodeSeedPassword:
		return "That registry asks for a password to host:  kanpachi password"
	}
	return ""
}

// classify says which code a failure leaves with, and with which number.
//
// # Why there are three exit codes and not one
//
// Because a script has to be able to tell "the daemon said no" from "there is no
// daemon". The first is an answer from the product and the script can carry on;
// the second means nothing that comes after is going to work.
//
// The text code is the PROTOCOL's whenever there is one, so a script looks at the
// same word the interface looks at. The two that do not come from the daemon
// carry a name of their own so those do not have to be guessed from the number
// either.
func classify(err error) (protocol.Code, int) {
	if c := client.Code(err); c != "" {
		return c, 1
	}
	var bad *usageError
	if errors.As(err, &bad) {
		return "usage", 2
	}
	var no *refusalError
	if errors.As(err, &no) {
		return "refused", 1
	}
	return "no_daemon", 3
}

// usageError is a command written wrong, which is the fault of whoever wrote it
// and not of the daemon. It has a type of its own so [report] can give it its
// code.
type usageError struct{ msg string }

func (e *usageError) Error() string { return e.msg }

func badUsage(format string, args ...any) error {
	return &usageError{msg: fmt.Sprintf(format, args...)}
}

// refusalError is the product saying no, decided on this side.
//
// # Why a bare error will not do
//
// Because a bare error falls into 3, which means "could not talk to the daemon",
// and that would be a lie: the daemon answered perfectly and what it said is
// that there is no room. A script looking at the code to decide whether to retry
// would retry forever. Running `kanpachi link` with no room found it, which used
// to exit 3.
type refusalError struct{ msg string }

func (e *refusalError) Error() string { return e.msg }

func refuse(format string, args ...any) error {
	return &refusalError{msg: fmt.Sprintf(format, args...)}
}

// errInterrupted is somebody pressing Ctrl+C inside a question.
//
// It is an ANSWER, not a failure, and that is why it has a sentinel of its own.
// Inside an interactive question the interruption does not arrive as a signal:
// survey leaves the console in a mode where the only thing that happens is that
// the question returns this.
var errInterrupted = errors.New("interrupted")

// parseFlags splits the arguments into global flags and the rest.
//
// By hand and not with `flag`, and the reason is ordering: `flag.Parse` stops
// reading at the first argument that is not a flag, so `kanpachi host --nick
// pepe` would hand `--nick` to the subcommand instead of to the session. With
// this the global flags work in any position, which is how they actually get
// written.
func parseFlags(args []string) (options, []string, error) {
	op := options{data: paths.Data(), channel: pipe.Name}
	var rest []string

	for i := 0; i < len(args); i++ {
		a := args[i]
		// After the first subcommand, a `--something` may belong to it. Only the
		// global ones get intercepted, and they are in this list and nowhere
		// else.
		value := func(name string) (string, error) {
			if i+1 >= len(args) {
				return "", badUsage("%s is missing its value", name)
			}
			i++
			return args[i], nil
		}
		var err error
		switch a {
		case "--data", "-data":
			op.data, err = value(a)
		case "--pipe", "-pipe", "--socket":
			op.channel, err = value(a)
		case "--nick", "-nick", "--nickname":
			op.nick, err = value(a)
		case "--timeout", "-timeout":
			op.timeoutText, err = value(a)
		case "--json", "-json":
			op.json = true
		case "--help", "-help", "-h":
			op.help = true
		default:
			rest = append(rest, a)
		}
		if err != nil {
			return op, nil, err
		}
	}

	// The deadline is parsed HERE and not where it gets used, and that ordering
	// is the point: it is a flag value, so a bad one has to be rejected without
	// talking to anybody. It was validated inside `dial`, after the connection
	// was already open, so `kanpachi --timeout abc status` with the daemon down
	// reported the daemon being down. Two wrong things at once, and the one it
	// named was the one the person had not caused.
	if op.timeoutText != "" {
		d, err := parseDuration(op.timeoutText)
		if err != nil {
			return op, nil, err
		}
		op.timeout = d
	}

	// The nickname, for the same reason and in the same place: it is a flag
	// value, so a bad one gets rejected without talking to anybody. Rejecting it
	// here does not protect the daemon, which validates it anyway because it is
	// the boundary; it makes the message arrive before a connection gets opened.
	if op.nick != "" {
		if _, err := domain.ParseNickname(op.nick); err != nil {
			return op, nil, badUsage("--nick %q is not valid: %v", op.nick, err)
		}
	}
	return op, rest, nil
}

// dial connects and says hello.
//
// The error carries what to do about it, and that is not decoration: the most
// frequent failure of this binary is going to be that the service is not running
// or that `sudo` is missing, and both look the same from here, a socket that
// will not open, so the message names both.
func dial(op options) (*client.Client, error) {
	c, err := client.Open(op.channel, op.data)
	if err != nil {
		return nil, fmt.Errorf("%w\n%s", err, connectionHint(op))
	}
	if op.timeout > 0 {
		c.Plazo = op.timeout
	}
	return c, nil
}
