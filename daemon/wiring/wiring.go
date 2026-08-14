// Package wiring holds the pieces of dependency wiring that MORE THAN ONE
// binary needs, so that there is one of each instead of one per binary.
//
// # Why it exists
//
// Two things build a real session out of real adapters: `daemon/cmd/kanpachid`,
// which is the product, and `internal/roomprobe`, which measures the product
// without the service or the installer. Both used to write these three by hand,
// and the copies drifted the way copies do — the probe hosted without signing,
// and read its registry from a flag while its own screen read it from disk, so
// the header said one thing and creating a room said another.
//
// The rule this package encodes: **a fix to the wiring has to land in one
// place.** What stays out of here is anything that CHOOSES a concrete adapter
// per operating system; that lives next to each binary, where the build tags
// are.
package wiring

import (
	"context"
	"crypto/ed25519"
	"errors"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/core/port"
	statestore "github.com/accentiostudios/kanpachi/daemon/adapter/state/jsonfile"
	"github.com/accentiostudios/kanpachi/daemon/transport/control"
)

// SeedReader is the sliver of the state store this needs.
//
// An interface and not the concrete store because that is all it reads, and
// because a test double for it is two lines instead of a directory.
type SeedReader interface {
	LoadSeed() ([]byte, error)
}

// SeedFromDisk reads the registry where THIS machine opens rooms, and NEVER
// fails a startup over it.
//
// # Why empty is not an error
//
// Because it is the normal state of an installation that has neither hosted nor
// joined anything. The registry is learnt from one of those two, and until one
// happens, not having it is correct. Whoever tries to create a room runs into
// `port.ErrNoOwnSeed`, which leads to configuring it rather than to retrying.
//
// # Why an unreadable file does not stop the start either
//
// Same reason an unreadable room file does not stop the seed's registry: a
// daemon that does not start is worse than one with no registry configured,
// because with it go the window, `doctor`, and the channel through which any of
// this gets explained. It is said loudly in the log and the start goes on.
//
// **It goes through the domain's parser and not through a TrimSpace.** Out of
// this value come a URL this process dials and the engine's `--peers`, and this
// process runs as SYSTEM or as root. On Windows the file also lives in a
// directory every user of the machine can read, so it deserves the same
// suspicion as a pasted link. See [domain.ParseOwnSeed].
func SeedFromDisk(store SeedReader, log port.Logger) string {
	raw, err := store.LoadSeed()
	if err != nil {
		if !errors.Is(err, statestore.ErrNoState) {
			log.Warn("no se pudo leer el registro guardado, se arranca sin ninguno", "error", err)
		}
		return ""
	}
	seed, err := domain.ParseOwnSeed(raw)
	if err != nil {
		log.Error("el registro guardado no es un nombre válido, se ignora", "error", err)
		return ""
	}
	return seed
}

// ControlIdentity turns this installation's long-term key into what the control
// channel needs to sign the lobby answer.
//
// **The private key is not handed over, a signer is.** One package creates
// `identity.key` and everything else consumes it; passing the key itself down
// the wiring would spread the very thing that package exists to guard. See
// decision 25.
func ControlIdentity(priv ed25519.PrivateKey) control.Identity {
	if len(priv) == 0 {
		return control.Identity{}
	}
	return control.Identity{
		Public: priv.Public().(ed25519.PublicKey),
		Sign:   func(msg []byte) []byte { return ed25519.Sign(priv, msg) },
	}
}

// FirewallAudit is the half of the audit the firewall answers.
type FirewallAudit interface {
	FirewallEnabled(ctx context.Context) ([]domain.FirewallProfileState, error)
	Enforcement(ctx context.Context) (domain.Enforcement, error)
}

// Exposure joins the two halves of the audit WITHOUT embedding them.
//
// Explicit rather than embedded, even though it costs three more lines:
// embedding the firewall would promote `Apply` and `PurgeOwned` onto the object
// whose only job is to measure, and an audit with methods that modify is the
// opposite of an audit.
//
// It exists because the firewall adapter refuses to implement the whole port:
// answering `nil, nil` to the router's question would make "there are no
// mappings" and "nobody looked" indistinguishable, on the one screen whose job
// is telling those two apart.
type Exposure struct {
	FW     FirewallAudit
	Router port.ExposureAudit
}

var _ port.ExposureAudit = Exposure{}

func (e Exposure) FirewallEnabled(ctx context.Context) ([]domain.FirewallProfileState, error) {
	return e.FW.FirewallEnabled(ctx)
}

func (e Exposure) Enforcement(ctx context.Context) (domain.Enforcement, error) {
	return e.FW.Enforcement(ctx)
}

func (e Exposure) RouterMappings(ctx context.Context) ([]domain.PortMapping, error) {
	return e.Router.RouterMappings(ctx)
}
