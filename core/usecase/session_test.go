package usecase

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
)

func ctx() context.Context { return context.Background() }

// TestArrancarPurgaLoQueDejóLaEjecuciónAnterior.
//
// El caso que arregla no pasa por CreateRoom: una muerte sucia del daemon deja
// reglas puestas y reglas ajenas suspendidas, y el usuario abre la app sin
// crear ninguna sala. Purgar al arrancar es lo que garantiza que nunca queden
// puertos huérfanos abiertos.
func TestArrancarPurgaLoQueDejóLaEjecuciónAnterior(t *testing.T) {
	b := nuevoBanco(t)
	if b.firewall.purgas != 1 {
		t.Errorf("purgas al arrancar = %d", b.firewall.purgas)
	}
	if b.firewall.restauras != 1 {
		t.Errorf("restauraciones de reglas ajenas al arrancar = %d", b.firewall.restauras)
	}
}

func TestCrearSala(t *testing.T) {
	b := nuevoBanco(t)

	st, err := b.sesión.CreateRoom(ctx(), nick(t, "alvaro"), "Los panas")
	if err != nil {
		t.Fatal(err)
	}
	if st.Conn != domain.StateConnected || st.Role != domain.RoleHost {
		t.Fatalf("estado tras crear: %s, %s", st.Conn, st.Role)
	}
	if st.Room.InviteID.String() != "A7K2-M9QX" {
		t.Errorf("el código no salió del registro: %q", st.Room.InviteID.String())
	}
	if !st.HostPresent {
		t.Error("el host no se marcó presente en su propia sala")
	}
	if st.LocalIP != domain.HostAddress(st.Subnet) {
		t.Errorf("el host no tomó la .1: %s en %s", st.LocalIP, st.Subnet)
	}
}

// TestUnaSalaNaceSinJuegoYSinPuertos es la decisión 20 y la cuarentena por
// defecto en el mismo test: red cifrada, cero puertos abiertos.
func TestUnaSalaNaceSinJuegoYSinPuertos(t *testing.T) {
	b := nuevoBanco(t)
	st, err := b.sesión.CreateRoom(ctx(), nick(t, "alvaro"), "Los panas")
	if err != nil {
		t.Fatal(err)
	}
	if !st.Game.IsZero() {
		t.Fatalf("la sala nació con juego: %s", st.Game.ID)
	}
	if !b.firewall.estado().IsEmpty() {
		t.Fatalf("la sala nació con reglas: %+v", b.firewall.estado().Rules)
	}
}

// TestElSecretoDeLaRedRealNoDerivaDelCódigo: es la propiedad central de la
// decisión 2. Dos salas creadas con el mismo invite ID tienen que tener
// identidades de red distintas.
func TestElSecretoDeLaRedRealNoDerivaDelCódigo(t *testing.T) {
	b := nuevoBanco(t)
	if _, err := b.sesión.CreateRoom(ctx(), nick(t, "alvaro"), "Los panas"); err != nil {
		t.Fatal(err)
	}
	spec := b.motor.hostSpec

	rdv := domain.DeriveRendezvous(b.sesión.Status().Room.InviteID)
	if spec.RealNetworkName() == rdv.NetworkName() {
		t.Fatal("la red real y el vestíbulo son la misma: el código sería el secreto de la sala")
	}
	var cero [32]byte
	if spec.NetworkSecret == cero {
		t.Fatal("el secreto de la red real quedó en cero")
	}
}

// TestSiElRegistroNoRespondeLaSalaSeCreaIgual. Es solo presentación: lo que se
// pierde es la tarjeta, no la sala.
func TestSiElRegistroNoRespondeLaSalaSeCreaIgual(t *testing.T) {
	b := nuevoBanco(t)
	b.registro.err = errors.New("504")

	st, err := b.sesión.CreateRoom(ctx(), nick(t, "alvaro"), "Los panas")
	if err != nil {
		t.Fatalf("el registro caído impidió crear la sala: %v", err)
	}
	if st.Room.InviteID.IsZero() {
		t.Fatal("la sala quedó sin código")
	}
	if st.Conn != domain.StateConnected {
		t.Fatalf("estado = %s", st.Conn)
	}
}

// TestElRegistroRecibeLaTarjetaCifradaYNoElNombre. Si algún día el servidor
// pudiera leer nombres de sala o nicks, sería una decisión de producto que se
// escribe en la 17, no un detalle de implementación.
func TestElRegistroRecibeLaTarjetaCifradaYNoElNombre(t *testing.T) {
	b := nuevoBanco(t)
	if _, err := b.sesión.CreateRoom(ctx(), nick(t, "alvaro"), "Los panas"); err != nil {
		t.Fatal(err)
	}
	depositado := string(b.registro.publicado)
	if strings.Contains(depositado, "Los panas") || strings.Contains(depositado, "alvaro") {
		t.Fatalf("el nombre de la sala o el nick viajaron en claro al registro: %q", depositado)
	}
}

// TestElEnlaceLlevaLaClaveDeLaTarjeta, que es lo único que permite descifrarla
// y lo único que el servidor no recibe.
func TestElEnlaceLlevaLaClaveDeLaTarjeta(t *testing.T) {
	b := nuevoBanco(t)
	if _, err := b.sesión.CreateRoom(ctx(), nick(t, "alvaro"), "Los panas"); err != nil {
		t.Fatal(err)
	}
	link := b.sesión.InviteLink()
	id, frag, ok := strings.Cut(link, "#")
	if !ok {
		t.Fatalf("el enlace no lleva fragmento: %q", link)
	}
	if !strings.HasSuffix(id, "A7K2-M9QX") {
		t.Errorf("el enlace no lleva el código: %q", id)
	}
	key, err := domain.ParseCardKeyFragment(frag)
	if err != nil {
		t.Fatalf("el fragmento no es una clave: %v", err)
	}
	card, err := domain.OpenRoomCard(b.registro.publicado, key)
	if err != nil {
		t.Fatalf("la clave del enlace no abre la tarjeta que se depositó: %v", err)
	}
	if card.Room != "Los panas" || card.Host.String() != "alvaro" {
		t.Fatalf("la tarjeta dice otra cosa: %+v", card)
	}
}

func TestNoSePuedeEstarEnDosSalas(t *testing.T) {
	b := nuevoBanco(t)
	if _, err := b.sesión.CreateRoom(ctx(), nick(t, "alvaro"), "Los panas"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.sesión.CreateRoom(ctx(), nick(t, "alvaro"), "Otra"); !errors.Is(err, ErrBusy) {
		t.Fatalf("se crearon dos salas: %v", err)
	}
	if _, err := b.sesión.JoinRoom(ctx(), "A7K2M9QX", nick(t, "alvaro")); !errors.Is(err, ErrBusy) {
		t.Fatalf("se entró a otra sala teniendo una: %v", err)
	}
}

// TestUnFalloAMitadDeCaminoVuelveAIdle: sin esto, la sesión se quedaría en
// Resolving para siempre y la UI mostrando una ruedita que no gira a ningún
// lado.
func TestUnFalloAMitadDeCaminoVuelveAIdle(t *testing.T) {
	b := nuevoBanco(t)
	b.motor.errHost = errors.New("el motor no arrancó")

	if _, err := b.sesión.CreateRoom(ctx(), nick(t, "alvaro"), "Los panas"); err == nil {
		t.Fatal("la creación no falló")
	}
	if st := b.sesión.Status(); st.Conn != domain.StateIdle {
		t.Fatalf("quedó en %s", st.Conn)
	}
}

func salaCreada(t *testing.T) *banco {
	t.Helper()
	b := nuevoBanco(t)
	if _, err := b.sesión.CreateRoom(ctx(), nick(t, "alvaro"), "Los panas"); err != nil {
		t.Fatal(err)
	}
	return b
}

// TestElJuegoNoAbreNadaHastaQueHayaAlguien: RemoteAddresses son siempre los
// miembros presentes y no existe forma de decir "cualquiera".
func TestElJuegoNoAbreNadaHastaQueHayaAlguien(t *testing.T) {
	b := salaCreada(t)

	if _, err := b.sesión.ActivateProfile(ctx(), "project-zomboid"); err != nil {
		t.Fatal(err)
	}
	if !b.firewall.estado().IsEmpty() {
		t.Fatalf("se abrieron puertos con la sala vacía: %+v", b.firewall.estado().Rules)
	}
}

func TestElJuegoAbreLosPuertosCuandoEntraAlguien(t *testing.T) {
	b := salaCreada(t)
	self := b.sesión.Status().LocalIP
	invitado := self.Next()

	b.motor.peers = []domain.Peer{
		{VirtualIP: self, Name: nick(t, "alvaro"), Host: true},
		{VirtualIP: invitado, Name: nick(t, "humberto"), Path: domain.PathDirect},
	}
	if _, err := b.sesión.ActivateProfile(ctx(), "project-zomboid"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.sesión.OnPeersChanged(ctx()); err != nil {
		t.Fatal(err)
	}

	rs := b.firewall.estado()
	if len(rs.Rules) != 1 {
		t.Fatalf("reglas = %d: %+v", len(rs.Rules), rs.Rules)
	}
	r := rs.Rules[0]
	if r.From != 16261 || r.To != 16262 || r.Proto != domain.ProtoUDP {
		t.Errorf("la regla no es la del perfil: %+v", r)
	}
	if len(r.Remote) != 1 || r.Remote[0] != invitado {
		t.Errorf("el alcance no es el invitado: %v", r.Remote)
	}
}

// TestElPerfilLlevaSusAjustesAlAdaptador, y quitarlo los revierte sin que
// nadie tenga que deshacerlos uno por uno.
func TestElPerfilLlevaSusAjustesAlAdaptador(t *testing.T) {
	b := salaCreada(t)

	if _, err := b.sesión.ActivateProfile(ctx(), "project-zomboid"); err != nil {
		t.Fatal(err)
	}
	if !b.netcfg.estado().MulticastRoute {
		t.Fatal("el perfil pedía ruta de multicast y no llegó al adaptador")
	}
	if _, err := b.sesión.ActivateProfile(ctx(), ""); err != nil {
		t.Fatal(err)
	}
	if b.netcfg.estado().MulticastRoute {
		t.Fatal("quitar el juego no revirtió la ruta de multicast")
	}
}

// TestLaAPISoloAceptaPerfilesDelCatálogo es la mitigación principal de la
// superficie del named pipe: no existe la operación "abrir puerto arbitrario".
func TestLaAPISoloAceptaPerfilesDelCatálogo(t *testing.T) {
	b := salaCreada(t)
	_, err := b.sesión.ActivateProfile(ctx(), "un-juego-que-no-existe")
	if !errors.Is(err, ErrUnknownGame) {
		t.Fatalf("se aceptó un juego fuera del catálogo: %v", err)
	}
}

// TestSiElFirewallRechazaElConjuntoElJuegoNoSeDaPorActivo: la UI mostraría
// puertos abiertos que no lo están.
func TestSiElFirewallRechazaElConjuntoElJuegoNoSeDaPorActivo(t *testing.T) {
	b := salaCreada(t)
	b.firewall.errApply = errors.New("COM dijo que no")

	if _, err := b.sesión.ActivateProfile(ctx(), "project-zomboid"); err == nil {
		t.Fatal("se dio por activado")
	}
	if st := b.sesión.Status(); !st.Game.IsZero() {
		t.Fatalf("el juego quedó marcado como activo: %s", st.Game.ID)
	}
}

func TestSoloElHostPuedeLasTresOperaciones(t *testing.T) {
	b := nuevoBanco(t)
	b.control.credencial = domain.Credential{
		ID: "c1", Token: "t", NetworkName: "kanpachi-real",
		VirtualIP: netip.MustParseAddr("100.87.3.5"),
		Subnet:    netip.MustParsePrefix("100.87.3.0/24"),
	}
	if _, err := b.sesión.JoinRoom(ctx(), "A7K2M9QX", nick(t, "humberto")); err != nil {
		t.Fatal(err)
	}

	if _, err := b.sesión.ActivateProfile(ctx(), "project-zomboid"); !errors.Is(err, ErrNotHost) {
		t.Errorf("un invitado activó un juego: %v", err)
	}
	if _, err := b.sesión.KickMember(ctx(), netip.MustParseAddr("100.87.3.1")); !errors.Is(err, ErrNotHost) {
		t.Errorf("un invitado expulsó a alguien: %v", err)
	}
	if _, err := b.sesión.RotateInviteCode(ctx()); !errors.Is(err, ErrNotHost) {
		t.Errorf("un invitado renovó el código: %v", err)
	}
}

// TestEntrarPasaPorElVestíbuloYSaleDeÉl.
//
// El orden importa: quedarse en el vestíbulo después de entrar mantendría
// abierta una vía por la que cualquiera con el código ve que esta máquina está
// en esa sala.
func TestEntrarPasaPorElVestíbuloYSaleDeÉl(t *testing.T) {
	b := nuevoBanco(t)
	b.control.credencial = domain.Credential{
		ID: "c1", Token: "t", NetworkName: "kanpachi-real",
		VirtualIP: netip.MustParseAddr("100.87.3.5"),
		Subnet:    netip.MustParsePrefix("100.87.3.0/24"),
	}

	st, err := b.sesión.JoinRoom(ctx(), "kanpachi://A7K2-M9QX", nick(t, "humberto"))
	if err != nil {
		t.Fatal(err)
	}
	if st.Role != domain.RoleGuest || st.Conn != domain.StateConnected {
		t.Fatalf("estado tras entrar: %s, %s", st.Conn, st.Role)
	}

	pasos := b.motor.pasos()
	quiero := []string{"vestíbulo", "salir-vestíbulo", "red-real"}
	if len(pasos) != len(quiero) {
		t.Fatalf("pasos del motor = %v", pasos)
	}
	for i := range quiero {
		if pasos[i] != quiero[i] {
			t.Fatalf("pasos del motor = %v, se esperaba %v", pasos, quiero)
		}
	}
}

// TestElInvitadoNoAbreElCanalDeControl: solo el host escucha, y el deny-all
// del invitado queda literalmente intacto.
func TestElInvitadoNoAbreElCanalDeControl(t *testing.T) {
	b := nuevoBanco(t)
	b.control.credencial = domain.Credential{
		ID: "c1", Token: "t", NetworkName: "kanpachi-real",
		VirtualIP: netip.MustParseAddr("100.87.3.5"),
		Subnet:    netip.MustParsePrefix("100.87.3.0/24"),
	}
	if _, err := b.sesión.JoinRoom(ctx(), "A7K2M9QX", nick(t, "humberto")); err != nil {
		t.Fatal(err)
	}
	b.control.mu.Lock()
	sirviendo := b.control.sirviendo
	marcados := append([]netip.Addr(nil), b.control.marcados...)
	b.control.mu.Unlock()

	if sirviendo {
		t.Fatal("el invitado abrió un oyente")
	}
	// Dos llamadas, en este orden: primero el vestíbulo para pedir la
	// credencial, después la sala, que es la conexión que tiene que quedar
	// viva. Sin la segunda, la presencia del host queda clavada en lo que se
	// puso al entrar y el contador de veinte minutos no arranca nunca.
	if len(marcados) != 2 {
		t.Fatalf("marcó %d veces: %v", len(marcados), marcados)
	}
	if marcados[0] != domain.RendezvousHostAddress {
		t.Errorf("la primera no fue al vestíbulo: %s", marcados[0])
	}
	if marcados[1] != netip.MustParseAddr("100.87.3.1") {
		t.Errorf("la segunda no fue al host en la sala: %s", marcados[1])
	}
}

// TestSiElCanalConElHostNoLevantaNoSeEntra.
//
// Estar dentro sin ese socket es estar en una sala donde no se puede saber si
// el host se fue, o sea con el contador de veinte minutos muerto.
func TestSiElCanalConElHostNoLevantaNoSeEntra(t *testing.T) {
	b := nuevoBanco(t)
	b.control.credencial = domain.Credential{
		ID: "c1", Token: "t", NetworkName: "kanpachi-real",
		VirtualIP: netip.MustParseAddr("100.87.3.5"),
		Subnet:    netip.MustParsePrefix("100.87.3.0/24"),
	}
	// La primera llamada, al vestíbulo, funciona. La segunda, a la sala, no.
	b.control.fallarDesde = 2

	if _, err := b.sesión.JoinRoom(ctx(), "A7K2M9QX", nick(t, "humberto")); err == nil {
		t.Fatal("se entró sin canal con el host")
	}
	if st := b.sesión.Status(); st.Conn != domain.StateIdle {
		t.Fatalf("quedó en %s", st.Conn)
	}
}

// TestElHostAbreLaPuertaYLaSalaPorSeparado: son dos conversaciones con dos
// modelos de confianza, y meterlas en una sola regalaría la de la sala a
// cualquiera que tenga el código.
func TestElHostAbreLaPuertaYLaSalaPorSeparado(t *testing.T) {
	b := salaCreada(t)

	b.control.mu.Lock()
	scope := b.control.scope
	b.control.mu.Unlock()

	if scope.Lobby != domain.RendezvousHostAddress {
		t.Errorf("la puerta no está en la dirección conocida del vestíbulo: %s", scope.Lobby)
	}
	if scope.Room != b.sesión.Status().LocalIP {
		t.Errorf("la sala no escucha en la IP del adaptador: %s", scope.Room)
	}
	// El host entró al vestíbulo además de levantar la red real: sin eso nadie
	// puede alcanzarlo para pedirle la credencial.
	pasos := b.motor.pasos()
	if len(pasos) != 2 || pasos[0] != "host" || pasos[1] != "vestíbulo" {
		t.Fatalf("pasos del motor al crear = %v", pasos)
	}
	if b.motor.rdvSpec.Address != domain.RendezvousHostAddress {
		t.Errorf("el host no tomó su dirección fija en el vestíbulo: %s", b.motor.rdvSpec.Address)
	}
}

// TestElVestíbuloNoSeLeEntregaAUnaSala: si coincidieran, entrar a la sala
// cortaría la conexión que se está usando para pedir la credencial.
func TestElVestíbuloNoSeLeEntregaAUnaSala(t *testing.T) {
	b := salaCreada(t)
	if b.sesión.Status().Subnet == domain.RendezvousSubnet {
		t.Fatal("la sala se quedó con el /24 del vestíbulo")
	}
}

// TestUnaCredencialAMediasSeRechaza: llega de otra máquina, así que se revisa.
func TestUnaCredencialAMediasSeRechaza(t *testing.T) {
	b := nuevoBanco(t)
	b.control.credencial = domain.Credential{ID: "c1"} // sin token, sin IP, sin subred

	if _, err := b.sesión.JoinRoom(ctx(), "A7K2M9QX", nick(t, "humberto")); err == nil {
		t.Fatal("se entró con una credencial incompleta")
	}
	if st := b.sesión.Status(); st.Conn != domain.StateIdle {
		t.Fatalf("quedó en %s", st.Conn)
	}
}

func TestSiElHostNoRespondeElIngresoFalla(t *testing.T) {
	b := nuevoBanco(t)
	b.control.errDial = errors.New("conexión rechazada")

	_, err := b.sesión.JoinRoom(ctx(), "A7K2M9QX", nick(t, "humberto"))
	if err == nil {
		t.Fatal("se entró sin host")
	}
	if !strings.Contains(err.Error(), "reconectando") {
		t.Errorf("el mensaje manda a revisar lo equivocado: %v", err)
	}
}

func TestUnCódigoConFormaRaraNiSiquieraMueveElEstado(t *testing.T) {
	b := nuevoBanco(t)
	if _, err := b.sesión.JoinRoom(ctx(), "no-es-un-código", nick(t, "humberto")); err == nil {
		t.Fatal("se aceptó")
	}
	if st := b.sesión.Status(); st.Conn != domain.StateIdle {
		t.Fatalf("un código inválido movió el estado a %s", st.Conn)
	}
	if len(b.motor.pasos()) != 0 {
		t.Fatalf("se tocó el motor con un código inválido: %v", b.motor.pasos())
	}
}

// TestExpulsarRevocaYRecalcula: las dos capas de la decisión 22, en el mismo
// acto.
func TestExpulsarRevocaYRecalcula(t *testing.T) {
	b := salaCreada(t)
	self := b.sesión.Status().LocalIP
	invitado := self.Next()

	b.motor.peers = []domain.Peer{
		{VirtualIP: self, Name: nick(t, "alvaro"), Host: true},
		{VirtualIP: invitado, Name: nick(t, "humberto")},
	}
	b.motor.credentials = []domain.Credential{{ID: "cred-humberto", VirtualIP: invitado}}

	if _, err := b.sesión.ActivateProfile(ctx(), "project-zomboid"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.sesión.OnPeersChanged(ctx()); err != nil {
		t.Fatal(err)
	}
	if len(b.firewall.estado().Rules) != 1 {
		t.Fatal("no se llegó a abrir el puerto, el test no probaría nada")
	}

	if _, err := b.sesión.KickMember(ctx(), invitado); err != nil {
		t.Fatal(err)
	}
	if len(b.motor.revocadas) != 1 || b.motor.revocadas[0] != "cred-humberto" {
		t.Errorf("no se revocó la credencial: %v", b.motor.revocadas)
	}
	if !b.firewall.estado().IsEmpty() {
		t.Errorf("el expulsado sigue autorizado en el firewall: %+v", b.firewall.estado().Rules)
	}
	for _, ip := range b.control.alcanceActual() {
		if ip == invitado {
			t.Error("el expulsado sigue pudiendo abrir conexiones al canal de control")
		}
	}
}

// TestElRecorteDeMiembrosNoEsperaAlSiguienteSondeo: durante esos segundos la
// regla seguiría autorizando a alguien a quien el host acaba de echar.
func TestElRecorteDeMiembrosNoEsperaAlSiguienteSondeo(t *testing.T) {
	b := salaCreada(t)
	self := b.sesión.Status().LocalIP
	invitado := self.Next()

	b.motor.peers = []domain.Peer{
		{VirtualIP: self, Host: true},
		{VirtualIP: invitado},
	}
	b.motor.credentials = []domain.Credential{{ID: "c", VirtualIP: invitado}}
	if _, err := b.sesión.OnPeersChanged(ctx()); err != nil {
		t.Fatal(err)
	}

	// El motor sigue reportando a los dos: todavía no se enteró.
	st, err := b.sesión.KickMember(ctx(), invitado)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range st.Peers {
		if p.VirtualIP == invitado {
			t.Fatal("el expulsado sigue en la lista de miembros")
		}
	}
}

func TestNoTeExpulsasATiMismo(t *testing.T) {
	b := salaCreada(t)
	self := b.sesión.Status().LocalIP
	if _, err := b.sesión.KickMember(ctx(), self); !errors.Is(err, ErrSelfKick) {
		t.Fatalf("se aceptó autoexpulsión: %v", err)
	}
	if _, err := b.sesión.KickMember(ctx(), netip.MustParseAddr("10.0.0.9")); !errors.Is(err, ErrNotAMember) {
		t.Fatalf("se aceptó expulsar a un desconocido: %v", err)
	}
}

// TestRenovarElCódigoNoTocaALosPresentes es la mitad de la decisión 22 que la
// derivación local pura no podía dar.
func TestRenovarElCódigoNoTocaALosPresentes(t *testing.T) {
	b := salaCreada(t)
	self := b.sesión.Status().LocalIP
	invitado := self.Next()

	b.motor.peers = []domain.Peer{{VirtualIP: self, Host: true}, {VirtualIP: invitado}}
	if _, err := b.sesión.ActivateProfile(ctx(), "project-zomboid"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.sesión.OnPeersChanged(ctx()); err != nil {
		t.Fatal(err)
	}
	antes := b.sesión.Status()
	reglasAntes := b.firewall.estado()

	b.registro.siguiente = "B4N9PQRS"
	st, err := b.sesión.RotateInviteCode(ctx())
	if err != nil {
		t.Fatal(err)
	}

	if st.Room.InviteID == antes.Room.InviteID {
		t.Fatal("el código no cambió")
	}
	if len(st.Peers) != len(antes.Peers) {
		t.Fatalf("renovar echó gente: %d contra %d", len(st.Peers), len(antes.Peers))
	}
	if len(b.motor.revocadas) != 0 {
		t.Fatalf("renovar revocó credenciales: %v", b.motor.revocadas)
	}
	if len(b.firewall.estado().Rules) != len(reglasAntes.Rules) {
		t.Fatal("renovar cambió las reglas de firewall")
	}
	// La red real no cambia: la partida no se entera. Lo único que se
	// rehospeda es el vestíbulo, con el nombre que deriva del código nuevo.
	pasos := b.motor.pasos()
	if pasos[len(pasos)-1] != "vestíbulo" {
		t.Fatalf("renovar no rehospedó la puerta: %v", pasos)
	}
	for _, p := range pasos {
		if p == "salir" {
			t.Fatalf("renovar reinició el motor: %v", pasos)
		}
	}
	esperado := domain.DeriveRendezvous(st.Room.InviteID).NetworkName()
	if b.motor.rdvSpec.Rendezvous.NetworkName() != esperado {
		t.Fatal("la puerta quedó en el vestíbulo del código viejo: nadie podría entrar con el nuevo")
	}
}

func TestSalirCierraTodo(t *testing.T) {
	b := salaCreada(t)
	if _, err := b.sesión.ActivateProfile(ctx(), "project-zomboid"); err != nil {
		t.Fatal(err)
	}

	st := b.sesión.LeaveRoom(ctx())
	if st.Conn != domain.StateIdle {
		t.Fatalf("estado = %s", st.Conn)
	}
	if !b.firewall.estado().IsEmpty() {
		t.Errorf("quedaron reglas: %+v", b.firewall.estado().Rules)
	}
	if b.firewall.restauras < 2 { // una al arrancar, una al salir
		t.Errorf("no se restauraron las reglas ajenas al salir: %d", b.firewall.restauras)
	}
	if b.netcfg.revertió != 1 {
		t.Errorf("no se revirtieron los ajustes del adaptador: %d", b.netcfg.revertió)
	}
	if b.control.cierres == 0 {
		t.Error("el canal de control quedó abierto")
	}
	if st.Room.InviteID.String() != "" {
		t.Error("quedó el código de la sala anterior")
	}
}

func TestSalirEsIdempotente(t *testing.T) {
	b := nuevoBanco(t)
	if st := b.sesión.LeaveRoom(ctx()); st.Conn != domain.StateIdle {
		t.Fatalf("salir sin sala rompió el estado: %s", st.Conn)
	}
}

// TestLaSalidaAutomáticaALosVeinteMinutos, con el reloj en la mano.
func TestLaSalidaAutomáticaALosVeinteMinutos(t *testing.T) {
	b := nuevoBanco(t)
	b.control.credencial = domain.Credential{
		ID: "c1", Token: "t", NetworkName: "kanpachi-real",
		VirtualIP: netip.MustParseAddr("100.87.3.5"),
		Subnet:    netip.MustParsePrefix("100.87.3.0/24"),
	}
	if _, err := b.sesión.JoinRoom(ctx(), "A7K2M9QX", nick(t, "humberto")); err != nil {
		t.Fatal(err)
	}

	b.sesión.SetHostPresent(false)
	b.reloj.avanza(19 * time.Minute)
	if b.sesión.TickHostAbsence(ctx()) {
		t.Fatal("salió a los diecinueve minutos")
	}
	b.reloj.avanza(2 * time.Minute)
	if !b.sesión.TickHostAbsence(ctx()) {
		t.Fatal("no salió pasados los veinte minutos")
	}
	if st := b.sesión.Status(); st.Conn != domain.StateIdle {
		t.Fatalf("estado = %s", st.Conn)
	}
}

// TestElHostNoSeEchaDeSuPropiaSala, ni siquiera si el supervisor le manda una
// ausencia por error.
func TestElHostNoSeEchaDeSuPropiaSalaPorElContador(t *testing.T) {
	b := salaCreada(t)
	b.sesión.SetHostPresent(false)
	b.reloj.avanza(2 * time.Hour)

	if b.sesión.TickHostAbsence(ctx()) {
		t.Fatal("el host salió de la sala que hospeda")
	}
}

func TestElCatálogoSeCargaConPrecedencia(t *testing.T) {
	b := nuevoBanco(t)
	juegos := b.sesión.ListGames()
	if len(juegos) != 2 {
		t.Fatalf("juegos = %d", len(juegos))
	}
	for _, j := range juegos {
		if j.Origin != domain.OriginBuiltin {
			t.Errorf("%s vino como %s", j.ID, j.Origin)
		}
	}
}

// TestUnLocalCorruptoNoDejaSinCatálogo: se ignora entero y Kanpachi sigue
// funcionando con los builtin.
func TestUnLocalCorruptoNoDejaSinCatálogo(t *testing.T) {
	b := nuevoBanco(t)
	b.almacén.local = []byte(`{"kanpachi_catalog": esto no es json`)
	b.sesión.reloadCatalog(ctx())

	if len(b.sesión.ListGames()) != 2 {
		t.Fatalf("un local.json corrupto se llevó el catálogo por delante")
	}
}

// TestGuardarUnPerfilNoLoDaPorVerificado: la única vía es la pregunta al salir
// de la sala, y eso es lo que hace que la etiqueta signifique algo.
func TestGuardarUnPerfilNoLoDaPorVerificado(t *testing.T) {
	b := nuevoBanco(t)
	nuevo := domain.GameProfile{
		ID:        "valheim",
		Name:      "Valheim",
		HostPorts: []domain.PortRange{{Proto: domain.ProtoUDP, From: 2456, To: 2458}},
		Connect:   domain.ConnectHint{Kind: domain.ConnectDirectIP},
		Verified:  &domain.Verified{By: "yo mismo", Method: "porque sí"},
	}
	guardado, err := b.sesión.SaveProfile(ctx(), nuevo, false)
	if err != nil {
		t.Fatal(err)
	}
	if guardado.Verified != nil {
		t.Fatal("un perfil nació verificado porque lo pedía la petición")
	}
	if guardado.Origin != domain.OriginMine {
		t.Errorf("origen = %s", guardado.Origin)
	}
	// Y quedó en la lista, que es lo que hace que aparezca de inmediato sin
	// reiniciar nada.
	var vio bool
	for _, p := range b.sesión.ListGames() {
		if p.ID == "valheim" {
			vio = true
		}
	}
	if !vio {
		t.Fatal("el perfil guardado no apareció en la lista")
	}
}

// TestUnPerfilQuePidePuertoProhibidoNoSeGuarda.
func TestUnPerfilQuePidePuertoProhibidoNoSeGuarda(t *testing.T) {
	b := nuevoBanco(t)
	_, err := b.sesión.SaveProfile(ctx(), domain.GameProfile{
		ID:        "malo",
		Name:      "Malo",
		HostPorts: []domain.PortRange{{Proto: domain.ProtoTCP, From: 440, To: 450}},
		Connect:   domain.ConnectHint{Kind: domain.ConnectDirectIP},
	}, false)
	if !errors.Is(err, domain.ErrPortForbidden) {
		t.Fatalf("se guardó un perfil que abre el 445: %v", err)
	}
	if b.almacén.escrituras != 0 {
		t.Fatal("se escribió el archivo igual")
	}
}

// TestImportarSoloLoMarcadoYNuncaUnRechazado.
func TestImportarSoloLoMarcadoYNuncaUnRechazado(t *testing.T) {
	b := nuevoBanco(t)
	archivo := []byte(`{"kanpachi_catalog":1,"profiles":[
	  {"id":"valheim","schema":2,"name":"Valheim","host_ports":[{"proto":"udp","range":"2456-2458"}],
	   "client_ports":[],"connect_hint":{"kind":"direct_ip","text_es":"x"}},
	  {"id":"terraria","schema":2,"name":"Terraria","host_ports":[{"proto":"tcp","range":"7777"}],
	   "client_ports":[],"connect_hint":{"kind":"direct_ip","text_es":"x"}},
	  {"id":"rust","schema":2,"name":"Rust","host_ports":[{"proto":"tcp","range":"445"}],
	   "client_ports":[],"connect_hint":{"kind":"direct_ip","text_es":"x"}}
	]}`)

	// Se marcan los tres, incluido el rechazado, que es lo que pasaría si la
	// UI se desincronizara del daemon. En esa duda gana el daemon.
	cands, err := b.sesión.ImportCatalog(ctx(), archivo, []string{"valheim", "terraria", "rust"})
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 3 {
		t.Fatalf("candidatos = %d", len(cands))
	}

	ids := map[string]domain.Origin{}
	for _, p := range b.sesión.ListGames() {
		ids[p.ID] = p.Origin
	}
	if ids["valheim"] != domain.OriginImported || ids["terraria"] != domain.OriginImported {
		t.Fatalf("no entraron como importados: %v", ids)
	}
	if _, hay := ids["rust"]; hay {
		t.Fatal("entró el perfil que pide el 445")
	}
}

func TestImportarNadaNoEscribeNada(t *testing.T) {
	b := nuevoBanco(t)
	archivo := []byte(`{"kanpachi_catalog":1,"profiles":[
	  {"id":"valheim","schema":2,"name":"Valheim","host_ports":[{"proto":"udp","range":"2456"}],
	   "client_ports":[],"connect_hint":{"kind":"direct_ip","text_es":"x"}}]}`)

	if _, err := b.sesión.ImportCatalog(ctx(), archivo, nil); err != nil {
		t.Fatal(err)
	}
	if b.almacén.escrituras != 0 {
		t.Fatal("se reescribió local.json sin importar nada")
	}
}

// TestUnBuiltinNoSePuedeVerificarPorLaPuertaDeAtrás: escribir una copia local
// crearía un perfil "mine" que tapa al builtin, y a partir de ahí las
// actualizaciones de la app dejarían de llegarle a ese juego.
func TestUnBuiltinNoSePuedeVerificarPorLaPuertaDeAtrás(t *testing.T) {
	b := salaCreada(t)
	if _, err := b.sesión.ActivateProfile(ctx(), "project-zomboid"); err != nil {
		t.Fatal(err)
	}
	self := b.sesión.Status().LocalIP
	b.motor.peers = []domain.Peer{{VirtualIP: self}, {VirtualIP: self.Next()}}
	if _, err := b.sesión.OnPeersChanged(ctx()); err != nil {
		t.Fatal(err)
	}
	b.sesión.LeaveRoom(ctx())

	if err := b.sesión.MarkVerified(ctx(), "project-zomboid",
		domain.Verified{By: "alvaro", Method: "partida real"}); err != nil {
		t.Fatal(err)
	}
	if b.almacén.escrituras != 0 {
		t.Fatal("se escribió una copia local de un builtin")
	}
}

// TestUnFalloAlAbrirElCanalDeshaceLoQueYaSeHizo.
//
// Sin el teardown en el camino de error, un fallo tardío dejaría el motor
// levantado y a la máquina dentro de una red que la app cree que no existe.
func TestUnFalloAlAbrirElCanalDeshaceLoQueYaSeHizo(t *testing.T) {
	b := nuevoBanco(t)
	b.control.errServe = errors.New("no se pudo escuchar")

	if _, err := b.sesión.CreateRoom(ctx(), nick(t, "alvaro"), "Los panas"); err == nil {
		t.Fatal("la creación no falló")
	}
	if st := b.sesión.Status(); st.Conn != domain.StateIdle {
		t.Fatalf("quedó en %s", st.Conn)
	}
	var salió bool
	for _, p := range b.motor.pasos() {
		if p == "salir" {
			salió = true
		}
	}
	if !salió {
		t.Fatalf("el motor quedó levantado tras el fallo: %v", b.motor.pasos())
	}
	if b.netcfg.revertió == 0 {
		t.Error("no se revirtieron los ajustes del adaptador")
	}
}

// TestElHostSeMarcaPorElRolYNoPorLoQueDigaElMotor.
//
// El motor no puede saber quién hospeda: es un concepto del producto, no de la
// red. Creérselo a un peer sería dejar que cualquiera se declare host en la
// lista de los demás.
func TestElHostSeMarcaPorElRolYNoPorLoQueDigaElMotor(t *testing.T) {
	b := salaCreada(t)
	self := b.sesión.Status().LocalIP
	intruso := self.Next()

	// El motor reporta al invitado diciendo que él es el host, y a mí sin
	// marca. Es lo que vería un peer modificado.
	b.motor.peers = []domain.Peer{
		{VirtualIP: self, Name: nick(t, "alvaro"), Host: false},
		{VirtualIP: intruso, Name: nick(t, "mallory"), Host: true},
	}
	st, err := b.sesión.OnPeersChanged(ctx())
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range st.Peers {
		if p.VirtualIP == intruso && p.Host {
			t.Fatal("un peer consiguió declararse host en mi lista")
		}
		if p.Self && !p.Host {
			t.Fatal("yo hospedo y mi propio peer perdió la marca")
		}
	}
}

// TestElInvitadoVeAlHostEnLaDirecciónConocida, que es la .1 de la subred, y no
// en la que un peer diga.
func TestElInvitadoVeAlHostEnLaDirecciónConocida(t *testing.T) {
	b := nuevoBanco(t)
	subred := netip.MustParsePrefix("100.87.3.0/24")
	yo := netip.MustParseAddr("100.87.3.5")
	b.control.credencial = domain.Credential{
		ID: "c1", Token: "t", NetworkName: "kanpachi-real", VirtualIP: yo, Subnet: subred,
	}
	b.motor.peers = []domain.Peer{
		{VirtualIP: yo, Name: nick(t, "humberto")},
		{VirtualIP: netip.MustParseAddr("100.87.3.1"), Name: nick(t, "alvaro")},
		{VirtualIP: netip.MustParseAddr("100.87.3.9"), Name: nick(t, "mallory"), Host: true},
	}

	st, err := b.sesión.JoinRoom(ctx(), "A7K2M9QX", nick(t, "humberto"))
	if err != nil {
		t.Fatal(err)
	}
	host, ok := st.HostPeer()
	if !ok {
		t.Fatal("no se identificó al host")
	}
	if host.VirtualIP != netip.MustParseAddr("100.87.3.1") {
		t.Fatalf("el host salió en %s: un peer se hizo pasar por el host", host.VirtualIP)
	}
}

// TestElCableadoIncompletoFallaAlArrancar y no media hora después dentro de una
// operación del usuario, con un panic como mensaje de error.
func TestElCableadoIncompletoFallaAlArrancar(t *testing.T) {
	if _, err := NewSession(ctx(), Deps{}); err == nil {
		t.Fatal("se montó una sesión sin ningún puerto")
	}
	b := nuevoBanco(t)
	incompleto := b.deps
	incompleto.Firewall = nil
	if _, err := NewSession(ctx(), incompleto); err == nil {
		t.Fatal("se montó una sesión sin firewall: abriría una sala sin cuarentena")
	}
}

// TestStatusNoNecesitaElCandado. No se puede probar el bloqueo sin una carrera
// artificial; lo que sí se prueba es que lee la copia publicada, que es lo que
// hace que no lo necesite.
func TestStatusLeeLaCopiaPublicada(t *testing.T) {
	b := nuevoBanco(t)
	if st := b.sesión.Status(); st.Conn != domain.StateIdle {
		t.Fatalf("antes de la primera publicación: %s", st.Conn)
	}
	if _, err := b.sesión.CreateRoom(ctx(), nick(t, "alvaro"), "Los panas"); err != nil {
		t.Fatal(err)
	}
	if st := b.sesión.Status(); st.Conn != domain.StateConnected {
		t.Fatalf("tras crear: %s", st.Conn)
	}

	// Y la copia es una copia: tocar los peers de un Status no puede alcanzar
	// al siguiente.
	primero := b.sesión.Status()
	if len(primero.Peers) > 0 {
		primero.Peers[0].Name = domain.Nickname{}
	}
	if segundo := b.sesión.Status(); len(segundo.Peers) > 0 && segundo.Peers[0].Name.IsZero() {
		t.Fatal("quien recibe un Status puede mutar el estado del daemon")
	}
}

// TestElCódigoLlevaElSeedDelRegistroQueLoEmitió: un invite ID solo significa
// algo en el registro que lo emitió.
func TestElCódigoLlevaElSeedDelRegistroQueLoEmitió(t *testing.T) {
	b := nuevoBanco(t)
	b.registro.seed = "seed.humberto.dev"

	st, err := b.sesión.CreateRoom(ctx(), nick(t, "alvaro"), "Los panas")
	if err != nil {
		t.Fatal(err)
	}
	if st.Room.Seed != "seed.humberto.dev" {
		t.Fatalf("el código apunta a %q y lo emitió otro registro", st.Room.Seed)
	}
	if !strings.Contains(b.sesión.InviteLink(), "seed.humberto.dev") {
		t.Fatalf("el enlace manda al servidor equivocado: %q", b.sesión.InviteLink())
	}
}

// TestUnHostModificadoNoPuedeMandarCualquierCredencial.
//
// Que venga autenticada prueba quién la escribió, no que lo que escribió sea
// coherente. Cada caso rompe algo distinto de esta máquina.
func TestUnHostModificadoNoPuedeMandarCualquierCredencial(t *testing.T) {
	base := domain.Credential{
		ID: "c1", Token: "t", NetworkName: "kanpachi-real",
		VirtualIP: netip.MustParseAddr("100.87.3.5"),
		Subnet:    netip.MustParsePrefix("100.87.3.0/24"),
	}
	casos := []struct {
		nombre string
		muta   func(*domain.Credential)
	}{
		{"sin token", func(c *domain.Credential) { c.Token = "" }},
		{"sin IP", func(c *domain.Credential) { c.VirtualIP = netip.Addr{} }},
		{"la IP fuera de su propia subred", func(c *domain.Credential) {
			c.VirtualIP = netip.MustParseAddr("10.4.4.4")
		}},
		{"la sala en el rango del vestíbulo", func(c *domain.Credential) {
			c.Subnet = domain.RendezvousSubnet
			c.VirtualIP = netip.MustParseAddr("100.127.255.7")
		}},
		{"me da la dirección del host", func(c *domain.Credential) {
			c.VirtualIP = netip.MustParseAddr("100.87.3.1")
		}},
	}
	for _, caso := range casos {
		t.Run(caso.nombre, func(t *testing.T) {
			b := nuevoBanco(t)
			cred := base
			caso.muta(&cred)
			b.control.credencial = cred

			if _, err := b.sesión.JoinRoom(ctx(), "A7K2M9QX", nick(t, "humberto")); err == nil {
				t.Fatal("se aceptó")
			}
			if st := b.sesión.Status(); st.Conn != domain.StateIdle {
				t.Fatalf("quedó en %s", st.Conn)
			}
		})
	}
}

// TestEmitirCredencialNoRepiteDirecciones.
//
// Mirar solo los peers repartiría la misma dirección a dos personas que entran
// a la vez, que es exactamente lo que pasa cuando alguien manda el código al
// grupo: los tres lo pegan al mismo tiempo.
func TestEmitirCredencialNoRepiteDirecciones(t *testing.T) {
	b := salaCreada(t)
	b.motor.credenciales = func() domain.Credential {
		return domain.Credential{ID: "c", Token: "token-del-motor"}
	}

	var vistas []netip.Addr
	for i := 0; i < 3; i++ {
		cred, err := b.sesión.IssueCredentialFor(ctx(), domain.CredentialRequest{Name: nick(t, "humberto")})
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range vistas {
			if v == cred.VirtualIP {
				t.Fatalf("se repartió %s dos veces", cred.VirtualIP)
			}
		}
		vistas = append(vistas, cred.VirtualIP)

		// El motor todavía no reporta a nadie: lo único que impide el choque es
		// la lista de credenciales emitidas.
		b.motor.mu.Lock()
		b.motor.credentials = append(b.motor.credentials, cred)
		b.motor.mu.Unlock()
	}
	if vistas[0] != netip.MustParseAddr(b.sesión.Status().Subnet.Addr().Next().Next().String()) {
		t.Fatalf("la primera credencial no fue la .2: %s", vistas[0])
	}
}

// TestLaCredencialEmitidaNoLlevaElSecretoDeLaRed. Es la propiedad que hace que
// revocar sirva: quien entró nunca tuvo con qué volver por su cuenta.
func TestLaCredencialEmitidaNoLlevaElSecretoDeLaRed(t *testing.T) {
	b := salaCreada(t)
	b.motor.credenciales = func() domain.Credential {
		return domain.Credential{ID: "c", Token: "token-del-motor"}
	}
	cred, err := b.sesión.IssueCredentialFor(ctx(), domain.CredentialRequest{Name: nick(t, "humberto")})
	if err != nil {
		t.Fatal(err)
	}
	if cred.NetworkName != b.motor.hostSpec.RealNetworkName() {
		t.Errorf("la credencial no lleva el nombre de la red real: %q", cred.NetworkName)
	}
	secreto := fmt.Sprintf("%x", b.motor.hostSpec.NetworkSecret)
	if strings.Contains(fmt.Sprintf("%+v", cred), secreto) {
		t.Fatal("el secreto de la red viajó dentro de la credencial")
	}
	if cred.ExpiresAt.Sub(cred.IssuedAt) != CredentialTTL {
		t.Errorf("vencimiento = %s", cred.ExpiresAt.Sub(cred.IssuedAt))
	}
}

// TestNiElHostSeQuedaSinDirecciónPropia: la .1 es suya y no se reparte.
func TestElHostNoRepartesuPropiaDirección(t *testing.T) {
	b := salaCreada(t)
	b.motor.credenciales = func() domain.Credential {
		return domain.Credential{ID: "c", Token: "t"}
	}
	cred, err := b.sesión.IssueCredentialFor(ctx(), domain.CredentialRequest{Name: nick(t, "humberto")})
	if err != nil {
		t.Fatal(err)
	}
	st := b.sesión.Status()
	if cred.VirtualIP == st.LocalIP || cred.VirtualIP == st.Subnet.Addr() {
		t.Fatalf("se repartió una dirección reservada: %s", cred.VirtualIP)
	}
}

// TestUnInvitadoNoEmiteCredenciales: dos máquinas de la misma sala repartiendo
// credenciales distintas sería una sala con dos puertas.
func TestUnInvitadoNoEmiteCredenciales(t *testing.T) {
	b := nuevoBanco(t)
	b.control.credencial = domain.Credential{
		ID: "c1", Token: "t", NetworkName: "kanpachi-real",
		VirtualIP: netip.MustParseAddr("100.87.3.5"),
		Subnet:    netip.MustParsePrefix("100.87.3.0/24"),
	}
	if _, err := b.sesión.JoinRoom(ctx(), "A7K2M9QX", nick(t, "humberto")); err != nil {
		t.Fatal(err)
	}
	if _, err := b.sesión.IssueCredentialFor(ctx(), domain.CredentialRequest{Name: nick(t, "otro")}); !errors.Is(err, ErrNotHost) {
		t.Fatalf("un invitado emitió una credencial: %v", err)
	}
}

// TestNoSeEmiteCredencialSinNombre. Llega de otra máquina, así que se exige
// acá: sin nombres, expulsar a alguien es adivinar.
func TestNoSeEmiteCredencialSinNombre(t *testing.T) {
	b := salaCreada(t)
	if _, err := b.sesión.IssueCredentialFor(ctx(), domain.CredentialRequest{}); !errors.Is(err, domain.ErrNicknameEmpty) {
		t.Fatalf("se emitió una credencial sin nombre: %v", err)
	}
}

// TestElSondeoNoDevuelveAlExpulsado.
//
// La revocación tarda alrededor de un segundo, así que el motor sigue
// reportándolo. Sin la lista de expulsados recientes, el primer evento de
// cambio de miembros lo devuelve a los presentes y le reabre el puerto,
// deshaciendo justo la mitad de la expulsión que era inmediata.
func TestElSondeoNoDevuelveAlExpulsado(t *testing.T) {
	b := salaCreada(t)
	self := b.sesión.Status().LocalIP
	invitado := self.Next()

	b.motor.peers = []domain.Peer{{VirtualIP: self}, {VirtualIP: invitado}}
	b.motor.credentials = []domain.Credential{{ID: "c", VirtualIP: invitado}}
	if _, err := b.sesión.ActivateProfile(ctx(), "project-zomboid"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.sesión.OnPeersChanged(ctx()); err != nil {
		t.Fatal(err)
	}
	if len(b.firewall.estado().Rules) != 1 {
		t.Fatal("no se abrió el puerto, el test no probaría nada")
	}

	if _, err := b.sesión.KickMember(ctx(), invitado); err != nil {
		t.Fatal(err)
	}
	// El motor todavía lo reporta: no se enteró.
	st, err := b.sesión.OnPeersChanged(ctx())
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range st.Peers {
		if p.VirtualIP == invitado {
			t.Fatal("el sondeo devolvió al expulsado a la lista")
		}
	}
	if !b.firewall.estado().IsEmpty() {
		t.Fatalf("el sondeo le reabrió el puerto al expulsado: %+v", b.firewall.estado().Rules)
	}

	// Pasada la ventana, si está, está: volver con un código que el host no
	// renovó es legítimo y tiene que funcionar.
	b.reloj.avanza(KickGrace + time.Second)
	st, err = b.sesión.OnPeersChanged(ctx())
	if err != nil {
		t.Fatal(err)
	}
	var volvió bool
	for _, p := range st.Peers {
		if p.VirtualIP == invitado {
			volvió = true
		}
	}
	if !volvió {
		t.Fatal("el expulsado quedó vetado para siempre: expulsar no es bloquear")
	}
}

// TestSiElFirewallFallaAlExpulsarElCanalYaSeRecortó.
//
// Es la superficie que corre como SYSTEM y parsea entrada de la sala. Quedarse
// sin recortarla porque falló otra cosa deja al expulsado hablándole al código
// que más revisión merece del proyecto.
func TestSiElFirewallFallaAlExpulsarElCanalYaSeRecortó(t *testing.T) {
	b := salaCreada(t)
	self := b.sesión.Status().LocalIP
	invitado := self.Next()

	b.motor.peers = []domain.Peer{{VirtualIP: self}, {VirtualIP: invitado}}
	b.motor.credentials = []domain.Credential{{ID: "c", VirtualIP: invitado}}
	if _, err := b.sesión.OnPeersChanged(ctx()); err != nil {
		t.Fatal(err)
	}
	b.firewall.errApply = errors.New("COM dijo que no")

	if _, err := b.sesión.KickMember(ctx(), invitado); err == nil {
		t.Fatal("no falló")
	}
	for _, ip := range b.control.alcanceActual() {
		if ip == invitado {
			t.Fatal("el expulsado sigue pudiendo hablarle al canal de control")
		}
	}
	if len(b.motor.revocadas) != 1 {
		t.Errorf("la credencial no se revocó: %v", b.motor.revocadas)
	}
}

func salaConInvitado(t *testing.T) *banco {
	t.Helper()
	b := nuevoBanco(t)
	b.control.credencial = domain.Credential{
		ID: "c1", Token: "t", NetworkName: "kanpachi-real",
		VirtualIP: netip.MustParseAddr("100.87.3.5"),
		Subnet:    netip.MustParsePrefix("100.87.3.0/24"),
	}
	b.motor.peers = []domain.Peer{
		{VirtualIP: netip.MustParseAddr("100.87.3.1"), Name: nick(t, "alvaro")},
		{VirtualIP: netip.MustParseAddr("100.87.3.5"), Name: nick(t, "humberto")},
	}
	if _, err := b.sesión.JoinRoom(ctx(), "A7K2M9QX", nick(t, "humberto")); err != nil {
		t.Fatal(err)
	}
	return b
}

// TestElHostAnunciaElJuegoYElInvitadoLoAplica.
//
// Sin esto, la pantalla en sala de un invitado no tiene juego que mostrar, no
// tiene guía de conexión, y `client_ports` es código que nunca corre.
func TestElHostAnunciaElJuegoYElInvitadoLoAplica(t *testing.T) {
	host := salaCreada(t)
	if _, err := host.sesión.ActivateProfile(ctx(), "juego-de-malla"); err != nil {
		t.Fatal(err)
	}
	ann, ok := host.control.últimoAnuncio()
	if !ok {
		t.Fatal("el host no anunció nada")
	}
	if ann.GameID != "juego-de-malla" || ann.RoomName != "Los panas" {
		t.Fatalf("anuncio = %+v", ann)
	}

	invitado := salaConInvitado(t)
	st, err := invitado.sesión.OnRoomAnnounce(ctx(), ann)
	if err != nil {
		t.Fatal(err)
	}
	if st.Game.ID != "juego-de-malla" {
		t.Fatalf("el invitado no aplicó el juego: %q", st.Game.ID)
	}
	if st.Name != "Los panas" {
		t.Errorf("el invitado no recibió el nombre de la sala: %q", st.Name)
	}
	// Y ahora sí, la malla abre en el invitado, que era código inalcanzable.
	rs := invitado.firewall.estado()
	if len(rs.Rules) != 1 || rs.Rules[0].From != 7777 {
		t.Fatalf("client_ports no abrió nada en el invitado: %+v", rs.Rules)
	}
}

// TestElAnuncioLleveIdYNuncaElPerfil: es la diferencia entre que el host diga
// "estamos jugando Zomboid" y que le dicte reglas de firewall a otra máquina.
func TestElInvitadoResuelveElJuegoContraSuPropioCatálogo(t *testing.T) {
	b := salaConInvitado(t)
	st, err := b.sesión.OnRoomAnnounce(ctx(), domain.RoomAnnounce{
		RoomName: "Los panas", GameID: "un-juego-que-no-tengo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !st.Game.IsZero() {
		t.Fatalf("se activó un juego que no está en el catálogo propio: %s", st.Game.ID)
	}
	if !b.firewall.estado().IsEmpty() {
		t.Fatalf("se abrieron puertos por un id que no existe acá: %+v", b.firewall.estado().Rules)
	}
	if b.sesión.MissingGame() != "un-juego-que-no-tengo" {
		t.Errorf("no se puede decir qué perfil falta: %q", b.sesión.MissingGame())
	}
}

// TestElHostNoAceptaAnuncios: aceptarlos le permitiría a un miembro modificado
// cambiarle el juego activo justo a la máquina donde se abren los puertos.
func TestElHostNoAceptaAnuncios(t *testing.T) {
	b := salaCreada(t)
	st, err := b.sesión.OnRoomAnnounce(ctx(), domain.RoomAnnounce{
		RoomName: "Sala de Mallory", GameID: "juego-de-malla",
	})
	if err != nil {
		t.Fatal(err)
	}
	if st.Name != "Los panas" || !st.Game.IsZero() {
		t.Fatalf("un anuncio le cambió el estado al host: %q, %q", st.Name, st.Game.ID)
	}
}

// TestUnAnuncioConBasuraSeAcota. Llega de otra máquina.
func TestUnAnuncioConBasuraSeAcota(t *testing.T) {
	b := salaConInvitado(t)
	st, err := b.sesión.OnRoomAnnounce(ctx(), domain.RoomAnnounce{
		RoomName: strings.Repeat("ñ", 300),
		GameID:   "../../etc/passwd",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len([]rune(st.Name)) != domain.MaxRoomNameLen {
		t.Errorf("el nombre no se acotó: %d runas", len([]rune(st.Name)))
	}
	if !st.Game.IsZero() {
		t.Errorf("un id con forma de ruta llegó a buscarse: %q", st.Game.ID)
	}
}

// TestElNombreSeAcotaAlEscribirYNoSoloAlLeer: el host tiene que ver lo mismo
// que ven los demás.
func TestElNombreSeAcotaAlEscribirYNoSoloAlLeer(t *testing.T) {
	b := salaCreada(t)
	st, err := b.sesión.RenameRoom(ctx(), strings.Repeat("é", 100))
	if err != nil {
		t.Fatal(err)
	}
	if len([]rune(st.Name)) != domain.MaxRoomNameLen {
		t.Fatalf("el host ve %d runas y los demás verían %d", len([]rune(st.Name)), domain.MaxRoomNameLen)
	}
	ann, _ := b.control.últimoAnuncio()
	if ann.RoomName != st.Name {
		t.Errorf("se anunció otro nombre: %q", ann.RoomName)
	}
}

// TestQuienEntraDespuésTambiénSeEntera: no estaba cuando se anunció lo
// anterior, así que su pantalla arrancaría vacía.
func TestQuienEntraDespuésTambiénSeEntera(t *testing.T) {
	b := salaCreada(t)
	if _, err := b.sesión.ActivateProfile(ctx(), "project-zomboid"); err != nil {
		t.Fatal(err)
	}
	antes := len(b.control.anuncios)

	b.motor.peers = []domain.Peer{
		{VirtualIP: b.sesión.Status().LocalIP},
		{VirtualIP: b.sesión.Status().LocalIP.Next()},
	}
	if _, err := b.sesión.OnPeersChanged(ctx()); err != nil {
		t.Fatal(err)
	}
	if len(b.control.anuncios) <= antes {
		t.Fatal("entró alguien y no se le anunció el estado de la sala")
	}
}

// TestLasAlertasDeExposiciónLleganPorStatus.
//
// Es el único canal: el módulo publica su último resultado y Status lo
// arrastra, así que una alerta nunca puede bloquear ni retrasar una respuesta.
func TestLasAlertasDeExposiciónLleganPorStatus(t *testing.T) {
	b := nuevoBanco(t)
	b.auditoría.perfiles = []domain.FirewallProfileState{
		{Profile: domain.ProfileDomain, Enabled: true},
		{Profile: domain.ProfilePublic, Enabled: false},
	}
	b.auditoría.intactas = false
	b.auditoría.mapeos = []domain.PortMapping{
		{ExternalPort: 25565, InternalIP: netip.MustParseAddr("192.168.1.7")},
	}

	st := b.sesión.RefreshAlerts(ctx())
	tipos := map[domain.AlertKind]bool{}
	for _, a := range st.Alerts {
		tipos[a.Kind] = true
	}
	for _, k := range []domain.AlertKind{
		domain.AlertFirewallOff, domain.AlertRulesTampered, domain.AlertRouterMapping,
	} {
		if !tipos[k] {
			t.Errorf("falta la alerta %d: %+v", k, st.Alerts)
		}
	}
	if st2 := b.sesión.Status(); len(st2.Alerts) != len(st.Alerts) {
		t.Error("las alertas no viajan dentro de Status")
	}
}

// TestUnaAuditoríaQueFallaNoRompeNada: cada método responde una pregunta que
// Kanpachi no controla, y que la consulta falle no puede impedir jugar.
func TestUnaAuditoríaQueFallaNoRompeNada(t *testing.T) {
	b := nuevoBanco(t)
	b.auditoría.err = errors.New("COM no responde")

	st := b.sesión.RefreshAlerts(ctx())
	if len(st.Alerts) != 0 {
		t.Fatalf("una auditoría rota inventó alertas: %+v", st.Alerts)
	}
	if _, err := b.sesión.CreateRoom(ctx(), nick(t, "alvaro"), "Los panas"); err != nil {
		t.Fatalf("una auditoría rota impidió crear la sala: %v", err)
	}
}

// TestDiagnoseNoPisaLoQueElMotorNoSabe: el MTU lo sondea netcfg y la subred la
// eligió el plan de direcciones. Pisarlos con ceros dejaría el diagnóstico peor
// que antes de pedirlo.
func TestDiagnoseNoPisaLoQueElMotorNoSabe(t *testing.T) {
	b := salaCreada(t)
	antes := b.sesión.Status().Net

	check, err := b.sesión.Diagnose(ctx())
	if err != nil {
		t.Fatal(err)
	}
	if check.NATKind != "cone" {
		t.Errorf("no llegó lo del motor: %+v", check)
	}
	if check.MTU != antes.MTU || check.MTU == 0 {
		t.Errorf("se perdió el MTU sondeado: %d contra %d", check.MTU, antes.MTU)
	}
	if check.Subnet != antes.Subnet || check.SubnetReason != antes.SubnetReason {
		t.Errorf("se perdió el plan de direcciones: %+v", check)
	}
}

// TestLaFotoDeSocketsSoloMiraElÁrbolYLo QueEscuchaEnTodasLasInterfaces.
func TestLaFotoDeSocketsFiltraLoQueNoSirve(t *testing.T) {
	b := nuevoBanco(t)
	b.deps.Inspector = inspectorFalso{sockets: []domain.Listener{
		{Proto: domain.ProtoUDP, Port: 2456, Address: "0.0.0.0", PID: 10},
		{Proto: domain.ProtoUDP, Port: 2457, Address: "0.0.0.0", PID: 11},
		{Proto: domain.ProtoTCP, Port: 9999, Address: "127.0.0.1", PID: 10},
		{Proto: domain.ProtoTCP, Port: 8888, Address: "192.168.1.5", PID: 10},
		{Proto: domain.ProtoUDP, Port: 27015, Address: "0.0.0.0", PID: 10},
		{Proto: domain.ProtoTCP, Port: 4444, Address: "0.0.0.0", PID: 999},
	}}
	s, err := NewSession(ctx(), b.deps)
	if err != nil {
		t.Fatal(err)
	}

	rangos, err := s.ObserveGame(ctx(),
		domain.ProcessRef{PID: 10, Executable: "valheim.exe"},
		map[int]bool{10: true, 11: true}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rangos) != 1 || rangos[0].Spec() != "2456-2457" {
		t.Fatalf("rangos = %v", rangos)
	}
}

// TestSinÁrbolLaFotoNoDevuelveLaMáquinaEntera: un árbol vacío es que no se
// encontró el proceso, y la respuesta correcta a eso es "no vi nada".
func TestSinÁrbolLaFotoSoloMiraLaRaíz(t *testing.T) {
	b := nuevoBanco(t)
	b.deps.Inspector = inspectorFalso{sockets: []domain.Listener{
		{Proto: domain.ProtoUDP, Port: 2456, Address: "0.0.0.0", PID: 10},
		{Proto: domain.ProtoTCP, Port: 4444, Address: "0.0.0.0", PID: 999},
	}}
	s, err := NewSession(ctx(), b.deps)
	if err != nil {
		t.Fatal(err)
	}
	rangos, err := s.ObserveGame(ctx(), domain.ProcessRef{PID: 10}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rangos) != 1 || rangos[0].From != 2456 {
		t.Fatalf("la foto sin árbol trajo la máquina entera: %v", rangos)
	}
}

// TestNoSePuedeMarcarVerificadoAMano.
//
// "No se puede marcar a mano" no significa nada si la API acepta la marca de
// quien la pida: sin la comprobación, cualquier proceso del usuario escribe
// "verificado en partida real" en un perfil que nadie probó.
func TestNoSePuedeMarcarVerificadoAMano(t *testing.T) {
	b := nuevoBanco(t)
	_, err := b.sesión.SaveProfile(ctx(), domain.GameProfile{
		ID:        "valheim",
		Name:      "Valheim",
		HostPorts: []domain.PortRange{{Proto: domain.ProtoUDP, From: 2456, To: 2458}},
		Connect:   domain.ConnectHint{Kind: domain.ConnectDirectIP},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	err = b.sesión.MarkVerified(ctx(), "valheim", domain.Verified{By: "yo", Method: "porque sí"})
	if !errors.Is(err, ErrNotPlayed) {
		t.Fatalf("se marcó verificado sin partida: %v", err)
	}
}

// TestSoloSeVerificaTrasUnaSalaConMásGente, que son las dos condiciones que
// admite el documento.
func TestSoloSeVerificaTrasUnaSalaConMásGente(t *testing.T) {
	b := salaCreada(t)
	if _, err := b.sesión.SaveProfile(ctx(), domain.GameProfile{
		ID:        "valheim",
		Name:      "Valheim",
		HostPorts: []domain.PortRange{{Proto: domain.ProtoUDP, From: 2456, To: 2458}},
		Connect:   domain.ConnectHint{Kind: domain.ConnectDirectIP},
	}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := b.sesión.ActivateProfile(ctx(), "valheim"); err != nil {
		t.Fatal(err)
	}

	// Solo yo en la sala: no habilita nada.
	b.sesión.LeaveRoom(ctx())
	if err := b.sesión.MarkVerified(ctx(), "valheim", domain.Verified{By: "alvaro"}); !errors.Is(err, ErrNotPlayed) {
		t.Fatalf("una sala de una persona habilitó la marca: %v", err)
	}

	// Ahora con alguien más.
	b2 := salaCreada(t)
	if _, err := b2.sesión.SaveProfile(ctx(), domain.GameProfile{
		ID:        "valheim",
		Name:      "Valheim",
		HostPorts: []domain.PortRange{{Proto: domain.ProtoUDP, From: 2456, To: 2458}},
		Connect:   domain.ConnectHint{Kind: domain.ConnectDirectIP},
	}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := b2.sesión.ActivateProfile(ctx(), "valheim"); err != nil {
		t.Fatal(err)
	}
	self := b2.sesión.Status().LocalIP
	b2.motor.peers = []domain.Peer{{VirtualIP: self}, {VirtualIP: self.Next()}}
	if _, err := b2.sesión.OnPeersChanged(ctx()); err != nil {
		t.Fatal(err)
	}
	b2.sesión.LeaveRoom(ctx())

	if err := b2.sesión.MarkVerified(ctx(), "valheim", domain.Verified{By: "alvaro", Method: "partida real"}); err != nil {
		t.Fatalf("no se pudo verificar tras una partida real: %v", err)
	}
	// Y una sola vez: el testigo se consume.
	if err := b2.sesión.MarkVerified(ctx(), "valheim", domain.Verified{By: "alvaro"}); !errors.Is(err, ErrNotPlayed) {
		t.Error("el permiso para verificar se pudo usar dos veces")
	}
}

// TestGuardarNoTapaUnBuiltinEnSilencio: taparlo deja al juego sin recibir las
// correcciones que trae cada versión de la app.
func TestGuardarNoTapaUnBuiltinEnSilencio(t *testing.T) {
	b := nuevoBanco(t)
	choca := domain.GameProfile{
		ID:        "project-zomboid",
		Name:      "Zomboid mío",
		HostPorts: []domain.PortRange{{Proto: domain.ProtoUDP, From: 16261, To: 16262}},
		Connect:   domain.ConnectHint{Kind: domain.ConnectDirectIP},
	}
	if _, err := b.sesión.SaveProfile(ctx(), choca, false); !errors.Is(err, ErrShadowsBuiltin) {
		t.Fatalf("se tapó un builtin sin preguntar: %v", err)
	}
	if b.almacén.escrituras != 0 {
		t.Fatal("se escribió igual")
	}
	// Con el sí explícito del usuario, entra.
	if _, err := b.sesión.SaveProfile(ctx(), choca, true); err != nil {
		t.Fatalf("con el permiso del usuario no entró: %v", err)
	}
}

// TestNoSeEntraAUnaSalaQuePisaLaLANDeCasa.
//
// La subred la eligió el host mirando SU máquina. Instalarla sin comprobarla
// acá deja sin internet a quien tenga ese rango, y el síntoma sería que entrar
// a la sala te desconecta de todo.
func TestNoSeEntraAUnaSalaQuePisaLaLANDeCasa(t *testing.T) {
	b := nuevoBanco(t)
	b.deps.Routes = rutasFalsas{prefijos: []netip.Prefix{netip.MustParsePrefix("192.168.1.0/24")}}
	b.control.credencial = domain.Credential{
		ID: "c1", Token: "t", NetworkName: "kanpachi-real",
		VirtualIP: netip.MustParseAddr("192.168.1.5"),
		Subnet:    netip.MustParsePrefix("192.168.1.0/24"),
	}
	s, err := NewSession(ctx(), b.deps)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.JoinRoom(ctx(), "A7K2M9QX", nick(t, "humberto")); err == nil {
		t.Fatal("se entró a una sala que pisa la LAN de casa")
	} else if !strings.Contains(err.Error(), "192.168.1.0/24") {
		t.Errorf("el error no dice qué red se pisaba: %v", err)
	}
	if st := s.Status(); st.Conn != domain.StateIdle {
		t.Fatalf("quedó en %s", st.Conn)
	}
}

// TestDirectPlaySeEnciendeConElPerfilYSeApagaAlQuitarlo.
func TestDirectPlaySigueAlPerfil(t *testing.T) {
	b := nuevoBanco(t)
	b.almacén.builtin = []byte(`{"kanpachi_catalog":1,"profiles":[
	  {"id":"viejo","schema":2,"name":"Juego viejo",
	   "host_ports":[{"proto":"udp","range":"6073"}],"client_ports":[],
	   "system_tweaks":{"broadcast_route":true,"multicast_route":false,"prefer_ipv4":false,"directplay":true},
	   "connect_hint":{"kind":"lan_browser","text_es":"aparece solo"}}]}`)
	s, err := NewSession(ctx(), b.deps)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateRoom(ctx(), nick(t, "alvaro"), "Los panas"); err != nil {
		t.Fatal(err)
	}
	if b.netcfg.directPlay {
		t.Fatal("DirectPlay encendido sin juego activo")
	}
	if _, err := s.ActivateProfile(ctx(), "viejo"); err != nil {
		t.Fatal(err)
	}
	if !b.netcfg.directPlay {
		t.Fatal("el perfil pedía DirectPlay y no se encendió")
	}
	if _, err := s.ActivateProfile(ctx(), ""); err != nil {
		t.Fatal(err)
	}
	if b.netcfg.directPlay {
		t.Fatal("quitar el juego no apagó DirectPlay")
	}
}
