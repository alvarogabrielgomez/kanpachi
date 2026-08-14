package usecase

import (
	"context"
	"errors"
	"fmt"
	"net/netip"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/core/port"
)

// JoinRoom entra a una sala ajena.
//
// Son dos redes y no una, y ese es el corazón de la decisión 2:
//
//  1. El VESTÍBULO, derivado del invite ID con Argon2id. Es público: cualquiera
//     con el código puede derivarlo, el seed incluido. Lo único que se hace ahí
//     es pedirle la credencial al host.
//  2. La red REAL, cuya identidad es aleatoria y no deriva de ningún string.
//     Se entra con la credencial, y el secreto de esa red no viaja nunca.
//
// Entrar depende de que el host esté alcanzable en ese instante. Es un costo
// aceptado y explícito: si está reconectando, el ingreso falla y hay que
// reintentar. Se gana poder revocar y renovar, se pierde robustez.
// Los resultados van con NOMBRE para que el cierre diferido pueda anotar el
// error en el diario sin tocar cada `return`. Mismo motivo que en CreateRoom.
func (s *Session) JoinRoom(ctx context.Context, input string, nick domain.Nickname) (estado domain.RoomState, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.state.Conn.InRoom() {
		return domain.RoomState{}, ErrBusy
	}
	if nick.IsZero() {
		return domain.RoomState{}, domain.ErrNicknameEmpty
	}

	// A partir de acá la operación se puede cancelar desde la pantalla. Ver
	// [Session.begin].
	ctx, soltar := s.begin(ctx)
	defer soltar()

	// El parseo es la frontera de entrada hostil del producto: acepta las seis
	// formas documentadas y rechaza entero cualquier otra cosa. Que ocurra
	// ANTES de la transición importa, porque un código mal pegado no tiene por
	// qué mover la máquina de estados.
	s.deps.Progress.Begin("entrar a la sala")
	s.deps.Progress.Step(domain.ScopeDaemon, "leyendo el código pegado")
	room, err := domain.ParseRoom(input)
	if err != nil {
		return domain.RoomState{}, err
	}
	s.deps.Progress.Stepf(domain.ScopeDaemon, "código válido: %s, servido por %s", room.InviteID, room.Seed)

	if err := s.state.Transition(domain.StateResolving, "el usuario pegó un código"); err != nil {
		return domain.RoomState{}, err
	}
	ok := false
	defer func() {
		// Igual que al crear: cancelar es una respuesta, no un fallo.
		if !ok && ctx.Err() != nil {
			err = fmt.Errorf("%w mientras se entraba a la sala", ErrCanceled)
		}
		s.deps.Progress.End(err)
		if !ok {
			// Contexto propio y vivo para deshacer. Ver [cleanupContext].
			limpio, fin := cleanupContext(ctx)
			s.teardown(limpio)
			fin()
			_ = s.state.TransitionWithExit(domain.StateIdle, "falló el ingreso a la sala", domain.ExitFailed)
			// Republicar no es opcional. Quien llama recibe un error y descarta
			// el estado, así que sin esto la copia que lee Status se quedaría
			// con la de antes del fallo y la pantalla de inicio no diría nunca
			// que el ingreso falló.
			s.snapshot()
		}
	}()

	// **El fallo temprano: preguntar si ese código existe antes de levantar
	// nada.**
	//
	// Sin esto, un código inventado o vencido se descubre al final: se levanta
	// el motor, se entra a un vestíbulo donde no espera nadie, y se agotan los
	// reintentos del canal de control. Es alrededor de un minuto de ruedita
	// para llegar a "no se pudo", cuando la respuesta se sabía en el primer
	// segundo. Y no es gratis: durante ese minuto hay una red virtual arriba y
	// reglas escritas por una sala que no existe.
	if err := s.checkRoomExists(ctx, room); err != nil {
		return domain.RoomState{}, err
	}

	if err := s.state.Transition(domain.StateConnecting, "buscando al host"); err != nil {
		return domain.RoomState{}, err
	}
	cred, err := s.joinRealNetworkLocked(ctx, room, nick)
	if err != nil {
		return domain.RoomState{}, err
	}

	s.state.Role = domain.RoleGuest
	s.state.Room = room
	s.state.LocalIP = cred.VirtualIP
	s.state.Subnet = cred.Subnet
	s.state.Net.Subnet = cred.Subnet
	s.state.Net.SubnetReason = "la subred la eligió el host de la sala"
	s.nick = nick

	// La conexión de control se rehace contra la dirección del host EN LA SALA.
	//
	// La del vestíbulo ya no sirve: se acaba de salir de esa red. Y no es un
	// detalle de limpieza, es de lo que dependen dos cosas de la decisión 20 y
	// la 23: que la caída de este socket sea lo que dice que el host no está, y
	// que el contador de veinte minutos tenga algo que contar. Sin rehacerla,
	// la presencia del host quedaría clavada en lo que se puso al entrar y el
	// contador no arrancaría nunca.
	if err := s.deps.Control.Dial(ctx, domain.HostAddress(cred.Subnet)); err != nil {
		// Se anota QUIÉN veía el motor en ese instante. La tabla OSPF sirve para
		// separar que la sala nunca formó una ruta de que la ruta existe pero su
		// plano de datos todavía no responde; no prueba que el SYN haya llegado.
		//
		// Se pregunta al motor y no a `s.state.Peers`, que en este punto sigue
		// teniendo lo de antes de entrar: los miembros se refrescan más abajo,
		// después de esta misma línea que falló.
		s.logMeshOnDialFailureLocked(ctx, domain.HostAddress(cred.Subnet))
		return domain.RoomState{}, fmt.Errorf(
			"se entró a la sala y el canal con el host no levantó: %w", err)
	}
	// El rol ya está fijado unas líneas más arriba, y NoteHostAlive lo exige:
	// solo un invitado registra pruebas de vida del host.
	s.state.NoteHostAlive(s.deps.Clock.Now())

	s.configureAdapter(ctx)

	// El invitado también acota la compuerta, y NO es un extra.
	//
	// `BuildRuleSet` le abre sus `ClientPorts`, o sea que un invitado también
	// escribe permisos, y también necesita quién los acote frente a los demás
	// miembros de la sala. Va con la SALA SOLA: el vestíbulo se soltó unas
	// líneas más arriba, a propósito, y ya no existe adaptador que acotar.
	if err := s.deps.Firewall.BindRoom(ctx, cred.Subnet, netip.Prefix{}, domain.BindRoomOnly); err != nil {
		return domain.RoomState{}, fmt.Errorf("acotando la contención a la sala: %w", err)
	}

	if err := s.refreshPeersLocked(ctx); err != nil {
		return domain.RoomState{}, err
	}
	if err := s.state.Transition(domain.StateConnected, "dentro de la sala"); err != nil {
		return domain.RoomState{}, err
	}

	// Un invitado en un juego de estrella no abre nada, que es la enorme
	// mayoría de los casos. Se aplica el conjunto igual, por lo mismo que al
	// crear: la cuarentena por defecto es un acto, no una omisión.
	if err := s.applyPolicy(ctx); err != nil {
		return domain.RoomState{}, err
	}

	// La última sala se guarda al entrar y no al salir, así una muerte sucia
	// del daemon tampoco la pierde.
	s.saveLastRoomLocked()

	ok = true
	s.deps.Log.Info("dentro de la sala",
		"código", room.InviteID.String(), "seed", room.Seed, "ip", cred.VirtualIP.String())
	return s.snapshot(), nil
}

// joinRealNetworkLocked es el canje por el vestíbulo y la entrada a la red real.
//
// Está aparte porque tiene DOS llamadores y las dos veces es exactamente lo
// mismo: entrar por primera vez, y volver a entrar solo cuando el host se cayó y
// volvió sin recordar a nadie. Ver [Session.Rejoin]. Dos copias de esta
// secuencia serían dos formas de entrar a una sala que se separan en silencio, y
// la parte que menos se puede duplicar es la validación de lo que manda el host.
//
// Lo que NO hace, y por eso sirve a los dos: no toca la máquina de estados, no
// fija el rol, no guarda nada en disco y no marca al canal de control. Cada
// llamador tiene su propia idea de qué significa haber llegado hasta acá.
//
// Asume el candado tomado.
func (s *Session) joinRealNetworkLocked(ctx context.Context, room domain.Room, nick domain.Nickname) (domain.Credential, error) {
	// Se deriva en el cliente y no se le pregunta al seed. El seed podría
	// decir cuál es la red de encuentro, derivarla acá hace que llegar al
	// vestíbulo no dependa de que su API esté viva ni de que diga la verdad.
	rdv := domain.DeriveRendezvous(room.InviteID)
	seeds := seedsFor(room)

	// La tabla de rutas se lee UNA vez y sirve a las dos cosas que dependen de
	// ella: avisar del conflicto de rango y esquivar las direcciones que esta
	// máquina ya tiene. Un fallo al leerla no impide entrar, solo deja el
	// diagnóstico sin datos, así que se sigue con la lista vacía.
	local, err := s.deps.Routes.LocalPrefixes(ctx)
	if err != nil {
		s.deps.Log.Warn("no se pudo leer la tabla de rutas antes del vestíbulo", "error", err)
		local = nil
	}
	// Antes de entrar, porque el paso siguiente es el que se cuelga treinta
	// segundos si esto pasa, y sin decirlo el síntoma es un adaptador que no toma
	// su dirección y ninguna pista de por qué.
	s.noteLobbyConflictLocked(domain.LobbyOverlap(local, rdv.LobbySubnet()))

	lobbyIP, err := domain.RendezvousGuestAddress(s.deps.Rand, rdv.LobbySubnet(), local)
	if err != nil {
		return domain.Credential{}, err
	}
	s.deps.Progress.Step(domain.ScopeEngine, "entrando al vestíbulo, que es donde espera el host")
	if err := s.deps.Engine.JoinRendezvous(ctx, domain.RendezvousSpec{
		Rendezvous: rdv, Address: lobbyIP, Name: nick, Seeds: seeds,
	}); err != nil {
		return domain.Credential{}, fmt.Errorf("entrando al vestíbulo de la sala: %w", err)
	}

	s.deps.Progress.Step(domain.ScopeDaemon, "pidiéndole al host la credencial de la sala")
	cred, err := s.exchangeForCredential(ctx, rdv, nick)
	if err != nil {
		return domain.Credential{}, err
	}
	s.deps.Progress.Stepf(domain.ScopeDaemon, "credencial recibida, tu dirección será %s", cred.VirtualIP)
	// La subred la eligió el host mirando SU máquina, no esta. Antes de
	// instalarla hay que comprobarla contra la tabla de rutas de acá: una sala
	// en 192.168.1.0/24 rompe la LAN de casa de quien tenga ese rango, y el
	// síntoma sería que entrar a la sala te deja sin internet.
	//
	// Se rechaza en vez de avisar por el mismo motivo por el que el plan de
	// direcciones prefiere fallar a forzar un rango: pisar una ruta existente
	// rompe conectividad que el usuario ya tenía, y eso es peor que no entrar.
	s.deps.Progress.Stepf(domain.ScopeDaemon, "comprobando que %s no pise ninguna red de esta máquina", cred.Subnet)
	if err := s.checkSubnetAgainstLocal(ctx, cred.Subnet); err != nil {
		return domain.Credential{}, err
	}

	// Salir del vestíbulo antes de entrar a la red real. No es higiene: el
	// vestíbulo es observable por cualquiera que tenga el código, y quedarse
	// ahí después de haber entrado mantendría abierta una vía por la que un
	// desconocido ve que esta máquina está en esa sala.
	if err := s.deps.Engine.LeaveRendezvous(ctx); err != nil {
		s.deps.Log.Warn("el motor no salió limpio del vestíbulo", "error", err)
	}
	s.deps.Progress.Step(domain.ScopeEngine, "saliendo del vestíbulo y entrando a la red real")
	if err := s.deps.Engine.JoinWithCredential(ctx, domain.GuestSpec{
		Credential: cred, Name: nick, Seeds: seeds,
	}); err != nil {
		return domain.Credential{}, fmt.Errorf("entrando a la sala: %w", err)
	}
	return cred, nil
}

// checkRoomExists le pregunta al registro si ese código existe, y no sigue sin
// una respuesta suya.
//
// # Las dos formas de parar, que dicen cosas distintas
//
// El registro contestando que ese invite ID no existe es [ErrNoSuchRoom]: hay
// respuesta, y la respuesta es que no. El registro no contestando es
// [ErrNoRegistry]: no se sabe nada. Las dos paran el ingreso, y se distinguen
// porque lo que hay que hacer es distinto, revisar el código contra volver a
// intentarlo en un rato.
//
// # Por qué también para cuando no contesta, que antes no lo hacía
//
// Esto seguía adelante ante cualquier fallo que no fuera un "no", con el
// argumento de que el registro es solo presentación y que la sala vive en las
// máquinas de sus miembros. La segunda mitad es cierta y la conclusión no: **el
// seed es el punto de encuentro**. Su máquina es la que viaja como par en la
// configuración del motor, así que sin ella el vestíbulo no se forma y el
// invitado se queda un minuto mirando una ruedita para terminar en un fallo
// genérico.
//
// La política del producto es decirlo en el primer segundo. Es la misma razón
// por la que el adaptador virtual se sondea al arrancar el daemon en vez de
// descubrirse al abrir la primera sala.
//
// # Lo que esto exige del registro, y por eso no se pudo hacer antes
//
// Que su "no" sea confiable. Con el almacén en RAM y `Restart=always`, un
// reinicio le hacía contestar que no conoce salas abiertas y alcanzables, y
// endurecer esto encima habría convertido una molestia en un muro. Por eso el
// registro persiste primero. Ver la cabecera de `registry/persist.go`.
//
// # Se le pregunta AL REGISTRO DEL CÓDIGO, y antes se le preguntaba al nuestro
//
// Porque **un invite ID solo significa algo en el registro que lo emitió.**
// Mientras el cliente hablaba con un registro fijo, un código de otro solo se
// podía comprobar preguntándole al equivocado, así que había una rama que se
// saltaba la comprobación entera y entraba a ciegas: sin fallo temprano y sin
// tarjeta, o sea el minuto de ruedita de vuelta para justo esos códigos.
//
// Con un cliente por seed esa rama desapareció. La contrapartida es explícita y
// está aceptada en la decisión 16: **pegar un código puede hacer que esta
// máquina le hable al servidor de un desconocido**, que ve su IP pública y el
// invite ID consultado. Jamás ve el secreto de la sala real, así que no puede
// unirse a ninguna, y el diálogo de confianza enseña a dónde se va antes de
// llegar acá.
func (s *Session) checkRoomExists(ctx context.Context, room domain.Room) error {
	dir, err := s.deps.Directories.For(room.Seed)
	if err != nil {
		s.deps.Log.Error("no se pudo abrir el registro del código", "seed", room.Seed, "error", err)
		s.deps.Progress.Stepf(domain.ScopeSeed, "el registro %s no se pudo usar", room.Seed)
		return fmt.Errorf("%w: %v", ErrNoRegistry, err)
	}

	s.deps.Progress.Stepf(domain.ScopeSeed, "preguntándole a %s si ese código existe", room.Seed)
	vista, err := dir.Lookup(ctx, room.InviteID)
	switch {
	case err == nil:
		// **Una tarjeta falsificada NO detiene la entrada, y se dice por qué.**
		// La tarjeta es presentación: el nombre de la sala y el nick de quien
		// invita. A qué sala se entra lo deciden el invite ID y el vestíbulo, y
		// ninguno de los dos sale de acá. Pararse en este punto sería bloquear
		// una entrada por un dato decorativo, que es justo el reflejo que la
		// decisión 25 rechaza para la huella cambiada.
		//
		// Lo que sí significa es que ESE registro está sirviendo algo que la
		// llave que él mismo fijó no respalda, o sea que está comprometido. Queda
		// en el log con esas palabras, y quien lo enseña en pantalla es la
		// pantalla del enlace, que es donde alguien todavía puede decidir.
		if vista.Trust() == domain.CardForged {
			s.deps.Log.Error("el registro sirve una tarjeta que su propia llave fijada no respalda",
				"código", room.InviteID.String(), "seed", room.Seed)
			s.deps.Progress.Step(domain.ScopeSeed, "ojo: la tarjeta de esa sala no valida contra la llave del registro")
		}
		s.deps.Progress.Step(domain.ScopeSeed, "el código existe, se sigue")
		return nil

	case errors.Is(err, port.ErrUnknownRoom):
		// La respuesta. Se para acá, con el motor todavía sin arrancar y sin
		// una sola regla escrita.
		s.deps.Progress.Step(domain.ScopeSeed, "el registro dice que ese código no existe")
		return fmt.Errorf("%w: %s", ErrNoSuchRoom, room.InviteID)

	default:
		// Ausencia de información, y se para igual. Seguir a ciegas cuesta un
		// minuto de ruedita contra un vestíbulo que no se va a formar, porque la
		// máquina del registro es también la que hace de punto de encuentro.
		s.deps.Log.Error("el registro no contestó, así que no se entra",
			"código", room.InviteID.String(), "error", err)
		s.deps.Progress.Step(domain.ScopeSeed, "el registro no contestó")
		return fmt.Errorf("%w: %v.\n\n"+
			"Es la máquina por la que las salas se encuentran, así que sin ella no hay "+
			"a dónde llegar.\n\n"+
			"Qué hacer: vuelve a intentarlo en un momento", ErrNoRegistry, err)
	}
}

// exchangeForCredential es el canje del paso 5 y 6 del flujo de conexión.
//
// Va por el canal de control, que solo escucha en la máquina del host. Lo que
// se intercambia acá va firmado contra la llave del host y cifrado contra la
// del invitado, porque el vestíbulo es observable: un observador ve que
// alguien pidió entrar, y no obtiene la credencial ni la identidad de la sala.
func (s *Session) exchangeForCredential(
	ctx context.Context, rdv domain.Rendezvous, nick domain.Nickname,
) (domain.Credential, error) {
	// La dirección del host en el vestíbulo la derivan los dos lados del mismo
	// código, sin hablarse, que es justo lo que hace falta acá: la subred de la
	// sala llega DENTRO de la credencial, o sea después de esta conexión.
	//
	// Marcar hacia una dirección conocida, y no aceptar conexiones entrantes,
	// es lo que hace imposible que un miembro se haga pasar por el host.
	if err := s.deps.Control.Dial(ctx, rdv.LobbyHostAddress()); err != nil {
		// El host no está alcanzable. Es el costo aceptado de la decisión 2 y
		// merece un mensaje propio, porque "no se pudo conectar" mandaría a
		// alguien a revisar su internet cuando el problema es que el host está
		// reconectando.
		return domain.Credential{}, fmt.Errorf("el host no respondió, puede estar reconectando: %w", err)
	}
	cred, err := s.deps.Control.RequestCredential(ctx, domain.CredentialRequest{Name: nick})
	if err != nil {
		return domain.Credential{}, fmt.Errorf("el host no emitió la credencial: %w", err)
	}
	// Todo lo que sigue revisa un valor que llegó de OTRA MÁQUINA. Que venga
	// autenticado prueba quién lo escribió, no que lo que escribió sea
	// coherente, y un host modificado es el escenario que hay que suponer.
	switch {
	case cred.Token == "" || !cred.VirtualIP.IsValid() || !cred.Subnet.IsValid():
		// Una credencial a medias dejaría al motor arrancando con una IP
		// inválida, y el fallo aparecería tres capas más abajo sin decir de
		// dónde vino.
		return domain.Credential{}, fmt.Errorf("la credencial que emitió el host está incompleta")

	case !cred.Subnet.Contains(cred.VirtualIP):
		// Con la IP fuera de su propia subred, las reglas de firewall quedan
		// ancladas a una dirección que el adaptador no tiene, y el resultado es
		// una sala que conecta y en la que nada llega.
		return domain.Credential{}, fmt.Errorf(
			"el host asignó %s, que está fuera de la subred %s que él mismo declaró", cred.VirtualIP, cred.Subnet)

	case domain.LobbySpace.Overlaps(cred.Subnet):
		// Poner la sala dentro del espacio de los vestíbulos cortaría la conexión
		// por la que acaba de llegar esta credencial. Se comprueba contra el
		// espacio entero y no contra el /24 de ESTA sala, que es más estricto y
		// correcto: ahí no vive ninguna sala legítima, y un host modificado que
		// eligiera el vestíbulo de otra sala tampoco tendría por qué.
		return domain.Credential{}, fmt.Errorf("el host puso la sala en el rango reservado del vestíbulo")

	case cred.VirtualIP == domain.HostAddress(cred.Subnet):
		// La .1 es del host y es la dirección a la que marcan los invitados.
		// Dársela a uno haría que se mande mensajes a sí mismo creyendo que
		// habla con el host.
		return domain.Credential{}, fmt.Errorf("el host asignó su propia dirección, %s", cred.VirtualIP)
	}
	return cred, nil
}

// logMeshOnDialFailureLocked anota quién veía el motor cuando el marcado al
// host falló.
//
// **No devuelve error y no puede cambiar nada.** Es lo último que se hace antes
// de deshacer un ingreso que ya falló, y un fallo acá solo puede empeorar el
// mensaje que el usuario va a recibir.
//
// Asume el candado tomado.
func (s *Session) logMeshOnDialFailureLocked(ctx context.Context, host netip.Addr) {
	peers, err := s.deps.Engine.Peers(ctx)
	if err != nil {
		s.deps.Log.Warn("no se pudo preguntar al motor quién hay en la sala justo tras fallar el canal",
			"error", err)
		return
	}

	var hostPeer *domain.Peer
	for _, p := range peers {
		if p.Self {
			continue
		}
		s.deps.Log.Info("  la red de la sala tenía a este par al fallar el canal",
			"ip", p.VirtualIP.String(), "nombre", p.Name.String(),
			"camino", p.Path.String(), "rtt", p.RTT.String(), "host", p.Host)
		if p.VirtualIP == host {
			peer := p
			hostPeer = &peer
		}
	}
	if hostPeer != nil {
		s.deps.Log.Warn("el motor conoce una ruta al host, pero el canal no levantó: "+
			"la ruta no prueba que el plano de datos haya entregado el SYN; revisar la sesión relay, el listener y el firewall",
			"host", host.String(), "camino", hostPeer.Path.String(), "rtt", hostPeer.RTT.String())
		if hostPeer.RTT <= 0 {
			s.deps.Log.Warn("el rtt del host es 0: todavía no hay una medición de ida y vuelta; "+
				"la ruta OSPF puede existir mientras el handshake del relay sigue sin completar",
				"host", host.String())
		}
		return
	}
	s.deps.Log.Warn("el motor NO veía al host: la red cifrada de la sala no llegó a "+
		"formarse, y el firewall no tiene nada que ver con esto",
		"host", host.String(), "pares-vistos", len(peers))
}

// checkSubnetAgainstLocal rechaza una subred de sala que pise algo de esta
// máquina.
func (s *Session) checkSubnetAgainstLocal(ctx context.Context, subnet netip.Prefix) error {
	local, err := s.deps.Routes.LocalPrefixes(ctx)
	if err != nil {
		// Sin la tabla no se puede comprobar, y entrar a ciegas es justo el
		// riesgo que esta función existe para evitar.
		return fmt.Errorf("leyendo la tabla de rutas antes de entrar: %w", err)
	}
	for _, p := range local {
		if p.IsValid() && p.Addr().Is4() && p.Overlaps(subnet) {
			return fmt.Errorf(
				"la sala usa %s y esta máquina ya tiene %s: entrar te dejaría sin esa red", subnet, p)
		}
	}
	return nil
}

// refreshPeersLocked relee la lista de miembros. Asume el candado tomado.
func (s *Session) refreshPeersLocked(ctx context.Context) error {
	peers, err := s.deps.Engine.Peers(ctx)
	if err != nil {
		return fmt.Errorf("consultando los miembros de la sala: %w", err)
	}
	s.state.Peers = s.withAdmittedLocked(
		markRoles(peers, s.state.LocalIP, s.state.Role, s.state.Subnet))
	// La calidad de la conexión sale de esta tabla y de nada más. Va ANTES de
	// deducir la presencia del host, que exige un estado concreto para no
	// dispararse a mitad de un ingreso.
	s.rederiveConnLocked()
	s.inferHostPresenceLocked()
	return nil
}

// inferHostPresenceLocked deduce la presencia del host de la tabla de peers.
//
// Es la capa que sigue funcionando cuando el canal de control está roto,
// colgado o nunca arrancó: el motor propaga la tabla entera a cada nodo, así
// que la .1 del host está o no está sin que el canal opine. Cuesta cero
// llamadas nuevas, porque la lista ya se acaba de leer.
//
// **Solo puede APAGAR la presencia, jamás encenderla.** La asimetría es el
// punto entero: que el motor reporte al host prueba que su nodo está en la red,
// y no prueba que su canal de control funcione. Encenderla desde acá desarmaría
// el contador de veinte minutos con evidencia que no lo respalda, y el caso
// real que rompería es el host que dejó la máquina encendida con Kanpachi
// colgado.
//
// Los dos guardas evitan el falso positivo. Una lista vacía es que el motor no
// tiene nada que decir, no que el host se haya ido. Y exigir que la conexión
// esté establecida impide que dispare a mitad de un ingreso, cuando todavía no
// hay a quién ver.
//
// **Establecida incluye DEGRADADO,** y escribirlo como `== StateConnected` era
// un fallo: una sala degradada tiene túnel y tiene tabla de miembros, así que
// esto vale ahí exactamente igual. Con el pestillo de degradado que se midió el
// 2026-08-05, un invitado se quedaba en degradado para siempre tras cualquier
// corte de red, y con eso esta capa entera se apagaba sin que nada lo dijera:
// justo la que existe para cuando el canal de control ya no sirve.
//
// Asume el candado tomado.
func (s *Session) inferHostPresenceLocked() {
	if s.state.Role != domain.RoleGuest || !s.state.Conn.Established() || len(s.state.Peers) == 0 {
		return
	}
	hostIP := domain.HostAddress(s.state.Subnet)
	for _, p := range s.state.Peers {
		if p.VirtualIP == hostIP {
			return
		}
	}
	if s.state.HostPresent {
		s.deps.Log.Info("el host no está en la tabla de miembros del motor", "esperada", hostIP.String())
	}
	s.state.SetHostPresent(false, s.deps.Clock.Now())
}

// markRoles decide cuál de los peers soy yo y cuál hospeda.
//
// Las dos marcas se ponen ACÁ y no se le creen al motor, por motivos
// distintos y los dos importan:
//
//   - "yo" el motor no lo sabe: reporta la propia máquina como un peer más.
//     Sin esta comparación la UI no podría poner "(tú)" y expulsarse a uno
//     mismo sería una operación válida.
//   - "host" el motor NO PUEDE saberlo: es un concepto del producto, no de la
//     red. Sale del rol propio, que fijó CreateRoom o JoinRoom y no cambia en
//     toda la vida de la sala, y de que el host toma la .1 de la subred por
//     convención. Creérselo a un peer sería dejar que cualquiera se declare
//     host en la lista de los demás.
func markRoles(peers []domain.Peer, local netip.Addr, role domain.Role, subnet netip.Prefix) []domain.Peer {
	hostIP := domain.HostAddress(subnet)

	out := make([]domain.Peer, 0, len(peers))
	for _, p := range peers {
		p.Self = p.VirtualIP == local
		if p.Self {
			p.Path = domain.PathSelf
			p.Host = role == domain.RoleHost
		} else {
			p.Host = role == domain.RoleGuest && p.VirtualIP == hostIP
		}
		out = append(out, p)
	}
	return out
}
