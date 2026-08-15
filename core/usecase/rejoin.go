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

// El reingreso del invitado: volver a pedir credencial cuando el host se cayó y
// volvió sin recordar a nadie.
//
// # Qué pasa hoy sin esto
//
// Las credenciales viven en la memoria del motor del host y mueren con su
// proceso. Así que un host que reinicia, aunque reabra la MISMA sala con el
// MISMO código, ya no reconoce la llave de nadie: los invitados dejan de ser de
// confianza, no se forma ruta hacia él, y su canal de control no puede levantar
// aunque reintente. A los veinte minutos cada uno sale solo y su usuario tiene
// que volver a pegar el código.
//
// El invitado tiene todo lo que hace falta para arreglarlo por su cuenta, y lo
// tiene guardado desde siempre: el código, el seed y su apodo están en
// `last-room.json`, que es exactamente lo que pide entrar. Lo único que faltaba
// era que alguien lo llamara solo.
//
// # Por qué NO se guarda la credencial en disco, que sería lo otro
//
// Porque volver a pedirla da el mismo resultado sin poner en disco una llave de
// sala, y porque **renovar el código sigue cerrando la puerta gratis**: quien
// estaba conectado recibió el código nuevo por el canal de control y entra, y
// quien estaba apagado tiene el viejo, deriva el vestíbulo viejo, y ahí no
// espera nadie. Con la credencial guardada eso habría que construirlo.
//
// El precio es que se recibe una dirección nueva. Ver [timing.ArrivalGrace].

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
	// El reloj se sella ANTES de intentar, igual que en [Session.announceLocked]:
	// si esto falla, el intento siguiente va cuando toque y no de inmediato.
	s.lastRejoin = now
	s.rejoinWait = s.nextRejoinWaitLocked()

	room, nick := s.state.Room, s.nick
	s.deps.Log.Info("el host no está, se le vuelve a pedir credencial",
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

	s.deps.Log.Info("se volvió a entrar a la sala",
		"código", room.InviteID.String(), "ip", s.state.LocalIP.String())
	s.snapshot()
	return nil
}

// rejoinLocked es el cuerpo, aparte para que el diario de progreso se cierre en
// un solo sitio pase lo que pase. Asume el candado tomado.
func (s *Session) rejoinLocked(ctx context.Context, room domain.Room, nick domain.Nickname) error {
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
