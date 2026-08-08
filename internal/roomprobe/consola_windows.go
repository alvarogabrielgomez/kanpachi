//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"golang.org/x/sys/windows"
)

// elevar vuelve a lanzar este binario como administrador si no lo es ya.
//
// Devuelve `true` cuando lanzó al hijo y este proceso tiene que terminar sin
// hacer nada más.
//
// Hace falta administrador porque se escribe en el firewall de Windows y el
// motor crea adaptadores virtuales. NO hace falta SYSTEM: eso solo lo exige el
// daemon instalado, para cruzar a la sesión del usuario con `WTSQueryUserToken`.
func elevar() (bool, error) {
	if windows.GetCurrentProcessToken().IsElevated() {
		return false, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return false, fmt.Errorf("no se pudo saber qué ejecutable relanzar: %w", err)
	}

	// Los argumentos van como LISTA de PowerShell, uno por elemento.
	//
	// Antes se juntaban en una sola cadena y se volvían a comillar enteros, y
	// con eso `-data "C:\ruta con espacios"` llegaba al hijo como UN argumento
	// con las comillas dentro. Cada elemento se comilla solo, doblando la
	// comilla simple, que es como PowerShell escapa dentro de literales.
	var lista []string
	for _, a := range os.Args[1:] {
		lista = append(lista, "'"+strings.ReplaceAll(a, "'", "''")+"'")
	}
	orden := "Start-Process -FilePath '" + strings.ReplaceAll(exe, "'", "''") + "' -Verb RunAs"
	if len(lista) > 0 {
		orden += " -ArgumentList " + strings.Join(lista, ",")
	}

	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", orden)
	if err := cmd.Run(); err != nil {
		return false, fmt.Errorf("hace falta una consola de administrador, y no se pudo pedir: %w", err)
	}
	return true, nil
}

// consola es el dueño de la terminal mientras la vista está dibujando.
//
// # Por qué hay un dueño y se traspasa a mano
//
// Porque hay tres candidatos a escribir: survey mientras pregunta, el bucle de
// la vista mientras dibuja, y el log. **El log ya no escribe a pantalla nunca**
// (ver [logRoomprobe]), así que quedan dos, y se turnan: `crudo` mientras dibuja
// la vista, `cocido` antes de cualquier pregunta. Dos a la vez dejan basura.
type consola struct {
	entrada windows.Handle
	salida  windows.Handle
	modo0   uint32
	tiene0  bool
}

func abrirConsola() *consola {
	c := &consola{
		entrada: windows.Handle(os.Stdin.Fd()),
		salida:  windows.Handle(os.Stdout.Fd()),
	}
	// Secuencias de escape en la salida. Sin esto, la vista pinta los códigos
	// en crudo en vez de mover el cursor.
	var modoSalida uint32
	if err := windows.GetConsoleMode(c.salida, &modoSalida); err == nil {
		_ = windows.SetConsoleMode(c.salida, modoSalida|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING)
	}
	if err := windows.GetConsoleMode(c.entrada, &c.modo0); err == nil {
		c.tiene0 = true
	}
	return c
}

// crudo apaga el eco, la edición de línea y —esto es lo importante— los eventos
// que no son de teclado.
//
// `ENABLE_MOUSE_INPUT` y `ENABLE_WINDOW_INPUT` se limpian porque
// [consola.esperarTecla] espera a que el handle se señale, y el handle se
// señala con CUALQUIER registro de entrada: sin limpiarlos, pasar el ratón por
// encima de la ventana despierta la espera y el `Read` de después se queda
// bloqueado esperando una tecla que nadie pulsó, con el redibujado parado
// detrás. `x/term.MakeRaw` no las limpia, por eso esto se hace a mano.
//
// Con `ENABLE_PROCESSED_INPUT` apagado, Ctrl+C deja de ser una señal y llega
// como el byte 0x03. Quien llame tiene que atenderlo.
func (c *consola) crudo() {
	if !c.tiene0 {
		return
	}
	const apagar = windows.ENABLE_LINE_INPUT | windows.ENABLE_ECHO_INPUT |
		windows.ENABLE_PROCESSED_INPUT | windows.ENABLE_MOUSE_INPUT | windows.ENABLE_WINDOW_INPUT
	_ = windows.SetConsoleMode(c.entrada, c.modo0&^apagar)
}

// cocido repone el modo original. **Lo llama todo camino de salida**: sin esto
// la terminal queda sin eco después de que el proceso termine, y quien la use
// después escribe a ciegas.
func (c *consola) cocido() {
	if c.tiene0 {
		_ = windows.SetConsoleMode(c.entrada, c.modo0)
	}
}

// esperarTecla espera hasta `d` a que llegue una tecla.
//
// Devuelve `false` si venció el plazo, que es el caso NORMAL: es lo que le da
// al bucle de la vista su latido sin quemar CPU y sin una goroutine lectora que
// después no se pueda cancelar.
func (c *consola) esperarTecla(d time.Duration) (byte, bool) {
	ev, err := windows.WaitForSingleObject(c.entrada, uint32(d.Milliseconds()))
	if err != nil || ev != uint32(windows.WAIT_OBJECT_0) {
		return 0, false
	}
	var b [1]byte
	n, err := os.Stdin.Read(b[:])
	if err != nil || n == 0 {
		return 0, false
	}
	// Se tira el resto de la ráfaga: una flecha son tres bytes y aquí solo
	// interesa la primera tecla de cada pulsación.
	_ = windows.FlushConsoleInputBuffer(c.entrada)
	return b[0], true
}

// Las tres secuencias que usa la vista.
//
// Cursor al origen y borrar de ahí hacia abajo, en vez de borrar la pantalla
// entera antes de escribirla. A un fotograma por segundo, borrarla entera
// parpadea de forma insoportable; así se sobreescribe y solo se borra lo que
// sobra.
func (c *consola) alOrigen()      { fmt.Print("\033[H") }
func (c *consola) borrarElResto() { fmt.Print("\033[0J") }
func (c *consola) limpiar()       { fmt.Print("\033[H\033[2J") }
