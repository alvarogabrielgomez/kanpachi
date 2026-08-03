//go:build windows

package main

import (
	"net"
	"time"

	"github.com/Microsoft/go-winio"
)

// dial abre el named pipe.
//
// Aparte del resto para que `go vet ./...` del job de Linux no se tope con
// winio, igual que en el paquete del pipe.
func dial(nombre string) (net.Conn, error) {
	plazo := 5 * time.Second
	return winio.DialPipe(nombre, &plazo)
}
