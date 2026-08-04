//go:build !windows

package main

// Los subcomandos que tocan el firewall solo existen en Windows.
//
// Fallan en vez de no estar, y eso es el punto: `listen` y `probe` SÍ corren
// acá, porque la otra máquina de una medición puede ser cualquier cosa. Que el
// binario compile en Linux es lo que permite tenerlo del otro lado sin montar
// nada.

import "fmt"

var soloWindows = fmt.Errorf("este subcomando toca el firewall de Windows, así que solo " +
	"corre ahí. `listen` y `probe` sí funcionan acá, que es para lo que hace falta " +
	"la otra máquina")

func adapters() error        { return soloWindows }
func audit([]string) error   { return soloWindows }
func enabled([]string) error { return soloWindows }
func state([]string) error   { return soloWindows }
func apply([]string) error   { return soloWindows }
func purge([]string) error   { return soloWindows }
