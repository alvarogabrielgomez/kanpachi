//go:build windows

package kanpachi

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// El lanzamiento del proceso hijo, que es la parte con privilegios.
//
// # Lo que este fichero tiene que hacer bien, y qué pasa si no
//
//   - **Lista de argumentos, jamás una cadena de shell.** Es la superficie de
//     inyección real de algo que corre como SYSTEM. Acá además la lista está
//     VACÍA: el motor rechaza cualquier argumento, porque todo lo que necesita
//     llega por el tubo de entrada.
//   - **Entorno explícito, nunca heredado.** Cada bandera de EasyTier tiene
//     gemela por variable de entorno, `ET_CONFIG_SERVER` y `ET_PORT_FORWARD`
//     entre ellas, así que un hijo que hereda el entorno acepta capacidades
//     prohibidas sin que nadie las escriba en ningún sitio. Se le pasa una lista
//     vacía, y esa lista vacía es la decisión.
//   - **Job Object con KILL_ON_JOB_CLOSE.** Sin él, un daemon que muere de forma
//     sucia deja un motor vivo con la red arriba y el firewall ya purgado: la
//     red virtual abierta sin nada conteniéndola.
//   - **Huérfanos al arrancar.** Por si una salida sucia anterior dejó uno.
//
// # Lo que NO hace falta hacer, y conviene saber por qué
//
// No hay que acotar qué handles hereda el hijo: Go ya lo hace. En
// `syscall.StartProcess` de Windows se arma `PROC_THREAD_ATTRIBUTE_HANDLE_LIST`
// con exactamente los tres estándar más `AdditionalInheritedHandles`, con el
// comentario *"Do not accidentally inherit more than these handles"*. O sea que
// el motor no recibe por herencia la sesión de WFP ni el pipe de la UI. La
// consecuencia práctica es una sola: **`AdditionalInheritedHandles` se deja
// vacío, siempre.**

// windowsChild es un motor corriendo, con sus tubos y su job.
type windowsChild struct {
	cmd  *exec.Cmd
	in   io.WriteCloser
	out  io.Reader
	job  windows.Handle
	once sync.Once
	done chan error
}

func (c *windowsChild) Stdin() io.WriteCloser { return c.in }
func (c *windowsChild) Stdout() io.Reader     { return c.out }
func (c *windowsChild) Wait() error           { return <-c.done }

func (c *windowsChild) Kill() error {
	var err error
	c.once.Do(func() {
		// Cerrar el job se lleva a todo lo que haya dentro: el hijo y cualquier
		// cosa que él hubiera arrancado.
		if c.job != 0 {
			err = windows.CloseHandle(c.job)
			c.job = 0
		}
		if c.cmd.Process != nil {
			_ = c.cmd.Process.Kill()
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
	// El directorio de trabajo es el del binario porque `Packet.dll` es una
	// importación DURA del motor: sin ella al lado el proceso no llega ni a
	// arrancar, y lo único que dice Windows es 0xC0000135, sin nombrar cuál
	// falta. Medido sobre el binario propio con `dumpbin /imports`.
	cmd.Dir = filepath.Dir(abs)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

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

	c := &windowsChild{cmd: cmd, in: in, out: out, done: make(chan error, 1)}

	job, err := jailProcess(cmd.Process.Pid)
	if err != nil {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("metiendo el motor en su job: %w", err)
	}
	c.job = job

	go func() { c.done <- cmd.Wait() }()
	return c, nil
}

// jailProcess crea el Job Object y mete al hijo dentro.
//
// `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE` es lo único que garantiza que el motor
// muera con el daemon incluso cuando el daemon muere de forma sucia, sin correr
// ningún `defer`. Es la diferencia entre una máquina que queda limpia y una que
// queda con la red virtual arriba y el firewall ya purgado.
func jailProcess(pid int) (windows.Handle, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, err
	}

	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		_ = windows.CloseHandle(job)
		return 0, err
	}

	h, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		_ = windows.CloseHandle(job)
		return 0, err
	}
	defer func() { _ = windows.CloseHandle(h) }()

	if err := windows.AssignProcessToJobObject(job, h); err != nil {
		_ = windows.CloseHandle(job)
		return 0, err
	}
	return job, nil
}

// killOrphans mata motores que dejó una ejecución anterior.
//
// Un daemon que murió de forma sucia pudo dejar uno vivo. Arrancar otro encima
// daría dos motores peleándose por el mismo adaptador virtual, y el síntoma
// sería una sala que conecta a ratos, que es de los peores de diagnosticar.
//
// **Se compara por RUTA COMPLETA y jamás por nombre.** Matar cualquier proceso
// llamado `kanpachi-engine.exe` sería matar el de otra instalación, y esto corre
// como SYSTEM, o sea que puede.
//
// Los fallos se ignoran a propósito: es limpieza oportunista, y no poder mirar
// un proceso ajeno es lo normal, no un problema.
func killOrphans(abs string) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return
	}
	defer func() { _ = windows.CloseHandle(snap) }()

	base := strings.ToLower(filepath.Base(abs))
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))

	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		if strings.ToLower(windows.UTF16ToString(e.ExeFile[:])) != base {
			continue
		}
		if int(e.ProcessID) == os.Getpid() {
			continue
		}
		h, err := windows.OpenProcess(
			windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.PROCESS_TERMINATE,
			false, e.ProcessID)
		if err != nil {
			continue
		}
		if ruta, err := imagePath(h); err == nil && strings.EqualFold(ruta, abs) {
			_ = windows.TerminateProcess(h, 1)
		}
		_ = windows.CloseHandle(h)
	}
}

// imagePath devuelve la ruta completa del ejecutable de un proceso.
func imagePath(h windows.Handle) (string, error) {
	buf := make([]uint16, windows.MAX_PATH)
	n := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &n); err != nil {
		return "", err
	}
	return windows.UTF16ToString(buf[:n]), nil
}
