//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows"
)

var (
	kernel32          = windows.NewLazySystemDLL("kernel32.dll")
	procAttachConsole = kernel32.NewProc("AttachConsole")
)

// attachParentProcess es la constante `ATTACH_PARENT_PROCESS`, que es -1 sin
// signo.
const attachParentProcess = ^uintptr(0)

// reengancharConsola devuelve la salida estándar a la consola de quien lanzó
// este proceso, si es que había una.
//
// # Por qué hace falta
//
// El acceso directo del instalador apunta a ESTE binario, así que hacer doble
// clic lo ejecuta. Un programa de Go es de subsistema consola por defecto, y un
// subsistema consola lanzado desde el Explorador abre una ventana negra: es
// exactamente el parpadeo que el producto promete que nadie va a ver, entrando
// por la puerta principal.
//
// La solución es enlazar con `-H windowsgui`, que hace el binario de subsistema
// gráfico y quita la ventana. Y trae su propio problema: un binario gráfico
// arrancado desde una terminal tampoco escribe nada, así que `--console`,
// `--reset` y `--help` se quedan mudos justo donde alguien está mirando.
//
// `AttachConsole(ATTACH_PARENT_PROCESS)` es lo que resuelve las dos mitades a la
// vez: pide la consola del proceso padre, que existe si el padre es una
// terminal y no existe si el padre es el Explorador. Así el doble clic no
// enseña nada y la línea de comandos sigue imprimiendo.
//
// # Por qué es seguro llamarlo siempre
//
// Sin la bandera de enlace, este proceso ya tiene consola propia y
// `AttachConsole` falla con `ERROR_ACCESS_DENIED`. Ahí no hay nada que hacer y
// no se toca nada: las salidas que ya funcionan se quedan como están. O sea que
// esto es correcto con la bandera y sin ella, que es lo que hay que pedirle a
// algo que gobierna si el usuario ve los errores.
// # Y por qué se mira ANTES cuáles faltan
//
// Porque un descriptor que ya sirve no se toca. Medido: `kanpachid --data ...`
// desde PowerShell con la salida redirigida imprimía en la nada. PowerShell
// crea tuberías y se las pasa al hijo, así que `os.Stdout` YA era bueno; la
// primera versión de esto se enganchaba igual y lo reemplazaba por `CONOUT$`,
// o sea que el texto salía por la ventana de la consola en vez de por la
// tubería que alguien estaba leyendo. El error se veía como un binario mudo, y
// no lo habría encontrado ningún test: hay que ejecutarlo con la salida
// redirigida, que es como lo ejecuta cualquier script.
func reengancharConsola() {
	// Se anota antes de enganchar, porque enganchar puede cambiar lo que
	// `GetStdHandle` contesta.
	faltaSalida := !handleVálido(windows.STD_OUTPUT_HANDLE)
	faltaError := !handleVálido(windows.STD_ERROR_HANDLE)
	faltaEntrada := !handleVálido(windows.STD_INPUT_HANDLE)

	if !faltaSalida && !faltaError && !faltaEntrada {
		// Todo heredado: tubería, redirección a fichero, o consola propia. No
		// hay nada roto que arreglar.
		return
	}

	r, _, _ := procAttachConsole.Call(attachParentProcess)
	if r == 0 {
		// O no hay consola de padre, que es el doble clic desde el Explorador,
		// o ya teníamos la nuestra. Los dos se tratan igual: no tocar nada.
		return
	}

	// Enganchada. Ahora hay que APUNTAR ahí SOLO lo que faltaba, porque con
	// `-H windowsgui` los descriptores que nadie heredó nacen inválidos y
	// engancharse a una consola no los arregla solo.
	//
	// Cada uno por separado y sin abortar si uno falla: que no se pueda abrir
	// la entrada no es motivo para quedarse sin salida de errores.
	if faltaSalida || faltaError {
		if out, err := os.OpenFile("CONOUT$", os.O_RDWR, 0); err == nil {
			if faltaSalida {
				os.Stdout = out
			}
			if faltaError {
				os.Stderr = out
			}
		}
	}
	if faltaEntrada {
		if in, err := os.OpenFile("CONIN$", os.O_RDWR, 0); err == nil {
			os.Stdin = in
		}
	}
}

// handleVálido dice si este proceso heredó ese descriptor estándar.
//
// Cero e `InvalidHandle` son las dos formas que tiene Windows de decir que no
// hay nada ahí, y hay que comprobar las dos: un proceso gráfico sin padre que
// le pase nada recibe una, y uno arrancado de otras formas recibe la otra.
func handleVálido(id uint32) bool {
	h, err := windows.GetStdHandle(id)
	return err == nil && h != 0 && h != windows.InvalidHandle
}
