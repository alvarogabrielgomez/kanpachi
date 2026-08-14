#!/usr/bin/env bash
#
# Builds `kanpachi-amd64.deb` out of what is already compiled.
#
# # What it does NOT do, and why
#
# It does not build the engine. That binary comes out of `scripts/build-linux.sh`
# in the engine's repository, running ON Linux, and this script receives it via
# `--engine`. Putting both things in one script would make building the package
# depend on having the Rust toolchain, which on the publishing runner is two
# separate steps and on a test machine is half an hour of difference.
#
# # The version goes INSIDE the package and not in its name
#
# The file is called `kanpachi-amd64.deb`, with no version, because the page
# points at `releases/latest/download/<file>`, which is a permanent URL. The
# version travels in the control file's `Version` field, which is where `dpkg`
# reads it from. The reason is written in `release.yml`: a hand-written version
# constant stayed empty forever and the screen said "there is no installer yet"
# for the whole life of the product.
#
# # --strict: the same check, with teeth
#
# The glibc floor is a warning here and a hard stop with --strict, and that is
# the difference between a development machine and a publication. Warning is
# right when somebody is building a package to try it out with whatever engine
# they have around. A package that cannot start on the release most people run
# is not something to warn about and publish, so the release passes --strict.
# It also writes SHA256SUMS-linux there: whoever produces the artifact produces
# its manifest.
#
# Usage:
#   scripts/build-deb.sh --version 0.2.0 --engine /path/kanpachi-engine [--out dist] [--strict]

set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
version=""
engine=""
out="$root/dist"
strict=0

while [ $# -gt 0 ]; do
	case "$1" in
	--version) shift; version="${1:-}" ;;
	--engine) shift; engine="${1:-}" ;;
	--out) shift; out="${1:-}" ;;
	--strict) strict=1 ;;
	*) echo "unknown option: $1" >&2; exit 2 ;;
	esac
	shift
done

step() { printf '\n=== %s ===\n' "$1"; }
ok() { printf '  OK  %s\n' "$1"; }

[ -n "$version" ] || { echo "--version is required" >&2; exit 2; }
[ -n "$engine" ] || { echo "--engine is required, with the path of the already built engine" >&2; exit 2; }
[ -x "$engine" ] || { echo "not there or not executable: $engine" >&2; exit 1; }
command -v dpkg-deb >/dev/null || { echo "dpkg-deb is missing. Install it with: sudo apt install dpkg-dev" >&2; exit 1; }

# Without the leading `v`: dpkg sorts versions and `v0.2.0` is not a number to
# it.
version="${version#v}"

# The product VARIANT, which is a different axis from the operating system.
#
# Today `headless` is what comes out by default outside Windows, so passing it is
# the same as not passing it. It is passed anyway, and that is the point: the day
# Linux has a desktop interface, that system's default may change and this
# package, which is the SERVER one, has to stay the server one. A `.deb` that
# turned into a desktop one by inheriting a default is the kind of change nobody
# sees until a server tries to open a window.
#
# See `daemon/cmd/kanpachid/variant.go` for the whole table.
VARIANT=headless

step "building the daemon and the CLI ($VARIANT)"
# CGO off: without it the binary does not drag in glibc's dynamic linker for NSS,
# which is what makes a binary built on one distribution fail to start on
# another. The daemon uses nothing that needs it.
export CGO_ENABLED=0
mkdir -p "$out"
# build, with the fallback for the foreign checkout case.
#
# `go build` stamps the binary with the repository's state, and to do that it
# asks git. Running from WSL over a Windows checkout, git refuses with `detected
# dubious ownership` because the files belong to another user, and what comes out
# is `error obtaining VCS status: exit status 128`, which names neither git nor
# the owner.
#
# It retries without the stamping and SAYS SO. Losing it in the artifact being
# published would be a real loss, and there it does not happen: the runner builds
# its own checkout. Here it happens always, and that is no reason to be unable to
# build a test package.
build() {
	local target=$1 package=$2 ldflags=${3:-}
	if go build -trimpath -tags "$VARIANT" -ldflags "$ldflags" -o "$target" "$package" 2>/dev/null; then
		return 0
	fi
	if go build -trimpath -tags "$VARIANT" -ldflags "$ldflags" -buildvcs=false -o "$target" "$package"; then
		echo "  --  without git stamping: this checkout belongs to another user." >&2
		return 0
	fi
	return 1
}

build "$out/kanpachid" "$root/daemon/cmd/kanpachid"
ok "kanpachid"

# The version is stamped into the CLI, which is the only one of the three that
# says it out loud. The mechanism is the same the seed already uses. Without this
# the binary answers `dev`, which is the truth for one built by hand and a lie
# inside a package: a bug report would point at a published version that is not
# the one that failed.
build "$out/kanpachi" "$root/daemon/cmd/kanpachi" "-X main.Version=$version"
ok "kanpachi"

step "building the package tree"
tree="$out/kanpachi-deb"
rm -rf "$tree"
mkdir -p \
	"$tree/DEBIAN" \
	"$tree/usr/bin" \
	"$tree/usr/libexec/kanpachi" \
	"$tree/usr/share/kanpachi" \
	"$tree/lib/systemd/system"

install -m 0755 "$out/kanpachi" "$tree/usr/bin/kanpachi"
install -m 0755 "$out/kanpachid" "$tree/usr/libexec/kanpachi/kanpachid"
install -m 0755 "$engine" "$tree/usr/libexec/kanpachi/kanpachi-engine"

# The catalog that ships with the package. It is READ ONLY for the daemon: the
# package updates it, and the user's own lives separately in /var/lib.
#
# # No catalog, no package
#
# This was a warning and had to be a stop. The first version looked at
# `catalog/builtin.json`, which does not exist in this repository -- the file
# lives beside its parser, which is where the four Windows scripts look for it --
# so the warning branch was taken ALWAYS and the package went out without a
# single game. Measured on 2026-08-10 with `kanpachi games`, which answered that
# the catalog was empty. `dpkg-deb --contents` does not show it: what has to be
# looked at is what is NOT there.
catalog="$root/daemon/adapter/catalog/jsonfile/builtin.json"
[ -f "$catalog" ] || {
	echo "the catalog is not there: $catalog" >&2
	echo "  Without it the room opens and there is not a single game to activate." >&2
	exit 1
}
install -m 0644 "$catalog" "$tree/usr/share/kanpachi/builtin.json"
ok "builtin.json ($(grep -c '"id"' "$catalog") profiles)"

install -m 0644 "$root/packaging/systemd/kanpachid.service" "$tree/lib/systemd/system/"
install -m 0644 "$root/packaging/systemd/kanpachi-quarantine.service" "$tree/lib/systemd/system/"

sed "s/@VERSION@/$version/" "$root/packaging/debian/control" >"$tree/DEBIAN/control"
for s in postinst prerm postrm; do
	install -m 0755 "$root/packaging/debian/$s" "$tree/DEBIAN/$s"
done
ok "tree at $tree"

step "checking before packaging"
# The ENGINE's glibc floor decides which Ubuntu the package runs on, and it is
# the only one of the three binaries that has one: the Go ones go without cgo.
# Built on 24.04 it demands 2.39 and does NOT start on 22.04, which is the one
# most often seen on a VPS.
glibc="$(objdump -T "$tree/usr/libexec/kanpachi/kanpachi-engine" 2>/dev/null |
	grep -o 'GLIBC_[0-9.]*' | sort -V | tail -1 || true)"
if [ -n "$glibc" ]; then
	printf '  the engine demands %s\n' "$glibc"
	case "$glibc" in
	GLIBC_2.3[6-9] | GLIBC_2.4*)
		echo "  --  above the 2.35 of Ubuntu 22.04: this package will NOT start there." >&2
		echo "      The artifact that gets published is built on a 22.04 runner." >&2
		[ "$strict" -eq 0 ] || exit 1
		;;
	esac
fi

step "packaging"
deb="$out/kanpachi-amd64.deb"
# `--root-owner-group` so everything comes out as root:root without needing
# fakeroot.
dpkg-deb --build --root-owner-group "$tree" "$deb" >/dev/null
ok "$deb ($(( $(stat -c %s "$deb") / 1024 )) KiB)"

step "what ended up inside"
dpkg-deb --contents "$deb" | awk '{print "  " $1, $6}'

# The manifest only with --strict, which is to say only when this publishes. On a
# development machine a loose SHA256SUMS-linux in dist/ is verified by nobody,
# and the name carries `-linux` because three workflows write into the same
# release: with a shared name, the last one to upload overwrites the others.
if [ "$strict" -eq 1 ]; then
	step "sums"
	(cd "$out" && sha256sum kanpachi-amd64.deb >SHA256SUMS-linux && cat SHA256SUMS-linux)
fi

step "done"
