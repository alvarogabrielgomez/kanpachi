package main

// `kanpachi doctor`: what this needs to work, and what is broken.
//
// # It runs at two levels, and the separation is the point
//
// Doctor has to be useful JUST when the daemon will not start. So it looks at
// the environment itself, locally, with no daemon: the TUN node, the kernel, the
// units, the socket, the engine. What needs a network measurement it asks the
// daemon for over the methods that already exist, and only if it answers.
//
// The second part does NOT get reimplemented, and saying so matters because the
// temptation is real: `diag_report` already brings the NAT and the MTU from the
// engine, `exposure` already says whether the gate is up. Doctor presents them.
// A second measurer would give a second, different number and nobody would know
// which one to believe.
//
// # By default it writes NOTHING
//
// Fixing is `doctor --fix`. A diagnosis that modifies the machine cannot be run
// to understand what is going on, which is exactly what it gets run for.
//
// # The rule about fixing: only OUR things get touched, with one noted exception
//
// Our units, our tables, our socket, our device node. The operator's things,
// Docker, the kernel, and every manager that is not blocking the room today, get
// reported with the exact command and do not get executed, not even with
// `--fix`. It is the same rule that makes `SuspendForeign` refuse on Linux and
// the same one that led to forking EasyTier: the two calls that were removed
// from it wrote permanent rules into the firewall of whoever ran it.
//
// The exception is decision 36, and there is one: a ufw or a firewalld that is
// going to swallow the inbound of BOTH Kanpachi adapters gets opened with
// `--fix`, down the same recorded path the `kanpachi host` question uses, and it
// gets undone when the room closes or on the next start. Typing `--fix` after
// reading the verdict that names the commands IS the consent. The difference
// with the fork's behaviour is total: that one wrote without asking and without
// undoing.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/accentiostudios/kanpachi/daemon/transport/client"
	"github.com/accentiostudios/kanpachi/daemon/transport/protocol"
	"github.com/accentiostudios/kanpachi/internal/engineid"
)

type state int

const (
	stateGood state = iota
	stateWarn
	stateBad
	// stateUnknown is the check not having been possible, which is NOT the same
	// as coming out fine. It shows differently on purpose: reading it as good is
	// exactly the mistake this product cannot make.
	stateUnknown
)

func (e state) mark() string {
	switch e {
	case stateGood:
		return "OK  "
	case stateWarn:
		return "WARN"
	case stateBad:
		return "BAD "
	default:
		return "?   "
	}
}

// verdict is how one thing stands.
type verdict struct {
	state  state
	detail string
	// command is what the PERSON has to run, and it is what stands in for the
	// automatic fix in everything that is not ours. A diagnosis that says "you
	// have ufw blocking" without saying what to type leaves the work half done.
	command string
}

func good(f string, a ...any) verdict {
	return verdict{state: stateGood, detail: fmt.Sprintf(f, a...)}
}
func warn(f string, a ...any) verdict {
	return verdict{state: stateWarn, detail: fmt.Sprintf(f, a...)}
}
func bad(f string, a ...any) verdict {
	return verdict{state: stateBad, detail: fmt.Sprintf(f, a...)}
}
func unknown(f string, a ...any) verdict {
	return verdict{state: stateUnknown, detail: fmt.Sprintf(f, a...)}
}

// withFix attaches to a verdict the command that repairs it by hand.
func (v verdict) withFix(command string) verdict { v.command = command; return v }

// check is one check with its name and, if it is ours, its fix.
type check struct {
	name string
	// look answers how it stands. **It never writes**, not even with `--fix`.
	look func(ctx context.Context, op options) verdict
	// fix is nil in everything that is not ours, and that absence is the rule
	// turned into something nobody can skip by accident.
	fix func(ctx context.Context, op options) error
}

func cmdDoctor(ctx context.Context, op options, args []string) error {
	repair := false
	for _, a := range args {
		switch a {
		case "--fix", "-fix":
			repair = true
		default:
			return badUsage("doctor does not understand %q. It only takes --fix", a)
		}
	}

	fmt.Println("THE ENVIRONMENT")
	measured := map[string]verdict{}
	outstanding := []check{}
	for _, c := range systemChecks() {
		v := c.look(ctx, op)
		printVerdict(c.name, v)
		measured[c.name] = v
		if v.state == stateBad && c.fix != nil {
			outstanding = append(outstanding, c)
		}
	}

	if repair && len(outstanding) > 0 {
		fmt.Println("\nFIXING WHAT IS OURS")
		for _, c := range outstanding {
			if err := c.fix(ctx, op); err != nil {
				fmt.Printf("  %s %-30s could not: %v\n", stateBad.mark(), c.name, err)
				continue
			}
			// It gets LOOKED at again instead of taking the fix as done: a command
			// that returns zero and changes nothing is a real case, and calling it
			// fixed would be the worst possible outcome of a diagnosis. This is the
			// verdict that counts from here on.
			v := c.look(ctx, op)
			printVerdict(c.name, v)
			measured[c.name] = v
		}
	} else if len(outstanding) > 0 {
		fmt.Printf("\n  `kanpachi doctor --fix` can fix %d of those.\n", len(outstanding))
	}

	// The summary comes out of what was measured, with the fixed ones already
	// updated. Nothing gets looked at again: on this list, going over it once
	// more is half a dozen child processes and a read of the ruleset, and it is
	// all answered already.
	worst := stateGood
	for _, v := range measured {
		if v.state > worst {
			worst = v.state
		}
	}

	if err := whatTheDaemonMeasures(ctx, op); err != nil {
		fmt.Println("\nWHAT THE DAEMON MEASURES")
		fmt.Println("  could not ask:", err)
	}

	switch worst {
	case stateGood:
		fmt.Println("\nEverything that could be checked is fine.")
		return nil
	case stateWarn, stateUnknown:
		fmt.Println("\nThere are things to look at. Nothing stops a room from opening.")
		return nil
	default:
		return refuse("something is broken. It is above, marked BAD")
	}
}

func printVerdict(name string, v verdict) {
	fmt.Printf("  %s %-30s %s\n", v.state.mark(), name, v.detail)
	if v.command != "" {
		for _, line := range strings.Split(v.command, "\n") {
			fmt.Printf("       %s\n", line)
		}
	}
}

// whatTheDaemonMeasures is level 2: what is already measured and does not get
// recalculated.
//
// It gets skipped whole if the daemon does not answer, and without noise: that
// it does not answer was already said by level 1, which looks at the unit and
// the socket. Repeating it here would be the same failure told twice in two
// different words.
func whatTheDaemonMeasures(ctx context.Context, op options) error {
	c, err := client.Open(op.channel, op.data)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	fmt.Println("\nWHAT THE DAEMON MEASURES")

	st, err := client.Ask[protocol.RoomView](c, protocol.MethodStatus, nil)
	if err != nil {
		return err
	}
	if st.Conn == "idle" || st.Conn == "" {
		fmt.Println("  No room is open, so there is nothing of it to measure.")
	} else {
		fmt.Printf("  Room %s, %s, %d members\n", st.Name, connState(st.Conn), len(st.Peers))
		if st.Canary.Measured {
			fmt.Printf("  Kanpachi Protection: %s\n", canaryVerdict(st.Canary.Verdict))
		}
		for _, a := range st.Alerts {
			fmt.Printf("  ALERT %s: %s\n", alertName(a.Kind), a.Detail)
		}
	}

	// The exposure gets asked for always, with a room and without one: what it
	// answers is what Kanpachi has in the firewall NOW, and a gate that is not up
	// is news even with nobody playing.
	exp, err := client.Ask[protocol.ExposureView](c, protocol.MethodExposure, nil)
	if err != nil {
		return err
	}
	if !exp.Measured {
		fmt.Println("  The firewall could NOT be read. This does not say nothing is open:")
		fmt.Println("  it says nobody knows.")
		return nil
	}
	fmt.Printf("  Gate: %s, with %d open ports\n", gateState(exp.Gate), len(exp.Ports))
	for _, u := range exp.Unexpected {
		fmt.Printf("  RULE NOBODY ASKED FOR: %s\n", u)
	}
	fmt.Println("\n  `kanpachi exposure` shows them one by one. `kanpachi diag` measures the network.")
	return nil
}

// ─── Pieces that work on both systems ────────────────────────────────────────

// channelCheck looks at whether the control channel opens.
//
// It is the check that HELPS most, because its failure is the one that looks
// worst from outside: the socket does not open the same way with the service
// stopped as with the service running and no permission, and whoever suffers it
// has no way to tell them apart.
func channelCheck() check {
	return check{
		name: "the control channel",
		look: func(_ context.Context, op options) verdict {
			c, err := client.Open(op.channel, op.data)
			if err == nil {
				_ = c.Close()
				return good("%s answers", op.channel)
			}
			if os.IsPermission(err) {
				return bad("no permission for %s", op.channel).withFix(elevationHint())
			}
			return bad("%v", err).withFix(connectionHint(op))
		},
	}
}

// measuredQuarantineCheck asks the DAEMON what the quarantine measurement
// says, decision included. It is the Windows quarantine check, the first one
// this face has, and it is the symptom→cause bridge: whoever runs the doctor
// over "I cannot share a folder" has to find the cause here without knowing
// the word quarantine.
//
// # What --fix may and may not do here
//
// Fixing REPAIRS what the user chose, and never chooses for them. Partial, or
// absent with the answer on yes, is a broken yes and gets repaired with
// `quarantine on`. Absent by the user's own no is a CHOICE and paints as a
// notice with no fix; absent with the question unanswered likewise, naming
// where to answer it.
func measuredQuarantineCheck() check {
	return check{
		name: "the base quarantine",
		look: func(_ context.Context, op options) verdict {
			q, err := quarantineFromDaemon(op)
			if err != nil {
				return unknown("the daemon is not answering, so nobody can measure it: %v", err)
			}
			switch q.Verdict {
			case "applied":
				return good("on: nobody reaches this PC's file sharing or Remote Desktop. " +
					"If YOU need those, `kanpachi quarantine off`")
			case "partial":
				return bad("of %d rules, %d missing, %d disabled and %d edited: "+
					"something other than Kanpachi changed it",
					q.Total, q.Missing, q.Disabled, q.Drifted).
					withFix("kanpachi quarantine on")
			case "absent":
				switch q.Decision {
				case "no":
					return warn("off, by this machine's own decision. Sharing folders and " +
						"entering this PC over Remote Desktop work; on strange networks " +
						"they are reachable. `kanpachi quarantine on` closes them")
				case "yes":
					return bad("you asked for it and it is NOT in place").
						withFix("kanpachi quarantine on")
				default:
					return warn("off, and the question is unanswered. `kanpachi quarantine on` " +
						"closes file sharing and Remote Desktop INTO this PC on every " +
						"network (recommended)")
				}
			default:
				return unknown("the daemon could not read the firewall")
			}
		},
		fix: func(_ context.Context, op options) error {
			// Only the broken-yes cases reach here, so `on` repairs a choice
			// already made instead of making one.
			c, err := dial(op)
			if err != nil {
				return err
			}
			defer func() { _ = c.Close() }()
			_, err = c.Call(protocol.MethodQuarantine, struct {
				Set string `json:"set"`
			}{"on"})
			return err
		},
	}
}

// quarantineFromDaemon asks for the state and trims it to the quarantine view.
func quarantineFromDaemon(op options) (protocol.QuarantineView, error) {
	c, err := dial(op)
	if err != nil {
		return protocol.QuarantineView{}, err
	}
	defer func() { _ = c.Close() }()
	raw, err := c.Call(protocol.MethodStatus, nil)
	if err != nil {
		return protocol.QuarantineView{}, err
	}
	var v protocol.RoomView
	if e := json.Unmarshal(raw, &v); e != nil {
		return protocol.QuarantineView{}, e
	}
	return v.Quarantine, nil
}

// engineCheck looks at whether the engine is where the daemon goes for it.
//
// It does not get fixed: the package puts the engine there, and a doctor that
// started downloading it would be installing software on its own, which is not
// what somebody asks for when they type `doctor --fix`.
func engineCheck(path string) check {
	return check{
		name: "the engine",
		look: func(context.Context, options) verdict {
			info, err := os.Stat(path)
			if os.IsNotExist(err) {
				return bad("it is not at %s", path).
					withFix("The package puts it there. Reinstalling puts it back.")
			}
			if err != nil {
				return unknown("could not look at %s: %v", path, err)
			}
			if info.Mode()&0o111 == 0 {
				return bad("%s is not executable (mode %04o)", path, info.Mode().Perm())
			}
			// The build id off the FILE, which answers "which engine is this
			// machine going to run". An engine from before the sentinel is a
			// normal find and reads as unknown, not as a failure.
			id, err := engineid.Scan(path)
			if err != nil {
				return unknown("could not read %s: %v", path, err)
			}
			build := id.String()
			if build == "" {
				build = "build unknown, older than the sentinel"
			}
			return good("%s, %d KiB, %s", path, info.Size()/1024, build)
		},
	}
}
