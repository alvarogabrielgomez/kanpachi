# Your first room, on Windows

By the end of this you will have a room open, a friend inside it, and a game
reachable between the two machines with nothing else exposed. It takes about ten
minutes, and most of that is the two downloads.

This tutorial makes every choice for you. Where an option is worth knowing
about, it appears at the end and not in the middle.

**What you need:** a Windows PC, a friend with another machine, and a way to
send them a line of text. No account, no router configuration, no port
forwarding, nothing to pay.

**The steps:**

1. [Install Kanpachi](#1-install-kanpachi)
2. [Say what your friends call you](#2-say-what-your-friends-call-you)
3. [Open the room](#3-open-the-room)
4. [Hand out the code](#4-hand-out-the-code)
5. [Your friend joins](#5-your-friend-joins)
6. [Choose the game](#6-choose-the-game)
7. [Finish](#7-finish)

---

## 1. Install Kanpachi

Download `kanpachi-setup.exe` from the
[releases page](https://github.com/alvarogabrielgomez/kanpachi/releases/latest)
and run it.

Windows asks for administrator once. Accept it. That single prompt covers the
whole install, and playing never asks again.

When it finishes, Kanpachi opens.

## 2. Say what your friends call you

The first screen asks for a name, up to twelve letters and numbers.

Nobody verifies it and no server receives it. It exists so the other people in
the room see something readable in place of your Windows computer name, which on
a lot of machines is somebody's real name.

Type it and continue. This screen appears once in the life of the install.

## 3. Open the room

On the home screen, press **Create room** and give it a name.

**The first time, Kanpachi asks which registry to open rooms on.** A registry is
a meeting point: it hands out the invite code and introduces the two machines to
each other. Kanpachi ships without one on purpose, because anybody can run one
and the room's registry travels inside every code. Type:

```
kanpachi.accentio.dev
```

and save. Kanpachi remembers why you were there and carries on creating the
room. You answer this once.

Next comes a dialog naming that registry and what a hostile one could do. Read
it and press **Trust and create**.

Creating the room takes close to a minute. Your machine is generating its
network identity, building the encrypted network, and publishing a sealed card
to the registry.

**The room is now open, and no port is open on it.** Every room starts there.

## 4. Hand out the code

The room screen shows an invite code:

```
A7K2-M9QX@kanpachi.accentio.dev
```

Copy it and send it to your friend, however you talk to them. Chat is fine.

The code is a lookup ticket. It says which registry to ask and which room to ask
about, and it carries nothing that admits anybody: your machine issues a
credential once they knock.

The half after the `@` carries the registry. The same eight characters on a
different one are a different room, so a code without it opens nothing.

## 5. Your friend joins

They install Kanpachi the same way, pick their name, paste the whole code into
the field on the home screen and press **Join**.

The field takes it however it arrives: with or without dashes, upper or lower
case, or as the full link. They get the same trust dialog, showing the registry
that came *inside the code you sent*. That is the thing to compare, character by
character, against what you sent. They press **Trust and enter**.

They appear in your member list, and joining sets no registry on their machine:
theirs stays whatever it was.

## 6. Choose the game

Back on your machine, pick the game from the room screen. Your installed games
come first, and the full catalogue sits one click below with a search box.

The moment you activate a profile, **the ports that game asks for open on the
virtual adapter, on your machine only, and only toward the addresses of the
people currently in the room**. Nothing else opens. Kanpachi recalculates the
rules from scratch every time somebody joins or leaves.

Start the game, host the session in it as you always would, and have your friend
connect to your Kanpachi address, shown next to your name in the member list.

## 7. Finish

Press **Close room** when you are done. The ports close with it.

Closing the window instead leaves the room open, with the service holding it.
The window drives the daemon and never contains it, which is what lets a room
survive a reboot on a server.

---

## What you just built

Your two machines are on one private network with an encrypted peer-to-peer
tunnel between them. Between your two routers, directly, whenever the routers
allow it. The registry introduced you and then left the path.

Everything on your machine that the game did not ask for stayed closed on that
adapter. File sharing, remote desktop, remote management: closed, explicitly, by
a rule group that stays in place even while Kanpachi is stopped.

## Where to go from here

- **Somebody has to leave and not come back.** Kicking removes them now;
  renewing the code stops the invitation you already handed out from working
  again. They are two independent controls, and neither interrupts the game.
  See [every command](reference-cli.md).
- **The game is on a server nobody sits at.** Linux, no window, reopens its own
  room at boot with the same code: [run a 24/7 game server](run-a-game-server.md),
  or [the same thing with Docker](run-a-game-server-docker.md).
- **You would rather not use our meeting point.** Run your own in one line:
  [host your own seed](host-a-seed.md).
- **You want to know what the registry can see.** It is written down, including
  what a hostile one could do differently: [the seed](../../kanpachi-seed.md).
- **You want to know what is closed and what is not.**
  [Kanpachi Protection](../../kanpachi-protection.md) states the promise and its
  limits.
