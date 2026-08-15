//go:build windows

package client

import (
	"net"

	"github.com/Microsoft/go-winio"
	"github.com/accentiostudios/kanpachi/core/timing"
)

// Dial abre el named pipe.
//
// Aparte del resto para que `go vet ./...` del job de Linux no se tope con
// winio, igual que en el paquete del pipe.
func Dial(nombre string) (net.Conn, error) {
	plazo := timing.PipeDialTimeout
	return winio.DialPipe(nombre, &plazo)
}
