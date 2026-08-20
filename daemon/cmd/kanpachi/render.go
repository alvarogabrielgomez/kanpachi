package main

// How what the daemon answers gets painted.
//
// # No colour, and that is not laziness
//
// The output of this is going to end up in an `ssh`, in a `tmux`, in a file
// through `> out.txt` and pasted into a chat when somebody asks for help. Colour
// codes survive all four places and only look right in the first, so what they
// would add gets charged back everywhere else. The one thing written outside
// plain text is the screen clear `watch` needs, without which it is not a
// screen.
//
// # The wire enums get translated HERE
//
// The protocol sends stable English names on purpose, so the UI does not break
// when somebody finds a better word. Turning them into something readable is the
// job of whoever paints, which is this. That the destination here is also
// English does not make the translation pointless: `unreachable` is a protocol
// value and "no one answered, so this proves nothing" is a sentence for a
// person.

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/accentiostudios/kanpachi/daemon/transport/protocol"
)

// clearScreen leaves the cursor at the top and wipes what is below.
//
// It wipes downwards (`ESC[J`) instead of wiping everything and jumping: that
// way the redraw does not flicker, because the new screen gets written over the
// old one rather than over a black gap.
func clearScreen(w io.Writer) { fmt.Fprint(w, "\033[H\033[J") }

const rule = "  ────────────────────────────────────────────────────────────────"

// printReturning paints a machine on its way back into a room it was in.
//
// # It goes where "no room is open" goes, and that is not a contradiction
//
// Going back is NOT being in a room: there is no tunnel, no ports and no
// members, and `Conn` says `idle` throughout. What there is is a standing intent
// and a clock. So it belongs under that line rather than in the room block, and
// the two things it has to say are where it is going and when it tries next.
//
// The registry being silent is called out separately because it is a different
// cause for the same wait: "nobody is hosting yet" and "the meeting server is
// not answering" send somebody to look in different places.
func printReturning(w io.Writer, v *protocol.ReturningView, seedDown bool) {
	name := v.Name
	if name == "" {
		name = "(unnamed)"
	}
	fmt.Fprintf(w, "  Going back to  %s  %s@%s\n", name, v.Code, v.Seed)

	when := "now"
	if v.NextInMS > 0 {
		when = "in " + millis(v.NextInMS)
	}
	fmt.Fprintf(w, "  Attempt %d, next %s.\n", v.Attempts+1, when)
	switch {
	case seedDown:
		fmt.Fprintln(w, "  The meeting server is not answering, so nothing is being decided yet.")
	case v.Reason != "":
		fmt.Fprintf(w, "  Last: %s\n", v.Reason)
	}
	fmt.Fprintln(w, "\n  `kanpachi leave` stops going back. `kanpachi join <code>` enters another room.")
}

// gameAddress builds what a player pastes inside the game: the host's address
// and the active profile's first port.
//
// # Why the catalog comes in as an argument
//
// Because the port is NOT in the room view. The wire carries the game's id and
// name, never its ports, so whoever wants the address resolves the id against
// the catalog — which is exactly what the window does before it paints the same
// box. The two faces resolve the same way from the same source rather than the
// daemon growing a display string.
//
// Empty whenever any half is missing, and the caller says nothing instead of
// printing a half address. Missing halves are ordinary: no game is active, the
// members table has not arrived yet, or the host is down.
func gameAddress(st protocol.RoomView, catalog []protocol.GameView) string {
	if st.Game == "" {
		return ""
	}
	host := ""
	for _, p := range st.Peers {
		if p.Host {
			host = p.IP
			break
		}
	}
	if host == "" {
		return ""
	}
	for _, g := range catalog {
		if g.ID != st.Game || len(g.HostPorts) == 0 {
			continue
		}
		return fmt.Sprintf("%s:%d", host, g.HostPorts[0].From)
	}
	return ""
}

// printRoom is the main screen.
func printRoom(w io.Writer, st protocol.RoomView, catalog []protocol.GameView) {
	if st.Conn == "idle" || st.Conn == "" {
		fmt.Fprintln(w, "  No room is open.")
		if st.LastExit != "" {
			fmt.Fprintf(w, "  The last one ended: %s\n", exitReason(st.LastExit))
		}
		if st.Returning != nil {
			printReturning(w, st.Returning, st.SeedDown)
			return
		}
		fmt.Fprintln(w, "\n  `kanpachi host` opens one. `kanpachi join <code>` enters someone else's.")
		return
	}

	fmt.Fprintln(w, rule)
	name := st.Name
	if name == "" {
		name = "(unnamed)"
	}
	fmt.Fprintf(w, "  %-34s %s, %s\n", name, role(st.Role), connState(st.Conn))
	fmt.Fprintln(w, rule)

	// BOTH forms, with the registry stuck to the short one.
	//
	// An invite ID only means something on the registry that issued it, so
	// showing it bare offers, for copying, a thing that on another registry is a
	// different room. A terminal has no copy button: what stands in for it is
	// having both forms there to select, the short one to dictate and the link
	// to paste into a chat.
	if st.Code != "" {
		fmt.Fprintf(w, "  Code     %s", hyphenated(st.Code))
		if st.Seed != "" {
			fmt.Fprintf(w, "@%s", st.Seed)
		}
		fmt.Fprintln(w)
	}
	if st.Link != "" {
		fmt.Fprintf(w, "  Link     %s\n", st.Link)
	}
	if st.CodeLost {
		// Said loudly because the room STILL works for the people inside: what
		// broke is anybody new getting in, and nothing else on the screen would
		// give it away.
		fmt.Fprintln(w, "  WARNING  the registry no longer knows this code: nobody new can join.")
		fmt.Fprintln(w, "           `kanpachi rotate` fixes it, and voids the links you handed out.")
	}
	if st.SeedDown && !st.CodeLost {
		// The transient sibling of CodeLost, and the terminal painted NEITHER of
		// them until now — it travelled on the wire and no face read it. Same
		// consequence, opposite cure: this one clears up by itself, so it says so
		// rather than pointing at a command.
		fmt.Fprintln(w, "  WARNING  the registry is not answering, so nobody new can join right now.")
		fmt.Fprintln(w, "           Whoever is already in stays in. It clears up on its own.")
	}
	if st.LocalIP != "" {
		fmt.Fprintf(w, "  Your IP  %s", st.LocalIP)
		if st.Subnet != "" {
			fmt.Fprintf(w, "  on %s", st.Subnet)
		}
		fmt.Fprintln(w)
	}
	if st.GameName != "" {
		fmt.Fprintf(w, "  Game     %s%s\n", st.GameName, healthSuffix(st.GameHealth))
	} else if st.Game != "" {
		fmt.Fprintf(w, "  Game     %s%s\n", st.Game, healthSuffix(st.GameHealth))
	}
	// Where the game is listening when the room does not reach it there, and
	// what to do about that. Three shapes rather than one, because a single
	// sentence was wrong for two of them: a guest cannot bind somebody else's
	// server and read "bind the server to 0.0.0.0" as a job it had no way of
	// doing, and a host that is already redirecting had that instruction sitting
	// directly above the line saying the traffic already arrives. The window
	// answers this per role for the same reason, and these are its words.
	switch {
	case st.GameRedirectedTo != "":
		// Said out loud on both faces, always. A redirect nobody can see is the
		// opposite of what this program is for.
		if st.Role == "host" {
			fmt.Fprintf(w, "  Sent on  %s, because that is where the game listens. Nothing to do.\n",
				st.GameRedirectedTo)
		} else {
			fmt.Fprintf(w, "  Sent on  %s, where the host's game listens. The room reaches it.\n",
				st.GameRedirectedTo)
		}
	case st.GameWhere != "" && st.Role == "host":
		// The room's own address first and 0.0.0.0 behind it: binding to
		// Kanpachi's address is what the product recommends, and the wider bind
		// is the fallback for a game that will not let you choose.
		fmt.Fprintf(w, "  Bound to %s, which the room does not reach.\n", st.GameWhere)
		if st.LocalIP != "" {
			fmt.Fprintf(w, "           Bind the server to %s, or to 0.0.0.0.\n", st.LocalIP)
		} else {
			fmt.Fprintln(w, "           Bind the server to 0.0.0.0.")
		}
	case st.GameWhere != "":
		fmt.Fprintf(w, "  Bound to %s on the host's machine, which the room does not reach.\n",
			st.GameWhere)
		fmt.Fprintln(w, "           Only the host can fix it.")
	}
	// The address a player pastes inside the game. The window has painted it
	// since it existed and this face never did, so somebody on a headless host
	// had to read the members table and the profile's ports and put the two
	// together by hand. It is the same line, and the verb changes with the role
	// for the same reason it does over there: the host hands it out and a guest
	// uses it, and saying the wrong one sends half the room to do the wrong
	// thing.
	if addr := gameAddress(st, catalog); addr != "" {
		if st.Role == "host" {
			fmt.Fprintf(w, "  Hand out %s\n", addr)
		} else {
			fmt.Fprintf(w, "  Connect  %s\n", addr)
		}
	}
	if st.MissingGame != "" {
		fmt.Fprintf(w, "  Missing  %s: it is active in the room and you do not have it installed\n",
			st.MissingGame)
	}

	if st.Role == "guest" && !st.HostPresent {
		fmt.Fprintf(w, "  Host     gone for %s\n", millis(st.HostGoneForMS))
	}
	// Before the tunnel line: this is the one that explains the pause. A room
	// that goes quiet for ten seconds looks like a hang, and this is the daemon
	// saying it is asking the host for a new credential right now.
	if st.Rejoining {
		fmt.Fprintln(w, "  Room     asking the host for a credential again")
	}
	if st.ReconnectingForMS > 0 {
		fmt.Fprintf(w, "  Tunnel   reconnecting for %s\n", millis(st.ReconnectingForMS))
	}

	fmt.Fprintln(w)
	printMembers(w, st)
	printCanary(w, st.Canary)

	if len(st.Alerts) > 0 {
		fmt.Fprintln(w, "\n  ALERTS")
		for _, a := range st.Alerts {
			fmt.Fprintf(w, "    %-20s %s\n", alertName(a.Kind), a.Detail)
		}
	}
}

func printMembers(w io.Writer, st protocol.RoomView) {
	if len(st.Peers) == 0 {
		fmt.Fprintln(w, "  Nobody is in the room.")
		return
	}
	fmt.Fprintf(w, "  MEMBERS (%d)\n", len(st.Peers))
	for _, p := range st.Peers {
		marks := ""
		if p.Host {
			marks += " [host]"
		}
		if p.Self {
			marks += " [you]"
		}
		latency := "-"
		if p.RTTMS > 0 {
			latency = fmt.Sprintf("%d ms", p.RTTMS)
		}
		fmt.Fprintf(w, "    %-14s %-16s %-8s %s%s\n",
			p.Name, p.IP, peerPath(p.Path), latency, marks)
	}
}

// printCanary shows the last round of Kanpachi Protection.
//
// The two sources show separately because they are two different things: what
// the host saw with its own socket cannot be faked, and what the members said
// are messages, and a message can lie. Merging them into one sentence would
// throw away exactly what makes it believable.
func printCanary(w io.Writer, c protocol.CanaryView) {
	if !c.Measured {
		return
	}
	fmt.Fprintf(w, "\n  PROTECTION   %s", canaryVerdict(c.Verdict))
	if c.Port != 0 {
		fmt.Fprintf(w, "  (port %d)", c.Port)
	}
	fmt.Fprintln(w)
	if c.Touched {
		fmt.Fprintln(w, "    the host saw traffic come in there, with its own socket")
	}
	for _, a := range c.Answers {
		fmt.Fprintf(w, "    %-14s tcp %s, udp %s\n", a.From, outcome(a.TCP), outcome(a.UDP))
	}
}

func printGames(w io.Writer, games []protocol.GameView) {
	if len(games) == 0 {
		fmt.Fprintln(w, "  The catalog is empty.")
		return
	}
	// Sorted by name: the daemon returns them in catalog order, which is not the
	// order somebody scanning the list with their eyes expects.
	sort.Slice(games, func(i, j int) bool {
		return strings.ToLower(games[i].Name) < strings.ToLower(games[j].Name)
	})
	fmt.Fprintf(w, "  %-24s %-34s %s\n", "ID", "NAME", "")
	for _, g := range games {
		marks := []string{}
		if g.Installed {
			marks = append(marks, "installed")
		}
		if g.Verified {
			marks = append(marks, "verified")
		}
		fmt.Fprintf(w, "  %-24s %-34s %s\n", g.ID, g.Name, strings.Join(marks, ", "))
	}
	fmt.Fprintln(w, "\n  `kanpachi game <id>` activates one.")
}

// printExposure shows what is open.
//
// # Why the first thing is whether anybody could look
//
// Because an empty list means two very different things, that nothing is open or
// that Kanpachi could not read what it has in place, and confusing them shows
// reassurance where there is blindness. The protocol carries an explicit boolean
// for that and it gets respected here.
func printExposure(w io.Writer, v protocol.ExposureView) {
	if !v.Measured {
		fmt.Fprintln(w, "  Kanpachi could NOT read what it has in the firewall.")
		fmt.Fprintln(w, "  This does not say nothing is open: it says nobody knows.")
		return
	}
	fmt.Fprintf(w, "  Gate: %s\n", gateState(v.Gate))
	if len(v.Ports) == 0 {
		fmt.Fprintln(w, "  Kanpachi has no port open.")
	}
	for _, p := range v.Ports {
		ports := fmt.Sprintf("%d", p.From)
		if p.To != p.From {
			ports = fmt.Sprintf("%d-%d", p.From, p.To)
		}
		what := "game"
		if p.Control {
			what = "room channel"
		}
		toward := strings.Join(append(append([]string{}, p.Members...), p.Nets...), ", ")
		if toward == "" {
			// Empty NEVER means "to anybody": the domain cannot express that. It
			// is said this way so nobody reads it backwards.
			toward = "nobody"
		}
		state := "applied"
		if !p.Applied {
			state = "ASKED FOR AND NOT APPLIED"
		}
		fmt.Fprintf(w, "    %-4s %-12s %-14s toward %s [%s]\n", p.Proto, ports, what, toward, state)
	}
	for _, u := range v.Unexpected {
		fmt.Fprintf(w, "    RULE NOBODY ASKED FOR: %s\n", u)
	}
}

func printNetwork(w io.Writer, v protocol.NetView) {
	if v.NATKind != "" {
		fmt.Fprintf(w, "  NAT      %s\n", v.NATKind)
	}
	fmt.Fprintf(w, "  UDP      %s\n", map[bool]string{true: "blocked", false: "gets through"}[v.UDPBlocked])
	if v.MTU > 0 {
		fmt.Fprintf(w, "  MTU      %d\n", v.MTU)
	}
	if v.Subnet != "" {
		fmt.Fprintf(w, "  Subnet   %s", v.Subnet)
		if v.SubnetReason != "" {
			fmt.Fprintf(w, "  (%s)", v.SubnetReason)
		}
		fmt.Fprintln(w)
	}
	for seed, rtt := range v.SeedRTTMS {
		fmt.Fprintf(w, "  Registry %s: %d ms\n", seed, rtt)
	}
}

// printProbe shows the one measurement in this product that crosses the network
// for real, and therefore the one that asks for most care when reading it.
func printProbe(w io.Writer, v protocol.ProbeView) {
	if !v.Measured {
		fmt.Fprintln(w, "  Nothing was measured.")
		return
	}
	fmt.Fprintf(w, "  Probed from %s (%s): %s\n", v.Name, v.Target, probeVerdict(v.Verdict))
	for _, r := range v.Results {
		fmt.Fprintf(w, "    %-6d %-11s %-24s %s\n",
			r.Port, probeKind(r.Kind), r.Label, outcome(r.Outcome))
	}
}

// ─── The wire enums, for reading ─────────────────────────────────────────────

func connState(s string) string {
	switch s {
	case "idle":
		return "no room"
	case "resolving":
		return "resolving"
	case "connecting":
		return "connecting"
	case "connected":
		return "connected"
	case "degraded":
		return "degraded"
	case "reconnecting":
		return "reconnecting"
	default:
		// What is not recognised shows AS IT CAME instead of being translated
		// into something reassuring. An old client talking to a new daemon has to
		// show the word it does not understand, not invent another one.
		return s
	}
}

func role(s string) string {
	switch s {
	case "host":
		return "you are the host"
	case "guest":
		return "you are a guest"
	default:
		return s
	}
}

func peerPath(s string) string {
	switch s {
	case "direct":
		return "direct"
	case "relay":
		return "relayed"
	case "self":
		return "you"
	case "unconfirmed":
		return "path unknown"
	default:
		return "?"
	}
}

func outcome(s string) string {
	switch s {
	case "answered":
		return "answered"
	case "refused":
		return "refused"
	case "silent":
		return "silent"
	case "failed":
		return "failed"
	default:
		return s
	}
}

func gateState(s string) string {
	switch s {
	case "present":
		return "up"
	case "absent":
		return "NOT UP"
	default:
		return "unchecked"
	}
}

func probeVerdict(s string) string {
	switch s {
	case "leaky":
		// POSITIVE proof of exposure: something nobody asked for answered.
		return "LEAK: something nobody opened answered from outside"
	case "unreachable":
		// It proves nothing, and saying so matters: it looks the same on a
		// hardened machine as on one that is switched off.
		return "nobody answered, not even the room channel, so this proves nothing"
	case "sealed":
		return "sealed: the channel answers and nothing forbidden does"
	default:
		return "not measured"
	}
}

func canaryVerdict(s string) string {
	switch s {
	case "leaking":
		return "LEAKING"
	case "clean":
		return "clean"
	case "unconfirmed":
		return "unconfirmed"
	case "mismatch":
		return "answers that do not agree"
	default:
		return "unchecked"
	}
}

func probeKind(s string) string {
	switch s {
	case "reference":
		return "reference"
	case "forbidden":
		return "forbidden"
	case "game":
		return "of the game"
	default:
		return s
	}
}

func exitReason(s string) string {
	switch s {
	case "user":
		return "you closed it"
	case "kicked":
		return "you were kicked"
	case "host_gone":
		return "the host left"
	case "room_closed":
		return "the host closed it"
	case "failed":
		return "it failed"
	case "tunnel_lost":
		return "the tunnel was lost"
	default:
		return s
	}
}

// printQuarantine is the symptom→cause bridge: the person who reads this
// arrives with "I cannot share a folder" or "I cannot reach my PC over Remote
// Desktop", never with the word quarantine. The state names the symptom in
// both directions, says what it does NOT mean, and hands the exact command.
func printQuarantine(w io.Writer, q protocol.QuarantineView) {
	ports := make([]string, len(q.Ports))
	for i, p := range q.Ports {
		ports[i] = strconv.Itoa(int(p))
	}

	fmt.Fprintln(w)
	switch q.Verdict {
	case "applied":
		fmt.Fprintln(w, "  QUARANTINE   on: Kanpachi is blocking file sharing and Remote Desktop")
		fmt.Fprintln(w, "               INTO this PC, on all its networks.")
		fmt.Fprintf(w, "  Ports        %s\n", strings.Join(ports, ", "))
		fmt.Fprintln(w, "\n  Having trouble sharing a folder FROM this PC, or entering it over")
		fmt.Fprintln(w, "  Remote Desktop? This is why. `kanpachi quarantine off` turns it off")
		fmt.Fprintln(w, "  and they work again right away. Reaching OTHER machines' shares and")
		fmt.Fprintln(w, "  desktops was never affected.")
	case "partial":
		fmt.Fprintf(w, "  QUARANTINE   partial: of %d rules, %d are missing, %d disabled, %d edited.\n",
			q.Total, q.Missing, q.Disabled, q.Drifted)
		fmt.Fprintln(w, "\n  Something other than Kanpachi changed it. `kanpachi quarantine on`")
		fmt.Fprintln(w, "  repairs what is missing.")
	case "absent":
		fmt.Fprintln(w, "  QUARANTINE   off: this PC answers file sharing and Remote Desktop on")
		fmt.Fprintln(w, "               every network it joins.")
		fmt.Fprintf(w, "  Would close  %s\n", strings.Join(ports, ", "))
		fmt.Fprintln(w, "\n  What that means: on a bar's or a hotel's wifi, people on that network")
		fmt.Fprintln(w, "  can ask this PC for its shared folders or its desktop. What it does")
		fmt.Fprintln(w, "  NOT mean: Kanpachi did not open any of it and never does, your room's")
		fmt.Fprintln(w, "  members cannot reach it while the room is open, and nobody reaches it")
		fmt.Fprintln(w, "  from the internet. `kanpachi quarantine on` closes it — recommended,")
		fmt.Fprintln(w, "  unless this PC shares folders or gets entered over Remote Desktop.")
	default:
		fmt.Fprintln(w, "  QUARANTINE   could not be checked. That is not \"off\": it is that")
		fmt.Fprintln(w, "               nobody could look. `kanpachi doctor` says more.")
	}

	switch q.Decision {
	case "yes":
		fmt.Fprintln(w, "\n  Your answer   yes, close them. Every start repairs it.")
	case "no":
		fmt.Fprintln(w, "\n  Your answer   no, leave them open. Kanpachi respects it and keeps")
		fmt.Fprintln(w, "                saying what state the machine is in.")
	default:
		fmt.Fprintln(w, "\n  Your answer   not given yet. Opening or joining a room from the")
		fmt.Fprintln(w, "                terminal will ask once.")
	}
}

func alertName(s string) string {
	switch s {
	case "firewall_off":
		return "firewall off"
	case "rules_tampered":
		return "rules tampered with"
	case "router_mapping":
		return "mapping on the router"
	case "foreign_rule":
		return "foreign rule"
	case "lobby_conflict":
		return "lobby clash"
	case "kick_incomplete":
		return "partial kick"
	case "audit_failed":
		return "audit failed"
	case "canary_leaking":
		return "leak detected"
	case "game_lost":
		return "game not restored"
	case "quarantine_off":
		return "quarantine off"
	default:
		return s
	}
}

// ─── Formats ─────────────────────────────────────────────────────────────────

// hyphenated splits the code into two halves, which is how it gets read out loud
// and how the invitation page shows it.
func hyphenated(code string) string {
	if len(code) != 8 {
		return code
	}
	return code[:4] + "-" + code[4:]
}

func millis(ms int64) string {
	switch {
	case ms < 1000:
		return fmt.Sprintf("%d ms", ms)
	case ms < 60_000:
		return fmt.Sprintf("%d s", ms/1000)
	default:
		return fmt.Sprintf("%d min", ms/60_000)
	}
}

// healthSuffix says whether the game's server is up, next to its name.
//
// The host measured it on its own machine and it travels with the room's
// announcement, so a guest reads the same answer without probing anything,
// which for a UDP game it could not do at all. Nothing is printed when nobody
// knows: an empty suffix is the honest shape of "not measured".
func healthSuffix(health string) string {
	switch health {
	case "listening":
		return "  (healthy)"
	case "silent":
		return "  (nothing listening)"
	case "elsewhere":
		return "  (listening elsewhere)"
	default:
		return ""
	}
}
