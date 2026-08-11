package usecase

import (
	"context"
	"fmt"
	"strings"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// LeaveRoomOnShutdown devuelve la máquina a como estaba y DICE si lo consiguió.
//
// Lo llama `service.Shutdown` al apagar el daemon, y es lo único de todo el
// apagado que de verdad importa que corra: es lo que cierra los puertos,
// restaura las reglas ajenas que se hubieran suspendido y revierte los ajustes
// del adaptador.
//
// # Apagar ya no es cerrar la sala, y eso cambió dos cosas
//
// Antes, apagar limpio SALÍA de la sala y borraba su fichero, y la ausencia del
// fichero era la señal de "murió sucio". Ahora el fichero se queda, porque
// apagar el proceso no es que la sala se acabe: un `apt upgrade`, un reinicio o
// un `systemctl restart` paran el daemon y la sala sigue siendo de quien la
// abrió. Al arrancar se reabre con el MISMO código, así que los enlaces que ya
// repartió siguen valiendo. Cerrarla de verdad es [Session.LeaveRoom], que sí
// borra el fichero.
//
// Lo segundo, y es lo que se rompería sin decirlo en voz alta: **acá NO se avisa
// a los miembros de que la sala se cierra**. [Session.LeaveRoom] manda ese aviso
// y hace bien, porque ahí la sala se acabó. Mandarlo desde el apagado sería
// mentira y encima cara: cada invitado saldría de la sala y dejaría de
// reconectar, que es justo lo contrario de lo que esto ahora hace posible. Lo
// que ven es al host ausente, y para eso ya existe el contador de veinte minutos.
//
// # Por qué devuelve error si LeaveRoom no lo hace
//
// Porque acá no hay nadie mirando la pantalla. [Session.LeaveRoom] anota lo que
// sale mal como una alerta del estado, y eso funciona mientras alguien vaya a
// leer ese estado. Al apagar, el proceso se muere justo después: una alerta
// añadida a un estado que nadie va a leer no es un informe. El error sube hasta
// el log del servicio, que es lo único que va a quedar.
//
// Es idempotente. Lo puede llamar un apagado que llegue con la sala ya cerrada,
// y eso no es un fallo.
func (s *Session) LeaveRoomOnShutdown(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Se vuelve a guardar ANTES de bajar nada, y no es redundante con lo que ya
	// escribieron crear, renovar, renombrar y elegir juego: lo que actualiza es
	// `SavedAt`, que es lo que después dice hace cuánto quedó abierta esa sala.
	// Con la sala ya cerrada no hace nada, porque exige ser host y estar dentro.
	s.saveRoomLocked()

	// `ExitNone` y no un motivo inventado: no hay nada que explicar, porque la
	// sala no terminó. Los motivos existen para que la pantalla de inicio diga
	// por qué se volvió a ella, y acá lo siguiente que pasa es que el proceso se
	// muere.
	s.leaveLocked(ctx, "el daemon se apagó", domain.ExitNone, conservarParaReabrir)

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
