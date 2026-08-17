# Every command

Two programs, on two different machines. `kanpachi` is the client, on the
machine that plays. `kanpseed` is the meeting point, on a server.

This is a reference. For walkthroughs see
[run a 24/7 game server](run-a-game-server.md) and
[host your own seed](host-a-seed.md).

---

# `kanpachi`

The client. It talks to `kanpachid`, which is the service that actually holds
the room, over the control channel.

**It ships with the Linux package.** The Windows installer ships the window
instead, which drives the same daemon over the same protocol, so every command
below has a screen equivalent there.

**With no arguments it opens a wizard**, driven with the arrow keys. Everything
the wizard does has a subcommand below, and the wizard exists so a freshly
installed server can be used without having read this page.

## The room

| Command | What it does |
|---|---|
| `status` | what there is right now: room, members, network and protection |
| `watch` | the same, redrawn until you press Ctrl+C |
| `host [name]` | open a room and be its host |
| `join <code\|link>` | enter someone else's room |
| `leave` | close your room, leave someone else's, or stop going back to the last one |
| `link` | the invite link, to copy and hand out |
| `rotate` | renew the code: the links you handed out stop working |
| `rename <name>` | rename the room |

`host` takes close to a minute: two adapters have to come up, a credential has
to be exchanged, and the MTU has to be measured. Ctrl+C during it drops the CLI
and **does not close the room** — the room lives in the daemon.

`join` accepts the code in every form it travels: with or without dashes, upper
or lower case, with the seed attached (`A7K2M9QX@seed.example.com`) or as a full
URL with or without `https://`. A bare code without its seed is not enough,
because the same eight characters on two seeds are two different rooms.

## Who is in

| Command | What it does |
|---|---|
| `members` | who is in, by which path, and with what latency |
| `kick <name\|ip>` | kick someone out of the room |

`kick` and `rotate` are the pair that does the work of a ban list, and they are
independent on purpose: `kick` removes someone who is in without touching the
code, `rotate` stops a code that has been passed around without touching the
people playing. There is no ban: a kicked person holding a live code can come
back, and closing that door is what `rotate` is for.

## The game

| Command | What it does |
|---|---|
| `games` | the game catalogue, and which ones are installed |
| `game [id]` | activate a game profile; with no id, close the ports |

Activating a profile opens the ports it names, on this machine only, and only
toward the members present at that moment. With nobody in the room, nothing
opens. Detection orders the list and never filters it: a game Kanpachi failed to
find is still selectable.

## Checking

| Command | What it does |
|---|---|
| `exposure` | what Kanpachi has open, and toward whom |
| `diag` | the network as the engine sees it: NAT, UDP and MTU |
| `probe` | probe this machine **from** another one in the room |
| `protect` | put Kanpachi Protection back. It is idempotent |

`probe` is the one that answers a question the local machine cannot: what
somebody else in the room can actually reach here.

## What was left from before

| Command | What it does |
|---|---|
| `pending` | whether a room was left open from the previous start |
| `resume` | reopen that room with the same code |
| `discard` | forget it |
| `last` | the last room you entered as a guest |

The daemon reopens its own room by itself at startup, so these are for the case
where that did not happen or should not.

## The system

| Command | What it does |
|---|---|
| `seed [host]` | the registry this machine opens rooms on; with no host, shows it |
| `password` | the password of a registry that asks for one to host. Never on the command line |
| `doctor [--fix]` | what this needs to work, and what is broken |
| `upgrade [--check] [--version v] [--yes]` | fetch the new version. Restarts the service, so the room drops |
| `version` | which version this is |
| `help` | the list |

`seed` only governs rooms **this machine opens**. Rooms it joins follow the seed
that arrived inside the code.

## Options, valid in any position

| Flag | Meaning |
|---|---|
| `--nick <name>` | how the room sees you. Remembered |
| `--json` | the daemon's raw answer, unrendered |
| `--data <dir>` | a different data directory |
| `--pipe <path>` | a different control channel (`--socket` is the same flag) |
| `--timeout <duration>` | how long to wait for an answer, `90s` by default |

Only these five are intercepted globally. After the subcommand, any other
`--flag` belongs to the subcommand.

`--timeout` is validated before anything connects, so a bad value is reported as
a bad value rather than as the daemon being down.

## Exit codes

| Code | Meaning |
|---|---|
| `0` | it worked |
| `1` | it failed, and the message says how |
| `2` | usage: unknown command, missing flag value, bad flag |

## When the control channel does not open

The two frequent failures look identical from the client: the service stopped,
and the service running while you are not root. The error message names both,
because naming one sends half the people to look where the problem is not.

---

# `kanpseed`

The meeting point. One binary that is both the CLI and the registry.

## Commands a person runs

| Command | What it does |
|---|---|
| `init` | installs and configures everything. One single run |
| `upgrade [--check]` | updates to the latest published version; `--check` only reports |
| `doctor` | checks everything is as it should be, and says what is missing |
| `config` | shows or changes the ports, and rewrites the services |
| `password [--open]` | the password to HOST on this seed; `--open` removes it |
| `reconfigure` | rewrites the services the way this version wants them, and restarts |
| `nginx` | reprints the block to paste into the reverse proxy (`proxy` is the same) |
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
install**, because the machine's reverse proxy points at one of them.

`upgrade` runs `reconfigure` on its own. Running `reconfigure` by hand is how
you undo a unit somebody edited.

### About `password`

There is no `--password` flag and there will not be one: the argv of a process
is world readable, and a flag would put the seed's password in shell history as
well. It requires a terminal.

The password is bound to the configured domain, so `config` has to hold the name
people actually type. Changing the password rotates the signing key, which
throws every signed-in host out at once.

## The command systemd runs

`serve` starts the registry. There is no reason to invoke it by hand.

---

## See also

- [The seed's HTTP API](../../registry/API.md) — what `serve` exposes.
- [What gets installed, and where](reference-files.md).
