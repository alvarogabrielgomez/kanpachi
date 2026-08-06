//go:build !windows

package uihost

import "fmt"

// Fuera de Windows no hay sesión que buscar ni token que bajar, así que esto
// existe para que el job de Linux compile el paquete entero. El fallo se
// devuelve en `openJob`, o sea en el constructor, para que nadie llegue a creer
// que tiene un host y luego descubra que no lanza nada.

func (h *Host) openJob() error {
	return fmt.Errorf("uihost: lanzar la interfaz en la sesión del usuario es de Windows")
}

func (h *Host) launch(bool, bool) error {
	return fmt.Errorf("uihost: lanzar la interfaz en la sesión del usuario es de Windows")
}

func (h *Host) watch()        {}
func (h *Host) signalWake()   {}
func (h *Host) closeHandles() {}

func avisarEnSesión(string, string) {}
