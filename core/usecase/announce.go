package usecase

import (
	"context"
	"net/netip"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// announceLocked le cuenta a los presentes cómo está la sala.
//
// Lo llama el host después de todo lo que cambia algo que el invitado necesita
// saber: elegir juego, quitarlo, renombrar la sala, y cada cambio de miembros,
// porque quien acaba de entrar no estaba cuando se anunció lo anterior. Y cada
// [timing.AnnounceInterval], que es lo que hace medible el silencio del otro lado.
//
// No es fatal que falle. Lo que se pierde es que la pantalla del otro muestre
// el juego, y lo que NO se pierde es la sala: el túnel sigue, las reglas del
// host ya están aplicadas, y el que entró puede jugar si sabe a qué. Cortar la
// operación por esto convertiría un problema de presentación en uno de red.
//
// Asume el candado tomado.
func (s *Session) announceLocked(ctx context.Context) {
	s.announceToLocked(ctx, netip.Addr{})
}

// announceToLocked es lo mismo dirigido a UN miembro, y con la dirección en
// cero es [Session.announceLocked].
//
// # Por qué el que entra necesita uno para él
//
// Porque el anuncio es lo único que le dice qué juego está activo, y entrar no
// lo dispara. Sin esto, quien acaba de llegar espera al periódico, que es
// [timing.AnnounceInterval]: hasta dos minutos leyendo «todavía no eligió
// juego», que no es «no lo sé», es una afirmación sobre el host que además es
// falsa. Medido el 2026-08-20 con una sala en Docker.
//
// # Por qué al resto no se le repite
//
// Porque ya lo sabían. El anuncio es ESTADO, así que mandárselo otra vez no
// rompe nada, y por eso el reparto a todos sigue siendo lo normal en los demás
// casos; lo que no hace es servir de algo. Un miembro que entra no cambia el
// nombre de la sala ni el juego, que es todo lo que el anuncio lleva.
//
// # El reloj no se sella acá
//
// Porque este anuncio no es el periódico ni lo reemplaza: le habla a uno solo.
// Sellarlo correría el plazo de los demás por algo que no recibieron, y tres
// invitados entrando seguidos dejarían a la sala sin anuncio general durante
// seis minutos.
//
// Asume el candado tomado.
func (s *Session) announceToLocked(ctx context.Context, to netip.Addr) {
	if !s.state.IsHost() || !s.state.Conn.InRoom() {
		return
	}
	if !to.IsValid() {
		// El reloj se sella pase lo que pase, y no solo cuando sale bien. Si el
		// anuncio falla, reintentarlo en el siguiente latido en vez de en cada
		// evento es lo correcto: el canal está roto y machacarlo no lo arregla.
		s.lastAnnounce = s.deps.Clock.Now()
	}

	s.measureGameHealthLocked(ctx)

	err := s.deps.Control.Announce(ctx, to, domain.RoomAnnounce{
		RoomName:         s.state.Name,
		GameID:           s.state.Game.ID,
		GameHealth:       s.gameReach.Health,
		GameWhere:        s.gameReach.Where,
		GameRedirectedTo: s.redirectedTo,
	})
	if err != nil {
		s.deps.Log.Warn("no se pudo anunciar el estado de la sala", "error", err, "a", to.String())
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
	// An empty name is NO INFORMATION, never a rename: no face offers renaming
	// a room to nothing, so the only machines that announce an empty name are
	// hosts that never had one to send. Taking it would trade a name this
	// machine learned from the card or from its own disk for a blank. Measured
	// live on 2026-08-18: a host announcing without a name wiped "Merwebo
	// Zomboid" off every guest's screen one heartbeat after they joined.
	if a.RoomName != "" {
		s.state.Name = a.RoomName
	}
	s.announcedGame = a.GameID
	// La salud la MIDIÓ el host sobre su propia máquina, así que acá se toma
	// tal cual: es lo único de este anuncio que un invitado no puede
	// contestarse solo. No decide nada, solo se pinta.
	s.gameReach = domain.GameReach{Health: a.GameHealth, Where: a.GameWhere}
	s.announcedRedirect = a.GameRedirectedTo

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
	// El desvío va acá también, y su ausencia era un ámbar falso. Esto
	// reconstruye un anuncio que ya llegó para volver a aplicarlo, y
	// [Session.OnRoomAnnounce] escribe lo que reciba: sin este campo, importar
	// el perfil que faltaba dejaba a cero un desvío que el host sí tenía, justo
	// en el instante en que la tarjeta del juego aparece. Se corregía solo dos
	// minutos después, en el anuncio periódico.
	ann := domain.RoomAnnounce{
		RoomName:         s.state.Name,
		GameID:           s.announcedGame,
		GameHealth:       s.gameReach.Health,
		GameWhere:        s.gameReach.Where,
		GameRedirectedTo: s.announcedRedirect,
	}
	s.mu.Unlock()

	if !pendiente {
		return
	}
	if _, err := s.OnRoomAnnounce(ctx, ann); err != nil {
		s.deps.Log.Warn("no se pudo aplicar el juego que faltaba", "error", err)
	}
}

// measureGameHealthLocked mira si algo escucha en los puertos del juego activo.
//
// Corre en el HOST y solo ahí: es su tabla de sockets la que contesta si el
// servidor de la partida está levantado. Va pegado al anuncio, que es cuando
// hace falta y lo que le pone cadencia: cada cambio que importa y cada
// [timing.AnnounceInterval], nunca un bucle propio.
//
// Que falle no es un error de nada. Se registra fino y el estado queda en
// [domain.GameHealthUnknown], que es lo que hace que la pantalla no pinte
// ningún punto en vez de pintar uno equivocado.
//
// Mide también DÓNDE escucha, no solo si escucha, y de ahí cuelgan las dos
// cosas que arreglan el caso: la pantalla puede nombrar la dirección, y el modo
// contenedor sabe hacia dónde desviar. Ver [domain.GameReach].
//
// Asume el candado tomado.
func (s *Session) measureGameHealthLocked(ctx context.Context) {
	if len(s.state.Game.HostPorts) == 0 {
		s.gameReach = domain.GameReach{}
		s.applyRedirectLocked(ctx)
		return
	}
	listeners, err := s.deps.Listeners.Listening(ctx)
	if err != nil {
		s.deps.Log.Warn("no se pudo mirar qué escucha en esta máquina", "error", err)
		s.gameReach = domain.GameReach{}
		s.applyRedirectLocked(ctx)
		return
	}
	s.gameReach = domain.GameReachOf(s.state.Game.HostPorts, listeners, s.state.LocalIP)
	s.applyRedirectLocked(ctx)
}

// applyRedirectLocked pone o quita el desvío hacia donde el juego escucha.
//
// **Solo en modo contenedor**, y el porqué de esa frontera vive en
// [domain.RedirectSpec]: ahí la intención del operador es inequívoca y no hay
// red de casa que proteger. En una máquina normal esto no llega a llamarse,
// porque el adaptador que lo haría no se cablea.
//
// Se llama en cada medición, con desvío y sin él, y ese es el punto: la
// condición se recalcula entera cada vez, así que un servidor que se ata bien
// después de haberse atado mal quita el desvío solo, sin que nadie tenga que
// acordarse.
//
// Que falle no saca a nadie de la sala. Se anota y el estado publicado no
// afirma que haya desvío, que es lo correcto: lo que se enseña es lo que se
// consiguió, no lo que se intentó.
//
// Asume el candado tomado.
func (s *Session) applyRedirectLocked(ctx context.Context) {
	if s.deps.Redirect == nil {
		return
	}

	spec := domain.RedirectSpec{
		Adapter: domain.AdapterName,
		RoomIP:  s.state.LocalIP,
		To:      s.gameReach.Where,
		Ports:   s.state.Game.HostPorts,
	}
	quiere := s.state.IsHost() && s.state.Conn.InRoom() &&
		s.gameReach.Health == domain.GameHealthElsewhere && spec.Understood()

	if !quiere {
		if !s.redirectedTo.IsValid() {
			return
		}
		if err := s.deps.Redirect.Clear(ctx); err != nil {
			s.deps.Log.Warn("no se pudo quitar el desvío hacia el juego", "error", err)
			return
		}
		s.deps.Log.Info("se quita el desvío hacia el juego", "era", s.redirectedTo.String())
		s.redirectedTo = netip.Addr{}
		s.reapplyForRedirectLocked(ctx)
		return
	}

	if s.redirectedTo == spec.To {
		return
	}
	if err := s.deps.Redirect.Apply(ctx, spec); err != nil {
		s.deps.Log.Warn("no se pudo desviar hacia donde escucha el juego",
			"hacia", spec.To.String(), "error", err)
		s.redirectedTo = netip.Addr{}
		return
	}
	s.redirectedTo = spec.To
	s.deps.Log.Info("el juego escucha en otra dirección y se desvía hacia ella",
		"juego", s.state.Game.ID, "sala", spec.RoomIP.String(), "hacia", spec.To.String())
	s.reapplyForRedirectLocked(ctx)
}

// reapplyForRedirectLocked vuelve a escribir los permisos después de que el
// desvío se ponga o se quite.
//
// Hace falta porque el conjunto deseado CAMBIA con el desvío: con él, las mismas
// reglas cubren además la dirección traducida. Y hay que olvidar la firma antes,
// porque el atajo de alta frecuencia compara conjuntos y este cambio no viene de
// los miembros ni del perfil. Ver [Session.applyPolicyIfChanged].
//
// Asume el candado tomado.
func (s *Session) reapplyForRedirectLocked(ctx context.Context) {
	s.appliedRules = ""
	if err := s.applyPolicy(ctx); err != nil {
		s.deps.Log.Warn("no se pudieron reescribir los permisos tras cambiar el desvío", "error", err)
	}
}
