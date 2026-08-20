// Package nftnat desvía lo que llega por la sala hacia donde el juego escucha.
//
// # Por qué existe
//
// Un servidor atado a una dirección concreta —la del contenedor, la del pod—
// no recibe lo que llega a la dirección de la sala, y el kernel contesta
// "puerto inalcanzable" a todo el cuarto. Medido el 2026-08-19: túnel perfecto,
// puertos abiertos, y ni un paquete llegando al juego. En un compose o en un
// manifiesto esa configuración es la que la gente copia, así que decirlo en un
// README no alcanza.
//
// # Por qué SOLO en contenedor
//
// Porque ahí la intención no es ambigua: ese contenedor declaró un juego,
// comparte el espacio de red de Kanpachi y existe para servir esa sala. En una
// máquina normal, atar un servicio a una dirección es lo que hace alguien para
// que NO lo alcancen desde otro sitio, y desviarlo por su cuenta rompería esa
// decisión. El cableado es quien aplica esa frontera: fuera de contenedor este
// adaptador no se construye. Ver [domain.RedirectSpec].
//
// # Lo que escribe, y dónde
//
// Una tabla PROPIA, `kanpachi-nat`, aparte de la de la compuerta y aparte de la
// cuarentena, con una sola cadena de `prerouting`. Se borra entera al quitar el
// desvío y al salir de la sala, y no sobrevive a un reinicio: es efímera como
// la compuerta, así que no necesita libro. Nada del operador se toca, que es la
// misma regla que gobierna el resto del firewall en Linux.
package nftnat

import (
	"github.com/accentiostudios/kanpachi/core/port"
)

// TableName es la tabla propia del desvío.
//
// Aparte de la de la compuerta a propósito: la compuerta se reescribe entera en
// cada cambio de miembros, y barrer una tabla no puede llevarse por delante la
// traducción que hace que el juego se alcance.
const TableName = "kanpachi-nat"

// ChainName es la única cadena, en el hook donde el destino todavía se puede
// reescribir.
const ChainName = "prerouting"

// Redirect es el adaptador. No guarda estado: lo que hay puesto se lee del
// kernel, que es la única fuente que no puede desincronizarse.
type Redirect struct{}

func New() *Redirect { return &Redirect{} }

var _ port.TrafficRedirect = (*Redirect)(nil)
