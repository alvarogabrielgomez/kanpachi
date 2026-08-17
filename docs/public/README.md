# Kanpachi documentation

This is the public documentation, in English. Four kinds of document, and the
split is about what you are doing when you open one, not about the subject.

Kanpachi's **design** documents are a different thing and live one directory up,
in [`docs/`](../). They are in Spanish, they are the source of truth for why
each decision was taken, and they are written for whoever is changing the code.

---

## Tutorial — learning by doing

One path, from nothing to a game running with a friend. It makes every choice
for you on purpose.

- **[Your first room](tutorial-first-room.md)**

## How-to guides — solving one problem

Each of these assumes you already know what Kanpachi is and want to get
something done.

| Guide | For when you want to |
|---|---|
| [Install on Windows](install-windows.md) | put Kanpachi on a gaming PC, or run it without installing |
| [Install on Linux](install-linux.md) | put Kanpachi on a Linux box, check it, or remove it |
| [Run a 24/7 game server](run-a-game-server.md) | host a room on a VPS nobody is sitting at |
| [Host your own seed](host-a-seed.md) | run the meeting point yourself, with a domain and an optional password |
| [Build and test from source](build-from-source.md) | compile the installer, the `.deb`, the seed, or run the checks CI runs |
| [Fork it](fork-the-branding.md) | publish your own build under your own name |

## Reference — looking something up

Dry, complete, organised by the shape of the thing being described.

| Reference | What it lists |
|---|---|
| [Every command](reference-cli.md) | `kanpachi` and `kanpseed`, subcommand by subcommand |
| [What gets installed, and where](reference-files.md) | paths, services, sockets and state, on both systems |
| [The seed's HTTP API](../../registry/API.md) | every endpoint, its rate limit, and what defends it |
| [Changelog](../../CHANGELOG.md) | what changed in each release |

## Explanation — understanding how it works

Read these when the question is *why*, and no task is pending.

| Document | What it explains |
|---|---|
| [Kanpachi Protection](../../kanpachi-protection.md) | the promise, the four things that keep it, and where it stops |
| [The seed](../../kanpachi-seed.md) | what a meeting point sees, stores, and can never learn |
| [Architecture](architecture.md) | three repositories, three processes, and why the split is the security model |

Two more live in the repositories they describe:

- [`kanpachi-engine`](https://github.com/alvarogabrielgomez/kanpachi-engine) —
  what the engine is, and why it listens on nothing.
- [`EasyTier/FORK.md`](https://github.com/alvarogabrielgomez/EasyTier/blob/kanpachi/FORK.md) —
  what the fork changes against upstream, hunk by hunk, and why a fork was the
  only way to get it.

---

## A note on how these are kept

A document here describes behaviour that exists. When behaviour changes, the
document changes in the same commit, which is the rule the whole project runs
on — see [`docs/CLAUDE.md`](../CLAUDE.md).

These files are public and are linked from outside this repository, so paths
matter. [`kanpachi-protection.md`](../../kanpachi-protection.md) and
[`kanpachi-seed.md`](../../kanpachi-seed.md) stay at the repository root for
that reason: the engine's README and the fork's `FORK.md` link to them by URL,
and moving them would break two other repositories.
