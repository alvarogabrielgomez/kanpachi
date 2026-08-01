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

	banner("Kanpachi seed · doctor", "revisando la instalación")

	problemas := 0
	malo := func(formato string, a ...any) { fallo(formato, a...); problemas++ }

	cfg, err := setup.Cargar()
	if errors.Is(err, os.ErrNotExist) {
		malo("no hay ninguna instalación: falta %s", setup.RutaConfig())
		codigo("sudo kanpseed init")
		return resumenDoctor(problemas)
	}
	if err != nil {
		malo("%v", err)
		return resumenDoctor(problemas)
	}
	ok("configuración leída de %s", setup.RutaConfig())
	tenue("  registro %d · motor %d · rpc %d · dominio %s",
		cfg.PuertoRegistro, cfg.PuertoMotor, cfg.PuertoRPC, sinVacio(cfg.Dominio, "sin definir"))

	seccion("Archivos")
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
			ok("EasyTier %s, la versión fijada", v)
		} else {
			aviso("EasyTier instalado: %s, y la fijada es %s", v, setup.VersionEasyTier)
			tenue("  una versión distinta a la fijada cambia el comportamiento de la red sin avisar")
		}
	}

	seccion("Servicios")
	for _, u := range []string{setup.UnitMotor, setup.UnitReg} {
		estado := setup.EstadoUnit(u)
		habilitada := setup.UnitHabilitada(u)
		switch {
		case estado == "active" && habilitada:
			ok("%s activo y habilitado", u)
		case estado == "active":
			aviso("%s activo, NO habilitado: no volverá solo tras reiniciar el servidor", u)
			codigo("sudo systemctl enable " + u)
			problemas++
		default:
			malo("%s está %s", u, estado)
			codigo("sudo systemctl status "+u, "journalctl -u "+u+" -n 30 --no-pager")
			if logs := setup.LogsDeUnit(u, 5); logs != "" {
				tenue("  últimas líneas:")
				for _, l := range strings.Split(logs, "\n") {
					tenue("    %s", l)
				}
			}
		}
	}

	seccion("Red")
	if escuchando("127.0.0.1", cfg.PuertoRegistro) {
		ok("el registro escucha en 127.0.0.1:%d", cfg.PuertoRegistro)
	} else {
		malo("nada escucha en 127.0.0.1:%d, que es a donde apunta tu proxy", cfg.PuertoRegistro)
	}
	if escuchando("0.0.0.0", cfg.PuertoMotor) || escuchando("127.0.0.1", cfg.PuertoMotor) {
		ok("el motor escucha en el %d", cfg.PuertoMotor)
	} else {
		malo("nada escucha en el %d, que es por donde entran los clientes", cfg.PuertoMotor)
	}
	// Que el RPC NO se alcance desde fuera del loopback es una invariante, no
	// una preferencia: es el panel de control del motor.
	if alcanzable(ipNoLoopback(), cfg.PuertoRPC) {
		malo("el portal RPC del motor responde fuera del loopback, y no debe")
		codigo("sudo kanpseed init   # reescribe los servicios con la configuración correcta")
	} else {
		ok("el portal RPC solo responde en loopback")
	}

	seccion("Servicio")
	salud, err := consultarSalud(cfg)
	if err != nil {
		malo("/healthz no responde: %v", err)
	} else {
		ok("/healthz responde: %d salas vivas", entero(salud["rooms"]))
		if motivo, hay := salud["counter"]; hay {
			aviso("el contador de miembros no funciona: %v", motivo)
			tenue("  la página sigue sirviéndose y las salas siguen resolviendo, sin el contador")
			problemas++
		} else {
			ok("el contador lee el motor correctamente")
		}
	}

	seccion("Cortafuegos")
	revisarCortafuegos(cfg, malo)

	seccion("Proxy inverso")
	if cfg.Dominio == "" {
		aviso("no hay dominio configurado, así que no puedo comprobar el proxy")
		codigo("sudo kanpseed config --domain kanpachi.tudominio.com")
	} else {
		tenue("  el proxy tiene que apuntar a 127.0.0.1:%d", cfg.PuertoRegistro)
		tenue("  ejecuta `kanpseed nginx` para ver el bloque completo")
	}

	return resumenDoctor(problemas)
}

func resumenDoctor(problemas int) error {
	fmt.Println()
	if problemas == 0 {
		fmt.Println(cCaja.Render(cOK.Render("Todo en orden")))
		fmt.Println()
		return nil
	}
	fmt.Println(cCaja.Render(cAviso.Render(fmt.Sprintf("%d cosas que revisar", problemas))))
	fmt.Println()
	return fmt.Errorf("el diagnóstico encontró %d problemas", problemas)
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
