package control

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/netip"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/core/port"
	"github.com/accentiostudios/kanpachi/core/timing"
	"github.com/accentiostudios/kanpachi/daemon/transport/wire"
)

// TestElCanalSatisfaceElPuerto: la comprobación que evita que el adaptador y el
// puerto se separen sin que nadie lo note hasta cablear el servicio.
func TestElCanalSatisfaceElPuerto(t *testing.T) {
	var _ port.ControlChannel = (*Channel)(nil)
}

func TestDialWithRetryReintentaTrasElPrimerFallo(t *testing.T) {
	want := errors.New("el relay todavía está negociando")
	var calls int

	client, server := net.Pipe()
	t.Cleanup(func() {
		_ = client.Close()
		_ = server.Close()
	})

	conn, err := dialWithRetry(
		context.Background(),
		netip.AddrPortFrom(ipHost, domain.ControlPort),
		2,
		time.Second,
		0,
		func(ctx context.Context, _ netip.AddrPort) (net.Conn, error) {
			calls++
			if _, ok := ctx.Deadline(); !ok {
				t.Fatal("el intento no tuvo plazo propio")
			}
			if calls == 1 {
				return nil, want
			}
			return client, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if conn != client {
		t.Fatal("no devolvió la conexión del segundo intento")
	}
	if calls != 2 {
		t.Fatalf("intentos = %d, se esperaban 2", calls)
	}
}

func TestDialWithRetryRespetaLaCancelación(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var calls int
	_, err := dialWithRetry(
		ctx,
		netip.AddrPortFrom(ipHost, domain.ControlPort),
		2,
		time.Second,
		time.Hour,
		func(context.Context, netip.AddrPort) (net.Conn, error) {
			calls++
			cancel()
			return nil, errors.New("el relay todavía está negociando")
		},
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, se esperaba context.Canceled", err)
	}
	if calls != 1 {
		t.Fatalf("intentos = %d, se esperaba 1", calls)
	}
}

// TestServirSinEmisorNoAceptaNada: aceptar conexiones para no poder
// contestarlas es peor que no escuchar.
func TestServirSinEmisorNoAceptaNada(t *testing.T) {
	r := nuevaRed()
	c := New(Deps{Clock: &relojFalso{t: time.Now()}, Log: logMudo{}, Listen: r.listen, Dial: r.desde(ipHost)})

	err := c.Serve(ctx(), domain.ControlScope{Lobby: ipPuerta, Room: ipHost})
	if !errors.Is(err, ErrNotAttached) {
		t.Fatalf("Serve sin emisor = %v", err)
	}
}

// TestElCanjeDeCredencialVaSelladoPorLaPuerta.
//
// Es el paso 5 del flujo de conexión. Lo que se comprueba además de que
// funcione: **el token no aparece en claro en el cable**. El vestíbulo es
// público y el motor puede relayar por otro peer, así que en claro el
// intermediario se llevaría con qué entrar a la sala.
func TestElCanjeDeCredencialVaSelladoPorLaPuerta(t *testing.T) {
	b := nuevoBanco(t)
	espía := &redEspía{red: b.red}
	invitado := New(Deps{Clock: b.reloj, Log: logMudo{}, Listen: b.red.listen, Dial: espía.desde(ipUno)})
	defer invitado.Close()

	if err := invitado.Dial(ctx(), ipPuerta); err != nil {
		t.Fatal(err)
	}
	cred, err := invitado.RequestCredential(ctx(), domain.CredentialRequest{Name: nick(t, "humberto")})
	if err != nil {
		t.Fatal(err)
	}

	if cred.Token != "token-secretísimo-"+ipUno.String() {
		t.Fatalf("la credencial no llegó entera: %+v", cred)
	}
	if cred.VirtualIP != ipUno || cred.Subnet != b.subred {
		t.Fatalf("la credencial no trae la dirección ni la subred: %+v", cred)
	}
	if bytes.Contains(espía.visto(), []byte("token-secretísimo")) {
		t.Fatal("el token viajó en claro por el vestíbulo")
	}

	// La llave que se guardó es la que mandó el que entra, y la puso el
	// adaptador: el caso de uso pidió la credencial sin llave ninguna.
	pedido, ok := b.emisor.últimoPedido()
	if !ok || len(pedido.PublicKey) != keyLen {
		t.Fatalf("el pedido llegó sin llave de sesión: %+v", pedido)
	}
}

// TestLaPuertaSoloAdmiteUnPedidoDeCredencial.
//
// La puerta acepta desconocidos por definición, así que lo único que la acota es
// que ahí no se pueda pedir otra cosa. Un aviso mandado por acá no se
// interpreta, no se contesta y no llega a ningún caso de uso.
func TestLaPuertaSoloAdmiteUnPedidoDeCredencial(t *testing.T) {
	b := nuevoBanco(t)

	conn, err := b.red.desde(ipNadie)(ctx(), netip.AddrPortFrom(ipPuerta, domain.ControlPort))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	sobre, _ := wrap(KindNotice, noticeMsg{Seq: 1, Kind: "kicked", Reason: "te saco yo"})
	if err := wire.NewWriter(conn, MaxMessage).Write(sobre); err != nil {
		t.Fatal(err)
	}

	// La conexión se corta sin respuesta. Leer devuelve error, no un mensaje.
	_ = conn.SetReadDeadline(time.Now().Add(plazo))
	if linea, err := wire.NewReader(conn, MaxMessage).ReadLine(); err == nil {
		t.Fatalf("la puerta contestó un aviso: %s", linea)
	}
}

// TestLaSalaNoAdmitePedidosDeCredencial: la puerta y la sala son dos
// conversaciones distintas, y confundirlas sería regalarle la sala a cualquiera
// que tenga el código.
func TestLaSalaNoAdmitePedidosDeCredencial(t *testing.T) {
	b := nuevoBanco(t, ipUno)

	conn, err := b.red.desde(ipUno)(ctx(), netip.AddrPortFrom(ipHost, domain.ControlPort))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	llaves, _ := newKeyPair()
	sobre, _ := wrap(KindCredentialRequest, credentialRequestMsg{Name: "humberto", PublicKey: llaves.pub[:]})
	if err := wire.NewWriter(conn, MaxMessage).Write(sobre); err != nil {
		t.Fatal(err)
	}

	time.Sleep(100 * time.Millisecond)
	if _, hubo := b.emisor.últimoPedido(); hubo {
		t.Fatal("un pedido de credencial por la sala llegó al emisor")
	}
}

// TestUnaIPQueNoEsMiembroSeRechazaAntesDeLeerNada.
//
// El rechazo ocurre en el Accept, así que de esa conexión no se interpreta ni un
// byte. Es la primera línea del código que corre como SYSTEM.
func TestUnaIPQueNoEsMiembroSeRechazaAntesDeLeerNada(t *testing.T) {
	b := nuevoBanco(t, ipUno)
	intruso := b.invitado(t, ipNadie)

	if err := intruso.Dial(ctx(), ipHost); err != nil {
		t.Fatal(err)
	}
	// Marcar "funciona" porque el pipe se establece. Lo que no ocurre es que el
	// host lo trate como miembro: la conexión se cierra y nada suyo se lee.
	if err := b.host.Announce(ctx(), domain.RoomAnnounce{RoomName: "Los panas"}); err != nil {
		t.Fatal(err)
	}
	nadaEn(t, intruso.Announcements(), "un anuncio para quien no es miembro")
}

// TestRecortarElAlcanceCierraLaConexiónDelExpulsado.
//
// Es la mitad de la decisión 22 que corre en el host: expulsar saca a alguien
// también de la lista de quién puede hablarle al código que corre como SYSTEM, y
// no espera a que el otro lado se dé por enterado.
func TestRecortarElAlcanceCierraLaConexiónDelExpulsado(t *testing.T) {
	b := nuevoBanco(t, ipUno, ipDos)
	uno := b.enSala(t, ipUno)
	dos := b.enSala(t, ipDos)

	// Se recorta el alcance dejando solo a ipDos, que es lo que hace KickMember.
	if err := b.host.Serve(ctx(), domain.ControlScope{
		Lobby: ipPuerta, Room: ipHost, Members: []netip.Addr{ipDos},
	}); err != nil {
		t.Fatal(err)
	}

	// Al expulsado se le cae la conexión, que es lo que su lado ve como
	// "el host no está".
	esperaBool(t, uno.HostPresence(), false)

	// Y el que se quedó sigue recibiendo.
	if err := b.host.Announce(ctx(), domain.RoomAnnounce{RoomName: "Los panas", GameID: "project-zomboid"}); err != nil {
		t.Fatal(err)
	}
	a := espera(t, dos.Announcements(), "el anuncio para quien se quedó")
	if a.GameID != "project-zomboid" {
		t.Fatalf("el anuncio llegó mal: %+v", a)
	}
}

// TestLaPuertaSigueAbiertaParaElExpulsado.
//
// Expulsar no es banear, y esto es esa decisión dicha como test: el alcance de
// la sala se recorta y el de la puerta no. Quien fue expulsado puede volver a
// tocar con el mismo código hasta que el host lo renueve, que es la otra
// operación y es independiente.
func TestLaPuertaSigueAbiertaParaElExpulsado(t *testing.T) {
	b := nuevoBanco(t, ipUno)
	if err := b.host.Serve(ctx(), domain.ControlScope{
		Lobby: ipPuerta, Room: ipHost, Members: nil,
	}); err != nil {
		t.Fatal(err)
	}

	vuelve := b.invitado(t, ipNadie)
	if err := vuelve.Dial(ctx(), ipPuerta); err != nil {
		t.Fatal(err)
	}
	if _, err := vuelve.RequestCredential(ctx(), domain.CredentialRequest{Name: nick(t, "humberto")}); err != nil {
		t.Fatalf("el que volvió a tocar la puerta no consiguió credencial: %v", err)
	}
}

// TestUnMensajeQuePasaDelTopeCortaSoloEsaConexión.
func TestUnMensajeQuePasaDelTopeCortaSoloEsaConexión(t *testing.T) {
	b := nuevoBanco(t, ipUno, ipDos)
	dos := b.enSala(t, ipDos)

	conn, err := b.red.desde(ipUno)(ctx(), netip.AddrPortFrom(ipHost, domain.ControlPort))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// Se escribe crudo: el escritor del canal se negaría a mandar algo que pasa
	// del tope, y lo que se prueba acá es qué hace el que RECIBE.
	_, _ = conn.Write([]byte(`{"kind":"ack","payload":{"seq":1,"basura":"` + strings.Repeat("x", MaxMessage) + `"}}` + "\n"))

	if err := b.host.Announce(ctx(), domain.RoomAnnounce{RoomName: "Los panas"}); err != nil {
		t.Fatal(err)
	}
	espera(t, dos.Announcements(), "el anuncio del que sigue conectado")
}

// TestLaSegundaConexiónDeUnMiembroDesplazaALaPrimera: reconectar es normal, y
// sin esto un miembro podría acumular conexiones contra el proceso que corre
// como SYSTEM.
func TestLaSegundaConexiónDeUnMiembroDesplazaALaPrimera(t *testing.T) {
	b := nuevoBanco(t, ipUno)
	primera := b.enSala(t, ipUno)

	vieja, _ := mustHost(t, b).peer(ipUno)

	segunda := b.invitado(t, ipUno)
	if err := segunda.Dial(ctx(), ipHost); err != nil {
		t.Fatal(err)
	}
	b.esperaReemplazo(t, ipUno, vieja)

	if err := b.host.Announce(ctx(), domain.RoomAnnounce{RoomName: "Los panas"}); err != nil {
		t.Fatal(err)
	}
	espera(t, segunda.Announcements(), "el anuncio por la conexión nueva")
	nadaEn(t, primera.Announcements(), "un anuncio por la conexión desplazada")
}

// TestUnaConexiónQueLlegaYNoHablaSeCorta.
//
// El reloj falso está en el pasado, así que el plazo de la puerta ya venció
// cuando la conexión llega. Sin esto habría que esperar cinco segundos de
// verdad, y un test que duerme es un test que nadie corre.
func TestUnaConexiónQueLlegaYNoHablaSeCorta(t *testing.T) {
	b := nuevoBanco(t)
	b.reloj.mueve(time.Now().Add(-time.Hour))

	conn, err := b.red.desde(ipNadie)(ctx(), netip.AddrPortFrom(ipPuerta, domain.ControlPort))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Now().Add(plazo))
	if _, err := wire.NewReader(conn, MaxMessage).ReadLine(); err == nil {
		t.Fatal("la conexión callada siguió viva")
	}
}

// TestElAvisoSeAcusaYLaExpulsiónNoEsperaMásDeLaCuenta.
//
// Dos mitades de la decisión 22. Con acuse, el host sigue en cuanto el otro
// contesta. Sin acuse, sigue igual al vencer el tope: esperar sin límite
// convertiría la expulsión en cooperativa.
func TestElAvisoSeAcusaYLaExpulsiónNoEsperaMásDeLaCuenta(t *testing.T) {
	b := nuevoBanco(t, ipUno)
	uno := b.enSala(t, ipUno)

	arranque := time.Now()
	if err := b.host.Notify(ctx(), ipUno, domain.RoomNotice{
		Kind: domain.NoticeKicked, Reason: "el host te sacó de la sala",
	}); err != nil {
		t.Fatal(err)
	}
	if tardó := time.Since(arranque); tardó >= timing.NoticeAckWait {
		t.Fatalf("con acuse tendría que volver enseguida, tardó %s", tardó)
	}
	aviso := espera(t, uno.Notices(), "el aviso de expulsión")
	if aviso.Kind != domain.NoticeKicked {
		t.Fatalf("el aviso llegó como %v", aviso.Kind)
	}
}

// TestSinAcuseSeSigueIgualAlVencerElTope.
func TestSinAcuseSeSigueIgualAlVencerElTope(t *testing.T) {
	b := nuevoBanco(t, ipUno)

	// Un miembro que recibe y jamás contesta: el caso del cliente modificado que
	// ignora el aviso. Lee para que el aviso salga, y no acusa.
	conn, err := b.red.desde(ipUno)(ctx(), netip.AddrPortFrom(ipHost, domain.ControlPort))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	go func() {
		buf := make([]byte, 1024)
		for {
			if _, err := conn.Read(buf); err != nil {
				return
			}
		}
	}()
	b.esperaRegistro(t, ipUno)

	arranque := time.Now()
	if err := b.host.Notify(ctx(), ipUno, domain.RoomNotice{Kind: domain.NoticeKicked}); err != nil {
		t.Fatal(err)
	}
	tardó := time.Since(arranque)
	if tardó < timing.NoticeAckWait {
		t.Fatalf("devolvió en %s sin haber esperado el acuse", tardó)
	}
	if tardó > 3*timing.NoticeAckWait {
		t.Fatalf("esperó %s, o sea que el tope no está acotando nada", tardó)
	}
}

// TestElCódigoNuevoLlegaSoloPorSuCanalYSoloAQuienEs.
//
// Sellar el código no es proteger un secreto: el invite code es un ticket
// desechable. Es para que RENOVAR conserve su efecto, porque repartirlo en claro
// por un enlace que puede pasar por otro peer le devolvería al que quedó afuera
// justo el ticket que la renovación le quitó.
func TestElCódigoNuevoLlegaSoloPorSuCanalYSoloAQuienEs(t *testing.T) {
	b := nuevoBanco(t)
	uno := b.invitado(t, ipUno)
	if err := uno.Dial(ctx(), ipPuerta); err != nil {
		t.Fatal(err)
	}
	if _, err := uno.RequestCredential(ctx(), domain.CredentialRequest{Name: nick(t, "humberto")}); err != nil {
		t.Fatal(err)
	}
	// Con la credencial emitida, el host ya conoce a ipUno y su llave.
	if err := b.host.Serve(ctx(), domain.ControlScope{
		Lobby: ipPuerta, Room: ipHost, Members: []netip.Addr{ipUno},
	}); err != nil {
		t.Fatal(err)
	}
	if err := uno.Dial(ctx(), ipHost); err != nil {
		t.Fatal(err)
	}
	esperaBool(t, uno.HostPresence(), true)
	b.esperaRegistro(t, ipUno)

	nueva, err := domain.ParseRoom("B8L3N4RY@kanpachi.accentio.dev")
	if err != nil {
		t.Fatal(err)
	}
	if err := b.host.AnnounceCode(ctx(), nueva); err != nil {
		t.Fatal(err)
	}

	llegó := espera(t, uno.Codes(), "el código nuevo")
	if llegó != nueva {
		t.Fatalf("el código que llegó es %+v", llegó)
	}
	nadaEn(t, uno.Announcements(), "el código por el canal de los anuncios")
}

// TestUnCódigoSelladoParaOtroNoSeAbreNiSeUsa: la llave del sobre es la de cada
// miembro, así que el que no es destinatario lo descarta y sigue.
func TestUnCódigoSelladoParaOtroNoSeAbreNiSeUsa(t *testing.T) {
	b := nuevoBanco(t, ipUno)
	uno := b.enSala(t, ipUno)

	ajenas, _ := newKeyPair()
	plano, _ := jsonBytes(roomMsg{InviteID: "B8L3N4RY", Seed: "kanpachi.accentio.dev"})
	sellado, err := seal(ajenas.pub[:], plano)
	if err != nil {
		t.Fatal(err)
	}
	sobre, _ := wrap(KindCode, codeMsg{Sealed: sellado})

	srv, err := b.host.host()
	if err != nil {
		t.Fatal(err)
	}
	pc, ok := srv.peer(ipUno)
	if !ok {
		t.Fatal("no hay conexión con el miembro")
	}
	if err := pc.write(sobre); err != nil {
		t.Fatal(err)
	}

	nadaEn(t, uno.Codes(), "un código que no era para esta máquina")
}

// TestUnHostNoTomaAnunciosDeNadie: aceptarlos le permitiría a un miembro
// modificado cambiarle el juego activo justo a la máquina donde se abren los
// puertos.
func TestUnHostNoTomaAnunciosDeNadie(t *testing.T) {
	b := nuevoBanco(t, ipUno)

	conn, err := b.red.desde(ipUno)(ctx(), netip.AddrPortFrom(ipHost, domain.ControlPort))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	sobre, _ := wrap(KindAnnounce, announceMsg{RoomName: "sala del intruso", GameID: "otro-juego"})
	if err := wire.NewWriter(conn, MaxMessage).Write(sobre); err != nil {
		t.Fatal(err)
	}
	nadaEn(t, b.host.Announcements(), "un anuncio que le mandaron al host")
}

// TestElInvitadoNoEscuchaNada: su deny-all queda literalmente intacto, que es la
// razón por la que la superficie se concentra en una sola máquina.
func TestElInvitadoNoEscuchaNada(t *testing.T) {
	b := nuevoBanco(t, ipUno)
	b.enSala(t, ipUno)

	if _, err := b.red.desde(ipDos)(ctx(), netip.AddrPortFrom(ipUno, domain.ControlPort)); err == nil {
		t.Fatal("el invitado tiene un oyente abierto")
	}
}

// TestCerrarNoEsperaALectores.
//
// Es la regresión del abrazo mortal: el caso de uso llama a Close con el candado
// de la sesión tomado, y nadie está leyendo los canales de salida. Si Close
// esperara a una goroutine bloqueada emitiendo, el daemon quedaría colgado con
// la sala a medio cerrar.
func TestCerrarNoEsperaALectores(t *testing.T) {
	b := nuevoBanco(t, ipUno, ipDos)
	uno := b.enSala(t, ipUno)
	b.enSala(t, ipDos)

	// Se llena el búfer de salida sin que nadie lea.
	for i := 0; i < outBuffer*3; i++ {
		_ = b.host.Announce(ctx(), domain.RoomAnnounce{RoomName: "Los panas"})
	}

	listo := make(chan struct{})
	go func() {
		_ = uno.Close()
		_ = b.host.Close()
		close(listo)
	}()
	select {
	case <-listo:
	case <-time.After(plazo):
		t.Fatal("Close se quedó esperando a alguien")
	}

	// Y es idempotente: el camino de error lo llama antes de que exista nada.
	if err := b.host.Close(); err != nil {
		t.Fatalf("el segundo Close devolvió %v", err)
	}
}

// TestLosCanalesSobrevivenAUnCierre.
//
// La sesión llama a Close en CADA salida de sala y vuelve a marcar al entrar a
// la siguiente. Cerrar los canales dejaría al supervisor tratando la segunda
// sala como una suscripción muerta, que es lo que hace con un canal cerrado.
func TestLosCanalesSobrevivenAUnCierre(t *testing.T) {
	b := nuevoBanco(t, ipUno)
	uno := b.enSala(t, ipUno)

	if err := uno.Close(); err != nil {
		t.Fatal(err)
	}
	b.vuelveAMarcar(t, uno, ipUno)

	if err := b.host.Announce(ctx(), domain.RoomAnnounce{RoomName: "Los panas", GameID: "project-zomboid"}); err != nil {
		t.Fatal(err)
	}
	a := espera(t, uno.Announcements(), "el anuncio tras volver a marcar")
	if a.GameID != "project-zomboid" {
		t.Fatalf("el anuncio llegó mal: %+v", a)
	}
}

// TestLoQueLlegaDeOtraMáquinaSeAcota: el nombre por runas y el id contra el
// alfabeto que exige un perfil. Un id que no puede existir en ningún catálogo se
// descarta acá en vez de buscarlo.
func TestLoQueLlegaDeOtraMáquinaSeAcota(t *testing.T) {
	b := nuevoBanco(t, ipUno)
	uno := b.enSala(t, ipUno)

	if err := b.host.Announce(ctx(), domain.RoomAnnounce{
		RoomName: strings.Repeat("ñ", 500), GameID: "../../etc/passwd",
	}); err != nil {
		t.Fatal(err)
	}
	a := espera(t, uno.Announcements(), "el anuncio")
	if a.GameID != "" {
		t.Fatalf("un id imposible sobrevivió: %q", a.GameID)
	}
	if len([]rune(a.RoomName)) > 64 {
		t.Fatalf("el nombre llegó sin acotar: %d runas", len([]rune(a.RoomName)))
	}
}

// TestPedirCredencialSinHaberMarcadoNoRevientaNada.
func TestPedirCredencialSinHaberMarcadoNoRevientaNada(t *testing.T) {
	b := nuevoBanco(t)
	solo := b.invitado(t, ipUno)

	if _, err := solo.RequestCredential(ctx(), domain.CredentialRequest{Name: nick(t, "humberto")}); !errors.Is(err, ErrNotDialed) {
		t.Fatalf("pedir sin marcar = %v", err)
	}
}

// TestElHostQueNoEmiteDevuelveElMotivoYNoSeCuelga.
func TestElHostQueNoEmiteDevuelveElMotivoYNoSeCuelga(t *testing.T) {
	b := nuevoBanco(t)
	b.emisor.err = errors.New("la sala no tiene direcciones libres")

	invitado := b.invitado(t, ipUno)
	if err := invitado.Dial(ctx(), ipPuerta); err != nil {
		t.Fatal(err)
	}
	_, err := invitado.RequestCredential(ctx(), domain.CredentialRequest{Name: nick(t, "humberto")})
	if err == nil || !strings.Contains(err.Error(), "direcciones libres") {
		t.Fatalf("el motivo del host no llegó: %v", err)
	}
}

// redEspía copia lo que pasa por el cable, para poder afirmar que algo NO viajó
// en claro.
type redEspía struct {
	red *red

	mu    sync.Mutex
	bytes []byte
}

func (e *redEspía) desde(origen netip.Addr) func(context.Context, netip.AddrPort) (net.Conn, error) {
	base := e.red.desde(origen)
	return func(ctx context.Context, to netip.AddrPort) (net.Conn, error) {
		conn, err := base(ctx, to)
		if err != nil {
			return nil, err
		}
		return &connEspía{Conn: conn, espía: e}, nil
	}
}

func (e *redEspía) visto() []byte {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]byte(nil), e.bytes...)
}

type connEspía struct {
	net.Conn
	espía *redEspía
}

func (c *connEspía) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if n > 0 {
		c.espía.mu.Lock()
		c.espía.bytes = append(c.espía.bytes, p[:n]...)
		c.espía.mu.Unlock()
	}
	return n, err
}

func (c *connEspía) Write(p []byte) (int, error) {
	c.espía.mu.Lock()
	c.espía.bytes = append(c.espía.bytes, p...)
	c.espía.mu.Unlock()
	return c.Conn.Write(p)
}

// TestElInvitadoVuelveAMarcarSolo.
//
// Del lado del invitado no hay servidor, solo un cliente que reconecta. Que la
// conexión esté caída es lo que alimenta la presencia del host, así que volver a
// marcar tiene que devolverla sin que nadie más intervenga.
func TestElInvitadoVuelveAMarcarSolo(t *testing.T) {
	b := nuevoBanco(t, ipUno)
	uno := b.enSala(t, ipUno)

	srv, err := b.host.host()
	if err != nil {
		t.Fatal(err)
	}
	pc, _ := srv.peer(ipUno)
	pc.close()

	esperaBool(t, uno.HostPresence(), false)
	// El primer escalón de la escalera es un segundo, así que esto tarda eso.
	select {
	case v := <-uno.HostPresence():
		if !v {
			t.Fatalf("volvió a emitir ausencia en vez de presencia")
		}
	case <-time.After(reconnect[0] + plazo):
		t.Fatal("el invitado no volvió a marcar solo")
	}
}

// TestCerrarNoFugaGoroutines.
//
// El canal es el segundo sitio del proyecto con goroutines de larga vida, y
// entrar y salir de salas es lo que más se repite en una sesión de juego. Una
// fuga por sala es un daemon que crece toda la tarde.
func TestCerrarNoFugaGoroutines(t *testing.T) {
	antes := runtime.NumGoroutine()

	for i := 0; i < 5; i++ {
		r := nuevaRed()
		reloj := &relojFalso{t: time.Now()}
		host := New(Deps{Clock: reloj, Log: logMudo{}, Listen: r.listen, Dial: r.desde(ipHost)})
		host.Attach(&emisorFalso{siguiente: ipUno, subred: netip.MustParsePrefix("100.87.3.0/24")})
		if err := host.Serve(ctx(), domain.ControlScope{
			Lobby: ipPuerta, Room: ipHost, Members: []netip.Addr{ipUno},
		}); err != nil {
			t.Fatal(err)
		}
		invitado := New(Deps{Clock: reloj, Log: logMudo{}, Listen: r.listen, Dial: r.desde(ipUno)})
		if err := invitado.Dial(ctx(), ipHost); err != nil {
			t.Fatal(err)
		}
		esperaBool(t, invitado.HostPresence(), true)

		_ = invitado.Close()
		_ = host.Close()
	}

	// Un margen: el planificador puede no haber terminado de recoger las que ya
	// devolvieron. Lo que se busca es crecimiento por sala, no el número exacto.
	hasta := time.Now().Add(plazo)
	for time.Now().Before(hasta) {
		if runtime.NumGoroutine() <= antes+2 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("goroutines antes = %d, después = %d", antes, runtime.NumGoroutine())
}
