# Your first room

By the end of this you will have a room open, a friend inside it, and a game
reachable between the two machines with nothing else exposed. It takes about ten
minutes, and most of that is the two downloads.

This tutorial makes every choice for you. Where there is an option worth
knowing about, it is named at the end and not in the middle.

**What you need:** a Windows PC, a friend with another machine, and a way to
send them a line of text. No account, no router configuration, no port
forwarding, nothing to pay.

---

## 1. Install Kanpachi

Download `kanpachi-setup.exe` from the
[releases page](https://github.com/alvarogabrielgomez/kanpachi/releases/latest)
and run it.

Windows will ask for administrator once. Accept it. That single prompt covers
the whole install, and playing never asks again.

When it finishes, Kanpachi opens.

## 2. Say what your friends call you

The first screen asks for a name, up to twelve letters and numbers.

It is not an account. It is not verified, it is not sent to any server, and it
exists so the other people in the room see something readable instead of your
Windows computer name — which, on a lot of machines, is somebody's real name.

Type it and continue. This screen appears once in the life of the install.

## 3. Open the room

On the home screen, press **Create room** and give it a name. That is all it
asks.

A dialog appears before the room is created, showing the address of the seed
this machine opens rooms on. A seed is a meeting point: it hands out the invite
code and introduces the two machines to each other. Read the short warning about
what a malicious one could do, and press **Trust and create**.

Creating the room takes close to a minute. It is generating this machine's
network identity, building the encrypted network, and publishing a sealed card
to the registry.

**The room is now open, and no port is open on it.** That is a valid state and
it is the one every room starts in.

## 4. Hand out the code

The room screen shows an invite code that looks like this:

```
A7K2-M9QX@kanpachi.accentio.dev
```

Copy it and send it to your friend, however you normally talk. Chat is fine.

The code is not a secret and not a key. It is a lookup ticket: it says which
registry to ask and which room to ask about. What actually admits somebody is a
credential your machine issues once they knock, and the code has no way to
carry it.

The half after the `@` is not decoration. The same eight characters on a
different seed are a different room, so a code without its seed opens nothing.

## 5. Your friend joins

They install Kanpachi the same way, pick their name, paste the whole code into
the field on the home screen and press **Join**.

The field accepts it however it arrives: with or without dashes, upper or lower
case, or as the full link. They get the same trust dialog, showing the seed
address that came *inside the code you sent* — which is the thing to compare,
character by character, against what you sent. They press **Trust and enter**.

They appear in your member list.

## 6. Choose the game

Back on your machine, pick the game from the room screen. Your installed games
are listed first; the full catalogue is one click below, with a search box.

The moment you activate a profile, **the ports that game asks for open on the
virtual adapter, on your machine only, and only toward the addresses of the
people currently in the room**. Nothing else opens. The rules are recalculated
from scratch every time somebody joins or leaves.

Start the game, host the session in it as you always would, and have your
friend connect to your Kanpachi address, shown next to your name in the member
list.

## 7. Finish

Press **Close room** when you are done. The ports close with it.

If you only close the window, the room stays open and the service keeps holding
it — the window is a remote control, not the program. That is deliberate, and
it is what lets a room survive a reboot on a server.

---

## What you just built

Your two machines are on one private network with an encrypted peer-to-peer
tunnel between them. Between your two routers, directly, whenever the routers
allow it. The seed introduced you and then left the path.

Everything on your machine that the game did not ask for stayed closed on that
adapter. File sharing, remote desktop, remote management: closed, explicitly,
by a rule group that stays in place even while Kanpachi is stopped.

## Where to go from here

- **Somebody has to leave and not come back.** Kicking removes them now;
  renewing the code stops the invitation you already handed out from working
  again. They are two independent controls, and neither interrupts the game.
  See [every command](reference-cli.md).
- **The game is on a server nobody sits at.** Linux, no window, reopens its own
  room at boot with the same code: [run a 24/7 game server](run-a-game-server.md).
- **You would rather not use our meeting point.** Run your own in one line:
  [host your own seed](host-a-seed.md).
- **You want to know what the seed can see.** It is written down, including
  what a hostile one could do differently: [the seed](../../kanpachi-seed.md).
- **You want to know exactly what is closed and what is not.**
  [Kanpachi Protection](../../kanpachi-protection.md) states the promise and its
  limits.
