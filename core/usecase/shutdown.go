package usecase

import (
	"context"
	"fmt"
	"strings"
)

// LeaveRoomOnShutdown devuelve la máquina a como estaba y DICE si lo consiguió.
//
// Lo llama `service.Shutdown` al apagar el daemon, y es lo único de todo el
// apagado que de verdad importa que corra: es lo que cierra los puertos,
// restaura las reglas ajenas que se hubieran suspendido y revierte los ajustes
// del adaptador.
//
// # Por qué apagar limpio es SALIR, y no dejar la sala guardada
//
// Porque la ausencia de `room.json` es lo que dice que la salida fue limpia, y
// no hay bandera dentro del archivo que lo diga: una bandera es un campo que
// alguien puede escribir a mano, y este hecho no se puede falsificar desde
// dentro. Conservarlo al apagar bien haría que TODO apagado se leyera como una
// muerte sucia, y el aviso de "quedó una sala abierta" dejaría de significar
// nada por salir siempre.
//
// Y la sala no sobreviviría igual: el motor muere con el daemon, los miembros se
// caen, y la red que el código nombra deja de existir. Retomar es para el caso
// sucio, que es justo el que este método existe para evitar.
//
// # Por qué devuelve error si LeaveRoom no lo hace
//
// Porque acá no hay nadie mirando la pantalla. [Session.LeaveRoom] anota lo que
// sale mal como una alerta del estado, y eso funciona mientras alguien vaya a
// leer ese estado. Al apagar, el proceso se muere justo después: una alerta
// añadida a un estado que nadie va a leer no es un informe. El error sube hasta
// el log del servicio, que es lo único que va a quedar.
//
// Es idempotente, igual que salir de la sala. Lo puede llamar un apagado que
// llegue con la sala ya cerrada, y eso no es un fallo.
func (s *Session) LeaveRoomOnShutdown(ctx context.Context) error {
	// Salir toma el candado por su cuenta, así que va fuera de la sección de
	// abajo. Ya cierra puertos, restaura reglas ajenas y revierte los ajustes.
	s.LeaveRoom(ctx)

	s.mu.Lock()
	defer s.mu.Unlock()

	// Y se vuelve a medir, porque salir informa sus fallos por el log y por
	// alertas, y ninguno de los dos llega hasta el que apaga. Esta es la única
	// medición del apagado, y sin ella un cierre que dejó puertos abiertos se
	// vería exactamente igual que uno limpio.
	blind, extra := s.checkClosedLocked(ctx)
	if blind != nil {
		return fmt.Errorf("no se pudo comprobar que el firewall quedó cerrado al apagar: %w", blind)
	}
	if len(extra) > 0 {
		return fmt.Errorf("al apagar quedaron reglas puestas que Kanpachi no pudo quitar: %s",
			strings.Join(extra, ", "))
	}
	return nil
}
