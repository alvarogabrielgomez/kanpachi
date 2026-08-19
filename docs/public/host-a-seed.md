# Host your own seed

A seed is the meeting point: it mints invite codes, holds a sealed card it
cannot read, counts heads, and introduces two machines so they can punch a hole
through their routers. Once the tunnel is up it is out of the path.

Running your own is the reason the seed's name travels **inside** the invite
code: `A7K2-M9QX@seed.example.com`. Two seeds are two unrelated worlds, and a
code says which one it means.

What this needs: a Linux box with systemd, amd64 or arm64, a public IP, a domain
name, and a reverse proxy you are willing to configure.

For what a seed can see, store and never learn, read
[the seed](../../kanpachi-seed.md). This page is about putting one up.

## 1. Point the domain first

Create an A record for the name your users will type, aimed at the machine.

Do it before installing. The name is not decoration: it is stamped into every
invite code minted here, and it is bound **inside** the hash a hosting password
is proved with,
so a proof captured on one seed is worth nothing on another. Get the name
wrong and the seed rejects every host that types the correct password.

## 2. Install

```sh
curl -fsSL https://raw.githubusercontent.com/alvarogabrielgomez/kanpachi/main/seed/install.sh | sudo sh -s -- --domain seed.yourdomain.com
```

Without `--domain` it asks. Set `KANPSEED_VERSION` to install something other
than the latest release.

The script is deliberately dumb: it checks the architecture, downloads the
binary and the invitation page, verifies both against `SHA256SUMS-seed-linux`
**before** granting execute permission, and hands the work to `kanpseed init`,
which is Go and has tests. The page is verified too, not only the binary: it is
served to strangers and is just as replaceable in transit.

`kanpseed init` then:

- downloads and places EasyTier `v2.6.4`, pinned and never `latest`;
- picks the ports: `11010` for the engine unless it is taken, the first free
  one from `8010` for the registry, `15888` for the engine's control RPC, bound
  to loopback;
- writes `kanpseed-engine.service` and `kanpseed-registry.service`, enables and
  starts both;
- waits for the registry to answer on `127.0.0.1`;
- offers to set a hosting password.

Running it again is how you upgrade, and it **keeps the ports of an existing
install** rather than choosing them afresh, because the machine's reverse proxy
points at one of them. On a machine that already has a seed, `sudo kanpseed
upgrade` does the same thing without needing curl or this URL.

## 3. Publish it — the part it does not do for you

The registry answers on loopback only. Exposing it is your call, and there are
two halves.

### The HTTP half: your reverse proxy

```sh
kanpseed nginx
```

prints the block to paste, with the port this install chose. For nginx by hand
it is:

```nginx
location / {
    proxy_pass http://127.0.0.1:8010;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-For $remote_addr;
}
```

Two lines there are load-bearing:

- **TLS is not cosmetic.** The invitation page uses `navigator.clipboard` for
  its copy button, and that API only exists in a secure context. Let's Encrypt
  with Force SSL, or the equivalent in whatever proxy you use.
- **`X-Forwarded-For` is not optional.** The registry's rate limit counts per
  IP, and it only believes that header when the connection comes from loopback.
  A proxy that does not set it makes every visitor on earth share one bucket,
  including every login attempt.

### The engine half: one port, both protocols

The engine's port, `11010` by default, has to be reachable from the internet
over **TCP and UDP**. It negotiates over TCP and hole-punches over UDP, so
allowing only one produces the worst possible failure: it works for some players
and not others, depending on somebody else's router.

```sh
sudo ufw allow 11010/tcp
sudo ufw allow 11010/udp
```

The installer does not do this for you, and says so at the end if ufw is in the
way. Opening a port to the whole world is the machine owner's decision.

**If you are coming from Docker, read this line.** Publishing a container port
does not open the firewall: it inserts DNAT rules that are evaluated *before*
ufw, so `ports: 11010:11010` was reachable whether ufw allowed it or not. A
native process has no such privilege. The symptom of getting this wrong is that
the engine listens, `doctor` sees it listening, systemd calls it healthy, and no
client ever connects.

## 4. Check it

```sh
sudo kanpseed doctor
```

It reports the services, the ports, the health endpoint and the firewall, and it
distinguishes "ufw is inactive" from "ufw is installed and I cannot read its
rules without root", because treating the second as the first would be a false
all-clear.

## 5. The optional password

A seed can ask for a password **to host on it**: opening a room, publishing to
one, renewing a code. That is what stops strangers parking their rooms on your
bandwidth.

```sh
sudo kanpseed password           # set or replace it
sudo kanpseed password --open    # remove it, anyone can host
```

`init` offers it at the end of a fresh install. What to know:

- **Joining a room never asks for it, on any seed.** A guest with a code gets in
  regardless.
- **One shared password, no accounts.** There is nobody to revoke individually.
- **It is typed at a terminal, never as a flag.** There is no `--password`
  option on purpose: the argv of a process is world readable, and a flag would
  put the seed's password into shell history as well.
- **Changing it throws out every signed-in host at once,** because the signing
  key is rotated. They get back in by typing the new one.
- **It is bound to the domain,** so it must be the name people type.

Users set theirs with `kanpachi password` on their own machine, which stores it
per seed and never on a command line either.

## Pointing clients at it

On each client:

```sh
kanpachi seed seed.yourdomain.com
```

or, in the Windows app, the screen behind the gear. From then on, rooms opened
by that machine are minted here. Rooms *joined* still follow whatever seed
arrived inside the code.

## Removing it

```sh
sudo kanpseed uninstall
```

Stops and removes both units, deletes `/usr/local/lib/kanpachi`,
`/etc/kanpachi` and the state directory. It does **not** touch your reverse
proxy or your firewall, because we did not write them.

The state directory is on that list for a reason: the operator credential lives
there, and it holds the key that mints tokens. Leaving signing material on a
machine where nothing uses it any more is not tidiness.

## What is where

| Path | What |
|---|---|
| `/usr/local/bin/kanpseed` | the binary: CLI and registry in one |
| `/usr/local/lib/kanpachi/` | EasyTier and `index.html`, the invitation page |
| `/etc/kanpachi/seed.json` | the chosen ports and the domain. The single source of truth |
| `/var/lib/kanpseed/` | state, including the operator credential |
| `/etc/systemd/system/kanpseed-engine.service` | EasyTier as a public node, `--no-tun` |
| `/etc/systemd/system/kanpseed-registry.service` | the registry and the page, on loopback |

The page and the API share one origin and one port, so there is no CORS to
configure and no second thing to expose.

## Next

- [The seed's HTTP API](../../registry/API.md) — every endpoint, its rate limit,
  and what defends it.
- [The seed](../../kanpachi-seed.md) — what it sees, what it stores, and what a
  hostile one could do differently.
- [Every command](reference-cli.md) — `kanpseed` and `kanpachi` in full.
