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
		MembersGen:    s.membersGen,
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

// saveMembersLocked baja el libro de credenciales a disco.
//
// # El orden con [Session.saveRoomLocked] importa, y falla del lado seguro
//
// Primero el libro, después la sala con la generación nueva dentro. Si falla el
// segundo, en disco queda un libro MÁS NUEVO que la generación que la sala
// recuerda, y eso se acepta: un libro adelantado no es una reversión. Si falla
// el primero, la sala recuerda una generación que el libro no alcanza y el
// libro se rechaza entero al reabrir, o sea que el host arranca sin él, que es
// exactamente lo que pasaba antes de que existiera.
//
// Solo el host: un invitado no emite ninguna credencial.
//
// Asume el candado tomado.
func (s *Session) saveMembersLocked() {
	if !s.state.IsHost() || !s.state.Conn.InRoom() || s.state.Room.InviteID.IsZero() {
		return
	}
	libro := domain.CredentialBook{Gen: s.membersGen + 1, Room: s.state.Room}
	for _, m := range s.members {
		if m.Cred == nil {
			continue
		}
		libro.Entries = append(libro.Entries, domain.BookEntry{
			VirtualIP: m.IP,
			ID:        m.Cred.ID,
			Name:      m.Cred.Name,
			MemberKey: m.Cred.MemberKey,
			IssuedAt:  m.Cred.IssuedAt,
			ExpiresAt: m.Cred.ExpiresAt,
			Revoked:   m.Cred.Revoked,
		})
	}
	raw, err := libro.Encode()
	if err != nil {
		s.deps.Log.Warn("no se pudo serializar el libro de credenciales", "error", err)
		return
	}
	if err := s.deps.State.SaveMembers(raw); err != nil {
		s.deps.Log.Warn("no se pudo guardar el libro de credenciales", "error", err)
		return
	}
	s.membersGen = libro.Gen
	s.saveRoomLocked()
}

// loadMembersLocked recupera el libro de la sala que se está reabriendo.
//
// Que falte es NORMAL y no es un fallo: nadie entró todavía, o esta máquina
// nunca hospedó. Que no se pueda leer tampoco detiene nada: la sala se reabre
// sin libro, que es como se reabría antes, y el aviso queda en el log.
//
// Asume el candado tomado.
func (s *Session) loadMembersLocked(saved domain.HostedRoom) {
	raw, err := s.deps.State.LoadMembers()
	if err != nil {
		s.membersGen = saved.MembersGen
		return
	}
	libro, err := domain.DecodeCredentialBook(raw, saved.MembersGen, saved.Room, s.deps.Clock.Now())
	if err != nil {
		// Se DICE, y con el motivo. Una reversión detectada es lo único de este
		// camino que describe a alguien manipulando ficheros, y confundirla con
		// «no había libro» dejaría el hallazgo sin registrar.
		s.deps.Log.Warn("el libro de credenciales guardado no se pudo usar y se ignora", "error", err)
		s.membersGen = saved.MembersGen
		return
	}
	if s.members == nil {
		s.members = domain.MemberTable{}
	}
	for _, e := range libro.Entries {
		c := domain.Credential{
			ID:        e.ID,
			Name:      e.Name,
			VirtualIP: e.VirtualIP,
			Subnet:    saved.Subnet,
			MemberKey: e.MemberKey,
			IssuedAt:  e.IssuedAt,
			ExpiresAt: e.ExpiresAt,
			Revoked:   e.Revoked,
		}
		m := s.members.At(e.VirtualIP)
		m.Cred = &c
		m.Name = e.Name
	}
	s.membersGen = libro.Gen
	s.deps.Log.Info("el libro de credenciales volvió del disco",
		"fichas", len(libro.Entries), "generación", libro.Gen)
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
