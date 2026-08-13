package control

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/daemon/transport/wire"
)

// Serve arranca el oyente del host, con los dos alcances de la decisión 23.
//
// Llamarlo otra vez REEMPLAZA el alcance, y eso es lo que hace que expulsar a
// alguien lo saque también de la lista de quién puede hablar: las conexiones de
// las IPs que ya no están entre los miembros se cierran en el acto, sin esperar
// a que el otro lado se dé por enterado.
//
// La puerta NO se recorta al expulsar, y es deliberado: expulsar y bloquear son
// cosas distintas, y quien fue expulsado puede volver a tocar hasta que el host
// renueve el código. Ver decisión 22.
func (c *Channel) Serve(ctx context.Context, scope domain.ControlScope) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.issuer == nil {
		return ErrNotAttached
	}
	if c.srv == nil {
		srv, err := newServer(ctx, c, scope)
		if err != nil {
			return err
		}
		c.srv = srv
		return nil
	}
	return c.srv.rescope(ctx, scope)
}

// server es el lado del host.
type server struct {
	ch     *Channel
	issuer Issuer

	ctx    context.Context
	cancel context.CancelFunc

	mu      sync.Mutex
	scope   domain.ControlScope
	members map[netip.Addr]bool
	// keys es la LIBRETA de llaves de sesión, por dirección virtual.
	//
	// Vive en memoria y muere con la sala. Es la llave con la que se sella el
	// código nuevo, y se guarda contra la dirección que el host mismo acaba de
	// asignar, no contra nada que el otro lado haya elegido.
	keys map[netip.Addr][]byte
	// conns es UNA conexión viva por dirección. La segunda desplaza a la
	// primera, así que un miembro no puede agotar descriptores abriendo mil.
	conns     map[netip.Addr]*peerConn
	doorConns int

	seq  uint64
	acks map[uint64]chan struct{}

	lobbyLn net.Listener
	roomLn  net.Listener
	lobbyAt netip.AddrPort
	roomAt  netip.AddrPort
}

func newServer(ctx context.Context, ch *Channel, scope domain.ControlScope) (*server, error) {
	hijo, cancel := context.WithCancel(context.WithoutCancel(ctx))
	s := &server{
		ch:      ch,
		issuer:  ch.issuer,
		ctx:     hijo,
		cancel:  cancel,
		members: map[netip.Addr]bool{},
		keys:    map[netip.Addr][]byte{},
		conns:   map[netip.Addr]*peerConn{},
		acks:    map[uint64]chan struct{}{},
	}
	if err := s.rescope(ctx, scope); err != nil {
		s.stop()
		return nil, err
	}
	return s, nil
}

// rescope reemplaza el alcance y levanta o cambia los oyentes que hagan falta.
func (s *server) rescope(ctx context.Context, scope domain.ControlScope) error {
	if err := s.relisten(ctx, scope); err != nil {
		return err
	}

	s.mu.Lock()
	s.scope = scope
	s.members = make(map[netip.Addr]bool, len(scope.Members))
	for _, m := range scope.Members {
		if m.IsValid() {
			s.members[m] = true
		}
	}
	sobran := make([]*peerConn, 0)
	for ip, pc := range s.conns {
		if !s.members[ip] {
			sobran = append(sobran, pc)
			delete(s.conns, ip)
			delete(s.keys, ip)
		}
	}
	s.mu.Unlock()

	for _, pc := range sobran {
		s.ch.log().Info("se cierra el canal de quien ya no es miembro", "ip", pc.addr.String())
		pc.close()
	}
	return nil
}

// relisten levanta los oyentes que faltan y baja los que cambiaron de
// dirección.
func (s *server) relisten(ctx context.Context, scope domain.ControlScope) error {
	lobby := puerto(scope.Lobby)
	room := puerto(scope.Room)

	s.mu.Lock()
	lobbyIgual := s.lobbyLn != nil && s.lobbyAt == lobby
	roomIgual := s.roomLn != nil && s.roomAt == room
	s.mu.Unlock()

	if !lobbyIgual && lobby.IsValid() {
		ln, err := s.ch.deps.Listen(ctx, lobby)
		if err != nil {
			return err
		}
		s.swapListener(&s.lobbyLn, &s.lobbyAt, ln, lobby)
		go s.accept(ln, true)
	}
	if !roomIgual && room.IsValid() {
		ln, err := s.ch.deps.Listen(ctx, room)
		if err != nil {
			return err
		}
		s.swapListener(&s.roomLn, &s.roomAt, ln, room)
		go s.accept(ln, false)
	}
	return nil
}

func (s *server) swapListener(dst *net.Listener, at *netip.AddrPort, ln net.Listener, nueva netip.AddrPort) {
	s.mu.Lock()
	viejo := *dst
	*dst, *at = ln, nueva
	s.mu.Unlock()
	if viejo != nil {
		_ = viejo.Close()
	}
}

func puerto(a netip.Addr) netip.AddrPort {
	if !a.IsValid() {
		return netip.AddrPort{}
	}
	return netip.AddrPortFrom(a, domain.ControlPort)
}

// accept atiende un oyente hasta que se cierre.
func (s *server) accept(ln net.Listener, esPuerta bool) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return // el oyente se cerró, que es la forma normal de terminar
		}
		if s.ctx.Err() != nil {
			_ = conn.Close()
			return
		}
		if esPuerta {
			go s.serveDoor(conn)
			continue
		}
		go s.serveRoom(conn)
	}
}

// serveDoor atiende UNA conexión de la puerta: un pedido de credencial y se
// cierra.
//
// Una por conexión y no varias, y eso es lo que impide que la puerta acumule
// nada: quien entró ya tiene lo que vino a buscar, y quien no habla se va por el
// plazo. La puerta acepta a cualquiera que haya llegado al vestíbulo, así que lo
// único que la acota es que ahí no se pueda pedir otra cosa.
func (s *server) serveDoor(conn net.Conn) {
	defer conn.Close()

	s.mu.Lock()
	if s.doorConns >= maxDoorConns {
		s.mu.Unlock()
		s.ch.log().Warn("la puerta está llena, se rechaza una conexión", "tope", maxDoorConns)
		return
	}
	s.doorConns++
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.doorConns--
		s.mu.Unlock()
	}()

	_ = conn.SetReadDeadline(s.ch.now().Add(doorHelloWait))
	linea, err := wire.NewReader(conn, MaxMessage).ReadLine()
	if err != nil {
		return
	}
	e, err := decodeEnvelope(linea)
	if err != nil {
		return
	}
	if !admitidoEnLaPuerta[e.Kind] {
		// No se interpreta, no se registra el contenido y no se contesta. Por la
		// puerta se pide una cosa, y quien pide otra no merece saber cuál.
		s.ch.log().Warn("mensaje descartado en la puerta", "tipo", string(e.Kind))
		return
	}

	req, err := payloadOf[credentialRequestMsg](e)
	if err != nil {
		return
	}
	_ = conn.SetReadDeadline(time.Time{})
	resp := s.issue(req)

	sobre, err := wrap(KindCredentialResponse, resp)
	if err != nil {
		return
	}
	_ = wire.NewWriter(conn, MaxMessage).Write(sobre)
}

// issue emite la credencial y la sella contra la llave del que entra.
func (s *server) issue(req credentialRequestMsg) credentialResponseMsg {
	nick, err := domain.ParseNickname(req.Name)
	if err != nil {
		return credentialResponseMsg{Error: "el nombre no vale: " + err.Error()}
	}
	if len(req.PublicKey) != keyLen {
		return credentialResponseMsg{Error: "falta la llave con la que sellar la respuesta"}
	}

	cred, err := s.issuer.IssueCredentialFor(s.ctx, domain.CredentialRequest{
		Name: nick, PublicKey: req.PublicKey,
	})
	if err != nil {
		s.ch.log().Warn("no se emitió la credencial", "nombre", nick.String(), "error", err)
		return credentialResponseMsg{Error: err.Error()}
	}

	plano, err := jsonBytes(credentialToWire(cred))
	if err != nil {
		return credentialResponseMsg{Error: "no se pudo preparar la credencial"}
	}
	sellado, err := seal(req.PublicKey, plano)
	if err != nil {
		return credentialResponseMsg{Error: "no se pudo sellar la credencial"}
	}

	// La llave se guarda contra la dirección que ESTE lado acaba de asignar, y
	// no contra nada que el otro haya dicho. Es con la que después se sella el
	// código nuevo si el host lo renueva.
	s.mu.Lock()
	s.keys[cred.VirtualIP] = req.PublicKey
	s.mu.Unlock()

	return credentialResponseMsg{Sealed: sellado}
}

// serveRoom atiende la conexión de un miembro.
//
// El rechazo por dirección ocurre ANTES de leer un byte: si quien marcó no está
// entre los miembros presentes, la conexión se cierra sin haber interpretado
// nada suyo.
func (s *server) serveRoom(conn net.Conn) {
	ip, ok := addrOf(conn)
	if !ok {
		_ = conn.Close()
		return
	}

	s.mu.Lock()
	admitido := s.members[ip]
	s.mu.Unlock()
	if !admitido {
		s.ch.log().Warn("conexión al canal de la sala desde quien no es miembro", "ip", ip.String())
		_ = conn.Close()
		return
	}

	pc := &peerConn{addr: ip, conn: conn, ch: s.ch, w: wire.NewWriter(conn, MaxMessage)}

	s.mu.Lock()
	anterior := s.conns[ip]
	s.conns[ip] = pc
	s.mu.Unlock()
	if anterior != nil {
		// Reconectar es normal: al motor se le cae un enlace y el cliente vuelve.
		// La vieja se cierra para que no queden dos.
		anterior.close()
	}

	// Y se avisa de que ahora sí hay por dónde hablarle. Ver
	// [Channel.MemberChannels]: es el instante que nadie más conoce.
	//
	// Sin bloquear, como todo lo que sale de acá: el canal está amortiguado y si
	// estuviera lleno, perder el aviso cuesta que el reintento periódico lo haga
	// más tarde. Bloquear costaría este miembro sin servir.
	select {
	case s.ch.joins <- ip:
	default:
	}

	s.read(pc)
}

// read consume lo que manda un miembro, que es una sola cosa.
func (s *server) read(pc *peerConn) {
	defer func() {
		pc.close()
		s.mu.Lock()
		if s.conns[pc.addr] == pc {
			delete(s.conns, pc.addr)
		}
		s.mu.Unlock()
	}()

	r := wire.NewReader(pc.conn, MaxMessage)
	for {
		linea, err := r.ReadLine()
		if err != nil {
			if errors.Is(err, wire.ErrTooLarge) {
				s.ch.log().Warn("un miembro mandó un mensaje que pasa del tope, se corta", "ip", pc.addr.String())
			}
			return
		}
		e, err := decodeEnvelope(linea)
		if err != nil {
			return
		}
		if !admitidoEnLaSala[e.Kind] {
			// Un anuncio o un aviso que llegue de un miembro se descarta sin
			// interpretarse: un host que los tomara le dejaría a un cliente
			// modificado cambiar el juego activo en la máquina donde se abren
			// los puertos.
			s.ch.log().Warn("mensaje descartado en la sala", "tipo", string(e.Kind), "ip", pc.addr.String())
			continue
		}
		switch e.Kind {
		case KindAck:
			ack, err := payloadOf[ackMsg](e)
			if err != nil {
				return
			}
			s.deliverAck(ack.Seq)

		case KindCanaryReport:
			msg, err := payloadOf[canaryReportMsg](e)
			if err != nil {
				return
			}
			// El remitente sale de la CONEXIÓN, `pc.addr`, y jamás del mensaje.
			// Con un campo de remitente, un miembro podría informar en nombre
			// de otro y el host no tendría cómo saber quién midió.
			emitir(s.ch, s.ch.canaryReports, canaryReportFromWire(pc.addr, msg), "informe del canario")
		}
	}
}

// RequestCanary le pide a UN miembro que marque al canario. SOLO el host.
//
// # Por qué a uno y no a todos
//
// Porque el canario es un socket abierto con plazo, y pedírselo a la sala entera
// multiplicaría los que marcan sin agregar información: el hecho que decide es
// si el paquete cruzó, y con uno que cruce ya está. Preguntarle a varios sirve
// para otra cosa, para acotar la mentira, y eso es política y vive en el caso de
// uso, que llama a esto una vez por cada uno.
//
// # Lo que NO se le manda
//
// La dirección. Ver [canaryRequestMsg]: el invitado marca a la conexión que ya
// tiene abierta contra este host, y no hay forma de pedirle otro destino.
func (c *Channel) RequestCanary(ctx context.Context, to netip.Addr, req domain.CanaryRequest) error {
	s, err := c.host()
	if err != nil {
		return err
	}
	if !to.IsValid() {
		// Sin destino no se difunde: un pedido a todos abriría tantos sondeos
		// como miembros haya contra un canario que espera uno.
		return fmt.Errorf("hay que decir a quién pedírselo")
	}
	if req.Port == 0 {
		return fmt.Errorf("el canario no dio puerto")
	}

	pc, ok := s.peer(to)
	if !ok {
		return fmt.Errorf("no hay canal abierto con %s", to)
	}
	sobre, err := wrap(KindCanaryRequest, canaryRequestMsg{
		Port:  req.Port,
		Nonce: req.Nonce[:],
	})
	if err != nil {
		return err
	}
	return pc.write(sobre)
}

func (s *server) deliverAck(seq uint64) {
	s.mu.Lock()
	ch := s.acks[seq]
	s.mu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- struct{}{}:
	default:
	}
}

// Announce les cuenta a los presentes cómo se llama la sala y qué juego está
// activo. Sin acuse: perderlo cuesta que una pantalla tarde dos minutos en
// ponerse al día, y el siguiente anuncio lo arregla.
func (c *Channel) Announce(_ context.Context, a domain.RoomAnnounce) error {
	s, err := c.host()
	if err != nil {
		return err
	}
	sobre, err := wrap(KindAnnounce, announceMsg{RoomName: a.RoomName, GameID: a.GameID})
	if err != nil {
		return err
	}
	for _, pc := range s.peers() {
		if err := pc.write(sobre); err != nil {
			c.log().Warn("no se pudo anunciar a un miembro", "ip", pc.addr.String(), "error", err)
		}
	}
	return nil
}

// Notify le manda un aviso a un miembro, o a todos si la dirección es cero.
//
// **Espera el acuse, con tope.** El canal es TCP, así que la orden se
// retransmite hasta que el otro lado la reconoce, y eso es lo que hace que el
// expulsado se desconecte solo y limpio. Sin ninguna espera queda una ventana
// real: escribir devuelve cuando los bytes entraron al búfer local, y un segundo
// después la revocación mata la conexión con lo que quedara sin salir.
//
// El tope impide que esperar el acuse convierta la expulsión en cooperativa.
// Vencido, se devuelve igual y lo que expulsa sigue siendo lo de siempre: el
// motor cerrándole la sesión y el firewall descartando sus paquetes.
func (c *Channel) Notify(ctx context.Context, to netip.Addr, n domain.RoomNotice) error {
	s, err := c.host()
	if err != nil {
		return err
	}
	nombre, ok := noticeKindName(n.Kind)
	if !ok {
		return fmt.Errorf("aviso de tipo desconocido: %d", n.Kind)
	}

	destinos := s.peers()
	if to.IsValid() {
		pc, ok := s.peer(to)
		if !ok {
			return fmt.Errorf("no hay canal abierto con %s", to)
		}
		destinos = []*peerConn{pc}
	}
	if len(destinos) == 0 {
		return nil
	}

	seq, esperar := s.nextAck()
	defer s.dropAck(seq)

	sobre, err := wrap(KindNotice, noticeMsg{Seq: seq, Kind: nombre, Reason: n.Reason})
	if err != nil {
		return err
	}
	var fallos []error
	for _, pc := range destinos {
		if err := pc.write(sobre); err != nil {
			fallos = append(fallos, fmt.Errorf("%s: %w", pc.addr, err))
		}
	}

	select {
	case <-esperar:
	case <-time.After(NoticeAckWait):
		c.log().Warn("el aviso no se acusó a tiempo, se sigue igual", "espera", NoticeAckWait.String())
	case <-ctx.Done():
	}
	return errors.Join(fallos...)
}

// AnnounceCode reparte el invite ID nuevo a los presentes, sellado contra la
// llave de cada uno.
//
// Existe porque renovar tenía un efecto secundario que nadie había nombrado: los
// que están dentro se quedaban con el código viejo guardado. Sellarlo es lo que
// hace que renovar CONSERVE su efecto: repartirlo en claro por un enlace que
// puede pasar por otro peer le devolvería al que acaba de quedar afuera el
// ticket que la renovación existía para quitarle.
//
// Que falle no invalida la renovación. El código nuevo ya es el bueno y el
// vestíbulo ya está levantado con él.
func (c *Channel) AnnounceCode(_ context.Context, r domain.Room) error {
	s, err := c.host()
	if err != nil {
		return err
	}
	plano, err := jsonBytes(roomMsg{InviteID: r.InviteID.String(), Seed: r.Seed})
	if err != nil {
		return err
	}

	var fallos []error
	for _, pc := range s.peers() {
		llave, ok := s.keyFor(pc.addr)
		if !ok {
			// Sin llave no se manda en claro: se registra y se calla. El que no
			// lo reciba pierde el código guardado, no la sala.
			c.log().Warn("no hay llave para sellarle el código nuevo a un miembro", "ip", pc.addr.String())
			continue
		}
		sellado, err := seal(llave, plano)
		if err != nil {
			fallos = append(fallos, fmt.Errorf("%s: %w", pc.addr, err))
			continue
		}
		sobre, err := wrap(KindCode, codeMsg{Sealed: sellado})
		if err != nil {
			fallos = append(fallos, err)
			continue
		}
		if err := pc.write(sobre); err != nil {
			fallos = append(fallos, fmt.Errorf("%s: %w", pc.addr, err))
		}
	}
	return errors.Join(fallos...)
}

// ConnectedMembers son las direcciones con el canal de la sala abierto.
//
// Sin oyente devuelve vacío y no error: en el invitado no hay nada que
// escuchar, y esto lo llama el mismo camino que relee miembros en los dos
// roles. Un error ahí obligaría a mirar el rol antes de preguntar, que es la
// clase de rama que se olvida.
//
// Ver el contrato en [port.ControlChannel.ConnectedMembers].
func (c *Channel) ConnectedMembers() []netip.Addr {
	c.mu.Lock()
	srv := c.srv
	c.mu.Unlock()
	if srv == nil {
		return nil
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	out := make([]netip.Addr, 0, len(srv.conns))
	for ip := range srv.conns {
		out = append(out, ip)
	}
	// Ordenadas porque el llamador arma con esto una lista de miembros y un
	// conjunto de reglas, y el orden de un mapa cambia en cada recorrido: sin
	// esto, la firma que decide si las reglas cambiaron cambiaría sola.
	sort.Slice(out, func(i, j int) bool { return out[i].Less(out[j]) })
	return out
}

func (c *Channel) host() (*server, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.srv == nil {
		return nil, errors.New("control: el oyente de la sala no está levantado")
	}
	return c.srv, nil
}

func (s *server) peers() []*peerConn {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*peerConn, 0, len(s.conns))
	for _, pc := range s.conns {
		out = append(out, pc)
	}
	return out
}

func (s *server) peer(ip netip.Addr) (*peerConn, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pc, ok := s.conns[ip]
	return pc, ok
}

func (s *server) keyFor(ip netip.Addr) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k, ok := s.keys[ip]
	return k, ok
}

func (s *server) nextAck() (uint64, chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	ch := make(chan struct{}, 1)
	s.acks[s.seq] = ch
	return s.seq, ch
}

func (s *server) dropAck(seq uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.acks, seq)
}

// stop baja los oyentes y las conexiones. No espera a nadie.
func (s *server) stop() {
	s.cancel()

	s.mu.Lock()
	oyentes := []net.Listener{s.lobbyLn, s.roomLn}
	s.lobbyLn, s.roomLn = nil, nil
	conns := make([]*peerConn, 0, len(s.conns))
	for _, pc := range s.conns {
		conns = append(conns, pc)
	}
	s.conns = map[netip.Addr]*peerConn{}
	s.keys = map[netip.Addr][]byte{}
	s.mu.Unlock()

	for _, ln := range oyentes {
		if ln != nil {
			_ = ln.Close()
		}
	}
	for _, pc := range conns {
		pc.close()
	}
}

// peerConn es una conexión con un miembro, con su escritura serializada.
//
// El candado protege la escritura y NADA MÁS. Cerrar no lo toma, y esa
// separación es un requisito y no un detalle: mandar bloquea mientras el otro
// lado no reciba, así que un cierre que necesitara el mismo candado se quedaría
// esperando justo a la goroutine que hay que desbloquear.
type peerConn struct {
	addr netip.Addr
	conn net.Conn
	ch   *Channel

	mu      sync.Mutex
	w       *wire.Writer
	cerrada atomic.Bool
}

// write manda un sobre, con plazo.
//
// **El plazo es lo que impide que un miembro congele al host.** Escribir en un
// socket bloquea cuando el otro lado deja de recibir, y esto lo llaman Announce
// y Notify, que el caso de uso invoca con el candado de la sesión tomado: sin
// plazo, un cliente modificado que abra la conexión y no lea nunca deja la sesión
// entera trabada, sin haber mandado un solo mensaje inválido.
//
// Vencido el plazo, esa conexión se cierra. Perder a un miembro que no recibe es
// exactamente lo que ya pasó de hecho.
func (p *peerConn) write(e envelope) error {
	if p.cerrada.Load() {
		return net.ErrClosed
	}
	p.mu.Lock()
	_ = p.conn.SetWriteDeadline(p.ch.now().Add(writeWait))
	err := p.w.Write(e)
	_ = p.conn.SetWriteDeadline(time.Time{})
	p.mu.Unlock()

	if err != nil {
		p.close()
	}
	return err
}

func (p *peerConn) close() {
	if p.cerrada.Swap(true) {
		return
	}
	_ = p.conn.Close()
}
