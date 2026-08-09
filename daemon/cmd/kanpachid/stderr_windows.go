//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows"
)

// redirigirStderr apunta la salida de errores del PROCESO al archivo dado.
//
// # El agujero que esto tapa, medido con el servicio de verdad
//
// El daemon se enlaza con `-H windowsgui` y corre como servicio, así que no
// tiene consola ni salida de errores: `GetStdHandle(STD_ERROR_HANDLE)` devuelve
// un handle inválido. Un pánico de Go escribe su traza por ahí, o sea que la
// escribe en la nada.
//
// El resultado es que el daemon puede morirse sin dejar UNA sola línea. No es
// hipotético: el 2026-08-08 el registro de eventos de Windows anotó dos veces
// *"The Kanpachi service terminated unexpectedly"*, con el servicio
// reiniciándose solo a los 5 y a los 10 segundos, y el log del daemon no tiene
// nada entre la última línea normal y el arranque siguiente. Sin la traza no
// hay forma de saber qué se cayó.
//
// # Por qué `SetStdHandle` alcanza, y por qué había que medirlo
//
// Porque el runtime de Go resuelve el handle con `GetStdHandle` en CADA
// escritura, no lo cachea al arrancar. Si lo cacheara, cambiarlo después no
// serviría de nada y haría falta otro mecanismo entero.
//
// Medido antes de escribir esto, con un binario `-H windowsgui` que hace
// `SetStdHandle` a un archivo y entra en pánico: el archivo queda con
// `panic: ...` y su goroutine, y el proceso sale con código 2.
//
// Se ponen las DOS mitades porque son cosas distintas: `SetStdHandle` es lo que
// lee el runtime para un pánico, y `os.Stderr` es a donde escribe el código
// normal, incluido el `cmd.Stderr` del motor. Con las dos, la traza de un
// pánico y lo que diga el motor caen en el mismo sitio.
func redirigirStderr(f *os.File) error {
	if err := windows.SetStdHandle(windows.STD_ERROR_HANDLE, windows.Handle(f.Fd())); err != nil {
		return err
	}
	os.Stderr = f
	return nil
}
