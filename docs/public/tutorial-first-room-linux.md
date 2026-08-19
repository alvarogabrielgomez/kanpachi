# Your first room, on Linux

By the end of this you will have a room open, a friend inside it, and a game
reachable between the two machines with nothing else exposed. It takes about ten
minutes, and most of that is the download and the minute the room takes to come
up.

This tutorial makes every choice for you. Where an option is worth knowing
about, it appears at the end and not in the middle.

**What you need:** Ubuntu 22.04 or newer on amd64, `sudo`, a friend with another
machine, and a way to send them a line of text. No account, no router
configuration, no port forwarding, nothing to pay.

There is no window here. The client is a command, and every step below is one
line you paste.

**The steps:**

1. [Install Kanpachi](#1-install-kanpachi)
2. [Say what your friends call you](#2-say-what-your-friends-call-you)
3. [Choose the registry your rooms open on](#3-choose-the-registry-your-rooms-open-on)
4. [Open the room](#4-open-the-room)
5. [Hand out the code](#5-hand-out-the-code)
6. [Your friend joins](#6-your-friend-joins)
7. [Choose the game](#7-choose-the-game)
8. [Finish](#8-finish)

---

## 1. Install Kanpachi

```sh
curl -fsSL -o /tmp/kanpachi.deb https://github.com/alvarogabrielgomez/kanpachi/releases/latest/download/kanpachi-amd64.deb && sudo apt install -y /tmp/kanpachi.deb
```

That is the whole install. It brings two systemd services up and enables them at
boot.

Every command below needs `sudo`, because the control channel is a root-only
socket. Check that the daemon answers:

```sh
sudo kanpachi status
```

It says no room is open, which is right.

## 2. Say what your friends call you

```sh
sudo kanpachi name alvaro
```

Up to twelve letters and numbers. Nobody verifies it and no server receives it.
It exists so the other people in the room see something readable in place of
this machine's hostname.

Skipping this works too: Kanpachi derives one from the hostname, says so, and
never writes it down. Choosing one now saves you seeing that line every time.

## 3. Choose the registry your rooms open on

```sh
sudo kanpachi seed kanpachi.accentio.dev
```

A registry is a meeting point: it hands out the invite code and introduces the
two machines to each other. Kanpachi ships without one on purpose, because
anybody can run one and the room's registry travels inside every code.

The command checks that the registry answers before saving it. You answer this
once per machine.

## 4. Open the room

```sh
sudo kanpachi host "Zomboid nights"
```

Two questions come first. One names the registry and what a hostile one could
do; the other asks whether to close this machine's risky server ports on every
network, which is Kanpachi Protection and which stays in place even while
Kanpachi is stopped. Answer both.

Then it takes close to a minute, and it says so before starting. This machine is
generating its network identity, building the encrypted network, and publishing
a sealed card to the registry.

**The room is now open, and no port is open on it.** Every room starts there.

## 5. Hand out the code

```sh
sudo kanpachi link
```

It prints the invite and nothing else, so it drops straight into a `$(...)`:

```
https://kanpachi.accentio.dev/A7K2-M9QX#...
```

Send it to your friend, however you talk to them. Chat is fine. `sudo kanpachi
status` shows the shorter dictating form, `A7K2-M9QX@kanpachi.accentio.dev`, for
reading out loud.

The code is a lookup ticket. It says which registry to ask and which room to ask
about, and it carries nothing that admits anybody: your machine issues a
credential once they knock.

The half after the `@` carries the registry. The same eight characters on a
different one are a different room, so a code without it opens nothing.

## 6. Your friend joins

On their machine, whichever it runs. On Linux:

```sh
sudo kanpachi join A7K2-M9QX@kanpachi.accentio.dev
```

On Windows they paste the same string into the field on the home screen. Either
way they get a dialog showing the registry that came *inside the code you sent*,
which is the thing to compare against what you sent.

Watch them arrive:

```sh
sudo kanpachi members
```

Joining sets no registry on their machine: theirs stays whatever it was.

## 7. Choose the game

```sh
sudo kanpachi games
sudo kanpachi game project-zomboid
```

The first lists the catalogue and marks what this machine has installed. The
second activates one.

The moment you activate a profile, **the ports that game asks for open on the
virtual adapter, on this machine only, and only toward the addresses of the
people currently in the room**. Nothing else opens. Kanpachi recalculates the
rules from scratch every time somebody joins or leaves. See for yourself:

```sh
sudo kanpachi exposure
```

Start the game server as you always would, and have your friend connect to your
Kanpachi address, which `sudo kanpachi status` prints.

## 8. Finish

```sh
sudo kanpachi leave
```

The ports close with the room.

Stopping the service instead leaves the room yours: the daemon holds it, and it
comes back with the same code after a reboot. Only closing it deletes the room,
which is what a server needs.

---

## What you just built

Your two machines are on one private network with an encrypted peer-to-peer
tunnel between them. Between your two routers, directly, whenever the routers
allow it. The registry introduced you and then left the path.

Everything on this machine that the game did not ask for stayed closed on that
adapter. If you said yes in step 4, file sharing, remote desktop and remote
management are closed across every network too, by an nftables table a systemd
unit loads before the network comes up.

## Where to go from here

- **You would rather not type.** `sudo kanpachi` with no arguments opens an
  assistant that walks the same path with arrow keys.
- **This machine is the server, and nobody sits at it.** It already reopens its
  room at boot with the same code. The details, and how to keep it that way, are
  in [run a 24/7 game server](run-a-game-server.md), or
  [the same thing with Docker](run-a-game-server-docker.md).
- **Somebody has to leave and not come back.** `kick` removes them now; `rotate`
  stops the invitation you already handed out. Neither interrupts the game. See
  [every command](reference-cli.md).
- **Something did not work.** `sudo kanpachi doctor` names what is missing, and
  `--fix` repairs what belongs to Kanpachi. On a server the usual answer is
  `ufw` swallowing the engine's port.
- **You would rather not use our meeting point.** Run your own in one line:
  [host your own seed](host-a-seed.md).
- **You want to know what is closed and what is not.**
  [Kanpachi Protection](../../kanpachi-protection.md) states the promise and its
  limits.
