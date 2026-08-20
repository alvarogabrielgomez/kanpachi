# Kanpachi in a container

Four templates in `templates/`. Copy one, change `KANPACHI_SEED`, `docker compose up -d`.

| Your case | File |
|---|---|
| The game server already runs on this machine | `compose.yml` |
| The game server is another container in the same compose | `compose.sidecar.yml` |
| The game is not in the catalogue and you know its ports | `compose.custom-ports.yml` |
| The registry asks for a password to host on it | `compose.seed-password.yml` |

All but the sidecar run with `network_mode: host`, so the game has to be on the
same machine. On Docker Desktop that machine is the Linux VM and not your
desktop, so a game on Windows or macOS reads as silent there. Use the sidecar.

The invite code and link are printed on **every** start:

```sh
docker compose logs kanpachi
```

Before the first run:

- **The volume is the room.** `/var/lib/kanpachi` holds the identity that pinned
  your invite code. Lose it and the code is gone for good, with nothing on
  screen to say so: guests keep arriving at a host that will never answer, for
  three more weeks.
- **Bind the game to `0.0.0.0` where the image lets you.** Sharing the network
  namespace is not enough: Kanpachi delivers packets to its own address, so a
  server bound to one address of the machine never sees them and answers "port
  unreachable" to the whole room. Zomboid calls it `SERVER_IP`, others `bind` or
  `server-ip`. The room's own address works too, and `kanpachi status` prints it
  as `Your IP` once the room is open. `127.0.0.1` never does: the guest's packet
  carries the room's address, and a redirected copy dies as martian in the
  kernel.
- **Some images rewrite that value, and there the redirect is the only way in.**
  `sknnr/zomboid-dedicated-server` sets `SERVER_IP=$(hostname -i)` in its own
  entrypoint, so the compose changes nothing. In a container Kanpachi covers for
  it: the room's traffic goes to wherever the game listens, and both the room and
  `kanpachi status` name the address. Measured on 2026-08-20 against that image,
  with a guest reaching the server over the relay.
- **Your compose file has to set `cap_add` and `devices`.** An image cannot
  grant itself either one. Without them the container stops at startup and names
  the missing one.

Full guide: [Run a game server with Docker](../docs/public/run-a-game-server-docker.md).

To build the image yourself, run the script. It stages the `.deb` beside the
Dockerfile and removes it afterwards, which `docker build` on its own does not
do.

```sh
# from a package you already have
scripts/build-image.sh --version 0.6.4 --deb dist/kanpachi-amd64.deb

# building the package too, from this tree
scripts/build-image.sh --version 0.6.4 --engine /path/to/kanpachi-engine
```

The package is extracted, never installed: it declares `Depends: systemd`, which
a container has no use for, and its `postinst` does nothing but call
`systemctl`.
