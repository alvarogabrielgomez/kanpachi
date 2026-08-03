//go:build windows

package pipe

import (
	"net"

	"github.com/Microsoft/go-winio"
)

// abrirPipe crea el named pipe de verdad.
//
// Vive aparte del resto del paquete para que TODO lo demás compile y se pruebe
// en Linux: las plazas, los plazos y el cierre son lógica, y la lógica que solo
// corre en la máquina donde se programa es lógica sin CI.
//
// Lo que go-winio pone y no se ve acá, que es por lo que se usa:
//
//   - FILE_FLAG_FIRST_PIPE_INSTANCE en la primera instancia. Es lo que hace que
//     el nombre sea nuestro: si ya existe, falla en vez de sumarse.
//   - PIPE_REJECT_REMOTE_CLIENTS, siempre. Nadie le habla a este pipe desde
//     otra máquina.
//   - E/S superpuesta, que es lo que permite que un Close corte un Read
//     bloqueado en vez de dejar la goroutine colgada para siempre.
func abrirPipe(nombre, sddl string) (net.Listener, error) {
	if sddl == "" {
		return nil, ErrSinDescriptor
	}
	// MessageMode en false a propósito: el enmarcado es por líneas y lo hace
	// `wire`, así que el modo mensaje de Windows sería un segundo enmarcado por
	// debajo del nuestro. Dos capas que dicen dónde termina un mensaje es una
	// de más, y la que se puede probar en Linux es la nuestra.
	return winio.ListenPipe(nombre, &winio.PipeConfig{
		SecurityDescriptor: sddl,
		MessageMode:        false,
		InputBufferSize:    64 * 1024,
		OutputBufferSize:   64 * 1024,
	})
}
