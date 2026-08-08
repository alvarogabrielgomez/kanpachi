//go:build !windows

package main

import "github.com/accentiostudios/kanpachi/core/port"

// Fuera de Windows no hay servicio con el que pelearse. Ver el gemelo.
func comprobarServicio(port.Logger, bool) error { return nil }
