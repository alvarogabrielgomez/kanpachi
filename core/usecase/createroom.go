package usecase

import (
	"context"
	"fmt"
	"io"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// CreateRoom abre una sala y te deja de host.
//
// NO pide juego, y eso es la decisión 20: la sala es independiente del juego
// activo. Nace con red cifrada y cero puertos abiertos, que es un estado
// válido y el estado por defecto, y el juego se elige adentro. Pedirlo acá
// haría que cambiar de juego se pareciera a crear otra sala.
//
// El rol queda fijado para toda la vida de la sala. Nadie lo hereda: si el
// host se va, no hay promoción ni elección, y quien quiera hospedar crea una
// sala nueva, que es un click.
func (s *Session) CreateRoom(ctx context.Context, nick domain.Nickname, roomName string) (domain.RoomState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.state.Conn.InRoom() {
		return domain.RoomState{}, ErrBusy
	}
	if nick.IsZero() {
		return domain.RoomState{}, domain.ErrNicknameEmpty
	}

	if err := s.state.Transition(domain.StateResolving, "el usuario creó una sala"); err != nil {
		return domain.RoomState{}, err
	}
	// Cualquier salida por error a partir de acá deshace lo que se alcanzó a
	// hacer y vuelve a Idle. Las dos mitades importan: sin la transición, la
	// sesión se quedaría en Resolving para siempre y la UI mostrando una
	// ruedita que no gira hacia ningún lado; sin el teardown, un fallo al
	// abrir el canal de control dejaría el motor levantado y a la máquina
	// dentro de una red que la app cree que no existe.
	ok := false
	defer func() {
		if !ok {
			s.teardown(ctx)
			_ = s.state.TransitionWithExit(domain.StateIdle, "falló la creación de la sala", domain.ExitFailed)
			// Se republica por lo mismo que en JoinRoom: quien llama descarta
			// el estado al recibir un error, así que si no se publica acá la
			// copia que lee Status se queda con la de antes del fallo.
			s.snapshot()
		}
	}()

	plan, err := s.planSubnet(ctx)
	if err != nil {
		return domain.RoomState{}, err
	}

	// La identidad de la red REAL. Aleatoria, generada acá, y no derivada de
	// ningún string que alguien pueda escribir: ni el seed, ni quien adivine
	// un invite ID, ni quien lea un log llegan a ella. Es lo que hace que
	// acertar un código dé la puerta y no la entrada.
	spec := domain.HostSpec{
		Name:   nick,
		Subnet: plan.Subnet,
	}
	if _, err := io.ReadFull(s.deps.Rand, spec.NetworkID[:]); err != nil {
		return domain.RoomState{}, fmt.Errorf("generando la identidad de la red: %w", err)
	}
	if _, err := io.ReadFull(s.deps.Rand, spec.NetworkSecret[:]); err != nil {
		return domain.RoomState{}, fmt.Errorf("generando el secreto de la red: %w", err)
	}

	room, key, err := s.publishCard(ctx, nick, roomName)
	if err != nil {
		return domain.RoomState{}, err
	}
	spec.Rendezvous = domain.DeriveRendezvous(room.InviteID)
	spec.Seeds = seedsFor(room)

	if err := s.state.Transition(domain.StateConnecting, "levantando la red"); err != nil {
		return domain.RoomState{}, err
	}
	if err := s.deps.Engine.HostNetwork(ctx, spec); err != nil {
		return domain.RoomState{}, fmt.Errorf("levantando la red: %w", err)
	}

	local := domain.HostAddress(plan.Subnet)
	s.state.Role = domain.RoleHost
	s.state.Room = room
	s.state.Name = domain.ClampRoomName(roomName)
	s.state.LocalIP = local
	s.state.Subnet = plan.Subnet
	s.state.HostPresent = true
	s.state.Net.Subnet = plan.Subnet
	s.state.Net.SubnetReason = plan.Reason
	s.state.Peers = []domain.Peer{{
		VirtualIP: local, Name: nick, Path: domain.PathSelf, Self: true, Host: true,
	}}
	s.hostSpec = spec
	s.cardKey = key
	s.nick = nick

	s.configureAdapter(ctx)

	// El host se queda TAMBIÉN en el vestíbulo mientras la sala esté abierta.
	// Es su puerta: el invitado no tiene forma de alcanzarlo en la red real
	// antes de tener la credencial, y la credencial es justo lo que va a pedir.
	if err := s.deps.Engine.JoinRendezvous(ctx, domain.RendezvousSpec{
		Rendezvous: spec.Rendezvous,
		Address:    domain.RendezvousHostAddress,
		Name:       nick,
		Seeds:      spec.Seeds,
	}); err != nil {
		return domain.RoomState{}, fmt.Errorf("abriendo la puerta de la sala: %w", err)
	}

	// La compuerta se acota ACÁ, con los DOS adaptadores ya arriba y antes de
	// abrir un solo puerto.
	//
	// Es fatal, a diferencia de los ajustes del adaptador: un MTU mal puesto
	// degrada la partida, y una sala sin compuerta deja la lista de permitidos
	// en aditiva, o sea una regla ajena de escritorio remoto alcanzando al
	// usuario por la red virtual. Una sala que no abre es mejor que una que dice
	// estar contenida y no lo está.
	if err := s.deps.Firewall.BindRoom(ctx, plan.Subnet, domain.BindRoomAndLobby); err != nil {
		return domain.RoomState{}, fmt.Errorf("acotando la contención a la sala: %w", err)
	}

	// El canal de control SOLO escucha en la máquina del host. Los invitados
	// marcan hacia afuera y no abren nada, así que su deny-all queda intacto.
	if err := s.deps.Control.Serve(ctx, s.controlScope()); err != nil {
		return domain.RoomState{}, fmt.Errorf("abriendo el canal de la sala: %w", err)
	}

	if err := s.state.Transition(domain.StateConnected, "la sala está levantada"); err != nil {
		return domain.RoomState{}, err
	}

	// Sin juego activo, el conjunto deseado es el vacío. Se aplica igual y no
	// se salta: es lo que garantiza la cuarentena por defecto, o sea que la
	// interfaz virtual nace sin nada abierto en vez de heredar lo que hubiera.
	if err := s.applyPolicy(ctx); err != nil {
		return domain.RoomState{}, err
	}

	// La sala se guarda recién cuando está entera y funcionando. Guardarla
	// antes dejaría en disco una sala que nunca llegó a existir, y el arranque
	// siguiente ofrecería reabrir algo que jamás estuvo abierto.
	s.saveRoomLocked()

	ok = true
	s.deps.Log.Info("sala creada",
		"código", room.InviteID.String(), "seed", room.Seed, "subred", plan.Subnet.String(), "motivo", plan.Reason)
	return s.snapshot(), nil
}

// publishCard registra la sala en el seed y devuelve el invite ID emitido.
//
// El registro EMITE el ID en vez de aceptarlo: quien tiene que garantizar
// unicidad es él, así que emitir evita el ida y vuelta de proponer y ser
// rechazado.
//
// Que el registro no conteste NO impide crear la sala. Es solo presentación:
// lo que se pierde es que la página muestre el nombre de la sala y quién se
// identifica como host, y lo que se gana al seguir sin él es que crear una
// sala no dependa de que un servidor esté vivo. En ese caso el ID se genera
// acá, con el mismo alfabeto, y la sala funciona igual.
func (s *Session) publishCard(ctx context.Context, nick domain.Nickname, roomName string) (domain.Room, [domain.CardKeyLen]byte, error) {
	var key [domain.CardKeyLen]byte

	card := domain.RoomCard{Host: nick, Room: domain.ClampRoomName(roomName)}
	sealed, key, err := domain.SealRoomCard(card, s.deps.Rand)
	if err != nil {
		return domain.Room{}, key, err
	}

	room, err := s.deps.Directory.Open(ctx, sealed)
	if err != nil {
		s.deps.Log.Warn("el registro del seed no respondió, la sala va sin tarjeta", "error", err)
		id, err := domain.NewInviteID(s.deps.Rand)
		if err != nil {
			return domain.Room{}, key, fmt.Errorf("generando un código sin registro: %w", err)
		}
		// Sin registro que conteste no hay forma de saber a qué seed pertenece
		// el código, y el por defecto es el único que el otro lado va a probar
		// con un ID pelado.
		return domain.Room{InviteID: id, Seed: domain.DefaultSeedHost}, key, nil
	}
	return room, key, nil
}

// configureAdapter sondea el MTU y aplica el estado del adaptador.
//
// Ni el sondeo ni la aplicación son fatales. Un MTU mal puesto degrada la
// partida y no impide entrar, y abortar la creación de la sala por no poder
// escribir una métrica dejaría al usuario sin nada en vez de con algo
// imperfecto. Los dos fallos quedan en el log, que es donde se diagnostica el
// "conecta pero el mundo no carga".
func (s *Session) configureAdapter(ctx context.Context) {
	if mtu, err := s.deps.NetCfg.ProbeMTU(ctx); err != nil {
		s.deps.Log.Warn("no se pudo sondear el MTU del camino", "error", err)
	} else {
		s.state.Net.MTU = mtu
	}
	want := domain.AdapterStateFor(s.state.LocalIP, s.state.Subnet, s.state.Net.MTU, s.state.Game)
	if err := s.deps.NetCfg.ApplyAdapter(ctx, want); err != nil {
		s.deps.Log.Warn("no se pudieron aplicar los ajustes del adaptador", "error", err)
	}
	// DirectPlay va aparte porque no es un ajuste del adaptador. Se apaga solo
	// cuando no hay juego, que es lo que hace que salir de la sala lo revierta
	// sin que nadie tenga que acordarse.
	if err := s.deps.NetCfg.SetDirectPlay(ctx, s.state.Game.Tweaks.DirectPlay); err != nil {
		s.deps.Log.Warn("no se pudo cambiar DirectPlay", "quería", s.state.Game.Tweaks.DirectPlay, "error", err)
	}
}
