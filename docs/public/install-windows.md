# Install on Windows

Windows 10 or 11, 64-bit. The daemon uses APIs that do not exist under WOW64, so
there is no 32-bit build and there will not be one.

## The installer

Download `kanpachi-setup.exe` from the
[releases page](https://github.com/alvarogabrielgomez/kanpachi/releases/latest),
or open an invite link and let the invitation page hand you the right file.

Run it. Windows asks for administrator **once**, and that prompt is the only one
in the life of the product. Everything needing elevation happens there: the
service, the `ProgramData` directory with its ACL, and the permission for your
user to start the service without elevating. Playing never asks again.

Before installing, the wizard shows a licence notice rather than an
accept/decline gate. That is deliberate: the AGPL and the LGPL grant permissions
and take none away, neither requires accepting anything to *use* the program,
and a fake acceptance screen teaches people that these screens are paperwork.
The notice is a real obligation, because shipping the engine ships LGPL code.

### What it leaves behind

A Windows service called `kanpachi-daemon`, running as `LocalSystem` with
delayed automatic start, which is what holds the room and writes the firewall
rules. A window program that talks to it over a named pipe. A `kanpachi:` URL
handler, so an invite link in a browser reaches the daemon.

The full list of paths, services and state is in
[what gets installed, and where](reference-files.md).

**The window is a remote control, not the program.** Closing it leaves the room
open and the ports as they were. What ends a room is closing the room.

## Verifying the download

Every release publishes `SHA256SUMS-windows`, covering both the installer and
the portable build:

```powershell
curl.exe -fsSL -O https://github.com/alvarogabrielgomez/kanpachi/releases/latest/download/SHA256SUMS-windows
Get-FileHash .\kanpachi-setup.exe -Algorithm SHA256
```

Compare the two by eye, or on a machine with `sha256sum` available run
`sha256sum -c SHA256SUMS-windows --ignore-missing`.

This buys one thing: it catches a truncated or tampered **download**, not a bad
release, since the sums file lives in the same release as the binary. What
protects the release itself is that everything here is public and reproducible
from source. See [build and test from source](build-from-source.md).

## Without installing: the portable build

`kanpachi-portable.exe`, in the same release, is the whole product in one
executable: daemon, window, engine, DLLs and catalogue. It unpacks itself into a
temporary folder, runs, and deletes it on the way out. Nothing to install and
nothing to uninstall.

It is the right answer for a machine you do not own, a quick test, or a friend
who will not run an installer. It costs three things, and they are worth naming:

- **One UAC prompt per start.** The installed version asks once and, in
  exchange, grants your user permission to start the service. A file that was
  copied granted nothing, so it elevates every time.
- **Its data inherits the permissions of wherever it runs.** The installed
  version puts its own ACL on `ProgramData`; here there is no installer to put
  one.
- **It does not start with Windows,** because there is no registered service for
  Windows to bring up.

The two builds keep their state in different places and listen on different
pipes, so they do not collide. Running both at once is not useful, since only
one of them can hold a room.

## Uninstalling

Settings → Apps → Kanpachi → Uninstall, or the entry in Add/Remove Programs.

The order it runs in is the opposite of the intuitive one, and it matters: the
uninstaller stops the service, then runs the daemon once more to clean up
**while its files are still on disk**, and only then deletes the service. The
other way around there would be no binary left to do the cleaning, and the
machine would keep the base quarantine in place: ports `445` and `3389`
blocked, with no Kanpachi installed and nothing to blame.

`ProgramData\Kanpachi` goes with it, identity key included. A firewall rule that
outlives the program that wrote it is the hardest kind of problem to diagnose,
so Kanpachi leaves nothing behind on purpose.

## If something is wrong

The window says what is broken in a sentence you can act on rather than a code.
Beyond that:

```powershell
sc.exe query kanpachi-daemon
```

and, inside the app, the exposure screen shows what Kanpachi has open and toward
whom. The equivalent on the command line, on Linux, is `kanpachi exposure`.

## Next

- [Your first room](tutorial-first-room-windows.md), if you have not opened one yet.
- [What gets installed, and where](reference-files.md).
- [Kanpachi Protection](../../kanpachi-protection.md): what stays closed, and
  what the promise does not cover.
