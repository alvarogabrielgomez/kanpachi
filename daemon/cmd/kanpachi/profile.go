package main

// Creating a profile from the terminal.
//
// # Why it exists, when this was already written
//
// Because it was written with one mouth. `save_profile` has lived in the
// protocol since there has been a profile editor, and the only face that calls
// it is the Flutter window, which means Windows. On a headless host there was no
// way to describe a game the catalog does not carry, and describing one by hand
// in `local.json` means writing a file the daemon reads with a strict decoder.
//
// This command reimplements nothing: it builds the profile's JSON and hands it
// to the same method, which decodes it again with [domain.ParseGameProfile] and
// applies the whole set of invariants. What is gained is the mouth, not the
// logic.
//
// # Why re-running it is safe
//
// The core's save overwrites by id, so running it again with different ports
// UPDATES the same profile instead of creating a second one. That is what lets a
// container call it on every start.

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/daemon/transport/protocol"
)

// profileJSON is what travels as `profile`, and it carries only the fields a
// terminal-side creation can fill.
//
// **The fields that are not there are omitted on purpose, and not left empty.**
// The daemon decodes with unknown fields forbidden, so one extra key rejects the
// whole profile; the missing ones take their zero value, which is what suits a
// profile described by its ports and nothing else. `origin` does not travel: the
// daemon fixes it at "mine", and sending it would be asking it to trust whatever
// the writer says.
type profileJSON struct {
	ID          string          `json:"id"`
	Schema      int             `json:"schema"`
	Name        string          `json:"name"`
	HostPorts   []portRangeJSON `json:"host_ports"`
	ConnectHint connectHintJSON `json:"connect_hint"`
}

type portRangeJSON struct {
	Proto string `json:"proto"`
	Range string `json:"range"`
}

// connectHintJSON is required even though it always holds the same value here:
// the schema demands `kind`, and of the three accepted values the only one that
// describes a dedicated server is the direct IP.
type connectHintJSON struct {
	Kind string `json:"kind"`
}

func cmdProfile(_ context.Context, op options, args []string) error {
	id := ""
	name := ""
	tcp := ""
	udp := ""
	replace := false

	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--replace", a == "-replace":
			replace = true
		case a == "--name", a == "-name":
			var err error
			if name, err = valueOf(args, &i, "--name"); err != nil {
				return err
			}
		case a == "--tcp", a == "-tcp":
			var err error
			if tcp, err = valueOf(args, &i, "--tcp"); err != nil {
				return err
			}
		case a == "--udp", a == "-udp":
			var err error
			if udp, err = valueOf(args, &i, "--udp"); err != nil {
				return err
			}
		case strings.HasPrefix(a, "--name="):
			name = strings.TrimPrefix(a, "--name=")
		case strings.HasPrefix(a, "--tcp="):
			tcp = strings.TrimPrefix(a, "--tcp=")
		case strings.HasPrefix(a, "--udp="):
			udp = strings.TrimPrefix(a, "--udp=")
		case strings.HasPrefix(a, "-"):
			return badUsage("profile does not understand %q. It takes --name, --tcp, --udp and --replace", a)
		case id == "":
			id = a
		default:
			return badUsage("profile takes one id, and it got %q as well", a)
		}
	}

	if id == "" {
		return badUsage("profile needs an id: kanpachi profile my-server --name \"My server\" --tcp 25565")
	}
	if strings.TrimSpace(name) == "" {
		return badUsage("profile needs --name, which is how this machine lists the game")
	}
	if tcp == "" && udp == "" {
		return badUsage("profile needs --tcp or --udp, or it describes nothing")
	}

	// The ports get validated HERE as well as in the daemon, and that is not
	// duplicating the invariant: it is so the error comes out with the range the
	// person wrote. The one in charge is still the daemon's, which runs over the
	// assembled JSON and is the one that can say no.
	ranges := make([]portRangeJSON, 0, domain.MaxPortRanges)
	for _, pair := range []struct {
		proto domain.Proto
		list  string
	}{
		{domain.ProtoTCP, tcp},
		{domain.ProtoUDP, udp},
	} {
		if pair.list == "" {
			continue
		}
		read, err := domain.ParsePortRanges(pair.proto, pair.list)
		if err != nil {
			return badUsage("--%s: %v", pair.proto, err)
		}
		for _, r := range read {
			ranges = append(ranges, portRangeJSON{Proto: r.Proto.String(), Range: r.Spec()})
		}
	}

	c, err := dial(op)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	saved, done, err := request[protocol.GameView](c, op, protocol.MethodSaveProfile, struct {
		Profile profileJSON `json:"profile"`
		Replace bool        `json:"replace"`
	}{
		Profile: profileJSON{
			ID:          id,
			Schema:      domain.SchemaVersion,
			Name:        name,
			HostPorts:   ranges,
			ConnectHint: connectHintJSON{Kind: "direct_ip"},
		},
		Replace: replace,
	})
	if done || err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "Saved %s (%s).\n", saved.ID, saved.Name)
	for _, r := range saved.HostPorts {
		if r.From == r.To {
			fmt.Fprintf(os.Stdout, "  %d/%s\n", r.From, r.Proto)
			continue
		}
		fmt.Fprintf(os.Stdout, "  %d-%d/%s\n", r.From, r.To, r.Proto)
	}
	fmt.Fprintf(os.Stdout, "\n  Activate it with: kanpachi game %s\n", saved.ID)
	return nil
}

// valueOf takes the value that follows a flag and advances the index.
func valueOf(args []string, i *int, flag string) (string, error) {
	if *i+1 >= len(args) {
		return "", badUsage("%s is missing its value", flag)
	}
	*i++
	return args[*i], nil
}
