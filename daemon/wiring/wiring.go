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
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

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

// thisHostname is the machine's name, and it never fails.
//
// It exists so `core` keeps not importing `os`: what the session receives is a
// string, and turning it into a legal nickname is the domain's job. See
// [domain.NicknameFromHost].
func thisHostname() string {
	h, err := os.Hostname()
	if err != nil {
		return ""
	}
	return h
}

// ProfileStore is the sliver of the state store the adoption below needs.
type ProfileStore interface {
	LoadProfile() ([]byte, error)
	SaveProfile([]byte) error
}

// AdoptLegacyNickname moves the name from wherever a face used to keep it into
// the file the daemon owns, exactly once, and then removes the old one.
//
// # What it is repairing
//
// Until the daemon owned the name, each face kept its own: the window wrote the
// `nickname` key of `ui-prefs.json` and the terminal wrote `nickname.txt`, both
// in this same directory. A machine that used both ended up with two names, and
// the room showed whichever face entered it. Measured on 2026-08-18.
//
// # Why the derived one is thrown away
//
// Because the terminal wrote its GUESS to disk — the machine's own name,
// cleaned up — and once written it stopped being distinguishable from a name
// somebody chose. That is the defect, more than the duplication: it is why
// `AlvaroGDeskt` beat `Alvaro`. Anything identical to what this machine would
// derive today is treated as a guess and dropped.
//
// # Why the window wins the tie
//
// Because it is the one a person typed into a field. The terminal's file could
// also hold a typed name, from `--nick`, and there is no way to tell the two
// apart once on disk; between a value that is certainly chosen and one that may
// be a leftover, the certain one wins.
//
// It runs only when there is no profile yet, so it costs one failed read per
// start after the first. Nothing here fails a startup: a machine that ends up
// with no name gets asked for one, which is the same place a fresh install
// starts from.
func AdoptLegacyNickname(store ProfileStore, dataDir, hostname string, log port.Logger) {
	if raw, err := store.LoadProfile(); err == nil && !domain.ParseProfile(raw).Nick.IsZero() {
		return
	}

	derivado := domain.NicknameFromHost(hostname)
	limpiar := func(candidato, origen string) (domain.Nickname, string, bool) {
		nick, err := domain.ParseNickname(strings.TrimSpace(candidato))
		if err != nil {
			return domain.Nickname{}, "", false
		}
		if nick.String() == derivado.String() {
			return domain.Nickname{}, "", false
		}
		return nick, origen, true
	}

	viejo := filepath.Join(dataDir, "nickname.txt")
	var elegido domain.Nickname
	var origen string
	if b, err := os.ReadFile(filepath.Join(dataDir, "ui-prefs.json")); err == nil {
		var prefs struct {
			Nickname string `json:"nickname"`
		}
		if json.Unmarshal(b, &prefs) == nil {
			if nick, de, ok := limpiar(prefs.Nickname, "ui-prefs.json"); ok {
				elegido, origen = nick, de
			}
		}
	}
	if elegido.IsZero() {
		if b, err := os.ReadFile(viejo); err == nil {
			if nick, de, ok := limpiar(string(b), "nickname.txt"); ok {
				elegido, origen = nick, de
			}
		}
	}

	if !elegido.IsZero() {
		raw, err := domain.EncodeProfile(domain.Profile{Nick: elegido})
		if err == nil && store.SaveProfile(raw) == nil {
			log.Info("se adoptó el nombre que guardaba una cara",
				"apodo", elegido.String(), "de", origen)
		}
	}

	// El fichero del CLI se borra pase lo que pase: con el nombre adoptado ya
	// no hace falta, y sin adoptar era el nombre derivado, o sea nada que
	// perder. Dejarlo sería dejar un segundo escritor a la vista. El de la
	// ventana NO se toca: sigue guardando el tamaño de la ventana y la
	// narración, y quien le quita la clave del nombre es ella misma.
	if err := os.Remove(viejo); err != nil && !os.IsNotExist(err) {
		log.Warn("no se pudo retirar el nickname.txt viejo", "error", err)
	}
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
	QuarantineState(ctx context.Context) (domain.QuarantineState, error)
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

func (e Exposure) QuarantineState(ctx context.Context) (domain.QuarantineState, error) {
	return e.FW.QuarantineState(ctx)
}
