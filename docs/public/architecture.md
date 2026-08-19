# Architecture

Kanpachi is three repositories and three processes, and the split is not
organisational. It **is** the security model: each piece is allowed to decide
less than the one above it, so compromising the lower ones does not buy the
thing worth having.

This page explains the shape. The full design documents, with every decision and
its rejected alternatives, are in Spanish in [`docs/`](../).

## The three repositories

| Repository | What it is | What it may decide |
|---|---|---|
| [`kanpachi`](https://github.com/alvarogabrielgomez/kanpachi) | daemon, window, CLI, seed, catalogue | everything: who is in the room, and what may be reached |
| [`kanpachi-engine`](https://github.com/alvarogabrielgomez/kanpachi-engine) | the Rust network binary | nothing. It moves packets |
| [`EasyTier` (`kanpachi` branch)](https://github.com/alvarogabrielgomez/EasyTier) | the fork the engine builds on | nothing of ours lives there |

**The engine decides nothing, and that sentence is load-bearing.** The daemon
decides and writes what may be reached, so a compromise of the engine cannot
open the machine, and the engine offers no way to be told otherwise. It takes
commands on stdin and writes answers on stdout: no port, no named pipe, no
config file, no command-line arguments at all.

### Why the dependency is a fork

Upstream EasyTier **opens the virtual adapter in the Windows Firewall while
creating it**, and no configuration turns that off. That is the exact opposite
of Kanpachi's promise, so the fork removes those two calls and keeps everything
else upstream does.

The official `easytier-core.exe` also opens an administration portal on
`0.0.0.0:15888` with **no authentication of any kind**, through which any local
process can issue credentials, add peers, forward ports and ask for the network
secret in cleartext. Upstream deliberately declined a request for
authentication in favour of an IP allowlist whose default already includes
`127.0.0.0/8`. `kanpachi-engine` listens on nothing.

The claim that the fork is upstream plus a named list is meant to be checked
rather than believed:

```sh
git diff v2.6.4 kanpachi -- '*.rs' '*.proto'   # every hunk belongs to an entry in FORK.md
```

Nothing of Kanpachi's own lives in the fork (no rooms, no invite codes, no
games), because the value of that repository is that its diff reads in one
glance.

## The three processes

```
session 0  (isolated, no desktop, no notification area)
│
│  kanpachid.exe                       Windows service, LocalSystem
│    │                                 holds the room, writes the rules
│    ├──[job]──> kanpachi-engine.exe   session 0, LocalSystem
│    │                                 moves packets, listens on nothing
└────┼──────────────────────────────────────────────────────────────
     │
user session
     │
     └──[job]──> kanpachiui.exe        Flutter, NOT elevated
                   the tray icon lives here
                   talks to the daemon over a named pipe + token
```

The window is a remote control. Closing it leaves the room open. On Linux there
is no window at all and the `kanpachi` command drives the same daemon over a
Unix socket.

The engine is a child inside a job object on Windows and inside the service's
cgroup on Linux, which is the strong layer of "the engine dies with the daemon":
it holds even when the daemon cannot run a single deferred call.

### Inside the daemon

Dependencies point inward, and `core/` knows nothing above it:

| Layer | Contains | Knows about |
|---|---|---|
| `core/domain` | types, rules, invariants | nothing. No I/O, no syscalls, no Windows API |
| `core/port` | the interfaces the domain needs | `domain` |
| `core/usecase` | one intention per file | `domain`, `port` |
| `daemon/adapter` | firewall over COM, netcfg, engine, Steam, catalogue, disk | everything concrete |
| `daemon/service` | startup order, supervisor, heartbeat, watchdogs | interfaces only |
| `daemon/transport` | the pipe or socket, and the room's control channel | |

That boundary is not taste. `core` and `daemon/service` are pure Go and run in
the Linux CI job, which is what gives the startup order tests at all.

## A connection, step by step

```
 1. Somebody pastes the invite code
 2. Argon2id over the invite ID derives the RENDEZVOUS identity
 3. The daemon resolves seeds: DNS record first, a compiled-in reserved IP as backup
 4. The engine enters the rendezvous network. The host sits at .1 of a fixed
    /24, so there is nothing to search for
 5. The guest sends its nickname and public key, signed. The host verifies
 6. The host issues a temporary credential (the REAL network's name, subnet and
    virtual IP), encrypted against the guest's key. The network secret is not in it
 7. The engine leaves the rendezvous and enters the real network with the
    credential. That network's secret never travelled
 8. The seed returns the other members' endpoints
 9. Synchronised hole-punch on both sides
10. Direct peer-to-peer tunnel (fallback: relay through the seed, shown in amber)
11. The rule set is computed for the members present
12. The firewall applies the difference
```

**Step 5 assumes the lobby is observable.** Anybody holding the invite ID can
derive the rendezvous identity, the seed included. That is why what is exchanged
there is signed against the host's long-term key and encrypted against the
guest's. An observer of the lobby sees that somebody asked to enter; they get
neither the credential nor the room's secret.

The invite code is a lookup key, not cryptographic material. The credential is
what admits somebody.

## Where the seed sits, and where it does not

The seed mints invite IDs, holds a card it cannot read, counts heads over a
loopback-only RPC, and introduces peers. When a direct path is built, which is
the normal case, **it is no longer in the path at all**.

When no direct path can be built, usually because of symmetric NAT, packets fall
back to travelling through it, still encrypted with a key that machine was never
given, and the app says the room is degraded rather than hiding it.

If the registry is down, the room still works. The invitation card's
presentation is what degrades.

Full detail, including what a hostile seed could do differently:
[the seed](../../kanpachi-seed.md).

## What is encrypted, with what

| What | How | Who cannot read it |
|---|---|---|
| The room card | AES-256-GCM, key travels in the URL fragment | the seed, which stores it and never sees the key |
| State on disk | AES-256-GCM, key derived by HKDF-SHA256 from the identity key | another user of the machine, on Windows |
| Control-channel envelopes | NaCl anonymous box: X25519 with XSalsa20-Poly1305 | a peer relaying the bytes |
| Traffic with the registry | TLS, verified, no environment proxy, no redirect following | anyone on the path |
| The tunnel between machines | AES-128-GCM inside EasyTier's protocol. **No TLS, no certificates anywhere** | anyone on the path, for the RPC frames |

The last row is the one to read carefully, and it is stated here rather than
rounded up.

There was never a certificate decision to take: the engine dials
`tcp://IP:11010` in the clear and all confidentiality comes from a layer inside
EasyTier's own protocol. Encryption is switched on explicitly, and the algorithm
is the default, AES-**128**-GCM.

**The tunnel key does not come from a KDF.** EasyTier passes the network secret
through Rust's `DefaultHasher`, a general-purpose non-cryptographic hash built
with a zero key. The real network's secret is 32 random bytes that derive from
no typeable string, and *that strength does not carry through to the wire key*.
Choosing `aes-256-gcm` would not fix it, because the 256-bit path uses the same
hasher. There is no forward secrecy at that layer.

Measured on the wire with `tcpdump`, 212 protocol frames parsed: the RPC frames
are encrypted, the heartbeat and the handshake are not, and the two network
names travel in the clear. The room name, the nickname, the invite code and the
virtual addresses do not appear.

## The threat model, briefly

| Threat | Result |
|---|---|
| A compromised member of the room | reaches only the active game's ports on the host. `445`, `3389`, `22` closed always |
| A compromised seed | sees network IDs and public IPs. Does not decrypt, does not join, reaches no service |
| A leaked room code | the bearer gets in until the host renews the code. Renewing is one click and does not evict anybody |
| An evicted member who insists | credential revoked, out of the network in about a second. Back only with a live code, which renewing closes |
| A member impersonating the host, in the room | cannot. Guests dial a known address and accept no inbound connections |
| A member flooding the control channel | the most serious surface in the product, and it exists only on the host's machine. Every write carries a deadline |
| Local malware as the user | uses the API like the user: joins rooms, applies catalogue profiles. Cannot open arbitrary ports |
| Local malware with admin | out of scope. It already owns the machine |
| Another user of the same Windows PC | **not covered by a seed password.** The local channel is granted to the interactive user so the window can talk without elevating. On Linux the socket is `0600` root |
| A stranger wanting to host on somebody's seed | with the seed closed, the three mutating routes require a token, and the token comes from a password the operator hands out |

### The ceiling of a malicious catalogue profile

The catalogue is an editable file and Kanpachi accepts imported profiles, so
this has to be answerable without hedging. **What a hostile profile cannot buy
is exposure to the internet.** The port it opens is bounded to the virtual
adapter, to the room's `/24`, and to the addresses of the members *present*.
There is no UPnP, no router port forwarding, no exit node and no subnet routing,
and none of the three exists as something a profile could ask for. The user's
router is never touched.

What it can buy: one authorised peer of that room reaching one non-forbidden
port of yours, through the tunnel, while you are inside their room. Getting
there needs four consents of yours and one lapse: importing their catalogue
(which asks, cannot overwrite a factory profile, and rejects forbidden ports),
joining their room (which asks again), having something listening on that port,
and not looking at the exposure screen, which lists that port by number and by
who it is open toward.

**A malicious host cannot push you a profile.** Your machine opens what *your*
catalogue says; the game's identifier travels on the wire, never its ports.

It is deliberately the same ceiling as inviting a stranger onto your physical
LAN, with one difference in Kanpachi's favour: here every open port has a name
and a recipient, and both are readable.

## See also

- [Kanpachi Protection](../../kanpachi-protection.md) — the promise, and the
  four things that keep it.
- [The seed](../../kanpachi-seed.md) — what a meeting point sees and stores.
- [The seed's HTTP API](../../registry/API.md).
- [`FORK.md`](https://github.com/alvarogabrielgomez/EasyTier/blob/kanpachi/FORK.md)
  — the fork's changelog against upstream, hunk by hunk.
- [`docs/`](../) — the design documents, in Spanish, and the source of truth for
  why each of these decisions was taken.
