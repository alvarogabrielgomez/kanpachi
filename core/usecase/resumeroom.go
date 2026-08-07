package usecase

import (
	"context"
	"errors"
	"fmt"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/core/port"
)

// ErrNoPendingRoom es reanudar sin nada que reanudar.
var ErrNoPendingRoom = errors.New("no hay ninguna sala del arranque anterior")

// PendingRoom es la sala que quedó abierta cuando el daemon murió sucio.
//
// **Su sola existencia es la señal de mal cierre.** Salir limpio borra el
// archivo y morir sucio lo deja, así que no hace falta una bandera `dirty`
// dentro, que sería un campo más que alguien puede escribir a mano.
//
// La lee la UI al arrancar para preguntar. Preguntar y no reanudar solo: es la
// invariante de que nada que llegue de fuera de la app surte efecto sin
// confirmación dentro de la app, y acá lo de fuera es un archivo del arranque
// anterior.
func (s *Session) PendingRoom() (domain.PersistedRoom, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.pending, s.hasPending
}

// DiscardPendingRoom tira la sala anterior. Es el "no, ciérrala" de la
// pregunta del arranque.
func (s *Session) DiscardPendingRoom(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.hasPending {
		return ErrNoPendingRoom
	}
	if err := s.deps.State.ClearRoom(); err != nil {
		return fmt.Errorf("borrando la sala del arranque anterior: %w", err)
	}
	s.pending = domain.PersistedRoom{}
	s.hasPending = false
	s.deps.Log.Info("la sala del arranque anterior se descartó")
	return nil
}

// ResumeRoom vuelve a abrir la sala que quedó de un apagón, con el MISMO
// código y la MISMA red.
//
// Que la identidad de la red sea la misma es lo que hace que esto sirva: los
// invitados guardan una credencial contra esa red, así que un host que vuelve
// dentro de los veinte minutos del contador de la decisión 20 se los encuentra
// reconectando solos. Uno que tarda más reabre una sala vacía con el mismo
// código, que es igual de correcto y era el otro caso a cubrir.
//
// # Lo que NO se restaura del disco
//
// Los miembros y las reglas. Los miembros son lo que reporte el motor, y las
// reglas se recalculan desde ahí. Restaurarlas del archivo abriría puertos
// hacia direcciones que hoy pueden no ser de nadie, que es exactamente lo que
// la cuarentena por defecto existe para impedir. Con cero miembros presentes el
// conjunto deseado es el vacío, así que reponer el juego no abre nada hasta que
// haya alguien de verdad.
func (s *Session) ResumeRoom(ctx context.Context) (domain.RoomState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.state.Conn.InRoom() {
		return domain.RoomState{}, ErrBusy
	}
	if !s.hasPending {
		return domain.RoomState{}, ErrNoPendingRoom
	}
	saved := s.pending

	if err := s.state.Transition(domain.StateResolving, "el usuario reabrió la sala anterior"); err != nil {
		return domain.RoomState{}, err
	}
	ok := false
	defer func() {
		if !ok {
			s.teardown(ctx)
			_ = s.state.TransitionWithExit(domain.StateIdle, "falló reabrir la sala anterior", domain.ExitFailed)
			s.snapshot()
		}
	}()

	// La subred se comprueba contra la máquina de HOY. Una laptop que abrió la
	// sala en casa y la reabre en la oficina puede tener ahora una LAN que pisa
	// ese rango, y levantarla igual dejaría al usuario sin esa red.
	//
	// Se falla en vez de elegir otra subred, y es deliberado: cambiarla haría
	// que las credenciales emitidas apuntaran a un rango que ya no existe, o
	// sea rompería justo la reconexión por la que esta función existe. El
	// usuario descarta la sala y crea una nueva, que es un click.
	if err := s.checkSubnetAgainstLocal(ctx, saved.Subnet); err != nil {
		return domain.RoomState{}, fmt.Errorf("la sala anterior ya no cabe en esta máquina: %w", err)
	}

	spec := domain.HostSpec{
		Rendezvous:    domain.DeriveRendezvous(saved.Room.InviteID),
		NetworkID:     saved.NetworkID,
		NetworkSecret: saved.NetworkSecret,
		Name:          saved.Host,
		Subnet:        saved.Subnet,
		Seeds:         seedsFor(saved.Room),
	}

	if err := s.state.Transition(domain.StateConnecting, "levantando la red de la sala anterior"); err != nil {
		return domain.RoomState{}, err
	}
	if err := s.deps.Engine.HostNetwork(ctx, spec); err != nil {
		return domain.RoomState{}, fmt.Errorf("levantando la red de la sala anterior: %w", err)
	}

	local := domain.HostAddress(saved.Subnet)
	s.state.Role = domain.RoleHost
	s.state.Room = saved.Room
	s.state.Name = saved.Name
	s.state.LocalIP = local
	s.state.Subnet = saved.Subnet
	s.state.HostPresent = true
	s.state.Net.Subnet = saved.Subnet
	s.state.Net.SubnetReason = "la subred es la que tenía la sala anterior"
	s.state.Peers = []domain.Peer{{
		VirtualIP: local, Name: saved.Host, Path: domain.PathSelf, Self: true, Host: true,
	}}
	s.hostSpec = spec
	s.cardKey = saved.CardKey
	s.sealedCard = saved.Card
	s.nick = saved.Host

	s.configureAdapter(ctx)

	if err := s.deps.Engine.JoinRendezvous(ctx, domain.RendezvousSpec{
		Rendezvous: spec.Rendezvous,
		Address:    domain.RendezvousHostAddress,
		Name:       saved.Host,
		Seeds:      spec.Seeds,
	}); err != nil {
		return domain.RoomState{}, fmt.Errorf("abriendo la puerta de la sala anterior: %w", err)
	}
	// Los dos adaptadores ya están arriba, y la compuerta se acota antes de
	// abrir un solo puerto, igual que al crear. Reabrir una sala no es un
	// camino de menos exigencia: es el mismo host con la misma red.
	if err := s.deps.Firewall.BindRoom(ctx, saved.Subnet, domain.BindRoomAndLobby); err != nil {
		return domain.RoomState{}, fmt.Errorf("acotando la contención a la sala anterior: %w", err)
	}
	if err := s.deps.Control.Serve(ctx, s.controlScope()); err != nil {
		return domain.RoomState{}, fmt.Errorf("abriendo el canal de la sala: %w", err)
	}
	if err := s.state.Transition(domain.StateConnected, "la sala anterior está levantada"); err != nil {
		return domain.RoomState{}, err
	}

	// El juego se repone DESPUÉS de estar conectado y por el camino de siempre,
	// resolviendo el id contra el catálogo de esta máquina. Un perfil que ya no
	// está deja la sala sin juego, que es el estado por defecto y el seguro.
	s.restoreGameLocked(ctx, saved.GameID)

	// Sin juego el conjunto deseado es el vacío, y se aplica igual: la
	// cuarentena por defecto es un acto, no una omisión. Con juego,
	// restoreGameLocked ya aplicó, y volver a aplicar el mismo conjunto es un
	// no-op porque el puerto es declarativo.
	if err := s.applyPolicy(ctx); err != nil {
		return domain.RoomState{}, err
	}

	s.republishCardLocked(ctx)

	s.saveRoomLocked()
	s.pending = domain.PersistedRoom{}
	s.hasPending = false

	ok = true
	s.deps.Log.Info("sala anterior reabierta",
		"código", saved.Room.InviteID.String(), "subred", saved.Subnet.String(), "juego", s.state.Game.ID)
	return s.snapshot(), nil
}

// restoreGameLocked repone el juego que estaba activo. Asume el candado tomado.
//
// No es fatal que falle, y por eso no devuelve error: la sala ya está abierta y
// levantada, y quedarse sin reabrirla porque un perfil se borró sería cambiar
// un problema chico por uno grande. El usuario elige el juego a mano y sigue.
func (s *Session) restoreGameLocked(ctx context.Context, gameID string) {
	if gameID == "" {
		return
	}
	p, ok := s.catalog.Find(gameID)
	if !ok {
		s.deps.Log.Info("el juego que estaba activo ya no está en el catálogo", "juego", gameID)
		return
	}
	s.state.Game = p
	if err := s.applyPolicy(ctx); err != nil {
		s.state.Game = domain.GameProfile{}
		s.deps.Log.Warn("no se pudieron reponer las reglas del juego anterior", "juego", gameID, "error", err)
		return
	}
	s.configureAdapter(ctx)
	s.deps.Log.Info("juego repuesto de la sala anterior", "juego", gameID)
}

// republishCardLocked vuelve a subir la tarjeta que ya estaba publicada.
//
// # Por qué hace falta reabrir para eso
//
// El registro guarda las tarjetas EN MEMORIA y con vencimiento, así que una
// sala que sobrevive a un apagón se encuentra a sí misma en pie y su tarjeta
// muerta: la página de invitación mostraría la genérica sobre una sala que está
// funcionando. Nada más en el producto la vuelve a subir.
//
// Se suben los MISMOS bytes, no una tarjeta nueva. Es lo que conserva válidos
// los enlaces que ya se repartieron: la clave que los abre es la que se acaba de
// cargar del disco, y volver a sellar produciría otra.
//
// **No es fatal y no es un reintento.** Que falle cuesta la tarjeta y nada más,
// que es exactamente lo que este puerto promete costar. Y se intenta UNA vez,
// porque el límite de tasa del registro cuenta también lo que falla.
//
// Sin tarjeta guardada no se llama a nadie: es el caso de un `room.json` escrito
// antes de que el campo existiera, y el del respaldo de crear, donde el invite ID
// lo generó esta máquina y el registro no lo emitió nunca. Esa guarda es además
// lo que hace que esto sea SOLO del host por el dato y no por convención: un
// invitado jamás llena sealedCard.
//
// # Por qué ya no lo llama solo reabrir
//
// Porque la tarjeta también muere EN VIDA de la sala. El registro la caduca a
// las seis horas de la última publicación, así que una partida que sigue abierta
// dejaba de aceptar gente nueva sin un solo error de este lado. Ahora esto
// además cuelga del latido, cada [RepublishInterval]. Ver `enforceDeadlinesLocked`.
//
// Asume el candado tomado.
func (s *Session) republishCardLocked(ctx context.Context) {
	if len(s.sealedCard) == 0 {
		return
	}
	// El reloj se sella pase lo que pase, y no solo cuando sale bien. Es lo
	// mismo que hace announceLocked y por lo mismo: si el registro no contesta,
	// reintentar en el siguiente intervalo es correcto, y machacarlo en cada
	// latido de quince segundos solo gasta el límite de tasa con peticiones que
	// también cuentan cuando fallan.
	s.lastPublish = s.deps.Clock.Now()

	err := s.deps.Directory.Publish(ctx, s.state.Room.InviteID, s.sealedCard)

	// "No conozco esa sala" es distinto de "no contesto", y por eso se separa.
	// Lo primero es definitivo: la entrada se fue del registro y publicar no
	// crea, así que el código repartido ya no sirve y solo lo arregla renovarlo.
	// Lo segundo se cura solo en el siguiente latido y NO borra un código ya
	// declarado muerto: solo una publicación aceptada o renovarlo demuestra que
	// vuelve a existir.
	if err == nil {
		s.state.CodeLost = false
	} else if errors.Is(err, port.ErrUnknownRoom) {
		s.state.CodeLost = true
	}

	switch {
	case err == nil:
		if s.cardPublishFailing {
			s.cardPublishFailing = false
			s.deps.Log.Info("el registro volvió a aceptar la tarjeta de la sala",
				"código", s.state.Room.InviteID.String())
		}
	case s.cardPublishFailing:
		// Ya se avisó. Repetirlo en cada intervalo llenaría el diario sin
		// aportar nada nuevo, que es como se pierde de vista lo que sí importa.
	default:
		s.cardPublishFailing = true
		s.deps.Log.Warn("la tarjeta de la sala no se pudo republicar, la página de invitación "+
			"va a mostrar la genérica", "error", err, "código", s.state.Room.InviteID.String())
	}
}
