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
	case "nginx", "proxy":
		err = cmdNginx(args[1:])
	case "uninstall":
		err = cmdUninstall(args[1:])
	case "version", "--version", "-v":
		fmt.Printf("kanpseed %s (EasyTier %s, %s/%s)\n", Version, setup.VersionEasyTier, runtime.GOOS, runtime.GOARCH)
		return 0
	case "help", "--help", "-h":
		ayuda()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "no conozco el comando %q\n\n", args[0])
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

  El nodo de encuentro de Kanpachi. Presenta a los peers entre sí, resuelve
  invite IDs y sirve la página de invitación.

Comandos que ejecuta una persona:

  init        instala y configura todo. Una sola ejecución
  doctor      revisa que todo esté como debe, y dice qué falta
  config      muestra o cambia los puertos, y reescribe los servicios
  nginx       repite el bloque que hay que pegar en el proxy inverso
  uninstall   quita los servicios y los binarios

Comando que ejecuta systemd:

  serve       arranca el registro. No hace falta invocarlo a mano

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
	return fmt.Errorf("%s necesita root: vuelve a ejecutarlo con sudo", que)
}

// requiereLinux evita un mensaje confuso al probar en la máquina equivocada.
func requiereLinux() error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("el seed es Linux: esto no se puede instalar en %s", runtime.GOOS)
	}
	if !setup.HaySystemd() {
		return fmt.Errorf("esta máquina no usa systemd, y el seed se instala como servicio de systemd")
	}
	return nil
}
