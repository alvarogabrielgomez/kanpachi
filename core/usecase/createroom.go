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
// Los resultados van con NOMBRE para que el cierre diferido pueda leer el
// error. Sin eso, anotar el fallo en el diario obligaría a tocar cada uno de
// los catorce `return` de aquí abajo, y el que se olvidara sería el único que
// no dejaría rastro.
func (s *Session) CreateRoom(ctx context.Context, nick domain.Nickname, roomName string) (estado domain.RoomState, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.state.Conn.InRoom() {
		return domain.RoomState{}, ErrBusy
	}
	if nick.IsZero() {
		return domain.RoomState{}, domain.ErrNicknameEmpty
	}

	// A partir de acá la operación se puede cancelar desde la pantalla. Ver
	// [Session.begin]: el contexto de aquí abajo es el que corta el botón.
	ctx, soltar := s.begin(ctx)
	defer soltar()

	// El diario se abre ANTES de la primera transición, porque el primer paso
	// que puede fallar ya está dentro. Ver [Journal].
	s.deps.Progress.Begin("crear la sala")
	s.deps.Progress.Step(domain.ScopeDaemon, "recibida la orden de crear una sala")

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
		// Cancelar es una respuesta y no un fallo, y la pantalla necesita
		// distinguirlas para no pintar un aviso de error encima de un botón que
		// la persona acaba de pulsar. Se traduce acá, en el único sitio por el
		// que salen los catorce `return` de abajo.
		if !ok && ctx.Err() != nil {
			err = fmt.Errorf("%w mientras se creaba la sala", ErrCanceled)
		}
		// El diario se cierra SIEMPRE, y con el error si lo hubo: lo que la
		// pantalla enseña dentro de "ver detalles" son justo estos pasos.
		s.deps.Progress.End(err)
		if !ok {
			// **Con un contexto propio, VIVO.** Si esto salió por una
			// cancelación, el de arriba está cancelado y cada paso de la
			// limpieza devolvería "context canceled" sin llegar a tocar
			// Windows: cancelar dejaría abierto justo lo que promete cerrar.
			// Ver [cleanupContext].
			limpio, fin := cleanupContext(ctx)
			s.teardown(limpio)
			fin()
			_ = s.state.TransitionWithExit(domain.StateIdle, "falló la creación de la sala", domain.ExitFailed)
			// Se republica por lo mismo que en JoinRoom: quien llama descarta
			// el estado al recibir un error, así que si no se publica acá la
			// copia que lee Status se queda con la de antes del fallo.
			s.snapshot()
		}
	}()

	s.deps.Progress.Step(domain.ScopeDaemon, "leyendo la tabla de rutas para elegir un rango libre")
	plan, err := s.planSubnet(ctx)
	if err != nil {
		return domain.RoomState{}, err
	}
	s.deps.Progress.Stepf(domain.ScopeDaemon, "rango elegido para la sala: %s", plan.Subnet)

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
	s.deps.Progress.Step(domain.ScopeDaemon, "generada la identidad de la red, que nunca sale de esta máquina")

	room, key, sealed, err := s.publishCard(ctx, nick, roomName)
	if err != nil {
		return domain.RoomState{}, err
	}
	spec.Rendezvous = domain.DeriveRendezvous(room.InviteID)
	spec.Seeds = seedsFor(room)

	if err := s.state.Transition(domain.StateConnecting, "levantando la red"); err != nil {
		return domain.RoomState{}, err
	}
	s.deps.Progress.Step(domain.ScopeEngine, "levantando la red cifrada de la sala")
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
	// La clave y el blob se fijan JUNTOS, siempre. Es la invariante de los dos:
	// la clave persistida tiene que abrir el blob persistido, y separarlos deja
	// en disco un enlace que no descifra la tarjeta que el registro tiene.
	s.cardKey = key
	s.sealedCard = sealed
	// El registro acaba de aceptar la tarjeta, así que el reloj de la
	// republicación arranca acá y no en cero. Con el respaldo `sealed` viene
	// vacío, no hay nada publicado, y republicar no va a llamar a nadie.
	if len(sealed) > 0 {
		s.lastPublish = s.deps.Clock.Now()
	}
	s.nick = nick

	s.configureAdapter(ctx)

	// El host se queda TAMBIÉN en el vestíbulo mientras la sala esté abierta.
	// Es su puerta: el invitado no tiene forma de alcanzarlo en la red real
	// antes de tener la credencial, y la credencial es justo lo que va a pedir.
	s.deps.Progress.Step(domain.ScopeEngine, "abriendo el vestíbulo, que es por donde va a entrar quien tenga el código")
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
	s.deps.Progress.Step(domain.ScopeFirewall, "acotando la contención a la sala y al vestíbulo")
	if err := s.deps.Firewall.BindRoom(ctx, plan.Subnet, domain.BindRoomAndLobby); err != nil {
		return domain.RoomState{}, fmt.Errorf("acotando la contención a la sala: %w", err)
	}

	// El canal de control SOLO escucha en la máquina del host. Los invitados
	// marcan hacia afuera y no abren nada, así que su deny-all queda intacto.
	s.deps.Progress.Step(domain.ScopeDaemon, "abriendo el canal de la sala")
	scope, err := s.controlScope(ctx)
	if err != nil {
		return domain.RoomState{}, fmt.Errorf("preparando el alcance del canal de la sala: %w", err)
	}
	if err := s.deps.Control.Serve(ctx, scope); err != nil {
		return domain.RoomState{}, fmt.Errorf("abriendo el canal de la sala: %w", err)
	}
	s.deps.Progress.Step(domain.ScopeDaemon, "la sala está abierta")

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
//
// Devuelve también el blob SELLADO, y solo cuando el registro lo aceptó. Con el
// respaldo devuelve nada: ese invite ID no lo emitió el registro, así que no
// tiene ninguna tarjeta suya que restaurar, y guardar una haría que cada
// reapertura intentara republicarla y se llevara un "esa sala no existe".
func (s *Session) publishCard(ctx context.Context, nick domain.Nickname, roomName string) (domain.Room, [domain.CardKeyLen]byte, []byte, error) {
	var key [domain.CardKeyLen]byte

	card := domain.RoomCard{Host: nick, Room: domain.ClampRoomName(roomName)}
	sealed, key, err := domain.SealRoomCard(card, s.deps.Rand)
	if err != nil {
		return domain.Room{}, key, nil, err
	}
	s.deps.Progress.Step(domain.ScopeDaemon, "tarjeta de la sala sellada, con una clave que solo viaja en el enlace")

	s.deps.Progress.Stepf(domain.ScopeSeed, "pidiéndole un código al registro (%s)", domain.DefaultSeedHost)
	room, err := s.deps.Directory.Open(ctx, sealed)
	if err != nil {
		s.deps.Log.Warn("el registro del seed no respondió, la sala va sin tarjeta", "error", err)
		s.deps.Progress.Stepf(domain.ScopeSeed, "el registro no contestó (%v), se sigue con un código local", err)
		id, err := domain.NewInviteID(s.deps.Rand)
		if err != nil {
			return domain.Room{}, key, nil, fmt.Errorf("generando un código sin registro: %w", err)
		}
		s.deps.Progress.Stepf(domain.ScopeDaemon, "código generado aquí: %s, sin tarjeta ni página de invitación", id)
		// Sin registro que conteste no hay forma de saber a qué seed pertenece
		// el código, y el por defecto es el único que el otro lado va a probar
		// con un ID pelado.
		return domain.Room{InviteID: id, Seed: domain.DefaultSeedHost}, key, nil, nil
	}
	s.deps.Progress.Stepf(domain.ScopeSeed, "código reservado: %s", room.InviteID)
	return room, key, sealed, nil
}

// configureAdapter sondea el MTU y aplica el estado del adaptador.
//
// Ni el sondeo ni la aplicación son fatales. Un MTU mal puesto degrada la
// partida y no impide entrar, y abortar la creación de la sala por no poder
// escribir una métrica dejaría al usuario sin nada en vez de con algo
// imperfecto. Los dos fallos quedan en el log, que es donde se diagnostica el
// "conecta pero el mundo no carga".
func (s *Session) configureAdapter(ctx context.Context) {
	s.deps.Progress.Step(domain.ScopeNetwork, "sondeando el MTU del camino")
	if mtu, err := s.deps.NetCfg.ProbeMTU(ctx); err != nil {
		s.deps.Log.Warn("no se pudo sondear el MTU del camino", "error", err)
		s.deps.Progress.Stepf(domain.ScopeNetwork, "el sondeo del MTU falló (%v), se sigue con el valor por defecto", err)
	} else {
		s.state.Net.MTU = mtu
		s.deps.Progress.Stepf(domain.ScopeNetwork, "MTU del camino: %d", mtu)
	}
	want := domain.AdapterStateFor(s.state.LocalIP, s.state.Subnet, s.state.Net.MTU, s.state.Game)
	if err := s.deps.NetCfg.ApplyAdapter(ctx, want); err != nil {
		s.deps.Log.Warn("no se pudieron aplicar los ajustes del adaptador", "error", err)
		s.deps.Progress.Stepf(domain.ScopeNetwork, "no se pudieron aplicar los ajustes del adaptador (%v), la sala abre igual", err)
	} else {
		s.deps.Progress.Stepf(domain.ScopeNetwork, "adaptador configurado con %s", s.state.LocalIP)
	}
	// DirectPlay va aparte porque no es un ajuste del adaptador. Se apaga solo
	// cuando no hay juego, que es lo que hace que salir de la sala lo revierta
	// sin que nadie tenga que acordarse.
	if err := s.deps.NetCfg.SetDirectPlay(ctx, s.state.Game.Tweaks.DirectPlay); err != nil {
		s.deps.Log.Warn("no se pudo cambiar DirectPlay", "quería", s.state.Game.Tweaks.DirectPlay, "error", err)
	}
}
