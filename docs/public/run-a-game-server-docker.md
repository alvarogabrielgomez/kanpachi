# Run a game server with Docker

`docker compose up -d`, and a room opens with a code you paste into the group
chat once. Destroy the container, rebuild it, and **the same code comes back**.
The code lives in the volume, and rebuilding never touches it.

If the machine is not running Docker, [the plain guide](run-a-game-server.md) is
shorter and the result is the same.

## Before anything

Your compose file has to grant two things, because a container cannot grant them
to itself:

- **`cap_add: [NET_ADMIN]`**, because Kanpachi builds a virtual network adapter
  and writes nftables rules.
- **`devices: ["/dev/net/tun:/dev/net/tun"]`**, because it builds that adapter
  on the TUN device.

If either is missing the container stops at startup and says which. If you
mapped the device and it still fails, the host kernel has no `tun` module. Run
`modprobe tun` on the machine, not in the container.

Nothing else needs configuring first. Kanpachi's rooms live in `100.64.0.0/10`
and `10.99.0.0/16` and its lobbies in `198.19.0.0/16`, none of which Docker's
default networks touch.

## The quick version

```sh
curl -fsSLO https://raw.githubusercontent.com/alvarogabrielgomez/kanpachi/main/docker/templates/compose.yml
# edit KANPACHI_SEED, and KANPACHI_GAME if it is not Zomboid
docker compose up -d
docker compose logs kanpachi
```

The logs end with the invite:

```
  ------------------------------------------------------------
  Room   Zomboid nights
  Code   A7K2-M9QX@kanpachi.accentio.dev
  Link   https://kanpachi.accentio.dev/A7K2-M9QX#...
  ------------------------------------------------------------
```

Hand out either one. It is printed on **every** start, including the ones where
nothing changed, because on an unattended server this is the only place anybody
can go looking for it.

## Which template

Four of them, in [`docker/templates/`](../../docker/templates/). Each is whole:
copy one, change `KANPACHI_SEED`, run it.

| Your case | File |
|---|---|
| The game server already runs on this machine | `compose.yml` |
| The game server is another container in the same compose | `compose.sidecar.yml` |
| The game is not in the catalogue and you know its ports | `compose.custom-ports.yml` |
| The registry asks for a password to host on it | `compose.seed-password.yml` |

The first, third and fourth run with `network_mode: host`, so they need the game on the
same machine as the container. On Docker Desktop that machine is the Linux VM: see
[Two ways to place the network](#two-ways-to-place-the-network).

## Settings

Only `KANPACHI_SEED` is required.

| Variable | Default | What it does |
|---|---|---|
| `KANPACHI_SEED` | — | the registry this machine opens its room on. A host name: no `https://`, no trailing slash |
| `KANPACHI_ROOM_NAME` | the hostname | the room's name, and the only name your guests read |
| `KANPACHI_GAME` | none | a game id from [the catalogue](reference-cli.md#game-id) |
| `KANPACHI_PORTS_TCP` | none | ports of your own, comma separated. Excludes `KANPACHI_GAME` |
| `KANPACHI_PORTS_UDP` | none | the same, for UDP |
| `KANPACHI_GAME_NAME` | `Custom game` | names that profile on this machine. Needs the ports above |
| `KANPACHI_SEED_PASSWORD_FILE` | none | a file holding the hosting password |
| `KANPACHI_SEED_PASSWORD` | none | the same password, in the environment. Simpler and worse |
| `KANPACHI_QUARANTINE` | `false` | `true` closes this machine's risky server ports on every network |

### Ports of your own

Single ports or ranges, comma separated, at most eight entries across the two
lines:

```yaml
KANPACHI_PORTS_TCP: "25565"
KANPACHI_PORTS_UDP: "25565,19132-19133"
```

A port needed on both protocols goes in both lines and spends two of the eight.
Twelve ports are refused outright, and the check is by containment: a range of
`440-450` is rejected for covering 445.

Your guests are told the game's **id**, never its ports, so anybody without that
entry in their own catalogue sees `custom` and opens nothing. The name they read
is the room's, `KANPACHI_ROOM_NAME`.

### The password

The two variables do the same thing and the file is the better one: the
environment shows up whole in `docker inspect` and in `/proc/<pid>/environ`.
Either way the entrypoint hands it over on standard input, which is the only way
Kanpachi accepts it.

## The volume is the room

`/var/lib/kanpachi` holds the identity that pinned your invite code, the saved
room, your game profiles and the log. **Losing it loses the code for good**, and
the failure is silent: a new identity is generated without an error, and guests
keep arriving at a host that will never answer until the registry sweeps the
room three weeks later.

So: `docker compose down` and `up` are safe, rebuilding the image is safe, and
`docker compose down -v` is the one that throws the room away.

The daemon's detailed log lives in there too:

```sh
docker compose exec kanpachi tail -f /var/lib/kanpachi/logs/kanpachi.log
```

## Two ways to place the network

**`network_mode: host`** is the common one, for a game server already running on
the machine. Kanpachi shares the host's network, which is where its rules do
their work.

On Docker Desktop that machine is the Linux VM, not your Windows or macOS
desktop. The container sees the VM's interfaces and the VM's sockets, so a game
running on your desktop stays invisible: the room opens and the game reads as
silent. Use the sidecar there.

**Sidecar** puts the game in a container of its own with
`network_mode: "service:kanpachi"`, so the two share one network namespace and
the game is reachable through the room and nowhere else. A service set up that
way cannot declare `ports:` or `networks:` of its own, and does not need to:
the room exists to make it reachable.

Publishing a port to the host is the thing Kanpachi exists to avoid, so none of
the templates does it.

## Point the game at the room

Set the game's bind address to one of these:

| Value | When |
|---|---|
| `0.0.0.0` | the usual choice. It covers every address on the machine, and you can set it before the room exists |
| the room's own address | tighter. `kanpachi status` prints it as `Your IP`, and it survives restarts because the room keeps its range in the volume |

Do not use `127.0.0.1`. The guest's packet carries the room's address, so a
socket on loopback never receives it, and Kanpachi refuses to redirect there.

Zomboid calls the setting `SERVER_IP`, others `bind` or `server-ip`. Some images
overwrite it: `sknnr/zomboid-dedicated-server` sets `SERVER_IP=$(hostname -i)` in
its own entrypoint. In a container Kanpachi covers for that and sends the room's
traffic to wherever the game listens, naming the address in the room and in
`kanpachi status`.

## Choosing a version

Every release publishes two tags: the number, and `latest`.

```yaml
image: ghcr.io/alvarogabrielgomez/kanpachi:latest   # follows every release
image: ghcr.io/alvarogabrielgomez/kanpachi:0.6.4    # stays put until you change it
```

Partial tags are not published, so `:0.6` resolves to nothing. Pin the whole
number or follow `latest`.

## Upgrading

Rebuild:

```sh
docker compose pull && docker compose up -d
```

Pulling a version your room was not opened with is fine: the room lives in the
volume and is reopened by whatever binary starts next.

`kanpachi upgrade` does not work inside a container and reports success anyway:
it installs the package and then tries to restart a systemd service that is not
there, which leaves the new files on disk and the old daemon running. Do not use
it here.

## When something is wrong

```sh
docker compose exec kanpachi kanpachi doctor
docker compose exec kanpachi kanpachi status
docker compose exec kanpachi kanpachi exposure
```

`doctor` checks the TUN device, the kernel, the control channel and the engine,
and it names a foreign firewall without touching it. It skips capabilities, so
the entrypoint checks those itself before starting the daemon.

Two limits:

- **Kanpachi Protection's promise does not reach a container.** On a normal
  install the containment stays up while Kanpachi is off, because a systemd unit
  loads its nftables table at boot. A container whose only process is the daemon
  has no boot of its own to load it, so `KANPACHI_QUARANTINE` starts off here.
- **If the virtual adapter fails and stays down for ten minutes**, or the engine
  dies eight times in a row, the host closes the room and the code changes on
  the next start. Losing the network does not do this. Losing the adapter does,
  and inside a container that means `/dev/net/tun` going away underneath it.

## See also

- [Run a 24/7 game server](run-a-game-server.md) without Docker.
- [Every command](reference-cli.md), including `profile` and the game ids.
- [Kanpachi Protection](../../kanpachi-protection.md): the promise and where it
  stops.
