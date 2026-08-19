# Kanpachi Protection

**Everything the game did not ask for is closed on the virtual adapter.**

That is the whole promise, and it is the reason this project is split across
three repositories instead of being a thin wrapper around an existing VPN. This
document is the shared statement of what the promise means, what each part does
to keep it, and what it does not cover.

It is written in English because the public engine and library repositories link
to it, and it stays at the repository root for the same reason: two other
repositories link to it by URL. Read it when the question is *why*; when there
is a task pending, the guides are in
[the public documentation](docs/public/README.md). Kanpachi's design documents
are in Spanish and live in [`docs/`](docs/).

## The problem it solves

Playing an old game with friends over the internet usually means one of two
things, and both are worse than they look.

**Forwarding ports on the router.** The game's port becomes reachable from the
entire internet, permanently, long after the session is over. Whoever set it up
is the only person who can undo it, and typically does not.

**Joining a general-purpose virtual LAN.** These place the machines in one flat
network and stop there. Once inside, every open port on the machine is reachable
by everyone else in it. On Windows that is not hypothetical: file sharing,
remote desktop, remote management and device discovery all listen by default,
and a remote-desktop tool the user installed for legitimate reasons listens on
whatever port they chose.

The people in a game room are friends of friends holding a disposable invite
code. Kanpachi's position is that they should be able to reach the game and
nothing else.

## What it consists of

### 1. The adapter is born closed

A room's virtual adapter starts with **no allow rules at all**. Windows blocks
unsolicited inbound traffic by default, so the absence of a rule already is the
deny-all. Ports are added only when a game profile asks for them, only on the
machine hosting, and only toward the addresses of the members currently in the
room. Leaving the room takes them away; removing somebody from the room
takes away theirs.

### 2. A quarantine that outlives the daemon

Some ports are never opened by any profile and cannot be expressed in one:
`22, 135, 137, 138, 139, 445, 3389, 3702, 5357, 5358, 5985, 5986`. File sharing,
remote desktop, remote management, device discovery.

Absence of a rule is not enough for these, because a game installer or another
program may have left a permissive rule behind. Kanpachi can block them
**explicitly**, in both directions, **on every network interface of the whole
machine**, not just the virtual adapter, in a separate rule group. Closing
ports that wide is a real trade (it is this machine's own file sharing and
Remote Desktop that stop answering, everywhere), so it is the **user's
decision**: Kanpachi asks once, recommends yes with the reasons spelled out,
and the answer is a switch that works in both directions. Once the user says
yes, every start repairs it, and **no automatic path can remove it**: not a
sweep, not a restart, not a reset. That is the protection: the quarantine
stays in place while Kanpachi is stopped, crashed, or half uninstalled, until
the person who put it there says otherwise, or uninstalls.

### 3. A gate that makes the allow list complete

Windows Firewall rules are **additive**. Kanpachi's rules can open a port; they
cannot close one that another program's rule opened. That is the gap a permissive
remote-desktop rule walks straight through, over the virtual network, to a user
who never knew the rule existed.

There is a second layer for that: a packet filter of Kanpachi's own, scoped to
the virtual adapter, that blocks everything and permits back only what the
first layer opened. A block there is hard and beats any allow rule; a permit
there is soft and **cannot** override a block the user set. That asymmetry is
deliberate: it closes the hole without taking away the user's veto over their
own machine.

It is scoped to the virtual adapter, always. A filter without that scope would
apply to every interface on the machine, and being a hard block it would cut the
user off from their own home network.

### 4. An engine that cannot be told what to do

Kanpachi's peer-to-peer engine is a
[binary of its own](https://github.com/alvarogabrielgomez/kanpachi-engine). The
widely used alternative opens an administration portal with **no authentication
of any kind**, through which any local process can issue credentials for the
network, add peers, forward ports, and ask for the network secret in cleartext.

The engine takes commands on stdin and nothing else. Not a port, not a named
pipe, not a watched file, not a signal, and it accepts no command-line arguments.
The pipes it uses are anonymous: they have no name, no path and no address, so
connecting to them is not forbidden, it is an operation that does not exist.

The build leaves capabilities the product forbids **out of the binary** where
possible, as absent compile-time features rather than flags that are turned off.

### 5. A library that does not open what the product closes

The engine is built on
[Kanpachi's fork](https://github.com/alvarogabrielgomez/EasyTier) of EasyTier.
Upstream writes permanent Windows Firewall allow rules while creating the virtual
adapter: one set opens the adapter to all traffic, and another grants the engine
inbound "any protocol" on **every** interface of the machine. Neither can be
disabled by configuration, and both outlive a reboot and an uninstall.

The fork is upstream with those two calls removed and nothing else, so that
`git diff v2.6.4 v2.6.4-kanpachi.1` reads in one glance. That is also why the
engine lives in its own repository rather than inside the fork: a claim of "this
is upstream and nothing more" is worth only as much as the effort it takes to
check it.

### 6. It checks itself, from another machine

Every check above measures what this machine has **configured**. A configuration
can be impeccable and still not contain anything, and that failure is invisible
from the inside by definition.

Kanpachi therefore asks a member of the room to knock on a port of the host,
and the host watches what arrives. On Windows a blocked port and a port with
nobody behind it look identical, so Kanpachi puts a listener behind the door
**on purpose**, for a few seconds, on a random port. Knowing for certain that
somebody is listening is what gives the silence a single meaning.

When it works, Kanpachi shows nothing. The first leak raises no alarm: the
daemon repairs the protection itself and lets the next round judge. It reports
only a second leak in a row, which changes what the message means, from
*"something happened, press this"* to **"something happened, I tried to fix it,
and it did not hold"**.

## Status

Claims here are measured, not assumed. This table is what has been measured so
far and what has not.

| | Status |
|---|---|
| Quarantine survives with the daemon stopped | **Measured.** Its rules are present with no daemon running |
| Engine opens no listening socket, with the adapter up | **Measured.** Zero, against a real seed. An earlier attempt at this measurement was wrong because it ran without a virtual adapter and missed a socket |
| The network secret never reaches the command line | **Measured.** The engine's command line is the path of the executable and nothing else |
| Killing the daemon without cleanup takes the engine and its network down | **Measured.** Job Object with kill-on-close |
| The library writes no firewall rules | **Measured** as a transition: rule group empty, engine started, adapter up, group still empty |
| The gate contains what it should, seen from another machine | **Measured** in isolation, with two machines |
| The gate is switched on during a real room | **NOT YET.** It is written and tested and the daemon does not yet turn it on. Until that is wired, a room is protected by the first layer alone |

## What it does not protect against

Stating this is part of the promise being worth anything.

- **A program already running as SYSTEM on the machine.** It can stop the daemon
  and rewrite the firewall directly. Nothing local survives that, and it is not
  the attacker this design is built for. The attacker it is built for is the
  unprivileged program, which against a portal with no authentication needs only
  to connect to it.
- **Anything the active game profile opens.** If a game asks for a port, that
  port is open toward the members of the room while the game is selected. What
  Kanpachi guarantees is that nothing *else* is.
- **What the members do inside the game.** Kanpachi is a network, not a referee.
- **A user who turns their own firewall off.** The exposure screen says so
  rather than pretending otherwise.

## Where each piece lives

| Repository | What it holds |
|---|---|
| [kanpachi](https://github.com/alvarogabrielgomez/kanpachi) | The daemon, which decides and writes; the UI; the seed. The design documents in `docs/` |
| [kanpachi-engine](https://github.com/alvarogabrielgomez/kanpachi-engine) | The engine binary: builds the encrypted network, listens on nothing |
| [EasyTier](https://github.com/alvarogabrielgomez/EasyTier) | The forked library, upstream minus what opened the adapter |

The division is the point: the daemon decides what may be reached and writes it
to the system, the engine moves packets and decides nothing. A failure of the
engine cannot open the machine, and a failure of the daemon cannot break anybody
else's game.
