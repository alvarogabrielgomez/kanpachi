package usecase

import (
	"context"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// AnnounceInterval es cada cuánto el host REPITE el anuncio de sala.
//
// No es un latido nuevo y no contradice la decisión 23: es el mismo mensaje que
// ya se manda en cada cambio de juego, de nombre y de miembros, repetido. La
// conexión sigue siendo el latido para el caso normal, y esto existe para el
// caso en que la conexión miente, o sea un socket TCP medio abierto que
// sobrevive horas a una máquina apagada de golpe.
//
// Dos minutos son unos pocos cientos de bytes por miembro y por hora, y tres
// anuncios perdidos caben dentro de [domain.HostSilenceLimit].
const AnnounceInterval = 2 * time.Minute

// announceLocked le cuenta a los presentes cómo está la sala.
//
// Lo llama el host después de todo lo que cambia algo que el invitado necesita
// saber: elegir juego, quitarlo, renombrar la sala, y cada cambio de miembros,
// porque quien acaba de entrar no estaba cuando se anunció lo anterior. Y cada
// [AnnounceInterval], que es lo que hace medible el silencio del otro lado.
//
// No es fatal que falle. Lo que se pierde es que la pantalla del otro muestre
// el juego, y lo que NO se pierde es la sala: el túnel sigue, las reglas del
// host ya están aplicadas, y el que entró puede jugar si sabe a qué. Cortar la
// operación por esto convertiría un problema de presentación en uno de red.
//
// Asume el candado tomado.
func (s *Session) announceLocked(ctx context.Context) {
	if !s.state.IsHost() || !s.state.Conn.InRoom() {
		return
	}
	// El reloj se sella pase lo que pase, y no solo cuando sale bien. Si el
	// anuncio falla, reintentarlo en el siguiente latido en vez de en cada
	// evento es lo correcto: el canal está roto y machacarlo no lo arregla.
	s.lastAnnounce = s.deps.Clock.Now()

	err := s.deps.Control.Announce(ctx, domain.RoomAnnounce{
		RoomName: s.state.Name,
		GameID:   s.state.Game.ID,
	})
	if err != nil {
		s.deps.Log.Warn("no se pudo anunciar el estado de la sala", "error", err)
	}
}

// OnRoomAnnounce aplica lo que anunció el host. Lo llama el supervisor cuando
// llega algo por el canal de control.
//
// **El id del juego se resuelve contra el catálogo PROPIO.** Esa línea es la
// frontera: el host dice qué se está jugando, y esta máquina decide qué abre.
// Si no tiene ese perfil no abre nada, y si lo tiene abre lo que dice SU copia,
// con sus invariantes, no la del otro. Es la misma regla que gobierna el named
// pipe, aplicada al otro canal por el que entra una orden de fuera.
//
// Un host no toma anuncios. Su estado es el original, y aceptarlos le
// permitiría a un miembro modificado cambiarle el juego activo al host, que es
// justo la máquina donde se abren los puertos.
func (s *Session) OnRoomAnnounce(ctx context.Context, raw domain.RoomAnnounce) (domain.RoomState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.state.Conn.InRoom() || s.state.IsHost() {
		return s.snapshot(), nil
	}
	// Un anuncio que llega es prueba de vida del host, y se anota antes de
	// mirar qué dice: es la señal que llega sola cuando la caída del socket es
	// una señal que nunca va a llegar.
	s.state.NoteHostAlive(s.deps.Clock.Now())

	if s.enforceDeadlinesLocked(ctx) {
		return s.snapshot(), nil
	}

	a := raw.Sanitize()
	s.state.Name = a.RoomName
	s.announcedGame = a.GameID

	previo := s.state.Game
	switch {
	case a.GameID == "":
		s.state.Game = domain.GameProfile{}
	default:
		p, ok := s.catalog.Find(a.GameID)
		if !ok {
			// No es un error y no se reintenta. Es el caso normal de que el
			// host juegue algo que esta máquina no tiene en su catálogo: la
			// sala sigue, no se abre nada, y la UI puede decir qué falta.
			s.deps.Log.Info("el host activó un juego que no está en este catálogo", "juego", a.GameID)
			s.state.Game = domain.GameProfile{}
			break
		}
		s.state.Game = p
	}

	if s.state.Game.ID == previo.ID {
		return s.snapshot(), nil
	}
	if err := s.applyPolicy(ctx); err != nil {
		s.state.Game = previo
		return domain.RoomState{}, err
	}
	// Los ajustes del adaptador salen del perfil, así que un invitado en un
	// juego que pide ruta de multicast también la necesita: el descubrimiento
	// LAN no funciona en un solo sentido.
	s.configureAdapter(ctx)

	s.deps.Log.Info("el host cambió el juego", "juego", s.state.Game.ID, "anterior", previo.ID)
	return s.snapshot(), nil
}

// MissingGame devuelve el id que anunció el host y que esta máquina no tiene
// en su catálogo. Vacío si no falta nada.
//
// Se CALCULA cada vez en vez de guardar un booleano: el catálogo cambia
// mientras la sala está viva, porque el usuario importa el perfil que le
// acaban de mandar por Telegram, y una marca guardada seguiría diciendo que
// falta después de que ya no falte.
func (s *Session) MissingGame() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.announcedGame == "" {
		return ""
	}
	if _, hay := s.catalog.Find(s.announcedGame); hay {
		return ""
	}
	return s.announcedGame
}

// ReapplyAnnouncedGame vuelve a intentar con lo último que anunció el host.
//
// Lo llama la sesión después de importar o guardar un perfil: si lo que
// faltaba era justo ese, la sala pasa a tener juego sin que el host tenga que
// volver a elegirlo ni nadie que reconectar.
func (s *Session) ReapplyAnnouncedGame(ctx context.Context) {
	s.mu.Lock()
	pendiente := s.announcedGame != "" && s.state.Game.ID != s.announcedGame && !s.state.IsHost()
	ann := domain.RoomAnnounce{RoomName: s.state.Name, GameID: s.announcedGame}
	s.mu.Unlock()

	if !pendiente {
		return
	}
	if _, err := s.OnRoomAnnounce(ctx, ann); err != nil {
		s.deps.Log.Warn("no se pudo aplicar el juego que faltaba", "error", err)
	}
}
