//go:build !windows && !linux

package pipe

import (
	"errors"
	"net"
)

// Las direcciones de un sistema donde este canal todavía no existe.
//
// No son alcanzables: [abrirPipe] se niega antes de mirarlas. Se declaran porque
// los identificadores son parte de la API del paquete y `main.go` los nombra sin
// preguntar dónde corre, así que sin esto el binario no compilaría en ningún
// sistema fuera de Windows y Linux, ni siquiera para descubrir que no hay canal.
const (
	Name         = "/run/kanpachi/api.sock"
	PortableName = "/run/kanpachi/portable.sock"
	ConsoleName  = "/run/kanpachi/console.sock"
)

// SecurityDescriptor tiene la forma del de Linux por lo mismo: no lo lee nadie.
const SecurityDescriptor = "0600"

// abrirPipe fuera de Windows y de Linux no existe, y decirlo así es mejor que no
// compilar.
//
// El producto es Windows y Linux, y esto no lo cambia. Existe para que el resto
// del paquete, que es lógica pura sobre net.Listener, compile en cualquier
// sistema. Un daemon que llegara a llamarlo se encuentra un error claro en vez de
// un binario que no se pudo construir.
//
// # Por qué el descriptor se comprueba ACÁ y antes que nada
//
// Porque es la única forma de que esa regla la pruebe un CI que no sea el del
// sistema de turno. La negativa a escuchar sin descriptor vive en el fichero de
// cada sistema, y sin esta comprobación `TestSinDescriptorNoSeEscucha` recibiría
// el error de "esto no es tu sistema" y fallaría, o sea que la invariante se
// quedaría sin guardián.
//
// El orden importa y es este: primero la regla, después el sistema. Al revés, la
// regla vuelve a ser inalcanzable.
func abrirPipe(_ string, descriptor string) (net.Listener, error) {
	if descriptor == "" {
		return nil, ErrSinDescriptor
	}
	return nil, errors.New("pipe: el canal local está escrito para Windows y para Linux, " +
		"y este binario no es de ninguno de los dos")
}

// checkPeer no comprueba nada donde no hay canal que atender.
func checkPeer(net.Conn) error { return nil }
