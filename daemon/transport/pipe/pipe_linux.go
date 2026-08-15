//go:build linux

package pipe

// El canal local en Linux: un socket Unix en el sistema de ficheros.
//
// # Por qué del sistema de ficheros y no abstracto
//
// Linux tiene sockets Unix "abstractos" (los que empiezan por `@`), que no
// existen en el disco y no hay que borrar al salir. Son cómodos y no se usan
// acá: **un socket abstracto no tiene permisos**. Cualquier proceso del mismo
// espacio de nombres de red se conecta, sin uid, sin modo y sin directorio que
// lo acote. Es exactamente el agujero que el prefijo protegido cierra en
// Windows, así que elegir el abstracto sería tirar la defensa por comodidad.
//
// Que el socket viva en el disco trae la contrapartida de que puede quedar un
// fichero muerto tras un SIGKILL. Eso lo resuelve [clearDeadSocket], que es un
// problema mucho más chico que el que evita.

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/accentiostudios/kanpachi/core/timing"
)

// socketDir es el directorio del canal, y es la mitad de la seguridad de esto.
//
// **Es el análogo de `ProtectedPrefix` de Windows**, y hace su mismo trabajo por
// otro mecanismo: allá el sistema prohíbe crear el nombre a quien no sea
// administrador; acá el directorio es de root y no tiene ni un bit para nadie
// más, así que un proceso sin privilegios no puede crear el socket antes que el
// daemon ni reemplazarlo después. La diferencia con defenderlo por carrera es la
// misma de siempre: esto no se puede perder por arrancar lento.
//
// `/run` y no `/var/run` (que hoy es un enlace) ni `/tmp`. En `/tmp` cualquiera
// escribe, que es justo lo contrario de lo que hace falta. Con systemd, la
// unidad lo declara con `RuntimeDirectory=kanpachi` y el arranque lo encuentra
// hecho; corriendo a mano lo crea [prepareDir].
const socketDir = "/run/kanpachi"

// Name es el canal del producto instalado.
const Name = socketDir + "/api.sock"

// PortableName existe para que las tres direcciones sigan siendo tres.
//
// **En Linux no hay producto portable**, y es una decisión escrita: el portable
// de Windows sirve para correr sin instalar y sin privilegios permanentes, y acá
// haría falta CAP_NET_ADMIN igual, o sea `sudo` en cada arranque a cambio de
// nada. Así que a esta dirección no llega ningún camino.
//
// Se declara igual porque el identificador es parte de la API del paquete y
// `main.go` lo nombra sin preguntar en qué sistema corre. Dejarlo valiendo lo
// mismo que [Name] sería peor que no tenerlo: convertiría una rama muerta en una
// colisión silenciosa el día que alguien la despierte.
const PortableName = socketDir + "/portable.sock"

// ConsoleName es el del modo desarrollo, y es OTRO por lo mismo que en Windows.
//
// Compartir dirección con producción haría que arrancar el binario real con
// `--console` ocupara el canal del daemon de verdad. Acá el directorio ya impide
// que lo haga alguien sin privilegios; lo que impide es que lo haga el operador
// sin darse cuenta.
const ConsoleName = socketDir + "/console.sock"

// SecurityDescriptor es el modo del socket, en octal.
//
// El mismo identificador que en Windows y otro mecanismo: allá es una DACL en
// SDDL, acá son los nueve bits de permiso. Lo que comparten es lo único que
// [Listen] necesita saber, que es que **sin esto el valor por omisión es el
// permisivo**: un socket recién creado toma `0777 &^ umask`, o sea 0755 con el
// umask de siempre, y eso lo abre cualquiera de la máquina.
//
// # Por qué 0600 y no 0660 con un grupo
//
// Un grupo `kanpachi` al que se añadan los operadores parece más cómodo y da
// exactamente el poder de abrir salas, cambiar el firewall y matar el motor, o
// sea el poder de root por otra puerta. Quien puede administrar Kanpachi ya
// puede ser root en esa máquina; darle una vía que NO pasa por `sudo` solo quita
// el registro de quién hizo qué.
const SecurityDescriptor = "0600"

// abrirPipe crea el socket Unix de verdad.
//
// El orden de las comprobaciones no es cosmético, y es este:
//
//  1. el descriptor, porque es la regla que el job de Linux prueba y que dejaría
//     de ser alcanzable si algo fallara antes;
//  2. la dirección, porque una ruta relativa o abstracta se salta el directorio;
//  3. el directorio, porque es lo que impide que nos ocupen el nombre;
//  4. el socket muerto, porque si no el arranque tras un SIGKILL falla para
//     siempre;
//  5. escuchar, y recién ahí el modo.
func abrirPipe(nombre, descriptor string) (net.Listener, error) {
	// La regla PRIMERO y el sistema después, igual que en `pipe_other.go`: al
	// revés, la negativa a escuchar sin descriptor se vuelve inalcanzable para el
	// único CI que puede probarla.
	if descriptor == "" {
		return nil, ErrSinDescriptor
	}
	mode, err := parseMode(descriptor)
	if err != nil {
		return nil, err
	}

	// Absoluta, y eso descarta de paso el socket abstracto: `@kanpachi` no
	// empieza por `/`. Ver la cabecera del fichero.
	if !filepath.IsAbs(nombre) {
		return nil, fmt.Errorf("pipe: %q no es una ruta absoluta, y el canal tiene que estar "+
			"en un directorio que podamos proteger", nombre)
	}
	if err := prepareDir(filepath.Dir(nombre)); err != nil {
		return nil, err
	}
	if err := clearDeadSocket(nombre); err != nil {
		return nil, err
	}

	ln, err := net.Listen("unix", nombre)
	if err != nil {
		return nil, err
	}
	// La ventana entre crear el socket y ponerle el modo es real: en ese instante
	// tiene `0777 &^ umask`. Lo que la cierra NO es la velocidad de estas dos
	// líneas, es [prepareDir]: sin permiso de entrada al directorio nadie llega a
	// mirar el socket, esté como esté. Es la misma razón por la que el directorio
	// va en 0700 y no en 0755.
	if err := os.Chmod(nombre, mode); err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("pipe: poniendo el modo %04o a %s: %w", mode, nombre, err)
	}
	// Cerrar el oyente borra el fichero, que es lo que hace `net.UnixListener`
	// cuando fue él quien lo creó. Por eso el camino de salida limpio no deja
	// nada, y [clearDeadSocket] solo tiene trabajo cuando el daemon murió sucio.
	return ln, nil
}

// parseMode lee el descriptor y se niega a un modo que abra el canal a otros.
//
// Rechazar los bits de grupo y otros va acá y no en la constante porque una
// constante no la comprueba nadie: el día que alguien pase "0666" para depurar,
// esto lo para. Un descriptor permisivo es tan malo como el descriptor vacío, y
// el vacío ya es [ErrSinDescriptor].
func parseMode(descriptor string) (os.FileMode, error) {
	n, err := strconv.ParseUint(descriptor, 8, 32)
	if err != nil {
		return 0, fmt.Errorf("pipe: el descriptor %q no es un modo en octal: %w", descriptor, err)
	}
	mode := os.FileMode(n)
	if mode&0o077 != 0 {
		return 0, fmt.Errorf("pipe: el modo %04o le da permisos al grupo o a otros, y este canal "+
			"abre salas y escribe en el firewall", mode)
	}
	return mode, nil
}

// prepareDir deja el directorio del canal como tiene que estar, o se niega.
//
// # Qué se arregla y qué se rechaza
//
// Si el directorio es NUESTRO y quedó con permisos de más, se aprieta: es
// nuestro, y arreglar lo propio es lo que hace el producto en todas partes. Si
// es de otro usuario, se rechaza sin tocarlo: eso ya no es "quedó mal", es que
// alguien está donde no debería, y apretarle los permisos a un directorio ajeno
// sería a la vez inútil y una modificación de la máquina de otro.
func prepareDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("pipe: creando %s: %w", dir, err)
	}

	// Lstat y no Stat: `Stat` sigue los enlaces, así que un enlace plantado ahí
	// pasaría por directorio bueno y estaríamos protegiendo el enlace mientras el
	// socket sale en el destino, que es de cualquiera.
	info, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("pipe: mirando %s: %w", dir, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("pipe: %s es un enlace simbólico, y el canal tiene que vivir en un "+
			"directorio de verdad para que sus permisos signifiquen algo", dir)
	}
	if !info.IsDir() {
		return fmt.Errorf("pipe: %s existe y no es un directorio", dir)
	}

	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("pipe: no se pudo leer el dueño de %s", dir)
	}
	if me := os.Geteuid(); int(st.Uid) != me {
		return fmt.Errorf("pipe: %s es del uid %d y este proceso es el uid %d.\n"+
			"  El directorio del canal tiene que ser de quien corre el daemon: es lo que impide "+
			"que otro proceso ocupe la dirección antes que él", dir, st.Uid, me)
	}
	if info.Mode().Perm()&0o077 != 0 {
		if err := os.Chmod(dir, 0o700); err != nil {
			return fmt.Errorf("pipe: %s deja entrar al grupo o a otros y no se pudo apretar: %w",
				dir, err)
		}
	}
	return nil
}

// clearDeadSocket se lleva el socket que dejó un daemon que murió sucio.
//
// # Por qué se pregunta antes de borrar
//
// Borrar sin preguntar convertiría el segundo daemon en el ladrón del canal del
// primero: el fichero desaparece, el que estaba escuchando se queda con un
// socket sin nombre y las conexiones nuevas van al recién llegado. Es la misma
// clase de fallo que FILE_FLAG_FIRST_PIPE_INSTANCE impide en Windows, y por eso
// acá la regla es la de allá: si ya hay alguien, se falla.
//
// Solo se borra con la respuesta que **prueba** que no hay nadie, que es
// ECONNREFUSED: el fichero está y el kernel dice que nadie escucha. Cualquier
// otro error deja el socket donde está, incluido el plazo agotado, porque un
// daemon vivo y atascado también deja de contestar.
func clearDeadSocket(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("pipe: mirando %s: %w", path, err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("pipe: %s existe y no es un socket, así que no lo borra nadie desde acá",
			path)
	}

	c, err := net.DialTimeout("unix", path, timing.SocketProbeTimeout)
	if err == nil {
		_ = c.Close()
		return fmt.Errorf("pipe: ya hay un daemon escuchando en %s", path)
	}
	if !errors.Is(err, syscall.ECONNREFUSED) {
		return fmt.Errorf("pipe: %s existe y no se pudo saber si está vivo, así que se deja "+
			"donde está: %w", path, err)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("pipe: borrando el socket muerto %s: %w", path, err)
	}
	return nil
}

// checkPeer pregunta al kernel QUIÉN se conectó, y corta si no es de casa.
//
// # Para qué, si el modo ya es 0600
//
// Porque las dos cosas fallan distinto. El modo lo pone este proceso al arrancar
// y depende de que el directorio, el umask y el chmod hayan salido bien; esto lo
// contesta el kernel por cada conexión, con el uid REAL del proceso del otro
// lado, y no hay forma de falsearlo desde el cliente. Si algún día el modo queda
// mal por un cambio en la unidad de systemd o por un directorio preexistente,
// esto sigue cerrado.
//
// Se acepta el uid propio y el 0. Root ya puede leer el token, escribir en el
// firewall y matar el proceso, así que rechazarlo no protegería nada y solo haría
// que `sudo kanpachi` fallara en una máquina donde el daemon corre con su propio
// usuario.
//
// # El caso de los tests
//
// Los tests inyectan su oyente por `Deps.listen` y sus conexiones son `net.Pipe`,
// que no tiene dueño que preguntar. Ahí esto deja pasar: la alternativa sería que
// todo el paquete dejara de poder probarse, y lo que de verdad decide quién entra
// en producción es el modo del socket más el directorio.
func checkPeer(c net.Conn) error {
	unixConn, ok := c.(*net.UnixConn)
	if !ok {
		return nil
	}
	raw, err := unixConn.SyscallConn()
	if err != nil {
		return fmt.Errorf("pipe: no se pudo mirar quién se conectó: %w", err)
	}

	var cred *syscall.Ucred
	var credErr error
	if err := raw.Control(func(fd uintptr) {
		cred, credErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); err != nil {
		return fmt.Errorf("pipe: no se pudo mirar quién se conectó: %w", err)
	}
	if credErr != nil {
		return fmt.Errorf("pipe: no se pudo leer las credenciales del otro lado: %w", credErr)
	}

	me := os.Geteuid()
	if int(cred.Uid) != me && cred.Uid != 0 {
		return fmt.Errorf("se conectó el uid %d (pid %d) y este canal es del uid %d",
			cred.Uid, cred.Pid, me)
	}
	return nil
}
