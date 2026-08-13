package cli

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/accentiostudios/kanpachi/registry"
	"github.com/accentiostudios/kanpachi/registry/setup"
)

// cmdConfig muestra o cambia la configuración.
//
// Cambiar un puerto reescribe las units y reinicia, porque si no quedarían dos
// verdades: el JSON diciendo una cosa y /etc/systemd otra. El comando que
// promete cambiar algo tiene que dejarlo cambiado de verdad.
func cmdConfig(args []string) error {
	fs := flag.NewFlagSet("config", flag.ContinueOnError)
	puerto := fs.Int("port", 0, "internal port of the registry")
	puertoMotor := fs.Int("engine-port", 0, "public port of the engine")
	dominio := fs.String("domain", "", "domain the page is served on")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := setup.Cargar()
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("nothing is installed yet. Run: sudo %s init", setup.Binario)
	}
	if err != nil {
		return err
	}

	// Sin argumentos, solo muestra.
	if *puerto == 0 && *puertoMotor == 0 && *dominio == "" {
		mostrarConfig(cfg)
		return nil
	}

	if err := requiereRoot("changing the configuration"); err != nil {
		return err
	}

	nueva := cfg
	if *dominio != "" {
		nueva.Dominio = *dominio
	}
	if *puertoMotor != 0 {
		nueva.PuertoMotor = *puertoMotor
	}
	if *puerto != 0 {
		// Se comprueba que esté libre, salvo que sea el que ya usamos: ahí
		// está ocupado por nosotros mismos y eso es correcto.
		if *puerto != cfg.PuertoRegistro {
			if p, err := setup.PuertoLibre(*puerto, *puerto, *puerto); err != nil || p != *puerto {
				return fmt.Errorf("port %d is taken by something else", *puerto)
			}
		}
		nueva.PuertoRegistro = *puerto
	}

	if nueva == cfg {
		aviso("nothing to change")
		return nil
	}

	if err := nueva.Guardar(); err != nil {
		return err
	}
	cambiadas, err := setup.EscribirUnits(nueva)
	if err != nil {
		return err
	}
	ok("configuration saved")

	if cambiadas {
		seccion("Restarting")
		for _, u := range []string{setup.UnitMotor, setup.UnitReg} {
			if err := setup.Systemctl("restart", u); err != nil {
				return fmt.Errorf("%w\n\nLast lines of the journal:\n%s", err, setup.LogsDeUnit(u, 15))
			}
			ok("%s restarted", u)
		}
	}

	if nueva.PuertoRegistro != cfg.PuertoRegistro {
		fmt.Println()
		fmt.Println(cCaja.Render(
			cAviso.Render(fmt.Sprintf("THE PORT CHANGED: %d → %d", cfg.PuertoRegistro, nueva.PuertoRegistro)) + "\n" +
				cTenue.Render("The reverse proxy has to be updated or the page stops loading")))
		fmt.Println()
		fmt.Print(setup.BloqueDeProxy(nueva))
	}
	return nil
}

// estadoDeLaPuerta dice si hospedar pide password.
//
// Se lee del disco y no de la configuración, porque no vive ahí: la credencial
// está en el directorio de estado, y lo que se enseña acá tiene que ser lo que
// hay, no lo que alguien anotó. Un fichero ilegible se cuenta como cerrado, que
// es la lectura conservadora: decir «open» sobre algo que no se pudo leer sería
// afirmar lo que no se sabe.
func estadoDeLaPuerta() string {
	auth, err := registry.OpenAuth(setup.DirState)
	switch {
	case err != nil:
		return "unknown, the credential could not be read"
	case auth.Closed():
		return "password required"
	default:
		return "open to anyone who reaches this seed"
	}
}

func mostrarConfig(c setup.Config) {
	banner("Kanpachi seed · config", setup.RutaConfig())
	fmt.Printf("  registry port   %s  %s\n",
		cCodigo.Render(fmt.Sprint(c.PuertoRegistro)), cTenue.Render("internal, this is the one for the reverse proxy"))
	fmt.Printf("  engine port     %s  %s\n",
		cCodigo.Render(fmt.Sprint(c.PuertoMotor)), cTenue.Render("public, TCP and UDP, compiled into the client"))
	fmt.Printf("  RPC portal      %s  %s\n",
		cCodigo.Render(fmt.Sprint(c.PuertoRPC)), cTenue.Render("loopback only, it never leaves the machine"))
	fmt.Printf("  domain          %s  %s\n",
		cCodigo.Render(sinVacio(c.Dominio, "not set")),
		cTenue.Render("what users type, and the password is bound to it"))
	fmt.Printf("  hosting         %s\n\n", cCodigo.Render(estadoDeLaPuerta()))
	tenue("To change something:")
	codigo(
		"sudo "+setup.Binario+" config --port 8020",
		"sudo "+setup.Binario+" config --domain kanpachi.yourdomain.com",
		"sudo "+setup.Binario+" password",
	)
	fmt.Println()
}

func cmdNginx(args []string) error {
	cfg, err := setup.Cargar()
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("nothing is installed yet. Run: sudo %s init", setup.Binario)
	}
	if err != nil {
		return err
	}
	fmt.Println()
	fmt.Print(setup.BloqueDeProxy(cfg))
	return nil
}

// cmdUninstall deja la máquina como estaba.
//
// Existe porque un instalador sin desinstalador convierte "voy a probarlo" en
// una decisión, y este proyecto se prueba en el servidor de producción de
// alguien.
func cmdUninstall(args []string) error {
	fs := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	si := fs.Bool("yes", false, "do not ask")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := requiereRoot("uninstall"); err != nil {
		return err
	}

	banner("Kanpachi seed · uninstall", "removes the services and the files")
	tenue("  %s and %s are stopped and removed", setup.UnitMotor, setup.UnitReg)
	tenue("  %s, %s and %s are deleted", setup.DirLib, setup.DirConfig, setup.DirState)
	tenue("  your reverse proxy and firewall are NOT touched")
	fmt.Println()

	if !*si && !confirmar("Are you sure?", false) {
		return fmt.Errorf("cancelled")
	}

	for _, u := range []string{setup.UnitReg, setup.UnitMotor} {
		// Los errores aquí se ignoran a propósito: desinstalar tiene que
		// funcionar aunque la instalación estuviera a medias, que es
		// justamente cuando más falta hace.
		_ = setup.Systemctl("disable", "--now", u)
		if err := os.Remove(filepath.Join(setup.DirUnits, u)); err == nil {
			ok("%s removed", u)
		}
	}
	_ = setup.Systemctl("daemon-reload")

	// El directorio de estado va en la lista, y hay que decir por qué: ahí vive
	// la CREDENCIAL del operador, con la clave que acuña tokens. Dejarla puesta
	// tras desinstalar sería dejar material que firma en una máquina donde ya no
	// corre nada que lo use.
	//
	// Con DynamicUser=yes el nombre de arriba es un enlace a /var/lib/private, y
	// `RemoveAll` sobre un enlace borra el enlace y no lo apuntado, así que se
	// borran los dos.
	for _, d := range []string{setup.DirLib, setup.DirConfig, setup.DirState, setup.DirStatePrivado} {
		if err := os.RemoveAll(d); err == nil {
			ok("%s deleted", d)
		}
	}
	// El binario se borra al final, porque es el que está ejecutando esto. En
	// Linux se puede: el inodo sigue vivo hasta que el proceso termina.
	if err := os.Remove(filepath.Join(setup.DirBin, setup.Binario)); err == nil {
		ok("%s deleted", filepath.Join(setup.DirBin, setup.Binario))
	}

	fmt.Println()
	tenue("The reverse proxy entry still has to be removed by hand.")
	return nil
}
