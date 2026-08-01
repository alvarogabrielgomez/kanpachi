package cli

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/accentiostudios/kanpachi/registry/setup"
)

// cmdConfig muestra o cambia la configuración.
//
// Cambiar un puerto reescribe las units y reinicia, porque si no quedarían dos
// verdades: el JSON diciendo una cosa y /etc/systemd otra. El comando que
// promete cambiar algo tiene que dejarlo cambiado de verdad.
func cmdConfig(args []string) error {
	fs := flag.NewFlagSet("config", flag.ContinueOnError)
	puerto := fs.Int("port", 0, "puerto interno del registro")
	puertoMotor := fs.Int("engine-port", 0, "puerto público del motor")
	dominio := fs.String("domain", "", "dominio por el que se sirve la página")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := setup.Cargar()
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("no hay ninguna instalación todavía. Ejecuta: sudo %s init", setup.Binario)
	}
	if err != nil {
		return err
	}

	// Sin argumentos, solo muestra.
	if *puerto == 0 && *puertoMotor == 0 && *dominio == "" {
		mostrarConfig(cfg)
		return nil
	}

	if err := requiereRoot("cambiar la configuración"); err != nil {
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
				return fmt.Errorf("el puerto %d está ocupado por otra cosa", *puerto)
			}
		}
		nueva.PuertoRegistro = *puerto
	}

	if nueva == cfg {
		aviso("nada que cambiar")
		return nil
	}

	if err := nueva.Guardar(); err != nil {
		return err
	}
	cambiadas, err := setup.EscribirUnits(nueva)
	if err != nil {
		return err
	}
	ok("configuración guardada")

	if cambiadas {
		seccion("Reiniciando")
		for _, u := range []string{setup.UnitMotor, setup.UnitReg} {
			if err := setup.Systemctl("restart", u); err != nil {
				return fmt.Errorf("%w\n\nÚltimas líneas del diario:\n%s", err, setup.LogsDeUnit(u, 15))
			}
			ok("%s reiniciado", u)
		}
	}

	if nueva.PuertoRegistro != cfg.PuertoRegistro {
		fmt.Println()
		fmt.Println(cCaja.Render(
			cAviso.Render(fmt.Sprintf("EL PUERTO CAMBIÓ: %d → %d", cfg.PuertoRegistro, nueva.PuertoRegistro)) + "\n" +
				cTenue.Render("Hay que actualizar el proxy inverso o la página deja de verse")))
		fmt.Println()
		fmt.Print(setup.BloqueDeProxy(nueva))
	}
	return nil
}

func mostrarConfig(c setup.Config) {
	banner("Kanpachi seed · config", setup.RutaConfig())
	fmt.Printf("  puerto del registro   %s  %s\n",
		cCodigo.Render(fmt.Sprint(c.PuertoRegistro)), cTenue.Render("interno, es el del proxy inverso"))
	fmt.Printf("  puerto del motor      %s  %s\n",
		cCodigo.Render(fmt.Sprint(c.PuertoMotor)), cTenue.Render("público, TCP y UDP, va compilado en el cliente"))
	fmt.Printf("  portal RPC            %s  %s\n",
		cCodigo.Render(fmt.Sprint(c.PuertoRPC)), cTenue.Render("solo loopback, jamás sale de la máquina"))
	fmt.Printf("  dominio               %s\n\n",
		cCodigo.Render(sinVacio(c.Dominio, "sin definir")))
	tenue("Para cambiar algo:")
	codigo(
		"sudo "+setup.Binario+" config --port 8020",
		"sudo "+setup.Binario+" config --domain kanpachi.tudominio.com",
	)
	fmt.Println()
}

func cmdNginx(args []string) error {
	cfg, err := setup.Cargar()
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("no hay ninguna instalación todavía. Ejecuta: sudo %s init", setup.Binario)
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
	si := fs.Bool("yes", false, "no preguntar")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := requiereRoot("uninstall"); err != nil {
		return err
	}

	banner("Kanpachi seed · uninstall", "quita los servicios y los archivos")
	tenue("  se detienen y borran %s y %s", setup.UnitMotor, setup.UnitReg)
	tenue("  se borra %s y %s", setup.DirLib, setup.DirConfig)
	tenue("  NO se toca tu proxy inverso ni el firewall")
	fmt.Println()

	if !*si && !confirmar("¿Seguro?", false) {
		return fmt.Errorf("cancelado")
	}

	for _, u := range []string{setup.UnitReg, setup.UnitMotor} {
		// Los errores aquí se ignoran a propósito: desinstalar tiene que
		// funcionar aunque la instalación estuviera a medias, que es
		// justamente cuando más falta hace.
		_ = setup.Systemctl("disable", "--now", u)
		if err := os.Remove(filepath.Join(setup.DirUnits, u)); err == nil {
			ok("%s quitado", u)
		}
	}
	_ = setup.Systemctl("daemon-reload")

	for _, d := range []string{setup.DirLib, setup.DirConfig} {
		if err := os.RemoveAll(d); err == nil {
			ok("%s borrado", d)
		}
	}
	// El binario se borra al final, porque es el que está ejecutando esto. En
	// Linux se puede: el inodo sigue vivo hasta que el proceso termina.
	if err := os.Remove(filepath.Join(setup.DirBin, setup.Binario)); err == nil {
		ok("%s borrado", filepath.Join(setup.DirBin, setup.Binario))
	}

	fmt.Println()
	tenue("Queda pendiente quitar a mano la entrada del proxy inverso.")
	return nil
}
