//go:build !windows

package main

import (
	"errors"
	"net/netip"
)

// Este arnés mide sobre un adaptador virtual de Windows. Existe este fichero
// para que el paquete compile en el job de Linux y no rompa `go build ./...`.

func adaptadorDeLaSala() (netip.Addr, netip.Prefix, error) {
	return netip.Addr{}, netip.Prefix{}, errors.New("netcfgprobe solo mide en Windows")
}

func hayRuta(netip.Prefix) bool   { return false }
func ponerRutaPorDefecto() error  { return errors.New("netcfgprobe solo mide en Windows") }
func quitarRutaPorDefecto() error { return errors.New("netcfgprobe solo mide en Windows") }
