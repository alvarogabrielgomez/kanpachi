package main

// `kanpachi upgrade`: fetching the new version of the client.
//
// # Upgrading is NOT replacing a binary
//
// Here it is the daemon, the engine, the CLI, the two systemd units and the
// quarantine, and they have to agree with each other. **The engine and the
// daemon always travel together**: the engine's protocol decodes strictly, so a
// new daemon sending `ipv4` to an old engine gets the whole message rejected and
// the room does not come up. Changing one piece on its own does not give a clear
// error: it gives a machine that announces one version and behaves like another.
//
// That is why this writes NO files: it downloads the package, verifies it, and
// hands it to `apt`. The package is what knows how to place all six things at
// once.
//
// # Why the binary does not get replaced in place, the way the seed does
//
// Because the Linux client comes from a `.deb`. Overwriting a file dpkg believes
// is its own leaves its database lying, and the next `apt upgrade` writes over it
// without a word, handing back the old version with nobody asking for it. The
// seed can do it because an `install.sh` that copies files installed it, and
// there is no database to contradict.
//
// # Which version is out there: the CHANNEL gets asked, and the channel is GitHub
//
// And never the room's registry, even though it used to be the other way round.
// Anybody has been able to stand a registry up since it stopped shipping
// compiled, and believing it about which version exists means letting somebody
// else's machine choose which one you take. See [latestPublished], and
// [selfupdate.Repo] for moving it in a fork.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/term"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/daemon/adapter/state/jsonfile"
	"github.com/accentiostudios/kanpachi/daemon/transport/protocol"
	"github.com/accentiostudios/kanpachi/internal/selfupdate"
)

// SumsFile is the CLIENT's manifest, which is not the seed's.
//
// The three workflows write into the same release, so all three names have to
// differ or the last one to upload writes over the others. See
// `registry/selfupdate`, which kept `SHA256SUMS-seed-linux` precisely to leave
// this name free: the client's gets downloaded by strangers from the page.
const SumsFile = "SHA256SUMS-linux"

// DebFile is the package, with no version in the name.
//
// Because the page points at `releases/latest/download/<file>`, which is a
// permanent URL. The version travels inside, in the control file's `Version`
// field, which is where dpkg reads it from.
const DebFile = "kanpachi-amd64.deb"

func cmdUpgrade(ctx context.Context, op options, args []string) error {
	checkOnly := false
	noQuestions := false
	force := false
	asked := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--check", "-check":
			checkOnly = true
		case "--yes", "-yes", "-y":
			noQuestions = true
		case "--force", "-force":
			force = true
		case "--version", "-version":
			if i+1 >= len(args) {
				return badUsage("--version is missing the number, for example v0.2.0")
			}
			i++
			asked = args[i]
		default:
			return badUsage("upgrade does not understand %q. It takes --check, --version <v>, --force and --yes", args[i])
		}
	}

	// What this machine already found out, from whichever face asked last.
	//
	// **A shared answer and not a cache with a lifetime.** A published version
	// does not get unpublished, so this cannot go stale; what it saves is the
	// other face asking the same question of a channel that allows sixty an
	// hour PER IP, shared by everybody behind one router. The window has kept
	// this answer since it existed and the terminal asked again every time.
	//
	// It fails quietly. With no daemon there is no shared answer, and asking
	// the channel still works: a daemon that is down is not a reason to be
	// unable to upgrade.
	guardada := ""
	if !force {
		guardada = versiónPendienteGuardada(op)
	}
	if checkOnly && asked == "" && guardada != "" &&
		selfupdate.IsVersion(Version) && selfupdate.Outdated(Version, guardada) {
		fmt.Printf("%s is out, and this machine is on %s.\n", guardada, Version)
		fmt.Println("  Found earlier, here or in the window.")
		fmt.Println("  To ask the channel again:  kanpachi upgrade --check --force")
		return nil
	}

	tag := asked
	if tag == "" {
		var err error
		tag, err = latestPublished(ctx)
		if err != nil {
			return fmt.Errorf("could not find out which version is published: %w", err)
		}
		// Se apunta para la otra cara, con el mismo criterio que usa la
		// ventana: solo lo que de verdad va por delante de lo que corre.
		if selfupdate.IsVersion(Version) && selfupdate.Outdated(Version, tag) {
			guardarVersiónPendiente(op, tag)
		}
	}

	// The quiet shortcut only holds when comparing against the latest. With
	// --version it installs whatever was asked for even if it is the same or
	// older, which is what makes this command a way of going back.
	//
	// And with --force it does not hold either, because the question the
	// shortcut answers is not the one being asked sometimes. The shortcut
	// compares NUMBERS, and two different binaries can carry the same one: a
	// version republished over a fix leaves the deployed machine on the old
	// build and `upgrade` saying there is nothing to do. Measured on the droplet
	// with 0.6.2 on 2026-08-19, which had to be retagged and could not be picked
	// up any other way.
	if asked == "" && !force && selfupdate.IsVersion(Version) && !selfupdate.Outdated(Version, tag) {
		fmt.Printf("Already up to date (%s).\n", Version)
		fmt.Println("  `--force` installs it anyway, which is how a republished version is picked up.")
		return nil
	}

	if checkOnly {
		switch {
		case selfupdate.Outdated(Version, tag):
			fmt.Printf("There is a new version: %s → %s\n", Version, tag)
			fmt.Println("  sudo kanpachi upgrade")
		case !selfupdate.IsVersion(Version):
			// A hand-built binary. Saying "up to date" would be a lie and saying
			// "out of date" would be too, so it says what it knows.
			fmt.Printf("This binary is %s, so there is nothing to compare it against.\n", Version)
			fmt.Printf("  The latest published is %s\n", tag)
		default:
			fmt.Println("You are up to date.")
		}
		return nil
	}

	if err := canUpgradeHere(op); err != nil {
		return err
	}
	if !noQuestions && !hasTerminal() {
		// With no terminal there is nobody to ask, and assuming a yes would be
		// worse than refusing: inside a cron, restarting the service takes away
		// the room of people who were playing with nobody having asked for it.
		return badUsage("with no terminal you have to say --yes, and that restarts the service")
	}
	if !noQuestions {
		// With the same number on both sides, «installed X, available X» says
		// nothing and the question looks like a bug. What is going to happen is a
		// reinstall, and that is what it gets called.
		same := Version == tag
		if same {
			fmt.Printf("Installing %s over itself, which is what --force is for.\n", tag)
		} else {
			fmt.Printf("Installed %s, available %s.\n", Version, tag)
		}
		// The room gets named because restarting the service takes it down, and
		// whoever has people playing wants to pick the moment.
		fmt.Println("Upgrading restarts the service, so the room drops and has to be reopened.")
		question := fmt.Sprintf("Upgrade to %s?", tag)
		if same {
			question = fmt.Sprintf("Reinstall %s?", tag)
		}
		yes, err := confirm(question)
		if err != nil {
			return err
		}
		if !yes {
			fmt.Println("Cancelled.")
			return nil
		}
	}

	fmt.Printf("Downloading %s (%s)...\n", DebFile, tag)
	pkg, err := downloadAndVerify(ctx, tag)
	if err != nil {
		return err
	}
	fmt.Println("SHA256 verified.")

	// To a real file and not to memory: `apt-get install` takes a PATH, which is
	// what makes dpkg record the installation as its own. It goes to the data
	// directory and not to /tmp because anybody writes in /tmp, and this is a
	// package that is going to be installed as root a minute later.
	target := filepath.Join(op.data, DebFile)
	if err := os.WriteFile(target, pkg, 0o600); err != nil {
		return fmt.Errorf("saving the package to %s: %w", target, err)
	}
	defer func() {
		if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "kanpachi: the package was left at %s: %v\n", target, err)
		}
	}()

	fmt.Println("Installing with apt...")
	// Reinstall when the number does not go up. apt compares versions the same
	// way the shortcut above does and answers «already the newest version»
	// without touching anything, so skipping the CLI's shortcut and not apt's
	// leaves the command saying it installed without having installed.
	reinstall := force && !selfupdate.Outdated(Version, tag)
	if err := installPackage(ctx, target, reinstall); err != nil {
		return err
	}

	fmt.Printf("\nDone: %s.\n", tag)
	fmt.Println("  `kanpachi doctor` checks that everything landed where it should.")
	return nil
}

// latestPublished asks the UPDATE CHANNEL, which is GitHub.
//
// # Why the registry no longer gets asked, which is what used to happen
//
// It used to ask this machine's registry first and fall back to GitHub. That
// made sense while the registry was one machine and compiled in: it was a host
// this client talked to anyway, and its answer was cached for everybody.
//
// With registries anybody can run, that becomes **somebody else's machine
// deciding which version you install**. It cannot serve you a binary, because the
// package and its manifest both come from GitHub and get verified, and it can do
// two things: answer with an old tag, so `Outdated` says you are up to date and
// freezes you on your version in silence, or answer with a real release that is
// not the latest and send you to one with a known bug.
//
// The reason the detour existed was GitHub's quota, 60 an hour per IP, shared by
// a whole household. That argument belonged to the AUTOMATIC check, which asked
// several times a session with nobody requesting it. On demand, an `upgrade
// --check` somebody types spends one of sixty.
func latestPublished(ctx context.Context) (string, error) {
	return selfupdate.Latest(ctx)
}

// seedOfThisMachine answers this machine's registry, or the empty string.
//
// # What it is left for, if the version no longer gets asked of it
//
// For NAMING the download page on Windows, and for nothing else. That registry
// serves the page the installer came from for whoever installed from there, so it
// is the useful address to hand them. The version gets asked of the update
// channel, see [latestPublished].
//
// # The two sources, and why both are needed
//
// The open room first, which is the freshest data. And the daemon's file as a
// fallback, **read directly**: upgrading has to be possible with the service
// down, which is also when it is most needed, and there is nobody to ask then.
// That is why that file is the one piece of state that does not travel sealed,
// see `jsonfile.Store.LoadSeed`.
//
// Empty is not a failure: it is an install that has neither hosted nor entered
// any room, which means one that was just installed. There GitHub gets asked,
// which is where the running binary came from.
func seedOfThisMachine(op options) string {
	// The daemon not answering is NOT a failure here, see above.
	if st, err := currentMenuStatus(op); err == nil && st.Seed != "" {
		return st.Seed
	}
	raw, err := os.ReadFile(filepath.Join(op.data, jsonfile.SeedFile))
	if err != nil {
		return ""
	}
	// Through the domain's parser and not a `TrimSpace`: a URL this process is
	// going to dial comes out of this value, and a file on disk deserves the same
	// distrust as a pasted link. See [domain.ParseOwnSeed].
	seed, err := domain.ParseOwnSeed(raw)
	if err != nil {
		fmt.Fprintf(os.Stderr, "kanpachi: the saved registry is not a valid name, ignoring it: %v\n", err)
		return ""
	}
	return seed
}

func downloadAndVerify(ctx context.Context, tag string) ([]byte, error) {
	base := selfupdate.Base(tag)
	pkg, err := selfupdate.Get(ctx, base+"/"+DebFile, 200<<20)
	if err != nil {
		return nil, fmt.Errorf("downloading %s: %w", DebFile, err)
	}
	sums, err := selfupdate.Get(ctx, base+"/"+SumsFile, 1<<20)
	if err != nil {
		return nil, fmt.Errorf("could not download %s: without it nothing is verified and "+
			"nothing is installed: %w", SumsFile, err)
	}
	if err := selfupdate.Verificar(string(sums), SumsFile, DebFile, pkg); err != nil {
		return nil, err
	}
	return pkg, nil
}

// canUpgradeHere refuses before downloading anything.
//
// Three refusals and all three early: downloading forty megabytes to find out
// afterwards that there is no `apt` is spending somebody's bandwidth for
// nothing.
//
// It takes the options only for the first one: the download address comes from
// this machine's registry, and reading that means knowing where the state lives.
func canUpgradeHere(op options) error {
	if runtime.GOOS != "linux" {
		// The download page is served by a registry, so the address comes from
		// THIS machine's registry. With none configured there is no page to name,
		// and it points at the releases, which is where the running binary
		// definitely came from.
		where := selfupdate.Releases
		if seed := seedOfThisMachine(op); seed != "" {
			where = "https://" + seed + "/download"
		}
		return refuse("`upgrade` installs a `.deb`, so it only works on Linux.\n"+
			"  On Windows the update goes through the installer: %s", where)
	}
	if runtime.GOARCH != "amd64" {
		return refuse("the published package is %s and this machine is %s.\n"+
			"  Nothing is published for this architecture yet", DebFile, runtime.GOARCH)
	}
	if os.Geteuid() != 0 {
		return refuse("installing a package is root's job: sudo kanpachi upgrade")
	}
	return nil
}

// installPackage hands it to apt, which is what knows how to place the six
// pieces.
//
// `apt-get install ./path.deb` and not `dpkg -i`: apt resolves the dependencies
// in the package's control file, and dpkg stops dead if one is missing, leaving
// the system with a half-configured package. The path carries `./` on purpose,
// because without a slash apt would read it as the NAME of a repository package,
// and that name exists in none of them.
func installPackage(ctx context.Context, path string, reinstall bool) error {
	if !strings.HasPrefix(path, "/") && !strings.HasPrefix(path, "./") {
		path = "./" + path
	}
	return aptInstall(ctx, path, reinstall)
}

// hasTerminal says whether anybody is watching.
func hasTerminal() bool { return term.IsTerminal(int(os.Stdin.Fd())) }

// versiónPendienteGuardada pregunta al daemon qué versión nueva se sabe ya.
//
// Devuelve "" ante cualquier problema, y eso NO es tragarse un error: la
// respuesta guardada es un atajo, y no tenerlo solo cuesta una pregunta a la
// red. Fallar acá dejaría sin actualizar a quien tiene el daemon caído, que es
// justo una de las razones para actualizar.
func versiónPendienteGuardada(op options) string {
	c, err := dial(op)
	if err != nil {
		return ""
	}
	defer func() { _ = c.Close() }()
	raw, err := c.Call(protocol.MethodSettings, nil)
	if err != nil {
		return ""
	}
	var v struct {
		PendingUpdate string `json:"pending_update"`
	}
	if json.Unmarshal(raw, &v) != nil {
		return ""
	}
	return v.PendingUpdate
}

// guardarVersiónPendiente apunta lo encontrado para la otra cara.
//
// No falla el comando si no puede: quien escribió `upgrade` viene a
// actualizar, no a dejar una nota.
func guardarVersiónPendiente(op options, tag string) {
	c, err := dial(op)
	if err != nil {
		return
	}
	defer func() { _ = c.Close() }()
	_, _ = c.Call(protocol.MethodSettings, struct {
		PendingUpdate string `json:"pending_update"`
	}{tag})
}
