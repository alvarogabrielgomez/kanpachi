package usecase

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/core/port"
	"github.com/accentiostudios/kanpachi/core/timing"
)

// Los dobles de prueba de core.
//
// Existen para que estos tests corran sin admin, sin red y sin Windows, que es
// la métrica que dice si la arquitectura sigue sana. Si algún día un test de
// acá necesitara un adaptador de verdad, el problema no sería el test.

type mockMotor struct {
	mu sync.Mutex

	hostSpec  domain.HostSpec
	rdvSpec   domain.RendezvousSpec
	guestSpec domain.GuestSpec

	peers       []domain.Peer
	credentials []domain.Credential
	revocadas   []domain.CredentialID
	renovadas   []domain.CredentialID
	eventos     chan domain.EngineEvent

	// ahora es el reloj con el que este falso fecha lo que renueva. El banco le
	// pasa el mismo que ve la sesión, para que adelantarlo mueva las dos cosas.
	// En nil usa el de verdad, que sirve para los tests a los que la fecha les
	// da igual.
	ahora func() time.Time

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
	errRenovar error
}

func nuevoMotor() *mockMotor {
	return &mockMotor{eventos: make(chan domain.EngineEvent, 8)}
}

func (m *mockMotor) anota(s string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.visitó = append(m.visitó, s)
}

func (m *mockMotor) pasos() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.visitó...)
}

func (m *mockMotor) HostNetwork(_ context.Context, s domain.HostSpec) error {
	m.anota("host")
	m.mu.Lock()
	m.hostSpec = s
	m.mu.Unlock()
	return m.errHost
}

func (m *mockMotor) JoinRendezvous(_ context.Context, s domain.RendezvousSpec) error {
	m.anota("vestíbulo")
	m.mu.Lock()
	m.rdvSpec = s
	m.mu.Unlock()
	return m.errRdv
}

func (m *mockMotor) JoinWithCredential(_ context.Context, s domain.GuestSpec) error {
	m.anota("red-real")
	m.mu.Lock()
	m.guestSpec = s
	m.mu.Unlock()
	return m.errJoin
}

func (m *mockMotor) LeaveRendezvous(context.Context) error {
	m.anota("salir-vestíbulo")
	return nil
}

func (m *mockMotor) Leave(context.Context) error { m.anota("salir"); return nil }

func (m *mockMotor) IssueCredential(context.Context, domain.CredentialRequest) (domain.Credential, error) {
	if m.credenciales != nil {
		return m.credenciales(), nil
	}
	return domain.Credential{}, nil
}

func (m *mockMotor) RenewCredential(_ context.Context, id domain.CredentialID, ttl time.Duration) (time.Time, error) {
	m.anota("renovar")
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.errRenovar != nil {
		return time.Time{}, m.errRenovar
	}
	m.renovadas = append(m.renovadas, id)
	// Fecha contada desde AHORA, como el motor de verdad, y con el reloj del
	// banco para que adelantarlo mueva las dos cosas a la vez.
	ahora := time.Now
	if m.ahora != nil {
		ahora = m.ahora
	}
	return ahora().Add(ttl), nil
}

func (m *mockMotor) RevokeCredential(_ context.Context, id domain.CredentialID) error {
	m.anota("revocar")
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.errRevocar != nil {
		return m.errRevocar
	}
	m.revocadas = append(m.revocadas, id)
	return nil
}

func (m *mockMotor) Restart(context.Context) error {
	m.anota("reiniciar")
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reinicios++
	return nil
}

// ListCredentials devuelve lo mismo que el adaptador real: id y vencimiento,
// SIN dirección virtual.
//
// El borrado de `VirtualIP` es el punto entero de este método. El motor no sabe
// a qué dirección fue cada credencial, y mientras el falso sí lo sabía, ocho
// tests de expulsión daban verde sobre un producto donde expulsar no encontraba
// a nadie y ningún invitado podía entrar. Un falso que puede más que el real no
// prueba el producto, prueba el falso.
func (m *mockMotor) ListCredentials(context.Context) ([]domain.Credential, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := append([]domain.Credential(nil), m.credentials...)
	for i := range out {
		out[i].VirtualIP = netip.Addr{}
	}
	return out, nil
}

func (m *mockMotor) Peers(context.Context) ([]domain.Peer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]domain.Peer(nil), m.peers...), nil
}

func (m *mockMotor) Events() <-chan domain.EngineEvent { return m.eventos }

func (m *mockMotor) Diagnostics(context.Context) (domain.NetCheck, error) {
	return domain.NetCheck{NATKind: "cone"}, nil
}

type mockFirewall struct {
	mu sync.Mutex

	aplicado     domain.RuleSet
	aplicaciones int
	purgas       int
	restauras    int
	suspendió    []domain.ForeignRule
	ajenas       []domain.ForeignRule

	cuarentena []domain.QuarantineRule
	// cuarentenaTrasPurgas lleva cuántas purgas se habían hecho en cada llamada
	// a la cuarentena. Un cero ahí es la afirmación de que fue primero.
	cuarentenaTrasPurgas []int
	// cuarentenaRetirada cuenta las retiradas a pedido: la afirmación es que
	// SOLO la decisión del usuario llega acá, jamás un camino automático.
	cuarentenaRetirada int

	// acotado es a qué está acotada la compuerta ahora, y vacío es sin acotar.
	acotado netip.Prefix
	// acotadoLobby es el /24 del vestíbulo con el que se acotó. Vacío es que se
	// pidió solo la sala, que es lo normal en un invitado.
	acotadoLobby netip.Prefix
	vínculo      domain.RoomBinding
	vínculos     []domain.RoomBinding
	// abrióSinCompuerta es la afirmación que este falso existe para poder hacer:
	// si alguna vez se pidió abrir puertos con la compuerta suelta, la lista de
	// permitidos volvió a ser aditiva y la sala no estaba contenida.
	abrióSinCompuerta bool

	// bloqueos is what InboundBlocked answers: the foreign firewall a test
	// wants in the way. Empty keeps every scenario on the clear path.
	bloqueos []domain.FirewallBlock
	// abrió is what AllowAdapters was asked to open, and retiró how many times
	// the book was paid.
	abrió  []domain.FirewallBlock
	retiró int

	errApply      error
	errCuarentena error
	errBind       error
}

func (f *mockFirewall) Apply(_ context.Context, rs domain.RuleSet) error {
	if f.errApply != nil {
		return f.errApply
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(rs.Rules) > 0 && !f.acotado.IsValid() {
		f.abrióSinCompuerta = true
	}
	f.aplicado = rs
	f.aplicaciones++
	return nil
}

func (f *mockFirewall) BindRoom(
	_ context.Context, room, lobby netip.Prefix, with domain.RoomBinding,
) error {
	if f.errBind != nil {
		return f.errBind
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.acotado, f.vínculo = room, with
	f.acotadoLobby = lobby
	f.vínculos = append(f.vínculos, with)
	return nil
}

func (f *mockFirewall) UnbindRoom() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.acotado, f.vínculo = netip.Prefix{}, 0
}

func (f *mockFirewall) alcance() (netip.Prefix, domain.RoomBinding) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.acotado, f.vínculo
}

func (f *mockFirewall) veces() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.aplicaciones
}

func (f *mockFirewall) estado() domain.RuleSet {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.aplicado
}

func (f *mockFirewall) ApplyBaseQuarantine(_ context.Context, rules []domain.QuarantineRule) error {
	if f.errCuarentena != nil {
		return f.errCuarentena
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	// Se anota CUÁNTAS purgas había cuando se llamó, y no solo que se llamó.
	// El orden es lo que se quiere afirmar: la cuarentena tiene que entrar antes
	// de que la purga deje la máquina sin las reglas de la sala anterior.
	f.cuarentenaTrasPurgas = append(f.cuarentenaTrasPurgas, f.purgas)
	f.cuarentena = rules
	return nil
}

func (f *mockFirewall) RemoveBaseQuarantineAtUserRequest(context.Context) error {
	if f.errCuarentena != nil {
		return f.errCuarentena
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cuarentenaRetirada++
	f.cuarentena = nil
	return nil
}

func (f *mockFirewall) cuarentenaPuesta() []domain.QuarantineRule {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cuarentena
}

func (f *mockFirewall) PurgeOwned(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.purgas++
	return nil
}

func (f *mockFirewall) AuditForeign(context.Context, domain.GameProfile) ([]domain.ForeignRule, error) {
	return f.ajenas, nil
}

func (f *mockFirewall) SuspendForeign(_ context.Context, r []domain.ForeignRule) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.suspendió = append(f.suspendió, r...)
	return nil
}

func (f *mockFirewall) RestoreForeign(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.restauras++
	return nil
}

// The foreign-firewall gate: the mock never blocks, which keeps every existing
// scenario on the clear path. A test that wants a block sets `bloqueos`.
func (f *mockFirewall) InboundBlocked(context.Context) ([]domain.FirewallBlock, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bloqueos, nil
}

func (f *mockFirewall) AllowAdapters(_ context.Context, blocks []domain.FirewallBlock) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.abrió = append(f.abrió, blocks...)
	return nil
}

func (f *mockFirewall) WithdrawAdapters(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.retiró++
	return nil
}

type mockNetcfg struct {
	mu         sync.Mutex
	aplicado   domain.AdapterState
	directPlay bool
	revertió   int
}

func (n *mockNetcfg) ApplyAdapter(_ context.Context, s domain.AdapterState) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.aplicado = s
	return nil
}

func (n *mockNetcfg) RevertTweaks(context.Context) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.revertió++
	return nil
}

func (n *mockNetcfg) ProbeMTU(context.Context) (int, error) { return 1380, nil }

func (n *mockNetcfg) SetDirectPlay(_ context.Context, want bool) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.directPlay = want
	return nil
}

func (n *mockNetcfg) estado() domain.AdapterState {
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

type mockCatalog struct {
	mu         sync.Mutex
	builtin    []byte
	local      []byte
	escrituras int
	errLocal   error
}

func (a *mockCatalog) LoadBuiltin() ([]byte, error) {
	if len(a.builtin) == 0 {
		return nil, errors.New("no hay builtin")
	}
	return a.builtin, nil
}

func (a *mockCatalog) LoadLocal() ([]byte, error) {
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

func (a *mockCatalog) SaveLocal(raw []byte) error {
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

type mockRegistry struct {
	mu sync.Mutex

	emitidos  []domain.InviteID
	publicado []byte
	// publicaciones cuenta las llamadas a Publish, incluidas las que fallan. Es
	// lo que distingue "no republicó" de "republicó y le contestaron que no".
	publicaciones int
	// cerrados are the rooms closed in the registry, in order.
	cerrados  []domain.InviteID
	siguiente string
	seed      string
	// pruebas son los proofs que llegaron a Authenticate, en orden.
	pruebas []string
	err     error
}

// registrosFalsos es la fábrica, y devuelve SIEMPRE el mismo registro.
//
// Es deliberado y es lo que hace que estos tests sigan hablando de "el
// registro" en singular: lo que se ejercita acá es la política de los casos de
// uso, no el reparto por host, que es del adaptador y se prueba en su paquete.
// Un falso que devolviera uno distinto por seed obligaría a cada test a declarar
// hosts que no le importan.
type registrosFalsos struct {
	reg *mockRegistry
	// sinPropio simula una máquina que todavía no configuró registro, que es lo
	// que devuelve [port.ErrNoOwnSeed] al crear.
	//
	// Es un puntero para que [registrosFalsos.SetOwn] pueda apagarlo de verdad.
	// Un falso con receptor por valor que aceptara el cambio y no lo aplicara
	// haría pasar un test de "configurar y crear" sin que configurar sirviera de
	// nada, que es la peor clase de falso.
	sinPropio *bool
}

func (f registrosFalsos) Own() (port.RoomDirectory, error) {
	if f.sinPropio != nil && *f.sinPropio {
		return nil, port.ErrNoOwnSeed
	}
	return f.reg, nil
}

func (f registrosFalsos) For(string) (port.RoomDirectory, error) { return f.reg, nil }

// SetOwn apaga la simulación de "sin registro": después de configurar uno, esta
// máquina puede abrir salas.
func (f registrosFalsos) SetOwn(string) {
	if f.sinPropio != nil {
		*f.sinPropio = false
	}
}

// Seed es con qué registro habla este falso.
//
// Antes su valor por defecto era la cadena vacía a propósito, para que el fallo
// temprano de `JoinRoom` **se saltara a sí mismo** en los tests que no lo
// ejercitaban: el código traía un seed y este no era el mismo. Esa rama
// desapareció con la fábrica, así que ahora este valor es solo lo que se
// imprime.
func (r *mockRegistry) Seed() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.seed == "" {
		return "seed.midominio.com"
	}
	return r.seed
}

// Reachable contesta que sí salvo que el test haya declarado al registro
// caído, que es lo mismo que hacen los otros tres métodos con `err`.
//
// Un campo aparte sería otra palanca que recordar: con `err` puesto, este falso
// representa un registro que no contesta A NADA, que es el caso que importa.
func (r *mockRegistry) Reachable(context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.err
}

func (r *mockRegistry) Open(_ context.Context, sealed []byte) (domain.Room, error) {
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
		seed = "seed.midominio.com"
	}
	return domain.Room{InviteID: id, Seed: seed}, nil
}

func (r *mockRegistry) Lookup(context.Context, domain.InviteID) (domain.InviteLookup, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	// No key and no signature: this double belongs to the use cases that do not
	// look at provenance, so the verdict that comes out is `CardUnverified`,
	// which is the truth about a registry that does not say which key it serves
	// under.
	return domain.InviteLookup{Sealed: r.publicado}, nil
}

func (r *mockRegistry) Publish(_ context.Context, _ domain.InviteID, sealed []byte) error {
	r.mu.Lock()
	r.publicaciones++
	r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.publicado = append([]byte(nil), sealed...)
	return nil
}

// Close records which rooms were closed in the registry, in order.
//
// They are recorded even when the fake is declared down: closing is best-effort,
// and what has to be assertable is that it was ATTEMPTED, which is exactly what
// tells "never called it" apart from "called it and the registry did not answer".
func (r *mockRegistry) Retire(_ context.Context, id domain.InviteID) error {
	r.mu.Lock()
	r.cerrados = append(r.cerrados, id)
	r.mu.Unlock()
	return r.err
}

type mockControl struct {
	mu sync.Mutex

	sirviendo bool
	scope     domain.ControlScope
	alcance   []netip.Addr
	marcados  []netip.Addr
	// conectados son las direcciones con el canal de la sala abierto, que es lo
	// que el host suma a la tabla del motor. Ver [Session.withAdmittedLocked].
	conectados []netip.Addr
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

	// El canario.
	pedidosCanario   []canarioPedido
	informesMandados []domain.CanaryReport
	informesCanario  chan domain.CanaryReport
	pedidosEntrantes chan domain.CanaryRequest
	errCanario       error
	errNotify        error
	errCódigo        error
	credencial       domain.Credential
	cierres          int

	errServe error
	errDial  error
	errReq   error
}

func nuevoControl() *mockControl {
	return &mockControl{
		presencia:        make(chan bool, 4),
		entrantes:        make(chan domain.RoomAnnounce, 4),
		avisosEntrantes:  make(chan domain.RoomNotice, 4),
		códigosEntantes:  make(chan domain.Room, 4),
		informesCanario:  make(chan domain.CanaryReport, 4),
		pedidosEntrantes: make(chan domain.CanaryRequest, 4),
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

func (c *mockControl) Serve(_ context.Context, scope domain.ControlScope) error {
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

func (c *mockControl) Dial(_ context.Context, host netip.Addr) error {
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

func (c *mockControl) HostPresence() <-chan bool { return c.presencia }

func (c *mockControl) Announce(_ context.Context, a domain.RoomAnnounce) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.anuncios = append(c.anuncios, a)
	return nil
}

func (c *mockControl) Announcements() <-chan domain.RoomAnnounce { return c.entrantes }

func (c *mockControl) AnnounceCode(_ context.Context, r domain.Room) error {
	if c.errCódigo != nil {
		return c.errCódigo
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.códigos = append(c.códigos, r)
	return nil
}

func (c *mockControl) Codes() <-chan domain.Room { return c.códigosEntantes }

// El canario. `pedidos` guarda a quién se le pidió y qué, que es lo que un test
// necesita para afirmar que el destino salió de la conexión y no de un campo.
func (c *mockControl) RequestCanary(_ context.Context, to netip.Addr, req domain.CanaryRequest) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pedidosCanario = append(c.pedidosCanario, canarioPedido{A: to, Req: req})
	return c.errCanario
}

func (c *mockControl) CanaryReports() <-chan domain.CanaryReport   { return c.informesCanario }
func (c *mockControl) CanaryRequests() <-chan domain.CanaryRequest { return c.pedidosEntrantes }

func (c *mockControl) SendCanaryReport(_ context.Context, r domain.CanaryReport) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.informesMandados = append(c.informesMandados, r)
	return nil
}

type canarioPedido struct {
	A   netip.Addr
	Req domain.CanaryRequest
}

func (c *mockControl) pedidosDeCanario() []canarioPedido {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]canarioPedido(nil), c.pedidosCanario...)
}

func (c *mockControl) informesEnviados() []domain.CanaryReport {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]domain.CanaryReport(nil), c.informesMandados...)
}

func (c *mockControl) códigosRepartidos() []domain.Room {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]domain.Room(nil), c.códigos...)
}

func (c *mockControl) Notify(_ context.Context, to netip.Addr, n domain.RoomNotice) error {
	if c.errNotify != nil {
		return c.errNotify
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.avisos = append(c.avisos, avisoFalso{a: to, n: n, tras: len(c.marcados) + len(c.anuncios)})
	c.avisados = append(c.avisados, to)
	return nil
}

func (c *mockControl) Notices() <-chan domain.RoomNotice { return c.avisosEntrantes }

// ConnectedMembers devuelve lo que el test haya puesto en `conectados`.
//
// Vacío por defecto, que es un host sin nadie dentro: así los tests que no
// hablan de esto siguen midiendo solo la tabla del motor.
func (c *mockControl) ConnectedMembers() []netip.Addr {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]netip.Addr(nil), c.conectados...)
}

func (c *mockControl) últimoAviso() (avisoFalso, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.avisos) == 0 {
		return avisoFalso{}, false
	}
	return c.avisos[len(c.avisos)-1], true
}

func (c *mockControl) últimoAnuncio() (domain.RoomAnnounce, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.anuncios) == 0 {
		return domain.RoomAnnounce{}, false
	}
	return c.anuncios[len(c.anuncios)-1], true
}

func (c *mockControl) RequestCredential(context.Context, domain.CredentialRequest) (domain.Credential, error) {
	if c.errReq != nil {
		return domain.Credential{}, c.errReq
	}
	return c.credencial, nil
}

func (c *mockControl) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cierres++
	c.sirviendo = false
	return nil
}

func (c *mockControl) alcanceActual() []netip.Addr {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]netip.Addr(nil), c.alcance...)
}

type mockAudit struct {
	perfiles []domain.FirewallProfileState
	// intactas true means "the system holds whatever was applied", which is
	// the normal case for tests that are not exercising this layer. It is
	// resolved by reading the fake firewall, so the domain diff actually runs
	// instead of being short-circuited by a boolean.
	intactas bool
	fw       *mockFirewall
	// overrideRules replaces the above when a test IS exercising this layer.
	overrideRules bool
	puestas       []domain.AppliedRule
	compuerta     domain.GateState
	mapeos        []domain.PortMapping

	// err rompe TODOS los métodos, que es el adaptador entero caído.
	err error
	// Y estos rompen uno solo. Hacen falta porque las tres comprobaciones no
	// valen lo mismo: las dos locales sostienen la promesa y la del router falla
	// en la mayoría de las máquinas, así que solo las dos primeras pueden
	// levantar el aviso de auditoría caída. Sin errores por método, esa
	// diferencia no se puede afirmar en un test.
	errPerfiles error
	errIntactas error
	errMapeos   error

	// cuarentenaMedida is what QuarantineState answers; its zero verdict is
	// Unknown, same as a real adapter that could not read. errCuarentena
	// breaks only that method, like the three above it.
	cuarentenaMedida domain.QuarantineState
	errCuarentena    error
}

func (a *mockAudit) FirewallEnabled(context.Context) ([]domain.FirewallProfileState, error) {
	return a.perfiles, primerError(a.errPerfiles, a.err)
}

func (a *mockAudit) Enforcement(context.Context) (domain.Enforcement, error) {
	if err := primerError(a.errIntactas, a.err); err != nil {
		return domain.Enforcement{}, err
	}
	e := domain.Enforcement{Gate: a.compuerta}
	switch {
	case a.overrideRules:
		e.Rules = a.puestas
	case a.intactas && a.fw != nil:
		for _, r := range a.fw.estado().Rules {
			e.Rules = append(e.Rules, domain.AppliedRule{
				Name:    r.Name,
				Layer:   domain.LayerFirewallRules,
				Enabled: true,
			})
		}
	}
	if a.compuerta == domain.GateUnknown && a.intactas {
		// El caso normal: el test dice "todo bien" y no le importa la compuerta.
		e.Gate = domain.GatePresent
	}
	return e, nil
}

// tamper makes the audit report a rule nobody asked for.
//
// It exists because "tampered" is no longer a boolean the adapter hands over:
// the verdict now comes from a real diff in the domain, so a test that wants a
// tampered firewall has to produce an actual discrepancy.
func (a *mockAudit) tamper() {
	a.intactas = false
	a.overrideRules = true
	a.puestas = []domain.AppliedRule{{
		Name:    "kanpachi-rule-nobody-asked-for",
		Layer:   domain.LayerFirewallRules,
		Enabled: true,
	}}
}

func (a *mockAudit) RouterMappings(context.Context) ([]domain.PortMapping, error) {
	return a.mapeos, primerError(a.errMapeos, a.err)
}

// cuarentenaMedida is what QuarantineState answers; its zero verdict is
// Unknown, same as the real adapters on a failed read.
func (a *mockAudit) QuarantineState(context.Context) (domain.QuarantineState, error) {
	return a.cuarentenaMedida, primerError(a.errCuarentena, a.err)
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

// mockSonda contesta lo que le digan por puerto, y silencio a lo que no esté.
//
// El silencio por defecto es deliberado: es lo que contesta una máquina
// blindada, así que un test que quiera una fuga tiene que pedirla explícito.
type mockSonda struct {
	mu        sync.Mutex
	respuesta map[uint16]domain.ProbeOutcome
	marcados  []netip.AddrPort
	espera    chan struct{}

	// Lo del canario va aparte de `respuesta` porque se pregunta por dirección y
	// no solo por puerto: los tests del lado del invitado comprueban que se marque
	// AL HOST y a nadie más.
	canarios               []canarioMarcado
	canarioTCP, canarioUDP domain.ProbeOutcome
}

// canarioMarcado es un sondeo de canario que ya ocurrió, con su número.
//
// El número se guarda porque su viaje intacto es media medición: uno mal copiado
// convierte el eco de UDP en un datagrama que no cuadra, y esa mitad se perdería
// en silencio.
type canarioMarcado struct {
	at    netip.AddrPort
	nonce domain.CanaryNonce
}

func (p *mockSonda) Probe(ctx context.Context, at netip.AddrPort) (domain.ProbeOutcome, time.Duration) {
	p.mu.Lock()
	p.marcados = append(p.marcados, at)
	out, ok := p.respuesta[at.Port()]
	espera := p.espera
	p.mu.Unlock()

	if espera != nil {
		select {
		case <-espera:
		case <-ctx.Done():
			return domain.ProbeFailed, 0
		}
	}
	if !ok {
		return domain.ProbeSilent, timing.ProbeDeadline
	}
	return out, 12 * time.Millisecond
}

func (p *mockSonda) ProbeCanary(ctx context.Context, at netip.AddrPort, nonce domain.CanaryNonce) (domain.ProbeOutcome, domain.ProbeOutcome) {
	p.mu.Lock()
	p.canarios = append(p.canarios, canarioMarcado{at: at, nonce: nonce})
	tcp, udp, espera := p.canarioTCP, p.canarioUDP, p.espera
	p.mu.Unlock()

	if espera != nil {
		select {
		case <-espera:
		case <-ctx.Done():
			return domain.ProbeFailed, domain.ProbeFailed
		}
	}
	// El cero significa "lo normal", que es el silencio de una máquina blindada.
	// Un test que quiera una fuga tiene que pedirla explícito, igual que en Probe.
	if tcp == 0 {
		tcp = domain.ProbeSilent
	}
	if udp == 0 {
		udp = domain.ProbeSilent
	}
	return tcp, udp
}

func (p *mockSonda) canariosMarcados() []canarioMarcado {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]canarioMarcado(nil), p.canarios...)
}

func (p *mockSonda) contesta(port uint16, out domain.ProbeOutcome) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.respuesta == nil {
		p.respuesta = map[uint16]domain.ProbeOutcome{}
	}
	p.respuesta[port] = out
}

func (p *mockSonda) cuántos() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.marcados)
}

// fixedClock no avanza salvo que un test lo mueva. Sin esto, probar los veinte
// minutos de la decisión 20 costaría veinte minutos.
type fixedClock struct {
	mu    sync.Mutex
	ahora time.Time
}

func (c *fixedClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ahora
}

func (c *fixedClock) avanza(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ahora = c.ahora.Add(d)
}

type logMudo struct{}

func (logMudo) Info(string, ...any)  {}
func (logMudo) Warn(string, ...any)  {}
func (logMudo) Error(string, ...any) {}

// bank es todo el escenario armado, para que cada test toque solo lo suyo.
type bank struct {
	deps     Deps
	motor    *mockMotor
	firewall *mockFirewall
	netcfg   *mockNetcfg
	catalog  *mockCatalog
	state    *mockState
	registry *mockRegistry
	control  *mockControl
	audit    *mockAudit
	sonda    *mockSonda
	canary   *fakeOpening
	clock    *fixedClock
	session  *Session
}

// fakeOpening es el port.CanaryPort de los tests.
//
// Guarda cada apertura, incluido el predicado de exclusión, porque comprobar
// QUÉ puertos se le prohibieron al canario es lo que impide la alarma que grita
// con todo bien.
type fakeOpening struct {
	mu       sync.Mutex
	openings []canaryOpening
	err      error

	// port es el que devuelve el canario falso. Fijo para que los informes de
	// los tests puedan nombrarlo.
	port uint16
}

type canaryOpening struct {
	at    netip.Addr
	nonce domain.CanaryNonce
	ttl   time.Duration
	avoid func(uint16) bool
	c     *mockCanary
}

func newOpening() *fakeOpening { return &fakeOpening{port: 51234} }

func (a *fakeOpening) Listen(at netip.Addr, nonce domain.CanaryNonce, ttl time.Duration,
	avoid func(uint16) bool) (port.Canary, error) {

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.err != nil {
		return nil, a.err
	}
	c := &mockCanary{port: a.port, touch: make(chan struct{})}
	a.openings = append(a.openings, canaryOpening{at: at, nonce: nonce, ttl: ttl, avoid: avoid, c: c})
	return c, nil
}

func (a *fakeOpening) veces() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.openings)
}

// awaitOpening bloquea hasta que se abra un canario DESPUÉS de `desde`, y
// devuelve el nuevo.
//
// Existe porque tocar el canario es lo que hace un paquete cruzando la
// compuerta, y eso pasa mientras la ronda ya está esperando. Se llama desde una
// goroutine, así que no puede usar t.Fatalf.
func (a *fakeOpening) awaitOpening(desde int) (*mockCanary, bool) {
	plazo := time.Now().Add(5 * time.Second)
	for time.Now().Before(plazo) {
		a.mu.Lock()
		n := len(a.openings)
		var c *mockCanary
		if n > desde {
			c = a.openings[n-1].c
		}
		a.mu.Unlock()
		if c != nil {
			return c, true
		}
		time.Sleep(time.Millisecond)
	}
	return nil, false
}

// última es la apertura más reciente. Los tests la usan para tocar el canario o
// para preguntarle al predicado qué habría rechazado.
func (a *fakeOpening) last(t interface{ Fatalf(string, ...any) }) canaryOpening {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.openings) == 0 {
		t.Fatalf("no se abrió ningún canario")
	}
	return a.openings[len(a.openings)-1]
}

// mockCanary imita al de verdad, incluida la parte que más importa: cerrar NO
// cierra el canal de toque.
type mockCanary struct {
	port uint16

	mu      sync.Mutex
	touched bool
	closed  int

	touch     chan struct{}
	touchOnce sync.Once
}

func (c *mockCanary) Port() uint16             { return c.port }
func (c *mockCanary) Touched() <-chan struct{} { return c.touch }

func (c *mockCanary) WasTouched() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.touched
}

func (c *mockCanary) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed++
	return nil
}

func (c *mockCanary) cierres() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

// tocar es alguien llegando hasta el socket, o sea el paquete cruzando la
// compuerta.
func (c *mockCanary) tocar() {
	c.mu.Lock()
	c.touched = true
	c.mu.Unlock()
	c.touchOnce.Do(func() { close(c.touch) })
}

// mockState es el disco de hosted-room.json y last-room.json.
//
// Guarda bytes y no structs, igual que el puerto de verdad: el decodificador
// estricto vive en el dominio y estos tests lo ejercitan de punta a punta.
type mockState struct {
	mu sync.Mutex

	room       []byte
	last       []byte
	seed       []byte
	seedToken  []byte
	knownHosts []byte
	// cuarentena is the persisted quarantine decision; nil is the absent
	// file, o sea sin decidir, que es el arranque normal del banco.
	cuarentena []byte
	// profile is the machine's own profile; nil is the absent file, o sea que
	// nadie eligió nombre todavía.
	profile []byte
	deleted int

	errSave error
}

func (e *mockState) LoadProfile() ([]byte, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.profile == nil {
		return nil, errors.New("no hay perfil guardado")
	}
	return append([]byte(nil), e.profile...), nil
}

func (e *mockState) SaveProfile(raw []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.errSave != nil {
		return e.errSave
	}
	e.profile = append([]byte(nil), raw...)
	return nil
}

func (e *mockState) LoadQuarantineDecision() ([]byte, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cuarentena == nil {
		return nil, errors.New("no hay decisión guardada")
	}
	return append([]byte(nil), e.cuarentena...), nil
}

func (e *mockState) SaveQuarantineDecision(raw []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.errSave != nil {
		return e.errSave
	}
	e.cuarentena = append([]byte(nil), raw...)
	return nil
}

func (e *mockState) LoadRoom() ([]byte, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.room == nil {
		return nil, errors.New("no hay sala guardada")
	}
	return append([]byte(nil), e.room...), nil
}

func (e *mockState) SaveRoom(raw []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.errSave != nil {
		return e.errSave
	}
	e.room = append([]byte(nil), raw...)
	return nil
}

func (e *mockState) ClearRoom() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.room = nil
	e.deleted++
	return nil
}

func (e *mockState) LoadLast() ([]byte, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.last == nil {
		return nil, errors.New("no hay última sala")
	}
	return append([]byte(nil), e.last...), nil
}

func (e *mockState) SaveLast(raw []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.errSave != nil {
		return e.errSave
	}
	e.last = append([]byte(nil), raw...)
	return nil
}

func (e *mockState) ClearLast() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.last = nil
	return nil
}

func (e *mockState) LoadSeed() ([]byte, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.seed == nil {
		return nil, errors.New("no hay registro configurado")
	}
	return append([]byte(nil), e.seed...), nil
}

func (e *mockState) SaveSeed(raw []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.errSave != nil {
		return e.errSave
	}
	e.seed = append([]byte(nil), raw...)
	return nil
}

// The refresh token of a closed seed. Kept in memory like everything else here,
// and with its own field: a test that checks it was cleared has to be able to
// see that the seed itself did not go with it.
func (e *mockState) LoadSeedToken() ([]byte, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.seedToken == nil {
		return nil, errors.New("no hay token guardado")
	}
	return append([]byte(nil), e.seedToken...), nil
}

func (e *mockState) SaveSeedToken(raw []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.errSave != nil {
		return e.errSave
	}
	e.seedToken = append([]byte(nil), raw...)
	return nil
}

func (e *mockState) ClearSeedToken() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.seedToken = nil
	return nil
}

// The fingerprint book. Same shape as the rest: bytes in, bytes out, with the
// strict decoder living in the domain.
func (e *mockState) LoadKnownHosts() ([]byte, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.knownHosts == nil {
		return nil, errors.New("no hay libreta guardada")
	}
	return append([]byte(nil), e.knownHosts...), nil
}

func (e *mockState) SaveKnownHosts(raw []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.errSave != nil {
		return e.errSave
	}
	e.knownHosts = append([]byte(nil), raw...)
	return nil
}

func (e *mockState) salaGuardada() []byte {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]byte(nil), e.room...)
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

func nuevoBanco(t interface{ Fatalf(string, ...any) }) *bank {
	b := bancoSinSesión()
	s, err := NewSession(context.Background(), b.deps)
	if err != nil {
		t.Fatalf("no se pudo montar la sesión: %v", err)
	}
	b.session = s
	return b
}

// bancoSinSesión arma los dobles y las Deps SIN llamar a NewSession.
//
// Existe aparte porque hay cosas que solo se pueden afirmar sobre el arranque
// mismo, y para eso hace falta poder cambiar un doble antes de que corra y poder
// ver fallar la construcción sin que el banco aborte el test.
func bancoSinSesión() *bank {
	b := &bank{
		motor:    nuevoMotor(),
		firewall: &mockFirewall{},
		netcfg:   &mockNetcfg{},
		catalog:  &mockCatalog{builtin: []byte(catálogoDePrueba)},
		state:    &mockState{},
		registry: &mockRegistry{},
		control:  nuevoControl(),
		audit:    &mockAudit{intactas: true},
		sonda:    &mockSonda{},
		canary:   newOpening(),
		clock:    &fixedClock{ahora: time.Date(2026, 8, 2, 20, 0, 0, 0, time.UTC)},
	}
	// La auditoría refleja lo que el firewall tiene aplicado, que es lo que
	// significa "intacto". Así el diff del dominio corre de verdad en los
	// tests en vez de esquivarse con un booleano.
	b.audit.fw = b.firewall
	b.deps = Deps{
		Engine:      b.motor,
		Firewall:    b.firewall,
		NetCfg:      b.netcfg,
		Routes:      rutasFalsas{prefijos: []netip.Prefix{netip.MustParsePrefix("192.168.1.0/24")}},
		Store:       b.catalog,
		State:       b.state,
		Library:     bibliotecaFalsa{},
		Directories: registrosFalsos{reg: b.registry},
		Control:     b.control,
		Audit:       b.audit,
		Inspector:   inspectorFalso{},
		Prober:      b.sonda,
		Canary:      b.canary,
		Clock:       b.clock,
		Log:         logMudo{},
		// Un lector constante hace que la subred y las claves salgan siempre
		// iguales, y que el test sea el mismo en cada ejecución.
		Rand: bytes.NewReader(bytes.Repeat([]byte{0x11}, 1<<16)),
		// Los tests usan la lista de Windows porque es la que este banco venía
		// ejercitando. Lo que cambia entre sistemas es qué puertos cierra, y eso
		// se prueba en `core/domain`, que es donde vive la decisión.
		Quarantine: domain.QuarantineWindows,
	}
	return b
}

// issueReq arma un pedido de emisión con una llave de miembro ÚNICA, que es lo
// que la puerta exige y lo que separa a dos máquinas aunque compartan apodo:
// el nombre no es identidad, la llave sí, y con la misma llave el host
// devuelve la misma credencial en vez de emitir otra.
var memberKeySeq atomic.Uint32

func issueReq(t interface{ Fatalf(string, ...any) }, name string) domain.CredentialRequest {
	key := fmt.Sprintf("member-key-de-prueba-%011d", memberKeySeq.Add(1))
	return domain.CredentialRequest{Name: nick(t, name), MemberKey: []byte(key)}
}

func nick(t interface{ Fatalf(string, ...any) }, s string) domain.Nickname {
	n, err := domain.ParseNickname(s)
	if err != nil {
		t.Fatalf("nick %q: %v", s, err)
	}
	return n
}

// Authenticate acepta cualquier prueba salvo que el registro esté declarado
// caído, como los demás métodos.
//
// Guarda LA PRUEBA y jamás un password, que es lo único que puede llegar acá:
// el hash lo calcula el caso de uso. Un falso que aceptara un password sería un
// falso que puede más que el real.
func (r *mockRegistry) Authenticate(_ context.Context, proof string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	r.pruebas = append(r.pruebas, proof)
	return nil
}
