package usecase

import (
	"context"
	"net/netip"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// LeaveRoom sale de la sala y devuelve la máquina a como estaba.
//
// No devuelve error si no hay sala. Salir es idempotente a propósito: lo llama
// el usuario, lo llama el contador de veinte minutos y lo llama el apagado del
// servicio, y que el segundo en llegar falle no aporta nada a nadie.
//
// Al salir se cierran los puertos, se restauran las reglas ajenas que se
// hubieran suspendido y se revierten los ajustes que pidió el perfil. Elegir
// el juego abre los puertos, salir de la sala los cierra, y no hay tercera
// vía: el daemon no observa procesos y no sabe si dejaste de jugar.
func (s *Session) LeaveRoom(ctx context.Context) domain.RoomState {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Si el que sale es el host, la sala se termina para todos. Avisar cuesta
	// un mensaje y le ahorra a cada invitado los veinte minutos del contador
	// mirando una sala que ya no existe.
	//
	// Va antes del teardown por lo mismo que el aviso de expulsión: después no
	// hay canal por donde mandarlo.
	if s.state.IsHost() && s.state.Conn.InRoom() {
		if err := s.deps.Control.Notify(ctx, netip.Addr{}, domain.RoomNotice{
			Kind:   domain.NoticeRoomClosed,
			Reason: "el host cerró la sala",
		}); err != nil {
			s.deps.Log.Warn("no se pudo avisar del cierre de la sala", "error", err)
		}
	}
	s.leaveLocked(ctx, "el usuario salió de la sala", domain.ExitUser)
	// Se comprueba DESPUÉS de salir, no solo al arrancar. El barrido periódico
	// no cubre este momento: exige sala para juzgar, corre por temporizador, y
	// si el daemon se apaga justo después de salir, que es lo que la gente hace
	// al terminar de jugar, nadie vuelve a medir nunca.
	s.verifyClosedLocked(ctx)
	return s.snapshot()
}

// OnRoomNotice aplica un aviso del host. Lo llama el supervisor cuando llega
// algo por el canal de control.
//
// Los dos avisos terminan en lo mismo, salir de la sala, y se distinguen solo
// en lo que la pantalla de inicio va a decir después. Eso vale: sin el motivo,
// que te expulsen y que el host cierre se ven exactamente igual.
//
// Un host no toma avisos. Nadie puede cerrarle la sala ni echarlo de ella.
func (s *Session) OnRoomNotice(ctx context.Context, n domain.RoomNotice) domain.RoomState {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.state.Conn.InRoom() || s.state.IsHost() {
		return s.snapshot()
	}
	// Un aviso que llega es prueba de vida del host, aunque lo que diga sea que
	// la sala se acabó. Se anota antes de actuar por si el aviso resulta ser de
	// un tipo que no se maneja: hasta un mensaje que se descarta demuestra que
	// del otro lado hay alguien.
	s.state.NoteHostAlive(s.deps.Clock.Now())

	switch n.Kind {
	case domain.NoticeKicked:
		// Salir por las buenas al recibirlo. No es obediencia: el host ya
		// revocó la credencial o está por hacerlo, así que quedarse solo
		// consigue que la salida sea sucia. Lo que se gana es revertir los
		// ajustes del adaptador y cerrar el motor limpio en vez de que se caiga
		// solo, y que la pantalla diga qué pasó.
		return s.leaveLocked(ctx, "el host expulsó a esta máquina", domain.ExitKicked)
	case domain.NoticeRoomClosed:
		return s.leaveLocked(ctx, "el host cerró la sala", domain.ExitRoomClosed)
	default:
		s.deps.Log.Info("aviso desconocido del host, se ignora", "tipo", int(n.Kind))
		return s.snapshot()
	}
}

// leaveLocked es el cuerpo compartido con la salida automática. Asume el
// candado tomado.
func (s *Session) leaveLocked(ctx context.Context, reason string, exit domain.ExitReason) domain.RoomState {
	if !s.state.Conn.InRoom() {
		return s.snapshot()
	}
	code := s.state.Room.InviteID.String()

	// Salir es el disparador de la pregunta "¿funcionó bien el multijugador?",
	// y es una acción del usuario. Kanpachi no sabe si de verdad jugaron, y por
	// eso pregunta en vez de suponer: no observa procesos y no detecta que
	// abriste un juego. Lo que sí puede afirmar son las dos condiciones que
	// habilitan la pregunta, que ese juego estuvo activo y que hubo alguien
	// más, y las anota acá porque en un instante se van a borrar con la sala.
	if !s.state.Game.IsZero() && len(s.state.Peers) >= 2 {
		if s.verificables == nil {
			s.verificables = make(map[string]string)
		}
		s.verificables[s.state.Game.ID] = s.deps.Clock.Now().UTC().Format("2006-01-02")
	}

	// El archivo de la sala se borra ACÁ, y su ausencia es lo que dice que esta
	// salida fue limpia. No hay bandera de "cerrado bien" dentro del archivo:
	// una bandera es un campo más que alguien puede escribir a mano, y este
	// hecho no se puede falsificar desde dentro.
	//
	// La última sala del invitado NO se borra: existe justamente para volver, y
	// que te hayan expulsado no cambia eso, porque expulsar no es banear.
	if err := s.deps.State.ClearRoom(); err != nil {
		s.deps.Log.Warn("no se pudo borrar la sala guardada al salir", "error", err)
	}

	s.teardown(ctx)
	s.hostSpec = domain.HostSpec{}
	s.cardKey = [domain.CardKeyLen]byte{}
	s.sealedCard = nil
	s.nick = domain.Nickname{}
	s.kicked = nil
	// Las credenciales mueren con la sala que las emitió, y esto NO es higiene.
	//
	// Sus direcciones son válidas, así que sobrevivir a la sala significa que la
	// siguiente arrancaría abriéndole el canal de control a las IP de la
	// anterior: [Session.authorizedControlIPsLocked] las agrega sin poder saber
	// que son de otra sala, y [domain.ControlRules] solo descarta las
	// inválidas. Se vacía en vez de anularse porque emitir escribe en el mapa
	// sin comprobar, y un mapa nil ahí es un pánico.
	clear(s.issued)
	// Y la firma del último conjunto de reglas, para que la primera aplicación
	// de la sala siguiente se anote aunque por casualidad pida lo mismo.
	s.appliedRules = ""
	s.announcedGame = ""
	s.lastAnnounce = time.Time{}
	s.lastPublish = time.Time{}
	s.cardPublishFailing = false
	s.tamperRepairs = 0

	// Transition a Idle limpia la sala entera. Es legal desde cualquier
	// estado, incluso a mitad de un intento de conexión que no responde,
	// porque salir es una acción del usuario que tiene que funcionar siempre.
	if err := s.state.TransitionWithExit(domain.StateIdle, reason, exit); err != nil {
		// Inalcanzable con la tabla de transiciones actual. Se registra en vez
		// de ignorarse porque, si alguien la edita mal, el síntoma sería una
		// sesión que no se puede abandonar y este log es lo que lo diría.
		s.deps.Log.Error("no se pudo volver a Idle", "error", err)
	}
	s.deps.Log.Info("fuera de la sala", "código", code, "motivo", reason)
	return s.snapshot()
}

// SetHostPresent lo llama el supervisor cuando el canal de control se cae o
// vuelve.
//
// Que el host esté o no NO es un estado de conexión y por eso no toca la
// máquina de estados: el túnel sigue perfecto, lo que falta es la persona que
// corre el juego. Mezclarlos haría que la UI dijera "reconectando" cuando la
// red está impecable.
func (s *Session) SetHostPresent(present bool) domain.RoomState {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.state.Conn.InRoom() || s.state.IsHost() {
		return s.snapshot()
	}
	was := s.state.HostPresent
	s.state.SetHostPresent(present, s.deps.Clock.Now())
	if was != present {
		s.deps.Log.Info("cambió la presencia del host", "presente", present)
	}
	// El flanco de subida es una prueba de vida más, y las tres capas conviven
	// sin estorbarse: el socket la da al instante, el anuncio la da cada dos
	// minutos y la tabla de peers la quita cuando el host desaparece de la red.
	return s.snapshot()
}
