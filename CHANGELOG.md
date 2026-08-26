# Changelog

What changed in each release of Kanpachi, for the person using it.

The format is [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and versions follow [SemVer](https://semver.org/). How it is kept, and why it is kept that way, is in [docs/CLAUDE.md](docs/CLAUDE.md): **one line per entry, imperative mood, linking its own commit**, written in the same commit as the change.

This file is in English, like commit messages and release notes, because a release body quotes it verbatim. The design reasoning lives in `docs/02-decisiones-de-diseno.md`, in Spanish, and the mechanical detail lives in the commit each entry links.

## Unreleased

### Fixed

- Let the container start a room again. Two defects sat on top of each other. The startup script asked the daemon to reopen the saved room without saying `--yes`, and reopening crosses the gate that asks before entering a room costs something else, which REFUSES with no terminal to answer in: what was in the way was the room the daemon had already reopened by itself, so the container refused over its own success. Then the code that explains such a refusal died before printing it, because a variable in it was named with an accent and `dash` reads that as a command, not an assignment. Every error path in that script ended there, so none of its explanations had ever been printed. Measured on a real cluster: seven restarts, and the only line the operator got named neither the room nor the reason (PENDING)

### Changed

- Cut the release notes down to what somebody deciding which file to download needs. The body repeated the whole product's story on every publication, so the two lines that change between versions sat under a page and a half that never does ([686b4fe](https://github.com/alvarogabrielgomez/kanpachi/commit/686b4fe))

## [0.7.1] - 2026-08-26

### Fixed

- Wait for the daemon to ANSWER before the container asks it anything. The startup script looked for the socket FILE, and that file lives on a volume that outlives the container, so every restart found yesterday's socket with nobody behind it, ran the first command 40 ms into a boot that had not bound anything yet, and died with "connection refused". The dead file stayed for the next start to trip over: 27 restarts in two hours on a real cluster, with the game server beside it running fine ([51bfd8c](https://github.com/alvarogabrielgomez/kanpachi/commit/51bfd8c))

### Changed

- Say whether you have played with this host's key before in a label beside the seed, instead of a four-line box under it. The box led with a count nobody asked for, "the same key as always, already in 126 rooms", and it printed a twenty-digit fingerprint with nothing to compare it against, which is how somebody learns to skim past a fingerprint. The count is gone from every layer: once you have played with a key, the number changes no decision ([e9322c1](https://github.com/alvarogabrielgomez/kanpachi/commit/e9322c1))
- Put the fingerprints behind a click on that label, in a panel that hangs off it. Two of them when a name you know arrives with a different key, one above the other, which is the only time there is anything to compare ([77c37ad](https://github.com/alvarogabrielgomez/kanpachi/commit/77c37ad))

## [0.7.0] - 2026-08-26

### Fixed

- Stop the daemon eating tens of megabytes on every room it opens and closes. Each read of the Windows firewall store left its enumerator behind, and with it a snapshot of every rule on the machine: six cycles of opening a room and switching games took the process from 80 MB to 426 MB, where it stayed, because that memory belongs to COM and nothing in Go can reclaim it. A machine at rest leaked it too, twice a minute, for as long as the daemon ran ([af5584b](https://github.com/alvarogabrielgomez/kanpachi/commit/af5584b))
- Stop the window's memory creeping up while a room is open. Every rebuild of a beating status dot subscribed another listener to its own animation and not one of them ever went away, at thirty a minute per dot for as long as the room stayed up ([af5584b](https://github.com/alvarogabrielgomez/kanpachi/commit/af5584b))
- Decode a game's cover at the size it is drawn instead of at Steam's. The 600×900 portrait was held in memory whole to be painted 34 pixels wide, three hundred times the pixels it needs, and the image cache fills to 100 MB before it evicts anything ([af5584b](https://github.com/alvarogabrielgomez/kanpachi/commit/af5584b))
- Stop going back to a room when you press "Salir de la sala" on the notice that says you are. Without a room open the window asked the daemon for nothing and only cleared its own copy, so the next beat brought the return back with its clock running and its attempt count intact: sixteen attempts in, the one button on that notice did nothing at all ([fb8abac](https://github.com/alvarogabrielgomez/kanpachi/commit/fb8abac))
- Ask before opening a room of your own while a return is pending, the way pasting somebody else's code already did. Both paths that open a room went straight to the trust dialog, so the daemon refused with "ya estás en una sala" over a return nobody had been offered the chance to drop, and the window had no way out of it ([fb8abac](https://github.com/alvarogabrielgomez/kanpachi/commit/fb8abac))
- Show what a return attempt is doing, and what stopped the last one, with the narration on. The daemon writes the same step diary it writes for joining, and `kanpachi status` has printed the reason of the last failure all along; the window read both off the wire and drew neither ([fb8abac](https://github.com/alvarogabrielgomez/kanpachi/commit/fb8abac))
- Let a host accept a `kanpachi://` link without leaving the room first. The link screen is shown over an open room deliberately, because somebody playing being handed a code is the ordinary case, and "Confiar y entrar" went straight to the daemon without asking what it would displace: the daemon refused, the notice said you were already in a room, and there was nowhere on that screen to say yes. That host could never accept a link ([fb8abac](https://github.com/alvarogabrielgomez/kanpachi/commit/fb8abac))
- Ask before opening or entering a room from the four remaining doors that never did: creating one from the game picker, saving a registry, handing over its password, and reopening the room this machine hosts. The rule was written into two of the six and the daemon refused the rest with "you are already in a room", over something nobody had been offered the chance to drop ([fb8abac](https://github.com/alvarogabrielgomez/kanpachi/commit/fb8abac))
- Read what entering a room would displace off the wire instead of guessing it. The daemon has always answered it in `status` and in a code's preview, and the terminal has always read it; the window worked it out from "there is a return armed", which is blind to the other two cases ([fb8abac](https://github.com/alvarogabrielgomez/kanpachi/commit/fb8abac))
- Say that closing your own room ends it. Entering another room while hosting deletes the room's file and retires its entry from the registry, so its code stops working and there is nothing left to reopen. Both the window and `kanpachi` announced it as ports closing, which is the smaller half of what happens ([fb8abac](https://github.com/alvarogabrielgomez/kanpachi/commit/fb8abac))
- Reopen the room this machine hosts, instead of racing a return to somebody else's for it. A machine holding both state files fired both at startup with nothing to arbitrate, so whichever took the lock first won: either the room never came back and only an error line was left, or it came back with the return armed and asleep until somebody closed it ([fb8abac](https://github.com/alvarogabrielgomez/kanpachi/commit/fb8abac))
- Stop a guest leaving somebody else's room from deleting the record of its own. The line that clears it never checked whether this machine is the host, while the line below it did. A reopen that failed keeps that file on purpose, which is what still offers reopening by hand, so the room was lost in silence ([fb8abac](https://github.com/alvarogabrielgomez/kanpachi/commit/fb8abac))
- Remember the window's settings in the installed product, where none of them had ever been saved. They were written to the daemon's data directory, which users can only read, and the failure was swallowed by a debug print that a release build does not print: the narration switched itself off on every start and the window forgot its size. The daemon keeps them now, beside the name, and the window asks it to write the way it already did for the name and the registry ([68ebc58](https://github.com/alvarogabrielgomez/kanpachi/commit/68ebc58))
- Stop asking the publishing channel twice whether a newer version is out. The window kept the answer and the terminal asked GitHub again on every `kanpachi upgrade`, against a quota of sixty an hour shared by everyone behind one router. Both faces share one answer now; `--force` asks again ([7971a02](https://github.com/alvarogabrielgomez/kanpachi/commit/7971a02))
- Leave nothing behind in each Windows profile when Kanpachi is uninstalled. The window's log had been landing in `%LOCALAPPDATA%\Kanpachi` since it existed and nothing ever removed it ([0383eb2](https://github.com/alvarogabrielgomez/kanpachi/commit/0383eb2))
- Let people back into a room they were already in. Who may reach the host's control port was computed from the engine's peer table, which is a liveness signal: it arrives late, it arrives before the route carries an address, and it has no event at all for somebody who closes a laptop lid. Somebody who did not formally leave has not left the room, and their chair is still theirs, which is what the member key already promised. Measured against a real host in Kubernetes: three people locked out for thirty-three hours while the pod reported Ready ([6234fe0](https://github.com/alvarogabrielgomez/kanpachi/commit/6234fe0))
- Re-read the member list once the engine's route table has converged. The only thing that re-read it was an engine event that fires before the route carries an address, so the re-read that follows returns a list without the member who just arrived; routes converge seconds later and produce no further event. The mesh watcher already polled every second and threw the answer into a log line, and now it wakes the session too ([08d1487](https://github.com/alvarogabrielgomez/kanpachi/commit/08d1487))
- Let the host kick somebody who is offline. Kicking resolved the target against the engine's table alone and answered that the address belongs to no member, which is exactly the case that matters now that an absent member keeps their address ([6234fe0](https://github.com/alvarogabrielgomez/kanpachi/commit/6234fe0))
- Hold a kicked member's address for as long as the kick veto lasts. The clamp only ever shortened the credential, so one with seconds left freed its address while the veto still covered it, and whoever received that address next dropped out of the member list, stayed outside the gate with no firewall rule, and saw a dial that never answers ([64eb956](https://github.com/alvarogabrielgomez/kanpachi/commit/64eb956))
- Say on the host when a room runs out of addresses. The error belonged to the guest on the far side of the lobby, and the host wrote no line and showed no screen, so the owner of the room learned about it only if somebody told them ([ac8263e](https://github.com/alvarogabrielgomez/kanpachi/commit/ac8263e))
- Give every redial attempt its own deadline. Against a dropped SYN each attempt spent the kernel's whole budget, around 130 seconds, so the 1/2/5/10/20/30 second ladder degenerated into one attempt every two minutes ([3cb3537](https://github.com/alvarogabrielgomez/kanpachi/commit/3cb3537))
- Survive a transient accept error instead of leaving the listener dead for good. One exhausted file descriptor or one aborted connection left the host accepting nothing for the rest of the room's life, with everything green on the outside ([552a88b](https://github.com/alvarogabrielgomez/kanpachi/commit/552a88b))
- Report a gate bound to a dead interface as absent. A slot counted as applied as soon as a rule carrying its comment turned up, and none of that rule's expressions were ever read, so a gate whose adapter index had changed reported everything applied with every block matching nothing ([dbce998](https://github.com/alvarogabrielgomez/kanpachi/commit/dbce998))
- Carry the round trip the engine already sends. It arrived on every peers response and was thrown away, so the latency column was always blank and a diagnostic concluded "no round trip measured yet" from a field nobody ever wrote ([30504bc](https://github.com/alvarogabrielgomez/kanpachi/commit/30504bc))
- Emit a peer change only once the new member's route carries an address, in the engine. The bus fires the moment a connection comes up and the peer list drops every route with no address yet, so the daemon's re-read returned a list without the member who had just arrived; the routes converged seconds later and produced no further event ([8823a5d](https://github.com/alvarogabrielgomez/kanpachi-engine/commit/8823a5d))
- Keep the member book on disk, so a host that restarts can still kick somebody who was already inside and can hand them back the same address. That link lived only in memory and was written down as a price paid knowingly. It brings nobody back into the room: the engine's credentials die with the engine ([2b47a31](https://github.com/alvarogabrielgomez/kanpachi/commit/2b47a31))

### Added

- Show who is AFK. A member with a live credential the engine cannot see stays in the list, marked away, with how long they have been gone and how long their chair lasts. It shows where the round trip goes, the same in the window, the terminal and the wizard. The canary stops asking them for a report once a minute, the heartbeat stops renewing their credential, and the stale-credential notice stops spending its retries on somebody with no channel to hear it on ([243d958](https://github.com/alvarogabrielgomez/kanpachi/commit/243d958))
- Warn when a room has members and nobody has opened a channel. The host held both halves of that diagnosis, the member list and the map of open sockets, and nothing ever compared them ([48d3103](https://github.com/alvarogabrielgomez/kanpachi/commit/48d3103))
- Warn before a room runs out of addresses, while the host can still do something about it ([48d3103](https://github.com/alvarogabrielgomez/kanpachi/commit/48d3103))
- Say when the room changes state, and say it where a container looks. Sixteen places moved the room between states carrying the reason as an argument and none of them logged it. In a container the daemon now writes to standard output as well as to its file, because `docker logs` and `kubectl logs` read nothing else: a host spent thirty-three hours unable to admit anybody and `kubectl logs` showed not one line ([b43b3b2](https://github.com/alvarogabrielgomez/kanpachi/commit/b43b3b2))
- Name who is at that address before kicking them. Addresses get recycled, so the one that was somebody else yesterday belongs to somebody else today, and kicking cannot be undone. `--yes` skips the question ([d83d814](https://github.com/alvarogabrielgomez/kanpachi/commit/d83d814))
- A guide for running a game server on Kubernetes, with the three things a cluster does differently that cost a whole evening each ([7fe4eb8](https://github.com/alvarogabrielgomez/kanpachi/commit/7fe4eb8))

### Changed

- One record per member instead of five stores keyed by the same address. The credential book, the kick veto, the stale-notice backoff, the last mesh sighting and the merged peer list were all swept in the same places and read by the same functions, and nothing reconciled them. Presence now carries a timestamp per source, because the engine, the control socket and the book each know something the other two cannot ([2ebf54c](https://github.com/alvarogabrielgomez/kanpachi/commit/2ebf54c))

## [0.6.8] - 2026-08-20

### Fixed

- Paint the game's status dot, which had never once appeared in the window. `Room.copyWith` rebuilt the room without carrying the health, the address the game listens on, or the redirect, so all three fell back to "not known"; and every room goes through that copy before reaching the screen, because that is where the foreign-rule findings get attached. The daemon measured it correctly and `kanpachi status` said so, and it was erased in the last metre. Neither a host nor a guest has seen it since it shipped ([16c463b](https://github.com/alvarogabrielgomez/kanpachi/commit/16c463b))
- Tell a guest the game is reachable when the host is redirecting traffic to it. The redirect never travelled in the announcement, so "bound to another address" arrived without the half that matters and the guest's screen claimed the game could not be played while they were playing it. Measured with Zomboid in a container: green on the host, amber on the guest, game running ([16c463b](https://github.com/alvarogabrielgomez/kanpachi/commit/16c463b))
- Read `elsewhere` when it arrives over the wire. A guest could translate "listening" and "silent", and the third value fell through to the default, so the only state a containerised server produces was the only one thrown away ([16c463b](https://github.com/alvarogabrielgomez/kanpachi/commit/16c463b))
- Measure the health of a game whose profile declares a range on both protocols. The comparison required the listener's protocol to equal the range's, and a listener is tcp or udp and never both, so the range matched nothing and a running server read as silent. It hit Age of Empires II, Stardew Valley and 7 Days to Die ([16c463b](https://github.com/alvarogabrielgomez/kanpachi/commit/16c463b))
- Redirect a range declared on both protocols. A nat rule carries one protocol and only one, and the range was rejected whole instead of expanded, so those profiles got no redirect at all ([16c463b](https://github.com/alvarogabrielgomez/kanpachi/commit/16c463b))
- Keep the host's announced redirect when reapplying a game this machine has just imported. The announcement was rebuilt without that field and zeroed it, which is a false amber at the exact moment the game card appears, until the next periodic announcement two minutes later ([16c463b](https://github.com/alvarogabrielgomez/kanpachi/commit/16c463b))
- Announce a redirect the moment it goes up or comes down, instead of leaving it up to two minutes. The heartbeat compared the game's health alone, and putting a redirect in place does not change that health: it stays "listening elsewhere" while the room goes from unreachable to reachable, which is the whole difference between a guest who can join and one who cannot ([7761f98](https://github.com/alvarogabrielgomez/kanpachi/commit/7761f98))
- Refuse to redirect the room's traffic to a loopback address. The kernel drops as martian a packet that arrives on an adapter addressed to `127.0.0.0/8`, so the rule went in, the dot turned green, the tooltip said there was nothing to do, and not one datagram reached the game. A server bound to `127.0.0.1` now reads as listening elsewhere and says so, which is what a host can act on ([7761f98](https://github.com/alvarogabrielgomez/kanpachi/commit/7761f98))

### Changed

- Open Steam's ports as well as the game's in the Project Zomboid profile, both ranges on TCP and UDP ([16c463b](https://github.com/alvarogabrielgomez/kanpachi/commit/16c463b))
- Explain the game's state in the dot's tooltip, with one text for the host and another for the guest. The sentence lived under the game's name and was written for one of them: "bind it to 0.0.0.0" is something only the host can do, and a guest read it as a chore they cannot carry out. A guest is now told whether they can join or whether to wait, and nothing else ([16c463b](https://github.com/alvarogabrielgomez/kanpachi/commit/16c463b))
- Name the fix this product recommends first, binding the game to Kanpachi's own address, and leave `0.0.0.0` as the way out for a game that will not let you choose. The message sent people to open their server on every card in the machine when the doctrine, written in the catalogue and in the domain, is the opposite ([16c463b](https://github.com/alvarogabrielgomez/kanpachi/commit/16c463b))
- Pulse only the green dot. Colour is a single channel and it fails for anyone who cannot tell amber from green; motion is a second channel that does not depend on it, and a beat says the thing that needs saying, that the server is alive ([16c463b](https://github.com/alvarogabrielgomez/kanpachi/commit/16c463b))
- Stop the relay notice pulsing. Somebody arriving over the relay is a settled fact about the room, and a dot beating over a fact asks for attention that leads nowhere. Reconnecting still pulses, because that is happening while you read it ([16c463b](https://github.com/alvarogabrielgomez/kanpachi/commit/16c463b))
- Put a shield on the exposure card, where the green dot was. That dot distinguished nothing: the card itself is the state, and the green only repeated the title ([16c463b](https://github.com/alvarogabrielgomez/kanpachi/commit/16c463b))
- Shorten the notice for returning to a room: a ring spinning while the attempt runs, a dot beating while the clock does, the leave button on the right, and one line instead of three. The bar still appears only when steps arrive to move it, because it is drawn from what already happened ([16c463b](https://github.com/alvarogabrielgomez/kanpachi/commit/16c463b))
- Say in `kanpachi status` what each role can act on, the way the window already does. A guest read "bind the server to 0.0.0.0", which is a job on somebody else's machine, and a host that was already redirecting read that instruction directly above the line saying the traffic arrives anyway. A host is now given the room's own address to bind to, a guest is told only that the host has to fix it, and a redirect in place replaces both with the address it is going to ([7761f98](https://github.com/alvarogabrielgomez/kanpachi/commit/7761f98))

## [0.6.7] - 2026-08-20

### Fixed

- Open the game's ports to a guest whose arrival the engine never announced. The host recalculated its rules only on the engine's `peers_changed` event, and measured on 2026-08-20 that event never came: the guest had a credential, an open control channel and `MEMBERS (2)` on their own screen, while the host showed `MEMBERS (1)` and had no rule for the game at all, so every packet died at the host's own gate. The safety net for exactly this undercount lived inside the reread that was not happening. The control channel opening now triggers the reread, which is first-hand evidence that somebody is there and the same evidence that already decides who may talk to the control channel ([45b6a99](https://github.com/alvarogabrielgomez/kanpachi/commit/45b6a99))
- Tell a guest which game is active the moment they walk in, instead of up to two minutes later. Entering a room triggered no announcement, so a new arrival waited for the next periodic one and read "X has not picked a game yet" in the meantime, which is a claim about the host and not an admission of not knowing. The announcement is addressed to that one member, the way a kick notice already was, and the periodic clock is left alone so several arrivals in a row cannot starve the room of its general announcement ([45b6a99](https://github.com/alvarogabrielgomez/kanpachi/commit/45b6a99))
- Ship a sidecar template that runs. `docker/templates/compose.sidecar.yml` named an image that does not exist, which fails the pull, and once past that the game needed `MAX_MEMORY` to start at all, wrote its saves to a path the file did not mount, and died on a volume owned by root while the server runs as uid 10000. It also told you to bind the server to `0.0.0.0`, which that image rewrites to the container's address in its own entrypoint, on purpose. `docker/README.md` said the same thing and now says where it holds and where the redirect is the only way in ([8edaef1](https://github.com/alvarogabrielgomez/kanpachi/commit/8edaef1))

## [0.6.6] - 2026-08-20

### Added

- Reach the game even when its server is bound to another address of the host's machine. In a container, the room's traffic is sent to wherever the game actually listens, in the ports the profile declares and nowhere else, and both the room and `kanpachi status` say where it is going. Everywhere else Kanpachi names the fix instead: "listening on 10.42.0.15, not on the room's address, bind it to 0.0.0.0". A server bound to the container's own IP answered "port unreachable" to a whole room while the tunnel, the ports and every screen looked perfect ([ea5eb1a](https://github.com/alvarogabrielgomez/kanpachi/commit/ea5eb1a))

## [0.6.5] - 2026-08-19

### Added

- Say whether the game's server is actually up, with a dot next to the game in the room and `(healthy)` next to its name in `kanpachi status`. The host reads its own socket table and the answer travels with the room announcement, remeasured every 15 seconds and sent the moment it changes, because a guest cannot find this out on its own: a UDP port with nobody behind it is as silent as one with the server running ([c3b8b7e](https://github.com/alvarogabrielgomez/kanpachi/commit/c3b8b7e))
- Ask any command how it is written, with `kanpachi <command> --help` or `kanpachi help <command>`: what it does, every flag it takes, what each flag changes, and an example to paste. Only the one-line list existed, so the flags of `profile`, `upgrade`, `host` and `join` were readable nowhere. `kanpachi --help` and `kanpachi help` stay the same page as before ([bae8c89](https://github.com/alvarogabrielgomez/kanpachi/commit/bae8c89))
- Run Kanpachi as a container: `docker/` carries the image and four whole compose files to copy, and the room comes back with the same invite code after the container is destroyed and rebuilt, because the state lives in a volume rather than in the image. The entrypoint prints the code and the link on every start, since `docker logs` is the only place an unattended server can be asked, and it refuses early with a readable message when the compose forgot `NET_ADMIN` or `/dev/net/tun` ([05d525e](https://github.com/alvarogabrielgomez/kanpachi/commit/05d525e))
- Describe a game the catalogue does not have without leaving the terminal, with `kanpachi profile <id> --name <n> --tcp <ports> --udp <ports>`. Creating a profile existed only in the Windows window, so a headless Linux host had no way to open a port for anything outside the eleven games that ship. Saving the same id again updates it instead of adding a second, which is what lets a container run it on every start ([d39c1cb](https://github.com/alvarogabrielgomez/kanpachi/commit/d39c1cb))

### Changed

- Offer the way back to your last room as a row under the code field, `×  Volver a <sala>  [Volver]`, in one line that ellipsises when the name is long. It was a notice with a title, three lines of prose and a full-width primary button, and it pushed the join and create fields down the page to explain what the button already says. The cross is new and it forgets the room on disk, so dismissing it survives a restart rather than coming back on the next start ([152fc3c](https://github.com/alvarogabrielgomez/kanpachi/commit/152fc3c))
- The client's source is in English: comments, identifiers and file names, `daemon/cmd/kanpachi` in full. Nothing it prints changed ([bae8c89](https://github.com/alvarogabrielgomez/kanpachi/commit/bae8c89))

### Fixed

- Say in the sidecar template and in `docker/README.md` that the game server has to listen on `0.0.0.0`. Sharing a network namespace is not enough: Kanpachi delivers packets to its own address, so a server bound to the container's own IP answers "port unreachable" to the whole room while the tunnel and the ports look perfect
- Stop `kanpachi game --help` trying to activate a game called `help`. `--help` was rewritten into the `help` subcommand wherever it appeared, so any command followed by it ran with `help` as its argument ([bae8c89](https://github.com/alvarogabrielgomez/kanpachi/commit/bae8c89))
- Line the command list back up. `profile` and `upgrade` carried their flags in the name column, which is 26 characters wide and which they overflowed by 13 and 23, pushing their descriptions off the column every other row lines up on ([bae8c89](https://github.com/alvarogabrielgomez/kanpachi/commit/bae8c89))
- Stop `docs/public/run-a-game-server.md` telling people to run `kanpachi game zomboid`, which is not a game id and answers `unknown_game`. It is `project-zomboid`, and the eleven ids are now listed with their ports in the command reference, where before they appeared nowhere a user could read ([05d525e](https://github.com/alvarogabrielgomez/kanpachi/commit/05d525e))

## [0.6.3] - 2026-08-19

### Added

- Install the published version even when the number already matches, with `--force` on `kanpachi upgrade` and on `kanpseed upgrade`. Both compared numbers and answered `Already up to date`, which is the wrong answer when a version was republished over a fix: the tag keeps its number and the installed machine has different bytes. On the client `--force` also tells apt to reinstall, because apt compares numbers too and would answer `already the newest version` without touching a file. Found on the droplet, which could not pick up the rebuilt 0.6.2 by any combination of the flags that existed ([701669b](https://github.com/alvarogabrielgomez/kanpachi/commit/701669b))

## [0.6.2] - 2026-08-19

### Fixed

- Stop published builds claiming a dirty tree again: 0.6.1 fixed this by ignoring `motor/`, the folder the release cloned the engine into, and this version renamed that folder to `engine/` when the engine started arriving as a download instead of a clone. The ignore rule did not come along, so `kanpachi version` went back to answering "with uncommitted changes" on a binary built from a clean tag. Caught on the droplet running the first build of this very version, which is why the tag was cut again ([41f42f1](https://github.com/alvarogabrielgomez/kanpachi/commit/41f42f1))

### Added

- Answer "which engine is this" everywhere it gets asked: `kanpachi version` now says `engine 0.1.0+g<commit> (easytier@v2.6.4-kanpachi.1)` read straight off the binary's sealed sentinels, `doctor` adds it to the engine verdict, the settings screen shows it under the product version, and the daemon logs the running engine's own answer once per process. An engine older than the sentinels reads as unknown instead of guessed ([b867f85](https://github.com/alvarogabrielgomez/kanpachi/commit/b867f85))

### Changed

- Ship the exact engine binary that passed its own checks: releases stop recompiling the engine from a moving branch and download the tagged, hash-pinned binaries its repository published. `engine.pin` records the tag and both SHA256s, the release refuses anything that does not match, and waits up to 25 minutes for an engine still publishing before going red with a name. Release bodies name the resolved tag instead of `@main` ([b867f85](https://github.com/alvarogabrielgomez/kanpachi/commit/b867f85))

## [0.6.1] - 2026-08-18

### Fixed

- Stop every published build claiming it was made from a dirty tree: `kanpachi version` said "with uncommitted changes" on binaries that came straight off a tag, because the release clones the engine's repository INTO the checkout and Go stamps the binary from what git sees there. The one question that command exists to answer was the one it got wrong ([9dbf7db](https://github.com/alvarogabrielgomez/kanpachi/commit/9dbf7db))
- Stop `kanpachi upgrade` ending on a paragraph about apt losing its sandbox: the package lands in the state directory, which is root-only on purpose, so the user apt fetches with cannot read it and apt says so every single time. Nothing is being fetched at that point — the file is already on disk with its SHA256 checked against the release manifest — so the run now says as much up front instead of letting apt discover it ([a912ffe](https://github.com/alvarogabrielgomez/kanpachi/commit/a912ffe))

## [0.6.0] - 2026-08-18

### Changed

- One name per machine, kept by the daemon: the window, the terminal and the wizard all read and write the same one, so changing it anywhere changes it everywhere. Each face used to keep its own file side by side in the data folder, and the room showed whichever one had entered it — a window saying Alvaro and a room showing AlvaroGDeskt. Your existing name is adopted on the next start with nothing to answer, `kanpachi name` shows or changes it from the terminal, and a machine where nobody has chosen one still opens a room with a name derived from its own — said out loud, never written down, because a guess saved to disk is a guess that beats the real answer ([670cb6a](https://github.com/alvarogabrielgomez/kanpachi/commit/670cb6a))

### Fixed

- Stop handing out a broken invite link when you are a guest: the key that unscrambles a room card is kept only by whoever hosts, so every guest was pasting a link ending in forty-three A's — thirty-two zero bytes — and whoever received it got a fragment that opens no card. A guest's link is now the dictated form, with no fragment, which enters the room just the same and shows the generic card. Reported live against the Linux CLI ([a00e14f](https://github.com/alvarogabrielgomez/kanpachi/commit/a00e14f))
- List `quarantine` in `kanpachi help`, where it had never appeared: the command shipped in 0.5.0 with its three faces and nothing telling anybody it exists, because the help is drawn from a second list a new command has to be added to by hand. It gains its own section in the command reference too, symptom first ([79bd3bc](https://github.com/alvarogabrielgomez/kanpachi/commit/79bd3bc))
- Keep the room's name on every guest's screen when the host announces without one: an empty name in an announce means the host has no name to send, never a rename, and taking it wiped the name learned from the invite one heartbeat after joining. The room header now also picks the name up when it arrives late, which is how it reaches whoever entered with the bare code ([84c9d92](https://github.com/alvarogabrielgomez/kanpachi/commit/84c9d92))
- Stop saying the quarantine "could not be checked" for the first minute after every start: the sweep that measures it only ticks once the interval is over, so the daemon repaired the rules and then every face vouched for nothing about them. The start now measures before it publishes anything, whatever the recorded answer was. Found live on Windows with the 48 rules already written ([20cf4b0](https://github.com/alvarogabrielgomez/kanpachi/commit/20cf4b0))

### Added

- Show the way back actually moving: while a return attempt runs, the home notice fills the same progress bar the waits use, fed by the daemon's real steps; between attempts the countdown text carries, as before. Asked for while watching a real return that said it was returning with nothing moving ([029ad84](https://github.com/alvarogabrielgomez/kanpachi/commit/029ad84))
- Leave the machine set up as recommended straight from the sign-up screen: a ticked-by-default box next to Continuar closes this PC's risky server ports when you save your name, with a "?" that explains in three lines what that means before it happens — it holds on every network, it stays until you remove it, and reaching OTHER machines never changes. Unticking it answers nothing: the decision stays untaken and the first room from the terminal still asks. Changing your name later shows no box and touches nothing but the name ([c7eea89](https://github.com/alvarogabrielgomez/kanpachi/commit/c7eea89))

## [0.5.0] - 2026-08-18

### Changed

- Stop closing this machine's risky server ports without asking: the base quarantine became YOUR decision, asked once at the door of `kanpachi host` and `join` with the exact ports listed and no default answer, `--quarantine on|off` answering it from a script, a terminal-less run without the flag refused on purpose, and the window never blocked on it. Saying yes closes them until you say otherwise, saying no removes what a yes had closed, every daemon start and `--reset` obey the recorded answer, and nothing but uninstalling or your own no ever removes it ([2fbbe5d](https://github.com/alvarogabrielgomez/kanpachi/commit/2fbbe5d), [8ac7234](https://github.com/alvarogabrielgomez/kanpachi/commit/8ac7234))

### Added

- Flip the quarantine like the switch it is, from any face: in the window's Configuración, drawn from the measurement and never from the intention, with plain words on what each position means and a one-line confirmation only when reopening the ports; from the wizard's menu entry that carries its state in its own label; and with `kanpachi quarantine on|off`, whose bare form tells the state in symptom-first words — "having trouble sharing a folder from this PC? this is why" — for whoever arrives without knowing the word quarantine. A room open without it shows the notice with the close-them button right next to it, and `kanpachi doctor` explains it on both systems, fixing only what a recorded yes asked for ([964449a](https://github.com/alvarogabrielgomez/kanpachi/commit/964449a), [3787bc3](https://github.com/alvarogabrielgomez/kanpachi/commit/3787bc3), [96827d1](https://github.com/alvarogabrielgomez/kanpachi/commit/96827d1))
- Measure whether the base quarantine is actually in force, once a minute, from the system itself: every rule present, some missing, disabled or edited away, none at all, or could-not-check, each its own answer. It travels in the status every face polls, visible today under `quarantine` in `kanpachi status --json`, and it is what the upcoming notices and the doctor will read ([ab1f223](https://github.com/alvarogabrielgomez/kanpachi/commit/ab1f223))

### Fixed

- Stop the Linux quarantine strangling the machine talking to ITSELF: a local process connecting to a quarantined port on 127.0.0.1 hung until its timeout, measured on the bench with a control port that connected instantly. The loopback is exempted first in both chains, which is what Windows already did at the system level — the quarantine protects from networks, and a machine talking to itself is not one ([88974b2](https://github.com/alvarogabrielgomez/kanpachi/commit/88974b2))
- Answer `kanpachi quarantine` asked with nothing: the bare read travels without parameters, the daemon refused it with "faltan los parámetros", and the state-telling half of the new switch never worked. Found live the first time the command met a real daemon — the exact absent-params defect `seed` had already found and fixed on its own method ([3b33818](https://github.com/alvarogabrielgomez/kanpachi/commit/3b33818))
- Let a GUEST disable the game's own leftover firewall rule on their machine: the notice offered the button and the daemon refused with "only the host can do that", a guard inherited from profile activation. The rule lives on the guest's own firewall and leaves their game reachable from their home network and from the whole room, so the suspension asks for a room and nothing else — which is what the restore-on-exit already assumed, running for every role since day one. Found live on 2026-08-18 as a guest in a friend's room ([3c91575](https://github.com/alvarogabrielgomez/kanpachi/commit/3c91575))
- Show the quarantine notice on the ROOM screen, not only on the home: while playing you live on the room screen, and opening a room is precisely one of the notice's triggers, so it was firing exactly where the player was not looking. Found live: the CLI showed it, the window did not ([3f9238f](https://github.com/alvarogabrielgomez/kanpachi/commit/3f9238f))
- Answer the Unirse click the instant it happens: asking the registry what is behind a code takes seconds with a distant one, and the button gave no reaction for that whole gap — a click without an answer is a click that gets repeated. It now spins and refuses reentry until the answer arrives ([40cba16](https://github.com/alvarogabrielgomez/kanpachi/commit/40cba16))
- Give the window the way back to your last room: leaving on purpose or being kicked keeps the room with its automatic return off, and the CLI and the wizard already offered going back while the window never even asked the daemon. The home now shows the room with a "Volver a la sala" button that enters through the same trust confirmation as pasting the code ([8ce9cf4](https://github.com/alvarogabrielgomez/kanpachi/commit/8ce9cf4))

## [0.4.0] - 2026-08-17

### Added

- Come back to the room you were in without pasting the code again: Kanpachi goes back to it by itself and keeps trying every five minutes, for as long as that room exists, whether it was already running when the host reappeared or was started afterwards. Closing Kanpachi, shutting the PC down, a power cut and a host that spends the night switched off are none of them leaving, so all of them come back. Three things stop it: pressing "salir de la sala", being kicked, and the meeting server saying that code is gone — and being kicked does not take the room away, the button to go back is still there, it just does not go back on its own ([e2eafd6](https://github.com/alvarogabrielgomez/kanpachi/commit/e2eafd6), [ed4484c](https://github.com/alvarogabrielgomez/kanpachi/commit/ed4484c))
- See where a machine on its way back stands, on the home screen, in `kanpachi status` and in the wizard: which room it is going to, how long until the next try, and whether it is the host who is not there or the meeting server that is not answering, which are two different reasons to keep waiting ([0a0dc52](https://github.com/alvarogabrielgomez/kanpachi/commit/0a0dc52), [97d8773](https://github.com/alvarogabrielgomez/kanpachi/commit/97d8773))
- Enter another room, or open your own, without leaving the one you are in first: Kanpachi says what doing it costs — leaving that room, closing yours with everyone inside and the game's ports, or giving up on the one you were going back to — and does the whole thing in one go once you say yes. From the terminal it asks the same, and a script with no terminal and no `--yes` gets refused instead of quietly abandoning the room that machine was in ([cc82839](https://github.com/alvarogabrielgomez/kanpachi/commit/cc82839))
- Read what the meeting server's HTTP surface actually is, in `registry/API.md`: every endpoint it has, what the optional hosting password does and does not cover, and what defends each thing ([271153f](https://github.com/alvarogabrielgomez/kanpachi/commit/271153f))
- Use, study, change and share Kanpachi: it is free software under the AGPL-3.0, with the game catalogue in the public domain. Everything was already public to read; now there is a licence that says what you may do with it, and that obliges anyone running a *modified* meeting server for other people to hand them its source ([3ade2f8](https://github.com/alvarogabrielgomez/kanpachi/commit/3ade2f8))
- See what ships alongside Kanpachi and under which licence before installing it, on a screen the installer shows and in `THIRD-PARTY-NOTICES.txt` next to the program: the network engine, where its source is, and the three Windows libraries that come with it ([3ade2f8](https://github.com/alvarogabrielgomez/kanpachi/commit/3ade2f8))
- Come back to a room as YOURSELF: each guest signs its credential request with a key of its own, per room, so the host hands the returning machine its old credential and its old address instead of a fresh pair — across restarts of Kanpachi too — and nobody can claim your place by picking your nickname. Leaving on purpose drops the key, and a kicked machine that returns comes back as a brand-new member with nothing of what it had ([e69a10b](https://github.com/alvarogabrielgomez/kanpachi/commit/e69a10b))
- Get told in the first second when a firewall on the machine (ufw, firewalld) would swallow the room, instead of a room that assembles perfectly and nobody can enter: `kanpachi host` and `join` refuse naming who blocks, both adapters and the exact commands, ask whether to open it — `--yes` says yes, a script with no terminal gets refused on purpose — and whatever gets opened is written down and closed again when the room ends, or on the next start after a dirty death. Turning the firewall on mid-room raises a warning on the room screen with the same commands ([9c1d411](https://github.com/alvarogabrielgomez/kanpachi/commit/9c1d411))
- Fix a blocking firewall from `kanpachi doctor`: the check that could only warn now marks a manager about to swallow the room as BAD with the exact commands, and `--fix` runs those same commands through the same ledger the room's question uses, undone when the room closes or on the next service start ([14aa6b8](https://github.com/alvarogabrielgomez/kanpachi/commit/14aa6b8))
- Read where to paste the address INSIDE the game, right under it on the room screen: the profile has carried that sentence since the catalogue existed and no screen ever showed it, so Kanpachi handed you an address and left the one step you cannot guess unsaid ([a2709fa](https://github.com/alvarogabrielgomez/kanpachi/commit/a2709fa))
- See the address to connect to in `kanpachi status` and in the terminal wizard, which the window has shown since it existed and this face never did: the host's address and the active game's port, labelled to hand out when you are the host and to connect to when you are not ([a2709fa](https://github.com/alvarogabrielgomez/kanpachi/commit/a2709fa))

### Changed

- Stop calling your own room "pending" where Kanpachi writes it down, because nothing is pending anybody's decision: `kanpachi pending` heads it "Saved room", and the window answers "esta máquina no tiene ninguna sala guardada" instead of "no quedó ninguna sala a medio cerrar del arranque anterior". The room comes back by itself on every start, and reopening it by hand is what is left for when that fails ([3782c9f](https://github.com/alvarogabrielgomez/kanpachi/commit/3782c9f))

### Fixed

- Stop rewriting the whole firewall twenty or thirty times a second while a room is being opened or a member is joining. The network engine reports its peers in bursts, and each report rebuilt and re-applied a rule set identical to the one already installed: measured at 19 applications per room opened, 31 inside a single second, 2221 across a day at the bench, and zero while a room sits still. On Windows each one of those reads the machine's entire firewall rule store twice — 152 ms over 1157 rules — so a burst was seconds of work for nothing, on the exact kind of pattern an antivirus watches. Rules that somebody deletes behind Kanpachi's back are still put back, by the sweep that exists for that ([fa5850a](https://github.com/alvarogabrielgomez/kanpachi/commit/fa5850a))
- Stop the room card handing out an address that leads nowhere. When the host was not in the members table — a reconnection, a host that just went down, a list still arriving — the card fell back to the FIRST member in the list, anybody at all, including yourself, and painted their address as the game's, with a Copy button next to it and no error anywhere. It now says it is waiting for the host ([a2709fa](https://github.com/alvarogabrielgomez/kanpachi/commit/a2709fa))
- See who hosts the room on the card instead of `host: —`, which is what it said every single time: the name was read from a field nothing ever filled, while the answer sat in the members list on the same screen ([a2709fa](https://github.com/alvarogabrielgomez/kanpachi/commit/a2709fa))
- Recognise a blocking foreign firewall in the window too, instead of it arriving as an error the app had no name for ([a2709fa](https://github.com/alvarogabrielgomez/kanpachi/commit/a2709fa))
- Come back to a room in seconds and with the SAME address when the tunnel dies under you, instead of asking the host for a new credential once a minute and burning a fresh address per lap: a guest now rebuilds its tunnel with the credential it already holds, so the game server never notices, and the full exchange through the lobby remains what runs when the host really did forget everybody ([bc9fd3b](https://github.com/alvarogabrielgomez/kanpachi/commit/bc9fd3b))
- Keep using a room's code while its host is switched off, instead of it going dead after six hours. A room now ends for two reasons and neither of them is silence: somebody closed it, or nobody hosted it for three weeks. A server that spends the night down, a PC that reboots, a power cut — the code you handed out still works when the host comes back, and the invitation page keeps showing the room's real name instead of falling back to a generic one ([4819d8a](https://github.com/alvarogabrielgomez/kanpachi/commit/4819d8a))
- Stop being asked whether to reopen your room while it is already reopening. A host that restarts got the question on top of the reopening it was asking about, with both of its buttons wrong at that moment: "Reabrir" answered that you are already in a room, and "Cerrarla" deleted the room out from under a reopening in flight. The room comes back on its own, and what is left is a notice for when it does not ([97d8773](https://github.com/alvarogabrielgomez/kanpachi/commit/97d8773))
- Get told a room is closed the moment you paste its code, instead of waiting twenty-one seconds for a message about the host reconnecting. Closing a room now tells the meeting server, so the code stops working right then. Renewing a code closes the old one too, which is what makes "renewing shuts the door" true when you press it rather than six hours later. Reopening your own room with the same code is untouched: closing marks the room over, it never releases the reservation of the code ([4fe32df](https://github.com/alvarogabrielgomez/kanpachi/commit/4fe32df))
- Find out when a room that reopened by itself came back without its game, instead of it being a line in a log nobody reads. A host that restarts brings its room back with the same code, and if the profile that was active is no longer in that machine's catalogue the room comes back with no ports open at all: everything looks fine, people join, and the server does not answer. The room now says so, and says the room itself is fine and the fix is to pick the game again ([5ff3516](https://github.com/alvarogabrielgomez/kanpachi/commit/5ff3516))

### Security

- Close the control channel to whoever is not in the room. The host's most sensitive listener was open to every address issued in the last 24 hours, present or not — measured at 73 addresses for a single guest while the game's rule had 2. It now admits the members present plus whoever got a credential in the last ten minutes and may still be on their way in, kicking someone slams it shut for them in the act, and a guest that leaves stops reaching it right away instead of a day later ([d1b1613](https://github.com/alvarogabrielgomez/kanpachi/commit/d1b1613))
- Stop a room that cannot hold itself from chewing through rejoins forever: after eight back-to-back re-entries with no calm in between, the guest leaves with a message that says so, and the automatic return keeps trying at its slower pace ([d1b1613](https://github.com/alvarogabrielgomez/kanpachi/commit/d1b1613))
- Rate limit the invitation page, which resolves invite codes exactly like the API does and was answering at any rate anybody asked for. A live code came back with the room card embedded in the page and a dead one came back empty, so walking the eight-character space was a matter of skipping the endpoint that counts and asking the page instead. Both now share one budget per address, so moving to the other route buys nothing ([1f83c0f](https://github.com/alvarogabrielgomez/kanpachi/commit/1f83c0f))

## [0.3.0] - 2026-08-14

### Security

- Refuse a credential in the lobby that is not signed by the host of that room. Anybody holding the invite code could sit on the host's lobby address and answer the request first, and what that got them is your machine joining THEIR network while it believes it is in your friends' one — with the game's ports opened towards it. The host now signs its answer with the long-term key of its installation, bound to the room and to that one request, and the guest checks it against the key the meeting server pinned for that code ([c84c683](https://github.com/alvarogabrielgomez/kanpachi/commit/c84c683))
- Check the same signature on the invitation web page, which claimed to check it and never did: a page that is handed a card the room's own key does not back now says it could not verify the invitation, instead of printing whatever name and nickname it was given. Browsers without Ed25519 in WebCrypto say nothing was verified — they never pretend it was ([b011aee](https://github.com/alvarogabrielgomez/kanpachi/commit/b011aee))
- Check the room card against the key the meeting server itself pinned for that code, instead of taking the card and the server's word for it. A server that has been taken over can no longer change the room name or the nickname on the invitation screen without the change showing: the card no longer opens, and the screen says so rather than painting what that server wanted read ([99fb2d5](https://github.com/alvarogabrielgomez/kanpachi/commit/99fb2d5))

### Added

- See who is hosting before you go in: the room you are about to enter now says whether you have played with that host before, and shows the fingerprint of the key the meeting server has pinned for that code. "You have played with Humberto, in 5 rooms" is a different sentence from "this is the first time", and until now the app could say neither ([4390c52](https://github.com/alvarogabrielgomez/kanpachi/commit/4390c52))
- Get warned when a host you know arrives with a different key, with the old fingerprint above the new one so the two can be compared over any other channel. It does not lock the door: reinstalling Windows produces a new key legitimately, so the button stays and reads "Entrar igual" ([4390c52](https://github.com/alvarogabrielgomez/kanpachi/commit/4390c52))
- See the game covers, which never once appeared: the hole with PORTADA STEAMDB written in it was all there ever was, and nothing anywhere asked for an image. They now come from Steam itself, in the shape each hole needs — the tall one for the thumbnails and the room, the wide one for the catalogue grid ([5bf4a72](https://github.com/alvarogabrielgomez/kanpachi/commit/5bf4a72))
- Give a game its Steam id when you add it by hand, and watch its cover appear in the preview as you type. The whole address, pasted from the browser, works too ([5bf4a72](https://github.com/alvarogabrielgomez/kanpachi/commit/5bf4a72))

### Changed

- **Entering a room now needs both machines running this version or newer.** A host on an older Kanpachi does not sign anything, and an unsigned answer is exactly what somebody impersonating the host would send, so it can no longer be told apart or accepted. Updating the host fixes it ([c84c683](https://github.com/alvarogabrielgomez/kanpachi/commit/c84c683))
- Take the cover from the Steam id the profile already carries for detection, instead of a link written into each profile: a URL inside a file nobody re-reads is a link that expires, copied into every profile anyone shares ([5bf4a72](https://github.com/alvarogabrielgomez/kanpachi/commit/5bf4a72))
- Say SIN PORTADA in the hole of a game that has none — two of the eleven profiles that ship with Kanpachi are not on Steam — instead of naming a site the covers never came from ([5bf4a72](https://github.com/alvarogabrielgomez/kanpachi/commit/5bf4a72))

### Fixed

- See the covers whole where they are shown wide — the game picker and the preview of a game added by hand — instead of with both sides cut off. The hole was 104 pixels tall at whatever width the window handed it, and the art it receives is 460×215, so the picture was scaled to fill and what got trimmed was the edge where the posters carry their title ([ba7c00b](https://github.com/alvarogabrielgomez/kanpachi/commit/ba7c00b))

## [0.2.2] - 2026-08-14

### Added

- Explain the meeting server whole, in `kanpachi-seed.md`: what it does, when your packets go through it and why it cannot read them, what its state file actually holds, what a modified one could do differently, and the lobby gap that is still open. The three "Más información" links in the app go there, instead of an anchor that existed on no page ([a1b616d](https://github.com/alvarogabrielgomez/kanpachi/commit/a1b616d))
- Go to the server screen from the bottom-right corner of the window, which is the only place the server is written down at all times and led nowhere. Hovering it says in one line what that machine is ([a1b616d](https://github.com/alvarogabrielgomez/kanpachi/commit/a1b616d))

### Fixed

- Type or paste a whole invite code on the home screen. The field dropped every character that is not a letter, a digit or a dash and cut what was left to nine, so `A7K2-M9QX@seed.example.com` became `A7K2-M9QX` — measured — and since a bare code has not been enough to join since the server started travelling inside the code, **the join button could never light up at all** ([a1b616d](https://github.com/alvarogabrielgomez/kanpachi/commit/a1b616d))
- Say what is missing under the field when what you typed is a bare code, instead of leaving a dead button next to eight characters that look complete ([a1b616d](https://github.com/alvarogabrielgomez/kanpachi/commit/a1b616d))
- Carry the room name you typed on the home screen into the dialog that confirms the server, which showed the suggested one instead. The field was never writing it down, so everything downstream read an empty draft — and the other direction did work, which is what made it look connected ([a1b616d](https://github.com/alvarogabrielgomez/kanpachi/commit/a1b616d))
- Open the room with the name you typed when the server asks for a password, and when you go through the game picker: both paths used a name of their own, so the room ended up called something else ([a1b616d](https://github.com/alvarogabrielgomez/kanpachi/commit/a1b616d))

### Changed

- Stop saying that everyone who comes in with your code passes through the meeting server, on the screen that asks for it and in the trust dialogs. It introduces people until the room is up and then steps out; what keeps going through it is the one connection that could not find a direct path, encrypted, and it has nothing to open it with ([a1b616d](https://github.com/alvarogabrielgomez/kanpachi/commit/a1b616d))
- Cut the warning above the confirm button down to two lines that each decide something — a modified server can write down the public IP of everyone who comes in, and what is inside the room is unreadable to any of them — instead of four that also claimed a bad one may "try to capture information between participants", which it cannot. The whole account moved to `kanpachi-seed.md`, behind the link ([a1b616d](https://github.com/alvarogabrielgomez/kanpachi/commit/a1b616d))
- Underline the "Más información" links when the mouse is on them, so the colour is not the only thing saying they can be pressed ([a1b616d](https://github.com/alvarogabrielgomez/kanpachi/commit/a1b616d))
- Say what the meeting server is for in Settings, instead of what the app does with it: it introduces the people who come in with your code to each other until the room is up, and from there the game goes straight between you, bar the connection that cannot find a direct path ([a1b616d](https://github.com/alvarogabrielgomez/kanpachi/commit/a1b616d))
- Say "Kanpachi 0.2.1" and, once asked, "tienes la versión más nueva hasta ahora", instead of "Tienes la 0.2.1" over a line that says the same again. With a newer one known the button stops offering to search, which cannot answer anything different any more, and offers to download ([a1b616d](https://github.com/alvarogabrielgomez/kanpachi/commit/a1b616d))
- Call the diagnostic switch "Mostrar detalles del servicio Kanpachi", and say in two lines what it shows and what it costs, instead of six that listed the steps one by one ([a1b616d](https://github.com/alvarogabrielgomez/kanpachi/commit/a1b616d))
- Give the Settings cards the same inside air as the ones in the room, which they had none of: the text and the button touched the border ([a1b616d](https://github.com/alvarogabrielgomez/kanpachi/commit/a1b616d))

## [0.2.1] - 2026-08-13

### Fixed

- Carry on opening the room after choosing a server or typing its password, instead of dropping you back on the home screen with nothing opened and no sign that you had to press Crear again ([7ff6fc1](https://github.com/alvarogabrielgomez/kanpachi/commit/7ff6fc1))
- Check the server answers before saving it, with the button showing it is working: a well-spelled name for a machine that does not exist used to be saved, and the failure surfaced later, while opening a room, with the name long gone from the screen ([7ff6fc1](https://github.com/alvarogabrielgomez/kanpachi/commit/7ff6fc1))
- Stop showing "ese servidor pide una contraseña" as a red notice on top of the screen that is asking for the password, and say "esa contraseña no es la de este servidor" under the field, with the field outlined in red, when it is rejected ([7ff6fc1](https://github.com/alvarogabrielgomez/kanpachi/commit/7ff6fc1))
- Ask for the password when a server wants one to host, which never happened: opening a room on a password-protected server answered "the meeting server does not answer, try again in a moment". It had answered, and what it said was that a password was missing, so retrying was advice that could not work and the password screen was unreachable ([e1bcfa6](https://github.com/alvarogabrielgomez/kanpachi/commit/e1bcfa6))
- Say the real version in the file properties of every Kanpachi executable. v0.2.0 shipped a service that called itself v0.1.9-10-g3f1c599, a portable that called itself v0.1.9-9-gc1da1b5 and a window that called itself 0.1.2+3 ([e1bcfa6](https://github.com/alvarogabrielgomez/kanpachi/commit/e1bcfa6))
- Say the room is being reopened from the first frame when Kanpachi starts with a room left from the previous run, instead of showing the home screen for the half minute it takes and then jumping to the room ([0d69b4d](https://github.com/alvarogabrielgomez/kanpachi/commit/0d69b4d))
- Go back from choosing a server, typing its password and changing your name. All three were entered and only left by completing them ([0ccfc92](https://github.com/alvarogabrielgomez/kanpachi/commit/0ccfc92))
- Show the button working while it works, on those same three screens: saving went quiet for as long as the service took, and a disabled button looks exactly like one that did nothing ([0ccfc92](https://github.com/alvarogabrielgomez/kanpachi/commit/0ccfc92))
- Stop printing raw internal errors where the screen explains what is wrong with what you typed. Choosing a server could answer `DaemonUnreachable(timedOut): el daemon no contestó a own_seed en 10 s`, which reads as if the name were the problem ([0ccfc92](https://github.com/alvarogabrielgomez/kanpachi/commit/0ccfc92))
- See and change the server you open rooms on from Settings, which only knew how to get there by failing: opening a room with none set, or the trust dialog. Settings calls itself "the little there is to decide" and did not show it, while the status bar of that same window said "sin servidor" ([8a76ddb](https://github.com/alvarogabrielgomez/kanpachi/commit/8a76ddb))
- Rename the room from the confirmation dialog the way it is renamed inside the room: the name in plain text with a pencil beside it, instead of a text box competing with the server you are being asked to confirm ([4dbd64e](https://github.com/alvarogabrielgomez/kanpachi/commit/4dbd64e))
- Grow the "what is a seed" explanation instead of making it appear in one frame, with its text fading in, and give the server address its own icon and the full width of the dialog ([4dbd64e](https://github.com/alvarogabrielgomez/kanpachi/commit/4dbd64e))
- Stop "Crear la sala sin juego" from growing by two pixels and shoving the page down when you point at it, and light it up at the same speed as the card below it instead of snapping to lit in one frame ([af22693](https://github.com/alvarogabrielgomez/kanpachi/commit/af22693))
- Keep everything a portable Kanpachi remembers inside the folder it was run from. The single-file portable kept its identity, its room and its server in the temporary folder it unpacks itself into and deletes on exit, so every launch was a brand new machine to anyone who had played with you, and the window settings went to `%APPDATA%`, outside the folder and shared with every other Kanpachi on the PC ([fa39c0d](https://github.com/alvarogabrielgomez/kanpachi/commit/fa39c0d))

### Changed

- Open the window at 940x625 the first time, which is the width the design stops growing at ([4dbd64e](https://github.com/alvarogabrielgomez/kanpachi/commit/4dbd64e))

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

[0.7.1]: https://github.com/alvarogabrielgomez/kanpachi/releases/tag/v0.7.1
[0.7.0]: https://github.com/alvarogabrielgomez/kanpachi/releases/tag/v0.7.0
[0.6.8]: https://github.com/alvarogabrielgomez/kanpachi/releases/tag/v0.6.8
[0.6.7]: https://github.com/alvarogabrielgomez/kanpachi/releases/tag/v0.6.7
[0.6.6]: https://github.com/alvarogabrielgomez/kanpachi/releases/tag/v0.6.6
[0.6.5]: https://github.com/alvarogabrielgomez/kanpachi/releases/tag/v0.6.5
[0.6.4]: https://github.com/alvarogabrielgomez/kanpachi/releases/tag/v0.6.4
[0.6.3]: https://github.com/alvarogabrielgomez/kanpachi/releases/tag/v0.6.3
[0.6.2]: https://github.com/alvarogabrielgomez/kanpachi/releases/tag/v0.6.2
[0.6.1]: https://github.com/alvarogabrielgomez/kanpachi/releases/tag/v0.6.1
[0.6.0]: https://github.com/alvarogabrielgomez/kanpachi/releases/tag/v0.6.0
[0.5.0]: https://github.com/alvarogabrielgomez/kanpachi/releases/tag/v0.5.0
[0.4.0]: https://github.com/alvarogabrielgomez/kanpachi/releases/tag/v0.4.0
[0.3.0]: https://github.com/alvarogabrielgomez/kanpachi/releases/tag/v0.3.0
[0.2.2]: https://github.com/alvarogabrielgomez/kanpachi/releases/tag/v0.2.2
[0.2.1]: https://github.com/alvarogabrielgomez/kanpachi/releases/tag/v0.2.1
[0.2.0]: https://github.com/alvarogabrielgomez/kanpachi/releases/tag/v0.2.0
[0.1.9]: https://github.com/alvarogabrielgomez/kanpachi/releases/tag/v0.1.9
[0.1.8]: https://github.com/alvarogabrielgomez/kanpachi/releases/tag/v0.1.8
[0.1.4]: https://github.com/alvarogabrielgomez/kanpachi/releases/tag/v0.1.4
[0.1.3]: https://github.com/alvarogabrielgomez/kanpachi/releases/tag/v0.1.3
[0.1.2]: https://github.com/alvarogabrielgomez/kanpachi/releases/tag/v0.1.2
[0.1.1]: https://github.com/alvarogabrielgomez/kanpachi/releases/tag/v0.1.1
[0.1.0]: https://github.com/alvarogabrielgomez/kanpachi/releases/tag/v0.1.0
