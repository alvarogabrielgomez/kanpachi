// Package directory is the client of the seed's room registry.
//
// # What it is for, and what it is not for
//
// It publishes the room's presentation card and asks the registry to mint
// invite IDs. That is ALL it is for. Nothing here is on the path of entering a
// room: the rendezvous identity is derived locally from the invite ID, so
// joining works with this whole server down. Losing it costs the invitation
// page showing a room name instead of the generic card.
//
// # What it refuses to do
//
// The refusals are the interesting part, and `docs/03-arquitectura.md` asks for
// them by name: fixed scheme and port, no redirects followed, caps on time and
// on response size, `CheckSeedAddr` over every resolved address on EVERY use,
// no proxy from the environment, and no retries. See `client.go` for each one.
//
// It receives and returns the card as OPAQUE BYTES. The sealing happens in the
// domain and the key never leaves the host's machine. A port that spoke of
// cards in the clear would force this adapter to decide what to encrypt them
// with, and that is where it would leak.
package directory

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"sync"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/core/port"
	"github.com/accentiostudios/kanpachi/daemon/adapter/identity"
)

// Deps is what the adapter needs. Same shape as the engine adapter's.
type Deps struct {
	// DataDir is where identity.key lives, next to the other state.
	DataDir string
	// Seed is the registry's host NAME, with no scheme and no port. A name and
	// never an address: see domain.ParseRoom, which refuses the other thing.
	Seed string
	Log  port.Logger
	// Protect puts identity.key's own ACL on it. Demanded, never defaulted.
	Protect identity.Protector
	// Resolve is injected to be able to test the rejection of a seed pointing
	// at the home network without depending on anybody's DNS. Nil uses the real
	// resolver.
	Resolve func(host string) ([]netip.Addr, error)

	// The test hooks, unexported on purpose so that no caller outside this
	// package can reach them. Same trick as engine.Deps.spawn.
	//
	// base overrides scheme and host; connect replaces the raw TCP dial that
	// happens AFTER resolving and checking, so a test drives the production
	// path whole, address check included.
	base    string
	connect connectFunc
}

// Directory implements port.RoomDirectory against kanpseed.
type Directory struct {
	deps   Deps
	base   string
	client *http.Client

	// The key is loaded on first use rather than in New, because a machine that
	// never hosts a room never needs one, and creating it eagerly would put a
	// file with its own ACL on disk for a user who only ever joins.
	mu   sync.Mutex
	priv ed25519.PrivateKey
}

var _ port.RoomDirectory = (*Directory)(nil)

// New builds the adapter. It does not touch the network or the disk.
func New(d Deps) (*Directory, error) {
	switch {
	case d.DataDir == "":
		return nil, errors.New("el cliente del registro necesita el directorio de datos")
	case d.Seed == "":
		return nil, errors.New("el cliente del registro necesita saber con qué seed habla")
	case d.Log == nil:
		return nil, errors.New("el cliente del registro necesita un log")
	case d.Protect == nil:
		return nil, errors.New("el cliente del registro necesita quien le ponga los permisos " +
			"a la llave de esta instalación")
	}

	if d.Resolve == nil {
		d.Resolve = resolveWithNet
	}
	if d.connect == nil {
		d.connect = (&net.Dialer{Timeout: dialTimeout}).DialContext
	}
	base := d.base
	if base == "" {
		// https and nothing else. The card is presentation, and presentation
		// that arrives tampered with is still a lie on somebody's screen.
		base = "https://" + d.Seed
	}

	dir := &Directory{deps: d, base: base}
	dir.client = newClient(func(ctx context.Context, network, addr string) (net.Conn, error) {
		return dialVerified(ctx, addr, d.Resolve, d.connect, network)
	})
	return dir, nil
}

func resolveWithNet(host string) ([]netip.Addr, error) {
	return net.DefaultResolver.LookupNetIP(context.Background(), "ip", host)
}

// Seed is the registry this adapter talks to.
//
// It exists so that a caller can tell whether a "not found" is about the right
// registry at all: an invite ID only means something in the registry that
// minted it. See [port.RoomDirectory.Seed].
func (d *Directory) Seed() string { return d.deps.Seed }

// Open asks the registry to mint an invite ID for this sealed card.
func (d *Directory) Open(ctx context.Context, sealed []byte) (domain.Room, error) {
	cuerpo, err := d.firmar(sealed)
	if err != nil {
		return domain.Room{}, err
	}
	var out openedBody
	if err := d.do(ctx, http.MethodPost, "/api/rooms", cuerpo, &out, http.StatusCreated); err != nil {
		return domain.Room{}, err
	}
	id, err := domain.ParseInviteID(out.InviteID)
	if err != nil {
		return domain.Room{}, fmt.Errorf("el registro emitió %q, que no tiene forma de código: %w",
			out.InviteID, err)
	}
	// The seed is pasted on here and not by the use case: an invite ID only
	// means something in the registry that minted it, and this adapter is what
	// knows which registry that is.
	return domain.Room{InviteID: id, Seed: d.deps.Seed}, nil
}

// Lookup returns the sealed card and how many people are inside.
//
// An absent count comes back as -1 and never as 0. The registry omits the
// number when it has never managed to talk to the engine, and a zero would
// turn "I do not know" into "there is nobody", which is a different claim and a
// false one.
func (d *Directory) Lookup(ctx context.Context, id domain.InviteID) ([]byte, int, error) {
	var out lookupBody
	if err := d.do(ctx, http.MethodGet, "/api/i/"+id.Raw(), nil, &out, http.StatusOK); err != nil {
		return nil, -1, err
	}
	sealed, err := deB64(out.Card)
	if err != nil {
		return nil, -1, fmt.Errorf("la tarjeta que devolvió el registro no es base64url: %w", err)
	}
	if len(sealed) > domain.MaxCardBytes {
		return nil, -1, fmt.Errorf("la tarjeta que devolvió el registro mide %d bytes y el tope es %d",
			len(sealed), domain.MaxCardBytes)
	}
	miembros := -1
	if out.Members != nil {
		miembros = *out.Members
	}
	return sealed, miembros, nil
}

// Publish updates the card of a room this installation already owns.
//
// It does NOT create anything: the registry answers that it does not know the
// room when the invite ID was never minted, or when its pin already expired and
// the entry was swept. That is the shape of the promise "or reopens the room
// with the same invite ID" — it holds while the pin is alive, and the pin is
// what stops an ex member who kept the code from getting there first.
func (d *Directory) Publish(ctx context.Context, id domain.InviteID, sealed []byte) error {
	cuerpo, err := d.firmar(sealed)
	if err != nil {
		return err
	}
	return d.do(ctx, http.MethodPut, "/api/i/"+id.Raw(), cuerpo, nil, http.StatusNoContent)
}

// Reachable asks the registry whether it is answering at all.
//
// `/healthz` and not any of the room endpoints, on purpose: it carries no
// invite ID, so it cannot be confused with asking about a room, and it is the
// same endpoint the registry's own watchdog uses to decide it is alive. See
// [port.RoomDirectory.Reachable].
//
// The body is discarded and only the status is read. What matters is that
// something on the other side spoke HTTP.
func (d *Directory) Reachable(ctx context.Context) error {
	return d.do(ctx, http.MethodGet, "/healthz", nil, nil, http.StatusOK)
}

// firmar signs the sealed card with this installation's key.
//
// The signature is over the RAW bytes of the sealed blob, which is what the
// registry verifies. It is what closes the hole encryption cannot: everyone who
// received the invitation link can produce a card the page will decrypt, so the
// registry cannot tell a member from the host by the card alone. What it can do
// is demand the signature of the key that pinned that invite ID first.
func (d *Directory) firmar(sealed []byte) (publishBody, error) {
	priv, err := d.llave()
	if err != nil {
		return publishBody{}, err
	}
	pub, ok := priv.Public().(ed25519.PublicKey)
	if !ok {
		return publishBody{}, errors.New("la llave de esta instalación no es Ed25519")
	}
	return publishBody{
		HostKey: b64(pub),
		Card:    b64(sealed),
		Sig:     b64(ed25519.Sign(priv, sealed)),
	}, nil
}

// llave loads the installation key once, under the lock.
//
// The lock matters beyond tidiness: two calls racing here would both find no
// file and both generate, and the loser's key would be the one left on disk
// while the winner's already signed something. The registry would then pin a
// key this machine no longer has.
func (d *Directory) llave() (ed25519.PrivateKey, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.priv != nil {
		return d.priv, nil
	}
	priv, err := identity.LoadOrCreate(d.deps.DataDir, d.deps.Protect)
	if err != nil {
		return nil, err
	}
	d.priv = priv
	return priv, nil
}
