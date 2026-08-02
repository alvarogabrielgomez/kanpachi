package usecase

import (
	"github.com/accentiostudios/kanpachi/core/domain"
)

// saveRoomLocked guarda la sala del HOST para que sobreviva a un apagón.
//
// Se llama después de todo lo que cambia algo que haría falta reponer: crear,
// renovar el código, renombrar y elegir juego. Ninguna de esas operaciones falla
// porque el disco falle, y el motivo es el mismo en las cuatro: la sala YA está
// abierta y funcionando, y lo único que se pierde al no poder guardar es poder
// reabrirla tras un corte de luz.
//
// La ausencia del archivo es lo que dice que la última salida fue limpia, así
// que guardar y borrar son las dos mitades de la misma señal.
//
// Asume el candado tomado.
func (s *Session) saveRoomLocked() {
	if !s.state.IsHost() || !s.state.Conn.InRoom() {
		return
	}
	raw, err := domain.PersistedRoom{
		Room:          s.state.Room,
		Name:          s.state.Name,
		Host:          s.nick,
		NetworkID:     s.hostSpec.NetworkID,
		NetworkSecret: s.hostSpec.NetworkSecret,
		Subnet:        s.state.Subnet,
		CardKey:       s.cardKey,
		GameID:        s.state.Game.ID,
		SavedAt:       s.deps.Clock.Now(),
	}.Encode()
	if err != nil {
		s.deps.Log.Warn("no se pudo serializar la sala para guardarla", "error", err)
		return
	}
	if err := s.deps.State.SaveRoom(raw); err != nil {
		s.deps.Log.Warn("no se pudo guardar la sala en disco", "error", err)
	}
}

// saveLastRoomLocked guarda la última sala de un INVITADO, para poder volver.
//
// Lleva el código y nada que sirva para entrar sin pasar por el host: volver
// hace el mismo canje que la primera vez, el host reemite y ve llegar a quien
// llega. Eso es lo que mantiene con sentido a la revocación.
//
// No se borra al salir. Es la ÚLTIMA sala, no la actual, y que te hayan
// expulsado tampoco la borra, porque expulsar no es banear.
//
// Asume el candado tomado.
func (s *Session) saveLastRoomLocked() {
	if s.state.IsHost() || !s.state.Conn.InRoom() || s.state.Room.InviteID.IsZero() {
		return
	}
	raw, err := domain.LastRoom{
		Room:    s.state.Room,
		Name:    s.state.Name,
		Nick:    s.nick,
		SavedAt: s.deps.Clock.Now(),
	}.Encode()
	if err != nil {
		s.deps.Log.Warn("no se pudo serializar la última sala", "error", err)
		return
	}
	if err := s.deps.State.SaveLast(raw); err != nil {
		s.deps.Log.Warn("no se pudo guardar la última sala en disco", "error", err)
	}
}

// LastRoom devuelve la última sala a la que se entró como invitado.
//
// Es lo que alimenta "volver a la última sala". Entrar sigue siendo el
// [Session.JoinRoom] de siempre con el código guardado: no hay un camino de
// ingreso aparte, y por eso esta función devuelve datos y no hace nada.
//
// El código guardado se mantiene vigente aunque el host lo renueve, porque el
// host se lo reparte a los presentes al renovarlo. Ver [Session.OnCodeRotated].
func (s *Session) LastRoom() (domain.LastRoom, bool) {
	raw, err := s.deps.State.LoadLast()
	if err != nil {
		return domain.LastRoom{}, false
	}
	last, err := domain.DecodeLastRoom(raw)
	if err != nil {
		s.deps.Log.Warn("la última sala guardada no se pudo interpretar", "error", err)
		return domain.LastRoom{}, false
	}
	return last, true
}
