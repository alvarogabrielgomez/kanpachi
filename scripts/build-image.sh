#!/usr/bin/env bash
#
# Builds the container image out of a `.deb`.
#
# # Why the Dockerfile does not fetch the package itself
#
# Because the job that publishes this image carries no `needs:`, like the other
# three publishers, so at the moment it runs the release may not have a `.deb`
# in it yet. A Dockerfile that downloaded `releases/latest` would then bake the
# PREVIOUS version and pass, which is the worst shape a release bug can take.
#
# So the package is staged next to the Dockerfile before the build, and this
# script is what puts it there: either one already built, with --deb, or one
# freshly built from this tree, with --engine.
#
# # It does not push
#
# Tagging and pushing belong to whoever has the credentials, which is the
# workflow. This leaves a local image with its tags and says what it made.
#
# Usage:
#   scripts/build-image.sh --version 0.6.4 --deb dist/kanpachi-amd64.deb
#   scripts/build-image.sh --version 0.6.4 --engine /path/kanpachi-engine
#   scripts/build-image.sh --version 0.6.4 --deb dist/kanpachi-amd64.deb \
#       --tag ghcr.io/owner/kanpachi:0.6.4 --tag ghcr.io/owner/kanpachi:latest

set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
version=""
deb=""
engine=""
tags=()

while [ $# -gt 0 ]; do
	case "$1" in
	--version) shift; version="${1:-}" ;;
	--deb) shift; deb="${1:-}" ;;
	--engine) shift; engine="${1:-}" ;;
	--tag) shift; tags+=("${1:-}") ;;
	*) echo "unknown option: $1" >&2; exit 2 ;;
	esac
	shift
done

step() { printf '\n=== %s ===\n' "$1"; }
ok() { printf '  OK  %s\n' "$1"; }

[ -n "$version" ] || { echo "--version is required" >&2; exit 2; }
[ -n "$deb" ] || [ -n "$engine" ] || {
	echo "one of --deb or --engine is required: either the package is already built, or this builds it" >&2
	exit 2
}
command -v docker >/dev/null || { echo "docker is missing" >&2; exit 1; }

# The staged copy is removed on the way out whatever happens. Leaving a `.deb`
# inside `docker/` would make the next build silently reuse it, which is the
# same mismatch this script exists to avoid.
staged="$root/docker/kanpachi-amd64.deb"
cleanup() { rm -f "$staged"; }
trap cleanup EXIT INT TERM

if [ -z "$deb" ]; then
	step "building the package"
	"$root/scripts/build-deb.sh" --version "$version" --engine "$engine" --out "$root/dist"
	deb="$root/dist/kanpachi-amd64.deb"
fi

[ -f "$deb" ] || { echo "not there: $deb" >&2; exit 1; }

step "staging"
cp "$deb" "$staged"
ok "$(basename "$deb") -> docker/"

step "building the image"
args=()
for t in ${tags+"${tags[@]}"}; do
	args+=(--tag "$t")
done
# With no tag asked for, one local name so the build leaves something usable.
[ ${#args[@]} -gt 0 ] || args+=(--tag "kanpachi:$version")

docker build "${args[@]}" "$root/docker"

step "what it is"
for t in ${tags+"${tags[@]}"}; do
	printf '  %s\n' "$t"
done
[ ${#tags[@]} -gt 0 ] || printf '  kanpachi:%s\n' "$version"

# The version is not a label the image can be asked for, so it is checked by
# asking the CLI inside it. A `.deb` staged from the wrong place shows up here.
step "checking what ended up inside"
first="${tags[0]:-kanpachi:$version}"
docker run --rm --entrypoint /usr/bin/kanpachi "$first" version

step "done"
