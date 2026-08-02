package usecase

import (
	"context"
	"fmt"
	"net/netip"

	"github.com/accentiostudios/kanpachi/core/domain"
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
func (s *Session) JoinRoom(ctx context.Context, input string, nick domain.Nickname) (domain.RoomState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.state.Conn.InRoom() {
		return domain.RoomState{}, ErrBusy
	}
	if nick.IsZero() {
		return domain.RoomState{}, domain.ErrNicknameEmpty
	}

	// El parseo es la frontera de entrada hostil del producto: acepta las seis
	// formas documentadas y rechaza entero cualquier otra cosa. Que ocurra
	// ANTES de la transición importa, porque un código mal pegado no tiene por
	// qué mover la máquina de estados.
	room, err := domain.ParseRoom(input)
	if err != nil {
		return domain.RoomState{}, err
	}

	if err := s.state.Transition(domain.StateResolving, "el usuario pegó un código"); err != nil {
		return domain.RoomState{}, err
	}
	ok := false
	defer func() {
		if !ok {
			s.teardown(ctx)
			_ = s.state.TransitionWithExit(domain.StateIdle, "falló el ingreso a la sala", domain.ExitFailed)
		}
	}()

	// Se deriva en el cliente y no se le pregunta al seed. El seed podría
	// decir cuál es la red de encuentro, derivarla acá hace que llegar al
	// vestíbulo no dependa de que su API esté viva ni de que diga la verdad.
	rdv := domain.DeriveRendezvous(room.InviteID)
	seeds := seedsFor(room)

	if err := s.state.Transition(domain.StateConnecting, "buscando al host"); err != nil {
		return domain.RoomState{}, err
	}
	lobbyIP, err := domain.RendezvousGuestAddress(s.deps.Rand)
	if err != nil {
		return domain.RoomState{}, err
	}
	if err := s.deps.Engine.JoinRendezvous(ctx, domain.RendezvousSpec{
		Rendezvous: rdv, Address: lobbyIP, Name: nick, Seeds: seeds,
	}); err != nil {
		return domain.RoomState{}, fmt.Errorf("entrando al vestíbulo de la sala: %w", err)
	}

	cred, err := s.exchangeForCredential(ctx, nick)
	if err != nil {
		return domain.RoomState{}, err
	}
	// La subred la eligió el host mirando SU máquina, no esta. Antes de
	// instalarla hay que comprobarla contra la tabla de rutas de acá: una sala
	// en 192.168.1.0/24 rompe la LAN de casa de quien tenga ese rango, y el
	// síntoma sería que entrar a la sala te deja sin internet.
	//
	// Se rechaza en vez de avisar por el mismo motivo por el que el plan de
	// direcciones prefiere fallar a forzar un rango: pisar una ruta existente
	// rompe conectividad que el usuario ya tenía, y eso es peor que no entrar.
	if err := s.checkSubnetAgainstLocal(ctx, cred.Subnet); err != nil {
		return domain.RoomState{}, err
	}

	// Salir del vestíbulo antes de entrar a la red real. No es higiene: el
	// vestíbulo es observable por cualquiera que tenga el código, y quedarse
	// ahí después de haber entrado mantendría abierta una vía por la que un
	// desconocido ve que esta máquina está en esa sala.
	if err := s.deps.Engine.LeaveRendezvous(ctx); err != nil {
		s.deps.Log.Warn("el motor no salió limpio del vestíbulo", "error", err)
	}
	if err := s.deps.Engine.JoinWithCredential(ctx, domain.GuestSpec{
		Credential: cred, Name: nick, Seeds: seeds,
	}); err != nil {
		return domain.RoomState{}, fmt.Errorf("entrando a la sala: %w", err)
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
		return domain.RoomState{}, fmt.Errorf(
			"se entró a la sala y el canal con el host no levantó: %w", err)
	}
	s.state.SetHostPresent(true, s.deps.Clock.Now())

	s.configureAdapter(ctx)

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

	ok = true
	s.deps.Log.Info("dentro de la sala",
		"código", room.InviteID.String(), "seed", room.Seed, "ip", cred.VirtualIP.String())
	return s.snapshot(), nil
}

// exchangeForCredential es el canje del paso 5 y 6 del flujo de conexión.
//
// Va por el canal de control, que solo escucha en la máquina del host. Lo que
// se intercambia acá va firmado contra la llave del host y cifrado contra la
// del invitado, porque el vestíbulo es observable: un observador ve que
// alguien pidió entrar, y no obtiene la credencial ni la identidad de la sala.
func (s *Session) exchangeForCredential(ctx context.Context, nick domain.Nickname) (domain.Credential, error) {
	// La dirección del host en el vestíbulo es fija y la conocen los dos lados
	// sin hablarse, que es justo lo que hace falta acá: la subred de la sala
	// llega DENTRO de la credencial, o sea después de esta conexión.
	//
	// Marcar hacia una dirección conocida, y no aceptar conexiones entrantes,
	// es lo que hace imposible que un miembro se haga pasar por el host.
	if err := s.deps.Control.Dial(ctx, domain.RendezvousHostAddress); err != nil {
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

	case cred.Subnet == domain.RendezvousSubnet:
		// Poner la sala en el /24 del vestíbulo cortaría la conexión por la que
		// acaba de llegar esta credencial.
		return domain.Credential{}, fmt.Errorf("el host puso la sala en el rango reservado del vestíbulo")

	case cred.VirtualIP == domain.HostAddress(cred.Subnet):
		// La .1 es del host y es la dirección a la que marcan los invitados.
		// Dársela a uno haría que se mande mensajes a sí mismo creyendo que
		// habla con el host.
		return domain.Credential{}, fmt.Errorf("el host asignó su propia dirección, %s", cred.VirtualIP)
	}
	return cred, nil
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
	s.state.Peers = markRoles(peers, s.state.LocalIP, s.state.Role, s.state.Subnet)
	return nil
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
