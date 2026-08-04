package control

import (
	"context"

	"fmt"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/daemon/transport/wire"
)

// reconnect es la escalera con la que el invitado vuelve a marcar.
//
// No es un plazo de corte y no alarga nada: mientras reintenta, el contador de
// ausencia del host de la decisión 20 corre igual, porque lo que ese contador
// mide es la ausencia y no los intentos. Arranca corto porque la causa común es
// un enlace del motor que se rehace en segundos, y se aplana en medio minuto
// porque más rápido no arregla nada y llena el log.
var reconnect = []time.Duration{
	1 * time.Second,
	2 * time.Second,
	5 * time.Second,
	10 * time.Second,
	20 * time.Second,
	30 * time.Second,
}

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
		ch:     c,
		at:     netip.AddrPortFrom(host, domain.ControlPort),
		puerta: host == domain.RendezvousHostAddress,
		llaves: llaves,
		creds:  make(chan credentialResponseMsg, 1),
	}
	cli.ctx, cli.cancel = context.WithCancel(context.WithoutCancel(ctx))
	c.cli = cli
	c.mu.Unlock()

	if anterior != nil {
		anterior.stop()
	}

	conn, err := c.dial(ctx, cli.at)
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
	sobre, err := wrap(KindCredentialRequest, credentialRequestMsg{
		Name:      req.Name.String(),
		PublicKey: cli.llaves.pub[:],
	})
	if err != nil {
		return domain.Credential{}, err
	}
	if err := cli.write(sobre); err != nil {
		return domain.Credential{}, fmt.Errorf("pidiéndole la credencial al host: %w", err)
	}

	select {
	case resp := <-cli.creds:
		return abrirCredencial(cli.llaves, resp)
	case <-time.After(credentialWait):
		return domain.Credential{}, fmt.Errorf("el host no contestó el pedido de credencial en %s", credentialWait)
	case <-ctx.Done():
		return domain.Credential{}, ctx.Err()
	case <-cli.ctx.Done():
		return domain.Credential{}, ErrNotDialed
	}
}

func abrirCredencial(llaves keyPair, resp credentialResponseMsg) (domain.Credential, error) {
	if resp.Error != "" {
		return domain.Credential{}, fmt.Errorf("el host no emitió la credencial: %s", resp.Error)
	}
	plano, err := llaves.open(resp.Sealed)
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
		anuncio := domain.RoomAnnounce{RoomName: msg.RoomName, GameID: msg.GameID}.Sanitize()
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
func (c *client) redial() {
	for intento := 0; ; intento++ {
		espera := reconnect[min(intento, len(reconnect)-1)]
		select {
		case <-time.After(espera):
		case <-c.ctx.Done():
			return
		}

		conn, err := c.ch.dial(c.ctx, c.at)
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
