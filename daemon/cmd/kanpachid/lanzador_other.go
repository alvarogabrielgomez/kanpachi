//go:build !windows

package main

import (
	"fmt"
	"net"
	"os"
	"time"
)

// Fuera de Windows no hay named pipe ni Administrador de servicios, así que el
// modo lanzador no existe. Estos gemelos están para que el job de Linux compile
// el paquete entero, y fallan diciendo por qué en vez de fingir que arrancaron
// algo.

func marcarPipe(time.Duration) (net.Conn, error) {
	return nil, fmt.Errorf("kanpachid: el canal de la API local es un named pipe de Windows")
}

func ArrancarServicio([]string) (bool, error) {
	return false, fmt.Errorf("kanpachid: el modo servicio es del Administrador de servicios de Windows")
}

func ArrancarSuelto([]string) error {
	return fmt.Errorf("kanpachid: relanzarse elevado es de Windows, y de su Control de cuentas de usuario")
}

// avisar imprime. Acá siempre hay a dónde, así que no hace falta ninguna
// ventana.
func avisar(msg string) { fmt.Fprintln(os.Stderr, msg) }
