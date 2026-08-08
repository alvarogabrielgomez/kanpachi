//go:build !windows

package main

import (
	"fmt"
	"time"
)

// Fuera de Windows no hay nada que elevar ni ninguna consola que poseer. Estos
// gemelos existen para que el job de Linux del CI compile y vete el paquete: lo
// que de verdad se comprueba ahí es `vista.go`, que es formateo puro y es la
// mitad de esta herramienta que se puede leer sin una máquina delante.

func elevar() (bool, error) { return false, nil }

type consola struct{}

func abrirConsola() *consola { return &consola{} }

func (c *consola) crudo()                                  {}
func (c *consola) cocido()                                 {}
func (c *consola) esperarTecla(time.Duration) (byte, bool) { return 0, false }
func (c *consola) alOrigen()                               { fmt.Print("\033[H") }
func (c *consola) borrarElResto()                          { fmt.Print("\033[0J") }
func (c *consola) limpiar()                                { fmt.Print("\033[H\033[2J") }
