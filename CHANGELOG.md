# Changelog

What changed in each release of Kanpachi, for the person using it.

The format is [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and versions follow [SemVer](https://semver.org/). How it is kept, and why it is kept that way, is in [docs/CLAUDE.md](docs/CLAUDE.md): **one line per entry, imperative mood, linking its own commit**, written in the same commit as the change.

This file is in English, like commit messages and release notes, because a release body quotes it verbatim. The design reasoning lives in `docs/02-decisiones-de-diseno.md`, in Spanish, and the mechanical detail lives in the commit each entry links.

## Unreleased

Nothing yet.

## [0.2.0] - 2026-08-13

### Added

- Choose which server your rooms are opened on, in a screen of its own. Entering somebody else's room never chooses it for you: it offers that server as a suggestion, marked as one, so nobody has to go dig the name out of a chat ([1384e75](https://github.com/alvarogabrielgomez/kanpachi/commit/1384e75))
- Confirm the server before opening or entering a room, with what a bad one can do spelled out on the same screen: it can log the public IP of everyone who introduces themselves to it, and it can try to capture what passes between them ([1384e75](https://github.com/alvarogabrielgomez/kanpachi/commit/1384e75))
- Ask for a password when a server requires one to host on it. **Entering a room never asks for anything, on any server** ([1384e75](https://github.com/alvarogabrielgomez/kanpachi/commit/1384e75))
- Close your own seed with `kanpseed password`, so only people with the password can host on it. Changing the password throws every host out at once, and they get back in by typing the new one ([1384e75](https://github.com/alvarogabrielgomez/kanpachi/commit/1384e75))
- Set the password of a server from the terminal client with `kanpachi password`, and the server itself with `kanpachi seed <host>`. The wizard asks for both in place when opening a room needs them ([1384e75](https://github.com/alvarogabrielgomez/kanpachi/commit/1384e75))
- Keep a guest in the room past the first day: the host now renews the credentials of everyone present, which used to expire twenty-four hours after each person joined and drop them one by one ([a885036](https://github.com/alvarogabrielgomez/kanpachi/commit/a885036), with [f1eeca3](https://github.com/alvarogabrielgomez/kanpachi-engine/commit/f1eeca3) in `kanpachi-engine`)
- Bring guests back by themselves when the host restarts: it no longer recognises anyone, so it says so and each guest asks for a new credential, which took three minutes of waiting before and now takes about twenty seconds ([a885036](https://github.com/alvarogabrielgomez/kanpachi/commit/a885036))
- Say on screen that the room is being re-entered, instead of showing a room that looks frozen ([f31c0a0](https://github.com/alvarogabrielgomez/kanpachi/commit/f31c0a0))
- Say "Saliendo de la sala" in the tray menu while it happens, and refuse a second click that would arrive with the room half closed ([f31c0a0](https://github.com/alvarogabrielgomez/kanpachi/commit/f31c0a0))
- Talk during the long waits, with phrases that move with the real steps, so opening a room stops looking like a hang ([f31c0a0](https://github.com/alvarogabrielgomez/kanpachi/commit/f31c0a0))
- Warn on the home screen when the meeting server is not answering, so a dead registry is known before a game is picked and a code is typed, and not after ([c6b44b0](https://github.com/alvarogabrielgomez/kanpachi/commit/c6b44b0))

### Changed

- Never show an invite code bare. Copying gives `A7K2-M9QX@server` and sharing gives the whole link, because the same eight characters on two servers are two different rooms that know nothing about each other ([1384e75](https://github.com/alvarogabrielgomez/kanpachi/commit/1384e75))
- Look for a new version only when asked, from Settings, instead of on every start and on every room that opens or closes ([1384e75](https://github.com/alvarogabrielgomez/kanpachi/commit/1384e75))
- Print the whole `kanpseed` command line in English, which was the only part of the seed still speaking Spanish to people who read the README over `ssh` ([1384e75](https://github.com/alvarogabrielgomez/kanpachi/commit/1384e75))
- Answer a failure under `--json` with a code and nothing else, on standard output, so a script parses one JSON document whether the command worked or not ([1384e75](https://github.com/alvarogabrielgomez/kanpachi/commit/1384e75))
- Check that this machine can build a virtual adapter when Kanpachi starts, rather than when the first room is opened, and say which of the two things is wrong when it cannot: the driver files being absent, or Windows refusing to install the driver. It used to be found out after choosing a game and typing an invite code, took thirty seconds, and came back as an address problem ([d6d9e85](https://github.com/alvarogabrielgomez/kanpachi/commit/d6d9e85))
- Close Kanpachi, after saying why and waiting for the message to be read, when this machine cannot build a virtual adapter at all: it is not a room that failed and trying again cannot fix it ([d6d9e85](https://github.com/alvarogabrielgomez/kanpachi/commit/d6d9e85))
- Answer `the virtual adapter` in `kanpachi doctor` on Windows, which had nothing equivalent to the `/dev/net/tun` check it does on Linux ([d6d9e85](https://github.com/alvarogabrielgomez/kanpachi/commit/d6d9e85))
- Say what each Kanpachi process is in Task Manager, which listed bare executable names with a blank icon ([d8e3902](https://github.com/alvarogabrielgomez/kanpachi/commit/d8e3902))
- Describe what every Kanpachi executable does in its file properties, and say it in the service list too ([3f1c599](https://github.com/alvarogabrielgomez/kanpachi/commit/3f1c599))
- Give the service and the tunnel engine an icon of their own, so the four Kanpachi processes are told apart at a glance instead of by reading ([550911d](https://github.com/alvarogabrielgomez/kanpachi/commit/550911d))
- Blink the tray icon while a room is up, so a running room is visible without opening the window ([32bfc9f](https://github.com/alvarogabrielgomez/kanpachi/commit/32bfc9f))
- Hide the scrollbars ([f31c0a0](https://github.com/alvarogabrielgomez/kanpachi/commit/f31c0a0))
- Refuse to open a room in the first second when the meeting server does not answer, instead of handing out a code that looks fine and that nobody can use to get in ([c6b44b0](https://github.com/alvarogabrielgomez/kanpachi/commit/c6b44b0))
- Refuse to enter a room in the first second when the meeting server does not answer, instead of a minute of spinning against a lobby that cannot form, and say it in words that differ from a code that does not exist, because one is worth retrying and the other is not ([c6b44b0](https://github.com/alvarogabrielgomez/kanpachi/commit/c6b44b0))

### Fixed

- Paste a link, or a code with a server in it, and have it reach the right room. The home field used to throw away everything after the code, so `A7K2-M9QX@a-friends-server.com` quietly became a code on the default server, which is a different room with the same eight characters. Four of the six documented forms turned into gibberish that still looked like a valid code ([1384e75](https://github.com/alvarogabrielgomez/kanpachi/commit/1384e75))
- Answer a request for an API path that does not exist with a failure code, instead of serving the invitation page with a 200. A client asking for an endpoint that was removed got HTML where it expected JSON, and a "everything is fine" where it expected a failure
- Say that a code is missing its server when that is what happened, instead of "something failed inside Kanpachi, restart the app and report it". Nothing failed: the eight characters were pasted without the part that says which server the room lives on
- Show which server this machine opens rooms on when asked with no arguments, which is the form the help documents and which answered "missing parameters"
- Say "you have not chosen a server yet" when that is what happened, instead of "something failed inside Kanpachi, restart the app and report it". Nothing had failed, and restarting fixed nothing ([1384e75](https://github.com/alvarogabrielgomez/kanpachi/commit/1384e75))
- Let people whose internet provider uses the same address range as Kanpachi's lobby into a room: entering hung with no explanation, and each room now takes its lobby from its own invite code, so renewing the code moves it out of the way ([a885036](https://github.com/alvarogabrielgomez/kanpachi/commit/a885036))
- Say when this machine's own network clashes with the lobby of the room being entered, which used to be a silent thirty-second wait ([a885036](https://github.com/alvarogabrielgomez/kanpachi/commit/a885036))
- Say that the virtual adapter was never created when that is what happened, instead of reporting it as an address that could not be taken: the two are different problems and only one of them is about addresses ([037ba55](https://github.com/alvarogabrielgomez/kanpachi/commit/037ba55))
- Wait for the message to be read before closing when the window cannot be kept open, which appeared at the very instant everything shut down and read as a crash, and say four attempts instead of three, which is the number Kanpachi actually allows ([d6d9e85](https://github.com/alvarogabrielgomez/kanpachi/commit/d6d9e85))
- Finish shutting down before quitting: leaving Kanpachi could exit while the room was still being closed, so the firewall rules, the engine process and the API token were left behind until the next start cleaned them up ([14e5a2a](https://github.com/alvarogabrielgomez/kanpachi/commit/14e5a2a))
- Stop Windows from asking for administrator on its own for the tunnel engine, which needed no privileges and was being elevated on a guess ([3f1c599](https://github.com/alvarogabrielgomez/kanpachi/commit/3f1c599))
- Stop the window from crashing every twenty to sixty minutes: it corrupted its own memory reading the daemon's pipe, and the pipe now lives in native code that owns its buffers ([e8000ca](https://github.com/alvarogabrielgomez/kanpachi/commit/e8000ca))
- Reconnect to the daemon after it drops an idle link, instead of leaving the window frozen on what it last knew ([40a260d](https://github.com/alvarogabrielgomez/kanpachi/commit/40a260d))
- Name the engine log `kanpachi-engine.log`, which shipped with no extension at all and left Windows asking what to open it with ([e6a5ca7](https://github.com/alvarogabrielgomez/kanpachi-engine/commit/e6a5ca7), in `kanpachi-engine`)
- Keep more of the engine log before it wraps, which at two megabytes rolled over mid-session ([e6a5ca7](https://github.com/alvarogabrielgomez/kanpachi-engine/commit/e6a5ca7), in `kanpachi-engine`)
- Keep open rooms reachable when the meeting server restarts, which used to empty its list of rooms and tell every guest of every open room that the room did not exist, with no way for the host to put it back but to renew the code and kill the links already handed out ([c6b44b0](https://github.com/alvarogabrielgomez/kanpachi/commit/c6b44b0))
- Say why the virtual adapter failed while entering a room, which was the one moment the reason was thrown away, and stop waiting thirty seconds for an address that is never coming ([c6b44b0](https://github.com/alvarogabrielgomez/kanpachi/commit/c6b44b0), with [9f6dd6b](https://github.com/alvarogabrielgomez/kanpachi-engine/commit/9f6dd6b) in `kanpachi-engine`)
- Let a guest actually play. The host counted only the members its tunnel engine reported, and with a guest arriving through the seed it counted none of them: measured with two machines, the guest saw two members in the room and the host saw one. Game ports are opened towards the members present, so a guest already inside, with its control channel open and its firewall rule written, never got a single game port. The host now also counts whoever it admitted and has an open channel with ([5dcf44b](https://github.com/alvarogabrielgomez/kanpachi/commit/5dcf44b))

### Security

- Keep the room secret and the private key out of `kanpachi-engine.log`, which the engine wrote in clear and which is the file people are asked to send over chat ([9f6dd6b](https://github.com/alvarogabrielgomez/kanpachi-engine/commit/9f6dd6b), in `kanpachi-engine`)
- Keep them out of a crash too, which bypassed the log entirely and printed straight to standard error ([9f6dd6b](https://github.com/alvarogabrielgomez/kanpachi-engine/commit/9f6dd6b), in `kanpachi-engine`)

## [0.1.9] - 2026-08-09

### Added

- Ship Kanpachi portable as a single `kanpachi-portable.exe` that needs no install: one UAC prompt, no service, no autostart entry, and nothing left behind but its log ([814ca1f](https://github.com/alvarogabrielgomez/kanpachi/commit/814ca1f))
- Keep the panic traceback of a crashing daemon, which used to be discarded because a service has no standard error ([b56c553](https://github.com/alvarogabrielgomez/kanpachi/commit/b56c553))
- Write what the engine says to `kanpachi-engine.log`, beside the daemon's own log, where it used to be thrown away ([8d4e137](https://github.com/alvarogabrielgomez/kanpachi-engine/commit/8d4e137), in `kanpachi-engine`)
- Write what the window says to `kanpachi-ui.log`, so a window that closes by itself leaves a reason behind ([b56c553](https://github.com/alvarogabrielgomez/kanpachi/commit/b56c553))
- Offer the portable version from the download page, under the installer button ([c7551cd](https://github.com/alvarogabrielgomez/kanpachi/commit/c7551cd))
- Say what is happening while a room is being left or closed, step by step, instead of leaving the room screen still ([27da918](https://github.com/alvarogabrielgomez/kanpachi/commit/27da918))

### Changed

- Name the daemon log `kanpachi.log`, and let `--log` put it in a chosen folder ([b56c553](https://github.com/alvarogabrielgomez/kanpachi/commit/b56c553))
- Hide the "start with Windows" setting in a portable copy, where no service exists to start ([814ca1f](https://github.com/alvarogabrielgomez/kanpachi/commit/814ca1f))
- Start a portable copy with the step-by-step narration already on ([814ca1f](https://github.com/alvarogabrielgomez/kanpachi/commit/814ca1f))
- Spin the "Renovar código" button while the registry answers, and refuse a second press that would kill the code the first one just handed out ([27da918](https://github.com/alvarogabrielgomez/kanpachi/commit/27da918))

### Fixed

- Shut the daemon down when the window asks, instead of leaving it running for ten more minutes ([b56c553](https://github.com/alvarogabrielgomez/kanpachi/commit/b56c553))
- Let a guest who left a room join it again, which used to fail on every attempt after the first ([81bd22c](https://github.com/alvarogabrielgomez/kanpachi/commit/81bd22c), with [8d4e137](https://github.com/alvarogabrielgomez/kanpachi-engine/commit/8d4e137) in `kanpachi-engine`)
- Open a new room right after closing one, instead of leaving the previous one fighting for the adapter ([8d4e137](https://github.com/alvarogabrielgomez/kanpachi-engine/commit/8d4e137), in `kanpachi-engine`)
- Close a portable Kanpachi whole when you quit it, instead of leaving its window running with nothing behind it ([b56c553](https://github.com/alvarogabrielgomez/kanpachi/commit/b56c553))

## [0.1.8] - 2026-08-08

### Fixed

- Connect guests through relays by enabling secure mode on the host and lobby ([c6dfadc](https://github.com/alvarogabrielgomez/kanpachi-engine/commit/c6dfadc), in `kanpachi-engine`)
- Retry the initial room control connection while the relay establishes its encrypted session ([638b0f3](https://github.com/alvarogabrielgomez/kanpachi/commit/638b0f3))

## [0.1.7] - 2026-08-07

### Added

- Say in the status bar when a newer version is out, and open the download page when it is clicked
- Warn on hover that installing it closes the room, which is what stopping the service does

### Changed

- Open the window at 888 × 565 the first time instead of 1000 × 555
- Log what changed instead of what ran: who entered or left the room by name, and the firewall rules only when the set actually changed
- Stop logging the router mapping query that no router answers, the network category that is never set, and every connection the window opens

### Fixed

- Let guests in: the host now opens the control channel to the address it just handed out, instead of to an address the engine never knew

## [0.1.6] - 2026-08-07

### Added

- Ask before replacing files when Kanpachi is running, saying that the open room will close for everyone ([d87bcdd](https://github.com/alvarogabrielgomez/kanpachi/commit/d87bcdd))

### Changed

- Name the version on the download button, asked once an hour by the seed instead of by every visitor's browser ([777a3e7](https://github.com/alvarogabrielgomez/kanpachi/commit/777a3e7))
- Serve the download page at `/download`, with `/descargar` still answering for the links already handed out ([d87bcdd](https://github.com/alvarogabrielgomez/kanpachi/commit/d87bcdd))

### Fixed

- Pre-authorize the host's control channel port for unexpired credentials so guests don't time out while the network engine forms the P2P mesh
- Land unit changes on `kanpseed upgrade` instead of needing a second command, by handing the second half to the binary just installed ([76fc320](https://github.com/alvarogabrielgomez/kanpachi/commit/76fc320))

## [0.1.4] - 2026-08-07

### Changed

- Draw the drifting shapes and the wordmark with a semitransparent colour instead of an `Opacity` layer, five offscreen buffers fewer per frame ([e380ae4](https://github.com/alvarogabrielgomez/kanpachi/commit/e380ae4))
- Make the whole name and avatar area of the title bar open the account menu, instead of only the pixels it paints ([025c936](https://github.com/alvarogabrielgomez/kanpachi/commit/025c936))
- Keep the window drag area off the title bar controls, so a click that moves a pixel opens the menu instead of dragging the window ([025c936](https://github.com/alvarogabrielgomez/kanpachi/commit/025c936))
- Audit foreign firewall rules on four triggers instead of every two seconds: entering the room, changing game, somebody joining, and every two minutes ([3d31a5c](https://github.com/alvarogabrielgomez/kanpachi/commit/3d31a5c))

### Fixed

- Stop dropping the service now and then: the heartbeat no longer overlaps itself nor queues behind the firewall sweep ([3d31a5c](https://github.com/alvarogabrielgomez/kanpachi/commit/3d31a5c))
- Stop rebuilding the title bar and its window buttons thirty times a minute for pixels that did not change ([025c936](https://github.com/alvarogabrielgomez/kanpachi/commit/025c936))
- Go back to the room, not to the home screen, when closing an invite link with a room open ([46bd095](https://github.com/alvarogabrielgomez/kanpachi/commit/46bd095))
- Ask the daemon whether there is a room every time the home screen appears, instead of trusting the last thing known ([46bd095](https://github.com/alvarogabrielgomez/kanpachi/commit/46bd095))

## [0.1.3] - 2026-08-07

### Fixed

- Let guests in at the seed: without secure mode a credential was refused on its first packet, and no room ever held more than one person ([c64a2cb](https://github.com/alvarogabrielgomez/kanpachi/commit/c64a2cb))
- Connect a guest to the host directly, instead of relaying every packet through the server ([c6dfadc](https://github.com/alvarogabrielgomez/kanpachi-engine/commit/c6dfadc), in `kanpachi-engine`)
- Send the back arrow to the previous screen for real, instead of to the home screen ([0be74b9](https://github.com/alvarogabrielgomez/kanpachi/commit/0be74b9))
- Keep the home screen out of reach while a room is open, which made it look like there was none ([0be74b9](https://github.com/alvarogabrielgomez/kanpachi/commit/0be74b9))
- Leave the user in their room when leaving it fails, instead of on a home screen that accepts nothing ([0be74b9](https://github.com/alvarogabrielgomez/kanpachi/commit/0be74b9))

## [0.1.2] - 2026-08-07

### Fixed

- Keep an installed Kanpachi apart from a portable one: each with its own channel, token and window ([8dec62f](https://github.com/alvarogabrielgomez/kanpachi/commit/8dec62f))
- Accept invite links exactly as Windows hands them over, with the trailing slash the browser adds ([6436de1](https://github.com/alvarogabrielgomez/kanpachi/commit/6436de1))
- Remove the interface preferences on uninstall, which Flutter stores outside Program Files ([6436de1](https://github.com/alvarogabrielgomez/kanpachi/commit/6436de1))
- Finish the installer message for a service that will not stop ([6d3e85e](https://github.com/alvarogabrielgomez/kanpachi/commit/6d3e85e), [0e69580](https://github.com/alvarogabrielgomez/kanpachi/commit/0e69580))

## [0.1.1] - 2026-08-06

### Added

- Offer to reopen the room left over from the previous start, instead of losing it ([7af511e](https://github.com/alvarogabrielgomez/kanpachi/commit/7af511e))

### Fixed

- Keep every registered room when the seed page is reloaded ([3c67f5b](https://github.com/alvarogabrielgomez/kanpachi/commit/3c67f5b))
- Publish the installer also when the release is created from the GitHub web ([981bead](https://github.com/alvarogabrielgomez/kanpachi/commit/981bead))

## [0.1.0] - 2026-08-06

First published version.

### Added

- Create a room, hand out its code, and open only the ports of the chosen game ([c81c0bf](https://github.com/alvarogabrielgomez/kanpachi/commit/c81c0bf))
- Join a room by pasting the code, or by opening a `kanpachi://` link from the browser ([7a8539e](https://github.com/alvarogabrielgomez/kanpachi/commit/7a8539e))
- Show what your PC has open, measured on the system instead of read back from what Kanpachi believes ([7a47467](https://github.com/alvarogabrielgomez/kanpachi/commit/7a47467))
- Cancel the wait for a room, undoing whatever it got as far as ([8dbd9e4](https://github.com/alvarogabrielgomez/kanpachi/commit/8dbd9e4))
- Ship a portable folder that copies and runs without installing anything ([250b3d5](https://github.com/alvarogabrielgomez/kanpachi/commit/250b3d5))
- Remember your name and the window size ([01fb7e5](https://github.com/alvarogabrielgomez/kanpachi/commit/01fb7e5), [68a543a](https://github.com/alvarogabrielgomez/kanpachi/commit/68a543a))
- Publish the installer from a single tag ([e4fd252](https://github.com/alvarogabrielgomez/kanpachi/commit/e4fd252))

[0.2.0]: https://github.com/alvarogabrielgomez/kanpachi/releases/tag/v0.2.0
[0.1.9]: https://github.com/alvarogabrielgomez/kanpachi/releases/tag/v0.1.9
[0.1.8]: https://github.com/alvarogabrielgomez/kanpachi/releases/tag/v0.1.8
[0.1.4]: https://github.com/alvarogabrielgomez/kanpachi/releases/tag/v0.1.4
[0.1.3]: https://github.com/alvarogabrielgomez/kanpachi/releases/tag/v0.1.3
[0.1.2]: https://github.com/alvarogabrielgomez/kanpachi/releases/tag/v0.1.2
[0.1.1]: https://github.com/alvarogabrielgomez/kanpachi/releases/tag/v0.1.1
[0.1.0]: https://github.com/alvarogabrielgomez/kanpachi/releases/tag/v0.1.0
