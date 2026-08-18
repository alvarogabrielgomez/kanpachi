# Build and test from source

Everything Kanpachi ships is buildable from public source. This page is the
recipe, and the important rule about it is that **the recipe lives in scripts,
not in this document and not in the CI YAML**. A runner and a person call the
same file, which is what keeps the two from drifting.

## What you need

| For | Tool |
|---|---|
| The daemon, the CLI, the seed | Go, the version in [`go.mod`](../../go.mod) (`1.25.3`) |
| The Windows window | Flutter `3.44.1`, stable |
| The engine | Rust, the version in the engine repo's `rust-toolchain.toml` |
| The Windows installer | Inno Setup 6 (`ISCC.exe`) |
| The `.deb` | `dpkg-deb`, on Linux |

## Running the checks

One script, one surface per CI job:

```powershell
.\scripts\verify.ps1                      # everything this machine can answer
.\scripts\verify.ps1 -Surface ci-linux
.\scripts\verify.ps1 -Surface ci-windows
.\scripts\verify.ps1 -Surface ci-ui
.\scripts\verify.ps1 -Surface release-windows
.\scripts\verify.ps1 -Surface release-linux
.\scripts\verify.ps1 -Surface release-seed
```

Each surface runs exactly what its job runs, no more and no less. A package that
has to enter the gate enters *there*, and nowhere else.

It does not stop at the first failure. A gate that aborts on error one forces a
full round trip per problem; this records them and prints the list at the end.

Two things it deliberately does not do:

- **`-race` on a development Windows machine.** It needs a C toolchain there,
  and the same code already runs with `-race` in the Linux job. The `all`
  surface says so when it finishes rather than letting you assume otherwise.
- **Start anything.** No rooms, no firewall writes, no elevated console. That is
  what the `measure-*.ps1` scripts are for, and each states its own terms.

## Building the Windows client

```powershell
.\scripts\build-installer.ps1
```

Five steps: stamp the version into the Windows resources, assemble the payload
(daemon, interface, catalogue, DLLs, engine), run Inno Setup over
[`installer/kanpachi.iss`](../../installer/kanpachi.iss), build the portable
single-file bundle, and write `SHA256SUMS-windows` covering both executables.

The `.syso` resource files are committed on purpose — they carry the icons and
the manifest that tells Windows these binaries do **not** request elevation, and
a hand-run `go build` without them would change that behaviour. What cannot stay
committed is the version number inside them, which is why the script regenerates
them.

For a development loop, the portable folder is faster and needs no installer:

```powershell
.\scripts\build-portable.ps1             # builds .\Kanpachi and starts it
.\scripts\build-portable.ps1 debug       # interface in debug, daemon in a visible console
```

## Building the Linux package

Two commands, both **on** Linux:

```sh
# in the engine repository
scripts/build-linux.sh
# in this one
scripts/build-deb.sh --version 0.2.0 --engine ~/.cache/kanpachi-engine-target/release/kanpachi-engine
```

There is no cross-compile in either direction, and that is not an omission: on
Linux the engine pulls in a vendored `dbus`, `zstd-sys` and `kcp-sys` through
bindgen, which is three C toolchains that would need a Linux linker and sysroot
mounted by hand anywhere else. The scripts name what is missing instead of
letting the compiler guess.

`build-deb.sh` does not build the engine, and receives it via `--engine`.
Folding both into one script would make packaging depend on having the Rust
toolchain, which is two separate steps on the publishing runner and half an hour
of difference on a test machine.

The file is called `kanpachi-amd64.deb` with no version in the name, because the
download page points at `releases/latest/download/<file>`, a permanent URL. The
version travels in the control file's `Version` field, which is where `dpkg`
reads it. `--strict` turns the glibc floor check from a warning into a hard
stop, which is the difference between a development machine and a publication.

## Building the seed

```sh
scripts/build-seed.sh
```

`kanpseed` for amd64 **and** arm64, always both — a seed runs on somebody else's
VPS and those come both ways, and `install.sh` picks the file by `uname -m`.
Plus the invitation page and their manifest, `SHA256SUMS-seed-linux`.

Built with `CGO_ENABLED=0` and static, because the seed is copied onto a machine
whose glibc nobody chose. Not stripped: `-ldflags "-s -w"` sets off antivirus
heuristics over Go binaries, and the size saved is not worth a false positive.

**Three separate manifests in one release, on purpose.** `SHA256SUMS-windows`,
`SHA256SUMS-linux` and `SHA256SUMS-seed-linux` come from three workflows writing
into the same release. A shared name would mean the last one to finish
overwrites the others, and then an installer verifies binaries that do not
appear in the file it downloaded.

## The measurement scripts

These are not tests. They start real things and each states what it needs:

| Script | What it measures |
|---|---|
| `measure-engine-end-to-end.ps1` | the engine, end to end |
| `measure-directory.ps1` | the registry as a client sees it |
| `measure-reset.ps1` | what a reset leaves behind |
| `measure-netcfg.ps1` | the adapter's configuration |
| `measure-network-change.ps1` | behaviour when the network changes underneath |
| `measure-return.ps1` | going back to the last room |
| `canary-two-machines.ps1` | the two-machine case, which one machine cannot fake |
| `clean-engine-rules.ps1` | removes engine-written firewall rules left behind |
| `build-test-tools.ps1` | builds the probes the above use |

Their output is the deliverable when a claim in the documentation needs
evidence.

## Building the engine and the fork

They live in their own repositories, with their own instructions:

- [`kanpachi-engine`](https://github.com/alvarogabrielgomez/kanpachi-engine) —
  the Rust binary the daemon runs as a child process.
- [`EasyTier`, `kanpachi` branch](https://github.com/alvarogabrielgomez/EasyTier) —
  the fork the engine builds on. Its diff against upstream `v2.6.4` is meant to
  be checked rather than believed:
  `git diff v2.6.4 kanpachi -- '*.rs' '*.proto'`.

The engine follows the fork's moving `kanpachi` branch; what records the exact
commit is `Cargo.lock`.

## Next

- [Fork it](fork-the-branding.md) — publishing your own build under your own
  name, and the constants that must never be touched while doing it.
- [Architecture](architecture.md) — why this is three repositories.
