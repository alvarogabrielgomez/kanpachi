# Every command

Two programs, on two different machines. **`kanpachi`** is the client, on the
machine that plays. **`kanpseed`** is the meeting point, on a server.

For walkthroughs rather than a list, see
[run a 24/7 game server](run-a-game-server.md) and
[host your own seed](host-a-seed.md).

---

# `kanpachi`

The Linux client. It ships with the `.deb`; the Windows installer ships the
window instead, which drives the same daemon over the same protocol, so every
command here has a screen equivalent there.

`kanpachi` decides nothing. It opens the control channel, sends one request and
renders the answer. **The room lives in `kanpachid`**, so Ctrl+C on a slow
command drops the client and never the room.

## Running it with no arguments

```sh
kanpachi
```

opens a wizard driven with the arrow keys: it asks for a name the first
time, then offers what makes sense for the state the machine is in. It exists so
a server installed a minute ago is usable without reading this page. Everything
it does has a subcommand below.

## The room

### `status`

What there is right now: the room and its code, the members, the network path
each one arrives by, and whether Kanpachi Protection is in place.

It is the command to run first when something looks wrong, and the one to pipe
through `--json` when a script needs the same facts.

### `watch`

`status`, redrawn until you press Ctrl+C. The daemon's own heartbeat drives the
redraw, so what changes on screen is what changed.

### `host [name]`

Opens a room and makes this machine its host.

The name is everything after the subcommand, spaces and all, so quoting is
optional: `kanpachi host Zomboid nights` and `kanpachi host "Zomboid nights"`
are the same room. **With no name it uses the machine's hostname**, which leaks
nothing, because the name travels inside the sealed card and the seed never sees
it.

It takes close to a minute, and the command says so before it starts: two
adapters have to come up, a credential has to be exchanged and the MTU has to be
measured.

Up to three questions come before that minute, and `--yes` answers all three:

1. **Displacing what is already open.** Hosting while already hosting closes the
   existing room, so it asks.
2. **Trusting the registry.** It shows which seed this machine mints codes on.
   It comes *before* the "this takes a minute" line, on purpose: the question is
   whether to start the wait.
3. **Opening a blocking firewall.** Only if the daemon reports something of the
   machine's own, usually `ufw`, standing in the way. The sentence names the
   exact commands, composed by the daemon from the same closed list it executes,
   and the daemon undoes the change when the room closes.

**The room comes up with no ports open.** Choose a game with `game` to open any.

### `join <code|link>`

Enters somebody else's room.

Every form works, as long as it carries the registry:

```
VA3BSF5L@seed.example.com
seed.example.com/VA3BSF5L
https://seed.example.com/VA3BSF5L#key
kanpachi://VA3BSF5L@seed.example.com
```

Dashes, case and the `https://` are all optional. **A bare code is not enough**:
the same eight characters on two seeds are two different rooms.

The text is passed to the daemon **exactly as pasted**, with nothing stripped
and nothing prepended. Parsing it is the daemon's job, because that is the
product's hostile-input boundary, and normalising it here would mean testing
something other than what arrives.

Same three questions as `host`, in the same order, and the same `--yes`. The
registry question only appears when the text parsed well enough to name one:
asking about a seed that could not be read would mean naming a machine nobody
mentioned.

### `leave`

One command for three situations, because from the user's side they are the same
intention: close the room you host, walk out of somebody else's, or stop
Kanpachi going back to the last one by itself.

### `link`

Prints the invite link and nothing else (no banner, no label), so it drops
straight into a `$(...)`:

```sh
echo "join me: $(kanpachi link)"
```

With no room open it fails and says so.

### `rotate`

Renews the code. It prints what that means before doing it: **the links already
handed out stop working, and whoever is inside stays inside.**

This is one half of the pair that replaces a ban list. The other is `kick`.

### `rename <name>`

Renames the room. Everything after the subcommand is the name. Presentation
only: it travels inside the encrypted card, and the seed cannot read it.

## Who is in

### `members`

Who is in, which path each one arrives by, and with what latency. **Direct is
the normal case**; somebody behind symmetric NAT falls back to relay through the
seed, and the list says so rather than hiding it.

### `kick <name|ip>`

Removes somebody from the room. It revokes their credential, and their ports go
with them.

Accepts the name shown on screen as well as the virtual IP, and resolves the
name locally before sending, because the protocol only understands addresses.
Two guards on that resolution:

- **An ambiguous name is refused, not guessed.** Two members called `pana`
  produce an error listing both IPs, because kicking the wrong one has no undo.
- **Kicking yourself is refused**, with a pointer to `leave`.

If the kick half-succeeds the command does not swallow the error: it means the
person is out of the room and a port of theirs stayed open.

**There is no ban.** Someone kicked who still holds a live code can come back;
`rotate` is what closes that door.

## The game

### `games`

The catalogue, and which of those games this machine has installed. Detection
orders the list and never filters it: a game Kanpachi failed to find is still
selectable, because Kanpachi has no business being right about what is
installed.

### `game [id]`

Activates a profile. The ports it names open **on this machine only, and only
toward the members present at that moment**. With nobody in the room the desired
set is empty and nothing opens, which is correct and not a failure.

**With no id it closes them all.** That is a legal instruction rather than a
missing argument: `kanpachi game` on its own means "stop having ports open".

## Checking

### `exposure`

What Kanpachi has open, and toward whom, port by port with its real number. This
is the command the product exists to be able to answer.

### `diag`

The network as the engine sees it: NAT type, UDP reachability, MTU. It goes out
to the network, so it takes a few seconds and says so first.

### `probe`

Asks another machine in the room to probe this one. It answers the question the
local machine structurally cannot: what somebody else in the room can reach
here. It needs somebody else in the room.

### `protect`

Puts Kanpachi Protection back. **Idempotent**, so running it when nothing is
wrong is a no-op rather than a risk.

## What was left from before

The daemon reopens its own room by itself at startup, so these four are for when
that did not happen, or should not.

### `pending`

Whether a room was left open by the previous start, and if so its name, code,
game and when it was saved. It ends by naming the two commands that act on it.

### `resume`

Reopens it with the same code, the same network identity, the same subnet and
the same game profile.

### `discard`

Forgets it. Answers `Forgotten.`

### `last`

The last room you entered **as a guest**, which is a different thing from
`pending` and worth not confusing: that one is yours and still is, this one is a
saved code you would have to re-enter with, credential exchange included.

The useful part is the line about whether Kanpachi returns to it on its own. The
saved file outlives both leaving and being kicked, so its presence says nothing;
only the flag does. When it is off, the command prints the exact `join` line to
go back by hand.

## The system

### `name [name]`

The name rooms show you by. With no argument it shows it.

**It is one name per machine, shared with the window and the wizard.** They all
ask the daemon, which keeps it in `profile.json` beside the rest of its state,
so changing it in one place changes it everywhere.

If nobody has chosen one, rooms show you by a name derived from the machine's
own, cleaned up to letters and digits, twelve at most. That derived name is a
**suggestion and is never written down**: `host` and `join` use it and say so on
stderr, and this command prints it with the line to type if you want it kept.
A suggestion saved to disk stops being distinguishable from a name somebody
chose, and then it wins over the real one.

`--nick <name>` does the same thing on the way into a room, and is remembered
for the same reason: you typed it.

### `seed [host]`

The registry this machine **opens** rooms on. With no argument it shows it.

Rooms you *join* follow whatever seed arrived inside the code, and this setting
does not touch them.

There is no default seed, and that is why this command exists: since the
registry started travelling inside every code, creating a room has no code yet
to read one from, because the code is what the registry mints.

If the machine has none configured it says so, and **suggests the seed of the
last room you entered without adopting it**, with the command written out to
copy. Adopting it silently would mean your next room is hosted on a stranger's
server because you once accepted an invitation.

### `quarantine [on|off]`

Closes this machine's risky server ports (file sharing, remote desktop, remote
management and printer discovery) **on every network it is connected to**, not
only on Kanpachi's. With no argument it tells you the state.

**Having trouble sharing a folder from this PC, or reaching it over Remote
Desktop? This is why.** `kanpachi quarantine off` puts it back and takes effect
immediately.

It is your decision and Kanpachi asks it once, at the door of the first `host`
or `join`, listing the exact ports and with no default answer. `--quarantine
on|off` on those commands answers it from a script; with no terminal and no flag
they refuse, because the absence of a terminal is not an answer.

Saying yes closes them until you say otherwise, and every start repairs what
went missing. Saying no removes what a yes had closed. Nothing else ever removes
it (not a sweep, not a restart, not `--reset`), and a machine without it says
so every time a room opens, because the notice is the state and not a scolding.

What it does NOT change: reaching OTHER machines. The blocks compare the LOCAL
port, so mounting a share, opening a remote desktop or `ssh`-ing out are all
untouched. And something else entirely contains the room, so turning this off
does not open your room to anybody.

### `password`

The password of a registry that asks for one to host. Joining never needs it.

**It takes no arguments, and there is no `--password` flag.** On Linux any user
can read `/proc/<pid>/cmdline`, and the shell keeps a history: a flag would put
somebody else's seed password in two places that outlive the command.

You type it, masked. **When stdin is redirected the command reads it from
there**, which is the supported door for a script:

```sh
kanpachi password < /etc/kanpachi/seed.pw   # a 0600 file never reaches an argument list
```

### `doctor [--fix]`

What this needs to work, and what is broken. On Linux it checks the control
channel, the engine, `/dev/net/tun`, the kernel, the two services, the base
quarantine, the channel permissions, and firewalls that are not ours.

That last one catches the classic VPS failure: `ufw` active and not letting the
engine's port in, which produces a symptom indistinguishable from somebody
else's home router being at fault.

Two properties worth relying on:

- **Looking never writes**, not even with `--fix`. The check and the repair are
  separate functions, and the repair is absent for everything that is not ours,
  which turns the rule into something you cannot skip by accident.
- **After fixing it looks again** rather than trusting the fix. A command that
  returned zero is not evidence that the state changed.

### `upgrade [--check] [--version <v>] [--yes]`

Fetches a new version. It restarts the service, so the room drops and reopens
itself on the way back up.

| Flag | What it does |
|---|---|
| `--check` | reports what is published, installs nothing |
| `--version <v>` | installs exactly that tag, **even if it is the same or older** |
| `--yes` | does not ask |

`--version` is what makes this a way back: without it, a version that is already
current short-circuits with `Already up to date`, and naming a tag on purpose
skips that shortcut.

A hand-built binary reports honestly. Calling it up to date or out of date would
both be false, so `--check` says what it knows: this binary is `dev`, and the
latest published is *X*.

### `version`

Which version this is, and what it was built against.

### `help`

The list, grouped by what gets done first rather than alphabetically. `--help`,
`-help` and `-h` reach it from anywhere.

## Options, valid in any position

| Flag | Meaning |
|---|---|
| `--nick <name>` | how the room sees you. The daemon remembers it, so it is needed once, and `kanpachi name` shows or changes it |
| `--json` | the daemon's raw answer, unrendered |
| `--data <dir>` | a different data directory |
| `--pipe <path>` | a different control channel. `--socket` is the same flag |
| `--timeout <duration>` | how long to wait for an answer. `90s` by default |

**Only these five are global.** After the subcommand, any other `--flag` belongs
to the subcommand, which is why `--yes` works on `host`, `join` and `upgrade`
and nowhere else.

`kanpachi` parses `--timeout` before anything connects. It used to validate the
value after the connection was open, so `kanpachi --timeout abc status` with the
daemon down reported the daemon being down: two wrong things at once, and it
named the one the person had not caused.

## Using it in a script

**Without a terminal and without `--yes`, the command refuses rather than
assumes the answer.** That is deliberate and it is the shape the "nothing
from outside takes effect without a confirmation inside" rule takes on Linux:
resolving a missing prompt as a yes would delete the confirmation where nobody
is watching.

A non-interactive `host` or `join` needs `--yes`, and saying so is the point.

`--json` gives the daemon's answer unrendered, which is the stable surface. The
human rendering is not.

### Exit codes

| Code | Meaning |
|---|---|
| `0` | it worked |
| `1` | it failed, and the message says how |
| `2` | usage: unknown command, missing flag value, bad flag |

### When the control channel does not open

The two frequent failures look identical from here: the service is stopped, and
the service is running while you are not root. The error names both, because
naming one sends half the people to look where the problem is not.

```sh
systemctl status kanpachid
sudo kanpachi status
```

---

# `kanpseed`

The meeting point. One binary that is both the CLI and the registry. It runs on
the server, not on the machine that plays.

## Commands a person runs

| Command | What it does |
|---|---|
| `init` | installs and configures everything. One single run |
| `upgrade [--check]` | updates to the latest published version; `--check` only reports |
| `doctor` | checks everything is as it should be, and says what is missing |
| `config` | shows or changes the ports, and rewrites the services |
| `password [--open]` | the password to HOST on this seed; `--open` removes it |
| `reconfigure` | rewrites the services the way this version wants them, and restarts |
| `nginx` | prints the block to paste into the reverse proxy. `proxy` is the same |
| `uninstall [--yes]` | removes the services and the binaries |
| `version`, `help` | |

All of them except `nginx`, `version` and `help` need root, and they say so
early rather than failing halfway through writing `/etc`.

### `init` flags

| Flag | Default |
|---|---|
| `--domain <name>` | asked interactively |
| `--port <n>` | first free port from `8010` |
| `--engine-port <n>` | `11010` |
| `--page <path>` | the `index.html` next to the binary |

Re-running `init` is a valid upgrade path and **keeps the ports of an existing
install**, because the machine's reverse proxy points at one of them. `upgrade`
runs `reconfigure` on its own; running `reconfigure` by hand is how you undo a
unit somebody edited.

### About `password`

Same rule as the client's, for the same reason: no `--password` flag, and a
terminal is required.

The seed binds the password to the configured domain, so `config` has to hold
the name people type. Changing it rotates the signing key, which throws every
signed-in host out at once; they get back in by typing the new one.

## The command systemd runs

`serve` starts the registry. There is no reason to invoke it by hand.

---

## See also

- [Install on Linux](install-linux.md) — getting `kanpachi` onto a machine.
- [Run a 24/7 game server](run-a-game-server.md) — these commands in the order
  a server needs them.
- [What gets installed, and where](reference-files.md).
- [The seed's HTTP API](../../registry/API.md) — what `kanpseed serve` exposes.
