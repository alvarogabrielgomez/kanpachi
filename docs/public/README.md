# Kanpachi documentation

This is the public documentation, in English. The sections go by what you are
doing when you open a page, not by subject.

Kanpachi's **design** documents are a different thing and live one directory up,
in [`docs/`](../). They are in Spanish, they are the source of truth for why
each decision was taken, and they are written for whoever is changing the code.

---

## Start here

One path, from nothing to a game running with a friend. It makes every choice
for you on purpose.

- **[Your first room](tutorial-first-room.md)**, which sends you to the one for
  the machine you host from: [Windows](tutorial-first-room-windows.md) or
  [Linux](tutorial-first-room-linux.md).

## Getting one thing done

These assume you already know what Kanpachi is.

| Guide | For when you want to |
|---|---|
| [Install on Windows](install-windows.md) | put Kanpachi on a gaming PC, or run it without installing |
| [Install on Linux](install-linux.md) | put Kanpachi on a Linux box, check it, or remove it |
| [Run a 24/7 game server](run-a-game-server.md) | host a room on a VPS nobody is sitting at |
| [Run a game server with Docker](run-a-game-server-docker.md) | the same, as a container, with the code surviving a rebuild |
| [Run a game server on Kubernetes](run-a-game-server-kubernetes.md) | the same, as a sidecar beside the game, and what a cluster does differently |
| [Host your own seed](host-a-seed.md) | run the meeting point yourself, with a domain and an optional password |
| [Build and test from source](build-from-source.md) | compile the installer, the `.deb`, the seed, or run the checks CI runs |
| [Fork it](fork-the-branding.md) | publish your own build under your own name |

## Looking something up

Dry and complete, organised by the shape of the thing described.

| Reference | What it lists |
|---|---|
| [Every command](reference-cli.md) | `kanpachi` and `kanpseed`, subcommand by subcommand |
| [What gets installed, and where](reference-files.md) | paths, services, sockets and state, on both systems |
| [The seed's HTTP API](../../registry/API.md) | every endpoint, its rate limit, and what defends it |
| [Changelog](../../CHANGELOG.md) | what changed in each release |

## Why it works the way it does

Read these with no task pending.

| Document | What it explains |
|---|---|
| [Kanpachi Protection](../../kanpachi-protection.md) | the promise, the four things that keep it, and where it stops |
| [The seed](../../kanpachi-seed.md) | what a meeting point sees, stores, and can never learn |
| [Architecture](architecture.md) | three repositories, three processes, and why the split is the security model |

Two more live in the repositories they describe:

- [`kanpachi-engine`](https://github.com/alvarogabrielgomez/kanpachi-engine):
  what the engine is, and why it listens on nothing.
- [`EasyTier/FORK.md`](https://github.com/alvarogabrielgomez/EasyTier/blob/kanpachi/FORK.md):
  what the fork changes against upstream, hunk by hunk, and why a fork was the
  only way to get it.

---

## A note on how these are kept

A document here describes behaviour that exists. When behaviour changes, the
document changes in the same commit, which is the rule the whole project runs
on. See [`docs/CLAUDE.md`](../CLAUDE.md).

These files are public and are linked from outside this repository, so paths
matter. [`kanpachi-protection.md`](../../kanpachi-protection.md) and
[`kanpachi-seed.md`](../../kanpachi-seed.md) stay at the repository root for
that reason: the engine's README and the fork's `FORK.md` link to them by URL,
and moving them would break two other repositories.
