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

Two things worth knowing before the first run:

- **The volume is the room.** `/var/lib/kanpachi` holds the identity that pinned
  the invite code. Lose it and the code is gone for good, and the failure is
  silent: guests keep arriving at a host that will never answer, for three more
  weeks.
- **`cap_add` and `devices` are not optional.** An image cannot grant itself
  either one, so they live in the compose file. Without them the container stops
  at startup and says so.

Full guide: [Run a game server with Docker](../docs/public/run-a-game-server-docker.md).

Building the image yourself goes through the script, never `docker build` on its
own: the `.deb` has to be staged next to the Dockerfile first, and the script is
what puts it there and takes it away again.

```sh
# from a package you already have
scripts/build-image.sh --version 0.6.4 --deb dist/kanpachi-amd64.deb

# building the package too, from this tree
scripts/build-image.sh --version 0.6.4 --engine /path/to/kanpachi-engine
```

The package is extracted, never installed: it declares `Depends: systemd`, which
a container has no use for, and its `postinst` does nothing but call
`systemctl`.
