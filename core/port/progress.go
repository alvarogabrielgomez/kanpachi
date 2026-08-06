package port

import "github.com/accentiostudios/kanpachi/core/domain"

// ProgressSink recibe los pasos de una operación larga mientras ocurre.
//
// # Para qué existe
//
// Crear una sala tarda decenas de segundos con todo funcionando bien: se lee la
// tabla de rutas, se le pide un código al registro, se levanta el motor, se
// espera a que dos adaptadores tomen dirección, se sondea el MTU, se acota la
// compuerta y se abre el canal. Desde fuera, esa espera y un cuelgue se ven
// igual, y cuando falla lo único que llega a la pantalla es la última línea de
// error, que casi nunca es donde estaba el problema.
//
// # Por qué es un puerto y no el log
//
// Podría leerse el log, y sería lo barato. No sirve por dos motivos: el log
// mezcla lo de esta operación con todo lo demás que el daemon hace a la vez, y
// sus líneas están escritas para diagnosticar y no para leerse en una pantalla.
// Un puerto propio deja que quien emite decida qué merece contarse.
//
// # Lo que NO es
//
// No es un canal de eventos hacia la interfaz. La API local es petición y
// respuesta pura, sin empuje del servidor, y esto no lo cambia: los pasos se
// acumulan de este lado y la pantalla los PIDE. Ver `docs/03`.
type ProgressSink interface {
	// Step anota un paso que ya ocurrió.
	//
	// El texto se escribe para leerse en pantalla, en castellano y sin jerga
	// de implementación. El scope dice quién lo hizo, que es la mitad de la
	// información: "el registro no contestó" y "el adaptador no tomó
	// dirección" mandan a mirar sitios distintos.
	Step(scope domain.ProgressScope, text string)
}

// NoProgress es un sumidero que no anota nada.
//
// Existe para que un adaptador no tenga que preguntar si le dieron uno. Un nil
// comprobado en cada llamada es la clase de comprobación que un día falta en el
// camino nuevo.
type NoProgress struct{}

func (NoProgress) Step(domain.ProgressScope, string) {}
