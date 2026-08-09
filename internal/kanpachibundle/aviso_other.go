//go:build !windows

package main

import (
	"fmt"
	"os"
)

// avisar fuera de Windows escribe por la salida de errores, que ahí sí existe.
//
// Existe para que el job de Linux del CI compile el paquete. Este bundle no
// corre fuera de Windows: ver [correr].
func avisar(titulo, texto string) {
	fmt.Fprintf(os.Stderr, "%s: %s\n", titulo, texto)
}
