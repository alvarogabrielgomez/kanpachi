package cli

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/AlecAivazis/survey/v2"
	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/registry"
	"github.com/accentiostudios/kanpachi/registry/setup"
)

// `kanpseed password` closes the seed, or reopens it.
//
// # What closing does, and what it does not
//
// It asks for a password to HOST: opening a room, publishing to one, renewing
// its code. Entering a room is never asked for anything, on any seed. The
// friction of the guest is what this product exists to remove, and hosting
// already assumes a more technical role.
//
// One shared password and no accounts. There is nobody to revoke individually,
// and the operator's way of throwing everybody out is to change it.
//
// # Why there is no --password flag
//
// Because on Linux any user reads /proc/<pid>/cmdline, and the shell keeps a
// history. A flag would put the password of the seed in two places that outlive
// the command. It is typed, masked, or it does not get set.
func cmdPassword(args []string) error {
	fs := flag.NewFlagSet("password", flag.ContinueOnError)
	open := fs.Bool("open", false, "removes the password and leaves the seed open to anyone")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if err := requiereLinux(); err != nil {
		return err
	}
	if err := requiereRoot("password"); err != nil {
		return err
	}

	cfg, err := setup.Cargar()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errors.New("there is no seed installed here. Run `kanpseed init` first")
		}
		return err
	}

	auth, err := registry.OpenAuth(setup.DirState)
	if err != nil {
		return err
	}

	if *open {
		return openTheSeed(auth)
	}
	return closeTheSeed(auth, cfg)
}

// closeTheSeed asks for the password and stores it.
func closeTheSeed(auth *registry.Auth, cfg setup.Config) error {
	// The domain is not decoration any more, and this is where it stops being
	// so. It is bound INSIDE the hash the client sends, so a proof captured on
	// one seed is worth nothing on another. That also means the name stored
	// here has to be the name people type: get it wrong and every host gets a
	// wrong password with a correct password.
	if cfg.Dominio == "" {
		return errors.New("this seed has no domain configured, and the password is bound to it.\n" +
			"Run `kanpseed config` and set the name your users type, then try again")
	}
	if !interactivo() {
		return errors.New("there is no terminal to type a password into.\n" +
			"There is no --password flag on purpose: the argv of a process is world readable")
	}

	banner("Kanpachi seed", "the password to HOST on this seed")
	if auth.Closed() {
		aviso("this seed already has a password, and this replaces it")
	}

	// The warning goes BEFORE the prompt, not after. Said afterwards it would
	// be a report of what already happened.
	tenue("  Every host signed into this seed is thrown out the moment this")
	tenue("  changes: the signing key is rotated, so every token stops being")
	tenue("  valid at once. They get back in by typing the new password.")
	fmt.Println()
	tenue("  The password is bound to %s. Anyone who reaches this seed by", cfg.Dominio)
	tenue("  another name will not be able to host on it.")
	fmt.Println()
	// La salida sin password se dice ACÁ y no solo en la ayuda del comando.
	// Quien llegó a este prompt ya está decidiendo, y no va a irse a leer
	// `kanpseed help` para enterarse de que existe una bandera.
	tenue("  Leave it empty to have no password: anyone who reaches this seed")
	tenue("  can host on it. Same as `kanpseed password --open`.")
	fmt.Println()

	pw, err := askForPassword()
	if err != nil {
		return err
	}
	// Vacío es "ninguna", y no un password que no cumple la regla.
	//
	// El prompt es el sitio donde alguien dice «no quiero password», y hasta
	// ahora ahí lo único que había era un callejón: la regla lo rechazaba por
	// corto, sin decir que la salida se llama `--open`. Enter es la respuesta
	// natural, así que Enter hace lo que significa.
	//
	// Lo hace por el MISMO camino que la bandera, así que el aviso de que se
	// abre a cualquiera y la confirmación son los de siempre.
	if pw == "" {
		return openTheSeed(auth)
	}

	if err := auth.SetPassword(cfg.Dominio, pw); err != nil {
		return err
	}
	if err := matchOwner(auth.Path(), setup.DirState); err != nil {
		return err
	}
	ok("password stored in %s", auth.Path())
	return reloadTheService()
}

// openTheSeed removes the credential.
func openTheSeed(auth *registry.Auth) error {
	if !auth.Closed() {
		ok("this seed is already open: anyone can host on it")
		return nil
	}
	aviso("this leaves the seed open: anyone who reaches it can host rooms on it")
	if interactivo() && !confirmar("Open the seed?", false) {
		tenue("  cancelled")
		return nil
	}
	if err := auth.Open(); err != nil {
		return err
	}
	ok("password removed, and every issued token died with it")
	return reloadTheService()
}

// askForPassword asks twice and masks both times. Empty means none.
//
// Twice because a typo in something that is never echoed locks the operator out
// of their own seed, and the only way back is running this command again on the
// machine. Cheap insurance against an expensive walk.
//
// Empty comes back as empty and NOT as a rule violation, and the caller reads it
// as "no password". The length rule still applies to everything else: four is
// already low, and the only thing below it that means anything is nothing.
func askForPassword() (string, error) {
	var pw string
	question := &survey.Password{
		Message: fmt.Sprintf("Password for hosting (%d-%d characters, empty for none):",
			domain.MinSeedPasswordLen, domain.MaxSeedPasswordLen),
	}
	if err := survey.AskOne(question, &pw); err != nil {
		return "", err
	}
	if pw == "" {
		// No se pide dos veces: no hay nada que confirmar en una cadena vacía, y
		// lo que sí hay que confirmar —que el seed queda abierto— lo pregunta
		// [openTheSeed], que es adonde va esto.
		return "", nil
	}
	if err := domain.ValidateSeedPassword(pw); err != nil {
		return "", err
	}

	var again string
	if err := survey.AskOne(&survey.Password{Message: "Type it again:"}, &again); err != nil {
		return "", err
	}
	if pw != again {
		return "", errors.New("the two do not match, and nothing was changed")
	}
	return pw, nil
}

// reloadTheService makes the running registry pick the credential up.
//
// A reload and not a restart: `kanpseed password` is a different process and
// cannot write into the memory of the one that is serving, and the unit's
// BindsTo would take the engine down with a restart. The SIGHUP that already
// re-reads the page re-reads this too.
//
// A registry that is not running is not a failure here: the credential is on
// disk, and it gets read on the next start.
func reloadTheService() error {
	if _, err := os.Stat(filepath.Join(setup.DirUnits, setup.UnitReg)); err != nil {
		tenue("  the registry is not installed as a service, so nothing to reload")
		return nil
	}
	if err := setup.Systemctl("reload", setup.UnitReg); err != nil {
		aviso("the registry did not reload: %v", err)
		tenue("  the credential is on disk and will be read on the next start")
		return nil
	}
	ok("the registry reloaded and is using it now")
	return nil
}
