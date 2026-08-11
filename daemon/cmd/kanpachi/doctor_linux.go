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
		chequeoDeUnidad("kanpachid", "el servicio"),
		chequeoDeLaCuarentena(),
		chequeoDelDirectorioDelCanal(),
		chequeoDelCanal(),
		chequeoDelMotor(engineLinux),
		chequeoDeFirewallsAjenos(),
	}
}

func pistaDeElevación() string { return "Prueba con sudo: el canal y el token son de root." }

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
				return fallar("no está, y sin él no hay red virtual").
					con("modprobe tun\n" +
						"mknod /dev/net/tun c 10 200 && chmod 0666 /dev/net/tun")
			}
			if err != nil {
				return noSeSabe("no se pudo mirar: %v", err)
			}
			if info.Mode()&os.ModeCharDevice == 0 {
				return fallar("existe y no es un dispositivo de caracteres").
					con("rm /dev/net/tun && mknod /dev/net/tun c 10 200")
			}
			st, listo := info.Sys().(*syscall.Stat_t)
			if !listo {
				return noSeSabe("no se pudieron leer sus números")
			}
			ma, mi := mayorMenor(uint64(st.Rdev))
			if ma != tunMajor || mi != tunMinor {
				return fallar("es %d/%d y tiene que ser %d/%d", ma, mi, tunMajor, tunMinor).
					con("rm /dev/net/tun && mknod /dev/net/tun c 10 200")
			}
			return ok("%d/%d, como tiene que ser", ma, mi)
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
		nombre: "el kernel",
		mirar: func(context.Context, opciones) veredicto {
			cfg, dónde, err := configDelKernel()
			if err != nil {
				// Que no esté la configuración es normal en muchas distribuciones,
				// y no es un fallo: se dice que no se sabe, que es la verdad.
				return noSeSabe("no se pudo leer la configuración del kernel: %v", err)
			}
			faltan := []string{}
			for _, opción := range []string{"CONFIG_TUN", "CONFIG_NF_TABLES", "CONFIG_NF_TABLES_INET"} {
				if !tieneOpción(cfg, opción) {
					faltan = append(faltan, opción)
				}
			}
			if len(faltan) > 0 {
				return fallar("le falta %s (según %s)", strings.Join(faltan, ", "), dónde).
					con("Es la configuración del kernel, así que esto no lo toca Kanpachi.")
			}
			return ok("TUN y nftables, según %s", dónde)
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
		return "", "", fmt.Errorf("ni /boot/config-<versión> ni /proc/config.gz")
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
					return avisar("corriendo, y NO arranca con la máquina (%s)", habilitado)
				}
				return ok("corriendo, y arranca con la máquina")
			case "":
				return noSeSabe("no hay systemctl que preguntar")
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
		nombre: "la cuarentena de base",
		mirar: func(ctx context.Context, _ opciones) veredicto {
			if _, err := os.Stat(nftpermits.QuarantineFile); os.IsNotExist(err) {
				// Antes del primer arranque del daemon no hay fichero, y eso es
				// normal: lo escribe él. No hay nada roto que arreglar.
				return avisar("todavía no hay %s: la escribe el daemon al arrancar",
					nftpermits.QuarantineFile)
			}
			puesta, err := nftpermits.QuarantineLoaded(ctx)
			if err != nil {
				return noSeSabe("no se pudo leer el ruleset: %v", err)
			}
			if !puesta {
				return fallar("el fichero está y la tabla NO está cargada: " +
					"los puertos del juego están abiertos desde internet").
					con("systemctl restart kanpachi-quarantine")
			}
			return ok("tabla inet %s cargada", nftpermits.QuarantineTable)
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
		nombre: "los permisos del canal",
		mirar: func(context.Context, opciones) veredicto {
			info, err := os.Lstat(dir)
			if os.IsNotExist(err) {
				return avisar("todavía no existe %s: lo crea el daemon al arrancar", dir)
			}
			if err != nil {
				return noSeSabe("no se pudo mirar %s: %v", dir, err)
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return fallar("%s es un enlace simbólico, así que sus permisos no "+
					"significan nada", dir)
			}
			if p := info.Mode().Perm(); p&0o077 != 0 {
				return fallar("%s está en %04o y deja entrar al grupo o a otros", dir, p).
					con("chmod 0700 " + dir)
			}
			// El socket solo se mira si el directorio ya está bien: con el
			// directorio abierto, un socket en 0600 no protege nada, y nombrar los
			// dos fallos a la vez esconde cuál es el que manda.
			if s, err := os.Stat(pipe.Name); err == nil {
				if p := s.Mode().Perm(); p&0o077 != 0 {
					return fallar("%s está en %04o", pipe.Name, p).
						con("chmod 0600 " + pipe.Name)
				}
			}
			return ok("%s en 0700, socket en 0600", dir)
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

// ─── Lo del operador, que se mira y no se toca ───────────────────────────────

// chequeoDeFirewallsAjenos busca lo que puede estar cerrando por encima nuestro.
//
// **No tiene arreglo, y esa ausencia es la regla.** Kanpachi no puede abrir por
// encima de un `drop` ajeno —netfilter no lo permite, por la misma propiedad que
// hace fuerte a nuestra compuerta— y tocarle la configuración del firewall a
// quien administra el servidor no es una opción. Se nombra, se dice el comando
// con el que lo miraría él, y se para ahí.
func chequeoDeFirewallsAjenos() chequeo {
	return chequeo{
		nombre: "firewalls que no son nuestros",
		mirar: func(ctx context.Context, _ opciones) veredicto {
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
				return ok("no hay ninguno activo")
			}
			return avisar("hay %s. Kanpachi NO puede abrir por encima de su bloqueo",
				strings.Join(encontrados, ", ")).
				con("Míralo tú: ufw status verbose / firewall-cmd --list-all / nft list ruleset\n" +
					"Kanpachi no toca esto ni con --fix.")
		},
	}
}

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
