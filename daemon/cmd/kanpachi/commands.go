package main

// The command table, which is the mirror of the closed list of methods.
//
// A mirror and not an automatic wrapper: there are protocol methods that are not
// a command and should not be one. `show_ui` and `pending_invite` belong to the
// desktop interface; `progress` and `cancel` get asked for over a separate
// connection while another one waits, which is a way of using this rather than an
// order somebody types. What is left is what a person wants to do to their room.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/accentiostudios/kanpachi/core/timing"
	"github.com/accentiostudios/kanpachi/daemon/transport/client"
	"github.com/accentiostudios/kanpachi/daemon/transport/protocol"
)

type command struct {
	// args is how what follows the name is written, for the list in
	// `kanpachi help`. It shares a column with every other command, so it stays
	// short: anything longer belongs in usage and help.
	args string
	// usage is the whole line for this command's own page, flags included. It
	// falls back to args when there is nothing more to say.
	usage string
	brief string
	// help is the body of `kanpachi <name> --help`: every flag, what each one
	// changes, and an example that can be pasted. Empty means the brief line is
	// the whole answer, which is true of no command that takes an argument.
	help string
	// run takes the context so a long operation can be cut with Ctrl+C. Creating
	// a room takes close to a minute.
	run func(ctx context.Context, op options, args []string) error
}

// groups orders the help by what gets done first, and not alphabetically.
//
// **A command that is not here does not show up in `kanpachi help`**, even
// though it exists and works. It happened to `quarantine`, which shipped in
// 0.5.0 with its three faces and without one line saying it existed. Adding a
// command to the table below means adding it here too, and nothing checks that:
// it is the one list in the binary that can fall behind in silence.
var groups = []struct {
	title string
	names []string
}{
	{"The room:", []string{"status", "watch", "host", "join", "leave", "link", "rotate", "rename"}},
	{"Who is in:", []string{"members", "kick"}},
	{"The game:", []string{"games", "game", "profile"}},
	{"Checking:", []string{"exposure", "diag", "probe", "protect"}},
	{"What was left from before:", []string{"pending", "resume", "discard", "last"}},
	{"The system:", []string{"name", "seed", "quarantine", "password", "doctor", "upgrade"}},
	{"Other:", []string{"version", "help"}},
}

// commands is the table. It gets filled in `init` and not in the declaration
// because several entries name each other (`help` walks `groups`, the wizard
// calls the same functions) and Go does not allow initialisation cycles.
var commands map[string]command

func init() {
	commands = map[string]command{
		"status": {brief: "what there is right now: room, members, network and protection",
			help: `  Takes no arguments. Prints the room, who is in it, the network and
  Kanpachi Protection, once. With --json it prints the daemon's answer
  instead of the screen, which is the shape a script should read.

    kanpachi status
    kanpachi --json status`,
			run: cmdStatus},

		"watch": {brief: "the same, redrawn until you press Ctrl+C",
			help: `  Takes no arguments. The same screen as status, redrawn every couple of
  seconds. Ctrl+C stops watching and does NOT close the room: the room
  lives in the daemon.

  It paints a screen, so it has no --json form. Inside a script, loop over
  ` + "`kanpachi status --json`" + ` instead.

    kanpachi watch`,
			run: cmdWatch},

		"host": {args: "[name]", usage: "[name] [--yes] [--quarantine on|off]",
			brief: "open a room and be its host",
			help: `  Opens a room on this machine's registry and makes you its host. It takes
  close to a minute: two adapters have to come up, the credential has to be
  exchanged and the MTU measured.

  The name is what the invited read. It travels inside the encrypted card
  and the registry never sees it. With no name, this machine's name is used.

  Everything after the command is the name, so quotes are optional.

    --yes                  do not ask about trusting the registry, and open
                           a foreign firewall if one blocks the room
    --quarantine on|off    answer the base-quarantine question without being
                           asked. It is answered once per machine, and --yes
                           does not answer it

    kanpachi host "Zomboid nights"
    kanpachi host --yes --quarantine off`,
			run: cmdHost},

		"join": {args: "<code|link>", usage: "<code|link> [--yes] [--quarantine on|off]",
			brief: "enter someone else's room",
			help: `  Enters somebody else's room. Every form of the invite works, as long as
  it carries the registry:

    VA3BSF5L@seed.example.com
    seed.example.com/VA3BSF5L
    https://seed.example.com/VA3BSF5L#key
    kanpachi://VA3BSF5L@seed.example.com

  Dashes and case do not matter. Joining does not change which registry
  this machine opens its own rooms on.

    --yes                  do not ask about trusting the registry, and open
                           a foreign firewall if one blocks the room
    --quarantine on|off    answer the base-quarantine question without being
                           asked

    kanpachi join VA3B-SF5L@seed.example.com`,
			run: cmdJoin},

		"leave": {brief: "close your room, leave someone else's, or stop going back to the last one",
			help: `  Takes no arguments. What it does depends on where you are:

    hosting       closes your room. The game ports close with it, everybody
                  inside drops, and the invite code stops resolving
    a guest       leaves the room. The host keeps it open
    going back    stops going back to the last room you were in

    kanpachi leave`,
			run: cmdLeave},

		"link": {brief: "the invite link, to copy and hand out",
			help: `  Takes no arguments. Prints the invite link and nothing else, so it drops
  straight into a $(...). With no room open it refuses.

  ` + "`kanpachi status`" + ` shows the shorter dictating form as well,
  CODE@registry, which is the one to read out loud.

    kanpachi link
    echo "come play: $(sudo kanpachi link)"`,
			run: cmdLink},

		"rotate": {brief: "renew the code: the links you handed out stop working",
			help: `  Takes no arguments. Issues a new invite code for the room you host. The
  links you already handed out stop resolving; whoever is already inside
  stays inside and the game does not drop.

  It is what closes the door on somebody. ` + "`kanpachi kick`" + ` removes
  them now, and this is what stops them coming back with the code they have.

    kanpachi rotate`,
			run: cmdRotate},

		"rename": {args: "<name>", brief: "rename the room",
			help: `  Renames the room you host. Everything after the command is the name, so
  quotes are optional.

  The name travels inside the encrypted card, so the registry never learns
  it. The invite code does not change.

    kanpachi rename "Zomboid nights"`,
			run: cmdRename},

		"members": {brief: "who is in, by which path, and with what latency",
			help: `  Takes no arguments. Who is in the room, the Kanpachi address of each one,
  whether they got there directly or through the relay, and the round trip.

  The address column is what a game connects to.

    kanpachi members`,
			run: cmdMembers},

		"kick": {args: "<name|ip>", brief: "kick someone out of the room",
			help: `  Removes somebody from the room you host. Takes either the name
  ` + "`kanpachi members`" + ` shows or their Kanpachi address. A name two
  members share is refused instead of guessed: write the address so you do
  not kick the wrong one.

  Kicking does not stop them coming back with the same code.
  ` + "`kanpachi rotate`" + ` is what does that.

    kanpachi kick alvaro
    kanpachi kick 100.71.3.4`,
			run: cmdKick},

		"games": {brief: "the game catalog, and which ones are installed",
			help: `  Takes no arguments. The whole catalog by id and name, with the ones this
  machine has installed marked. The id is what ` + "`kanpachi game`" + ` takes.

    kanpachi games`,
			run: cmdGames},

		"game": {args: "[id]", brief: "activate a game profile; with no id, close the ports",
			help: `  Activates a game profile, which is the only thing here that opens a port
  at all. The ports that game asks for open on the virtual adapter, on this
  machine, toward the addresses of the people currently in the room and
  nobody else. Kanpachi recalculates them whenever somebody joins or leaves.

  With no id it closes them again.

  ` + "`kanpachi games`" + ` lists the ids. ` + "`kanpachi profile`" + `
  describes one the catalog does not carry.

    kanpachi game project-zomboid
    kanpachi game`,
			run: cmdGame},

		"profile": {args: "<id>",
			usage: "<id> --name <name> [--tcp <list>] [--udp <list>] [--replace]",
			brief: "describe a game the catalog does not have",
			help: `  Describes a game the catalog does not carry, by the ports it needs, so
  ` + "`kanpachi game <id>`" + ` can open them.

  Saving it again under the same id UPDATES that profile instead of adding a
  second one, which is what lets a container run this on every start.

    --name <name>    how this machine lists it. Required
    --tcp <list>     TCP ports: single ones and ranges, comma separated
    --udp <list>     UDP ports, same shape
    --replace        overwrite a profile the catalog ships with this id

  One of --tcp and --udp is the least that describes anything. Between them
  they take eight entries, and a port needed on both protocols is written on
  both lists and spends two of the eight.

  A range that covers a port Kanpachi never opens is rejected whole, not
  trimmed: 440-450 is refused because 445 is inside it.

    kanpachi profile my-server --name "My modpack" --udp 16261-16262 --tcp 25565
    kanpachi game my-server`,
			run: cmdProfile},

		"exposure": {brief: "what Kanpachi has open, and toward whom",
			help: `  Takes no arguments. Every port Kanpachi has open, toward which addresses,
  and whether the rule was asked for and actually applied.

  An empty list and "could not be read" are different answers and print
  differently. A rule nobody asked for prints as such.

    kanpachi exposure`,
			run: cmdExposure},

		"diag": {brief: "the network as the engine sees it: NAT, UDP and MTU",
			help: `  Takes no arguments. What the engine sees of this machine's network: the
  kind of NAT in front of it, whether UDP gets through, the measured MTU,
  the room's subnet, and the round trip to the registry.

  It goes out to the network, so it takes a few seconds.

    kanpachi diag`,
			run: cmdDiag},

		"probe": {brief: "probe this machine FROM another one in the room",
			help: `  Takes no arguments. Asks another machine in the room to knock on this one
  and report what answered. It is the one measurement here that crosses the
  real network, so it needs somebody else in the room.

  Read the verdict carefully: "nobody answered" looks the same on a sealed
  machine as on one that is switched off, and it says so rather than
  reporting success.

    kanpachi probe`,
			run: cmdProbe},

		"protect": {brief: "put Kanpachi Protection back. It is idempotent",
			help: `  Takes no arguments. Puts back what Kanpachi Protection is made of: the
  gate, the room's rules, and the active game's ports. Running it with
  everything already in place changes nothing.

    kanpachi protect`,
			run: cmdProtect},

		"quarantine": {args: "[on|off]",
			brief: "close this PC's risky server ports on every network; bare, it tells the state",
			help: `  Closes this machine's file sharing, Remote Desktop and remote management,
  on every network it joins, whether Kanpachi is running or not. It is
  separate from any room and no room turns it on or off.

    on       close them. It repairs whatever is missing if part of it is gone
    off      leave them open
    (bare)   say what state this machine is in, and why sharing might be failing

  Both directions are idempotent, and both are answers: off is a decision,
  not the absence of one. Until it is answered, opening or joining a room
  from the terminal asks once.

    kanpachi quarantine
    kanpachi quarantine on`,
			run: cmdQuarantine},

		"pending": {brief: "whether a room was left open from the previous start",
			help: `  Takes no arguments. Says whether a room was left open when the daemon
  last stopped, and shows its code.

  ` + "`kanpachi resume`" + ` reopens it with that same code.
  ` + "`kanpachi discard`" + ` forgets it.

    kanpachi pending`,
			run: cmdPending},

		"resume": {brief: "reopen that room with the same code",
			help: `  Takes no arguments. Reopens the saved room with the same code, the same
  address range and the same link, so the invites you handed out keep
  working. It takes about as long as opening a room does.

  There is one case where the code changes anyway, and it says so loudly:
  the registry drops a room nobody republished for 21 days, and after that
  there is nothing to reopen under the old code.

    kanpachi resume`,
			run: cmdResume},

		"discard": {brief: "forget it",
			help: `  Takes no arguments. Forgets the saved room, so no start reopens it. The
  code goes with it.

    kanpachi discard`,
			run: cmdDiscard},

		"last": {brief: "the last room you entered as a guest",
			help: `  Takes no arguments. The last room this machine entered AS A GUEST, which
  is not the same thing as ` + "`kanpachi pending`" + `: that one is a room
  of yours that is still yours, this one is a saved code you have to enter
  with again.

  It also says whether Kanpachi is going back to it on its own.

    kanpachi last`,
			run: cmdLast},

		"name": {args: "[name]",
			brief: "the name rooms show you by, shared with the window; bare, it shows it",
			help: `  The name the other people in a room see, up to twelve letters and digits.
  The window, the wizard and this share one name, so setting it here sets it
  everywhere.

  With no name it prints the one in use. With none chosen, Kanpachi derives
  one from this machine's name, says so, and does not write it down.

  Nobody verifies it and no registry receives it: it travels inside the
  encrypted card.

    kanpachi name
    kanpachi name alvaro`,
			run: cmdName},

		"seed": {args: "[host]",
			brief: "the registry this machine opens rooms on; with no host, shows it",
			help: `  The registry this machine opens its rooms on. A host name, not a URL: no
  https:// and no trailing slash.

  With no host it prints the one configured, and suggests the last one you
  entered a room on if there is no answer yet. Entering somebody else's room
  never sets this on its own: the next room you open would be hosted on a
  stranger's server without anybody deciding it.

  The registry is checked to answer before it gets saved.

    kanpachi seed
    kanpachi seed kanpachi.accentio.dev`,
			run: cmdSeed},

		"password": {
			brief: "the password of a registry that asks for one to host. Never on the command line",
			help: `  The password of a registry that asks for one before it lets a machine
  host. Entering somebody else's room never needs one.

  It takes no arguments, and there is no --password flag and never will be:
  on Linux any user reads /proc/<pid>/cmdline and the shell keeps a history.
  It is typed, masked, or it does not get sent.

  Redirected input is read instead, which is the door for a script. A file
  at mode 0600 piped in never appears in an argument list:

    kanpachi password
    kanpachi password < /root/seed-password`,
			run: cmdPassword},

		"doctor": {args: "[--fix]", brief: "what this needs to work, and what is broken",
			help: `  What this machine needs for Kanpachi to work and what is broken, in two
  levels: what the environment looks like, checked here without the daemon,
  and what the daemon has already measured, asked for only if it answers.

  It writes nothing without --fix.

    --fix    repair what belongs to Kanpachi: its units, its tables, its
             socket, its device node. Each fix is re-measured afterwards
             instead of being assumed

  What belongs to the operator gets named with the exact command and is left
  alone even with --fix, with one exception: a ufw or a firewalld that would
  swallow the room's inbound gets opened, written down, and closed again
  when the room ends or on the next service start.

    kanpachi doctor
    sudo kanpachi doctor --fix`,
			run: cmdDoctor},

		"upgrade": {args: "[--check]",
			usage: "[--check] [--version <v>] [--force] [--yes]",
			brief: "fetch the new version. Restarts the service, so the room drops",
			help: `  Downloads the published .deb, verifies its SHA-256 against the release
  manifest, and hands it to apt. It never writes the files itself: the
  daemon, the engine, the CLI, the two units and the quarantine have to
  agree with each other, and the package is what knows how to place all six.

  Linux and amd64 only. On Windows the update goes through the installer.

  Installing restarts the service, which drops the open room, so it asks
  first and names that.

    --check          say what is published and stop. Answers from what this
                     machine already found, here or in the window, because a
                     published version does not get unpublished
    --version <v>    install that version instead of the latest, which is
                     how you go back
    --force          do it even when the shortcut says there is nothing to
                     do: install a version whose number already matches,
                     which is how a republished one gets picked up, and ask
                     the channel again instead of answering from before
    --yes            do not ask. Required when there is no terminal

    kanpachi upgrade --check
    sudo kanpachi upgrade
    sudo kanpachi upgrade --version v0.6.3 --yes`,
			run: cmdUpgrade},

		"version": {brief: "which version this is",
			help: `  Takes no arguments. Which version this is, which commit it was built
  from, and which engine sits next to it. The daemon, the terminal and the
  window come out of one cut and travel together, so one number covers all
  three.

    kanpachi version
    kanpachi --json version`,
			run: cmdVersion},

		"help": {args: "[command]",
			brief: "this, or one command's flags and examples",
			help: `  With no argument, the list of commands. With one, that command's page:
  what it does, every flag it takes, and an example.

  ` + "`kanpachi <command> --help`" + ` prints the same page.

    kanpachi help
    kanpachi help profile
    kanpachi profile --help`,
			run: cmdHelp},
	}
}

// cmdHelp is `help` with an optional command name.
//
// It shares its whole body with `--help`, which is read in [parseFlags]: two
// spellings of one question, answered by one renderer. An unknown name is a
// usage error rather than a silent fallback to the full list, because somebody
// who wrote `kanpachi help hsot` wants to be told, not handed the index.
func cmdHelp(_ context.Context, _ options, args []string) error {
	if len(args) == 0 {
		printHelp(os.Stdout)
		return nil
	}
	if len(args) > 1 {
		return badUsage("help explains one command at a time")
	}
	if _, ok := commands[args[0]]; !ok {
		return badUsage("there is no %q command. `kanpachi help` lists them", args[0])
	}
	printCommandHelp(os.Stdout, args[0])
	return nil
}

// ─── Asking the daemon for things ────────────────────────────────────────────

// request makes the call and, with `--json`, prints the raw answer and reports
// that it already printed.
//
// The raw form comes out BEFORE anything interprets it, and on purpose: what
// makes this scriptable is the wire shape, which is a contract with guardians,
// and not what gets rendered, which changes whenever somebody improves a screen.
// It comes out even when the daemon answered with an error, because a half-done
// kick brings a result and an error at once and the script needs both.
func request[T any](c *client.Client, op options, m protocol.Method, params any) (T, bool, error) {
	var out T
	raw, err := c.Call(m, params)
	if op.json && len(raw) > 0 {
		var indented bytes.Buffer
		if e := json.Indent(&indented, raw, "", "  "); e == nil {
			fmt.Println(indented.String())
		} else {
			// Unindented before anything else: what matters is that the raw
			// answer comes out.
			fmt.Println(string(raw))
		}
		return out, true, err
	}
	if len(raw) > 0 {
		if e := json.Unmarshal(raw, &out); e != nil && err == nil {
			return out, false, fmt.Errorf("parsing the answer to %s: %w", m, e)
		}
	}
	return out, false, err
}

// withRoom is the mould of nearly all of them: open, ask, paint the room.
func withRoom(op options, m protocol.Method, params any) error {
	c, err := dial(op)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	st, done, err := request[protocol.RoomView](c, op, m, params)
	if done {
		return err
	}
	// The room gets painted EVEN IF there is an error, and that ordering
	// matters: a half-done kick answers with the list already missing the kicked
	// member plus the notice of what could not be closed, and whoever looks only
	// at the error throws away the state they just asked for. See
	// [protocol.Response].
	if st.Conn != "" {
		printRoom(os.Stdout, st, catalogForAddress(c, st))
	}
	return err
}

// catalogForAddress fetches the catalog only when there is an address to build.
//
// The connect address needs the active profile's port, and the port does not
// travel in the room view: it travels in the catalog, the same place the window
// reads it from. With no game active there is nothing to build, so nothing is
// asked for and the common case costs no extra round trip.
//
// A failure here returns nil and stays quiet. Not being able to name the port
// is not a reason to withhold the room: without it the address line is skipped
// and everything else prints as it did.
func catalogForAddress(c *client.Client, st protocol.RoomView) []protocol.GameView {
	if st.Game == "" {
		return nil
	}
	catalog, err := client.Ask[[]protocol.GameView](c, protocol.MethodListGames, nil)
	if err != nil {
		return nil
	}
	return catalog
}

// ─── The room ────────────────────────────────────────────────────────────────

func cmdStatus(_ context.Context, op options, _ []string) error {
	return withRoom(op, protocol.MethodStatus, nil)
}

// cmdWatch redraws the state until somebody cuts it off.
//
// # It reuses the connection, and that is not a cosmetic saving
//
// Opening one per frame would be a handshake a second and would take one of the
// listener's slots every time. There are eight slots and the daemon counts them.
func cmdWatch(ctx context.Context, op options, _ []string) error {
	if op.json {
		return badUsage("watch paints a screen, so it has no --json form.\n" +
			"  For a script, `kanpachi status --json` inside a loop")
	}
	c, err := dial(op)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	// Once, outside the loop. The catalog is what the address line needs and it
	// does not change while this runs, so asking per frame would be a request a
	// second for an answer that is already in hand. Which profile is active can
	// change mid-loop, and that is read from the room view every frame.
	catalog, err := client.Ask[[]protocol.GameView](c, protocol.MethodListGames, nil)
	if err != nil {
		catalog = nil
	}

	for {
		st, err := client.Ask[protocol.RoomView](c, protocol.MethodStatus, nil)
		if err != nil {
			return err
		}
		clearScreen(os.Stdout)
		printRoom(os.Stdout, st, catalog)
		fmt.Println("  [Ctrl+C] to leave. This does NOT close the room: the room lives in the daemon.")

		select {
		case <-ctx.Done():
			return errInterrupted
		case <-time.After(timing.LiveViewRefresh):
		}
	}
}

func cmdHost(_ context.Context, op options, args []string) error {
	args, noQuestions := takeYes(args)
	args, quarantine, err := takeQuarantine(args)
	if err != nil {
		return err
	}
	nick, err := nickname(op)
	if err != nil {
		return err
	}
	// Opening a room displaces the same things entering somebody else's does,
	// and it goes through the same gate: whoever already hosts one would be
	// closing it.
	replace, err := confirmDisplacement(op, noQuestions)
	if err != nil {
		return err
	}
	// The registry gets read and confirmed BEFORE the minute-long wait is
	// announced. Asking after "this takes a minute" would make the question look
	// like part of the wait, and what is being decided is whether to start it.
	seed, err := registryOfThisMachine(op)
	if err != nil {
		return err
	}
	if err := trustTheRegistry(op, seed, "Opening a room", noQuestions); err != nil {
		return err
	}
	name := strings.Join(args, " ")
	if name == "" {
		// The name travels INSIDE the encrypted card and the seed never learns
		// it, so using the machine's leaks nothing and saves the question in the
		// normal case of a server.
		name = machineName()
	}
	if !op.json {
		fmt.Println("Opening the room. This takes about a minute: two adapters have to")
		fmt.Println("come up, the credential has to be exchanged, and the MTU measured.")
	}
	return withRoomConsenting(op, protocol.MethodCreateRoom, noQuestions, quarantine,
		func(allowFirewall bool, quarantine string) any {
			return struct {
				Nickname      string `json:"nickname"`
				Name          string `json:"name"`
				Replace       bool   `json:"replace,omitempty"`
				AllowFirewall bool   `json:"allow_firewall,omitempty"`
				Quarantine    string `json:"quarantine,omitempty"`
			}{nick, name, replace, allowFirewall, quarantine}
		})
}

// cmdJoin passes the text EXACTLY as the person pasted it.
//
// No stripping the dashes and no prefixing the seed's host: `domain.ParseRoom`
// accepts the six documented forms and is the product's hostile input boundary.
// Normalising before calling it is testing something other than what you meant
// to test.
func cmdJoin(_ context.Context, op options, args []string) error {
	args, noQuestions := takeYes(args)
	args, quarantine, err := takeQuarantine(args)
	if err != nil {
		return err
	}
	if len(args) == 0 {
		return badUsage("join needs the code or the link.\n" +
			"  Every form works as long as it carries the registry:\n" +
			"  VA3BSF5L@seed.example.com, seed.example.com/VA3BSF5L,\n" +
			"  https://seed.example.com/VA3BSF5L#key and kanpachi://VA3BSF5L@seed.example.com")
	}
	nick, err := nickname(op)
	if err != nil {
		return err
	}
	// Empty when the text does not parse, and there it does NOT ask: whoever has
	// to explain what that code is missing is the daemon, which is the hostile
	// input boundary. Asking about a registry that could not be read would be
	// naming a machine somebody made up.
	// What is in the way gets asked BEFORE the registry, and the ordering
	// matters: if somebody is going to say no, let them say it before reading a
	// paragraph about a machine they are not going to use.
	replace, err := confirmDisplacement(op, noQuestions)
	if err != nil {
		return err
	}
	if seed := registryFromCode(args[0]); seed != "" {
		if err := trustTheRegistry(op, seed, "Entering that room", noQuestions); err != nil {
			return err
		}
	}
	if !op.json {
		fmt.Println("Entering...")
	}
	return withRoomConsenting(op, protocol.MethodJoinRoom, noQuestions, quarantine,
		func(allowFirewall bool, quarantine string) any {
			return struct {
				Code          string `json:"code"`
				Nickname      string `json:"nickname"`
				Replace       bool   `json:"replace,omitempty"`
				AllowFirewall bool   `json:"allow_firewall,omitempty"`
				Quarantine    string `json:"quarantine,omitempty"`
			}{args[0], nick, replace, allowFirewall, quarantine}
		})
}

func cmdLeave(_ context.Context, op options, _ []string) error {
	return withRoom(op, protocol.MethodLeaveRoom, nil)
}

func cmdLink(_ context.Context, op options, _ []string) error {
	c, err := dial(op)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	v, done, err := request[struct {
		Link string `json:"link"`
	}](c, op, protocol.MethodInviteLink, nil)
	if done || err != nil {
		return err
	}
	if v.Link == "" {
		return refuse("no room is open, so there is no link to hand out")
	}
	// Bare and undecorated: this gets used inside a `$(...)`.
	fmt.Println(v.Link)
	return nil
}

func cmdRotate(_ context.Context, op options, _ []string) error {
	if !op.json {
		fmt.Println("Renewing the code. The links already handed out stop working;")
		fmt.Println("whoever is inside stays inside.")
	}
	return withRoom(op, protocol.MethodRotateInviteCode, nil)
}

func cmdRename(_ context.Context, op options, args []string) error {
	if len(args) == 0 {
		return badUsage("rename needs the new name")
	}
	return withRoom(op, protocol.MethodRenameRoom, struct {
		Name string `json:"name"`
	}{strings.Join(args, " ")})
}

// ─── Who is inside ───────────────────────────────────────────────────────────

func cmdMembers(_ context.Context, op options, _ []string) error {
	c, err := dial(op)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	st, done, err := request[protocol.RoomView](c, op, protocol.MethodStatus, nil)
	if done || err != nil {
		return err
	}
	printMembers(os.Stdout, st)
	return nil
}

// cmdKick accepts the name as well as the IP, which is why it asks for the state
// first.
//
// The protocol only understands the address, which is right: a name is not
// unique in a room and kicking the wrong person cannot be undone. Resolving it
// here lets whoever types use what they see on screen, and **it refuses when the
// name appears twice** instead of picking one.
func cmdKick(_ context.Context, op options, args []string) error {
	if len(args) == 0 {
		return badUsage("kick needs who: their name in the room or their virtual IP")
	}
	c, err := dial(op)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	st, err := client.Ask[protocol.RoomView](c, protocol.MethodStatus, nil)
	if err != nil {
		return err
	}
	ip, err := resolveMember(st, args[0])
	if err != nil {
		return err
	}

	v, done, err := request[protocol.RoomView](c, op, protocol.MethodKickMember, struct {
		IP string `json:"ip"`
	}{ip})
	if done {
		return err
	}
	if v.Conn != "" {
		printMembers(os.Stdout, v)
	}
	// A half-done kick brings both things and the error does not get swallowed:
	// it means the person left the room and some port of theirs stayed open.
	return err
}

// resolveMember translates what a person typed into a virtual IP.
func resolveMember(st protocol.RoomView, who string) (string, error) {
	var found []protocol.PeerView
	for _, p := range st.Peers {
		if p.IP == who || strings.EqualFold(p.Name, who) {
			found = append(found, p)
		}
	}
	switch len(found) {
	case 0:
		return "", badUsage("there is no %q in the room.\n  `kanpachi members` says who is", who)
	case 1:
		if found[0].Self {
			return "", badUsage("that is you. To leave the room it is `kanpachi leave`")
		}
		return found[0].IP, nil
	default:
		var ips []string
		for _, p := range found {
			ips = append(ips, p.IP)
		}
		return "", badUsage("there are %d members called %q: %s.\n"+
			"  Write the IP so you do not kick the wrong one",
			len(found), who, strings.Join(ips, ", "))
	}
}

// ─── The game ────────────────────────────────────────────────────────────────

func cmdGames(_ context.Context, op options, _ []string) error {
	c, err := dial(op)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	games, done, err := request[[]protocol.GameView](c, op, protocol.MethodListGames, nil)
	if done || err != nil {
		return err
	}
	printGames(os.Stdout, games)
	return nil
}

// cmdGame activates a profile, and with no id closes them all.
//
// The empty id is legal in the protocol and means exactly that, so it does not
// get rejected here: whoever writes a bare `kanpachi game` is saying "stop
// having open ports", which is an order that makes sense.
func cmdGame(_ context.Context, op options, args []string) error {
	id := ""
	if len(args) > 0 {
		id = args[0]
	}
	return withRoom(op, protocol.MethodActivateProfile, struct {
		Game string `json:"game"`
	}{id})
}

// ─── Checking ────────────────────────────────────────────────────────────────

func cmdExposure(_ context.Context, op options, _ []string) error {
	c, err := dial(op)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	v, done, err := request[protocol.ExposureView](c, op, protocol.MethodExposure, nil)
	if done || err != nil {
		return err
	}
	printExposure(os.Stdout, v)
	return nil
}

func cmdDiag(_ context.Context, op options, _ []string) error {
	c, err := dial(op)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	if !op.json {
		fmt.Println("Measuring. This goes out to the network, so it takes a few seconds.")
	}
	v, done, err := request[protocol.NetView](c, op, protocol.MethodDiagReport, nil)
	if done || err != nil {
		return err
	}
	printNetwork(os.Stdout, v)
	return nil
}

func cmdProbe(_ context.Context, op options, _ []string) error {
	c, err := dial(op)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	if !op.json {
		fmt.Println("Asking another machine in the room to probe this one.")
	}
	v, done, err := request[protocol.ProbeView](c, op, protocol.MethodProbeHost, nil)
	if done || err != nil {
		return err
	}
	printProbe(os.Stdout, v)
	return nil
}

func cmdProtect(_ context.Context, op options, _ []string) error {
	return withRoom(op, protocol.MethodReapplyProtection, nil)
}

// ─── What was left from before ───────────────────────────────────────────────

func cmdPending(_ context.Context, op options, _ []string) error {
	c, err := dial(op)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	v, done, err := request[struct {
		Found bool                   `json:"found"`
		Room  protocol.SavedRoomView `json:"room"`
	}](c, op, protocol.MethodSavedRoom, nil)
	if done || err != nil {
		return err
	}
	if !v.Found {
		fmt.Println("No room was left from the previous start.")
		return nil
	}
	fmt.Printf("  Saved room      %s\n", v.Room.Name)
	fmt.Printf("  Code            %s@%s\n", v.Room.Code, v.Room.Seed)
	if v.Room.Game != "" {
		fmt.Printf("  Game            %s\n", v.Room.Game)
	}
	if v.Room.SavedAt != "" {
		fmt.Printf("  Saved           %s\n", v.Room.SavedAt)
	}
	fmt.Println("\n  `kanpachi resume` reopens it with the same code. `kanpachi discard` forgets it.")
	return nil
}

func cmdResume(_ context.Context, op options, args []string) error {
	_, noQuestions := takeYes(args)
	// Reopening is entering, so it goes through the same gate as `host` and
	// `join`. The case it is here for is this machine holding both state files at
	// once: a room of its own to reopen, and a return to somebody else's already
	// in flight.
	replace, err := confirmDisplacement(op, noQuestions)
	if err != nil {
		return err
	}
	if !op.json {
		fmt.Println("Reopening the previous room with its same code.")
	}
	return withRoom(op, protocol.MethodResumeRoom, struct {
		Replace bool `json:"replace,omitempty"`
	}{replace})
}

func cmdDiscard(_ context.Context, op options, _ []string) error {
	c, err := dial(op)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	_, done, err := request[struct{}](c, op, protocol.MethodDiscardSavedRoom, nil)
	if done || err != nil {
		return err
	}
	fmt.Println("Forgotten.")
	return nil
}

// cmdLast shows the last room this machine entered AS A GUEST.
//
// It is not the same as `pending`, and confusing the two is the mistake this
// separation exists to avoid: that one belongs to the host and is still theirs,
// this one is a saved code you have to enter with again, with a credential
// exchanged and the host watching whoever comes in.
func cmdLast(_ context.Context, op options, _ []string) error {
	c, err := dial(op)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	v, done, err := request[struct {
		Found bool                  `json:"found"`
		Room  protocol.LastRoomView `json:"room"`
	}](c, op, protocol.MethodLastRoom, nil)
	if done || err != nil {
		return err
	}
	if !v.Found {
		fmt.Println("There is no previous room saved.")
		return nil
	}
	fmt.Printf("  %s  %s@%s  (as %s)\n", v.Room.Name, v.Room.Code, v.Room.Seed, v.Room.Nick)
	// Whether it comes back on its own is the one thing worth knowing about a
	// saved room, and the file's presence does not say it: it outlives leaving
	// and being kicked alike. Only the flag does.
	if v.Room.AutoReturn {
		fmt.Println("\n  Kanpachi is going back to it by itself. `kanpachi leave` stops that.")
		return nil
	}
	fmt.Printf("\n  It does not go back on its own. To go back:  kanpachi join %s@%s\n",
		v.Room.Code, v.Room.Seed)
	return nil
}

// ─── This machine's registry ─────────────────────────────────────────────────

// cmdQuarantine is the base quarantine's switch, and its reading.
//
// `on` and `off` ARE the decision, in whichever direction they say, and both are
// idempotent: turning it on with everything in place repairs what is missing,
// turning it off with nothing in place is already done. Bare, it tells the state
// with the symptom→cause bridge, which is what somebody arriving with "I cannot
// share a folder" and no idea of the word quarantine is going to read. With
// `--json`, the third face prints the whole state.
//
// Turning it off does not ask: the command as written IS the intent, same as
// `rotate`. What it does do is tell the one-line truth about what just happened.
func cmdQuarantine(_ context.Context, op options, args []string) error {
	set := ""
	if len(args) > 0 {
		switch args[0] {
		case "on", "off":
			set = args[0]
		default:
			return badUsage("quarantine takes on, off, or nothing to show the state")
		}
	}

	c, err := dial(op)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	var params any
	if set != "" {
		params = struct {
			Set string `json:"set"`
		}{set}
	}
	v, done, err := request[protocol.RoomView](c, op, protocol.MethodQuarantine, params)
	if done || err != nil {
		return err
	}

	switch set {
	case "on":
		fmt.Println("Done. Those ports are closed on every network of this machine, until")
		fmt.Println("you turn it off. Your rooms and games are not affected.")
	case "off":
		fmt.Println("Done. This PC answers file sharing and Remote Desktop again, on all")
		fmt.Println("its networks. Your Kanpachi room does not change.")
	}
	printQuarantine(os.Stdout, v.Quarantine)
	return nil
}

// cmdName reads or changes the name this machine enters rooms under.
//
// It is the mould of [cmdSeed], its sibling command: zero or one argument, and
// with an empty value the suggestion prints as a command ready to paste rather
// than being applied on its own. Applying it on its own would be exactly the
// defect this change fixes, with the machine's name promoted to nobody's choice.
func cmdName(_ context.Context, op options, args []string) error {
	fresh := ""
	switch len(args) {
	case 0:
	case 1:
		fresh = args[0]
		if strings.HasPrefix(fresh, "-") {
			return badUsage("name takes a name, not a flag: kanpachi name alvaro")
		}
	default:
		return badUsage("name takes at most one name, and a name has no spaces")
	}

	c, err := dial(op)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	var params any
	if fresh != "" {
		params = struct {
			Nickname string `json:"nickname"`
		}{fresh}
	}
	v, done, err := request[struct {
		Nickname  string `json:"nickname"`
		Suggested string `json:"suggested"`
	}](c, op, protocol.MethodNickname, params)
	if done || err != nil {
		return err
	}

	if v.Nickname == "" {
		fmt.Println("Nobody has chosen a name on this machine yet.")
		fmt.Printf("\n  Rooms would show you as %s, taken from this machine's name.\n", v.Suggested)
		fmt.Println("  To choose one:  kanpachi name alvaro")
		return nil
	}
	fmt.Printf("Rooms show you as %s.\n", v.Nickname)
	fmt.Println("\n  It is the same name in the window, the wizard and here.")
	return nil
}

// cmdSeed shows or changes the registry this machine opens its rooms on.
//
// # Why a command is needed, when nothing used to be
//
// Because the default seed is gone. Until 2026-08-12 a bare code meant one
// concrete machine, compiled in; now the registry travels in every code, and
// when CREATING there is no code yet, because the code is exactly what the
// registry issues. So creating needs to know who to ask, and this is where that
// gets answered from a terminal.
//
// # Why it does NOT adopt the registry of the room you entered
//
// Because the next room you opened would be hosted on a stranger's server
// without anybody deciding it. What that registry does do is show up here as a
// suggestion, so nobody has to go dig it out of a chat.
func cmdSeed(_ context.Context, op options, args []string) error {
	fresh := ""
	switch len(args) {
	case 0:
	case 1:
		fresh = args[0]
		if strings.HasPrefix(fresh, "-") {
			return badUsage("seed takes a host name, not a flag: kanpachi seed seed.example.com")
		}
	default:
		return badUsage("seed takes at most one host name")
	}

	c, err := dial(op)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	var params any
	if fresh != "" {
		params = struct {
			Seed string `json:"seed"`
		}{fresh}
	}
	v, done, err := request[struct {
		Seed      string `json:"seed"`
		Suggested string `json:"suggested"`
	}](c, op, protocol.MethodOwnSeed, params)
	if done || err != nil {
		return err
	}

	if v.Seed == "" {
		fmt.Println("This machine has no registry configured, so it cannot open rooms yet.")
		// The suggestion comes with the command already written, and does not
		// apply itself: entering somebody's room does not choose where you are
		// going to host.
		if v.Suggested != "" {
			fmt.Printf("\n  The last room you entered was on %s.\n", v.Suggested)
			fmt.Printf("  To use that one:  kanpachi seed %s\n", v.Suggested)
		} else {
			fmt.Println("\n  To set one:  kanpachi seed seed.example.com")
		}
		return nil
	}
	fmt.Printf("Rooms opened from this machine live on %s.\n", v.Seed)
	if v.Suggested != "" && v.Suggested != v.Seed {
		fmt.Printf("\n  The last room you entered was on %s, which is a different one.\n", v.Suggested)
	}
	return nil
}

// ─── The nickname ────────────────────────────────────────────────────────────

// nickname decides which name to enter under, and no longer REMEMBERS it: the
// daemon remembers it, which is the one piece all three faces share.
//
// # What it fixes that the file no longer living here
//
// That this machine has one name and not two. The terminal kept its own in
// `nickname.txt` and the window kept its own in `ui-prefs.json`, in the same
// directory, and the room showed whichever face had entered: measured on
// 2026-08-18, a window saying «Alvaro» and a room showing «AlvaroGDeskt».
//
// # And the derived one is NEVER saved
//
// That is half the fix, more than the unification. The branch below used to
// write the machine's cleaned-up name to disk, and once written it stopped being
// distinguishable from a chosen name: that is why it beat the real one. Today
// the daemon SUGGESTS it, the terminal uses it and says so on stderr, and nobody
// persists it. With `--nick` it does get saved, because a person typed that.
//
// The derivation does not live here either: it lives in the daemon, which runs
// on the same machine, so the machine's name is the same one and a copy fewer is
// a copy that cannot drift.
func nickname(op options) (string, error) {
	c, err := dial(op)
	if err != nil {
		return "", err
	}
	defer func() { _ = c.Close() }()
	return nicknameOn(c, op)
}

// nicknameOn is the same over a connection that is already open.
//
// It does not go through `request` on purpose: `request` prints the raw JSON
// with `--json`, and this is an internal step of creating or entering, not the
// answer to the command.
func nicknameOn(c *client.Client, op options) (string, error) {
	chosen, suggested, err := askNickname(c, op.nick)
	if err != nil {
		return "", err
	}
	if chosen != "" {
		return chosen, nil
	}
	if suggested == "" {
		return "", fmt.Errorf("this machine has no name yet and none could be derived.\n" +
			"  Choose one:  kanpachi name <yours>")
	}
	// On stderr and in one line: what is happening is correct and does not need
	// stopping, since the headless droplet and any script depend on a first run
	// with nothing configured working, and at the same time nobody chose this
	// name, so it gets said along with how to change it.
	fmt.Fprintf(os.Stderr, "kanpachi: using %s, derived from this machine's name."+
		" `kanpachi name <yours>` to change it.\n", suggested)
	return suggested, nil
}

// askNickname is the bare call: with a name it sets it, without one it only
// reads, and in both cases it returns the chosen one and the suggested one.
//
// It does not go through `request` on purpose: `request` prints the raw JSON
// with `--json`, and this is an internal step of creating, entering or the
// wizard, not the answer to the command somebody typed. The one that IS that
// answer is [cmdName].
func askNickname(c *client.Client, fresh string) (chosen, suggested string, err error) {
	var params any
	if fresh != "" {
		params = struct {
			Nickname string `json:"nickname"`
		}{fresh}
	}
	raw, err := c.Call(protocol.MethodNickname, params)
	if err != nil {
		return "", "", err
	}
	var v struct {
		Nickname  string `json:"nickname"`
		Suggested string `json:"suggested"`
	}
	if e := json.Unmarshal(raw, &v); e != nil {
		return "", "", fmt.Errorf("parsing the answer to %s: %w", protocol.MethodNickname, e)
	}
	return v.Nickname, v.Suggested, nil
}
