#!/usr/bin/env bash
#
# Builds the seed payload: kanpseed for two architectures, the invitation page,
# and their manifest.
#
# This used to be inline in release-seed.yml. It lives here so the runner and a
# person build the seed the same way, and so the reasons below survive.
#
# # Two architectures, always
#
# A seed runs on somebody else's VPS and those come both ways. Publishing only
# amd64 means the arm64 droplet has nothing to install, and finding out is not
# free: install.sh picks the file by `uname -m`.
#
# # CGO off, static
#
# The seed is copied into a machine whose glibc nobody chose. With cgo off the
# binary carries no dynamic dependency, so it starts on whatever the VPS runs.
# This is the same reason build-deb.sh states for the two Go binaries there, and
# the difference with the ENGINE, which does have a glibc floor because it is
# Rust with vendored C.
#
# # No -ldflags "-s -w"
#
# Stripping symbols sets off false positives in antivirus heuristics over Go
# binaries. It is a project rule and it holds on the server side too, where
# whoever downloads kanpseed may well scan it.
#
# # The version goes in WHOLE, with its v
#
# `kanpseed upgrade` compares what the binary reports against the tag_name the
# GitHub API returns, which carries the v. Cutting it here makes every installed
# seed believe it is one version behind, forever.
#
# # Why the manifest is named SHA256SUMS-seed-linux
#
# Three workflows write into one release. With a shared name the last upload
# overwrites the others and leaves install.sh verifying binaries that are not
# listed in the file it downloaded. It already happened.
#
# Usage:
#
#   scripts/build-seed.sh --version v0.2.0 [--out dist]
set -euo pipefail

raiz="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
version=""
out="$raiz/dist"

while [ $# -gt 0 ]; do
    case "$1" in
    --version)
        version="${2:-}"
        shift 2
        ;;
    --out)
        out="${2:-}"
        shift 2
        ;;
    *)
        echo "unknown option: $1" >&2
        exit 2
        ;;
    esac
done

if [ -z "$version" ]; then
    echo "--version is required: scripts/build-seed.sh --version v0.2.0 [--out dist]" >&2
    exit 2
fi

cd "$raiz"
mkdir -p "$out"

echo "== kanpseed $version =="
for arco in amd64 arm64; do
    CGO_ENABLED=0 GOOS=linux GOARCH=$arco go build \
        -trimpath \
        -ldflags "-X github.com/accentiostudios/kanpachi/registry/cli.Version=$version" \
        -o "$out/kanpseed-linux-$arco" \
        ./registry/cmd/kanpseed
    echo "  ok  kanpseed-linux-$arco"
done

# The page travels with the binary that serves it. kanpseed embeds the state
# into this same file at request time, so a page from another version would
# answer with fields the binary does not fill.
cp invite/index.html "$out/index.html"
echo "  ok  index.html"

echo "== sums =="
(cd "$out" && sha256sum kanpseed-linux-* index.html >SHA256SUMS-seed-linux && cat SHA256SUMS-seed-linux)
