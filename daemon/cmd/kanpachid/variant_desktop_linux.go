//go:build linux && desktop && !headless

package main

// El andamio de la interfaz de Linux, que todavía no existe.
//
// # Para qué sirve un fichero que solo rompe la compilación
//
// Porque `-tags desktop` en Linux compila hoy y falla al ARRANCAR: `uihost`
// tiene una mitad sin Windows que devuelve "lanzar la interfaz en la sesión del
// usuario es de Windows", así que el binario sale, se instala, y se planta la
// primera vez que alguien lo enciende. Ese es el peor sitio para enterarse.
//
// Con esto, la misma equivocación es un error de enlazado que nombra lo que
// pasa:
//
//	undefined: theLinuxDesktopUIDoesNotExistYet
//
// # Qué hay que hacer el día que exista
//
// Borrar este fichero, y antes de eso resolver lo que `variant.go` deja
// planteado: el canal de control es un socket 0600 de root, y una interfaz de
// escritorio corre como el usuario, así que no podría abrirlo. Esa decisión es
// de seguridad y hay que tomarla mirándola.

func init() { theLinuxDesktopUIDoesNotExistYet() }
