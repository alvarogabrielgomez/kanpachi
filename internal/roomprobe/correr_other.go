//go:build !windows

package main

import "errors"

// El ciclo de vida de una sala escribe en el Firewall de Windows y levanta
// adaptadores virtuales con el motor, así que esta herramienta solo mide donde
// Kanpachi corre.
//
// Existe fuera de Windows para que el job de Linux del CI la compile y la vete.
// Eso no es una formalidad: `vista.go`, `log.go` y `diagnostico.go` son código
// portable de verdad, y son la parte de esta herramienta que decide si el log
// sirve para entender por qué dos máquinas no se ven.
var soloWindows = errors.New("roomprobe ejercita el firewall y el motor de Windows, así que solo corre ahí")

func correr(opciones) error { return soloWindows }
