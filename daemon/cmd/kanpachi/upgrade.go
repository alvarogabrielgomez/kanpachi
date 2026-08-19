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
// # Qué versión hay: se le pregunta al CANAL, que es GitHub
//
// Y nunca al registro de la sala, aunque antes fuera al revés. El registro lo
// levanta cualquiera desde que dejó de venir compilado, y creerle qué versión
// hay es dejar que una máquina ajena elija a cuál te llevas. Ver
// [últimaPublicada], y [selfupdate.Repo] para moverlo en un fork.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/term"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/daemon/adapter/state/jsonfile"
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
	forzar := false
	pedida := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--check", "-check":
			soloMirar = true
		case "--yes", "-yes", "-y":
			sinPreguntar = true
		case "--force", "-force":
			forzar = true
		case "--version", "-version":
			if i+1 >= len(args) {
				return uso("--version is missing the number, for example v0.2.0")
			}
			i++
			pedida = args[i]
		default:
			return uso("upgrade does not understand %q. It takes --check, --version <v>, --force and --yes", args[i])
		}
	}

	tag := pedida
	if tag == "" {
		var err error
		tag, err = últimaPublicada(ctx)
		if err != nil {
			return fmt.Errorf("could not find out which version is published: %w", err)
		}
	}

	// El atajo silencioso solo vale comparando contra la última. Con --version se
	// instala lo que se pidió aunque sea la misma o anterior, que es lo que hace
	// de este comando una forma de volver atrás.
	//
	// Y con --force tampoco vale, porque la pregunta que contesta el atajo no es
	// la que a veces se hace. El atajo compara NÚMEROS, y dos binarios distintos
	// pueden llevar el mismo: una versión republicada sobre un arreglo deja a la
	// máquina desplegada con el build viejo y a `upgrade` diciendo que no hay
	// nada que hacer. Medido en el droplet con la 0.6.2 el 2026-08-19, que se
	// tuvo que retaguear y no había forma de recogerla.
	if pedida == "" && !forzar && selfupdate.IsVersion(Version) && !selfupdate.Outdated(Version, tag) {
		fmt.Printf("Already up to date (%s).\n", Version)
		fmt.Println("  `--force` installs it anyway, which is how a republished version is picked up.")
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

	if err := sePuedeActualizarAcá(op); err != nil {
		return err
	}
	if !sinPreguntar && !hayTerminal() {
		// Sin terminal no hay a quién preguntar, y dar el sí por supuesto sería
		// peor que negarse: dentro de un cron, el reinicio del servicio se lleva
		// la sala de gente que estaba jugando sin que nadie lo haya pedido.
		return uso("with no terminal you have to say --yes, and that restarts the service")
	}
	if !sinPreguntar {
		// Con el mismo número a los dos lados, «installed X, available X» no dice
		// nada y la pregunta parece un error. Lo que va a pasar es reinstalar, y
		// así se nombra.
		mismo := Version == tag
		if mismo {
			fmt.Printf("Installing %s over itself, which is what --force is for.\n", tag)
		} else {
			fmt.Printf("Installed %s, available %s.\n", Version, tag)
		}
		// La sala se nombra porque el reinicio del servicio se la lleva por
		// delante, y quien tenga gente jugando quiere elegir el momento.
		fmt.Println("Upgrading restarts the service, so the room drops and has to be reopened.")
		pregunta := fmt.Sprintf("Upgrade to %s?", tag)
		if mismo {
			pregunta = fmt.Sprintf("Reinstall %s?", tag)
		}
		ok, err := confirmar(pregunta)
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
	// Reinstalar cuando el número no sube. apt compara versiones igual que el
	// atajo de arriba y contesta «already the newest version» sin tocar nada, así
	// que saltarse el atajo del CLI y no el de apt deja el comando diciendo que
	// instaló sin haber instalado.
	reinstalar := forzar && !selfupdate.Outdated(Version, tag)
	if err := instalarPaquete(ctx, destino, reinstalar); err != nil {
		return err
	}

	fmt.Printf("\nDone: %s.\n", tag)
	fmt.Println("  `kanpachi doctor` checks that everything landed where it should.")
	return nil
}

// últimaPublicada le pregunta al CANAL DE ACTUALIZACIÓN, que es GitHub.
//
// # Por qué ya no se le pregunta al registro, que era lo de antes
//
// Preguntaba primero al registro de esta máquina y caía a GitHub. Tenía sentido
// mientras el registro era uno solo y compilado: era un host con el que este
// cliente hablaba igual, y su respuesta estaba cacheada para todos.
//
// Con registros de cualquiera eso pasa a ser **una máquina ajena decidiendo qué
// versión instalas**. No puede servirte un binario, porque el paquete y su
// manifiesto salen los dos de GitHub y se verifican, y sí puede hacer dos cosas:
// contestar un tag viejo, y entonces `Outdated` dice que estás al día y te
// congela en tu versión callado, o contestar una publicación real que no es la
// última, y dirigirte a una con un fallo conocido.
//
// El motivo por el que existía el rodeo era la cuota de GitHub, 60 por hora y
// por IP, compartida por toda la casa. Ese argumento era de la comprobación
// AUTOMÁTICA, que preguntaba varias veces por sesión sin que nadie lo pidiera.
// A pedido, un `upgrade --check` que alguien escribe gasta una de sesenta.
func últimaPublicada(ctx context.Context) (string, error) {
	return selfupdate.Latest(ctx)
}

// seedDeEstaMáquina contesta el registro de esta máquina, o cadena vacía.
//
// # Para qué queda, si la versión ya no se le pregunta
//
// Para NOMBRAR la página de descarga en Windows, y para nada más. Ese registro
// sirve la página de la que salió el instalador de quien lo instaló desde ahí,
// así que es la dirección útil que se le puede dar. La versión se le pregunta al
// canal de actualización, ver [últimaPublicada].
//
// # Las dos fuentes, y por qué hacen falta las dos
//
// La sala abierta primero, que es el dato más fresco. Y el fichero del daemon
// como respaldo, **leído directamente**: actualizar tiene que poder hacerse con
// el servicio caído, que además es cuando más falta hace, y ahí no hay a quién
// preguntarle. Por eso ese fichero es el único del estado que no va sellado, ver
// `jsonfile.Store.LoadSeed`.
//
// Vacío no es un fallo: es una instalación que todavía no hospedó ni entró a
// ninguna sala, o sea una que acaba de instalarse. Ahí se le pregunta a GitHub,
// que es de donde salió el binario que está corriendo.
func seedDeEstaMáquina(op opciones) string {
	// Que el daemon no conteste NO es un fallo acá, ver arriba.
	if st, err := currentMenuStatus(op); err == nil && st.Seed != "" {
		return st.Seed
	}
	raw, err := os.ReadFile(filepath.Join(op.datos, jsonfile.SeedFile))
	if err != nil {
		return ""
	}
	// Por el parser del dominio y no por un `TrimSpace`: de este valor sale una
	// URL a la que este proceso va a marcar, y un fichero de disco merece la
	// misma desconfianza que un enlace pegado. Ver [domain.ParseOwnSeed].
	seed, err := domain.ParseOwnSeed(raw)
	if err != nil {
		fmt.Fprintf(os.Stderr, "kanpachi: the saved registry is not a valid name, ignoring it: %v\n", err)
		return ""
	}
	return seed
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
//
// Recibe las opciones solo por la primera: la dirección de descarga sale del
// registro de esta máquina, y para leerlo hay que saber dónde vive el estado.
func sePuedeActualizarAcá(op opciones) error {
	if runtime.GOOS != "linux" {
		// La página de descarga la sirve un registro, así que la dirección sale
		// del registro de ESTA máquina. Sin ninguno configurado no hay página que
		// nombrar, y se manda a las publicaciones, que es de donde salió con
		// certeza el binario que está corriendo.
		dónde := selfupdate.Releases
		if seed := seedDeEstaMáquina(op); seed != "" {
			dónde = "https://" + seed + "/download"
		}
		return negativa("`upgrade` installs a `.deb`, so it only works on Linux.\n"+
			"  On Windows the update goes through the installer: %s", dónde)
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
func instalarPaquete(ctx context.Context, ruta string, reinstalar bool) error {
	if !strings.HasPrefix(ruta, "/") && !strings.HasPrefix(ruta, "./") {
		ruta = "./" + ruta
	}
	return aptInstall(ctx, ruta, reinstalar)
}

// hayTerminal dice si hay alguien mirando.
func hayTerminal() bool { return term.IsTerminal(int(os.Stdin.Fd())) }
