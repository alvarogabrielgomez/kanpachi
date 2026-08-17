# Install on Linux

Ubuntu 22.04 or newer, amd64. There is no window here: the client is a command,
`kanpachi`, and typing it with no arguments opens an assistant that walks
through hosting or joining without having read anything.

## One line

```sh
curl -fsSL -o /tmp/kanpachi.deb https://github.com/alvarogabrielgomez/kanpachi/releases/latest/download/kanpachi-amd64.deb && sudo apt install -y /tmp/kanpachi.deb
```

That is the whole install. Both services start and are enabled at boot.

### Why `apt install /path` and not `apt install kanpachi`

Kanpachi is not in Ubuntu's repositories, and with a plain `.deb` it will not
be. The path is what makes the difference, and getting it wrong is the one
mistake worth naming:

```sh
apt install kanpachi-amd64.deb     # E: Unable to locate package — apt searched the repos
apt install /tmp/kanpachi.deb      # installs it — apt saw a path, not a name
```

`apt` and not `dpkg -i`, because `dpkg` stops when a dependency is missing and
leaves the package half configured. `apt` pulls `nftables`, `libc6` and
`systemd` from the official repositories first.

There is no APT repository of ours to add, and that is a deliberate choice: an
APT signing key is the key that pushes code **as root** to every machine that
trusts it, and holding one is a permanent responsibility rather than an
afternoon of work.

## Verifying the download

Every release publishes `SHA256SUMS-linux`:

```sh
curl -fsSL -O https://github.com/alvarogabrielgomez/kanpachi/releases/latest/download/SHA256SUMS-linux
sha256sum -c SHA256SUMS-linux --ignore-missing
```

This catches a truncated or tampered **download**, not a bad release: the sums
file lives in the same release as the package. What protects the release itself
is that everything here is public and rebuildable — see
[build and test from source](build-from-source.md).

## The two services

```sh
systemctl status kanpachid kanpachi-quarantine
```

- **`kanpachid.service`** — the daemon. `Type=notify`, so `systemctl start` does
  not return until the control socket is listening. This is what holds the room
  and writes the rules.
- **`kanpachi-quarantine.service`** — **the one that matters when Kanpachi is
  off.** It loads before the network comes up and keeps file sharing, remote
  desktop, remote management and device discovery closed from the internet with
  nothing of ours running. Stopping it does not lift the quarantine. Only
  uninstalling does.

To see the quarantine as the kernel sees it:

```sh
sudo nft list table inet kanpachi-base
```

The full list of paths, units and state is in
[what gets installed, and where](reference-files.md).

## First run

```sh
kanpachi
```

With no arguments you get the assistant. It asks for a nickname the first time,
then offers to open a room or enter one, and it is the sensible starting point
on a server that was installed a minute ago.

Everything the assistant does has a direct subcommand as well — `kanpachi host`,
`kanpachi join <code>`, `kanpachi status`. The whole list is in
[every command](reference-cli.md).

Two worth knowing on day one:

```sh
kanpachi doctor      # what this needs to work, and what is broken. --fix repairs what it can
kanpachi exposure    # what Kanpachi has open, and toward whom
```

`doctor` is the one that catches the common server problem: `ufw` active and
not letting the engine's port in, which fails in a way that looks exactly like
somebody else's home network being at fault.

## Uninstalling

```sh
sudo apt remove kanpachi    # removes the program and the quarantine, keeps your identity
sudo apt purge kanpachi     # also deletes /var/lib/kanpachi and /etc/kanpachi
```

The difference is the identity key. `remove` keeps it, so reinstalling leaves
this machine as the same machine for everyone who already played with it;
`purge` deletes it.

Either way the quarantine goes. A firewall rule that outlives the program that
put it there is the hardest kind of problem to diagnose.

## Building the package yourself

Both steps run **on** Linux, and there is no cross-compile in either direction:

```sh
# in the engine repository
scripts/build-linux.sh
# in this one
scripts/build-deb.sh --version 0.2.0 --engine ~/.cache/kanpachi-engine-target/release/kanpachi-engine
```

That is not an omission. On Linux the engine pulls in a vendored `dbus`,
`zstd-sys` and `kcp-sys` through bindgen, which is three C toolchains that would
need a Linux linker and sysroot mounted by hand anywhere else. The scripts name
what is missing instead of letting the compiler guess. More in
[build and test from source](build-from-source.md).

## Next

- [Run a 24/7 game server](run-a-game-server.md) — the case this build exists
  for: a room that reopens itself after a reboot, with the same invite code.
- [Every command](reference-cli.md).
- [Host your own seed](host-a-seed.md) — a different program, `kanpseed`, on a
  different machine.
