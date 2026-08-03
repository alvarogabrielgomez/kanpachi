//go:build !windows

package pipe

import (
	"errors"
	"net"
)

// abrirPipe fuera de Windows no existe, y decirlo así es mejor que no compilar.
//
// El cliente es solo Windows y esto no lo cambia. Existe para que el resto del
// paquete, que es lógica pura sobre net.Listener, compile y se pruebe en el job
// de Linux. Un daemon que llegara a llamarlo en Linux se encuentra un error
// claro en vez de un binario que no se pudo construir.
func abrirPipe(string, string) (net.Listener, error) {
	return nil, errors.New("pipe: el named pipe es de Windows, y este binario no lo es")
}
