//go:build linux

package kanpachi

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

// El lanzamiento del proceso hijo, que es la parte con privilegios.
//
// # Lo que este fichero tiene que hacer bien, y qué pasa si no
//
// Las dos primeras reglas son las MISMAS que en Windows, y no por simetría: son
// del producto, no del sistema.
//
//   - **Lista de argumentos, jamás una cadena de shell.** Es la superficie de
//     inyección real de algo que corre como root. Acá además la lista está
//     VACÍA: el motor rechaza cualquier argumento, porque todo lo que necesita
//     llega por el tubo de entrada.
//   - **Entorno explícito, nunca heredado.** Cada bandera de EasyTier tiene
//     gemela por variable de entorno, `ET_CONFIG_SERVER` y `ET_PORT_FORWARD`
//     entre ellas, así que un hijo que hereda el entorno acepta capacidades
//     prohibidas sin que nadie las escriba en ningún sitio. Se le pasa una lista
//     vacía, y esa lista vacía es la decisión.
//
// # Que el motor muera con el daemon: acá son TRES capas y en Windows una
//
// En Windows lo resuelve el Job Object con `KILL_ON_JOB_CLOSE`, que el kernel
// respeta aunque el daemon muera sin correr un solo `defer`. Linux no tiene un
// equivalente exacto en el proceso, así que la garantía se arma con tres cosas
// que fallan por motivos distintos:
//
//  1. **El cgroup de systemd.** Es la capa fuerte y la que vale en producción.
//     La unidad corre con el `KillMode=control-group` de siempre, o sea que al
//     morir el proceso principal systemd se lleva TODO lo que quede en el
//     cgroup del servicio, y el motor está ahí por haber nacido dentro. Es el
//     análogo real del Job Object, y no lo pone este fichero: lo pone la unidad.
//  2. **`Pdeathsig`.** Cubre el daemon corrido a mano, que es como se prueba y
//     como arranca `--console`. Su límite hay que decirlo: `PR_SET_PDEATHSIG`
//     se dispara cuando muere el HILO que lanzó el proceso, no el proceso
//     entero, y Go mueve goroutines entre hilos. En la práctica el planificador
//     de Go aparca los hilos en vez de terminarlos, así que el caso raro es que
//     el motor muera de más y no de menos, que es el lado seguro del error.
//  3. **Los huérfanos al arrancar.** Es el respaldo de los otros dos y ya
//     existía en el diseño. Ver [killOrphans].
//
// `Setpgid` va con la primera: pone al motor y a lo que él arranque en un grupo
// propio, para que [linuxChild.Kill] pueda llevarse el grupo entero en vez de
// solo al hijo directo.

// linuxChild es un motor corriendo, con sus tubos y su grupo de procesos.
type linuxChild struct {
	cmd  *exec.Cmd
	in   io.WriteCloser
	out  io.Reader
	once sync.Once
	done chan error
}

func (c *linuxChild) Stdin() io.WriteCloser { return c.in }
func (c *linuxChild) Stdout() io.Reader     { return c.out }
func (c *linuxChild) Wait() error           { return <-c.done }

// Kill se lleva el GRUPO, que es lo que `Setpgid` hace posible.
//
// Matar solo al hijo directo dejaría vivo cualquier proceso que él hubiera
// arrancado, y ese es exactamente el estado que hay que impedir: algo hablando
// por la red virtual con el firewall ya purgado. Es lo mismo que consigue
// cerrar el Job Object en Windows.
func (c *linuxChild) Kill() error {
	var err error
	c.once.Do(func() {
		if c.cmd.Process == nil {
			return
		}
		// El negativo es el grupo entero. Si falla, queda el hijo directo, que
		// es mejor que nada.
		if err = syscall.Kill(-c.cmd.Process.Pid, syscall.SIGKILL); err != nil {
			err = c.cmd.Process.Kill()
		}
	})
	return err
}

// spawn lanza el motor.
func spawn(ctx context.Context, exe string) (child, error) {
	abs, err := filepath.Abs(exe)
	if err != nil {
		return nil, fmt.Errorf("resolviendo la ruta del motor: %w", err)
	}
	if _, err := os.Stat(abs); err != nil {
		return nil, fmt.Errorf("no está el motor en %s: %w", abs, err)
	}

	killOrphans(abs)

	cmd := exec.CommandContext(ctx, abs)
	// Sin argumentos, y el motor los rechaza además por su cuenta.
	cmd.Args = []string{abs}
	// La lista VACÍA es el punto, no un descuido. Ver la cabecera.
	cmd.Env = []string{}
	// El directorio de trabajo es el del binario. En Windows hace falta por una
	// DLL que se busca al lado; acá no hace falta y se pone igual, porque un
	// proceso privilegiado con el directorio de trabajo de quien lo lanzó
	// escribe donde no debería el día que escriba algo.
	cmd.Dir = filepath.Dir(abs)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid:   true,
		Pdeathsig: syscall.SIGKILL,
	}

	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("abriendo el tubo de entrada del motor: %w", err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("abriendo el tubo de salida del motor: %w", err)
	}
	// El stderr del motor va al del daemon y no se descarta: ahí es donde avisa
	// de un mensaje ilegible, que es justo lo que hay que ver cuando algo no
	// cuadra.
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("lanzando el motor: %w", err)
	}

	c := &linuxChild{cmd: cmd, in: in, out: out, done: make(chan error, 1)}
	go func() { c.done <- cmd.Wait() }()
	return c, nil
}

// killOrphans mata motores que dejó una ejecución anterior.
//
// Un daemon que murió de forma sucia pudo dejar uno vivo. Arrancar otro encima
// daría dos motores peleándose por el mismo adaptador virtual, y el síntoma
// sería una sala que conecta a ratos, que es de los peores de diagnosticar.
//
// **Se compara por RUTA COMPLETA y jamás por nombre.** Matar cualquier proceso
// llamado `kanpachi-engine` sería matar el de otra instalación, y esto corre
// como root, o sea que puede.
//
// La ruta la contesta `/proc/<pid>/exe`, que es un enlace al ejecutable de
// verdad y no al `argv[0]`, o sea que no se puede falsear desde el proceso
// mirado. Es lo mismo que `QueryFullProcessImageName` da en Windows.
//
// Los fallos se ignoran a propósito: es limpieza oportunista, y no poder mirar
// un proceso ajeno es lo normal, no un problema.
//
// Devuelve CUÁNTOS mató, y no un error, por lo mismo. Un error acá no diría
// nada útil; el número sí, porque un motor huérfano es la explicación de una
// sala que conecta a ratos, y `--reset` existe justo para el caso en que nadie
// puede diagnosticar nada.
func killOrphans(abs string) int {
	entradas, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}

	yo := os.Getpid()
	muertos := 0
	for _, e := range entradas {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == yo {
			// Lo que no es un número son las entradas que `/proc` tiene aparte
			// de los procesos: `self`, `net`, `sys` y compañía.
			continue
		}
		ruta, err := os.Readlink(filepath.Join("/proc", e.Name(), "exe"))
		if err != nil {
			// Un proceso del kernel no tiene ejecutable, y uno de otro usuario
			// no se deja mirar. Ninguna de las dos cosas es un problema.
			continue
		}
		if rutaDelEjecutable(ruta) != abs {
			continue
		}
		if syscall.Kill(pid, syscall.SIGKILL) == nil {
			muertos++
		}
	}
	return muertos
}

// rutaDelEjecutable quita el sufijo que el kernel le pone al binario borrado.
//
// **Este caso NO es raro: es el de después de actualizar.** `apt` reemplaza el
// fichero, y desde ese instante el motor que siguiera vivo tiene un
// `/proc/<pid>/exe` que dice `/usr/libexec/kanpachi/kanpachi-engine (deleted)`.
// Sin quitar el sufijo, el huérfano de la versión anterior no coincide con
// nada, sobrevive a la actualización, y se queda peleando por el adaptador con
// el motor nuevo.
//
// El corte es por el final y por la cadena exacta, así que un binario que de
// verdad se llamara así seguiría comparándose por su nombre entero.
func rutaDelEjecutable(enlace string) string {
	return strings.TrimSuffix(enlace, " (deleted)")
}
