# Fork it

Publishing your own build under your own name is two files. Everything that says
*who published this binary* lives in one place per language, and a fork edits
those and nothing else.

The licence makes this a right rather than a favour: Kanpachi is AGPL-3.0, and
§13 is what obliges anyone running a **modified** meeting server for other people
to hand them its source.

## The two files

| Language | File | What it holds |
|---|---|---|
| Go | [`internal/brand/brand.go`](../../internal/brand/brand.go) | `Repo`, `UpdatesEnabled`, `Docs` |
| Dart | [`ui/lib/core/brand.dart`](../../ui/lib/core/brand.dart) | the same values, mirrored |

`Repo` is the **update channel**. Three URLs hang off it: what version exists,
where a tag's artifacts live, and where a person goes to download. Changing
that one line moves all three.

`UpdatesEnabled = false` turns the version check off entirely, in both faces.
That switch exists because the alternative, pointing `Repo` at a repository that
does not publish, does not disable the check. It turns it into a 404 every time
somebody asks, which is a screen saying something is wrong when nothing is: this
fork publishes elsewhere.

`Docs` is the repository and not a domain, on purpose: a domain belongs to
whoever is paying a DNS bill, and the repository is where the binary came from.

## Nothing else may spell the repository out

Two tests enforce it. [`internal/arch/marca_test.go`](../../internal/arch/marca_test.go)
fails the build if the name reappears anywhere in the tree, and a second one
keeps the Go and Dart values in lockstep. The failure that second test prevents
is a fork that edits the Go constant and keeps shipping a window pointing at the
original.

The other faces get the value rather than carrying it:

- The invitation page receives it from the server in its SSR state.
- The systemd units import the Go constant.
- The Inno Setup installer takes it as a `/DRepo=` parameter.
- `seed/install.sh` is the one exception, because it is fetched over a URL that
  already contains the repository name.

The Rust engine has no branding file at all. It carries no such constant: its
only two URLs are in `Cargo.toml`, where `repository` is the canonical Rust place
for one and points at a different repository anyway.

## What must never move into these files

Anything the two machines in a room compute independently.

The Argon2id parameters ([`core/domain/identity.go`](../../core/domain/identity.go)),
the invite ID alphabet, and the pinned EasyTier version
([`registry/setup`](../../registry/setup/)) look like configuration and are not.
They are frozen, with golden vectors in the tests, because both sides derive
them separately and without talking to each other.

A fork that "configures" them produces rooms where people paste the same code and
end up alone, with nothing on screen pointing at the cause. That is the worst
failure this project can produce: silent, symmetric, and indistinguishable from
a network problem.

The rule is one line: **this is about who publishes, never about the protocol.**

## Also worth changing

Not required, and obvious once building:

- `logos/` and the icons compiled into the Windows resources.
- `installer/aviso-de-licencias.txt`, the notice shown before installing. It is
  a real LGPL obligation, so change the name in it rather than removing it.
- The seed you point clients at by default, which is a client-side setting
  (`kanpachi seed <host>`) and not a compiled constant.

## Then build it

See [build and test from source](build-from-source.md). Nothing about a fork
changes the build, other than passing your own `/DRepo=` to Inno Setup.

## Next

- [Architecture](architecture.md) — why this is three repositories, and what
  each one is allowed to decide.
- [Build and test from source](build-from-source.md).
