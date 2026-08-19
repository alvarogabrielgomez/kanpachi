# syntax=docker/dockerfile:1
#
# Kanpachi in a container.
#
# It carries the daemon, the engine and the CLI, taken from the `.deb`. The
# package is EXTRACTED and never installed: it declares `Depends: systemd`,
# which a container has no use for, and installing it would run a `postinst`
# whose whole job is `systemctl`.
#
# **Build it with `scripts/build-image.sh`, not by hand.** The package has to be
# staged into this directory first, and that script is what fetches a published
# one or builds one from this tree. A bare `docker build docker/` fails on the
# COPY below, which is the intended failure: an image built around whatever
# `.deb` happened to be lying here is the kind of mismatch that shows up as a
# room that will not open.
#
# amd64 only: that is the single architecture the Linux release publishes.

# ---------------------------------------------------------------- the package
FROM debian:bookworm-slim AS package

WORKDIR /tmp/kanpachi
COPY kanpachi-amd64.deb .

RUN dpkg-deb -x kanpachi-amd64.deb /extracted \
	&& ls -l /extracted/usr/libexec/kanpachi

# ------------------------------------------------------------------ the image
FROM debian:bookworm-slim

# `nftables` for the `nft` binary. The gate itself speaks netlink directly, so a
# room opens without it, but the base quarantine and the audit of a foreign
# firewall both shell out to `nft` and would degrade in silence.
# `jq` so the entrypoint reads the daemon's JSON instead of guessing at it.
RUN apt-get update \
	&& apt-get install -y --no-install-recommends ca-certificates jq nftables \
	&& rm -rf /var/lib/apt/lists/*

# /usr/sbin is where `nft` lands, and a container PATH often leaves it out.
ENV PATH="/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

# The binaries, the game catalogue, and the licence texts, which are an
# obligation and travel inside the package.
COPY --from=package /extracted/usr/bin/kanpachi /usr/bin/kanpachi
COPY --from=package /extracted/usr/libexec/kanpachi /usr/libexec/kanpachi
COPY --from=package /extracted/usr/share/kanpachi /usr/share/kanpachi
COPY --from=package /extracted/usr/share/doc/kanpachi /usr/share/doc/kanpachi

COPY entrypoint.sh /usr/local/bin/kanpachi-entrypoint
RUN chmod 0755 /usr/local/bin/kanpachi-entrypoint

# The room lives here. Declared so that running the image with no volume still
# keeps its state for the life of the container instead of failing to start.
VOLUME ["/var/lib/kanpachi"]

ENTRYPOINT ["/usr/local/bin/kanpachi-entrypoint"]
