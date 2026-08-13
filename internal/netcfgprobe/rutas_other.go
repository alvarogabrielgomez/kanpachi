//go:build !windows

package main

import (
	"errors"
	"net/netip"
)

// Este arnés mide sobre un adaptador virtual de Windows. Existe este fichero
// para que el paquete compile en el job de Linux y no rompa `go build ./...`.
//
// **Los nombres tienen que seguir a los de `rutas_windows.go`.** Un renombre que
// se salte este fichero no lo nota nadie compilando en Windows, y rompe el job
// de Linux entero: es lo que pasó, y estuvo roto hasta que un `GOOS=linux go
// build ./...` a mano lo encontró.

func roomAdapter() (netip.Addr, netip.Prefix, error) {
	return netip.Addr{}, netip.Prefix{}, errSoloWindows
}

func routeExists(netip.Prefix) bool { return false }
func setDefaultRoute() error        { return errSoloWindows }
func removeDefaultRoute() error     { return errSoloWindows }

var errSoloWindows = errors.New("netcfgprobe solo mide en Windows")
