// Package opener es lo ÚNICO que traduce el canario a la lengua de core.
//
// # Por qué es un paquete aparte y no un método más
//
// Porque `daemon/adapter/canary` tiene que conservar su grafo de imports libre
// de `core`, y devolver un [port.Canary] arrastraría `core/port` y con él
// `core/domain` dentro de aquel paquete.
//
// Eso importa por lo que aquel paquete es: un oyente que abre un proceso que
// corre como SYSTEM. Cuanto menos código tenga alcance ahí, menos hay que
// revisar cada vez que se lo toca. Hoy son `net`, `sync` y `time`, y ese
// inventario cabe en una línea.
//
// Acá viven veinte líneas de traducción y nada más. No hay decisión de política
// en este archivo, y no puede haberla: lo que decide qué puertos esquivar es
// `domain.RuleSet.Allows`, y lo que decide qué significa un toque es
// `domain.CanaryCheck`.
package opener

import (
	"net/netip"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/core/port"
	"github.com/accentiostudios/kanpachi/daemon/adapter/canary"
)

// Opener abre canarios. Sin estado propio: cada oyente vive lo que dura su
// ronda y se lo lleva quien lo pidió.
type Opener struct {
	log canary.Logger
}

// New arma el abridor. El logger es el del daemon, y `canary.Logger` lo declara
// aquel paquete para no depender de core, así que un `port.Logger` lo satisface
// sin conversión.
func New(log canary.Logger) Opener { return Opener{log: log} }

// Listen abre el canario y lo devuelve en la forma que core entiende.
//
// El número se copia de un tipo al otro en vez de compartir el tipo, y esa
// copia es el precio de que el paquete de abajo no conozca el dominio. Los dos
// son arreglos del mismo largo, así que el compilador comprueba el tamaño.
func (o Opener) Listen(at netip.Addr, nonce domain.CanaryNonce, ttl time.Duration,
	avoid func(uint16) bool) (port.Canary, error) {

	c, err := canary.Listen(at, canary.Nonce(nonce), ttl, avoid, o.log)
	if err != nil {
		// Se devuelve el nil de la INTERFAZ y no el puntero nulo del adaptador.
		// Un `*canary.Canary` nulo dentro de una interfaz no es igual a nil, y
		// quien llame comprobaría el error y despues llamaría a Close sobre algo
		// que no existe.
		return nil, err
	}
	return c, nil
}
