//go:build !windows && !linux

package main

import "fmt"

// Fuera de Windows no hay Administrador de servicios que consultar, así que
// esto existe para que el job de Linux compile el paquete entero.

func (h *procesoHost) Autostart() (bool, error) {
	return false, fmt.Errorf("el arranque con Windows lo gobierna el Administrador de servicios de Windows")
}

func (h *procesoHost) SetAutostart(bool) error {
	return fmt.Errorf("el arranque con Windows lo gobierna el Administrador de servicios de Windows")
}
