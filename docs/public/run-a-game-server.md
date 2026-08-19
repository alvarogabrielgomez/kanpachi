# Run a 24/7 game server

The case a window cannot cover: a Minecraft or Project Zomboid server on a VPS,
where the usual answer is to open a port to the whole internet and leave it
open. This guide sets one up so that the room survives reboots and the invite
code you paste into the group chat in March still works in June.

Start from [install on Linux](install-linux.md). Everything below assumes
`kanpachi` is installed and `kanpachid.service` is running.

## Open the room

```sh
kanpachi host "Zomboid nights"
```

It takes close to a minute: two adapters have to come up, the credential has to
be exchanged, and the MTU has to be measured. When it returns, the room exists
and **nothing is open on it**.

Get the invite and hand it out:

```sh
kanpachi link
```

## Activate the game

```sh
kanpachi games                  # the catalogue, and which ones this machine has installed
kanpachi game project-zomboid   # activate that profile
```

Activating a profile opens the ports it names, **on this machine only, and only
toward the addresses of the members currently in the room**. With nobody in the
room, the desired set is empty and nothing opens, which is correct rather than
broken.

`kanpachi game` with no id closes them again.

Now start the game server the way you normally would, and tell people to connect
to this machine's Kanpachi address, which `kanpachi status` prints.

## What happens on reboot

Nothing that needs a human. When the daemon comes up and finds a room that was
never closed, it brings it back with **the same invite code, the same network
identity, the same subnet and the same game profile**, and republishes the room
card so the invitation page keeps showing the real room instead of a generic
one.

That covers a reboot, a power cut, and an `apt upgrade` that restarts the
service.

Reopening runs in the background, behind the control socket, because it takes
about a minute and a `Type=notify` unit that takes a minute to report looks
hung. During that minute `kanpachi status` says it is connecting.

Three things about the reopened room:

- **It comes back with no ports open.** The daemon restores the game profile and
  recomputes the rules from the live member table rather than reading them off
  the disk. With nobody present there is no address to name, and there is no way
  to write "anybody".
- **The people who were in it are not.** Credentials do not survive the restart.
  The room reopens empty and everyone rejoins with the code they already have.
- **If reopening fails, the daemon stays up without a room** and `kanpachi
  status` says why. Bringing the daemon down would trade a room that did not
  come back for a machine without Kanpachi.

## Shutting down is not closing

They are two different events on purpose. Only somebody closing the room deletes
the file, so reopening can never resurrect a room you meant to end:

```sh
kanpachi leave     # closes your room
```

If a restart left something ambiguous, the three commands for it are:

```sh
kanpachi pending   # was a room left open from the previous start?
kanpachi resume    # reopen it with the same code
kanpachi discard   # forget it
```

## Running it

```sh
kanpachi status      # room, members, network and protection, right now
kanpachi watch       # the same, redrawn until Ctrl+C
kanpachi members     # who is in, by which path, and with what latency
kanpachi exposure    # what is open, and toward whom
```

`members` is where a relayed connection shows up. Direct is the normal case;
somebody behind symmetric NAT falls back to travelling through the seed, still
encrypted end to end, and `members` names that path rather than hiding it.

### When somebody has to go

```sh
kanpachi kick <name|ip>   # out now, without touching the code
kanpachi rotate           # renew the code: the links you handed out stop working
```

They are independent, and this is the pair that does the work Kanpachi has no
ban list for. `kick` removes somebody who is in; `rotate` stops an invitation
that has been passed around from letting anybody else in. Neither interrupts the
people who are already playing.

### Upgrading

```sh
kanpachi upgrade --check
kanpachi upgrade
```

It restarts the service, so the room drops and reopens itself on the way back
up. Plan it like any other restart.

## Two things to check before you blame the network

```sh
kanpachi doctor
kanpachi diag
```

`doctor` names what is missing or broken, and `--fix` repairs what can be
repaired from here. The classic failure on a VPS is `ufw` active and not
allowing the engine's port in, which produces a symptom indistinguishable from
somebody's home router being at fault.

`diag` shows the network as the engine sees it: NAT type, UDP reachability and
MTU.

## Next

- [The same thing with Docker](run-a-game-server-docker.md), if the machine
  already runs its game server that way. One compose file, and the code survives
  the container being rebuilt.
- [Every command](reference-cli.md), including the flags this page skipped.
- [Host your own seed](host-a-seed.md), so the meeting point is yours too.
- [Kanpachi Protection](../../kanpachi-protection.md): what stays closed on that
  machine while all of this is running, and what the promise does not cover.
