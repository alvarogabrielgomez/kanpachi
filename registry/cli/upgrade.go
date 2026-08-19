package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/accentiostudios/kanpachi/core/timing"
	"github.com/accentiostudios/kanpachi/registry/selfupdate"
	"github.com/accentiostudios/kanpachi/registry/setup"
)

// cmdUpgrade actualiza el seed a la última versión publicada.
//
// **Actualizar NO es reemplazar el binario.** El seed son cinco cosas que
// tienen que estar de acuerdo entre sí: el binario, la página que sirve, los
// binarios de EasyTier, las units de systemd y los procesos que están
// corriendo ahora mismo. Cambiar solo la primera deja una máquina que anuncia
// una versión y se comporta como otra, y ese desajuste no da ningún error: da
// un droplet que "va raro". Por eso este comando hace las cinco.
//
// Es re-ejecutable: si ya está al día, no toca nada y lo dice.
func cmdUpgrade(args []string) error {
	fs := flag.NewFlagSet("upgrade", flag.ContinueOnError)
	check := fs.Bool("check", false, "only reports whether there is a new version, installs nothing")
	target := fs.String("version", "", "a specific version to install, for example v0.1.0")
	force := fs.Bool("force", false, "install the latest even if it is the one already installed")
	assumeYes := fs.Bool("yes", false, "no preguntar antes de actualizar")
	noRestart := fs.Bool("no-restart", false, "do not restart the services when done")
	if err := fs.Parse(args); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), timing.UpgradeTimeout)
	defer cancel()

	tag := *target
	if tag == "" {
		ultima, err := selfupdate.Latest(ctx)
		if err != nil {
			return fmt.Errorf("could not ask for the latest version: %w", err)
		}
		tag = ultima
	}

	// El atajo silencioso solo vale comparando contra la última. Con --version
	// se instala lo que se pidió aunque sea la misma o anterior, que es lo que
	// hace de este comando una forma de volver atrás.
	//
	// Y con --force tampoco vale. El atajo compara NÚMEROS, y dos publicaciones
	// distintas pueden llevar el mismo: una versión republicada sobre un arreglo
	// deja a esta máquina con el build viejo y a este comando diciendo que no hay
	// nada que hacer. Acá pesa más que en el cliente, porque el seed son cinco
	// piezas que tienen que estar de acuerdo y una republicación puede tocar
	// cualquiera de ellas.
	if *target == "" && !*force && selfupdate.IsVersion(Version) && !selfupdate.Outdated(Version, tag) {
		ok("the seed is already up to date (%s)", Version)
		tenue("  --force installs it anyway, which is how a republished version is picked up")
		return nil
	}

	banner("Kanpachi seed · upgrade", "installed "+Version+"  →  available "+tag)

	if *check {
		if selfupdate.Outdated(Version, tag) {
			aviso("there is a new version: %s → %s", Version, tag)
			codigo("sudo kanpseed upgrade")
		} else if !selfupdate.IsVersion(Version) {
			// Un binario compilado a mano. Decir "actualizado" sería mentira y
			// decir "desactualizado" también, así que se dice lo que se sabe.
			aviso("this binary is %s, so there is nothing to compare against", Version)
			tenue("  the latest published one is %s", tag)
		} else {
			ok("you are up to date")
		}
		return nil
	}

	if err := requiereLinux(); err != nil {
		return err
	}
	if err := requiereRoot("upgrade"); err != nil {
		return err
	}

	// Se actualiza una instalación, no se hace una. Sin config no se sabe en qué
	// puerto vive el registro, y adivinarlo movería el servicio por debajo del
	// proxy inverso de la máquina.
	//
	// Se comprueba y se descarta: quien la USA es `reconfigure`, ya con el
	// binario nuevo. Acá sirve para negarse temprano, antes de bajar nada, en
	// vez de descubrirlo con el binario ya reemplazado.
	if _, err := setup.Cargar(); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("there is no seed installed on this machine (%s is missing).\n"+
				"  Para instalarlo por primera vez:\n"+
				"  curl -fsSL https://raw.githubusercontent.com/%s/main/seed/install.sh | sudo sh",
				setup.RutaConfig(), selfupdate.Repo)
		}
		return err
	}

	if !*assumeYes && interactivo() && !confirmar(fmt.Sprintf("Update the seed to %s?", tag), true) {
		tenue("  cancelled")
		return nil
	}

	seccion("Downloading")
	carga, err := selfupdate.Download(ctx, tag, func(f string, a ...any) { tenue("  "+f, a...) })
	if err != nil {
		return err
	}

	seccion("Installing")

	// El binario que se reemplaza es el que ejecuta systemd, no el que está
	// corriendo este comando. Suelen ser el mismo; cuando no lo son, actualizar
	// la copia desde la que alguien lanzó el comando dejaría el servicio con la
	// versión vieja, que es exactamente lo contrario de lo que se pidió.
	destino := filepath.Join(setup.DirBin, setup.Binario)
	if err := escribirAtomico(destino, carga.Binary, 0o755); err != nil {
		return fmt.Errorf("could not replace %s: %w", destino, err)
	}
	ok("binary %s in %s", tag, destino)
	if propio, err := os.Executable(); err == nil {
		if resuelto, err := filepath.EvalSymlinks(propio); err == nil {
			propio = resuelto
		}
		if propio != destino {
			aviso("this copy (%s) is still at %s", propio, Version)
			tenue("  the one the service runs is %s, and that one is already updated", destino)
		}
	}

	pagina := filepath.Join(setup.DirLib, selfupdate.PageFile)
	if err := escribirAtomico(pagina, carga.Page, 0o644); err != nil {
		return err
	}
	ok("invitation page in %s", pagina)

	// **A partir de acá manda el binario NUEVO, y eso es la mitad del comando.**
	//
	// Lo que falta —el motor de EasyTier, las units, el reinicio— se decide con
	// código que acaba de cambiar: el pin de EasyTier y el texto de las units
	// son constantes COMPILADAS. Este proceso lleva las de la versión anterior,
	// porque se lanzó antes del reemplazo de arriba, así que haciéndolo él
	// mismo escribe la configuración vieja sobre el binario nuevo.
	//
	// No es teórico y costó un despliegue: `--secure-mode true` se agregó a la
	// unit del motor, se publicó, se corrió `upgrade` en el droplet, y la unit
	// quedó sin la bandera. La máquina anunciaba la versión con el arreglo y se
	// comportaba como la de antes, que es exactamente el desajuste que la
	// cabecera de este comando dice que existe para evitar.
	//
	// Se le cede el trabajo a `reconfigure`, ejecutando el binario recién
	// puesto. Un proceso hijo y no `syscall.Exec` porque esto compila también
	// en Windows, donde ese `Exec` no existe: acá se prueba, en Linux se corre.
	if err := cederA(destino, *noRestart); err != nil {
		return err
	}

	seccion("Done")
	tenue("  the seed is running %s", tag)
	codigo("kanpseed doctor")
	fmt.Println()
	return nil
}

// cederA le pasa el resto del trabajo al binario que se acaba de instalar.
//
// La salida del hijo va directa a la de este proceso, así que quien mira la
// terminal ve una sola actualización y no dos programas hablando.
func cederA(binario string, noRestart bool) error {
	args := []string{"reconfigure"}
	if noRestart {
		args = append(args, "--no-restart")
	}
	cmd := exec.Command(binario, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("configuring with the new binary failed: %w", err)
	}
	return nil
}

// cmdReconfigure deja la máquina como dice el binario que lo ejecuta.
//
// # Para qué existe uno separado de `init`
//
// `init` INSTALA: elige puertos si faltan, pregunta el dominio, se copia a sí
// mismo a `/usr/local/bin`. Sirve para esto de rebote y por eso se venía
// usando, con el efecto de que actualizar el seed eran dos comandos y el
// segundo había que saberlo. Esto no instala nada: lee la configuración que ya
// hay, escribe lo que de ella se deriva, y reinicia.
//
// Lo llama `upgrade` por dentro, y queda disponible a mano para el caso en que
// alguien edite una unit y quiera devolverla a lo que el binario dice.
func cmdReconfigure(args []string) error {
	fs := flag.NewFlagSet("reconfigure", flag.ContinueOnError)
	noRestart := fs.Bool("no-restart", false, "do not restart the services when done")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if err := requiereLinux(); err != nil {
		return err
	}
	if err := requiereRoot("reconfigure"); err != nil {
		return err
	}

	cfg, err := setup.Cargar()
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("there is no seed installed on this machine (%s is missing).\n"+
			"  Para instalarlo por primera vez:\n"+
			"  curl -fsSL https://raw.githubusercontent.com/%s/main/seed/install.sh | sudo sh",
			setup.RutaConfig(), selfupdate.Repo)
	}
	if err != nil {
		return err
	}

	seccion("Configuring")

	// El pin de EasyTier es una constante de ESTE binario, así que acá sí
	// descubre que la versión fijada cambió y se trae el motor nuevo.
	descargado, err := setup.InstalarEasyTier(setup.DirLib, func(m string) { tenue("  %s", m) })
	if err != nil {
		return err
	}
	if descargado {
		ok("engine lib %s installed", setup.VersionEasyTier)
	} else {
		ok("engine lib %s was already there", setup.VersionEasyTier)
	}

	// Reescribir las units importa tanto como el binario: un arreglo que vive
	// en la unit (un límite, una directiva de aislamiento, una bandera del
	// motor) no llega por intercambiar el ejecutable, y sin esto la máquina se
	// quedaría con el fallo arreglado en el código y presente en la
	// configuración.
	cambiadas, err := setup.EscribirUnits(cfg)
	if err != nil {
		return err
	}
	if cambiadas {
		ok("services rewritten in %s", setup.DirUnits)
	} else {
		ok("the services were already up to date")
	}

	if *noRestart {
		seccion("Still needs a restart")
		aviso("the processes stay on the previous version until they are restarted")
		codigo("sudo systemctl restart " + setup.UnitMotor + " " + setup.UnitReg)
		fmt.Println()
		return nil
	}

	seccion("Restarting")
	for _, u := range []string{setup.UnitMotor, setup.UnitReg} {
		if err := setup.Systemctl("restart", u); err != nil {
			return fmt.Errorf("%w\n\nLast lines of the journal:\n%s", err, setup.LogsDeUnit(u, 15))
		}
	}
	if err := esperarSalud(cfg, 20*time.Second); err != nil {
		return fmt.Errorf("%w\n\nLast lines of the journal:\n%s", err, setup.LogsDeUnit(setup.UnitReg, 15))
	}
	ok("the registry answers on 127.0.0.1:%d", cfg.PuertoRegistro)
	ok("the engine listens on %d, TCP and UDP", cfg.PuertoMotor)
	return nil
}

// escribirAtomico deja el contenido en su sitio de una sola vez.
//
// Temporal en el MISMO directorio y rename encima: es atómico en POSIX y
// funciona incluso sobre un binario que se está ejecutando, que es justo el
// caso de este comando. Un temporal en /tmp no serviría, porque cruzar sistemas
// de archivos convierte el rename en copia y deja de ser atómico.
func escribirAtomico(destino string, datos []byte, modo os.FileMode) error {
	dir := filepath.Dir(destino)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(destino)+"-*.tmp")
	if err != nil {
		return err
	}
	nombre := tmp.Name()
	defer os.Remove(nombre)

	if _, err := tmp.Write(datos); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	// CreateTemp crea con 0600. El modo se pone aparte porque un binario sin
	// permiso de ejecución no lo arranca systemd, y una página sin lectura no
	// la sirve nadie.
	if err := os.Chmod(nombre, modo); err != nil {
		return err
	}
	return os.Rename(nombre, destino)
}
