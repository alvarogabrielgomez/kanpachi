# Changelog

What changed in each release of Kanpachi, for the person using it.

The format is [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and versions follow [SemVer](https://semver.org/). How it is kept, and why it is kept that way, is in [docs/CLAUDE.md](docs/CLAUDE.md): **one line per entry, imperative mood, linking its own commit**, written in the same commit as the change.

This file is in English, like commit messages and release notes, because a release body quotes it verbatim. The design reasoning lives in `docs/02-decisiones-de-diseno.md`, in Spanish, and the mechanical detail lives in the commit each entry links.

## Unreleased

### Changed

- Name the version on the download button, asked once an hour by the seed instead of by every visitor's browser ([777a3e7](https://github.com/alvarogabrielgomez/kanpachi/commit/777a3e7))

### Fixed

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

[0.1.4]: https://github.com/alvarogabrielgomez/kanpachi/releases/tag/v0.1.4
[0.1.3]: https://github.com/alvarogabrielgomez/kanpachi/releases/tag/v0.1.3
[0.1.2]: https://github.com/alvarogabrielgomez/kanpachi/releases/tag/v0.1.2
[0.1.1]: https://github.com/alvarogabrielgomez/kanpachi/releases/tag/v0.1.1
[0.1.0]: https://github.com/alvarogabrielgomez/kanpachi/releases/tag/v0.1.0
