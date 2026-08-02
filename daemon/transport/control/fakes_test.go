package control

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// La red falsa.
//
// El canal habla con direcciones virtuales que no existen en la máquina donde
// corre CI, así que el test trae su propia conmutación: un mapa de direcciones a
// oyentes y conexiones de net.Pipe con la dirección remota que el test decida.
//
// Esto es lo que hace que el rechazo por dirección se pueda PROBAR. En un
// loopback de verdad todas las conexiones vienen de 127.0.0.1, y el test no
// podría distinguir a un miembro de un desconocido.
type red struct {
	mu      sync.Mutex
	oyentes map[netip.AddrPort]*oyenteFalso
}

func nuevaRed() *red { return &red{oyentes: map[netip.AddrPort]*oyenteFalso{}} }

func (r *red) listen(_ context.Context, at netip.AddrPort) (net.Listener, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ya := r.oyentes[at]; ya {
		return nil, errors.New("esa dirección ya está ocupada")
	}
	ln := &oyenteFalso{at: at, entrantes: make(chan net.Conn, 8), cerrado: make(chan struct{}), red: r}
	r.oyentes[at] = ln
	return ln, nil
}

// desde arma el marcador de una máquina con esa dirección virtual.
func (r *red) desde(origen netip.Addr) func(context.Context, netip.AddrPort) (net.Conn, error) {
	return func(_ context.Context, to netip.AddrPort) (net.Conn, error) {
		r.mu.Lock()
		ln := r.oyentes[to]
		r.mu.Unlock()
		if ln == nil {
			return nil, errors.New("no hay nadie escuchando en " + to.String())
		}

		mío, suyo := net.Pipe()
		select {
		case ln.entrantes <- &connFalsa{Conn: suyo, remota: netip.AddrPortFrom(origen, 52000)}:
			return &connFalsa{Conn: mío, remota: to}, nil
		case <-ln.cerrado:
			return nil, net.ErrClosed
		}
	}
}

type oyenteFalso struct {
	at        netip.AddrPort
	entrantes chan net.Conn
	cerrado   chan struct{}
	una       sync.Once
	red       *red
}

func (l *oyenteFalso) Accept() (net.Conn, error) {
	select {
	case c := <-l.entrantes:
		return c, nil
	case <-l.cerrado:
		return nil, net.ErrClosed
	}
}

func (l *oyenteFalso) Close() error {
	l.una.Do(func() {
		close(l.cerrado)
		l.red.mu.Lock()
		delete(l.red.oyentes, l.at)
		l.red.mu.Unlock()
	})
	return nil
}

func (l *oyenteFalso) Addr() net.Addr { return direcciónFalsa(l.at) }

type connFalsa struct {
	net.Conn
	remota netip.AddrPort
}

func (c *connFalsa) RemoteAddr() net.Addr { return direcciónFalsa(c.remota) }

type direcciónFalsa netip.AddrPort

func (direcciónFalsa) Network() string  { return "tcp" }
func (d direcciónFalsa) String() string { return netip.AddrPort(d).String() }

// El emisor falso, que es lo único que el canal le pide al núcleo.
type emisorFalso struct {
	mu        sync.Mutex
	siguiente netip.Addr
	subred    netip.Prefix
	err       error
	pedidos   []domain.CredentialRequest
}

func (e *emisorFalso) IssueCredentialFor(_ context.Context, req domain.CredentialRequest) (domain.Credential, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.pedidos = append(e.pedidos, req)
	if e.err != nil {
		return domain.Credential{}, e.err
	}
	ip := e.siguiente
	e.siguiente = e.siguiente.Next()
	return domain.Credential{
		ID:          domain.CredentialID("cred-" + ip.String()),
		Token:       "token-secretísimo-" + ip.String(),
		NetworkName: "red-real-de-la-sala",
		Name:        req.Name,
		VirtualIP:   ip,
		Subnet:      e.subred,
		IssuedAt:    time.Date(2026, 8, 2, 20, 0, 0, 0, time.UTC),
		ExpiresAt:   time.Date(2026, 8, 3, 20, 0, 0, 0, time.UTC),
	}, nil
}

func (e *emisorFalso) últimoPedido() (domain.CredentialRequest, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.pedidos) == 0 {
		return domain.CredentialRequest{}, false
	}
	return e.pedidos[len(e.pedidos)-1], true
}

type relojFalso struct {
	mu sync.Mutex
	t  time.Time
}

func (r *relojFalso) Now() time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.t
}

func (r *relojFalso) mueve(a time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.t = a
}

type logMudo struct{}

func (logMudo) Info(string, ...any)  {}
func (logMudo) Warn(string, ...any)  {}
func (logMudo) Error(string, ...any) {}

// banco es un host servido y la red por la que se le llega.
type banco struct {
	red    *red
	host   *Channel
	emisor *emisorFalso
	reloj  *relojFalso
	subred netip.Prefix
}

var (
	ipHost  = netip.MustParseAddr("100.87.3.1")
	ipUno   = netip.MustParseAddr("100.87.3.2")
	ipDos   = netip.MustParseAddr("100.87.3.3")
	ipNadie = netip.MustParseAddr("100.87.3.99")
)

func nuevoBanco(t *testing.T, miembros ...netip.Addr) *banco {
	t.Helper()
	r := nuevaRed()
	reloj := &relojFalso{t: time.Now()}
	subred := netip.MustParsePrefix("100.87.3.0/24")
	emisor := &emisorFalso{siguiente: ipUno, subred: subred}

	host := New(Deps{Clock: reloj, Log: logMudo{}, Listen: r.listen, Dial: r.desde(ipHost)})
	host.Attach(emisor)
	if err := host.Serve(context.Background(), domain.ControlScope{
		Lobby: domain.RendezvousHostAddress, Room: ipHost, Members: miembros,
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = host.Close() })

	return &banco{red: r, host: host, emisor: emisor, reloj: reloj, subred: subred}
}

// invitado arma el canal de un invitado con esa dirección virtual.
func (b *banco) invitado(t *testing.T, ip netip.Addr) *Channel {
	t.Helper()
	c := New(Deps{Clock: b.reloj, Log: logMudo{}, Listen: b.red.listen, Dial: b.red.desde(ip)})
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// enSala marca a la dirección del host en la sala y espera a que el host tenga
// esa conexión registrada.
//
// Esperar el REGISTRO y no solo la presencia, porque son dos lados: el invitado
// se da por conectado en cuanto el socket queda armado, y el host la registra en
// su goroutine de atender. Sin esta espera, un Announce inmediato saldría antes
// de que el destinatario exista para el host, y el test fallaría por una carrera
// del banco de pruebas y no por el código.
func (b *banco) enSala(t *testing.T, ip netip.Addr) *Channel {
	t.Helper()
	c := b.invitado(t, ip)
	if err := c.Dial(context.Background(), ipHost); err != nil {
		t.Fatalf("marcar a la sala desde %s: %v", ip, err)
	}
	esperaBool(t, c.HostPresence(), true)
	b.esperaRegistro(t, ip)
	return c
}

// vuelveAMarcar rehace la conexión de un invitado y espera el reemplazo.
func (b *banco) vuelveAMarcar(t *testing.T, c *Channel, ip netip.Addr) {
	t.Helper()
	vieja, _ := mustHost(t, b).peer(ip)
	if err := c.Dial(context.Background(), ipHost); err != nil {
		t.Fatal(err)
	}
	esperaBool(t, c.HostPresence(), true)
	b.esperaReemplazo(t, ip, vieja)
}

func mustHost(t *testing.T, b *banco) *server {
	t.Helper()
	srv, err := b.host.host()
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

// esperaRegistro espera a que el host tenga una conexión viva con esa
// dirección.
func (b *banco) esperaRegistro(t *testing.T, ip netip.Addr) *peerConn {
	t.Helper()
	return b.esperaReemplazo(t, ip, nil)
}

// esperaReemplazo espera a que la conexión registrada sea OTRA que la de antes.
//
// Existe aparte porque volver a marcar deja la vieja registrada hasta que el
// host atiende la nueva, y un anuncio en esa ventana saldría por un socket que
// ya nadie lee. Es una carrera del banco de pruebas, y esperar el reemplazo es
// lo que la saca sin dormir a ciegas.
func (b *banco) esperaReemplazo(t *testing.T, ip netip.Addr, vieja *peerConn) *peerConn {
	t.Helper()
	srv, err := b.host.host()
	if err != nil {
		t.Fatal(err)
	}
	hasta := time.Now().Add(plazo)
	for time.Now().Before(hasta) {
		if pc, ok := srv.peer(ip); ok && pc != vieja {
			return pc
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("el host no registró la conexión de %s en %s", ip, plazo)
	return nil
}

func ctx() context.Context { return context.Background() }

// Los ayudantes de espera. Sin dormir: o llega, o el test falla por plazo.
const plazo = 2 * time.Second

func esperaBool(t *testing.T, ch <-chan bool, quiero bool) {
	t.Helper()
	select {
	case v := <-ch:
		if v != quiero {
			t.Fatalf("llegó %v y se esperaba %v", v, quiero)
		}
	case <-time.After(plazo):
		t.Fatalf("no llegó %v por el canal en %s", quiero, plazo)
	}
}

func espera[T any](t *testing.T, ch <-chan T, qué string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(plazo):
		t.Fatalf("no llegó %s en %s", qué, plazo)
	}
	var cero T
	return cero
}

func nadaEn[T any](t *testing.T, ch <-chan T, qué string) {
	t.Helper()
	select {
	case v := <-ch:
		t.Fatalf("llegó %s y no tenía que llegar: %v", qué, v)
	case <-time.After(150 * time.Millisecond):
	}
}

func nick(t *testing.T, s string) domain.Nickname {
	t.Helper()
	n, err := domain.ParseNickname(s)
	if err != nil {
		t.Fatal(err)
	}
	return n
}
