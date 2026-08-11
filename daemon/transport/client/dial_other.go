//go:build !windows && !linux

package client

import (
	"errors"
	"net"
)

// Dial se niega, que es lo honesto: el canal local está escrito para Windows y
// para Linux, y no hay un tercero al que caerse.
//
// Existe para que `go build ./...` siga compilando fuera de los dos, que es lo
// que mantiene compilable el resto del paquete.
func Dial(string) (net.Conn, error) {
	return nil, errors.New("el canal local está escrito para Windows y para Linux, " +
		"y este binario no es de ninguno de los dos")
}
