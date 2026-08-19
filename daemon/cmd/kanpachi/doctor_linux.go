//go:build linux

package main

// Las comprobaciones de Linux, que son las que de verdad hacen falta: acá el
// producto es un servicio en un servidor y nadie mira una ventana.

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/accentiostudios/kanpachi/daemon/adapter/firewall/linux/nftpermits"
	"github.com/accentiostudios/kanpachi/daemon/transport/pipe"
)

// engineLinux es dónde el paquete pone el motor.
//
// Repetido a mano y no importado del cableado del daemon, que es `package main`
// y no lo importa nadie. Es la misma limitación que llevó a hacer
// `daemon/paths`, y esta ruta no bajó ahí porque la mira UNO solo: el daemon la
// construye desde el directorio de su propio binario, así que no hay dos sitios
// que puedan desincronizarse, hay este y una construcción.
const engineLinux = "/usr/libexec/kanpachi/kanpachi-engine"

func chequeosDelSistema() []chequeo {
	return []chequeo{
		chequeoDeTUN(),
		chequeoDelKernel(),
		chequeoDeUnidad("kanpachid", "the service"),
		chequeoDeLaCuarentena(),
		chequeoDelDirectorioDelCanal(),
		chequeoDelCanal(),
		chequeoDelMotor(engineLinux),
		chequeoDeFirewallsAjenos(),
	}
}

func pistaDeElevación() string { return "Try sudo: the channel and the token belong to root." }

// ─── /dev/net/tun ────────────────────────────────────────────────────────────

// tunMajor y tunMinor son los números que Linux le asigna al nodo de TUN.
//
// Se comprueban además de que el fichero exista porque un nodo con los números
// equivocados existe, se abre, y falla al configurarse, que es mucho más difícil
// de leer que "no está".
const (
	tunPath  = "/dev/net/tun"
	tunMajor = 10
	tunMinor = 200
)

func chequeoDeTUN() chequeo {
	return chequeo{
		nombre: "/dev/net/tun",
		mirar: func(context.Context, opciones) veredicto {
			info, err := os.Stat(tunPath)
			if os.IsNotExist(err) {
				return fallar("it is missing, and without it there is no virtual network").
					con("modprobe tun\n" +
						"mknod /dev/net/tun c 10 200 && chmod 0666 /dev/net/tun")
			}
			if err != nil {
				return noSeSabe("could not look at it: %v", err)
			}
			if info.Mode()&os.ModeCharDevice == 0 {
				return fallar("it exists and is not a character device").
					con("rm /dev/net/tun && mknod /dev/net/tun c 10 200")
			}
			st, listo := info.Sys().(*syscall.Stat_t)
			if !listo {
				return noSeSabe("its numbers could not be read")
			}
			ma, mi := mayorMenor(uint64(st.Rdev))
			if ma != tunMajor || mi != tunMinor {
				return fallar("it is %d/%d and has to be %d/%d", ma, mi, tunMajor, tunMinor).
					con("rm /dev/net/tun && mknod /dev/net/tun c 10 200")
			}
			return ok("%d/%d, as it has to be", ma, mi)
		},
		arreglar: func(ctx context.Context, _ opciones) error {
			// `modprobe` primero: en la mayoría de los casos el nodo no está
			// porque el módulo no está cargado, y ahí lo crea el propio kernel
			// con los números correctos. Crearlo a mano antes tapa la causa.
			_ = ejecutar(ctx, "modprobe", "tun")
			if _, err := os.Stat(tunPath); err == nil {
				return nil
			}
			if err := os.MkdirAll("/dev/net", 0o755); err != nil {
				return err
			}
			return ejecutar(ctx, "mknod", tunPath, "c", "10", "200")
		},
	}
}

// mayorMenor parte un rdev en sus dos números, con la codificación de Linux.
//
// No es un desplazamiento y ya: los bits del menor están en DOS trozos, y una
// versión ingenua (`rdev>>8`, `rdev&0xff`) acierta en `10/200` por casualidad y
// falla en cuanto el menor pasa de 255.
func mayorMenor(rdev uint64) (uint32, uint32) {
	mayor := uint32((rdev >> 8) & 0xfff)
	menor := uint32(rdev&0xff) | uint32((rdev>>12)&^uint64(0xff))
	return mayor, menor
}

// ─── El kernel ───────────────────────────────────────────────────────────────

// chequeoDelKernel mira que el kernel traiga lo que hace falta.
//
// **No se arregla**, y no por prudencia: cambiar la configuración del kernel es
// recompilarlo o cambiar de kernel, o sea la máquina del operador y no lo
// nuestro. Además su ausencia se nota antes por otro lado, así que esto sirve
// sobre todo para poner nombre a un fallo que si no parece de Kanpachi.
func chequeoDelKernel() chequeo {
	return chequeo{
		nombre: "the kernel",
		mirar: func(context.Context, opciones) veredicto {
			cfg, dónde, err := configDelKernel()
			if err != nil {
				// Que no esté la configuración es normal en muchas distribuciones,
				// y no es un fallo: se dice que no se sabe, que es la verdad.
				return noSeSabe("could not read the kernel configuration: %v", err)
			}
			faltan := []string{}
			for _, opción := range []string{"CONFIG_TUN", "CONFIG_NF_TABLES", "CONFIG_NF_TABLES_INET"} {
				if !tieneOpción(cfg, opción) {
					faltan = append(faltan, opción)
				}
			}
			if len(faltan) > 0 {
				return fallar("it is missing %s (according to %s)", strings.Join(faltan, ", "), dónde).
					con("This is the kernel configuration, so Kanpachi does not touch it.")
			}
			return ok("TUN and nftables, according to %s", dónde)
		},
	}
}

// configDelKernel devuelve la configuración del kernel que esté a mano.
//
// Los dos sitios habituales, en el orden en que suelen existir: el fichero que
// deja el paquete del kernel en `/boot`, y el que el propio kernel expone
// comprimido cuando se compiló con `CONFIG_IKCONFIG_PROC`.
func configDelKernel() (string, string, error) {
	var uts syscall.Utsname
	if err := syscall.Uname(&uts); err == nil {
		release := aCadena(uts.Release[:])
		ruta := "/boot/config-" + release
		if b, err := os.ReadFile(ruta); err == nil {
			return string(b), ruta, nil
		}
	}

	f, err := os.Open("/proc/config.gz")
	if err != nil {
		return "", "", fmt.Errorf("neither /boot/config-<version> nor /proc/config.gz")
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", "", err
	}
	defer func() { _ = gz.Close() }()
	b, err := io.ReadAll(gz)
	if err != nil {
		return "", "", err
	}
	return string(b), "/proc/config.gz", nil
}

// tieneOpción acepta `=y` y `=m`: compilada dentro o como módulo, las dos valen.
func tieneOpción(cfg, nombre string) bool {
	return strings.Contains(cfg, "\n"+nombre+"=y") ||
		strings.Contains(cfg, "\n"+nombre+"=m") ||
		strings.HasPrefix(cfg, nombre+"=")
}

func aCadena(b []int8) string {
	var sb strings.Builder
	for _, c := range b {
		if c == 0 {
			break
		}
		sb.WriteByte(byte(c))
	}
	return sb.String()
}

// ─── Las unidades ────────────────────────────────────────────────────────────

func chequeoDeUnidad(unidad, nombre string) chequeo {
	return chequeo{
		nombre: nombre,
		mirar: func(ctx context.Context, _ opciones) veredicto {
			activo := salidaDe(ctx, "systemctl", "is-active", unidad)
			habilitado := salidaDe(ctx, "systemctl", "is-enabled", unidad)
			switch activo {
			case "active":
				if habilitado != "enabled" {
					// Corriendo y sin arrancar solo: funciona hoy y no mañana. Es
					// un aviso y no un fallo, porque puede ser lo que el operador
					// quiso.
					return avisar("running, and does NOT start with the machine (%s)", habilitado)
				}
				return ok("running, and starts with the machine")
			case "":
				return noSeSabe("there is no systemctl to ask")
			default:
				return fallar("%s (%s)", activo, habilitado).
					con("systemctl enable --now " + unidad)
			}
		},
		arreglar: func(ctx context.Context, _ opciones) error {
			return ejecutar(ctx, "systemctl", "enable", "--now", unidad)
		},
	}
}

// chequeoDeLaCuarentena es LA comprobación importante, y la que menos se ve.
//
// # Por qué no basta con mirar la unidad
//
// Porque lo que protege la máquina es la TABLA cargada en el kernel, y las dos
// cosas se separan de verdad: la unidad es `oneshot` con `RemainAfterExit`, así
// que figura activa desde que `nft -f` terminó una vez, y un `nft flush ruleset`
// posterior se lleva la tabla sin tocar el estado de la unidad. Ahí `systemctl
// status` diría que todo va bien con los puertos del juego abiertos a internet, y
// nada más en el sistema lo diría.
func chequeoDeLaCuarentena() chequeo {
	return chequeo{
		nombre: "the base quarantine",
		mirar: func(ctx context.Context, _ opciones) veredicto {
			if _, err := os.Stat(nftpermits.QuarantineFile); os.IsNotExist(err) {
				// Sin fichero no hay nada roto: la cuarentena es la DECISIÓN del
				// usuario desde que dejó de aplicarse sola, y el fichero solo
				// existe con la decisión en sí. Se dice dónde se decide.
				return avisar("not in place. It is this machine's decision now: " +
					"`kanpachi quarantine on` closes file sharing and Remote Desktop " +
					"INTO this machine on every network (recommended)")
			}
			puesta, err := nftpermits.QuarantineLoaded(ctx)
			if err != nil {
				return noSeSabe("could not read the ruleset: %v", err)
			}
			if !puesta {
				return fallar("the file is there and the table is NOT loaded: " +
					"the game ports are open from the internet").
					con("systemctl restart kanpachi-quarantine")
			}
			return ok("table inet %s loaded", nftpermits.QuarantineTable)
		},
		arreglar: func(ctx context.Context, _ opciones) error {
			return ejecutar(ctx, "systemctl", "restart", "kanpachi-quarantine")
		},
	}
}

// ─── El canal ────────────────────────────────────────────────────────────────

// chequeoDelDirectorioDelCanal mira lo que de verdad protege el socket.
//
// El directorio y no el socket, porque el directorio es el análogo de
// `ProtectedPrefix` de Windows: sin permiso de entrada nadie llega a mirar el
// socket esté como esté, y con permiso de entrada el modo del socket es lo único
// que queda. Se miran los dos, empezando por el que manda.
func chequeoDelDirectorioDelCanal() chequeo {
	dir := "/run/kanpachi"
	return chequeo{
		nombre: "the channel permissions",
		mirar: func(context.Context, opciones) veredicto {
			info, err := os.Lstat(dir)
			if os.IsNotExist(err) {
				return avisar("%s does not exist yet: the daemon creates it on start", dir)
			}
			if err != nil {
				return noSeSabe("could not look at %s: %v", dir, err)
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return fallar("%s is a symlink, so its permissions mean "+
					"nothing", dir)
			}
			if p := info.Mode().Perm(); p&0o077 != 0 {
				return fallar("%s is %04o and lets the group or others in", dir, p).
					con("chmod 0700 " + dir)
			}
			// El socket solo se mira si el directorio ya está bien: con el
			// directorio abierto, un socket en 0600 no protege nada, y nombrar los
			// dos fallos a la vez esconde cuál es el que manda.
			if s, err := os.Stat(pipe.Name); err == nil {
				if p := s.Mode().Perm(); p&0o077 != 0 {
					return fallar("%s is %04o", pipe.Name, p).
						con("chmod 0600 " + pipe.Name)
				}
			}
			return ok("%s at 0700, socket at 0600", dir)
		},
		arreglar: func(_ context.Context, _ opciones) error {
			if err := os.Chmod(dir, 0o700); err != nil {
				return err
			}
			if _, err := os.Stat(pipe.Name); err == nil {
				return os.Chmod(pipe.Name, 0o600)
			}
			return nil
		},
	}
}

// ─── Lo del operador, que se mira y casi nunca se toca ───────────────────────

// chequeoDeFirewallsAjenos busca lo que puede estar cerrando por encima nuestro.
//
// # Este chequeo tiene la ÚNICA excepción a "lo ajeno no se arregla"
//
// Un gestor que va a tragarse la entrada de los adaptadores de la sala es MAL,
// con arreglo: `--fix` corre exactamente los comandos que el veredicto nombra,
// por el mismo camino con libro que usa la pregunta de `kanpachi host`, y lo
// abierto se cierra al terminar la sala o en el próximo arranque. Escribir
// `--fix` después de leer el veredicto ES el consentimiento, igual de
// explícito que contestar la pregunta. Ver la decisión 36.
//
// Lo demás sigue como siempre: un gestor activo que hoy no bloquea, o una
// cadena de Docker, se nombran y no se tocan. Netfilter sigue sin dejarnos
// abrir por encima de un `drop` ajeno desde nuestra tabla; abrir es pedírselo
// al CLI del gestor, jamás pisarlo.
func chequeoDeFirewallsAjenos() chequeo {
	return chequeo{
		nombre: "firewalls that are not ours",
		mirar: func(ctx context.Context, _ opciones) veredicto {
			bloqueos, err := nftpermits.InboundBlocks(ctx)
			if err != nil {
				return noSeSabe("could not read their posture: %v", err)
			}
			if len(bloqueos) > 0 {
				partes := make([]string, 0, len(bloqueos))
				var comandos []string
				for _, b := range bloqueos {
					partes = append(partes, b.String())
					comandos = append(comandos, b.Fix...)
				}
				return fallar("%s. A room would assemble and nobody would get in",
					strings.Join(partes, "; ")).
					con("By hand: " + strings.Join(comandos, " && ") + "\n" +
						"`kanpachi doctor --fix` runs exactly that, writes it down, and it is\n" +
						"undone when the room closes or on the next service start.")
			}

			encontrados := []string{}
			if strings.Contains(salidaDe(ctx, "ufw", "status"), "Status: active") {
				encontrados = append(encontrados, "ufw")
			}
			if salidaDe(ctx, "systemctl", "is-active", "firewalld") == "active" {
				encontrados = append(encontrados, "firewalld")
			}
			if strings.Contains(salidaDe(ctx, "nft", "list", "ruleset"), "DOCKER-USER") ||
				strings.Contains(salidaDe(ctx, "iptables", "-S"), "DOCKER-USER") {
				encontrados = append(encontrados, "Docker")
			}
			if len(encontrados) == 0 {
				return ok("none is active")
			}
			return avisar("there is %s, not blocking the room today. Its policy can change",
				strings.Join(encontrados, ", ")).
				con("Look yourself: ufw status verbose / firewall-cmd --list-all / nft list ruleset\n" +
					"Kanpachi only ever touches these to let its own two adapters in, with consent.")
		},
		arreglar: func(ctx context.Context, _ opciones) error {
			bloqueos, err := nftpermits.InboundBlocks(ctx)
			if err != nil {
				return err
			}
			return nftpermits.AllowBlocked(ctx, bloqueos, logDelDoctor{})
		},
	}
}

// logDelDoctor is the sliver of logger AllowBlocked wants. The doctor's
// answer is the re-measured verdict, so the daemon-style log lines go nowhere
// on purpose: printing them twice would say more than what was measured.
type logDelDoctor struct{}

func (logDelDoctor) Info(string, ...any)  {}
func (logDelDoctor) Warn(string, ...any)  {}
func (logDelDoctor) Error(string, ...any) {}

// ─── Correr cosas ────────────────────────────────────────────────────────────

// salidaDe corre un comando y devuelve su salida sin espacios, o vacío.
//
// **Se traga el error a propósito**, y es el único sitio del repositorio donde
// eso es correcto: acá el fallo ES la respuesta. `systemctl is-active` sale con
// código distinto de cero justo cuando la unidad no está activa, y `ufw status`
// falla cuando ufw no está instalado, que es la respuesta buena. Quien llama
// distingue por el texto, no por el código.
func salidaDe(ctx context.Context, nombre string, args ...string) string {
	salida, _ := exec.CommandContext(ctx, nombre, args...).Output()
	return strings.TrimSpace(string(salida))
}

// ejecutar corre un arreglo y devuelve lo que dijo si falló.
//
// La salida del comando viaja DENTRO del error: `systemctl` explica por qué no
// pudo, y quedarse solo con "exit status 1" tira justo la parte que sirve.
func ejecutar(ctx context.Context, nombre string, args ...string) error {
	cmd := exec.CommandContext(ctx, nombre, args...)
	salida, err := cmd.CombinedOutput()
	if err != nil {
		texto := strings.TrimSpace(string(salida))
		if texto == "" {
			return fmt.Errorf("%s: %w", nombre, err)
		}
		return fmt.Errorf("%s: %w: %s", nombre, err, texto)
	}
	return nil
}

// rutaDelMotor es la misma ruta que mira el doctor, para `kanpachi version`.
func rutaDelMotor() string { return engineLinux }
