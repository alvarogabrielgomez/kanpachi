package usecase

import (
	"bytes"
	"context"
	"errors"
	"net/netip"
	"sync"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// Los dobles de prueba de core.
//
// Existen para que estos tests corran sin admin, sin red y sin Windows, que es
// la métrica que dice si la arquitectura sigue sana. Si algún día un test de
// acá necesitara un adaptador de verdad, el problema no sería el test.

type motorFalso struct {
	mu sync.Mutex

	hostSpec  domain.HostSpec
	rdvSpec   domain.RendezvousSpec
	guestSpec domain.GuestSpec

	peers       []domain.Peer
	credentials []domain.Credential
	revocadas   []domain.CredentialID
	eventos     chan domain.EngineEvent

	// visitó guarda el orden de las llamadas. El orden importa en al menos un
	// sitio de verdad: salir del vestíbulo antes de entrar a la red real.
	visitó []string

	// credenciales es lo que devuelve el motor al emitir. El token lo pone él y
	// no el caso de uso, que es lo que hace que revocar corte la sesión.
	credenciales func() domain.Credential

	reinicios int

	errHost    error
	errRdv     error
	errJoin    error
	errRevocar error
}

func nuevoMotor() *motorFalso {
	return &motorFalso{eventos: make(chan domain.EngineEvent, 8)}
}

func (m *motorFalso) anota(s string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.visitó = append(m.visitó, s)
}

func (m *motorFalso) pasos() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.visitó...)
}

func (m *motorFalso) HostNetwork(_ context.Context, s domain.HostSpec) error {
	m.anota("host")
	m.mu.Lock()
	m.hostSpec = s
	m.mu.Unlock()
	return m.errHost
}

func (m *motorFalso) JoinRendezvous(_ context.Context, s domain.RendezvousSpec) error {
	m.anota("vestíbulo")
	m.mu.Lock()
	m.rdvSpec = s
	m.mu.Unlock()
	return m.errRdv
}

func (m *motorFalso) JoinWithCredential(_ context.Context, s domain.GuestSpec) error {
	m.anota("red-real")
	m.mu.Lock()
	m.guestSpec = s
	m.mu.Unlock()
	return m.errJoin
}

func (m *motorFalso) LeaveRendezvous(context.Context) error {
	m.anota("salir-vestíbulo")
	return nil
}

func (m *motorFalso) Leave(context.Context) error { m.anota("salir"); return nil }

func (m *motorFalso) IssueCredential(context.Context, domain.CredentialRequest) (domain.Credential, error) {
	if m.credenciales != nil {
		return m.credenciales(), nil
	}
	return domain.Credential{}, nil
}

func (m *motorFalso) RevokeCredential(_ context.Context, id domain.CredentialID) error {
	m.anota("revocar")
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.errRevocar != nil {
		return m.errRevocar
	}
	m.revocadas = append(m.revocadas, id)
	return nil
}

func (m *motorFalso) Restart(context.Context) error {
	m.anota("reiniciar")
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reinicios++
	return nil
}

func (m *motorFalso) ListCredentials(context.Context) ([]domain.Credential, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]domain.Credential(nil), m.credentials...), nil
}

func (m *motorFalso) Peers(context.Context) ([]domain.Peer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]domain.Peer(nil), m.peers...), nil
}

func (m *motorFalso) Events() <-chan domain.EngineEvent { return m.eventos }

func (m *motorFalso) Diagnostics(context.Context) (domain.NetCheck, error) {
	return domain.NetCheck{NATKind: "cone"}, nil
}

type firewallFalso struct {
	mu sync.Mutex

	aplicado     domain.RuleSet
	aplicaciones int
	purgas       int
	restauras    int
	suspendió    []domain.ForeignRule
	ajenas       []domain.ForeignRule

	errApply error
}

func (f *firewallFalso) Apply(_ context.Context, rs domain.RuleSet) error {
	if f.errApply != nil {
		return f.errApply
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.aplicado = rs
	f.aplicaciones++
	return nil
}

func (f *firewallFalso) veces() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.aplicaciones
}

func (f *firewallFalso) estado() domain.RuleSet {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.aplicado
}

func (f *firewallFalso) PurgeOwned(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.purgas++
	return nil
}

func (f *firewallFalso) AuditForeign(context.Context, domain.GameProfile) ([]domain.ForeignRule, error) {
	return f.ajenas, nil
}

func (f *firewallFalso) SuspendForeign(_ context.Context, r []domain.ForeignRule) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.suspendió = append(f.suspendió, r...)
	return nil
}

func (f *firewallFalso) RestoreForeign(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.restauras++
	return nil
}

type netcfgFalso struct {
	mu         sync.Mutex
	aplicado   domain.AdapterState
	directPlay bool
	revertió   int
}

func (n *netcfgFalso) ApplyAdapter(_ context.Context, s domain.AdapterState) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.aplicado = s
	return nil
}

func (n *netcfgFalso) RevertTweaks(context.Context) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.revertió++
	return nil
}

func (n *netcfgFalso) ProbeMTU(context.Context) (int, error) { return 1380, nil }

func (n *netcfgFalso) SetDirectPlay(_ context.Context, want bool) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.directPlay = want
	return nil
}

func (n *netcfgFalso) estado() domain.AdapterState {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.aplicado
}

type rutasFalsas struct {
	prefijos []netip.Prefix
	err      error
}

func (r rutasFalsas) LocalPrefixes(context.Context) ([]netip.Prefix, error) {
	return r.prefijos, r.err
}

type almacénFalso struct {
	mu         sync.Mutex
	builtin    []byte
	local      []byte
	escrituras int
	errLocal   error
}

func (a *almacénFalso) LoadBuiltin() ([]byte, error) {
	if len(a.builtin) == 0 {
		return nil, errors.New("no hay builtin")
	}
	return a.builtin, nil
}

func (a *almacénFalso) LoadLocal() ([]byte, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.errLocal != nil {
		return nil, a.errLocal
	}
	if len(a.local) == 0 {
		return nil, errors.New("todavía no hay local")
	}
	return a.local, nil
}

func (a *almacénFalso) SaveLocal(raw []byte) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.local = append([]byte(nil), raw...)
	a.escrituras++
	return nil
}

type bibliotecaFalsa struct {
	refs []domain.GameRef
	err  error
}

func (b bibliotecaFalsa) Installed(context.Context) ([]domain.GameRef, error) {
	return b.refs, b.err
}

type registroFalso struct {
	mu sync.Mutex

	emitidos  []domain.InviteID
	publicado []byte
	siguiente string
	seed      string
	err       error
}

func (r *registroFalso) Open(_ context.Context, sealed []byte) (domain.Room, error) {
	if r.err != nil {
		return domain.Room{}, r.err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.publicado = append([]byte(nil), sealed...)

	txt := r.siguiente
	if txt == "" {
		txt = "A7K2M9QX"
	}
	id, err := domain.ParseInviteID(txt)
	if err != nil {
		return domain.Room{}, err
	}
	r.emitidos = append(r.emitidos, id)
	seed := r.seed
	if seed == "" {
		seed = domain.DefaultSeedHost
	}
	return domain.Room{InviteID: id, Seed: seed}, nil
}

func (r *registroFalso) Lookup(context.Context, domain.InviteID) ([]byte, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.publicado, 0, nil
}

func (r *registroFalso) Publish(_ context.Context, _ domain.InviteID, sealed []byte) error {
	if r.err != nil {
		return r.err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.publicado = append([]byte(nil), sealed...)
	return nil
}

type controlFalso struct {
	mu sync.Mutex

	sirviendo bool
	scope     domain.ControlScope
	alcance   []netip.Addr
	marcados  []netip.Addr
	// fallarDesde hace fallar Dial a partir de la n-ésima llamada, 1-indexada.
	fallarDesde     int
	presencia       chan bool
	anuncios        []domain.RoomAnnounce
	entrantes       chan domain.RoomAnnounce
	avisos          []avisoFalso
	avisados        []netip.Addr
	avisosEntrantes chan domain.RoomNotice
	códigos         []domain.Room
	códigosEntantes chan domain.Room
	errNotify       error
	errCódigo       error
	credencial      domain.Credential
	cierres         int

	errServe error
	errDial  error
	errReq   error
}

func nuevoControl() *controlFalso {
	return &controlFalso{
		presencia:       make(chan bool, 4),
		entrantes:       make(chan domain.RoomAnnounce, 4),
		avisosEntrantes: make(chan domain.RoomNotice, 4),
		códigosEntantes: make(chan domain.Room, 4),
	}
}

// avisoFalso guarda el aviso y CUÁNDO se mandó, contando las otras llamadas al
// canal. Es la única forma de comprobar que el aviso de expulsión sale antes
// de que al expulsado se le corte nada.
type avisoFalso struct {
	a    netip.Addr
	n    domain.RoomNotice
	tras int
}

func (c *controlFalso) Serve(_ context.Context, scope domain.ControlScope) error {
	if c.errServe != nil {
		return c.errServe
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sirviendo = true
	c.scope = scope
	c.alcance = append([]netip.Addr(nil), scope.Members...)
	return nil
}

func (c *controlFalso) Dial(_ context.Context, host netip.Addr) error {
	if c.errDial != nil {
		return c.errDial
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fallarDesde > 0 && len(c.marcados)+1 >= c.fallarDesde {
		return errors.New("no se pudo conectar")
	}
	c.marcados = append(c.marcados, host)
	return nil
}

func (c *controlFalso) HostPresence() <-chan bool { return c.presencia }

func (c *controlFalso) Announce(_ context.Context, a domain.RoomAnnounce) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.anuncios = append(c.anuncios, a)
	return nil
}

func (c *controlFalso) Announcements() <-chan domain.RoomAnnounce { return c.entrantes }

func (c *controlFalso) AnnounceCode(_ context.Context, r domain.Room) error {
	if c.errCódigo != nil {
		return c.errCódigo
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.códigos = append(c.códigos, r)
	return nil
}

func (c *controlFalso) Codes() <-chan domain.Room { return c.códigosEntantes }

func (c *controlFalso) códigosRepartidos() []domain.Room {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]domain.Room(nil), c.códigos...)
}

func (c *controlFalso) Notify(_ context.Context, to netip.Addr, n domain.RoomNotice) error {
	if c.errNotify != nil {
		return c.errNotify
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.avisos = append(c.avisos, avisoFalso{a: to, n: n, tras: len(c.marcados) + len(c.anuncios)})
	c.avisados = append(c.avisados, to)
	return nil
}

func (c *controlFalso) Notices() <-chan domain.RoomNotice { return c.avisosEntrantes }

func (c *controlFalso) últimoAviso() (avisoFalso, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.avisos) == 0 {
		return avisoFalso{}, false
	}
	return c.avisos[len(c.avisos)-1], true
}

func (c *controlFalso) últimoAnuncio() (domain.RoomAnnounce, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.anuncios) == 0 {
		return domain.RoomAnnounce{}, false
	}
	return c.anuncios[len(c.anuncios)-1], true
}

func (c *controlFalso) RequestCredential(context.Context, domain.CredentialRequest) (domain.Credential, error) {
	if c.errReq != nil {
		return domain.Credential{}, c.errReq
	}
	return c.credencial, nil
}

func (c *controlFalso) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cierres++
	c.sirviendo = false
	return nil
}

func (c *controlFalso) alcanceActual() []netip.Addr {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]netip.Addr(nil), c.alcance...)
}

type auditoríaFalsa struct {
	perfiles []domain.FirewallProfileState
	intactas bool
	mapeos   []domain.PortMapping

	// err rompe los TRES métodos, que es el adaptador entero caído.
	err error
	// Y estos rompen uno solo. Hacen falta porque las tres comprobaciones no
	// valen lo mismo: las dos locales sostienen la promesa y la del router falla
	// en la mayoría de las máquinas, así que solo las dos primeras pueden
	// levantar el aviso de auditoría caída. Sin errores por método, esa
	// diferencia no se puede afirmar en un test.
	errPerfiles error
	errIntactas error
	errMapeos   error
}

func (a *auditoríaFalsa) FirewallEnabled(context.Context) ([]domain.FirewallProfileState, error) {
	return a.perfiles, primerError(a.errPerfiles, a.err)
}

func (a *auditoríaFalsa) OwnRulesIntact(context.Context) (bool, error) {
	return a.intactas, primerError(a.errIntactas, a.err)
}

func (a *auditoríaFalsa) RouterMappings(context.Context) ([]domain.PortMapping, error) {
	return a.mapeos, primerError(a.errMapeos, a.err)
}

func primerError(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

type inspectorFalso struct {
	sockets []domain.Listener
	err     error
}

func (i inspectorFalso) Snapshot(context.Context, domain.ProcessRef) ([]domain.Listener, error) {
	return i.sockets, i.err
}

// relojFijo no avanza salvo que un test lo mueva. Sin esto, probar los veinte
// minutos de la decisión 20 costaría veinte minutos.
type relojFijo struct {
	mu    sync.Mutex
	ahora time.Time
}

func (c *relojFijo) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ahora
}

func (c *relojFijo) avanza(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ahora = c.ahora.Add(d)
}

type logMudo struct{}

func (logMudo) Info(string, ...any)  {}
func (logMudo) Warn(string, ...any)  {}
func (logMudo) Error(string, ...any) {}

// banco es todo el escenario armado, para que cada test toque solo lo suyo.
type banco struct {
	deps      Deps
	motor     *motorFalso
	firewall  *firewallFalso
	netcfg    *netcfgFalso
	almacén   *almacénFalso
	estado    *estadoFalso
	registro  *registroFalso
	control   *controlFalso
	auditoría *auditoríaFalsa
	reloj     *relojFijo
	sesión    *Session
}

// estadoFalso es el disco de room.json y last-room.json.
//
// Guarda bytes y no structs, igual que el puerto de verdad: el decodificador
// estricto vive en el dominio y estos tests lo ejercitan de punta a punta.
type estadoFalso struct {
	mu sync.Mutex

	sala     []byte
	última   []byte
	borrados int

	errGuardar error
}

func (e *estadoFalso) LoadRoom() ([]byte, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.sala == nil {
		return nil, errors.New("no hay sala guardada")
	}
	return append([]byte(nil), e.sala...), nil
}

func (e *estadoFalso) SaveRoom(raw []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.errGuardar != nil {
		return e.errGuardar
	}
	e.sala = append([]byte(nil), raw...)
	return nil
}

func (e *estadoFalso) ClearRoom() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.sala = nil
	e.borrados++
	return nil
}

func (e *estadoFalso) LoadLast() ([]byte, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.última == nil {
		return nil, errors.New("no hay última sala")
	}
	return append([]byte(nil), e.última...), nil
}

func (e *estadoFalso) SaveLast(raw []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.errGuardar != nil {
		return e.errGuardar
	}
	e.última = append([]byte(nil), raw...)
	return nil
}

func (e *estadoFalso) ClearLast() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.última = nil
	return nil
}

func (e *estadoFalso) salaGuardada() []byte {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]byte(nil), e.sala...)
}

const catálogoDePrueba = `{"kanpachi_catalog":1,"profiles":[
  {"id":"project-zomboid","schema":2,"name":"Project Zomboid",
   "detect":{"steam_appid":108600},
   "host_ports":[{"proto":"udp","range":"16261-16262"}],"client_ports":[],
   "system_tweaks":{"broadcast_route":false,"multicast_route":true,"prefer_ipv4":false,"directplay":false},
   "connect_hint":{"kind":"direct_ip","text_es":"Join, escribe la IP del host"}},
  {"id":"juego-de-malla","schema":2,"name":"Juego de malla",
   "host_ports":[{"proto":"udp","range":"7777"}],"client_ports":[{"proto":"tcp","range":"7777"}],
   "connect_hint":{"kind":"lan_browser","text_es":"aparece solo"}}
]}`

func nuevoBanco(t interface{ Fatalf(string, ...any) }) *banco {
	b := &banco{
		motor:     nuevoMotor(),
		firewall:  &firewallFalso{},
		netcfg:    &netcfgFalso{},
		almacén:   &almacénFalso{builtin: []byte(catálogoDePrueba)},
		estado:    &estadoFalso{},
		registro:  &registroFalso{},
		control:   nuevoControl(),
		auditoría: &auditoríaFalsa{intactas: true},
		reloj:     &relojFijo{ahora: time.Date(2026, 8, 2, 20, 0, 0, 0, time.UTC)},
	}
	b.deps = Deps{
		Engine:    b.motor,
		Firewall:  b.firewall,
		NetCfg:    b.netcfg,
		Routes:    rutasFalsas{prefijos: []netip.Prefix{netip.MustParsePrefix("192.168.1.0/24")}},
		Store:     b.almacén,
		State:     b.estado,
		Library:   bibliotecaFalsa{},
		Directory: b.registro,
		Control:   b.control,
		Audit:     b.auditoría,
		Inspector: inspectorFalso{},
		Clock:     b.reloj,
		Log:       logMudo{},
		// Un lector constante hace que la subred y las claves salgan siempre
		// iguales, y que el test sea el mismo en cada ejecución.
		Rand: bytes.NewReader(bytes.Repeat([]byte{0x11}, 1<<16)),
	}
	s, err := NewSession(context.Background(), b.deps)
	if err != nil {
		t.Fatalf("no se pudo montar la sesión: %v", err)
	}
	b.sesión = s
	return b
}

func nick(t interface{ Fatalf(string, ...any) }, s string) domain.Nickname {
	n, err := domain.ParseNickname(s)
	if err != nil {
		t.Fatalf("nick %q: %v", s, err)
	}
	return n
}
