//go:build !windows && !linux

package main

import (
	"context"
	"fmt"
)

// ServiceName existe fuera de Windows solo para que el nombre sea único en el
// programa. Acá no hay Administrador de servicios que lo registre.
const ServiceName = "kanpachi-daemon"

// EnServicio siempre dice que no.
//
// No es un provisional que finja: fuera de Windows no existe el Administrador
// de servicios, así que "a este proceso no lo arrancó" es literalmente cierto.
// El daemon de esta plataforma no llega mucho más lejos —el firewall se planta
// enseguida— y este archivo existe para que el job de Linux siga compilando el
// paquete entero.
func EnServicio() (bool, error) { return false, nil }

// CorrerComoServicio no puede correr acá, y lo dice.
func CorrerComoServicio(func(context.Context, []string) (func() error, func(), error)) error {
	return fmt.Errorf("kanpachid: el modo servicio es del Administrador de servicios de Windows")
}

// ArgShow existe fuera de Windows solo para que el nombre sea único en el
// programa. Acá no hay servicio al que pasarle argumentos.
const ArgShow = "--show"

// tiene dice si el argumento está en la lista.
func tiene(args []string, quéBusco string) bool {
	for _, a := range args {
		if a == quéBusco {
			return true
		}
	}
	return false
}
