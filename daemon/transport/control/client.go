package control

import (
	"bytes"
	"context"
	"crypto/ed25519"

	"fmt"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/core/timing"
	"github.com/accentiostudios/kanpachi/daemon/transport/wire"
)

// reconnect es la escalera con la que el invitado vuelve a marcar, y sale de
// [timing.ControlReconnectBackoff], donde está el porqué de cada peldaño.
var reconnect = timing.ControlReconnectBackoff()

// initialRoomDialAttempts es cuántas veces se intenta abrir el canal de la
// sala al entrar. El primer SYN puede disparar el handshake del relay de la
// red overlay y perderse antes de que el camino de datos esté listo.
const initialRoomDialAttempts = 2

// Dial conecta con el host. Reemplaza la conexión anterior.
//
// El invitado marca hacia afuera y no abre ningún puerto, así que su deny-all
// queda intacto. Marcar a una dirección conocida en vez de aceptar conexiones es
// también lo que hace imposible que un miembro se haga pasar por el host DENTRO
// de la sala: ahí las direcciones las asignó el host con la credencial.
//
// Son dos conversaciones y el destino las distingue. A la dirección del
// vestíbulo se va a una sola cosa, pedir la credencial, y esa conexión no dice
// nada sobre la presencia del host. La conexión a la sala sí: **que esté caída
// es lo que alimenta HostPresent y el contador de veinte minutos**, y es
// información confiable sin confiar en nadie, porque no es un mensaje
// falsificable, es la ausencia de un socket.
func (c *Channel) Dial(ctx context.Context, host netip.Addr) error {
	if !host.IsValid() {
		return fmt.Errorf("control: no hay dirección del host a la que marcar")
	}
	llaves, err := c.sessionKeys()
	if err != nil {
		return err
	}

	c.mu.Lock()
	anterior := c.cli
	cli := &client{
		ch: c,
		at: netip.AddrPortFrom(host, domain.ControlPort),
		// Marcar a una dirección del espacio de vestíbulos ES marcar la puerta.
		// Antes se comparaba contra una dirección constante; ahora cada sala
		// deriva la suya del código, así que lo que distingue es el espacio. Las
		// salas no viven ahí, así que no hay ambigüedad. Ver [domain.LobbySpace].
		puerta: domain.LobbySpace.Contains(host),
		llaves: llaves,
		creds:  make(chan credentialResponseMsg, 1),
	}
	cli.ctx, cli.cancel = context.WithCancel(context.WithoutCancel(ctx))
	c.cli = cli
	c.mu.Unlock()

	if anterior != nil {
		anterior.stop()
	}

	conn, err := c.dialInitial(ctx, cli.at, cli.puerta)
	if err != nil {
		cli.stop()
		c.mu.Lock()
		if c.cli == cli {
			c.cli = nil
		}
		c.mu.Unlock()
		return err
	}
	cli.adopt(conn)

	if !cli.puerta {
		emitir(c, c.presence, true, "presencia")
	}
	go cli.read(conn)
	return nil
}

// dialInitial abre la conexión que inicia una conversación de control.
//
// La puerta del vestíbulo se intenta una vez: no hay una transición de red
// previa que pueda estar preparando un relay. Al entrar a una sala, en cambio,
// el primer paquete puede activar el handshake del relay de EasyTier. Se hace
// un segundo intento con un plazo propio y acotado, en vez de dejar que TCP
// espere el timeout largo del sistema operativo.
func (c *Channel) dialInitial(ctx context.Context, at netip.AddrPort, puerta bool) (net.Conn, error) {
	if puerta {
		return c.dial(ctx, at)
	}
	return dialWithRetry(ctx, at, initialRoomDialAttempts, timing.InitialRoomDialWait,
		timing.InitialRoomRetryWait, c.dial)
}

// dialWithRetry marca con un presupuesto por intento y una pausa cancelable.
//
// El contexto padre conserva la autoridad: cancelar el ingreso corta el
// marcado o la espera y no inicia otro intento. El último error del marcador
// se devuelve sin ocultarlo para que el caso de uso pueda diagnosticarlo.
func dialWithRetry(
	ctx context.Context,
	at netip.AddrPort,
	attempts int,
	attemptWait time.Duration,
	retryWait time.Duration,
	dial func(context.Context, netip.AddrPort) (net.Conn, error),
) (net.Conn, error) {
	var last error
	for attempt := 0; attempt < attempts; attempt++ {
		attemptCtx, cancel := context.WithTimeout(ctx, attemptWait)
		conn, err := dial(attemptCtx, at)
		cancel()
		if err == nil {
			return conn, nil
		}
		last = err
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if attempt == attempts-1 {
			break
		}

		timer := time.NewTimer(retryWait)
		select {
		case <-timer.C:
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil, ctx.Err()
		}
	}
	return nil, last
}

// RequestCredential es el paso 5 del canje: se pide por la puerta y la respuesta
// vuelve sellada contra la llave de esta sesión.
//
// La llave la pone el adaptador y core no la ve, que es el contrato del puerto.
// Es efímera: se generó al marcar, vive lo que dure esto y se descarta al salir,
// así que no es identidad persistida y no habilita ningún baneo.
//
// Lo que este método comprueba es la FORMA. La coherencia de lo que dijo el host
// (que la subred contenga la IP, que no sea la del vestíbulo, que la IP no sea la
// suya) la revisa el caso de uso, que es quien sabe qué significa cada cosa.
func (c *Channel) RequestCredential(ctx context.Context, req domain.CredentialRequest) (domain.Credential, error) {
	c.mu.Lock()
	cli := c.cli
	c.mu.Unlock()

	if cli == nil || !cli.puerta {
		return domain.Credential{}, ErrNotDialed
	}
	if req.Member.IsZero() {
		// Sin llave de miembro el host rechaza, así que pedir sin ella solo
		// gasta la vuelta. Que falte acá es un error de cableado, no de red.
		return domain.Credential{}, fmt.Errorf("control: el pedido no trae llave de miembro")
	}
	// La firma se hace ACÁ y no en core, porque el transcript lleva la llave
	// efímera de esta conexión, que core no ve. Cubre la efímera a propósito:
	// sin ella, alguien del vestíbulo podría reenviar un pedido ajeno con su
	// propia efímera puesta y llevarse la credencial del que vuelve, sellada
	// contra la suya. Ver [memberTranscript].
	memberPub := req.Member.Public()
	sig := req.Member.Sign(memberTranscript(
		req.RendezvousName, cli.llaves.pub[:], memberPub, req.Name.String()))
	sobre, err := wrap(KindCredentialRequest, credentialRequestMsg{
		Name:           req.Name.String(),
		PublicKey:      cli.llaves.pub[:],
		RendezvousName: req.RendezvousName,
		MemberKey:      memberPub,
		MemberSig:      sig,
	})
	if err != nil {
		return domain.Credential{}, err
	}
	if err := cli.write(sobre); err != nil {
		return domain.Credential{}, fmt.Errorf("pidiéndole la credencial al host: %w", err)
	}

	select {
	case resp := <-cli.creds:
		return openCredential(cli.llaves, resp, req)
	case <-time.After(timing.CredentialWait):
		return domain.Credential{}, fmt.Errorf("el host no contestó el pedido de credencial en %s", timing.CredentialWait)
	case <-ctx.Done():
		return domain.Credential{}, ctx.Err()
	case <-cli.ctx.Done():
		return domain.Credential{}, ErrNotDialed
	}
}

// openCredential opens the envelope and, when it can, checks WHO sent it.
//
// # The three possible answers, and which one is the hard stop
//
//   - **A signature that validates against the pinned key**: it is that room's
//     host, or somebody who stole its `identity.key`. That is the most anyone
//     can claim.
//   - **No expected key**: there is nothing to compare against, so the exchange
//     goes on unverified. A terse registry must not turn into a room nobody can
//     enter.
//   - **A missing or bad signature, with an expected key in hand**: somebody
//     answered for the host. This is the hard case of the whole exchange:
//     anybody holding the code can sit in the lobby, and entering would mean
//     entering THEIR network believing it is the host's, with the game's ports
//     opened towards it.
func openCredential(
	keys keyPair, resp credentialResponseMsg, req domain.CredentialRequest,
) (domain.Credential, error) {
	if resp.Error != "" {
		return domain.Credential{}, fmt.Errorf("el host no emitió la credencial: %s", resp.Error)
	}
	if err := verifySignature(keys, resp, req); err != nil {
		return domain.Credential{}, err
	}
	plano, err := keys.open(resp.Sealed)
	if err != nil {
		// Un sobre que no abre es un sobre que no era para nosotros, y en el
		// vestíbulo eso puede ser cualquiera contestando. No se interpreta nada
		// de lo que traía.
		return domain.Credential{}, err
	}
	msg, err := wire.DecodeStrict[credentialMsg](plano)
	if err != nil {
		return domain.Credential{}, fmt.Errorf("%w: la credencial sellada: %v", errShape, err)
	}
	return credentialFromWire(msg)
}

// client es el lado del invitado. No hay servidor: solo un marcador que
// reconecta.
type client struct {
	ch     *Channel
	at     netip.AddrPort
	puerta bool
	llaves keyPair
	creds  chan credentialResponseMsg

	ctx    context.Context
	cancel context.CancelFunc

	mu      sync.Mutex
	conn    net.Conn
	w       *wire.Writer
	cerrado bool
}

func (c *client) adopt(conn net.Conn) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.conn, c.w = conn, wire.NewWriter(conn, MaxMessage)
}

// SendCanaryReport le cuenta al host lo que se vio. SOLO el invitado.
//
// Va por la conexión de la SALA, que es la única abierta contra el host, así que
// no hace falta decir a quién: el host saca el remitente de esa misma conexión.
//
// No espera acuse, a diferencia de un aviso de expulsión. Perder un informe
// cuesta que el host no confirme esa ronda, y eso ya tiene su estado
// ([domain.CanaryUnconfirmed]). Esperar acuse acá metería una espera en el
// camino de un miembro cualquiera, que es lo último que conviene en el código
// que corre como SYSTEM del otro lado.
func (c *Channel) SendCanaryReport(ctx context.Context, r domain.CanaryReport) error {
	c.mu.Lock()
	cli := c.cli
	c.mu.Unlock()

	if cli == nil || cli.puerta {
		// Por la puerta no: ahí solo se pide credencial, y el canario es de la
		// sala. Ver [admitidoEnLaPuerta].
		return ErrNotDialed
	}

	tcp, ok := probeOutcomeName(r.TCP)
	if !ok {
		return fmt.Errorf("resultado de TCP desconocido: %d", r.TCP)
	}
	udp, ok := probeOutcomeName(r.UDP)
	if !ok {
		return fmt.Errorf("resultado de UDP desconocido: %d", r.UDP)
	}

	sobre, err := wrap(KindCanaryReport, canaryReportMsg{Port: r.Port, TCP: tcp, UDP: udp})
	if err != nil {
		return err
	}
	return cli.write(sobre)
}

func (c *client) write(e envelope) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cerrado || c.w == nil {
		return net.ErrClosed
	}
	return c.w.Write(e)
}

// read consume lo que manda el host y lo reparte por los canales de salida.
//
// El adaptador solo emite lo que llegó por ESTA conexión, que es la que se abrió
// contra la dirección del host. Un miembro no puede anunciar nada, porque un
// miembro no es a quien este cliente le marcó.
func (c *client) read(conn net.Conn) {
	admitido := admitidoDelHost
	if c.puerta {
		admitido = admitidoDeLaPuerta
	}
	r := wire.NewReader(conn, MaxMessage)

	for {
		linea, err := r.ReadLine()
		if err != nil {
			c.perdió(conn, err)
			return
		}
		e, err := decodeEnvelope(linea)
		if err != nil {
			c.perdió(conn, err)
			return
		}
		if !admitido[e.Kind] {
			c.ch.log().Warn("mensaje del host descartado por no corresponder a esta conexión",
				"tipo", string(e.Kind), "puerta", c.puerta)
			continue
		}
		if !c.reparte(e) {
			c.perdió(conn, errShape)
			return
		}
	}
}

// reparte interpreta un mensaje ya admitido. Devuelve false si venía roto, que
// es motivo de cortar: quien manda basura por esta conexión dice ser el host.
func (c *client) reparte(e envelope) bool {
	switch e.Kind {
	case KindCredentialResponse:
		resp, err := payloadOf[credentialResponseMsg](e)
		if err != nil {
			return false
		}
		select {
		case c.creds <- resp:
		default:
		}

	case KindAnnounce:
		msg, err := payloadOf[announceMsg](e)
		if err != nil {
			return false
		}
		// Sanitize acota lo que llegó de otra máquina ANTES de que toque nada:
		// el nombre por runas, el id contra el alfabeto que exige un perfil.
		anuncio := domain.RoomAnnounce{
			RoomName:         msg.RoomName,
			GameID:           msg.GameID,
			GameHealth:       healthFromWire(msg.GameHealth),
			GameWhere:        addrFromWire(msg.GameWhere),
			GameRedirectedTo: addrFromWire(msg.GameRedirectedTo),
		}.Sanitize()
		emitir(c.ch, c.ch.announces, anuncio, "anuncio")

	case KindNotice:
		msg, err := payloadOf[noticeMsg](e)
		if err != nil {
			return false
		}
		kind, ok := noticeKinds[msg.Kind]
		if !ok {
			// Un aviso de tipo desconocido no se adivina. No corta la conexión:
			// una versión más nueva del otro lado puede mandar uno que esta no
			// entiende, y eso no es hostilidad.
			c.ch.log().Warn("aviso de tipo desconocido", "tipo", msg.Kind)
			return true
		}
		// El acuse va ANTES de repartir. Es lo que le dice al host que puede
		// seguir con la expulsión sin esperar el plazo entero, y mandarlo
		// después significaría acusar cuando el otro lado ya cortó.
		if sobre, err := wrap(KindAck, ackMsg{Seq: msg.Seq}); err == nil {
			_ = c.write(sobre)
		}
		emitir(c.ch, c.ch.notices, domain.RoomNotice{
			Kind: kind, Reason: domain.ClampRoomName(msg.Reason),
		}, "aviso")

	case KindCanaryRequest:
		msg, err := payloadOf[canaryRequestMsg](e)
		if err != nil {
			return false
		}
		// **Acá vive la invariante entera de esta función.** El destino sale de
		// `c.at.Addr()`, que es la dirección A LA QUE ESTA MÁQUINA MARCÓ para
		// entrar a la sala. No sale del mensaje, y no puede salir del mensaje
		// porque [canaryRequestMsg] no tiene campo de dirección.
		//
		// Sin eso, este mensaje convertiría el canal de la sala en un escáner
		// por encargo: un host modificado le pediría a todos los miembros que
		// marcaran a una máquina de fuera, y el tráfico saldría de sus casas.
		req, err := canaryRequestFromWire(c.at.Addr(), msg)
		if err != nil {
			c.ch.log().Warn("llegó un pedido de canario mal formado")
			return true
		}
		emitir(c.ch, c.ch.canaryReqs, req, "pedido de canario")

	case KindCode:
		msg, err := payloadOf[codeMsg](e)
		if err != nil {
			return false
		}
		plano, err := c.llaves.open(msg.Sealed)
		if err != nil {
			c.ch.log().Warn("llegó un código nuevo que no era para esta máquina")
			return true
		}
		sellado, err := wire.DecodeStrict[roomMsg](plano)
		if err != nil {
			return false
		}
		// El código vuelve por el MISMO parser que usa lo que el usuario pega,
		// con sus seis formas y su tope de longitud. Que venga sellado prueba
		// para quién es, no que sea un código.
		room, err := domain.ParseRoom(sellado.InviteID + "@" + sellado.Seed)
		if err != nil {
			c.ch.log().Warn("el código nuevo que mandó el host no tiene forma de código", "error", err)
			return true
		}
		emitir(c.ch, c.ch.codes, room, "código")
	}
	return true
}

// perdió cierra esta conexión y arranca la reconexión si corresponde.
func (c *client) perdió(conn net.Conn, err error) {
	c.mu.Lock()
	actual := c.conn == conn
	cerrado := c.cerrado
	c.mu.Unlock()
	_ = conn.Close()

	if cerrado || !actual {
		return // ya la reemplazó otro Dial, o se cerró el canal entero
	}
	if c.puerta {
		return // el vestíbulo es de una sola visita: se entra, se pide y se sale
	}

	c.ch.log().Warn("se cayó la conexión con el host", "error", err)
	emitir(c.ch, c.ch.presence, false, "presencia")
	go c.redial()
}

// redial vuelve a marcar con la escalera, hasta lograrlo o hasta que se cierre.
//
// # Cada intento lleva su propio plazo, y hasta el 2026-08-25 no lo llevaba
//
// El marcado inicial sí lo tiene ([dialWithRetry]), y la asimetría era cara
// contra un `drop`. Sin plazo propio, cada intento se come el presupuesto de
// SYN entero del núcleo, que en Linux son alrededor de 130 segundos, así que la
// escalera anunciada de 1/2/5/10/20/30 degeneraba en un intento cada dos
// minutos: quien esperaba volver a la sala esperaba veinte veces más de lo que
// dice el diseño, y ninguna línea de log lo delataba.
//
// El plazo es el MISMO constante que usa el marcado inicial, para que los dos
// caminos no puedan separarse.
func (c *client) redial() {
	for intento := 0; ; intento++ {
		espera := reconnect[min(intento, len(reconnect)-1)]
		select {
		case <-time.After(espera):
		case <-c.ctx.Done():
			return
		}

		intentoCtx, cancelar := context.WithTimeout(c.ctx, timing.InitialRoomDialWait)
		conn, err := c.ch.dial(intentoCtx, c.at)
		cancelar()
		if err != nil {
			continue
		}
		c.mu.Lock()
		cerrado := c.cerrado
		if !cerrado {
			c.conn, c.w = conn, wire.NewWriter(conn, MaxMessage)
		}
		c.mu.Unlock()
		if cerrado {
			_ = conn.Close()
			return
		}

		c.ch.log().Info("volvió la conexión con el host")
		emitir(c.ch, c.ch.presence, true, "presencia")
		go c.read(conn)
		return
	}
}

// stop corta esta conexión. No espera a nadie leer nada.
func (c *client) stop() {
	c.cancel()

	c.mu.Lock()
	conn := c.conn
	c.cerrado = true
	c.conn, c.w = nil, nil
	c.mu.Unlock()

	if conn != nil {
		_ = conn.Close()
	}
}

// verificarFirma comprueba la respuesta contra la llave que el REGISTRO fijó.
//
// La llave que viaja en el mensaje no se usa para verificar: se COMPARA. Una
// llave que llega junto a la firma que produce es un sello que se autofirma, y
// lo único que la convierte en una afirmación es que coincida con la que llegó
// antes y por otro camino. Ver [credentialResponseMsg].
func verifySignature(keys keyPair, resp credentialResponseMsg, req domain.CredentialRequest) error {
	if len(req.ExpectHostKey) != ed25519.PublicKeySize {
		// Nothing to compare against: the registry did not hand over the key, or
		// it could not be read. The exchange goes on unverified, because
		// refusing here would turn a terse registry into a room nobody can
		// enter.
		return nil
	}
	if len(resp.Sig) == 0 {
		// **A missing signature is a hard stop, and this branch is what makes
		// every other one worth anything.** The first version of this accepted
		// an unsigned answer as "could not be checked", and measuring the whole
		// exchange showed the obvious: a lobby squatter does not send a bad
		// signature, it sends none, and it got in anyway. A mechanism that turns
		// itself off by omitting a field is not a mechanism.
		//
		// The price, stated whole: a host running an older version signs
		// nothing, so a new guest cannot enter its room until it updates. It is
		// the one moment this product demands that both ends match, and it is
		// accepted because the alternative leaves open the only door through
		// which somebody can be walked into a stranger's network.
		return fmt.Errorf("quien contestó en el vestíbulo no firmó la credencial: " +
			"o el host corre una versión vieja de Kanpachi, o no es el host de esa sala")
	}
	if !bytes.Equal(resp.HostKey, req.ExpectHostKey) {
		return fmt.Errorf("quien contestó en el vestíbulo no es el host de esa sala: " +
			"la llave con la que firmó no es la que el registro tiene fijada para ese código")
	}
	msg := credentialTranscript(req.RendezvousName, keys.pub[:], resp.Sealed)
	if !ed25519.Verify(ed25519.PublicKey(req.ExpectHostKey), msg, resp.Sig) {
		return fmt.Errorf("la credencial no la firmó el host de esa sala, " +
			"o no es la respuesta a este pedido")
	}
	return nil
}

// healthFromWire lee el estado del juego que mandó el host.
//
// Lo que no es una de las dos palabras conocidas es "no se sabe", y eso incluye
// el campo ausente de un host más viejo que esta versión. Ver [announceMsg].
func healthFromWire(s string) domain.GameHealth {
	switch s {
	case domain.GameHealthListening.String():
		return domain.GameHealthListening
	case domain.GameHealthSilent.String():
		return domain.GameHealthSilent
	case domain.GameHealthElsewhere.String():
		// Faltaba, y era el único valor que este caso produce: un juego atado a
		// otra dirección de la máquina del host. El host lo serializa desde que
		// existe, y acá caía en el `default`, o sea que llegaba como "no se
		// sabe". El invitado no pintaba el punto ni la frase que nombra el
		// arreglo, con el anuncio llegando entero. Medido el 2026-08-20 con
		// Zomboid escuchando en la dirección del contenedor.
		return domain.GameHealthElsewhere
	default:
		// Lo que no se entiende se lee como "no se sabe" y no corta nada: una
		// versión más nueva del otro lado puede mandar un valor que esta no
		// conoce.
		return domain.GameHealthUnknown
	}
}

// addrFromWire lee la dirección donde el host dice que escucha su juego.
//
// Lo que no parsea es el cero, que ya significa "no se sabe". No se juzga qué
// dirección es: es de la máquina del otro y acá solo se pinta. Ver
// [domain.RoomAnnounce.GameWhere].
func addrFromWire(s string) netip.Addr {
	dir, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Addr{}
	}
	return dir
}
