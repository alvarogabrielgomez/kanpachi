package main

// `kanpachi upgrade`: traer la versión nueva del cliente.
//
// # Actualizar NO es reemplazar un binario
//
// Acá son el daemon, el motor, el CLI, las dos unidades de systemd y la
// cuarentena, y tienen que estar de acuerdo entre sí. **El motor y el daemon
// viajan juntos siempre**: el protocolo del motor decodifica estricto, así que
// un daemon nuevo mandándole `ipv4` a un motor viejo recibe el rechazo del
// mensaje entero y la sala no levanta. Cambiar una pieza sola no da un error
// claro: da una máquina que anuncia una versión y se comporta como otra.
//
// Por eso esto NO escribe ficheros: baja el paquete, lo verifica, y se lo pasa a
// `apt`. El paquete es lo que sabe poner las seis cosas a la vez.
//
// # Por qué no se reemplaza el binario en sitio, como hace el seed
//
// Porque el cliente de Linux viene de un `.deb`. Sobrescribir un fichero que
// dpkg cree suyo deja su base de datos mintiendo, y el siguiente `apt upgrade`
// lo pisa sin decir nada, devolviendo la versión vieja sin que nadie lo pida. El
// seed puede hacerlo porque lo instaló un `install.sh` que copia ficheros y no
// hay ninguna base de datos a la que contradecir.
//
// # Qué versión hay: se le pregunta al SEED, no a GitHub
//
// El seed ya publica `/api/version` con `client`, ya lo cachea una vez por hora,
// y es un host con el que este cliente habla de todas formas. GitHub queda de
// respaldo para el caso de un seed viejo o caído. Ver `registry/http.go`.

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
	"github.com/accentiostudios/kanpachi/internal/selfupdate"
)

// SumsFile es el manifiesto del CLIENTE, que no es el del seed.
//
// Los tres workflows escriben en el mismo release, así que los tres nombres
// tienen que ser distintos o el último en subir pisa a los otros. Ver
// `registry/selfupdate`, que se quedó con `SHA256SUMS-seed-linux` justo para
// dejar este nombre libre: el del cliente lo bajan desconocidos desde la página.
const SumsFile = "SHA256SUMS-linux"

// DebFile es el paquete, sin versión en el nombre.
//
// Porque la página apunta a `releases/latest/download/<fichero>`, que es una URL
// permanente. La versión viaja dentro, en el campo `Version` del control, que es
// de donde la lee dpkg.
const DebFile = "kanpachi-amd64.deb"

func cmdUpgrade(ctx context.Context, op opciones, args []string) error {
	soloMirar := false
	sinPreguntar := false
	pedida := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--check", "-check":
			soloMirar = true
		case "--yes", "-yes", "-y":
			sinPreguntar = true
		case "--version", "-version":
			if i+1 >= len(args) {
				return uso("--version is missing the number, for example v0.2.0")
			}
			i++
			pedida = args[i]
		default:
			return uso("upgrade does not understand %q. It takes --check, --version <v> and --yes", args[i])
		}
	}

	tag := pedida
	if tag == "" {
		var err error
		tag, err = últimaPublicada(ctx, op)
		if err != nil {
			return fmt.Errorf("could not find out which version is published: %w", err)
		}
	}

	// El atajo silencioso solo vale comparando contra la última. Con --version se
	// instala lo que se pidió aunque sea la misma o anterior, que es lo que hace
	// de este comando una forma de volver atrás.
	if pedida == "" && selfupdate.IsVersion(Version) && !selfupdate.Outdated(Version, tag) {
		fmt.Printf("Already up to date (%s).\n", Version)
		return nil
	}

	if soloMirar {
		switch {
		case selfupdate.Outdated(Version, tag):
			fmt.Printf("There is a new version: %s → %s\n", Version, tag)
			fmt.Println("  sudo kanpachi upgrade")
		case !selfupdate.IsVersion(Version):
			// Un binario compilado a mano. Decir "actualizado" sería mentira y
			// decir "desactualizado" también, así que se dice lo que se sabe.
			fmt.Printf("This binary is %s, so there is nothing to compare it against.\n", Version)
			fmt.Printf("  The latest published is %s\n", tag)
		default:
			fmt.Println("You are up to date.")
		}
		return nil
	}

	if err := sePuedeActualizarAcá(); err != nil {
		return err
	}
	if !sinPreguntar && !hayTerminal() {
		// Sin terminal no hay a quién preguntar, y dar el sí por supuesto sería
		// peor que negarse: dentro de un cron, el reinicio del servicio se lleva
		// la sala de gente que estaba jugando sin que nadie lo haya pedido.
		return uso("with no terminal you have to say --yes, and that restarts the service")
	}
	if !sinPreguntar {
		fmt.Printf("Installed %s, available %s.\n", Version, tag)
		// La sala se nombra porque el reinicio del servicio se la lleva por
		// delante, y quien tenga gente jugando quiere elegir el momento.
		fmt.Println("Upgrading restarts the service, so the room drops and has to be reopened.")
		ok, err := confirmar(fmt.Sprintf("Upgrade to %s?", tag))
		if err != nil {
			return err
		}
		if !ok {
			fmt.Println("Cancelled.")
			return nil
		}
	}

	fmt.Printf("Downloading %s (%s)...\n", DebFile, tag)
	paquete, err := bajarYVerificar(ctx, tag)
	if err != nil {
		return err
	}
	fmt.Println("SHA256 verified.")

	// A un fichero de verdad y no a memoria: `apt-get install` recibe una RUTA,
	// que es lo que hace que dpkg registre la instalación como suya. Va al
	// directorio de datos y no a /tmp porque en /tmp escribe cualquiera, y esto
	// es un paquete que se va a instalar como root en el minuto siguiente.
	destino := filepath.Join(op.datos, DebFile)
	if err := os.WriteFile(destino, paquete, 0o600); err != nil {
		return fmt.Errorf("saving the package to %s: %w", destino, err)
	}
	defer func() {
		if err := os.Remove(destino); err != nil && !os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "kanpachi: the package was left at %s: %v\n", destino, err)
		}
	}()

	fmt.Println("Installing with apt...")
	if err := instalarPaquete(ctx, destino); err != nil {
		return err
	}

	fmt.Printf("\nDone: %s.\n", tag)
	fmt.Println("  `kanpachi doctor` checks that everything landed where it should.")
	return nil
}

// últimaPublicada pregunta primero al seed y después a GitHub.
//
// El seed va primero porque ya tiene la respuesta cacheada una hora y es un host
// con el que este cliente habla igual. GitHub es el respaldo, no al revés: su
// API tiene límite por IP sin autenticar, y una máquina que pregunte seguido se
// queda sin respuesta justo cuando la necesita.
func últimaPublicada(ctx context.Context, op opciones) (string, error) {
	seed := seedDeLaSala(op)
	url := "https://" + seed + "/api/version"
	if datos, err := selfupdate.Get(ctx, url, 1<<16); err == nil {
		var cuerpo struct {
			Client string `json:"client"`
		}
		if json.Unmarshal(datos, &cuerpo) == nil && cuerpo.Client != "" {
			return cuerpo.Client, nil
		}
	}
	return selfupdate.Latest(ctx)
}

// seedDeLaSala usa el registro de la sala abierta, si la hay.
//
// Quien se autohospeda tiene su propio seed y su propia cadencia de
// publicación, así que preguntarle al de por omisión le contestaría por un
// producto que no es el que él reparte. Sin sala, el de por omisión, que es lo
// único que se puede saber.
func seedDeLaSala(op opciones) string {
	// Que el daemon no conteste NO es un fallo acá: actualizar tiene que poder
	// hacerse con el servicio caído, que además es cuando más falta hace.
	st, err := estadoParaElMenú(op)
	if err == nil && st.Seed != "" {
		return st.Seed
	}
	return domain.DefaultSeedHost
}

func bajarYVerificar(ctx context.Context, tag string) ([]byte, error) {
	base := selfupdate.Base(tag)
	paquete, err := selfupdate.Get(ctx, base+"/"+DebFile, 200<<20)
	if err != nil {
		return nil, fmt.Errorf("downloading %s: %w", DebFile, err)
	}
	sumas, err := selfupdate.Get(ctx, base+"/"+SumsFile, 1<<20)
	if err != nil {
		return nil, fmt.Errorf("could not download %s: without it nothing is verified and "+
			"nothing is installed: %w", SumsFile, err)
	}
	if err := selfupdate.Verificar(string(sumas), SumsFile, DebFile, paquete); err != nil {
		return nil, err
	}
	return paquete, nil
}

// sePuedeActualizarAcá se niega antes de bajar nada.
//
// Tres negativas y las tres tempranas: bajar cuarenta megas para descubrir
// después que no hay `apt` es gastarle a alguien su ancho de banda por nada.
func sePuedeActualizarAcá() error {
	if runtime.GOOS != "linux" {
		return negativa("`upgrade` installs a `.deb`, so it only works on Linux.\n" +
			"  On Windows the update goes through the installer: https://" +
			domain.DefaultSeedHost + "/download")
	}
	if runtime.GOARCH != "amd64" {
		return negativa("the published package is %s and this machine is %s.\n"+
			"  Nothing is published for this architecture yet", DebFile, runtime.GOARCH)
	}
	if os.Geteuid() != 0 {
		return negativa("installing a package is root's job: sudo kanpachi upgrade")
	}
	return nil
}

// instalarPaquete se lo pasa a apt, que es quien sabe poner las seis piezas.
//
// `apt-get install ./ruta.deb` y no `dpkg -i`: apt resuelve las dependencias del
// control del paquete, y dpkg se planta si falta alguna dejando el sistema con
// un paquete a medio configurar. La ruta lleva `./` a propósito, porque sin
// barra apt lo interpretaría como el NOMBRE de un paquete del repositorio, y ese
// nombre no existe en ninguno.
func instalarPaquete(ctx context.Context, ruta string) error {
	if !strings.HasPrefix(ruta, "/") && !strings.HasPrefix(ruta, "./") {
		ruta = "./" + ruta
	}
	return aptInstall(ctx, ruta)
}

// hayTerminal dice si hay alguien mirando.
func hayTerminal() bool { return term.IsTerminal(int(os.Stdin.Fd())) }
