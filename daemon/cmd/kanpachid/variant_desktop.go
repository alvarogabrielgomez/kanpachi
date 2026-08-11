//go:build !headless && (windows || desktop)

package main

// La variante de ESCRITORIO: hay una ventana y el daemon la lanza.
//
// Es lo que se compila por omisión en Windows, y en cualquier sistema con
// `-tags desktop`. Ver `variant.go` para la tabla entera.

// hostsUI: acá sí.
//
// El daemon corre como SYSTEM, o sea en otra sesión que la del usuario, así que
// lanzar la ventana desde ahí es todo un adaptador (`uihost`) justamente por eso.
const hostsUI = true

// uiExeName es el ejecutable de la interfaz, al lado del daemon.
//
// Lleva `.exe` porque la única variante de escritorio que existe hoy es la de
// Windows. El día que haya una de Linux, este fichero se parte en dos por
// sistema, que es la partición que `variant.go` describe y que este nombre es lo
// primero que va a pedir.
const uiExeName = "kanpachiui.exe"
