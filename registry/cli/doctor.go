package cli

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/accentiostudios/kanpachi/registry/setup"
)

// cmdDoctor revisa la instalación y dice qué falta.
//
// La regla de este comando: cada cosa que salga mal viene con qué hacer al
// respecto. Un diagnóstico que solo dice "mal" obliga a buscar por fuera, y
// entonces no ahorró nada.
func cmdDoctor(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}

	banner("Kanpachi seed · doctor", "checking the installation")

	problemas := 0
	malo := func(formato string, a ...any) { fallo(formato, a...); problemas++ }

	cfg, err := setup.Cargar()
	if errors.Is(err, os.ErrNotExist) {
		malo("nothing is installed: %s is missing", setup.RutaConfig())
		codigo("sudo kanpseed init")
		return resumenDoctor(problemas)
	}
	if err != nil {
		malo("%v", err)
		return resumenDoctor(problemas)
	}
	ok("configuration read from %s", setup.RutaConfig())
	tenue("  registry %d · engine %d · rpc %d · domain %s",
		cfg.PuertoRegistro, cfg.PuertoMotor, cfg.PuertoRPC, sinVacio(cfg.Dominio, "sin definir"))

	seccion("Files")
	for _, ruta := range []string{
		filepath.Join(setup.DirBin, setup.Binario),
		filepath.Join(setup.DirLib, "easytier-core"),
		filepath.Join(setup.DirLib, "easytier-cli"),
		filepath.Join(setup.DirLib, "index.html"),
	} {
		if info, err := os.Stat(ruta); err != nil {
			malo("falta %s", ruta)
			codigo("sudo kanpseed init")
		} else {
			ok("%s (%d KB)", ruta, info.Size()/1024)
		}
	}
	if v := versionEasyTier(); v != "" {
		if strings.Contains(v, strings.TrimPrefix(setup.VersionEasyTier, "v")) {
			ok("engine lib %s, the pinned version", v)
		} else {
			aviso("engine lib installed: %s, and the pinned one is %s", v, setup.VersionEasyTier)
			tenue("  a version other than the pinned one changes how the network behaves without saying so")
		}
	}

	seccion("Services")
	for _, u := range []string{setup.UnitMotor, setup.UnitReg} {
		estado := setup.EstadoUnit(u)
		habilitada := setup.UnitHabilitada(u)
		switch {
		case estado == "active" && habilitada:
			ok("%s active and enabled", u)
		case estado == "active":
			aviso("%s active, NOT enabled: it will not come back on its own after a reboot", u)
			codigo("sudo systemctl enable " + u)
			problemas++
		default:
			malo("%s is %s", u, estado)
			codigo("sudo systemctl status "+u, "journalctl -u "+u+" -n 30 --no-pager")
			if logs := setup.LogsDeUnit(u, 5); logs != "" {
				tenue("  last lines:")
				for _, l := range strings.Split(logs, "\n") {
					tenue("    %s", l)
				}
			}
		}
	}

	seccion("Network")
	if escuchando("127.0.0.1", cfg.PuertoRegistro) {
		ok("the registry listens on 127.0.0.1:%d", cfg.PuertoRegistro)
	} else {
		malo("nada escucha en 127.0.0.1:%d, que es a donde apunta tu proxy", cfg.PuertoRegistro)
	}
	if escuchando("0.0.0.0", cfg.PuertoMotor) || escuchando("127.0.0.1", cfg.PuertoMotor) {
		ok("the engine listens on %d", cfg.PuertoMotor)
	} else {
		malo("nada escucha en el %d, que es por donde entran los clientes", cfg.PuertoMotor)
	}
	// Que el RPC NO se alcance desde fuera del loopback es una invariante, no
	// una preferencia: es el panel de control del motor.
	if alcanzable(ipNoLoopback(), cfg.PuertoRPC) {
		malo("el portal RPC del motor responde fuera del loopback, y no debe")
		codigo("sudo kanpseed init   # rewrites the services with the right configuration")
	} else {
		ok("the RPC portal only answers on loopback")
	}

	seccion("Service")
	salud, err := consultarSalud(cfg)
	if err != nil {
		malo("/healthz no responde: %v", err)
	} else {
		ok("/healthz answers: %d live rooms", entero(salud["rooms"]))
		if motivo, hay := salud["counter"]; hay {
			aviso("the member counter is not working: %v", motivo)
			tenue("  the page is still served and rooms still resolve, just without the counter")
			problemas++
		} else {
			ok("the counter reads the engine correctly")
		}
	}

	seccion("Firewall")
	revisarCortafuegos(cfg, malo)

	seccion("Reverse proxy")
	if cfg.Dominio == "" {
		aviso("no domain configured, so the proxy cannot be checked")
		codigo("sudo kanpseed config --domain kanpachi.tudominio.com")
	} else {
		tenue("  the proxy has to point at 127.0.0.1:%d", cfg.PuertoRegistro)
		tenue("  run `kanpseed nginx` for the whole block")
	}

	return resumenDoctor(problemas)
}

func resumenDoctor(problemas int) error {
	fmt.Println()
	if problemas == 0 {
		fmt.Println(cCaja.Render(cOK.Render("All good")))
		fmt.Println()
		return nil
	}
	fmt.Println(cCaja.Render(cAviso.Render(fmt.Sprintf("%d things to look at", problemas))))
	fmt.Println()
	return fmt.Errorf("the check found %d problems", problemas)
}

func escuchando(host string, puerto int) bool {
	// Se intenta OCUPAR el puerto: si falla, hay algo escuchando. Es más
	// fiable que leer /proc/net, que cambia de formato entre distribuciones.
	l, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(puerto)))
	if err != nil {
		return true
	}
	l.Close()
	return false
}

// alcanzable comprueba si algo contesta en esa dirección. Se usa para verificar
// lo contrario de lo habitual: que el RPC NO conteste fuera del loopback.
func alcanzable(host string, puerto int) bool {
	if host == "" {
		return false
	}
	con, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(puerto)), 2e9)
	if err != nil {
		return false
	}
	con.Close()
	return true
}

// ipNoLoopback devuelve una IP propia que no sea 127.0.0.1, para probar desde
// "fuera" sin salir de la máquina.
func ipNoLoopback() string {
	dirs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, d := range dirs {
		red, ok := d.(*net.IPNet)
		if !ok || red.IP.IsLoopback() || red.IP.To4() == nil {
			continue
		}
		return red.IP.String()
	}
	return ""
}

func versionEasyTier() string {
	salida, err := exec.Command(filepath.Join(setup.DirLib, "easytier-core"), "--version").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(salida))
}

func sinVacio(s, alternativa string) string {
	if s == "" {
		return alternativa
	}
	return s
}

func entero(v any) int {
	if f, ok := v.(float64); ok {
		return int(f)
	}
	return 0
}
