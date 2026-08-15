# The seed's API

A complete reference for the HTTP surface of `kanpseed`, the seed's room registry. It explains what each endpoint does, how the optional password door works, and what concrete defence protects each thing.

---

## The whole map

One process, one port. The invitation page and its API share an origin, so there is no CORS to configure and no second thing to expose.

| Method and route | What it does | Rate limit | Asks for a token? |
|---|---|---|---|
| `POST /api/rooms` | Mints an invite ID and registers the room | 30/min per IP | Yes, if the seed is closed |
| `GET /api/i/{id}` | Resolves an invite ID | 30/min per IP | Never |
| `PUT /api/i/{id}` | Updates the card of an existing room | 30/min per IP | Yes, if the seed is closed |
| `DELETE /api/i/{id}` | Closes a room: expires its card, keeps its pin | 30/min per IP | Yes, if the seed is closed |
| `POST /api/auth/token` | Trades the password proof for tokens | **5/min** per IP | It is what issues them |
| `POST /api/auth/refresh` | Renews the access token | **5/min** per IP | It verifies the refresh |
| `GET /healthz` | Service health | None | Never, and it cannot |
| `/api/*` (the rest) | 404 with the error envelope | None | No |
| `GET /{any-route}` | The invitation page | 30/min per IP, the same bucket as the API | No |

Every response goes out with the headers from [`cabecerasSeguras`](http.go): a CSP of `default-src 'none'` with `connect-src 'self'` (the only request the page is authorized to make is resolving an invite ID, decision 24), `X-Content-Type-Options: nosniff` and `Referrer-Policy: no-referrer`.

## What an invite ID is

Eight symbols from a frozen 32-symbol alphabet (`23456789ABCDEFGHJKLMNPQRSTUVWXYZ`, without `0`, `O`, `1` or `I`), which is 40 bits ([`core/domain/inviteid.go`](../core/domain/inviteid.go)). The canonical form carries a dash, `A7K2-M9QX`; parsing is permissive about separators and lowercase, output is always canonical, and what travels in the URL is the raw 8-character form.

**It is not cryptographic material and it is not a secret.** It is a lookup key that the seed mints, stores and writes to its logs. What opens a room is the credential the host issues over the control channel, never the code. An invite ID only means something in the registry that minted it: the same eight characters on two seeds are two different rooms. That is why 40 bits are enough: guessing one gives you the presentation card and the right to knock, with a rate limit in front so that walking the space is not worth the trouble (decision 24).

## Every endpoint in detail

### `POST /api/rooms`, minting

Who calls it: `CreateRoom` and `RotateInviteCode` in the daemon, through [`Directory.Open`](../daemon/adapter/directory/directory.go). Renewing a code IS this endpoint: a new ID under the same key.

Body, the three fields in base64url without padding:

```json
{"host_key": "<Ed25519 public, 32 bytes>", "card": "<sealed card, ≤512 bytes>", "sig": "<Ed25519 signature over card, 64 bytes>"}
```

What it does, in order ([`store.go`](store.go)):

1. Rejects a card over `MaxCardBytes` (512). This endpoint is open to the internet and the registry lives in memory: without a cap, a card is an exhaustion vector.
2. Verifies `ed25519.Verify(host_key, card, sig)`. That proves self-consistency, not identity: anybody can generate a pair and sign their own card.
3. Draws a free invite ID, with eight attempts as the ceiling. A full registry answers `exhausted` instead of spinning forever.
4. Derives the ID's rendezvous network with Argon2id, **outside the store's lock and through the single-slot brake** (see "The brakes"). The request's context reaches all the way into the queue: somebody who hung up does not get 64 MiB spent on an answer nobody is waiting for.
5. Inserts the entry with its deadline, `RoomTTL` of 21 days, and mirrors it to disk.

Response: `201 {"invite_id": "A7K2-M9QX"}`.

| Failure | Status and code |
|---|---|
| Body that does not parse, unknown field, invalid base64 | `400 bad_request` |
| Signature that does not verify | `403 bad_sig` |
| Card over 512 bytes | `413 card_too_big` |
| No free invite ID in eight attempts | `503 exhausted` |
| Closed seed without a valid bearer | `401 unauthorized`, `sub: reauth` |
| The client hung up while waiting for the derivation | `499`, no body |

### `GET /api/i/{id}`, resolving

Who calls it: `checkRoomExists` and `PeekInvite` in the daemon, and the page's fallback `fetch`. **It never asks for anything, on any seed, open or closed.** The guest's friction is what this product exists to remove (decision 34).

Response `200`:

```json
{"card": "<sealed>", "host_key": "<the key PINNED for this ID>", "sig": "<if there is one>", "members": 2}
```

Two absences that mean something, and both are deliberate ([`http.go`](http.go), `vista`):

- `sig` is omitted when there is none. A room written before the registry started keeping signatures comes back from disk without one, and omitting it tells the truth, "I do not have it". An empty string would say "it is unsigned", which is a different claim and a false one. The client treats it as `CardUnverified`, never as a card the pinned key does not back.
- `members` is omitted when the counter has never managed to talk to the engine. A zero would be the claim "there is nobody", and it would be false; absent says "I do not know". The client decodes into a pointer and turns the absence into `-1` ([`directory.go`](../daemon/adapter/directory/directory.go)).

The count comes from `easytier-cli peer list-foreign` against the loopback RPC portal, polled every 3 seconds and cached ([`counter.go`](counter.go)): a flood of visitors does not turn into a flood of child processes. That JSON has peer IDs and addresses in it and no nicknames: nicknames travel inside the encrypted network and the seed relays without decrypting.

**A room stops resolving for exactly two reasons, and they answer alike.** Its host closed it, or nobody republished it for `RoomTTL` and the sweep took it. A renewed code reaches the first one, because renewing closes the old code. All three come back as the same `404 not_found` with nothing to tell them apart, and that is a property rather than an accident: **splitting that 404 would be an oracle**, telling whoever walks the code space that an ID was alive once. It is information about other people's rooms in exchange for a nuance nobody needs.

**A host being away is not one of those reasons.** There used to be a six-hour card deadline here, and it answered "no such room" about rooms that were only waiting for a machine to come back up, with weeks of pin still to go. Silence now costs nothing until the sweep.

| Failure | Status and code |
|---|---|
| The ID does not have the shape of an invite ID | `400 bad_request` |
| It does not exist, was closed, or was swept | `404 not_found` |

### `PUT /api/i/{id}`, publishing

Who calls it: the host's periodic card republish and reopening a room under the same code, through [`Directory.Publish`](../daemon/adapter/directory/directory.go). Same body as the POST.

What it demands, in order ([`store.go`](store.go), `publish`):

1. Card within the cap.
2. A valid signature by `host_key` over `card`.
3. **That the entry exists.** `Publish` never creates. Creating would reopen the race the pin exists to close: an ex-member who kept the code would get there first when the room reopens.
4. **That `host_key` is exactly the key pinned the first time.** This is the endpoint's real lock.

If it passes, the card and signature are refreshed, the deadline is renewed, and **a closed room is reopened**. That last one is the point: closing and coming back under the same code is the headless host's whole promise, and the pin has already proved this is the key that claimed the ID. Response: `204`, no body.

| Failure | Status and code |
|---|---|
| The entry does not exist, or its pin expired and it was swept | `404 not_found` |
| The key is not the pinned one | `403 pinned` |
| Invalid signature | `403 bad_sig` |
| Card too large | `413 card_too_big` |
| Closed seed without a bearer | `401 unauthorized`, `sub: reauth` |

A `403 pinned` and not a `409`: the invite ID exists and it is not yours. It is the answer a member gets when they try to overwrite the host's card.

### `DELETE /api/i/{id}`, closing

Who calls it: the host closing the room, and renewing a code, which closes the old one. Both through [`Directory.Close`](../daemon/adapter/directory/directory.go), and both **best-effort** — a registry that does not answer cannot stop somebody closing their own room.

Body:

```json
{"host_key": "<Ed25519 public, 32 bytes>", "sig": "<signature over the close message>", "ts": 1755264000}
```

**There is no card to sign here, so the signature covers a message built for this** ([`core/domain/roomclose.go`](../core/domain/roomclose.go)):

```
"kanpachi/room-close/v1" ‖ 0x00 ‖ inviteID ‖ 0x00 ‖ unix(8, big-endian)
```

The invite ID goes inside, or a good signature for one room would close any other room of the same host. And the timestamp goes inside because **closing is the one message whose recorded copy keeps working later**: publishing an old card leaves an old card, which is what was already there, but replaying a close after the host REOPENS the same room kills a live one — and reopening with the same code is exactly what a headless host does on every boot. `ts` travels beside the signature because the verifier has to rebuild the same bytes. Tolerance is ±5 minutes, in both directions: a stamp in the future is a wrong clock or somebody extending a signature's life on purpose.

What it does ([`store.go`](store.go), `retire`): stamps `ClosedAt` and empties the card and its signature. It does **not** delete the entry, and that is the whole design. `lookup` answers 404 from that instant and `publish` keeps working — so **reopening the same room with the same code is untouched**. Deleting the entry would return the ID to the pool and reopen the race the pin exists to close.

**Closing has a field of its own, and it used to borrow the card's deadline.** Pushing the expiry into the past made a closed room indistinguishable from a host who had been away six hours, which is how silence came to end rooms nobody had ended. `ClosedAt` never leaves this process: on the wire a closed room, a swept one and one that never existed are the same 404.

Closing an already-closed room is a no-op and not an error: closing is on the idempotent path out of a room, which three places call.

Response: `204`, no body.

| Failure | Status and code |
|---|---|
| Malformed key, signature or body | `400 bad_request` |
| The stamp is outside ±5 minutes | `400 bad_request` |
| The signature does not verify | `403 bad_sig` |
| The key is not the pinned one | `403 pinned` |
| No such entry, or its pin expired and it was swept | `404 not_found` |
| Closed seed without a bearer | `401 unauthorized`, `sub: reauth` |

### `POST /api/auth/token`, login

Body: `{"proof": "<43 characters>"}`. An open seed answers `409 seed_open`, which means "stop asking", not "retry".

The order of the three brakes is the whole point, and it is the one decision 34 asked for after measuring: **rate limit, growing delay, and only then the derivation slot.** A throttled attempt never reaches Argon2id, and a queued one dies with its socket. The proof's shape is checked before any derivation is spent. The final comparison is Argon2id over the proof against the stored hash, in constant time ([`auth.go`](auth.go)).

Response `200`:

```json
{"access_token": "...", "refresh_token": "...", "expires_in": 900}
```

Every credential failure is `401 unauthorized` with `sub: password`, never distinguishing whether the proof was invalid, short, or from another seed. See "The error envelope".

### `POST /api/auth/refresh`, renewing

Body: `{"refresh_token": "..."}`. Verifying a refresh is one HMAC, so this endpoint **does not go through the derivation slot**: there is nothing to guess at 128 bits of MAC. The 5/min limit still applies, because an endpoint anybody can reach is an endpoint anybody can flood.

Response `200`: `{"access_token": "...", "expires_in": 900}`. No new `refresh_token`, and that is by design: **the refresh does not slide.** The session has a hard ceiling of 30 days from the moment the password was typed, and a stolen token cannot renew itself forever. Failure: `401` with `sub: password`, because refreshing is what just failed and the only move left is typing.

### `GET /healthz`

`200 {"rooms": 24}` when everything works. When EasyTier does not answer, `503` with the same body plus `"counter": "<the error>"`: a degraded counter does not stop codes resolving or the page being served.

**It carries no password and it cannot.** The unit beats by asking for it with `WatchdogSec=30s` ([`setup/units.go`](setup/units.go), [`systemd.go`](systemd.go)): covering it with auth would restart the seed every thirty seconds, and the `BindsTo` would take the engine down with it. The heartbeat itself treats a `503` as alive ([`cli/serve.go`](cli/serve.go), `sano`): a downed engine is no reason to restart the registry.

All it reveals is how many entries are in the map, dead pins included, and the text of the counter's last error.

### The `/api/` wildcard

Any API route that does not match above dies as `404 not_found` with the error envelope, without naming the route or suggesting similar ones: that would be answering whoever is probing the space. **Measured against the deployment when it was added:** without this line, `/api/version` (which was deleted once it ran out of callers) answered `200` with the invitation HTML.

### `GET /{any-route}`, the page

Serves `invite/index.html` with the server state already embedded in a `<script type="application/json" id="ssr">` slot ([`page.go`](page.go)). The state carries the published version (cached from GitHub, one day of cadence, see [`release.go`](release.go)), the repository, and, when the route parses as a live invite ID, **the same room view that `GET /api/i/{id}` serves**: card, key, signature and count. What gets embedded is escaped (`<`, `>`, `&`, U+2028, U+2029) so that a hostile card cannot close the block and turn into markup. `Cache-Control: no-store`, because a cached front page showing a dead room is worse than one extra trip.

**It goes through the same limiter as the API, and until 2026-08-15 it did not.** That was a hole and it was measured against the deployment: `GET /U8DZ-DBU2` returned the room's full view and `GET /AAAA-AAAA` returned `"room":null`, at whatever rate anyone liked — the same existence oracle as the endpoint that does have a brake, so enumerating simply moved one route over. The code's comment claimed the route "cannot be enumerated", which was only ever true of the side with a guard.

The **same** bucket as the API and not one of its own: a budget of its own would hand an enumerator thirty a minute here plus thirty there, and sharing it puts the ceiling at thirty per IP no matter which door gets knocked on. It costs an honest visitor nothing — opening an invitation is one request for the page, the browser's implicit `/favicon.ico`, and the fallback `fetch` only when the server did not resolve the card already.

## The error envelope

Every failure of this API is `{"code": "...", "sub": "..."}` and nothing else. The prose left on purpose (decision 34): a message that explains is a message that distinguishes, and on the password path the difference between "that token expired" and "that token was never valid" is free information for whoever is guessing. Rather than remembering to be careful on two routes out of six, there is nowhere left to put the sentence. The prose lives in the client ([`client.go`](../daemon/adapter/directory/client.go), `errorDelRegistro`), which knows the language of the person reading; a seed somebody else runs no longer chooses what appears on anybody's screen.

| `code` | Status | What it means |
|---|---|---|
| `bad_request` | 400 | Body or ID that does not parse. Unknown fields included: the decode is strict |
| `not_found` | 404 | That room does not exist, or that API route does not exist |
| `pinned` | 403 | The invite ID belongs to another key |
| `bad_sig` | 403 | The signature does not verify against the key it carries |
| `card_too_big` | 413 | Over 512 bytes of card |
| `exhausted` | 503 | No free ID could be minted |
| `rate_limited` | 429 | Rate limit, with `Retry-After: 60` |
| `unauthorized` | 401 | Missing credential, or one that does not hold |
| `unavailable` | 503 | Unclassified internal failure |
| `seed_open` | 409 | A token was asked of a seed with no password |

`sub` says what to DO, never what went wrong, and there are exactly two: `reauth` (refresh, and if that fails ask for the password) and `password` (go straight to typing). **There is no sub-code for "expired"**, deliberately. The mutation `401` also carries `WWW-Authenticate: Bearer`, which is what tells a first-time host against a closed seed that the door exists at all.

The `499` with no body is the client that hung up: writing an answer there is work for a socket that is already gone.

## The optional door

### The two states

A seed is born **open**: anybody hosts on it, and the three guarded operations pass without a token, exactly as they did before the door existed. It is closed with `kanpseed password`, which demands a state directory: a password that disappears on the next restart is worse than none, because the operator believes a door is shut that is still open ([`auth.go`](auth.go), `ErrNoStateDir`). Startup says out loud which of the three cases it is in: no directory, closed, or open ([`cli/serve.go`](cli/serve.go)).

Closed or open, **reading never asks for anything**. The table from decision 34, which is the more important half of the decision:

```
Opening a room, publishing a card, renewing a code   →  asks for a credential
Resolving a code, /healthz, the page                 →  open, always
```

### The proof: the password does not travel, and the operator never learns it

What the client sends is not the password, it is its proof ([`core/domain/seedauth.go`](../core/domain/seedauth.go)):

```
proof = base64url( SHA-256( seed-host ‖ 0x00 ‖ "kanpachi/seed-auth/v1" ‖ password ) )
```

TLS already gives confidentiality on the wire; this solves something else: **the operator of a seed is a third party, and people reuse passwords.** With the hash, what that operator sees, stores or could leak is a value that is useless anywhere else. The host goes inside on purpose: to the server this proof IS the password, whatever authenticates once authenticates again, and without the binding a proof captured on one seed would open rooms on every other seed the same person uses. The client's hash is fast on purpose: the expensive one belongs on the side that can size itself for it.

The password rule: 4 to 128 characters, counted in runes. The minimum is low deliberately — what guards that door is the registry's brakes and not the entropy of what a group of friends types; a high minimum would push the operator into setting no password at all.

### What the server stores

`auth.json` in the state directory, `0600`, written atomically just like `rooms.json`:

| Field | What it is |
|---|---|
| `salt` (16 bytes) | The Argon2id salt |
| `hash` (32 bytes) | Argon2id of the proof. **The password is not recoverable by anybody, the operator included** |
| `signing` (32 bytes) | The key that signs the tokens. **The only real secret in the file:** whoever reads it mints tokens without knowing any password |
| `time`, `memory`, `threads` | The derivation parameters travel inside, so a future change verifies an old credential with the parameters that produced it |

The Argon2id memory is `domain.ArgonMemoryKiB` (64 MiB) and not a number of its own, and that carries an arithmetic: the unit's `MemoryMax` is computed as four times that constant (256 MiB), and authenticating shares the same single slot as the rendezvous derivation. Two independent brakes would put the peak at two derivations at once and make the unit's arithmetic false, which is the shape of the failure that already killed this service by OOM ([`setup/units.go`](setup/units.go)).

`kanpseed password` runs in another process and cannot write into the service's memory: the same `SIGHUP` that rereads the page reloads the credential ([`Auth.Reload`](auth.go)). A credential on disk that does not parse **does stop startup**, unlike a broken `rooms.json`: an unreadable rooms file degrades, an unreadable credential would silently reopen a seed the operator closed.

### The tokens

Opaque and signed, not a JWT: the client reads nothing out of one, so a format that invites reading is surface for no gain.

```
kind(1) ‖ expiry(8, big-endian, unix) ‖ nonce(16) ‖ HMAC-SHA256(32)
```

The MAC covers a versioned domain label (`kanpachi/seed-token/v1`) plus the body. The `kind` goes **inside** the signed bytes: a refresh cannot be presented where an access token is expected. The nonce makes two tokens minted in the same second different from each other, so a token is not a recognizable value that leaks when it was issued.

| Token | Lifetime | Why |
|---|---|---|
| Access | 15 minutes | It travels on every mutation and buys nothing by living longer: a refresh costs one round trip against a seed the client is already talking to |
| Refresh | 30 days, **does not slide** | A hard ceiling from the moment the password was typed. A stolen one expires on its own |

**The whole of revocation is changing the password.** Changing it rotates the signing key, and every token ever issued stops verifying at that instant, with no table to sweep: a store of live tokens would be a thing to sweep, and a sweep that falls behind is a door that stays open. Reopening the seed (`kanpseed password` with no password, [`Auth.Open`](auth.go)) removes the credential and kills the tokens with it.

### What the client does with all this

The daemon's adapter ([`../daemon/adapter/directory/auth.go`](../daemon/adapter/directory/auth.go)):

- **Sends the bearer only when it has one, and in the `Authorization` header**, never in a query parameter or a cookie: a token in a URL ends up in an access log on a machine somebody else runs. And never to a seed that did not ask: sending it to an open one would hand a credential to whoever operates it.
- **What lands on disk is the refresh token and the seed's name, nothing else** (`SeedToken`, sealed in the daemon's state store). The access token lives in memory: it lasts fifteen minutes, and writing it down would put a live credential where it can be stolen in exchange for saving one round trip after a reboot. A stored token whose seed is not the configured one is discarded rather than sent: one operator's credential is not put in front of another.
- **A `401 reauth` costs one refresh and ONE repeat of the call**, and that is not a retry policy: it does not fire on a timeout, on a 5xx or on being throttled, and it cannot happen twice for the same call. It covers the one case where the first answer was not about the request at all: an access token that ran out fifteen minutes into a room that is still open.
- **A refresh that fails is forgotten, in memory and on disk.** The registry refuses to say whether it expired or whether the operator changed the password, and both lead to the same place: the screen asks for the password again, and that is the only moment the interface asks for it.

## The brakes, all together

| Brake | Value | What it cuts |
|---|---|---|
| General limit | 30/min per IP, fixed window, **shared by the API and the page** | Walking 40 bits of invite IDs stops being worth it, by either route |
| Auth limit | 5/min per IP | Guards a password the rule allows to be four characters long |
| Growing login delay | 100 ms × accumulated failures, capped at 2 s, global | The half a per-IP brake cannot cover: a botnet walks around it, a global cost does not. Global because there are no accounts, and blocking "the account" would be a denial of service against every host at once. Capped, so the defence does not become the outage it prevents. A good login resets it |
| Derivation slot | 1, shared between creating a room and logging in | The memory peak does not scale with load: it is what makes the unit's `MemoryMax` true. Resolving a code never gets here, it reads an already-derived network |
| Body cap | 4 KiB, strict decode | Nothing large reaches a parser |
| Card cap | 512 bytes | The registry lives in memory |
| Server timeouts | header 5 s, read/write 15 s, idle 60 s | Deliberately slow clients |
| Watchdog | 30 s, beating at half of it | A process that is alive and hung, which `Restart=always` cannot see |

The IP the limiters count comes from [`ipDe`](http.go), and its rule matters: **`X-Forwarded-For` is only believed when the connection comes from loopback**, that is, from a proxy running on this same machine. From anywhere else `RemoteAddr` is used, which is the one thing the other end cannot write. Without that condition the limit was skipped by writing a header, and since there is a password that stopped being about diluting an enumeration limit and became about skipping the brute-force one. The flip side is printed in the nginx block `kanpseed init` shows: a proxy that does not set the header makes every visitor share a single bucket.

The unit runs with `DynamicUser`, no capabilities, the system read-only except for its state directory, `MemoryMax=256M` and `CPUQuota=25%` ([`setup/units.go`](setup/units.go)).

## What defends what

| Threat | What stops it |
|---|---|
| Enumerating codes through the API | 40 bits against 30/min per IP. And a hit does not grant entry: it grants the card and the right to knock |
| Enumerating codes through the page | The same 30/min bucket. It is one budget per IP and not one per door, so moving to the other route buys nothing |
| A third party overwrites somebody else's card | The pin: `PUT` demands the signature of the key that pinned that ID first. The password is not needed for this |
| An ex-member gets ahead of the host when the room reopens | `Publish` does not create, and the pin outlives the card by 21 days |
| Strangers parking rooms on somebody else's seed | The password. It is the only real brake on `POST`, because in a room that does not exist yet there is no pinned key to compare against |
| The registry serves a card its own key does not back | The client re-verifies the signature against the pinned key ([`InviteLookup.Trust`](../core/domain/roomcard.go)) and logs it as a compromised registry |
| A compromised registry changes key and card together | Outside this API, said in full: that is caught by the fingerprint book of decision 25, which remembers which key that host was seen with before. The lie only leaves a trace for somebody who already knew them |
| Guessing the password | 5/min, a growing global delay, Argon2id behind the slot, a constant-time comparison, and an error envelope that distinguishes nothing |
| Reusing a proof captured on another seed | The seed's host goes inside the hash |
| A stolen refresh token | It expires in 30 days with no way to slide, and the operator kills it earlier by changing the password |
| Exhausting the registry's memory | Body cap, card cap, the single derivation slot, and a `MemoryMax` that is computed rather than eyeballed |
| Work for clients that hung up | The context reaches the derivation queue; the answer is a `499` with no body |
| A tampered `rooms.json` or `auth.json` on disk | They are loaded as hostile input: version, exact sizes, coherent deadlines, and map keys that parse as invite IDs. What does not hold up is discarded, and a broken credential stops startup instead of opening the door |
| Theft of the state file | Inside there are sealed cards and public keys, no network secret and no password. The only thing worth anything is `auth.json`'s signing key, which is why it is `0600` and why it dies when the password rotates |

On the client side, the adapter brings its own refusals ([`client.go`](../daemon/adapter/directory/client.go)): a fixed `https` scheme, no redirects followed (a redirect means the registry is not what is on the other side), no proxy from the environment (this process runs as SYSTEM, and an environment variable does not get to choose where it dials), a response capped at 64 KiB, and **its own resolution with `CheckSeedAddr` over every address on every use**, handing TCP an address that was already approved so that DNS cannot answer differently between the check and the dial.

## What the API does not have, on purpose

- **A way to release an invite ID.** Closing expires the card and keeps the pin, so the code stays reserved for its host for the pin's whole life. There is no verb for "this ID is free again": that would be the race the pin exists to close, offered as an endpoint.
- **A room listing.** There is no way to ask which rooms exist, closed or open. The space can only be walked code by code, against the brake.
- **Accounts.** One shared password per seed, no operator or host identity anywhere in the auth API. The way to throw everybody out is to change it.
- **`/api/version`.** It existed and was deleted once it ran out of callers: the version travels embedded in the page.
- **CORS.** One origin serves everything, so there is no policy to open.

## Known limitations, measured on 2026-08-15

Both came out of the same test: open a room, close the room, and try to enter with the code still in hand. Two other things that test turned up are fixed and documented above — the page without a limiter, and a closed room still resolving.

1. **`members` measures the lobby, not the room.** The host stays in the rendezvous network while hosting and the guest leaves it as soon as it collects the credential, so the real number is "the host plus whoever is entering right now". As "how many people are in" it is a bad number; as a signal for "the host is at the door" it is exact. Two places read it, and this line used to claim nobody did: the invitation page paints it as "N en la sala", which is the bad reading, and the daemon carries it to `InviteLookup.Members`, which is the exact one — a zero there is how a failed join can say "that room exists, its host is not connected right now" instead of a generic error.
2. **A room served before the registry kept signatures comes back unsigned**, and the client treats it as unverified rather than as one the pinned key does not back. That is the truth about it and it is the right call, and it is still a room whose card nothing vouches for.

Neither touches containment or the real network's secret.

## What is NOT a limitation, and reads like one

**An entry outliving a host that died dirty is the design, not a leak.** A power cut, a killed VPS or a process that died are not a room ending: the host reopens by itself on its next start, with the same code, the same network identity and the same profile, and republishes its card on the way. Peers come back whenever they come back. The entry surviving is exactly what makes that possible — expiring it eagerly would break the case it exists to serve.

**A room is not its tunnel.** The tunnel, the NAT hole, the P2P paths and the engine itself are tooling: they drop, they get rebuilt, and none of them is the room. What the room IS survives on disk in three places — `hosted-room.json` on the host, `last-room.json` on each guest, and the pinned invite ID here. So the extreme case, every engine dying at once, host included, needs no code of its own: the host reopens under the same code, the lobby comes back up, and the guests return on their own clocks.

There is one deadline, and it is deliberately long:

| Host away for | What the code does |
|---|---|
| Any amount, up to 21 days | **Keeps resolving.** A guest reaches the lobby and waits on a host that is not there yet, or keeps retrying every five minutes |
| More than 21 days | `RoomTTL` ran out with nobody republishing, the sweep takes the entry, and the code answers that the room does not exist |

There used to be a second window here, a `CardTTL` of six hours, and it is gone. Its name said the card's life and its effect was the room's: a host whose VPS spent the night down came back to a code that had spent hours answering "no such room" to everybody holding it. A live room never gets near `RoomTTL`, because it republishes hourly.

**And the dial that costs the operating system's connect timeout is not a defect either.** With no host at the far end, a guest brings up the lobby and waits about twenty-one seconds on Windows. Bounding that would be the wrong fix: a slow link, a high RTT or a machine that is paging take that long with everything working, and cutting at five seconds turns a slow host into a missing one — hardest exactly where connections are worst. What was actually wrong was that the wait was mute, with the screen frozen on the previous line and a raw `connectex` string at the end. The dial announces itself now and says how long it can take.
