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
// The file says "there is a room to reopen", and its absence says nothing at
// all: a clean shutdown keeps it now. What clears it is closing the room. See
// `destino` in [Session.leaveLocked].
//
// Asume el candado tomado.
func (s *Session) saveRoomLocked() {
	if !s.state.IsHost() || !s.state.Conn.InRoom() {
		return
	}
	raw, err := domain.HostedRoom{
		Room:          s.state.Room,
		Name:          s.state.Name,
		Host:          s.nick,
		NetworkID:     s.hostSpec.NetworkID,
		NetworkSecret: s.hostSpec.NetworkSecret,
		Subnet:        s.state.Subnet,
		CardKey:       s.cardKey,
		Card:          s.sealedCard,
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
// # autoReturn, and why it is a parameter rather than a constant
//
// Because the file never goes away, its presence says nothing about intent,
// unlike the host's room file where being there IS the signal. Entering passes
// true; leaving rewrites it with what that exit means. See
// [domain.LastRoom.AutoReturn].
//
// Asume el candado tomado.
func (s *Session) saveLastRoomLocked(autoReturn bool) {
	if s.state.IsHost() || !s.state.Conn.InRoom() || s.state.Room.InviteID.IsZero() {
		return
	}
	last := domain.LastRoom{
		Room:       s.state.Room,
		Name:       s.state.Name,
		Nick:       s.nick,
		SavedAt:    s.deps.Clock.Now(),
		AutoReturn: autoReturn,
	}
	// La semilla de miembro sigue la MISMA regla que la vuelta automática:
	// salir a propósito — irse o ser expulsado — la descarta, y todo lo demás
	// la conserva para que la vuelta recupere credencial y dirección. Dejarla
	// tras una salida deliberada mantendría en disco una identidad enlazable
	// que su dueño decidió cerrar.
	if autoReturn {
		last.MemberSeed = s.memberKey.Seed()
	}
	raw, err := last.Encode()
	if err != nil {
		s.deps.Log.Warn("no se pudo serializar la última sala", "error", err)
		return
	}
	// The cache goes with the file, and BEFORE the write is judged: whether this
	// machine is going back is answered from memory on every snapshot, and a disk
	// that failed is not a reason to answer it wrong. See [Session.last].
	s.last, s.hasLast = last, true
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
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.last, s.hasLast
}
