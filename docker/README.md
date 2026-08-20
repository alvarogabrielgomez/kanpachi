# Kanpachi in a container

Four templates in `templates/`. Copy one, change `KANPACHI_SEED`, `docker compose up -d`.

| Your case | File |
|---|---|
| The game server already runs on this machine | `compose.yml` |
| The game server is another container in the same compose | `compose.sidecar.yml` |
| The game is not in the catalogue and you know its ports | `compose.custom-ports.yml` |
| The registry asks for a password to host on it | `compose.seed-password.yml` |

The invite code and link are printed on **every** start:

```sh
docker compose logs kanpachi
```

Before the first run:

- **The volume is the room.** `/var/lib/kanpachi` holds the identity that pinned
  your invite code. Lose it and the code is gone for good, with nothing on
  screen to say so: guests keep arriving at a host that will never answer, for
  three more weeks.
- **The game has to listen on `0.0.0.0`.** Sharing the network namespace is not
  enough: Kanpachi delivers packets to its own address, so a server bound to the
  container's own IP never sees them and answers "port unreachable" to the whole
  room. Zomboid calls it `SERVER_IP`, others `bind` or `server-ip`. Measured
  against a Kubernetes deployment feeding the server `status.podIP`: perfect
  tunnel, open ports, and not one packet reaching the game.
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
