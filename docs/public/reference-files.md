# What gets installed, and where

Three installs, three layouts. Nothing here is configurable: the project's rule
is that if something can be decided once, it is not asked.

---

## Windows

### Files

| Path | What |
|---|---|
| `C:\Program Files\Kanpachi\kanpachid.exe` | the daemon, which runs as the service |
| `C:\Program Files\Kanpachi\kanpachiui.exe` | the window |
| `C:\Program Files\Kanpachi\` | plus the Flutter bundle, the engine, its DLLs and the factory catalogue |
| `C:\ProgramData\Kanpachi\` | state: identity key, API token, the room, the name you are seen by |

`ProgramData\Kanpachi` is created **by the installer**, with its own ACL, and the
daemon refuses to start if it is missing rather than creating it. That ACL is
half the protection of everything inside, and creating the directory by accident
would lose it silently.

The ACL grants read to the machine's users on purpose, because the window has to
read files there, `api.token` among them, without elevating. That is why the
Windows CLI needs no elevation to talk over the channel, unlike the Linux one.
`identity.key` carries an ACL of its own.

### The service

| | |
|---|---|
| Name | `kanpachi-daemon` |
| Account | `LocalSystem` |
| Start | `delayed-auto` |
| On failure | restart after 5s, 10s, 30s; counter resets daily |

```powershell
sc.exe query kanpachi-daemon
```

The installer also grants your user permission to start and stop it, which is
what keeps the product to a single UAC prompt in its lifetime.

### Control channels

| Pipe | Used by |
|---|---|
| `\\.\pipe\ProtectedPrefix\Administrators\kanpachi-installed` | the installed product |
| `\\.\pipe\ProtectedPrefix\Administrators\kanpachi-portable` | the portable build |
| `\\.\pipe\ProtectedPrefix\Administrators\kanpachi-console` | a daemon run with `--console`, for development |

`ProtectedPrefix\Administrators\` means only an administrator can create the
pipe, so a non-elevated process cannot squat the name and impersonate the
daemon.

### Registry

`HKLM\SOFTWARE\Classes\kanpachi` registers the `kanpachi:` URL handler. A link
clicked in a browser reaches the daemon, which stores it and opens the window;
the window picks it up and asks for confirmation. Nothing arriving from outside
enters a room without somebody inside confirming it.

### The portable build

`kanpachi-portable.exe` keeps everything next to itself, in a `kanpachi-data\`
folder, and uses the portable pipe. There is no ACL, no service, and no registry
entry. That is why the CLI accepts `--data`: nothing here can guess where
somebody unzipped it.

---

## Linux

### Files

| Path | What |
|---|---|
| `/usr/bin/kanpachi` | the command line client |
| `/usr/libexec/kanpachi/kanpachid` | the daemon |
| `/usr/libexec/kanpachi/kanpachi-engine` | the network engine |
| `/usr/share/kanpachi/builtin.json` | the game catalogue shipped with the package |
| `/usr/share/doc/kanpachi/` | copyright, third-party notices, and the two GNU texts |
| `/etc/kanpachi/quarantine.nft` | the base quarantine, written by the daemon |
| `/var/lib/kanpachi/` | state: identity key, API token, the room, the name you are seen by. Mode `0700` |
| `/run/kanpachi/api.sock` | the control socket, in a `0700` root-owned directory |

The catalogue in `/usr/share` is the package's; a user's own lives separately in
`/var/lib`, so a package update never overwrites it.

### The two units

**`kanpachid.service`** — the daemon.

`Type=notify`, so `systemctl start` does not return until the control socket is
listening. Without that, a `kanpachi host` typed immediately afterwards would
find a closed door and report "no service" while the service was starting
correctly.

It runs as `root`, and that is a decision rather than a convenience:
`CAP_NET_ADMIN` (what it needs for nftables and `/dev/net/tun`) already allows
reconfiguring the machine's entire network, so a dedicated user would buy little
isolation while forcing every CLI call through `sudo -u kanpachi`. The hardening
is what bounds the rest:

| Directive | Why |
|---|---|
| `AmbientCapabilities=CAP_NET_ADMIN`, `CapabilityBoundingSet=CAP_NET_ADMIN` | one capability, and no others |
| `ProtectSystem=strict` with a narrow `ReadWritePaths` | `/etc/kanpachi`, `/var/lib/kanpachi`, and what `ufw` writes |
| `ProtectHome=read-only` | `yes` would make Steam detection see empty directories and answer "no Steam", which is a credible false answer |
| `RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6 AF_NETLINK` | the four it uses, and no more |
| `KillMode=control-group` | the engine dies with the daemon even if the daemon cannot run a single deferred call |
| `TimeoutStopSec=45` | shutting down closes the room, purges the rules and waits for the engine. Cutting that short leaves rules in place |

It orders itself `After=kanpachi-quarantine.service`, because the moment of
least protection is between purging the old rules and writing the new ones, and
that moment has to happen with the quarantine already up.

**`kanpachi-quarantine.service`** — the important one, and the one nobody sees.

It keeps the game ports closed from the internet **while Kanpachi is stopped**.
Without it, turning Kanpachi off would leave the machine as it was before
installing, and this product promises the opposite: that the protection does not
depend on a process being alive.

`DefaultDependencies=no` plus `Before=network-pre.target` is what places it
before the network exists. With default dependencies systemd would start it
after `basic.target`, leaving a window with the ports open to the world.

`Type=oneshot` with `RemainAfterExit=yes`, because the state that matters is the
kernel's and not the process's: `nft -f` finishes and the rules stay.

**There is no `ExecStop`, deliberately.** Stopping this unit cannot lift the
quarantine. Its value is that it stays with everything else off, so a
`systemctl stop` that removed it would turn a routine command into opening the
game ports to the internet without anybody asking. Removing it belongs to the
uninstaller, and the uninstaller does not go through here.

```sh
systemctl status kanpachid kanpachi-quarantine
sudo nft list table inet kanpachi-base
```

---

## The seed

| Path | What |
|---|---|
| `/usr/local/bin/kanpseed` | the binary: CLI and registry in one |
| `/usr/local/lib/kanpachi/` | EasyTier `v2.6.4` and `index.html`, the invitation page |
| `/etc/kanpachi/seed.json` | chosen ports and domain. The single source of truth |
| `/var/lib/kanpseed/` | state, including the operator credential |
| `/var/lib/private/kanpseed/` | where `DynamicUser=yes` puts it; the above is a symlink |
| `/etc/systemd/system/kanpseed-engine.service` | EasyTier as a public node, `--no-tun` |
| `/etc/systemd/system/kanpseed-registry.service` | the registry and the page |

### Ports

| Port | Protocol | Exposure |
|---|---|---|
| `11010` | TCP **and** UDP | public. Moves only if taken |
| `8010` | TCP | loopback. First free from `8010`; your reverse proxy points here |
| `15888` | TCP | loopback only. The engine's control RPC, never leaves the machine |

`11010` is the one clients carry compiled in, so moving it means releasing a
client. The internal one moves freely, because only the machine's own reverse
proxy knows it.

The invitation page and the API share one origin and one port, so there is no
CORS to configure and no second thing to expose.

The engine runs with `--no-tun`, so the seed has **no virtual interface of its
own**. It is not a member of anybody's room.

---

## See also

- [Every command](reference-cli.md)
- [Install on Windows](install-windows.md) · [Install on Linux](install-linux.md) · [Host your own seed](host-a-seed.md)
- [Kanpachi Protection](../../kanpachi-protection.md) — what the quarantine
  blocks, and why absence of a rule was not enough for those ports.
