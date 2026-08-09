//go:build !windows

package main

// rutaDelLog fuera de Windows no tiene nada que devolver: este bundle no corre
// ahí. Existe para que el job de Linux del CI compile el paquete.
func rutaDelLog() string { return "" }
