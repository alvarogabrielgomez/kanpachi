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
//
// # It also covers being in NO room, and that is not a special case
//
// A machine on its way back into a room is not in one, and "salir de la sala" is
// exactly what somebody means when they want that to stop. So this is the formal
// request either way: it switches `AutoReturn` off and cuts any attempt in
// flight. **It does not forget the room** — that is a different thing and only
// the room ceasing to exist does it, which is what keeps going back by hand on
// offer. See [Session.forgetLastRoomLocked].
func (s *Session) LeaveRoom(ctx context.Context) domain.RoomState {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.state.Conn.InRoom() && s.returningLocked().Returning() {
		s.stopReturningLocked()
		return s.snapshot()
	}
	s.leaveRoomLocked(ctx)
	return s.snapshot()
}

// leaveRoomLocked es el cuerpo de [Session.LeaveRoom].
//
// Va aparte porque lo comparte con el gate: entrar a otra sala teniendo una
// abierta sale de la primera, y tiene que salir POR EL MISMO CAMINO, con el
// mismo aviso a los miembros y la misma comprobación de que no quedó nada
// abierto. Dos formas de salir de una sala es cómo se consigue que una de las
// dos olvide cerrar algo. Ver [Session.clearTheWayLocked].
//
// Asume el candado tomado.
func (s *Session) leaveRoomLocked(ctx context.Context) {
	// El diario, **solo si de verdad hay sala de la que salir**.
	//
	// La guarda no es celo: salir es idempotente y lo llaman tres sitios, así
	// que sin ella un `LeaveRoom` sobre nada abriría un diario vacío y pisaría
	// el de la operación anterior. Eso destruye justo lo que la pantalla enseña
	// en "ver detalles" del fallo que acaba de ocurrir, y el caso es de verdad:
	// una sala que no abre deja al usuario apretando el botón de salir.
	//
	// Cierra siempre y sin error, porque `LeaveRoom` no devuelve ninguno: salir
	// termina fuera de la sala pase lo que pase. Lo que falle por dentro lo
	// anota el paso que lo intentaba, que es donde se puede leer cuál fue.
	if s.state.Conn.InRoom() {
		op := "salir de la sala"
		if s.state.IsHost() {
			op = "cerrar la sala"
		}
		s.deps.Progress.Begin(op)
		defer s.deps.Progress.End(nil)
		s.deps.Progress.Step(domain.ScopeDaemon, "recibida la orden de "+op)
	}

	// Si el que sale es el host, la sala se termina para todos. Avisar cuesta
	// un mensaje y le ahorra a cada invitado los veinte minutos del contador
	// mirando una sala que ya no existe.
	//
	// Va antes del teardown por lo mismo que el aviso de expulsión: después no
	// hay canal por donde mandarlo.
	if s.state.IsHost() && s.state.Conn.InRoom() {
		s.deps.Progress.Step(domain.ScopeDaemon, "avisando a los miembros de que la sala se cierra")
		if err := s.deps.Control.Notify(ctx, netip.Addr{}, domain.RoomNotice{
			Kind:   domain.NoticeRoomClosed,
			Reason: "el host cerró la sala",
		}); err != nil {
			s.deps.Log.Warn("no se pudo avisar del cierre de la sala", "error", err)
			s.deps.Progress.Step(domain.ScopeDaemon, "no se pudo avisar a los miembros, se sigue igual")
		}
	}
	s.leaveLocked(ctx, "el usuario salió de la sala", domain.ExitUser, cerrarDeVerdad)
	// Se comprueba DESPUÉS de salir, no solo al arrancar. El barrido periódico
	// no cubre este momento: exige sala para juzgar, corre por temporizador, y
	// si el daemon se apaga justo después de salir, que es lo que la gente hace
	// al terminar de jugar, nadie vuelve a medir nunca.
	s.deps.Progress.Step(domain.ScopeFirewall, "comprobando que no quedó ningún puerto abierto")
	s.verifyClosedLocked(ctx)
	s.deps.Progress.Step(domain.ScopeDaemon, "fuera de la sala")
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
		return s.leaveLocked(ctx, "el host expulsó a esta máquina", domain.ExitKicked, cerrarDeVerdad)
	case domain.NoticeStale:
		// El host dice que no tiene credencial de esta máquina. Es el ÚNICO que
		// lo puede saber, así que esto sustituye a esperar a notarlo por cuenta
		// propia, que en la medición del 2026-08-11 tardó tres minutos porque el
		// canal de control seguía en pie mientras la credencial ya no existía.
		//
		// No se reingresa acá dentro: esto corre en el despachador del
		// supervisor y volver a entrar tarda hasta un minuto. Lo que se hace es
		// dejarlo PEDIDO, y el latido siguiente lo lanza fuera del despachador,
		// que es donde tiene que correr. Ver [Session.Rejoin].
		//
		// Y se pide con un motivo propio, no apagando la presencia del host.
		// Apagarla fue el primer intento y no funcionó, medido: el host SÍ está
		// presente, tanto que el aviso llegó por su canal, así que la siguiente
		// prueba de vida la volvía a encender y el reingreso no llegaba a
		// dispararse nunca. Ver [Session.credencialMuerta].
		s.credencialMuerta = true
		// Y la copia local se suelta con ella: reengancharse con una credencial
		// que su emisor declaró muerta solo gastaría el intento.
		s.myCredential = domain.Credential{}
		s.lastRejoin = time.Time{}
		s.rejoinWait = 0
		s.deps.Log.Info("el host avisa que no tiene la credencial de esta máquina, se le vuelve a pedir")
		return s.snapshot()
	case domain.NoticeRoomClosed:
		return s.leaveLocked(ctx, "el host cerró la sala", domain.ExitRoomClosed, cerrarDeVerdad)
	default:
		s.deps.Log.Info("aviso desconocido del host, se ignora", "tipo", int(n.Kind))
		return s.snapshot()
	}
}

// destino says what happens to the saved room when the session comes down.
//
// # Why leaving and shutting down stopped being the same thing
//
// They are two different events and they were one code path. Leaving is a
// person pressing "close the room": the room is over and its file goes. Shutting
// down is the process stopping while the room is still theirs -- an upgrade, a
// reboot, a `systemctl restart` -- and there the file has to stay, because it
// carries the invite ID and the network identity, which is all that reopening
// the SAME room with the SAME code needs.
//
// Both still tear everything down: ports closed, foreign rules restored,
// adapter tweaks reverted, engine stopped. What differs is only the file.
type destino int

const (
	// cerrarDeVerdad borra la sala guardada. La sala se acabó.
	cerrarDeVerdad destino = iota
	// conservarParaReabrir la deja en disco. El proceso para y la sala sigue
	// siendo de quien la abrió.
	conservarParaReabrir
)

// leaveLocked es el cuerpo compartido con la salida automática. Asume el
// candado tomado.
func (s *Session) leaveLocked(
	ctx context.Context, reason string, exit domain.ExitReason, dest destino,
) domain.RoomState {
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

	// The room's file goes away HERE, and only when the room is really over.
	//
	// What its presence means changed with `destino`. It used to say "the last
	// exit was dirty", inferred from absence, and that reading is gone: shutting
	// down cleanly now keeps it too. It says "there is a room to reopen", and the
	// only thing that clears it is somebody closing the room.
	//
	// The guest's last room is NOT cleared either way: it exists precisely to go
	// back, and being kicked does not change that, because kicking is not
	// banning.
	if dest == cerrarDeVerdad {
		if err := s.deps.State.ClearRoom(); err != nil {
			s.deps.Log.Warn("no se pudo borrar la sala guardada al salir", "error", err)
		}
		s.closeRoomInRegistryLocked(ctx)
	}

	// A guest's last room is rewritten with what THIS exit means, and the file
	// itself stays either way. It is not deleted on the way out on purpose: it
	// exists to be able to go back, and being kicked does not change that,
	// because kicking is not banning.
	//
	// **Two exits switch the return off, and they are the two where somebody
	// decided.** Pressing leave, from any of the four faces, and the host
	// kicking. Everything else is the room ending without this side asking for
	// it -- shutting down, closing Kanpachi, a power cut, twenty minutes with no
	// host -- and coming back is what the person expects.
	//
	// Kicked keeps the file and loses the return, which is the whole of "kicking
	// is not banning" in one line: the button to go back is still there, and the
	// machine does not crawl back on its own ten seconds after the host threw it
	// out. Entering again on purpose switches it back on.
	s.saveLastRoomLocked(exit != domain.ExitUser && exit != domain.ExitKicked)

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
	s.credencialMuerta = false
	// La credencial del invitado muere con su sala, igual que las emitidas del
	// host arriba: es de ESTA sala, y la siguiente empieza pidiendo la suya.
	s.myCredential = domain.Credential{}
	// La llave de miembro también sale de la memoria. Su semilla ya quedó — o
	// no — en la última sala guardada, según lo que signifique esta salida:
	// eso lo decidió saveLastRoomLocked unas líneas arriba.
	s.memberKey = domain.MemberKey{}
	// Y la racha de reingresos: la sala siguiente empieza con la suya.
	s.rejoinStreak = 0
	s.lastRejoinSuccess = time.Time{}
	s.announcedGame = ""
	// El desvío hacia donde escuchaba el juego se va con la sala, igual que la
	// compuerta: describe una traducción hacia un servidor que ya no sirve a
	// nadie, y dejarla puesta mandaría a esa dirección lo que entre por el
	// adaptador de la sala siguiente. Se quita ANTES de perder el estado, que
	// es cuando todavía se sabe que había una. Ver [Session.applyRedirectLocked].
	s.gameReach = domain.GameReach{}
	s.applyRedirectLocked(ctx)
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

// closeRoomInRegistryLocked tells the registry this room is over.
//
// # Why it has to be said, measured
//
// Because closing the room reached the registry by no path at all, and its entry
// only died of old age. For up to six hours that code kept answering "it
// exists": whoever pasted it brought up a lobby, dialled a host that was gone,
// and ate the operating system's long timeout to land on a message about
// reconnecting. Checked against the deployment on 2026-08-15, twenty-two seconds
// of which twenty-one were that dial.
//
// # Host ONLY, and that guard is all that separates this from a mess
//
// `leaveLocked` runs for both roles, and a guest on the way out would arrive here
// holding the code of a room that is not theirs. They would not get anywhere,
// because the registry demands the signature of the key that pinned that invite
// ID, so all they would achieve is a 403 for every person who leaves a room. The
// guard is what keeps every guest's rate limit from being spent on requests that
// are going to fail.
//
// # Not fatal, and it cannot be
//
// Leaving ends outside the room whatever happens. A registry that does not answer
// costs the code resolving until the sweep takes the room, three weeks after the
// last time anybody hosted it, which is exactly what happened every time before
// this existed. One attempt and no more: the registry's rate
// limit counts what fails too.
//
// Asume el candado tomado.
func (s *Session) closeRoomInRegistryLocked(ctx context.Context) {
	if !s.state.IsHost() || s.state.Room.InviteID.IsZero() {
		return
	}
	// The registry that MINTED this code, and not our own. They are usually the
	// same and they do not have to be: a room reopened from the previous run
	// keeps its seed, and that is the only registry where its entry exists. Same
	// reason republishing the card takes it from the room.
	dir, err := s.deps.Directories.For(s.state.Room.Seed)
	if err != nil {
		s.deps.Log.Warn("no se pudo abrir el registro para cerrar la sala",
			"seed", s.state.Room.Seed, "error", err)
		return
	}
	if err := dir.Retire(ctx, s.state.Room.InviteID); err != nil {
		s.deps.Log.Warn("el registro no aceptó el cierre de la sala, así que el código "+
			"va a seguir resolviendo hasta que venza su tarjeta",
			"código", s.state.Room.InviteID.String(), "error", err)
		return
	}
	s.deps.Log.Info("la sala quedó cerrada en el registro",
		"código", s.state.Room.InviteID.String())
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
