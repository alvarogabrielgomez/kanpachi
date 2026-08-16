package usecase

import (
	"context"
	"fmt"
	"io"
	"net/netip"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/core/timing"
)

// La vuelta del invitado a su sala, por el camino más corto que siga siendo
// verdad: primero reengancharse con la credencial que ya tiene, y de respaldo
// el canje completo por el vestíbulo.
//
// # Los dos casos que esto cubre, que piden curas distintas
//
// **El motor murió y el host está bien.** El fallo medido el 2026-08-16: el
// camino de datos del motor se pudre, el silencio marca al host ausente, y la
// credencial sigue siendo perfectamente buena. La cura es reemplazar la
// instancia del motor CON ESA credencial: misma dirección, sin vestíbulo, en
// segundos. Antes de existir este camino, cada vuelta pedía credencial nueva y
// quemaba una dirección por minuto. Ver [Session.reattachLocked].
//
// **El host murió y volvió sin recordar a nadie.** Las credenciales emitidas
// viven en la memoria del motor del host y mueren con su proceso, así que el
// host que reinicia ya no reconoce la llave de nadie y el reenganche no forma
// ruta. Ahí corre el canje completo de siempre: el código, el seed y el apodo
// están en `last-room.json`, que es exactamente lo que pide entrar.
//
// # Por qué la credencial NO se guarda en disco, que sería lo tercero
//
// El reenganche la usa DESDE LA MEMORIA, y eso es deliberado: el disco del
// invitado no lleva nada que sirva para entrar sin pasar por el host, y
// **renovar el código sigue cerrando la puerta gratis**: quien estaba conectado
// recibió el código nuevo por el canal de control y entra, y quien estaba
// apagado tiene el viejo, deriva el vestíbulo viejo, y ahí no espera nadie. Con
// la credencial en disco eso habría que construirlo.
//
// El precio queda acotado al camino del canje: solo esa vuelta recibe una
// dirección nueva. Ver [timing.ArrivalGrace].

// maxRejoinStreak es cuántos reingresos SEGUIDOS se toleran antes de rendirse.
//
// Es un tope de intentos y no un plazo, así que vive acá y no en core/timing.
// El caso que frena se midió: un reingreso que triunfa y no se sostiene vuelve
// a disparar en un minuto, y como cada éxito reinicia el contador de ausencia
// del host, el corte de los veinte minutos no llega jamás. Ocho vueltas son
// suficientes para que cualquier tropiezo real se recupere, y pocas para que
// una sala que no se sostiene deje de masticarse a sí misma: se sale con
// motivo propio y la vuelta automática, que reintenta cada cinco minutos,
// queda como el ritmo de fondo.
const maxRejoinStreak = 8

// RejoinDue dice si toca reintentar el canje con el host.
//
// Es BARATO a propósito y se llama en cada latido: solo mira relojes y estado,
// no habla con nadie. Lo caro es [Session.Rejoin], que corre fuera del
// despachador del supervisor.
func (s *Session) RejoinDue() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rejoinDueLocked(s.deps.Clock.Now())
}

// rejoinDueLocked es la condición, en un solo sitio porque la comprueban los
// dos: el supervisor antes de lanzar la gorrutina, y [Session.Rejoin] otra vez
// ya con el candado tomado, porque entre una cosa y la otra el host pudo volver.
//
// Asume el candado tomado.
func (s *Session) rejoinDueLocked(now time.Time) bool {
	switch {
	// Un host no se reingresa a su propia sala. Y fuera de la sala no hay a
	// dónde volver: pasados los veinte minutos, el invitado ya salió y esto se
	// apaga solo, sin contador propio.
	case !s.state.Conn.InRoom(), s.state.IsHost():
		return false

	// Sin sala guardada no se puede rearmar el canje, y sin apodo tampoco: el
	// host exige nombre para emitir.
	case s.state.Room.InviteID.IsZero(), s.nick.IsZero():
		return false
	}

	// Que el HOST lo diga se salta las dos guardas de la ausencia, y hay que
	// saltarlas porque las dos lo impiden.
	//
	// Medido el 2026-08-11: el aviso llega por un canal de control VIVO, o sea
	// que el host está presente y su reloj de ausencia acaba de reiniciarse con
	// la prueba de vida que es el propio mensaje. Esperar a [timing.RejoinAfter] sobre
	// un reloj que no corre es esperar para siempre.
	//
	// Y no hay nada que desambiguar, que es para lo que existía la espera: aquí
	// no se está infiriendo nada de un socket que parpadea, lo dice el único que
	// lo puede saber. Ver [domain.NoticeStale].
	if !s.credencialMuerta {
		switch {
		// La ausencia del host es TODA la otra condición. Es el mismo hecho que
		// arma el corte de los veinte minutos, así que esto corre exactamente
		// durante esa ventana y ni un segundo más.
		case s.state.HostPresent:
			return false

		// Y no antes de [timing.RejoinAfter], que es lo que separa "el host reinició"
		// de "parpadeó la WiFi". El cero no puede pasar: significaría que el
		// host está, y eso ya lo descartó la rama de arriba.
		case s.state.HostGoneSince.IsZero(), now.Sub(s.state.HostGoneSince) < timing.RejoinAfter:
			return false
		}
	}
	return now.Sub(s.lastRejoin) >= s.rejoinWait
}

// Rejoin rehace el canje por el vestíbulo y vuelve a entrar a la red real.
//
// **LARGO.** Levanta el vestíbulo, marca al host, canjea, y reemplaza la
// instancia de sala del motor. Lo llama el supervisor desde una gorrutina
// propia, nunca desde su despachador: un latido perdido acá es el corte de los
// veinte minutos dejando de vencer.
//
// # Qué NO toca, y cada cosa por su motivo
//
// **La máquina de estados.** No se pasa a Reconectando, y eso es deliberado:
// esa transición arranca [timing.ReconnectLimit], o sea el corte de diez
// minutos, y acá el túnel del invitado nunca se cayó. Quien manda en este caso
// es el de veinte por ausencia del host, y meter el de diez lo adelantaría a la
// mitad sin que nada se hubiera roto.
//
// **La sala guardada.** El código y el apodo son los mismos, así que no hay nada
// nuevo que escribir.
//
// # Qué pasa cuando falla
//
// Se sale del vestíbulo y se deja todo como estaba. No se deshace nada más: el
// invitado ya estaba sin host cuando esto empezó, la ausencia sigue corriendo su
// contador, y el intento siguiente llega en menos de un minuto.
//
// Salir del vestíbulo sí es obligatorio, y es la única limpieza que hay. El
// vestíbulo lo puede observar cualquiera con el código, así que un intento
// fallido que lo deje levantado convierte cada reintento en una vía por la que
// un desconocido ve que esta máquina está en esa sala.
func (s *Session) Rejoin(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.deps.Clock.Now()
	if !s.rejoinDueLocked(now) {
		return nil
	}
	// El freno de los reingresos encadenados, ANTES de intentar otro. Ver
	// [maxRejoinStreak]: llegar acá con la racha llena significa que los
	// últimos ocho triunfaron y ninguno se sostuvo, y la novena vuelta no va a
	// ser distinta. Salir con motivo propio deja la pantalla diciendo la
	// verdad, y la vuelta automática sigue existiendo con su ritmo de fondo.
	if s.rejoinStreak >= maxRejoinStreak {
		s.deps.Log.Warn("la sala no se sostiene tras varios reingresos seguidos, se sale",
			"reingresos", s.rejoinStreak)
		s.leaveLocked(ctx, "la conexión con la sala no se sostuvo tras varios reingresos", domain.ExitTunnelLost, cerrarDeVerdad)
		return nil
	}

	// El reloj se sella ANTES de intentar, igual que en [Session.announceLocked]:
	// si esto falla, el intento siguiente va cuando toque y no de inmediato.
	s.lastRejoin = now
	s.rejoinWait = s.nextRejoinWaitLocked()

	room, nick := s.state.Room, s.nick
	s.deps.Log.Info("el host no está, se intenta volver a la sala",
		"código", room.InviteID.String(), "próximo intento en", s.rejoinWait)

	// Se publica que esto está pasando ANTES de empezar, y es la única
	// oportunidad: de acá en adelante el candado no se suelta hasta terminar, y
	// [Session.Status] lee la copia publicada justo para poder contestar
	// mientras tanto. Ver [domain.RoomState.Rejoining].
	s.state.Rejoining = true
	s.snapshot()

	s.deps.Progress.Begin("volver a la sala")
	err := s.rejoinLocked(ctx, room, nick)
	s.deps.Progress.End(err)

	s.state.Rejoining = false
	if err != nil {
		// Y se suelta el vestíbulo pase lo que pase. Ver la cabecera.
		if errSalir := s.deps.Engine.LeaveRendezvous(ctx); errSalir != nil {
			s.deps.Log.Warn("el motor no salió del vestíbulo tras un reingreso fallido", "error", errSalir)
		}
		s.deps.Log.Warn("no se pudo volver a entrar a la sala", "error", err)
		// Publicando, para que el cartel se apague al fallar y no dentro de un
		// latido: el que sale por acá no publica en ningún otro sitio.
		s.snapshot()
		return err
	}

	// Y se apaga el motivo, si el motivo era que el host lo dijo. Va DESPUÉS del
	// éxito a propósito: si el canje falló, la credencial sigue muerta y el
	// intento siguiente tiene que seguir saltándose las guardas de la ausencia.
	s.credencialMuerta = false

	// La racha cuenta ÉXITOS encadenados, no fallos: el bucle medido triunfaba
	// en cada vuelta. Dos reingresos con calma de por medio son incidentes
	// independientes y la racha vuelve a empezar; sin calma, se acumula hacia
	// el freno de arriba. Ver [timing.RejoinCalmAfter].
	if s.lastRejoinSuccess.IsZero() || now.Sub(s.lastRejoinSuccess) > timing.RejoinCalmAfter {
		s.rejoinStreak = 1
	} else {
		s.rejoinStreak++
	}
	s.lastRejoinSuccess = now

	s.deps.Log.Info("se volvió a entrar a la sala",
		"código", room.InviteID.String(), "ip", s.state.LocalIP.String())
	s.snapshot()
	return nil
}

// rejoinLocked es el cuerpo, aparte para que el diario de progreso se cierre en
// un solo sitio pase lo que pase. Asume el candado tomado.
//
// Son DOS caminos y el orden es el diseño: primero el reenganche con la
// credencial que esta sesión ya tiene, y solo si no se puede o no funciona, el
// canje completo por el vestíbulo. El fallo medido que motiva el primero es el
// camino de datos del motor muriendo con el host sano y la credencial buena:
// ahí reemplazar la instancia del motor es la cura entera, y pedir credencial
// nueva en cada vuelta era lo que quemaba una dirección por minuto.
func (s *Session) rejoinLocked(ctx context.Context, room domain.Room, nick domain.Nickname) error {
	if s.reattachPossibleLocked() {
		err := s.reattachLocked(ctx, nick)
		if err == nil {
			return nil
		}
		s.deps.Log.Warn("el reenganche con la credencial guardada no funcionó, se vuelve por el vestíbulo", "error", err)
	}

	// Re-entering goes through the SAME lobby, so it runs the same risk as
	// entering the first time: anybody holding the code can answer there. The
	// registry is asked again for the key it pinned.
	//
	// **And when the registry does not answer, the rejoin happens anyway,
	// unverified.** This path runs when the credential died and the room is
	// still up: refusing here would turn a registry outage into people thrown
	// out of a working room, which is worse than what is being avoided. It is
	// said in the log.
	cred, err := s.joinRealNetworkLocked(ctx, room, nick, s.pinnedHostKey(ctx, room))
	if err != nil {
		return err
	}
	s.myCredential = cred
	return s.settleIntoRoomLocked(ctx, cred)
}

// reattachPossibleLocked says whether the held credential is worth trying.
//
// Asume el candado tomado.
func (s *Session) reattachPossibleLocked() bool {
	switch {
	// El host dijo que no la tiene. Él es el único que lo puede saber, así que
	// intentarla igual solo gastaría la vuelta. Ver [Session.credencialMuerta].
	case s.credencialMuerta:
		return false
	case s.myCredential.Token == "", !s.myCredential.VirtualIP.IsValid():
		return false
	case s.myCredential.Expired(s.deps.Clock.Now()):
		return false
	}
	return true
}

// reattachLocked vuelve a la red real con la credencial que esta sesión ya
// tiene: mismo secreto, misma dirección, sin vestíbulo.
//
// Cuando el que falló es el motor y no la credencial, esto convierte la vuelta
// en segundos con la MISMA dirección, así que el servidor del juego ni se
// entera y el firewall del host no reescribe nada. El canje queda de respaldo
// para el caso contrario: un host que reinició no confía en la credencial de
// nadie, ahí esta entrada no forma ruta, el marcado vence, y el llamador cae
// al canje completo.
//
// Asume el candado tomado.
func (s *Session) reattachLocked(ctx context.Context, nick domain.Nickname) error {
	cred := s.myCredential
	s.deps.Progress.Stepf(domain.ScopeEngine, "reenganchando con tu credencial, tu dirección sigue siendo %s", cred.VirtualIP)
	if err := s.deps.Engine.JoinWithCredential(ctx, domain.GuestSpec{
		Credential: cred, Name: nick, Seeds: seedsFor(s.state.Room),
	}); err != nil {
		return fmt.Errorf("reentrando a la sala con la credencial guardada: %w", err)
	}
	return s.settleIntoRoomLocked(ctx, cred)
}

// settleIntoRoomLocked es todo lo que sigue a estar dentro de la red real, y
// es UNA función porque tiene dos llamadores, el canje y el reenganche. Dos
// copias de esta secuencia serían dos maneras de llegar que se separan en
// silencio, y la parte que menos puede separarse es la acotación del firewall.
//
// Asume el candado tomado.
func (s *Session) settleIntoRoomLocked(ctx context.Context, cred domain.Credential) error {
	// La dirección se anota ANTES de marcar al host, y el orden importa: el
	// motor ya está en la red con la dirección nueva, así que un fallo del
	// marcado tiene que dejar el estado diciendo la verdad. Al revés, la sesión
	// creería estar en una dirección que su propio adaptador ya no tiene, y el
	// reintento siguiente acotaría el firewall a la subred equivocada.
	s.state.LocalIP = cred.VirtualIP
	s.state.Subnet = cred.Subnet
	s.state.Net.Subnet = cred.Subnet
	s.state.Net.SubnetReason = "la subred la eligió el host de la sala"

	if err := s.deps.Control.Dial(ctx, domain.HostAddress(cred.Subnet)); err != nil {
		s.logMeshOnDialFailureLocked(ctx, domain.HostAddress(cred.Subnet))
		return fmt.Errorf("se volvió a entrar y el canal con el host no levantó: %w", err)
	}
	// Esto es lo que apaga el contador de los veinte minutos, y es el punto
	// entero de la función.
	s.state.NoteHostAlive(s.deps.Clock.Now())

	s.configureAdapter(ctx)

	// El adaptador es NUEVO: la instancia de sala del motor se reemplazó entera,
	// así que su identificador es otro. Una compuerta que se quedara apuntando
	// al viejo no falla en ningún sitio, emite sus filtros y devuelve éxito,
	// mientras debajo hay una red virtual sin bloqueo. Es la misma trampa que
	// documenta [Session.OnEngineRestarted], y acá es fatal por lo mismo.
	if err := s.deps.Firewall.BindRoom(ctx, cred.Subnet, netip.Prefix{}, domain.BindRoomOnly); err != nil {
		return fmt.Errorf("acotando la contención a la sala tras volver: %w", err)
	}
	if err := s.refreshPeersLocked(ctx); err != nil {
		return err
	}
	// Y las reglas se rehacen, porque cuelgan de la lista de miembros y de la
	// dirección propia, y las dos acaban de cambiar.
	return s.applyPolicy(ctx)
}

// nextRejoinWaitLocked reparte el intento siguiente dentro de la ventana.
//
// Sale de [port.Rand] y no de math/rand por lo mismo que el número del canario:
// hay una sola fuente de aleatoriedad en la sesión, es la que los tests pueden
// fijar, y no hace falta una segunda con sus propias sorpresas.
//
// Asume el candado tomado.
func (s *Session) nextRejoinWaitLocked() time.Duration {
	var b [1]byte
	if _, err := io.ReadFull(s.deps.Rand, b[:]); err != nil {
		// Sin aleatoriedad se espera el máximo, que es el lado seguro de
		// equivocarse: la manada queda sin dispersar, pero más separada en vez
		// de más apretada.
		s.deps.Log.Warn("no se pudo dispersar el reintento del canje", "error", err)
		return timing.RejoinInterval + timing.RejoinJitter
	}
	return timing.RejoinInterval + time.Duration(b[0])*timing.RejoinJitter/255
}
