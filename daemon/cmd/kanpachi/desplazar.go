package main

import (
	"fmt"

	"github.com/AlecAivazis/survey/v2"
	"github.com/accentiostudios/kanpachi/daemon/transport/client"
	"github.com/accentiostudios/kanpachi/daemon/transport/protocol"
)

// confirmarDesplazamiento asks before entering a room costs something else.
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
func confirmarDesplazamiento(op opciones, sinPreguntar bool) (bool, error) {
	d, err := loQueEstorba(op)
	if err != nil || d == nil {
		// A daemon that will not answer is not a reason to refuse here: the
		// command about to run is going to hit the same wall and say so with its
		// own words.
		return false, err
	}

	if sinPreguntar {
		if !op.json {
			fmt.Println(seVaAPerder(d))
		}
		return true, nil
	}
	if !hayTerminal() {
		return false, negativa("%s\n"+
			"  There is no terminal to confirm that in.\n"+
			"  If that is what you want, say so on purpose:  --yes", seVaAPerder(d))
	}

	fmt.Println()
	fmt.Println("  " + seVaAPerder(d))
	fmt.Println()

	var sí bool
	if err := preguntar(&survey.Confirm{
		Message: preguntaDe(d),
		Default: false,
	}, &sí); err != nil {
		return false, err
	}
	if !sí {
		return false, negativa("nothing was done.")
	}
	return true, nil
}

// loQueEstorba asks the daemon what entering would cost. Nil is "nothing".
func loQueEstorba(op opciones) (*protocol.DisplacesView, error) {
	c, err := abrir(op)
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

// seVaAPerder is one sentence per kind and nothing else. The wording is the only
// thing that differs between the three, because the decision was made elsewhere.
func seVaAPerder(d *protocol.DisplacesView) string {
	nombre := d.Name
	if nombre == "" {
		nombre = "(unnamed)"
	}
	switch d.Kind {
	case "close_room":
		s := fmt.Sprintf("Entering another room means CLOSING yours, %s.", nombre)
		if d.Members > 0 {
			s += fmt.Sprintf("\n  The %d people inside drop and the game ports close.", d.Members)
		} else {
			s += "\n  The game ports close."
		}
		return s
	case "stop_returning":
		return fmt.Sprintf("Entering another room means giving up on going back to %s, %s.",
			nombre, conGuion(d.Code))
	default:
		return fmt.Sprintf("Entering another room means leaving %s, %s.",
			nombre, conGuion(d.Code))
	}
}

func preguntaDe(d *protocol.DisplacesView) string {
	if d.Kind == "close_room" {
		return "Close your room and go on?"
	}
	return "Leave it and go on?"
}
