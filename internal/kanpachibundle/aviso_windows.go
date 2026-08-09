//go:build windows

package main

import "golang.org/x/sys/windows"

// avisar pone un cuadro de diálogo en el escritorio.
//
// # Por qué hace falta uno, y no un `fmt.Println`
//
// Porque este binario se enlaza con `-H windowsgui` y **no tiene consola**. Se
// hizo así por dos cosas que se ven en pantalla: una ventana negra abierta
// durante toda la sesión de juego, y un SEGUNDO icono en la barra de tareas,
// que hacía parecer que Kanpachi se abrió dos veces.
//
// Lo que eso cuesta es que un fallo dejaría de verse. Un `Println` sin consola
// escribe en un handle inválido, o sea en la nada, y lo que le llega a quien
// abrió el archivo es un icono que parpadea y desaparece. Este es el único
// canal que queda.
//
// `MB_SETFOREGROUND` porque quien acaba de hacer doble clic está mirando la
// pantalla, y un cuadro que nace detrás de la ventana del navegador es lo mismo
// que no haberlo puesto.
func avisar(titulo, texto string) {
	t, err := windows.UTF16PtrFromString(texto)
	if err != nil {
		return
	}
	c, err := windows.UTF16PtrFromString(titulo)
	if err != nil {
		return
	}
	_, _ = windows.MessageBox(0, t, c,
		windows.MB_OK|windows.MB_ICONERROR|windows.MB_SETFOREGROUND)
}
