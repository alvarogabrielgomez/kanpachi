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
//
// # Por qué el descriptor se comprueba ACÁ y antes que nada
//
// Porque es la única forma de que esa regla la pruebe el job de Linux, que es
// donde corre CI. La negativa a escuchar sin descriptor vive en el fichero de
// Windows, y ahí ningún test la alcanza; sin esta comprobación,
// `TestSinDescriptorNoSeEscucha` recibía el error de "esto es de Windows" y
// fallaba, o sea que la invariante se quedaba sin guardián en el único sitio que
// puede tenerlo.
//
// El orden importa y es este: primero la regla, después el sistema. Al revés,
// la regla vuelve a ser inalcanzable.
func abrirPipe(_ string, descriptor string) (net.Listener, error) {
	if descriptor == "" {
		return nil, ErrSinDescriptor
	}
	return nil, errors.New("pipe: el named pipe es de Windows, y este binario no lo es")
}
