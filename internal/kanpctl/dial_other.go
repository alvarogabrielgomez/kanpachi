//go:build !windows

package main

import (
	"errors"
	"net"
)

func dial(string) (net.Conn, error) {
	return nil, errors.New("el named pipe es de Windows, y este binario no lo es")
}
