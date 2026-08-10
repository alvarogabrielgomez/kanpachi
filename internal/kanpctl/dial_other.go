//go:build !windows && !linux

package main

import (
	"errors"
	"net"
)

func dial(string) (net.Conn, error) {
	return nil, errors.New("el canal local está escrito para Windows y para Linux, " +
		"y este binario no es de ninguno de los dos")
}
