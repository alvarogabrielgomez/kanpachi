package main

// The wizard: what comes up when somebody types a bare `kanpachi`.
//
// # What for, when there are already subcommands
//
// Because the normal case for this binary is a freshly installed server and a
// person who has just read one line of the README. The subcommands are for the
// script and for the second time; this is for the first, and for not having to
// remember that renewing the code is called `rotate`.
//
// # It calls the SAME functions the subcommands call
//
// Nothing here reimplements an order. A wizard with its own path to the daemon
// is a second place to fix each thing, and the one that gets forgotten is always
// the least used.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/AlecAivazis/survey/v2"
	"github.com/AlecAivazis/survey/v2/terminal"
	"golang.org/x/term"

	"github.com/accentiostudios/kanpachi/daemon/transport/client"
	"github.com/accentiostudios/kanpachi/daemon/transport/protocol"
)

func assistant(ctx context.Context, op options) error {
	// With no terminal there are no arrows to press. It says so and shows the
	// help, instead of failing with what survey would say, which talks about file
	// descriptors and not about what is happening to whoever is running it.
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprintln(os.Stderr, "kanpachi: with no terminal there is no wizard to show.")
		fmt.Fprintln(os.Stderr, "  Inside a script, use a subcommand:")
		fmt.Fprintln(os.Stderr)
		printHelp(os.Stderr)
		return badUsage("a subcommand is needed")
	}
	// The wizard never prints JSON: it is a screen. Having `--json` turn it off
	// in silence would be worse, so it gets said.
	if op.json {
		return badUsage("--json is for the subcommands, and the wizard is a screen")
	}

	for {
		if ctx.Err() != nil {
			return errInterrupted
		}
		st, err := currentMenuStatus(op)
		if err != nil {
			return err
		}
		var nextError error
		if st.Conn == "idle" || st.Conn == "" {
			nextError = presentNoRoomMenu(ctx, op, st)
		} else {
			nextError = roomMenu(ctx, op, st)
		}
		if errors.Is(nextError, errExit) {
			return nil
		}
		if nextError != nil {
			return nextError
		}
	}
}

// errExit is having chosen «Quit» in the menu, which is not a failure.
var errExit = errors.New("quit")

// currentMenuStatus opens, asks and closes.
//
// One connection per turn of the menu and not one for the whole wizard, on
// purpose: between two turns a minute may have gone by with somebody reading the
// menu, and an idle connection holds one of the listener's eight slots for all
// of it.
func currentMenuStatus(op options) (protocol.RoomView, error) {
	c, err := dial(op)
	if err != nil {
		return protocol.RoomView{}, err
	}
	defer func() { _ = c.Close() }()
	return client.Ask[protocol.RoomView](c, protocol.MethodStatus, nil)
}

// ─── With no room ────────────────────────────────────────────────────────────

func presentNoRoomMenu(ctx context.Context, op options, st protocol.RoomView) error {
	clearScreen(os.Stdout)
	fmt.Println(rule)
	fmt.Printf("  KANPACHI %-20s channel: %s\n", Version, op.channel)
	fmt.Println(rule)
	fmt.Println()

	// The state gets PAINTED, and until now this branch was the one mouth that
	// did not do it: the wizard showed the menu without saying why the last thing
	// ended or that it is going back to a room. It is the same renderer `status`
	// and `watch` use, not a second version.
	//
	// No catalog: this branch is the one with no room, so there is no host and
	// no active profile, and the address line has nothing to build from.
	printRoom(os.Stdout, st, nil)
	fmt.Println()

	const (
		open     = "Open a room"
		enter    = "Enter someone else's room"
		goBack   = "Go back to the last room I entered"
		tryNow   = "Try going back now"
		giveUp   = "Stop going back to that room"
		reopen   = "Reopen the room left from the previous start"
		forget   = "Forget that pending room"
		catalog  = "See the game catalog"
		checkSys = "Check the system"
		update   = "Look for a new version"
		rename   = "Change my name"
		quit     = "Quit"
	)

	// While going back, the two entries that matter come FIRST, because that is
	// what is happening right now. And `goBack` disappears: offering "go back" to
	// somebody already going back is offering what they already have.
	returning := st.Returning != nil
	var choices []string
	if returning {
		choices = append(choices, tryNow, giveUp)
	}
	choices = append(choices, open, enter)
	if yes, _ := hasSavedRoom(op); yes {
		choices = append(choices, reopen, forget)
	}
	// The question only gets asked if it can change anything. This menu opens a
	// connection for each of these checks, of the eight there are, and while
	// going back the answer already came in the state.
	if !returning {
		if yes, _ := hasLastRoom(op); yes {
			choices = append(choices, goBack)
		}
	}
	// The quarantine's switch, with the state in the label itself, which is how a
	// terminal menu gets to be a switch. It is where whoever read nothing is
	// going to find it. With no measurement it does not get offered: toggling
	// blind is not a switch, and «Check the system» already tells the reason.
	quarantine, turnOn := "", true
	switch st.Quarantine.Verdict {
	case "applied":
		quarantine, turnOn = "Quarantine is ON: reopen this PC's server ports (file sharing, Remote Desktop)", false
	case "absent":
		quarantine = "Quarantine is OFF: close this PC's risky server ports (recommended)"
	case "partial":
		quarantine = "Quarantine is HALF ON: repair it (close the ports again)"
	}
	if quarantine != "" {
		choices = append(choices, quarantine)
	}
	choices = append(choices, catalog, checkSys, update, rename, quit)

	sel, err := choose("What do we do:", choices)
	if err != nil {
		return err
	}
	if quarantine != "" && sel == quarantine {
		set := "on"
		if !turnOn {
			set = "off"
		}
		return withNotice(ctx, op, cmdQuarantine(ctx, op, []string{set}))
	}
	switch sel {
	case open:
		roomName, err := text("Room name:",
			"It travels inside the encrypted card. The registry never sees it.", machineName())
		if err != nil {
			return err
		}
		return withNotice(ctx, op, cmdHost(ctx, op, strings.Fields(roomName)))
	case enter:
		pasted, err := text("Paste the link or the code exactly as it reached you:",
			"All six forms work: VA3BSF5L, va3b-sf5l, kanpachi://VA3BSF5L,\n"+
				"VA3BSF5L@another-seed.com, kanpachi.accentio.dev/VA3BSF5L and https://...", "")
		if err != nil {
			return err
		}
		return withNotice(ctx, op, cmdJoin(ctx, op, []string{pasted}))
	case goBack, tryNow:
		// The same path for both, and that is not laziness: «go back» and «try
		// now» are literally the same thing, entering with the saved code. The
		// only difference is that in the second case a clock was already counting
		// it. The gate asks nothing, because going back to where you are already
		// going displaces nothing.
		return withNotice(ctx, op, backToLastRoom(ctx, op))
	case giveUp:
		return withNotice(ctx, op, cmdLeave(ctx, op, nil))
	case reopen:
		return withNotice(ctx, op, cmdResume(ctx, op, nil))
	case forget:
		return withNotice(ctx, op, cmdDiscard(ctx, op, nil))
	case catalog:
		return withNotice(ctx, op, cmdGames(ctx, op, nil))
	case checkSys:
		return checksMenu(ctx, op)
	case update:
		// `--check` and not the whole upgrade, on purpose: from the menu you
		// LOOK, and upgrading restarts the service. The command that does it gets
		// typed by hand, which is a step that is worth it there.
		return withNotice(ctx, op, cmdUpgrade(ctx, op, []string{"--check"}))
	case rename:
		return changeName(ctx, op)
	case quit:
		return errExit
	}
	return nil
}

// backToLastRoom is the GUEST's path, and it is not `resume`.
//
// Confusing them is the mistake this separation exists to avoid. `resume`
// belongs to the HOST and picks a room back up that is still theirs. This is the
// same entry as the first time with the saved code: a credential gets exchanged
// and the host watches whoever comes in, which is what keeps revocation
// meaningful.
//
// It composes `CODE@seed` and not the bare code: the last room saves its seed,
// and a bare code always resolves to the default seed.
func backToLastRoom(ctx context.Context, op options) error {
	c, err := dial(op)
	if err != nil {
		return err
	}
	v, err := client.Ask[struct {
		Found bool                  `json:"found"`
		Room  protocol.LastRoomView `json:"room"`
	}](c, protocol.MethodLastRoom, nil)
	_ = c.Close()
	if err != nil {
		return err
	}
	if !v.Found {
		return errors.New("there is no previous room saved")
	}
	return cmdJoin(ctx, op, []string{v.Room.Code + "@" + v.Room.Seed})
}

func hasSavedRoom(op options) (bool, error) {
	c, err := dial(op)
	if err != nil {
		return false, err
	}
	defer func() { _ = c.Close() }()
	v, err := client.Ask[struct {
		Found bool `json:"found"`
	}](c, protocol.MethodSavedRoom, nil)
	return v.Found, err
}

func hasLastRoom(op options) (bool, error) {
	c, err := dial(op)
	if err != nil {
		return false, err
	}
	defer func() { _ = c.Close() }()
	v, err := client.Ask[struct {
		Found bool `json:"found"`
	}](c, protocol.MethodLastRoom, nil)
	return v.Found, err
}

// ─── With a room ─────────────────────────────────────────────────────────────

// catalogForRoomMenu opens its own connection because the wizard hands the room
// view around without one.
//
// It is a menu a person is reading, so one extra round trip over a local pipe
// costs nothing anybody can perceive, and only when a profile is active. Every
// failure ends in nil, which drops the address line and keeps the menu.
func catalogForRoomMenu(op options, st protocol.RoomView) []protocol.GameView {
	if st.Game == "" {
		return nil
	}
	c, err := dial(op)
	if err != nil {
		return nil
	}
	defer func() { _ = c.Close() }()
	return catalogForAddress(c, st)
}

func roomMenu(ctx context.Context, op options, st protocol.RoomView) error {
	clearScreen(os.Stdout)
	printRoom(os.Stdout, st, catalogForRoomMenu(op, st))
	fmt.Println()

	const (
		watch     = "Watch the room live"
		copyLink  = "Show the link to hand out"
		rotate    = "Renew the code (the links handed out stop working)"
		pickGame  = "Activate a game profile"
		closeGame = "Close the game ports"
		checkSys  = "Check the system"
		otherRoom = "Enter another room"
		closeRoom = "Close the room"
		leaveRoom = "Leave the room"
		quit      = "Quit the wizard (the room stays open)"
	)

	choices := []string{watch, copyLink}
	kicks := map[string]string{}
	if st.Role == "host" {
		choices = append(choices, rotate, pickGame)
		if st.Game != "" {
			choices = append(choices, closeGame)
		}
		for _, p := range st.Peers {
			if p.Self {
				continue
			}
			label := fmt.Sprintf("Kick %s (%s)", p.Name, p.IP)
			kicks[label] = p.IP
			choices = append(choices, label)
		}
	}
	choices = append(choices, checkSys)
	// Changing rooms with one open is the case that does NOT exist in the
	// window, because over there being inside takes you to the room screen and
	// there is no code field. Here the terminal is the interface, and what can be
	// typed as `kanpachi join <other>` has to be selectable. The confirmation is
	// the gate's, with whichever sentence fits leaving or closing.
	choices = append(choices, otherRoom)
	if st.Role == "host" {
		choices = append(choices, closeRoom)
	} else {
		choices = append(choices, leaveRoom)
	}
	choices = append(choices, quit)

	sel, err := choose("Action:", choices)
	if err != nil {
		return err
	}
	if ip, isKick := kicks[sel]; isKick {
		return withNotice(ctx, op, cmdKick(ctx, op, []string{ip}))
	}
	switch sel {
	case watch:
		// Ctrl+C leaves the live view and comes back to the menu, not out of the
		// program: whoever presses it there wants to stop watching, not close
		// anything.
		err := cmdWatch(ctx, op, nil)
		if errors.Is(err, errInterrupted) {
			return nil
		}
		return withNotice(ctx, op, err)
	case copyLink:
		return withNotice(ctx, op, cmdLink(ctx, op, nil))
	case rotate:
		yes, err := confirm("The links you already handed out will stop working. Go on?")
		if err != nil || !yes {
			return err
		}
		return withNotice(ctx, op, cmdRotate(ctx, op, nil))
	case pickGame:
		return withNotice(ctx, op, chooseGame(ctx, op))
	case closeGame:
		return withNotice(ctx, op, cmdGame(ctx, op, nil))
	case checkSys:
		return checksMenu(ctx, op)
	case otherRoom:
		pasted, err := text("Paste the link or the code of the room you want to enter:",
			"Entering it means leaving this one. You get asked before anything happens.", "")
		if err != nil {
			return err
		}
		return withNotice(ctx, op, cmdJoin(ctx, op, []string{pasted}))
	case closeRoom, leaveRoom:
		return withNotice(ctx, op, cmdLeave(ctx, op, nil))
	case quit:
		return errExit
	}
	return nil
}

// chooseGame shows the catalog and activates whichever gets picked.
func chooseGame(ctx context.Context, op options) error {
	c, err := dial(op)
	if err != nil {
		return err
	}
	games, err := client.Ask[[]protocol.GameView](c, protocol.MethodListGames, nil)
	_ = c.Close()
	if err != nil {
		return err
	}
	if len(games) == 0 {
		return errors.New("the catalog is empty")
	}

	labels := make([]string, 0, len(games))
	byLabel := map[string]string{}
	for _, g := range games {
		e := g.Name
		if g.Installed {
			e += "  (installed)"
		}
		labels = append(labels, e)
		byLabel[e] = g.ID
	}
	sel, err := choose("Which game:", labels)
	if err != nil {
		return err
	}
	return cmdGame(ctx, op, []string{byLabel[sel]})
}

// ─── Checks ──────────────────────────────────────────────────────────────────

func checksMenu(ctx context.Context, op options) error {
	const (
		exposure = "What I have open, and toward whom"
		network  = "How my network looks from the engine"
		probe    = "Probe me from another machine in the room"
		restore  = "Put Kanpachi Protection back"
		back     = "<< Back"
	)
	sel, err := choose("What do we check:", []string{exposure, network, probe, restore, back})
	if err != nil {
		return err
	}
	switch sel {
	case exposure:
		return withNotice(ctx, op, cmdExposure(ctx, op, nil))
	case network:
		return withNotice(ctx, op, cmdDiag(ctx, op, nil))
	case probe:
		return withNotice(ctx, op, cmdProbe(ctx, op, nil))
	case restore:
		return withNotice(ctx, op, cmdProtect(ctx, op, nil))
	}
	return nil
}

// changeName asks for the name and saves it WHERE the other two faces save it,
// which as of today is the daemon.
//
// It used to read the CLI's file and write down a path that swallowed the
// failure: the screen said yes and the name might not have been saved. Now the
// error rises, which is what makes this start working for real on the installed
// product: there, the one that can write in the data directory is the daemon,
// and neither of the two faces.
func changeName(ctx context.Context, op options) error {
	c, err := dial(op)
	if err != nil {
		return withNotice(ctx, op, err)
	}
	defer func() { _ = c.Close() }()

	current, suggested, err := askNickname(c, "")
	if err != nil {
		return withNotice(ctx, op, err)
	}
	// With no name chosen it prefills with the suggestion, which is what would
	// get used entering a room anyway. Showing it here is the difference between
	// choosing it and having it choose itself.
	if current == "" {
		current = suggested
	}
	fresh, err := text("Your name:", "The other members of the room see it. "+
		"Letters and digits only, up to 12.", current)
	if err != nil {
		return err
	}
	if _, _, err := askNickname(c, fresh); err != nil {
		return withNotice(ctx, op, err)
	}
	return nil
}

// ─── Asking ──────────────────────────────────────────────────────────────────

// ask wraps survey so the interruption cannot escape.
//
// **No place in this binary calls `survey.AskOne` directly.** Inside a question,
// survey clears the console's `ENABLE_PROCESSED_INPUT`, so Windows does NOT
// generate CTRL_C_EVENT and `signal.NotifyContext` never hears anything: the one
// thing that happens is that `AskOne` returns `terminal.InterruptErr`.
func ask(p survey.Prompt, target any, opts ...survey.AskOpt) error {
	err := survey.AskOne(p, target, opts...)
	if errors.Is(err, terminal.InterruptErr) {
		return errInterrupted
	}
	return err
}

func choose(message string, choices []string) (string, error) {
	var sel string
	err := ask(&survey.Select{Message: message, Options: choices, PageSize: 15}, &sel)
	return sel, err
}

func text(message, help, fallback string) (string, error) {
	var v string
	err := ask(&survey.Input{Message: message, Help: help, Default: fallback}, &v,
		survey.WithValidator(survey.Required))
	return strings.TrimSpace(v), err
}

func confirm(message string) (bool, error) {
	var v bool
	err := ask(&survey.Confirm{Message: message, Default: false}, &v)
	return v, err
}

// withNotice shows the error and waits, instead of swallowing it or raising it.
//
// The errors on these paths are answers from the product, "that code does not
// exist", "the host did not answer", and what to do with them is read them. The
// interruption does rise, because it is the one thing that has to reach the end.
func withNotice(ctx context.Context, op options, err error) error {
	if err == nil {
		waitForEnter()
		return nil
	}
	if errors.Is(err, errInterrupted) || errors.Is(err, context.Canceled) {
		return err
	}
	fmt.Println("\n  BAD:", err)
	if fixableRightHere(ctx, op, err) {
		return nil
	}
	waitForEnter()
	return nil
}

// fixableRightHere offers to resolve on the spot what has a fix, and says
// whether it got that far.
//
// # Why here and not leaving it as advice
//
// Because the wizard exists for whoever read nothing, and sending them off to
// type `kanpachi seed <host>` is sending them out of the wizard to come back in.
// The subcommand does keep the advice, which is right there: whoever typed a
// command is in a terminal and can type another.
//
// **It does not retry the operation by itself.** Configuring the registry and
// opening a room are two decisions, and chaining them would leave somebody who
// only wanted to fix the configuration with an open room.
func fixableRightHere(ctx context.Context, op options, err error) bool {
	switch client.Code(err) {
	case protocol.CodeNoOwnSeed:
		fmt.Println("\n  This machine has no registry to open rooms on yet.")
		host, e := text("Registry host:",
			"The name of the server, with no https:// and no slashes. It travels\n"+
				"inside every code you hand out.", "")
		if e != nil || strings.TrimSpace(host) == "" {
			return false
		}
		return withNotice(ctx, op, cmdSeed(ctx, op, []string{strings.TrimSpace(host)})) == nil
	case protocol.CodeSeedPassword:
		fmt.Println("\n  That registry asks for a password to host on it.")
		fmt.Println("  Entering a room never asks for one.")
		return withNotice(ctx, op, cmdPassword(ctx, op, nil)) == nil
	}
	return false
}

func waitForEnter() {
	fmt.Println("\n  Press Enter to continue...")
	var nothing string
	_, _ = fmt.Scanln(&nothing)
}
