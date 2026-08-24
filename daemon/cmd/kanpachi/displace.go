package main

import (
	"fmt"

	"github.com/AlecAivazis/survey/v2"
	"github.com/accentiostudios/kanpachi/daemon/transport/client"
	"github.com/accentiostudios/kanpachi/daemon/transport/protocol"
)

// confirmDisplacement asks before entering a room costs something else.
//
// # It decides nothing, and that is the point
//
// The daemon works out what is in the way and hands it over in the status; this
// only renders it and asks. Three faces each deciding when a confirmation is due
// would be three copies of one rule, drifting apart. See [domain.Displacement].
//
// # The same three outcomes as confirming a registry, and for the same reasons
//
//  1. With `--yes`, it goes ahead. Whoever typed it already said so.
//  2. **With no terminal and no `--yes`, it REFUSES.** Inside a script there is
//     nowhere to confirm, and reading that absence as a yes is how a machine ends
//     up abandoning the room it was in without anybody asking it to.
//  3. With a terminal, it asks, defaulting to NO.
//
// Returns whether the caller should send `replace`.
func confirmDisplacement(op options, noQuestions bool) (bool, error) {
	d, err := whatIsInTheWay(op)
	if err != nil || d == nil {
		// A daemon that will not answer is not a reason to refuse here: the
		// command about to run is going to hit the same wall and say so with its
		// own words.
		return false, err
	}

	if noQuestions {
		if !op.json {
			fmt.Println(whatGetsLost(d))
		}
		return true, nil
	}
	if !hasTerminal() {
		return false, refuse("%s\n"+
			"  There is no terminal to confirm that in.\n"+
			"  If that is what you want, say so on purpose:  --yes", whatGetsLost(d))
	}

	fmt.Println()
	fmt.Println("  " + whatGetsLost(d))
	fmt.Println()

	var yes bool
	if err := ask(&survey.Confirm{
		Message: questionFor(d),
		Default: false,
	}, &yes); err != nil {
		return false, err
	}
	if !yes {
		return false, refuse("nothing was done.")
	}
	return true, nil
}

// whatIsInTheWay asks the daemon what entering would cost. Nil is "nothing".
func whatIsInTheWay(op options) (*protocol.DisplacesView, error) {
	c, err := dial(op)
	if err != nil {
		return nil, err
	}
	defer func() { _ = c.Close() }()

	st, err := client.Ask[protocol.RoomView](c, protocol.MethodStatus, nil)
	if err != nil {
		return nil, err
	}
	return st.Displaces, nil
}

// whatGetsLost is one sentence per kind and nothing else. The wording is the
// only thing that differs between the three, because the decision was made
// elsewhere.
func whatGetsLost(d *protocol.DisplacesView) string {
	name := d.Name
	if name == "" {
		name = "(unnamed)"
	}
	switch d.Kind {
	// Closing says the room ENDS, and the two lines below it used to stop at the
	// ports. What actually happens is that `hosted-room.json` goes and the entry
	// is retired from the registry: the code stops resolving and there is nothing
	// left to reopen. That is the most destructive thing this product does, and
	// announcing it as "the ports close" undersells it.
	case "close_room":
		s := fmt.Sprintf("Entering another room means CLOSING yours, %s, for good.", name)
		if d.Members > 0 {
			s += fmt.Sprintf("\n  The %d people inside drop and the game ports close.", d.Members)
		} else {
			s += "\n  The game ports close."
		}
		s += fmt.Sprintf("\n  Its code %s stops working and there is nothing left to reopen.",
			hyphenated(d.Code))
		return s
	case "stop_returning":
		return fmt.Sprintf("Entering another room means giving up on going back to %s, %s.",
			name, hyphenated(d.Code))
	default:
		return fmt.Sprintf("Entering another room means leaving %s, %s.",
			name, hyphenated(d.Code))
	}
}

func questionFor(d *protocol.DisplacesView) string {
	if d.Kind == "close_room" {
		return "Close your room and go on?"
	}
	return "Leave it and go on?"
}
