package cli

import (
	"fmt"
	"os"
	"runtime"

	"github.com/accentiostudios/kanpachi/registry/setup"
)

// Version la inyecta el enlazador al compilar. En un binario construido a mano
// queda en "dev", que es información útil y no un defecto.
var Version = "dev"

// Ejecutar despacha el subcomando. Devuelve el código de salida.
func Ejecutar(args []string) int {
	if len(args) == 0 {
		ayuda()
		return 2
	}
	var err error
	switch args[0] {
	case "serve":
		err = cmdServe(args[1:])
	case "init":
		err = cmdInit(args[1:])
	case "doctor":
		err = cmdDoctor(args[1:])
	case "config":
		err = cmdConfig(args[1:])
	case "password":
		err = cmdPassword(args[1:])
	case "nginx", "proxy":
		err = cmdNginx(args[1:])
	case "upgrade":
		err = cmdUpgrade(args[1:])
	case "reconfigure":
		err = cmdReconfigure(args[1:])
	case "uninstall":
		err = cmdUninstall(args[1:])
	case "version", "--version", "-v":
		fmt.Printf("kanpseed %s (EasyTier %s, %s/%s)\n", Version, setup.VersionEasyTier, runtime.GOOS, runtime.GOARCH)
		return 0
	case "help", "--help", "-h":
		ayuda()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", args[0])
		ayuda()
		return 2
	}

	if err != nil {
		fmt.Println()
		fallo("%v", err)
		return 1
	}
	return 0
}

func ayuda() {
	fmt.Printf(`kanpseed %s

  The meeting point of Kanpachi. It introduces peers to each other, resolves
  invite IDs and serves the invitation page.

Commands a person runs:

  init        installs and configures everything. One single run
  upgrade     updates to the latest published version
              --check reports whether there is a new one, installs nothing
  doctor      checks that everything is as it should be, and says what is missing
  config      shows or changes the ports, and rewrites the services
  password    asks for a password to HOST on this seed. Entering a room
              never asks for one. Changing it throws every host out
              --open removes it and leaves the seed open to anyone
  reconfigure rewrites the services the way this version wants them, and
              restarts. 'upgrade' does it on its own; by hand it undoes a
              unit that was edited
  nginx       reprints the block to paste into the reverse proxy
  uninstall   removes the services and the binaries

Command systemd runs:

  serve       starts the registry. No need to invoke it by hand

  version, help

`, Version)
}

// requiereRoot corta temprano y con un mensaje que se entiende. Lo que sigue
// escribe en /etc y habla con systemd, así que fallar más adelante produciría
// media instalación, que es peor que ninguna.
func requiereRoot(que string) error {
	if os.Geteuid() == 0 {
		return nil
	}
	return fmt.Errorf("%s needs root: run it again with sudo", que)
}

// requiereLinux evita un mensaje confuso al probar en la máquina equivocada.
func requiereLinux() error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("the seed is Linux: this cannot be installed on %s", runtime.GOOS)
	}
	if !setup.HaySystemd() {
		return fmt.Errorf("this machine does not use systemd, and the seed installs as a systemd service")
	}
	return nil
}
