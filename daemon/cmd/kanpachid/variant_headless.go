//go:build headless || (!windows && !desktop)

package main

// La variante HEADLESS: no hay ventana, y el cliente es el CLI.
//
// Es lo que se compila por omisión fuera de Windows, y en cualquier sistema con
// `-tags headless`. Ver `variant.go` para la tabla entera.

// hostsUI: acá no, y no es un pendiente.
//
// El cliente es un CLI que arranca el usuario cuando quiere, no algo que el
// daemon levante en la sesión de nadie. Un servidor headless no tiene sesión
// donde levantarla.
//
// Sin esta condición el arranque como servicio moría con `uihost: lanzar la
// interfaz en la sesión del usuario es de Windows`, y solo como servicio: en
// `--console` la rama no se pisa, así que el daemon parecía sano. Medido con el
// paquete instalado el 2026-08-10.
const hostsUI = false

// uiExeName no lo lee nadie con [hostsUI] en falso, y se declara igual.
//
// Vacío y no un nombre inventado: si alguna vez alguien lo usa fuera de esa
// condición, una ruta vacía falla enseguida y con un error que se entiende, en
// vez de buscar para siempre un ejecutable que en esta variante no existe.
const uiExeName = ""
