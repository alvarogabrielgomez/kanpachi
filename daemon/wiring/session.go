package wiring

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/core/port"
	"github.com/accentiostudios/kanpachi/core/usecase"
	"github.com/accentiostudios/kanpachi/daemon/adapter/canary/opener"
	catalogstore "github.com/accentiostudios/kanpachi/daemon/adapter/catalog/jsonfile"
	"github.com/accentiostudios/kanpachi/daemon/adapter/directory"
	kanpachiengine "github.com/accentiostudios/kanpachi/daemon/adapter/engine/kanpachi"
	"github.com/accentiostudios/kanpachi/daemon/adapter/identity"
	"github.com/accentiostudios/kanpachi/daemon/adapter/inspector"
	"github.com/accentiostudios/kanpachi/daemon/adapter/library/steam"
	"github.com/accentiostudios/kanpachi/daemon/adapter/netcfg"
	"github.com/accentiostudios/kanpachi/daemon/adapter/probe"
	"github.com/accentiostudios/kanpachi/daemon/adapter/router/igd"
	"github.com/accentiostudios/kanpachi/daemon/adapter/routes"
	"github.com/accentiostudios/kanpachi/daemon/adapter/sinimplementar"
	statestore "github.com/accentiostudios/kanpachi/daemon/adapter/state/jsonfile"
	"github.com/accentiostudios/kanpachi/daemon/adapter/sysevents"
	"github.com/accentiostudios/kanpachi/daemon/service/supervisor"
	"github.com/accentiostudios/kanpachi/daemon/transport/control"
)

// RealClock is the one [port.Clock] of every binary that wires real adapters.
// It used to be declared once per binary, under the same name, with the same
// single method.
type RealClock struct{}

func (RealClock) Now() time.Time { return time.Now() }

// Watchers are the observers of the machine: system events, the game library,
// the socket inspector and the router audit.
//
// Built SEPARATELY from the session so that the daemon can run its
// provisional-adapter guard between building them and building everything
// else: a binary carrying stubs must refuse to run as a service, and that
// check has to happen before the first real side effect, which is opening the
// firewall.
type Watchers struct {
	Events    port.SystemEvents
	Library   port.GameLibrary
	Inspector port.SocketInspector
	Router    port.ExposureAudit
}

// NewWatchers builds the real ones. None of them writes anything, so building
// and discarding them leaves no trace.
func NewWatchers(log port.Logger) Watchers {
	return Watchers{
		Events:    sysevents.New(log),
		Library:   steam.New(log),
		Inspector: inspector.New(),
		Router:    igd.New(log),
	}
}

// Stubbed asks which of the chosen ones do not really exist yet. The list is
// asked of the WIRING and not of a constant: a stub that comes back betrays
// itself. See [sinimplementar.Provisional].
func (w Watchers) Stubbed() []string {
	return sinimplementar.Names(w.Events, w.Library, w.Inspector, w.Router)
}

// SessionParams is what differs between the binaries that build a real
// session. **The zero value of every optional field means "what the product
// does"**: a tool states its deviations explicitly, so that nothing in the
// product can be loosened "for testing" without it reading that way in a diff.
type SessionParams struct {
	// DataDir is where identity.key, the sealed state and the catalog live.
	DataDir string
	// LogDir is where the ENGINE writes its own log, next to the caller's.
	LogDir string
	// EngineExe is the full path to the engine binary. Callers resolve it next
	// to their own executable, never from PATH: a PATH somebody can write is a
	// way for an elevated process to run another binary with that name.
	EngineExe string
	// BuiltinCatalogDir is where the catalog that ships with the app lives.
	BuiltinCatalogDir string
	// Protect puts a file its own ACL. The daemon passes the real one; a tool
	// whose data directory has no ACL to inherit passes a documented no-op.
	// Never nil: `identity` refuses to create the key without one.
	Protect identity.Protector
	// OnEngineFatal reports that this MACHINE cannot run the engine. Nil means
	// nobody to tell, which is a tool's case; the daemon wires the UI here.
	OnEngineFatal func(error)
	// SeedOverride, when set, wins over the registry saved on disk.
	SeedOverride string
	// KillOrphans kills leftover engine processes from a previous run before
	// anything else. Tools that die dirty often want it; the service does not.
	KillOrphans bool
	// CheckMachine probes at startup whether this machine can build a virtual
	// adapter, reporting through OnEngineFatal. 678 ms measured.
	CheckMachine bool

	Watchers Watchers
	Log      port.Logger
}

// Built is everything a startup gets back, plus the closers in BUILD order:
// callers run them in reverse, which is the order both binaries already used.
//
// The control channel is NOT among the closers, matching what the daemon does;
// the probe closes it itself. What IS here: the firewall and the engine, in
// the order that closes the engine first — the virtual network goes away, and
// only then the rules that contained it are released.
type Built struct {
	Session     *usecase.Session
	Engine      *kanpachiengine.Engine
	Control     *control.Channel
	Supervisor  *supervisor.Supervisor
	State       *statestore.Store
	Directories *directory.Factory
	Journal     *usecase.Journal
	// Fingerprint is this installation's, printed or logged by the caller. The
	// private key never leaves this function.
	Fingerprint string
	// OwnSeed is the registry this machine opens rooms on, already resolved
	// from the override or the disk. Empty is normal and not an error.
	OwnSeed string
	Closers []func()
}

// BuildSession wires a real session the one way there is.
//
// # Why this exists
//
// Two binaries build a session out of real adapters: the daemon, which is the
// product, and roomprobe, which measures the product. When this was written
// twice, the copies drifted three times in one week — the probe hosted without
// signing, read its registry from a flag while its screen read the disk, and
// kept its state unsealed with no tokens. Every one of those was a fix applied
// to one side. The ORDER also lives here and is load-bearing: the firewall
// opens first because [usecase.NewSession] purges inside its constructor, and
// `Attach` closes the circular dependency that once was written, tested and
// called by nobody.
func BuildSession(ctx context.Context, p SessionParams) (Built, error) {
	out := Built{}
	log := p.Log

	fw, audit, cerrarFw, err := NewFirewall(p.DataDir, log, p.Watchers.Router)
	if err != nil {
		return out, err
	}
	out.Closers = append(out.Closers, func() { _ = cerrarFw() })

	// Diary of long operations. Made here and not inside the session because
	// the ADAPTERS write to it too: only the engine adapter knows the engine
	// process just started, or that the virtual adapter took twelve seconds to
	// get its address.
	out.Journal = usecase.NewJournal(RealClock{}, log)

	out.Engine, err = kanpachiengine.New(kanpachiengine.Deps{
		Exe:      p.EngineExe,
		Log:      log,
		LogDir:   p.LogDir,
		Progress: out.Journal,
		OnFatal:  p.OnEngineFatal,
	})
	if err != nil {
		return out, fmt.Errorf("preparando el motor: %w", err)
	}
	out.Closers = append(out.Closers, func() { _ = out.Engine.Close() })

	if p.KillOrphans {
		// An orphaned engine from a previous run holds kanpachi0, and the
		// symptom would be a CreateRoom failing over an adapter that exists.
		if n := out.Engine.KillOrphans(); n > 0 {
			log.Warn("se mataron motores huérfanos de una corrida anterior", "cantidad", n)
		}
	}

	// The long-term key, and the control channel built AFTER it: it is the
	// host signing what it hands out at the lobby door, so without the key the
	// channel would come up unable to sign and nobody would notice.
	llave, err := identity.LoadOrCreate(p.DataDir, p.Protect)
	if err != nil {
		return out, err
	}
	out.Fingerprint = domain.Fingerprint(llave.Public().(ed25519.PublicKey))
	claveDelEstado, err := identity.StateKey(llave)
	if err != nil {
		return out, err
	}

	out.Control = control.New(control.Deps{
		Clock:    RealClock{},
		Log:      log,
		Identity: ControlIdentity(llave),
	})

	// The store is named instead of built inside `usecase.Deps` because the
	// registry factory below has to read from it BEFORE the session exists.
	//
	// SEALED for every binary, the tools included. The old excuse — "tools have
	// no identity to derive a key from" — stopped being true the moment the
	// probe started signing, and an unsealed known-hosts book is a book an
	// unprivileged process can plant an entry in, which is the attack the book
	// exists to detect.
	out.State = statestore.NewSealed(p.DataDir, claveDelEstado)

	out.OwnSeed = p.SeedOverride
	if out.OwnSeed == "" {
		out.OwnSeed = SeedFromDisk(out.State, log)
	}

	// Antes de la sesión, porque la sesión ya contesta el nombre y tiene que
	// contestar el bueno desde la primera vez que se le pregunte. Es de una
	// sola pasada: con el perfil escrito no vuelve a mirar los ficheros viejos.
	AdoptLegacyNickname(out.State, p.DataDir, thisHostname(), log)

	// One registry client per seed. `Tokens` is the SAME sealed store that
	// keeps the room, so the refresh token of a closed seed survives a restart
	// for every binary — the probe used to drop it and ask for the password on
	// every run.
	out.Directories = directory.NewFactory(directory.Deps{
		DataDir: p.DataDir,
		Log:     log,
		Protect: p.Protect,
		Tokens:  out.State.Protect(p.Protect),
	}, out.OwnSeed)

	if p.CheckMachine {
		// The error is not looked at on purpose: whoever shows it and shuts
		// down is OnEngineFatal, which is already wired and is the one place
		// that decides. The startup is NOT aborted from here: aborting would
		// kill the channel through which `doctor` and the window explain what
		// happened, right when it needs explaining.
		_ = out.Engine.CheckMachine()
	}

	// NewSession PURGES the firewall before returning, so from here on the
	// machine is in the state this startup decided, not the one the previous
	// run left. The purge living inside the constructor is what makes it
	// unskippable.
	out.Session, err = usecase.NewSession(ctx, usecase.Deps{
		Engine:      out.Engine,
		Firewall:    fw,
		Quarantine:  Quarantine,
		NetCfg:      netcfg.New(p.DataDir, log),
		Routes:      routes.New(),
		Store:       catalogstore.New(p.BuiltinCatalogDir, p.DataDir, log),
		State:       out.State,
		Library:     p.Watchers.Library,
		Directories: out.Directories,
		Control:     out.Control,
		Audit:       audit,
		Inspector:   p.Watchers.Inspector,
		Prober:      probe.New(),
		Canary:      opener.New(log),
		Clock:       RealClock{},
		Rand:        rand.Reader,
		Log:         log,
		Progress:    out.Journal,
		Hostname:    thisHostname(),
	})
	if err != nil {
		return out, err
	}

	// The channel and the session join HERE: the dependency is circular by
	// nature. Without this, `Serve` returns ErrNotAttached and creating a room
	// fails whole with the engine already up. It happened with the real
	// daemon: `Attach` existed, was tested, and only the tests called it.
	out.Control.Attach(out.Session)

	out.Supervisor, err = supervisor.New(supervisor.Deps{
		Room:    out.Session,
		Engine:  out.Engine,
		Control: out.Control,
		System:  p.Watchers.Events,
		Log:     log,
	})
	if err != nil {
		return out, err
	}

	// The mesh watcher asks the ENGINE who is in the room's network and notes
	// every change. It is the only datum separating two failures that look
	// identical from outside and are fixed in opposite ways: "the firewall
	// does not let it through" and "there is no path yet". It goes over the
	// engine's pipe, so it stays alive while the session holds its lock, which
	// is exactly when it is needed.
	vigía := &supervisor.VigiaDeMalla{Motor: out.Engine, Log: log}
	go vigía.Correr(ctx)

	return out, nil
}
