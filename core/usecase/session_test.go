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

	st, err := b.session.CreateRoom(ctx(), nick(t, "alvaro"), "Los panas", false)
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
	st, err := b.session.CreateRoom(ctx(), nick(t, "alvaro"), "Los panas", false)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Game.IsZero() {
		t.Fatalf("la sala nació con juego: %s", st.Game.ID)
	}
	// Reglas de JUEGO, que es lo que promete la cuarentena. El hueco del canal
	// de control es otra cosa: no lo pide ningún perfil, va al puerto de
	// Kanpachi y no al de ningún juego, y existe desde que el host abre la sala.
	if reglas := b.firewall.estado().GameRules(); len(reglas) > 0 {
		t.Fatalf("la sala nació con reglas de juego: %+v", reglas)
	}
}

// TestElSecretoDeLaRedRealNoDerivaDelCódigo: es la propiedad central de la
// decisión 2. Dos salas creadas con el mismo invite ID tienen que tener
// identidades de red distintas.
func TestElSecretoDeLaRedRealNoDerivaDelCódigo(t *testing.T) {
	b := nuevoBanco(t)
	if _, err := b.session.CreateRoom(ctx(), nick(t, "alvaro"), "Los panas", false); err != nil {
		t.Fatal(err)
	}
	spec := b.motor.hostSpec

	rdv := domain.DeriveRendezvous(b.session.Status().Room.InviteID)
	if spec.RealNetworkName() == rdv.NetworkName() {
		t.Fatal("la red real y el vestíbulo son la misma: el código sería el secreto de la sala")
	}
	var cero [32]byte
	if spec.NetworkSecret == cero {
		t.Fatal("el secreto de la red real quedó en cero")
	}
}

// TestSinRegistroNoSeCreaLaSala fija que crear FALLA cuando el registro no
// contesta, y que falla sin haber tocado nada.
//
// Este test decía lo contrario, y lo que protegía era un fallo. Con el respaldo,
// el invite ID lo generaba la propia máquina y salía marcado con el seed por
// defecto, que es EL MISMO que consulta el invitado: le preguntaban al registro,
// contestaba que no existe, y lo rechazaba antes de arrancar el motor. O sea que
// el host se quedaba con un código de aspecto normal que no le servía a nadie.
//
// Lo que se comprueba además del error es que no quede nada a medias: el motor
// sin arrancar y el firewall sin escribir. Es lo que separa fallar rápido de
// fallar tarde.
func TestSinRegistroNoSeCreaLaSala(t *testing.T) {
	b := nuevoBanco(t)
	b.registry.err = errors.New("504")

	_, err := b.session.CreateRoom(ctx(), nick(t, "alvaro"), "Los panas", false)
	if !errors.Is(err, ErrNoRegistry) {
		t.Fatalf("sin registro la sala se creó igual, o falló por otra cosa: %v", err)
	}
	if st := b.session.Status(); !st.Room.InviteID.IsZero() {
		t.Fatalf("quedó un código repartible de una sala que no existe: %s", st.Room.InviteID)
	}
	// Por el SECRETO y no por el struct entero, que lleva slices y no se puede
	// comparar, ni por `RealNetworkName`, que sobre un spec en cero devuelve
	// `kanpachi-` más ceros en vez de vacío. Un secreto en cero es que nadie
	// llamó a `HostNetwork`: uno de verdad siempre sale aleatorio, y hay un test
	// aparte que lo fija.
	if b.motor.hostSpec.NetworkSecret != [32]byte{} {
		t.Fatal("se levantó la red de una sala que no se pudo registrar")
	}
	if reglas := b.firewall.estado().GameRules(); len(reglas) > 0 {
		t.Fatalf("se escribieron reglas de una sala que no se pudo registrar: %+v", reglas)
	}
}

// TestElRegistroRecibeLaTarjetaCifradaYNoElNombre. Si algún día el servidor
// pudiera leer nombres de sala o nicks, sería una decisión de producto que se
// escribe en la 17, no un detalle de implementación.
func TestElRegistroRecibeLaTarjetaCifradaYNoElNombre(t *testing.T) {
	b := nuevoBanco(t)
	if _, err := b.session.CreateRoom(ctx(), nick(t, "alvaro"), "Los panas", false); err != nil {
		t.Fatal(err)
	}
	depositado := string(b.registry.publicado)
	if strings.Contains(depositado, "Los panas") || strings.Contains(depositado, "alvaro") {
		t.Fatalf("el nombre de la sala o el nick viajaron en claro al registro: %q", depositado)
	}
}

// TestElEnlaceLlevaLaClaveDeLaTarjeta, que es lo único que permite descifrarla
// y lo único que el servidor no recibe.
func TestElEnlaceLlevaLaClaveDeLaTarjeta(t *testing.T) {
	b := nuevoBanco(t)
	if _, err := b.session.CreateRoom(ctx(), nick(t, "alvaro"), "Los panas", false); err != nil {
		t.Fatal(err)
	}
	link := b.session.InviteLink()
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
	card, err := domain.OpenRoomCard(b.registry.publicado, key)
	if err != nil {
		t.Fatalf("la clave del enlace no abre la tarjeta que se depositó: %v", err)
	}
	if card.Name != "Los panas" || card.Host.String() != "alvaro" {
		t.Fatalf("la tarjeta dice otra cosa: %+v", card)
	}
}

func TestNoSePuedeEstarEnDosSalas(t *testing.T) {
	b := nuevoBanco(t)
	if _, err := b.session.CreateRoom(ctx(), nick(t, "alvaro"), "Los panas", false); err != nil {
		t.Fatal(err)
	}
	if _, err := b.session.CreateRoom(ctx(), nick(t, "alvaro"), "Otra", false); !errors.Is(err, ErrBusy) {
		t.Fatalf("se crearon dos salas: %v", err)
	}
	if _, err := b.session.JoinRoom(ctx(), "A7K2M9QX@seed.midominio.com", nick(t, "alvaro"), false); !errors.Is(err, ErrBusy) {
		t.Fatalf("se entró a otra sala teniendo una: %v", err)
	}
}

// TestUnFalloAMitadDeCaminoVuelveAIdle: sin esto, la sesión se quedaría en
// Resolving para siempre y la UI mostrando una ruedita que no gira a ningún
// lado.
func TestUnFalloAMitadDeCaminoVuelveAIdle(t *testing.T) {
	b := nuevoBanco(t)
	b.motor.errHost = errors.New("el motor no arrancó")

	if _, err := b.session.CreateRoom(ctx(), nick(t, "alvaro"), "Los panas", false); err == nil {
		t.Fatal("la creación no falló")
	}
	if st := b.session.Status(); st.Conn != domain.StateIdle {
		t.Fatalf("quedó en %s", st.Conn)
	}
}

// lobbyDe es la .1 del vestíbulo de la sala que tenga esa sesión.
//
// Se deriva del código en vez de escribirse porque desde el 2026-08-11 no hay
// una dirección fija: cada sala saca la suya de su invite ID. Ver
// [domain.Rendezvous.LobbySubnet].
func lobbyDe(b *bank) netip.Addr {
	return domain.DeriveRendezvous(b.session.Status().Room.InviteID).LobbyHostAddress()
}

func salaCreada(t *testing.T) *bank {
	t.Helper()
	b := nuevoBanco(t)
	if _, err := b.session.CreateRoom(ctx(), nick(t, "alvaro"), "Los panas", false); err != nil {
		t.Fatal(err)
	}
	return b
}

// emiteCredencial pide una credencial por el camino de verdad, que es el único
// que ata una dirección a un `CredentialID`.
//
// Antes los tests la fabricaban a mano dentro del motor falso, con su
// `VirtualIP` puesta. **El adaptador real no puede devolver eso**: el motor no
// sabe qué dirección lleva cada credencial, así que su lista viene con la IP en
// cero. Con esa diferencia, ocho tests de expulsión pasaban en verde sobre un
// producto en el que ni expulsar ni entrar funcionaban. Ver
// [port.EnginePort.ListCredentials].
//
// Comprueba de paso que la dirección elegida sea `quiero`, que es la que el
// test va a expulsar: sin eso, un reparto distinto fallaría más tarde y en otro
// sitio.
func emiteCredencial(t *testing.T, b *bank, nombre string, id domain.CredentialID, quiero netip.Addr) {
	t.Helper()
	b.motor.mu.Lock()
	b.motor.credenciales = func() domain.Credential {
		return domain.Credential{ID: id, Token: "token-del-motor"}
	}
	b.motor.mu.Unlock()

	cred, err := b.session.IssueCredentialFor(ctx(), domain.CredentialRequest{Name: nick(t, nombre)})
	if err != nil {
		t.Fatal(err)
	}
	if cred.VirtualIP != quiero {
		t.Fatalf("el host asignó %s y el test expulsa a %s", cred.VirtualIP, quiero)
	}
}

// TestElJuegoNoAbreNadaHastaQueHayaAlguien: RemoteAddresses son siempre los
// miembros presentes y no existe forma de decir "cualquiera".
func TestElJuegoNoAbreNadaHastaQueHayaAlguien(t *testing.T) {
	b := salaCreada(t)

	if _, err := b.session.ActivateProfile(ctx(), "project-zomboid"); err != nil {
		t.Fatal(err)
	}
	if reglas := b.firewall.estado().GameRules(); len(reglas) > 0 {
		t.Fatalf("se abrieron puertos con la sala vacía: %+v", reglas)
	}
}

func TestElJuegoAbreLosPuertosCuandoEntraAlguien(t *testing.T) {
	b := salaCreada(t)
	self := b.session.Status().LocalIP
	invitado := self.Next()

	b.motor.peers = []domain.Peer{
		{VirtualIP: self, Name: nick(t, "alvaro"), Host: true},
		{VirtualIP: invitado, Name: nick(t, "humberto"), Path: domain.PathDirect},
	}
	if _, err := b.session.ActivateProfile(ctx(), "project-zomboid"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.session.OnPeersChanged(ctx()); err != nil {
		t.Fatal(err)
	}

	reglas := b.firewall.estado().GameRules()
	if len(reglas) != 1 {
		t.Fatalf("reglas de juego = %d: %+v", len(reglas), reglas)
	}
	r := reglas[0]
	if r.From != 16261 || r.To != 16262 || r.Proto != domain.ProtoUDP {
		t.Errorf("la regla no es la del perfil: %+v", r)
	}
	if len(r.Remote) != 1 || r.Remote[0] != invitado {
		t.Errorf("el alcance no es el invitado: %v", r.Remote)
	}
}

// La compuerta se acota ANTES de que se abra un solo puerto, y este test
// afirma la consecuencia y no la llamada.
//
// El fallo que congela no es "no se llamó a BindRoom": es que con la compuerta
// suelta, la lista de permitidos vuelve a ser ADITIVA, y ahí una regla ajena de
// escritorio remoto alcanza al usuario por la red virtual. Por eso el falso
// levanta la bandera dentro de `Apply`, mirando si había alcance en el momento
// de abrir, en vez de contar invocaciones.
//
// El host se acota a los DOS adaptadores. El vestíbulo es donde llega gente que
// todavía no es miembro, o sea el que menos puede quedarse sin compuerta.
func TestLaCompuertaSeAcotaAntesDeAbrirNada(t *testing.T) {
	b := salaCreada(t)
	if _, err := b.session.ActivateProfile(ctx(), "project-zomboid"); err != nil {
		t.Fatal(err)
	}

	if b.firewall.abrióSinCompuerta {
		t.Error("se abrieron puertos con la compuerta suelta, o sea sin nada que los acote")
	}
	red, vínculo := b.firewall.alcance()
	if red != b.session.Status().Subnet {
		t.Errorf("la compuerta quedó acotada a %v y la sala es %v", red, b.session.Status().Subnet)
	}
	if vínculo != domain.BindRoomAndLobby {
		t.Errorf("el host se acotó a %v, y tiene sala y vestíbulo", vínculo)
	}
}

// El invitado TAMBIÉN acota la compuerta, y con la sala sola.
//
// No es un extra: `BuildRuleSet` le abre sus `ClientPorts`, o sea que un
// invitado también escribe permisos y también necesita quién los acote frente a
// los demás miembros. Y va sin vestíbulo porque lo soltó al entrar, a propósito:
// quedarse ahí mantendría abierta una vía por la que un desconocido con el
// código ve que esta máquina está en esa sala.
func TestElInvitadoAcotaLaCompuertaALaSalaSola(t *testing.T) {
	b := nuevoBanco(t)
	b.control.credencial = domain.Credential{
		ID: "c1", Token: "t", NetworkName: "kanpachi-real",
		VirtualIP: netip.MustParseAddr("100.87.3.5"),
		Subnet:    netip.MustParsePrefix("100.87.3.0/24"),
	}
	if _, err := b.session.JoinRoom(ctx(), "kanpachi://A7K2-M9QX@seed.midominio.com", nick(t, "humberto"), false); err != nil {
		t.Fatal(err)
	}

	if b.firewall.abrióSinCompuerta {
		t.Error("el invitado abrió puertos con la compuerta suelta")
	}
	red, vínculo := b.firewall.alcance()
	if red != b.session.Status().Subnet {
		t.Errorf("la compuerta quedó acotada a %v y la sala es %v", red, b.session.Status().Subnet)
	}
	if vínculo != domain.BindRoomOnly {
		t.Errorf("el invitado se acotó a %v, y ya soltó el vestíbulo", vínculo)
	}
}

// Y salir la suelta, DESPUÉS de cerrar los puertos.
func TestSalirSueltaLaCompuerta(t *testing.T) {
	b := salaCreada(t)
	if red, _ := b.firewall.alcance(); !red.IsValid() {
		t.Fatal("este test no prueba nada: la compuerta ya estaba suelta")
	}
	b.session.LeaveRoom(ctx())
	if red, _ := b.firewall.alcance(); red.IsValid() {
		t.Errorf("la compuerta quedó acotada a %v con la sala cerrada", red)
	}
}

// Un fallo al acotar la compuerta NO abre la sala.
//
// Es la diferencia con los ajustes del adaptador, que sí son best effort: un
// MTU mal puesto degrada la partida, y una sala sin compuerta miente sobre lo
// único que este producto promete.
func TestSinCompuertaNoHaySala(t *testing.T) {
	b := nuevoBanco(t)
	b.firewall.errBind = errors.New("no se encontró el adaptador")

	if _, err := b.session.CreateRoom(ctx(), nick(t, "alvaro"), "Prueba", false); err == nil {
		t.Fatal("la sala se abrió sin compuerta")
	}
	if st := b.session.Status(); st.Conn != domain.StateIdle {
		t.Errorf("el estado quedó en %s en vez de volver a idle", st.Conn)
	}
}

// TestElPerfilLlevaSusAjustesAlAdaptador, y quitarlo los revierte sin que
// nadie tenga que deshacerlos uno por uno.
func TestElPerfilLlevaSusAjustesAlAdaptador(t *testing.T) {
	b := salaCreada(t)

	if _, err := b.session.ActivateProfile(ctx(), "project-zomboid"); err != nil {
		t.Fatal(err)
	}
	if !b.netcfg.estado().MulticastRoute {
		t.Fatal("el perfil pedía ruta de multicast y no llegó al adaptador")
	}
	if _, err := b.session.ActivateProfile(ctx(), ""); err != nil {
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
	_, err := b.session.ActivateProfile(ctx(), "un-juego-que-no-existe")
	if !errors.Is(err, ErrUnknownGame) {
		t.Fatalf("se aceptó un juego fuera del catálogo: %v", err)
	}
}

// TestSiElFirewallRechazaElConjuntoElJuegoNoSeDaPorActivo: la UI mostraría
// puertos abiertos que no lo están.
func TestSiElFirewallRechazaElConjuntoElJuegoNoSeDaPorActivo(t *testing.T) {
	b := salaCreada(t)
	b.firewall.errApply = errors.New("COM dijo que no")

	if _, err := b.session.ActivateProfile(ctx(), "project-zomboid"); err == nil {
		t.Fatal("se dio por activado")
	}
	if st := b.session.Status(); !st.Game.IsZero() {
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
	if _, err := b.session.JoinRoom(ctx(), "A7K2M9QX@seed.midominio.com", nick(t, "humberto"), false); err != nil {
		t.Fatal(err)
	}

	if _, err := b.session.ActivateProfile(ctx(), "project-zomboid"); !errors.Is(err, ErrNotHost) {
		t.Errorf("un invitado activó un juego: %v", err)
	}
	if _, err := b.session.KickMember(ctx(), netip.MustParseAddr("100.87.3.1")); !errors.Is(err, ErrNotHost) {
		t.Errorf("un invitado expulsó a alguien: %v", err)
	}
	if _, err := b.session.RotateInviteCode(ctx()); !errors.Is(err, ErrNotHost) {
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

	st, err := b.session.JoinRoom(ctx(), "kanpachi://A7K2-M9QX@seed.midominio.com", nick(t, "humberto"), false)
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
	if _, err := b.session.JoinRoom(ctx(), "A7K2M9QX@seed.midominio.com", nick(t, "humberto"), false); err != nil {
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
	if marcados[0] != lobbyDe(b) {
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

	if _, err := b.session.JoinRoom(ctx(), "A7K2M9QX@seed.midominio.com", nick(t, "humberto"), false); err == nil {
		t.Fatal("se entró sin canal con el host")
	}
	if st := b.session.Status(); st.Conn != domain.StateIdle {
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

	if scope.Lobby != lobbyDe(b) {
		t.Errorf("la puerta no está en la dirección conocida del vestíbulo: %s", scope.Lobby)
	}
	if scope.Room != b.session.Status().LocalIP {
		t.Errorf("la sala no escucha en la IP del adaptador: %s", scope.Room)
	}
	// El host entró al vestíbulo además de levantar la red real: sin eso nadie
	// puede alcanzarlo para pedirle la credencial.
	pasos := b.motor.pasos()
	if len(pasos) != 2 || pasos[0] != "host" || pasos[1] != "vestíbulo" {
		t.Fatalf("pasos del motor al crear = %v", pasos)
	}
	if b.motor.rdvSpec.Address != lobbyDe(b) {
		t.Errorf("el host no tomó su dirección fija en el vestíbulo: %s", b.motor.rdvSpec.Address)
	}
}

// TestElVestíbuloNoSeLeEntregaAUnaSala: si coincidieran, entrar a la sala
// cortaría la conexión que se está usando para pedir la credencial.
func TestElVestíbuloNoSeLeEntregaAUnaSala(t *testing.T) {
	b := salaCreada(t)
	if domain.LobbySpace.Overlaps(b.session.Status().Subnet) {
		t.Fatal("la sala cayó en el espacio de los vestíbulos")
	}
}

// TestUnaCredencialAMediasSeRechaza: llega de otra máquina, así que se revisa.
func TestUnaCredencialAMediasSeRechaza(t *testing.T) {
	b := nuevoBanco(t)
	b.control.credencial = domain.Credential{ID: "c1"} // sin token, sin IP, sin subred

	if _, err := b.session.JoinRoom(ctx(), "A7K2M9QX@seed.midominio.com", nick(t, "humberto"), false); err == nil {
		t.Fatal("se entró con una credencial incompleta")
	}
	if st := b.session.Status(); st.Conn != domain.StateIdle {
		t.Fatalf("quedó en %s", st.Conn)
	}
}

func TestSiElHostNoRespondeElIngresoFalla(t *testing.T) {
	b := nuevoBanco(t)
	b.control.errDial = errors.New("conexión rechazada")

	_, err := b.session.JoinRoom(ctx(), "A7K2M9QX@seed.midominio.com", nick(t, "humberto"), false)
	if err == nil {
		t.Fatal("se entró sin host")
	}
	if !strings.Contains(err.Error(), "reconectando") {
		t.Errorf("el mensaje manda a revisar lo equivocado: %v", err)
	}
}

func TestUnCódigoConFormaRaraNiSiquieraMueveElEstado(t *testing.T) {
	b := nuevoBanco(t)
	if _, err := b.session.JoinRoom(ctx(), "no-es-un-código", nick(t, "humberto"), false); err == nil {
		t.Fatal("se aceptó")
	}
	if st := b.session.Status(); st.Conn != domain.StateIdle {
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
	self := b.session.Status().LocalIP
	invitado := self.Next()

	b.motor.peers = []domain.Peer{
		{VirtualIP: self, Name: nick(t, "alvaro"), Host: true},
		{VirtualIP: invitado, Name: nick(t, "humberto")},
	}
	emiteCredencial(t, b, "humberto", "cred-humberto", invitado)

	if _, err := b.session.ActivateProfile(ctx(), "project-zomboid"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.session.OnPeersChanged(ctx()); err != nil {
		t.Fatal(err)
	}
	if len(b.firewall.estado().GameRules()) != 1 {
		t.Fatal("no se llegó a abrir el puerto, el test no probaría nada")
	}

	if _, err := b.session.KickMember(ctx(), invitado); err != nil {
		t.Fatal(err)
	}
	if len(b.motor.revocadas) != 1 || b.motor.revocadas[0] != "cred-humberto" {
		t.Errorf("no se revocó la credencial: %v", b.motor.revocadas)
	}
	// Ni en las reglas del juego ni en el hueco del canal: expulsar cierra las
	// dos, y la segunda es la que corre como SYSTEM.
	for _, r := range b.firewall.estado().Rules {
		for _, ip := range r.Remote {
			if ip == invitado {
				t.Errorf("el expulsado sigue autorizado en %q: %+v", r.Name, r)
			}
		}
	}
	if reglas := b.firewall.estado().GameRules(); len(reglas) > 0 {
		t.Errorf("quedaron reglas de juego sin nadie a quien autorizar: %+v", reglas)
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
	self := b.session.Status().LocalIP
	invitado := self.Next()

	b.motor.peers = []domain.Peer{
		{VirtualIP: self, Host: true},
		{VirtualIP: invitado},
	}
	emiteCredencial(t, b, "humberto", "c", invitado)
	if _, err := b.session.OnPeersChanged(ctx()); err != nil {
		t.Fatal(err)
	}

	// El motor sigue reportando a los dos: todavía no se enteró.
	st, err := b.session.KickMember(ctx(), invitado)
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
	self := b.session.Status().LocalIP
	if _, err := b.session.KickMember(ctx(), self); !errors.Is(err, ErrSelfKick) {
		t.Fatalf("se aceptó autoexpulsión: %v", err)
	}
	if _, err := b.session.KickMember(ctx(), netip.MustParseAddr("10.0.0.9")); !errors.Is(err, ErrNotAMember) {
		t.Fatalf("se aceptó expulsar a un desconocido: %v", err)
	}
}

// TestRenovarElCódigoNoTocaALosPresentes es la mitad de la decisión 22 que la
// derivación local pura no podía dar.
func TestRenovarElCódigoNoTocaALosPresentes(t *testing.T) {
	b := salaCreada(t)
	self := b.session.Status().LocalIP
	invitado := self.Next()

	b.motor.peers = []domain.Peer{{VirtualIP: self, Host: true}, {VirtualIP: invitado}}
	if _, err := b.session.ActivateProfile(ctx(), "project-zomboid"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.session.OnPeersChanged(ctx()); err != nil {
		t.Fatal(err)
	}
	antes := b.session.Status()
	reglasAntes := b.firewall.estado()

	b.registry.siguiente = "B4N9PQRS"
	st, err := b.session.RotateInviteCode(ctx())
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
	if _, err := b.session.ActivateProfile(ctx(), "project-zomboid"); err != nil {
		t.Fatal(err)
	}

	st := b.session.LeaveRoom(ctx())
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
	if st := b.session.LeaveRoom(ctx()); st.Conn != domain.StateIdle {
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
	if _, err := b.session.JoinRoom(ctx(), "A7K2M9QX@seed.midominio.com", nick(t, "humberto"), false); err != nil {
		t.Fatal(err)
	}

	b.session.SetHostPresent(false)
	b.clock.avanza(19 * time.Minute)
	if b.session.TickHostAbsence(ctx()) {
		t.Fatal("salió a los diecinueve minutos")
	}
	b.clock.avanza(2 * time.Minute)
	if !b.session.TickHostAbsence(ctx()) {
		t.Fatal("no salió pasados los veinte minutos")
	}
	if st := b.session.Status(); st.Conn != domain.StateIdle {
		t.Fatalf("estado = %s", st.Conn)
	}
}

// TestElHostNoSeEchaDeSuPropiaSala, ni siquiera si el supervisor le manda una
// ausencia por error.
func TestElHostNoSeEchaDeSuPropiaSalaPorElContador(t *testing.T) {
	b := salaCreada(t)
	b.session.SetHostPresent(false)
	b.clock.avanza(2 * time.Hour)

	if b.session.TickHostAbsence(ctx()) {
		t.Fatal("el host salió de la sala que hospeda")
	}
}

func TestElCatálogoSeCargaConPrecedencia(t *testing.T) {
	b := nuevoBanco(t)
	juegos := b.session.ListGames()
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
	b.catalog.local = []byte(`{"kanpachi_catalog": esto no es json`)
	b.session.reloadCatalog(ctx())

	if len(b.session.ListGames()) != 2 {
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
	guardado, err := b.session.SaveProfile(ctx(), nuevo, false)
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
	for _, p := range b.session.ListGames() {
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
	_, err := b.session.SaveProfile(ctx(), domain.GameProfile{
		ID:        "malo",
		Name:      "Malo",
		HostPorts: []domain.PortRange{{Proto: domain.ProtoTCP, From: 440, To: 450}},
		Connect:   domain.ConnectHint{Kind: domain.ConnectDirectIP},
	}, false)
	if !errors.Is(err, domain.ErrPortForbidden) {
		t.Fatalf("se guardó un perfil que abre el 445: %v", err)
	}
	if b.catalog.escrituras != 0 {
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
	cands, err := b.session.ImportCatalog(ctx(), archivo, []string{"valheim", "terraria", "rust"})
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 3 {
		t.Fatalf("candidatos = %d", len(cands))
	}

	ids := map[string]domain.Origin{}
	for _, p := range b.session.ListGames() {
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

	if _, err := b.session.ImportCatalog(ctx(), archivo, nil); err != nil {
		t.Fatal(err)
	}
	if b.catalog.escrituras != 0 {
		t.Fatal("se reescribió local.json sin importar nada")
	}
}

// TestUnBuiltinNoSePuedeVerificarPorLaPuertaDeAtrás: escribir una copia local
// crearía un perfil "mine" que tapa al builtin, y a partir de ahí las
// actualizaciones de la app dejarían de llegarle a ese juego.
func TestUnBuiltinNoSePuedeVerificarPorLaPuertaDeAtrás(t *testing.T) {
	b := salaCreada(t)
	if _, err := b.session.ActivateProfile(ctx(), "project-zomboid"); err != nil {
		t.Fatal(err)
	}
	self := b.session.Status().LocalIP
	b.motor.peers = []domain.Peer{{VirtualIP: self}, {VirtualIP: self.Next()}}
	if _, err := b.session.OnPeersChanged(ctx()); err != nil {
		t.Fatal(err)
	}
	b.session.LeaveRoom(ctx())

	if err := b.session.MarkVerified(ctx(), "project-zomboid",
		domain.Verified{By: "alvaro", Method: "partida real"}); err != nil {
		t.Fatal(err)
	}
	if b.catalog.escrituras != 0 {
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

	if _, err := b.session.CreateRoom(ctx(), nick(t, "alvaro"), "Los panas", false); err == nil {
		t.Fatal("la creación no falló")
	}
	if st := b.session.Status(); st.Conn != domain.StateIdle {
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
	self := b.session.Status().LocalIP
	intruso := self.Next()

	// El motor reporta al invitado diciendo que él es el host, y a mí sin
	// marca. Es lo que vería un peer modificado.
	b.motor.peers = []domain.Peer{
		{VirtualIP: self, Name: nick(t, "alvaro"), Host: false},
		{VirtualIP: intruso, Name: nick(t, "mallory"), Host: true},
	}
	st, err := b.session.OnPeersChanged(ctx())
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

	st, err := b.session.JoinRoom(ctx(), "A7K2M9QX@seed.midominio.com", nick(t, "humberto"), false)
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
	if st := b.session.Status(); st.Conn != domain.StateIdle {
		t.Fatalf("antes de la primera publicación: %s", st.Conn)
	}
	if _, err := b.session.CreateRoom(ctx(), nick(t, "alvaro"), "Los panas", false); err != nil {
		t.Fatal(err)
	}
	if st := b.session.Status(); st.Conn != domain.StateConnected {
		t.Fatalf("tras crear: %s", st.Conn)
	}

	// Y la copia es una copia: tocar los peers de un Status no puede alcanzar
	// al siguiente.
	primero := b.session.Status()
	if len(primero.Peers) > 0 {
		primero.Peers[0].Name = domain.Nickname{}
	}
	if segundo := b.session.Status(); len(segundo.Peers) > 0 && segundo.Peers[0].Name.IsZero() {
		t.Fatal("quien recibe un Status puede mutar el estado del daemon")
	}
}

// TestElCódigoLlevaElSeedDelRegistroQueLoEmitió: un invite ID solo significa
// algo en el registro que lo emitió.
func TestElCódigoLlevaElSeedDelRegistroQueLoEmitió(t *testing.T) {
	b := nuevoBanco(t)
	b.registry.seed = "seed.humberto.dev"

	st, err := b.session.CreateRoom(ctx(), nick(t, "alvaro"), "Los panas", false)
	if err != nil {
		t.Fatal(err)
	}
	if st.Room.Seed != "seed.humberto.dev" {
		t.Fatalf("el código apunta a %q y lo emitió otro registro", st.Room.Seed)
	}
	if !strings.Contains(b.session.InviteLink(), "seed.humberto.dev") {
		t.Fatalf("el enlace manda al servidor equivocado: %q", b.session.InviteLink())
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
		{"la sala en el espacio de los vestíbulos", func(c *domain.Credential) {
			c.Subnet = netip.MustParsePrefix("198.19.7.0/24")
			c.VirtualIP = netip.MustParseAddr("198.19.7.7")
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

			if _, err := b.session.JoinRoom(ctx(), "A7K2M9QX@seed.midominio.com", nick(t, "humberto"), false); err == nil {
				t.Fatal("se aceptó")
			}
			if st := b.session.Status(); st.Conn != domain.StateIdle {
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
		cred, err := b.session.IssueCredentialFor(ctx(), domain.CredentialRequest{Name: nick(t, "humberto")})
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range vistas {
			if v == cred.VirtualIP {
				t.Fatalf("se repartió %s dos veces", cred.VirtualIP)
			}
		}
		vistas = append(vistas, cred.VirtualIP)
		// Nadie entró todavía: el motor no reporta miembros, y su lista de
		// credenciales no lleva direcciones. Así que lo único que puede evitar
		// que la segunda vuelta reparta otra vez la misma dirección es el
		// registro que la sesión escribió al emitir la primera.
	}
	if vistas[0] != netip.MustParseAddr(b.session.Status().Subnet.Addr().Next().Next().String()) {
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
	cred, err := b.session.IssueCredentialFor(ctx(), domain.CredentialRequest{Name: nick(t, "humberto")})
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
	cred, err := b.session.IssueCredentialFor(ctx(), domain.CredentialRequest{Name: nick(t, "humberto")})
	if err != nil {
		t.Fatal(err)
	}
	st := b.session.Status()
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
	if _, err := b.session.JoinRoom(ctx(), "A7K2M9QX@seed.midominio.com", nick(t, "humberto"), false); err != nil {
		t.Fatal(err)
	}
	if _, err := b.session.IssueCredentialFor(ctx(), domain.CredentialRequest{Name: nick(t, "otro")}); !errors.Is(err, ErrNotHost) {
		t.Fatalf("un invitado emitió una credencial: %v", err)
	}
}

// TestNoSeEmiteCredencialSinNombre. Llega de otra máquina, así que se exige
// acá: sin nombres, expulsar a alguien es adivinar.
func TestNoSeEmiteCredencialSinNombre(t *testing.T) {
	b := salaCreada(t)
	if _, err := b.session.IssueCredentialFor(ctx(), domain.CredentialRequest{}); !errors.Is(err, domain.ErrNicknameEmpty) {
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
	self := b.session.Status().LocalIP
	invitado := self.Next()

	b.motor.peers = []domain.Peer{{VirtualIP: self}, {VirtualIP: invitado}}
	emiteCredencial(t, b, "humberto", "c", invitado)
	if _, err := b.session.ActivateProfile(ctx(), "project-zomboid"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.session.OnPeersChanged(ctx()); err != nil {
		t.Fatal(err)
	}
	if len(b.firewall.estado().GameRules()) != 1 {
		t.Fatal("no se abrió el puerto, el test no probaría nada")
	}

	if _, err := b.session.KickMember(ctx(), invitado); err != nil {
		t.Fatal(err)
	}
	// El motor todavía lo reporta: no se enteró.
	st, err := b.session.OnPeersChanged(ctx())
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range st.Peers {
		if p.VirtualIP == invitado {
			t.Fatal("el sondeo devolvió al expulsado a la lista")
		}
	}
	for _, r := range b.firewall.estado().Rules {
		for _, ip := range r.Remote {
			if ip == invitado {
				t.Fatalf("el sondeo le reabrió %q al expulsado: %+v", r.Name, r)
			}
		}
	}

	// Pasada la ventana, si está, está: volver con un código que el host no
	// renovó es legítimo y tiene que funcionar.
	b.clock.avanza(KickGrace + time.Second)
	st, err = b.session.OnPeersChanged(ctx())
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
	self := b.session.Status().LocalIP
	invitado := self.Next()

	b.motor.peers = []domain.Peer{{VirtualIP: self}, {VirtualIP: invitado}}
	emiteCredencial(t, b, "humberto", "c", invitado)
	if _, err := b.session.OnPeersChanged(ctx()); err != nil {
		t.Fatal(err)
	}
	b.firewall.errApply = errors.New("COM dijo que no")

	if _, err := b.session.KickMember(ctx(), invitado); err == nil {
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

func salaConInvitado(t *testing.T) *bank {
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
	if _, err := b.session.JoinRoom(ctx(), "A7K2M9QX@seed.midominio.com", nick(t, "humberto"), false); err != nil {
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
	if _, err := host.session.ActivateProfile(ctx(), "juego-de-malla"); err != nil {
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
	st, err := invitado.session.OnRoomAnnounce(ctx(), ann)
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
	st, err := b.session.OnRoomAnnounce(ctx(), domain.RoomAnnounce{
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
	if b.session.MissingGame() != "un-juego-que-no-tengo" {
		t.Errorf("no se puede decir qué perfil falta: %q", b.session.MissingGame())
	}
}

// TestElHostNoAceptaAnuncios: aceptarlos le permitiría a un miembro modificado
// cambiarle el juego activo justo a la máquina donde se abren los puertos.
func TestElHostNoAceptaAnuncios(t *testing.T) {
	b := salaCreada(t)
	st, err := b.session.OnRoomAnnounce(ctx(), domain.RoomAnnounce{
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
	st, err := b.session.OnRoomAnnounce(ctx(), domain.RoomAnnounce{
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
	st, err := b.session.RenameRoom(ctx(), strings.Repeat("é", 100))
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
	if _, err := b.session.ActivateProfile(ctx(), "project-zomboid"); err != nil {
		t.Fatal(err)
	}
	antes := len(b.control.anuncios)

	b.motor.peers = []domain.Peer{
		{VirtualIP: b.session.Status().LocalIP},
		{VirtualIP: b.session.Status().LocalIP.Next()},
	}
	if _, err := b.session.OnPeersChanged(ctx()); err != nil {
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
	b.audit.perfiles = []domain.FirewallProfileState{
		{Profile: domain.ProfileDomain, Enabled: true},
		{Profile: domain.ProfilePublic, Enabled: false},
	}
	b.audit.tamper()
	b.audit.mapeos = []domain.PortMapping{
		{ExternalPort: 25565, InternalIP: netip.MustParseAddr("192.168.1.7")},
	}

	st := b.session.RefreshAlerts(ctx())
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
	if st2 := b.session.Status(); len(st2.Alerts) != len(st.Alerts) {
		t.Error("las alertas no viajan dentro de Status")
	}
}

// TestUnaAuditoríaQueFallaNoRompeNada: cada método responde una pregunta que
// Kanpachi no controla, y que la consulta falle no puede impedir jugar.
func TestUnaAuditoríaQueFallaNoRompeNada(t *testing.T) {
	b := nuevoBanco(t)
	b.audit.err = errors.New("COM no responde")

	st := b.session.RefreshAlerts(ctx())
	for _, a := range st.Alerts {
		if a.Kind != domain.AlertAuditFailed {
			t.Errorf("una auditoría rota inventó un hallazgo que nadie midió: %+v", a)
		}
	}
	if _, err := b.session.CreateRoom(ctx(), nick(t, "alvaro"), "Los panas", false); err != nil {
		t.Fatalf("una auditoría rota impidió crear la sala: %v", err)
	}
}

// TestUnaAuditoríaQueFallaLoDice cubre el agujero exacto que tenía el módulo:
// con las consultas fallando producía CERO alertas y la pantalla quedaba en
// verde. El módulo que existe para avisar que la promesa se rompió no podía
// avisar que él mismo había dejado de mirar.
func TestUnaAuditoríaQueFallaLoDice(t *testing.T) {
	b := nuevoBanco(t)
	b.audit.err = errors.New("COM no responde")

	st := b.session.RefreshAlerts(ctx())
	if !tieneAlerta(st, domain.AlertAuditFailed) {
		t.Fatalf("la auditoría entera falló y nadie se enteró: %+v", st.Alerts)
	}
}

// TestLaAuditoríaCaídaSeVaSolaCuandoVuelve: no es pegajosa.
//
// Pegajosa se quedaría encendida para siempre tras el primer fallo de COM,
// porque solo DropAlerts la quita y nadie tiene motivo para llamarla. Una alerta
// eterna deja de ser información.
func TestLaAuditoríaCaídaSeVaSolaCuandoVuelve(t *testing.T) {
	b := nuevoBanco(t)
	b.audit.err = errors.New("COM no responde")
	if st := b.session.RefreshAlerts(ctx()); !tieneAlerta(st, domain.AlertAuditFailed) {
		t.Fatalf("no avisó del fallo: %+v", st.Alerts)
	}

	b.audit.err = nil
	b.audit.intactas = true
	if st := b.session.RefreshAlerts(ctx()); tieneAlerta(st, domain.AlertAuditFailed) {
		t.Errorf("la auditoría volvió a contestar y el aviso se quedó puesto: %+v", st.Alerts)
	}
}

// TestElRouterQueNoContestaNoEsUnaAuditoríaCaída.
//
// La consulta al IGD falla en la mayoría de los routers del mundo, así que
// contarla como auditoría caída dejaría la alerta encendida en casi todas las
// máquinas, y una alerta que está siempre no significa nada.
func TestElRouterQueNoContestaNoEsUnaAuditoríaCaída(t *testing.T) {
	b := nuevoBanco(t)
	b.audit.intactas = true
	b.audit.errMapeos = errors.New("el router no contesta al IGD")

	st := b.session.RefreshAlerts(ctx())
	if tieneAlerta(st, domain.AlertAuditFailed) {
		t.Errorf("un router mudo levantó el aviso de auditoría caída: %+v", st.Alerts)
	}
}

// TestCadaComprobaciónLocalQueFallaLevantaElAviso, una por una: que el aviso
// salga con las dos caídas no prueba que salga con una sola.
func TestCadaComprobaciónLocalQueFallaLevantaElAviso(t *testing.T) {
	casos := []struct {
		qué   string
		rompe func(*mockAudit)
	}{
		{"el estado del firewall", func(a *mockAudit) {
			a.errPerfiles = errors.New("INetFwPolicy2 no responde")
		}},
		{"las reglas propias", func(a *mockAudit) {
			a.errIntactas = errors.New("no se pudo enumerar el grupo")
		}},
	}
	for _, c := range casos {
		t.Run(c.qué, func(t *testing.T) {
			b := nuevoBanco(t)
			b.audit.intactas = true
			c.rompe(b.audit)

			st := b.session.RefreshAlerts(ctx())
			if !tieneAlerta(st, domain.AlertAuditFailed) {
				t.Fatalf("no se pudo comprobar %s y nadie lo dijo: %+v", c.qué, st.Alerts)
			}
		})
	}
}

// TestDiagnoseNoPisaLoQueElMotorNoSabe: el MTU lo sondea netcfg y la subred la
// eligió el plan de direcciones. Pisarlos con ceros dejaría el diagnóstico peor
// que antes de pedirlo.
func TestDiagnoseNoPisaLoQueElMotorNoSabe(t *testing.T) {
	b := salaCreada(t)
	antes := b.session.Status().Net

	check, err := b.session.Diagnose(ctx())
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
	_, err := b.session.SaveProfile(ctx(), domain.GameProfile{
		ID:        "valheim",
		Name:      "Valheim",
		HostPorts: []domain.PortRange{{Proto: domain.ProtoUDP, From: 2456, To: 2458}},
		Connect:   domain.ConnectHint{Kind: domain.ConnectDirectIP},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	err = b.session.MarkVerified(ctx(), "valheim", domain.Verified{By: "yo", Method: "porque sí"})
	if !errors.Is(err, ErrNotPlayed) {
		t.Fatalf("se marcó verificado sin partida: %v", err)
	}
}

// TestSoloSeVerificaTrasUnaSalaConMásGente, que son las dos condiciones que
// admite el documento.
func TestSoloSeVerificaTrasUnaSalaConMásGente(t *testing.T) {
	b := salaCreada(t)
	if _, err := b.session.SaveProfile(ctx(), domain.GameProfile{
		ID:        "valheim",
		Name:      "Valheim",
		HostPorts: []domain.PortRange{{Proto: domain.ProtoUDP, From: 2456, To: 2458}},
		Connect:   domain.ConnectHint{Kind: domain.ConnectDirectIP},
	}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := b.session.ActivateProfile(ctx(), "valheim"); err != nil {
		t.Fatal(err)
	}

	// Solo yo en la sala: no habilita nada.
	b.session.LeaveRoom(ctx())
	if err := b.session.MarkVerified(ctx(), "valheim", domain.Verified{By: "alvaro"}); !errors.Is(err, ErrNotPlayed) {
		t.Fatalf("una sala de una persona habilitó la marca: %v", err)
	}

	// Ahora con alguien más.
	b2 := salaCreada(t)
	if _, err := b2.session.SaveProfile(ctx(), domain.GameProfile{
		ID:        "valheim",
		Name:      "Valheim",
		HostPorts: []domain.PortRange{{Proto: domain.ProtoUDP, From: 2456, To: 2458}},
		Connect:   domain.ConnectHint{Kind: domain.ConnectDirectIP},
	}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := b2.session.ActivateProfile(ctx(), "valheim"); err != nil {
		t.Fatal(err)
	}
	self := b2.session.Status().LocalIP
	b2.motor.peers = []domain.Peer{{VirtualIP: self}, {VirtualIP: self.Next()}}
	if _, err := b2.session.OnPeersChanged(ctx()); err != nil {
		t.Fatal(err)
	}
	b2.session.LeaveRoom(ctx())

	if err := b2.session.MarkVerified(ctx(), "valheim", domain.Verified{By: "alvaro", Method: "partida real"}); err != nil {
		t.Fatalf("no se pudo verificar tras una partida real: %v", err)
	}
	// Y una sola vez: el testigo se consume.
	if err := b2.session.MarkVerified(ctx(), "valheim", domain.Verified{By: "alvaro"}); !errors.Is(err, ErrNotPlayed) {
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
	if _, err := b.session.SaveProfile(ctx(), choca, false); !errors.Is(err, ErrShadowsBuiltin) {
		t.Fatalf("se tapó un builtin sin preguntar: %v", err)
	}
	if b.catalog.escrituras != 0 {
		t.Fatal("se escribió igual")
	}
	// Con el sí explícito del usuario, entra.
	if _, err := b.session.SaveProfile(ctx(), choca, true); err != nil {
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
	if _, err := s.JoinRoom(ctx(), "A7K2M9QX@seed.midominio.com", nick(t, "humberto"), false); err == nil {
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
	b.catalog.builtin = []byte(`{"kanpachi_catalog":1,"profiles":[
	  {"id":"viejo","schema":2,"name":"Juego viejo",
	   "host_ports":[{"proto":"udp","range":"6073"}],"client_ports":[],
	   "system_tweaks":{"broadcast_route":true,"multicast_route":false,"prefer_ipv4":false,"directplay":true},
	   "connect_hint":{"kind":"lan_browser","text_es":"aparece solo"}}]}`)
	s, err := NewSession(ctx(), b.deps)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateRoom(ctx(), nick(t, "alvaro"), "Los panas", false); err != nil {
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

// TestElAvisoDeExpulsiónSaleANTESDeCortarle.
//
// Es el único orden en que sirve: revocar le cierra la sesión en alrededor de
// un segundo, y a partir de ahí no hay por dónde mandarle un mensaje. Del otro
// lado eso es la diferencia entre "el host te sacó" y una partida que se cae
// sola sin explicación.
func TestElAvisoDeExpulsiónSaleANTESDeCortarle(t *testing.T) {
	b := salaCreada(t)
	self := b.session.Status().LocalIP
	invitado := self.Next()

	b.motor.peers = []domain.Peer{{VirtualIP: self}, {VirtualIP: invitado}}
	emiteCredencial(t, b, "humberto", "c", invitado)
	if _, err := b.session.OnPeersChanged(ctx()); err != nil {
		t.Fatal(err)
	}
	pasosAntes := len(b.motor.pasos())

	if _, err := b.session.KickMember(ctx(), invitado); err != nil {
		t.Fatal(err)
	}

	aviso, ok := b.control.últimoAviso()
	if !ok {
		t.Fatal("no se le avisó al expulsado")
	}
	if aviso.a != invitado {
		t.Errorf("el aviso fue a %s", aviso.a)
	}
	if aviso.n.Kind != domain.NoticeKicked {
		t.Errorf("aviso = %v", aviso.n.Kind)
	}
	// El motor no se tocó entre que se armó el aviso y que se mandó: revocar
	// tiene que ser posterior.
	pasos := b.motor.pasos()
	var iRevocar = -1
	for i, p := range pasos {
		if p == "revocar" {
			iRevocar = i
		}
	}
	if iRevocar < pasosAntes {
		t.Fatalf("se revocó antes de avisar: %v", pasos)
	}
}

// TestSiElAvisoFallaLaExpulsiónSigue: el aviso es cortesía, no el mecanismo.
func TestSiElAvisoFallaLaExpulsiónSigue(t *testing.T) {
	b := salaCreada(t)
	self := b.session.Status().LocalIP
	invitado := self.Next()

	b.motor.peers = []domain.Peer{{VirtualIP: self}, {VirtualIP: invitado}}
	emiteCredencial(t, b, "humberto", "c", invitado)
	if _, err := b.session.OnPeersChanged(ctx()); err != nil {
		t.Fatal(err)
	}
	b.control.errNotify = errors.New("el socket ya estaba muerto")

	if _, err := b.session.KickMember(ctx(), invitado); err != nil {
		t.Fatalf("un aviso que no salió detuvo la expulsión: %v", err)
	}
	if len(b.motor.revocadas) != 1 {
		t.Fatal("no se revocó la credencial")
	}
}

// TestElExpulsadoSaleLimpioYSabePorQué.
func TestElExpulsadoSaleLimpioYSabePorQué(t *testing.T) {
	b := salaConInvitado(t)

	st := b.session.OnRoomNotice(ctx(), domain.RoomNotice{
		Kind: domain.NoticeKicked, Reason: "el host te sacó de la sala",
	})
	if st.Conn != domain.StateIdle {
		t.Fatalf("estado = %s", st.Conn)
	}
	if st.LastExit != domain.ExitKicked {
		t.Fatalf("la pantalla de inicio no puede decir qué pasó: %v", st.LastExit)
	}
	// Salida limpia y no una caída: se revierten los ajustes y se cierra el
	// motor en vez de que se caiga solo.
	if b.netcfg.revertió == 0 {
		t.Error("no se revirtieron los ajustes del adaptador")
	}
	if !b.firewall.estado().IsEmpty() {
		t.Errorf("quedaron reglas: %+v", b.firewall.estado().Rules)
	}
}

// TestElHostAvisaQueCierraLaSala, y le ahorra a cada invitado los veinte
// minutos del contador mirando una sala que ya no existe.
func TestElHostAvisaQueCierraLaSala(t *testing.T) {
	b := salaCreada(t)
	b.session.LeaveRoom(ctx())

	aviso, ok := b.control.últimoAviso()
	if !ok {
		t.Fatal("el host se fue sin avisar")
	}
	if aviso.n.Kind != domain.NoticeRoomClosed {
		t.Errorf("aviso = %v", aviso.n.Kind)
	}
	if aviso.a.IsValid() {
		t.Errorf("el cierre es para todos, no para %s", aviso.a)
	}
}

// TestUnInvitadoQueSaleNoAvisaDeNada: cerrarle la sala a los demás no es suyo.
func TestUnInvitadoQueSaleNoAvisaDeNada(t *testing.T) {
	b := salaConInvitado(t)
	b.session.LeaveRoom(ctx())

	if _, ok := b.control.últimoAviso(); ok {
		t.Fatal("un invitado anunció el cierre de la sala")
	}
}

// TestAlHostNadieLoEchaDeSuPropiaSala.
func TestAlHostNadieLoEchaDeSuPropiaSala(t *testing.T) {
	b := salaCreada(t)
	for _, k := range []domain.NoticeKind{domain.NoticeKicked, domain.NoticeRoomClosed} {
		st := b.session.OnRoomNotice(ctx(), domain.RoomNotice{Kind: k})
		if st.Conn != domain.StateConnected {
			t.Fatalf("un aviso %v echó al host de su sala", k)
		}
	}
}

// TestElMotivoDeSalidaDistingueLosCuatroCaminos. Sin esto la pantalla de inicio
// no puede decir nada mejor que "no estás en ninguna sala".
func TestElMotivoDeSalidaDistingueLosCuatroCaminos(t *testing.T) {
	t.Run("salir por tu cuenta", func(t *testing.T) {
		b := salaCreada(t)
		if st := b.session.LeaveRoom(ctx()); st.LastExit != domain.ExitUser {
			t.Fatalf("%v", st.LastExit)
		}
	})
	t.Run("el host desaparece veinte minutos", func(t *testing.T) {
		b := salaConInvitado(t)
		b.session.SetHostPresent(false)
		b.clock.avanza(domain.HostAbsenceLimit + time.Minute)
		if !b.session.TickHostAbsence(ctx()) {
			t.Fatal("no salió")
		}
		if st := b.session.Status(); st.LastExit != domain.ExitHostGone {
			t.Fatalf("%v", st.LastExit)
		}
	})
	t.Run("el host cierra la sala", func(t *testing.T) {
		b := salaConInvitado(t)
		st := b.session.OnRoomNotice(ctx(), domain.RoomNotice{Kind: domain.NoticeRoomClosed})
		if st.LastExit != domain.ExitRoomClosed {
			t.Fatalf("%v", st.LastExit)
		}
	})
	t.Run("no se llegó a entrar", func(t *testing.T) {
		b := nuevoBanco(t)
		b.control.errDial = errors.New("nadie contesta")
		if _, err := b.session.JoinRoom(ctx(), "A7K2M9QX@seed.midominio.com", nick(t, "humberto"), false); err == nil {
			t.Fatal("entró")
		}
		if st := b.session.Status(); st.LastExit != domain.ExitFailed {
			t.Fatalf("%v", st.LastExit)
		}
	})
}

// TestExpulsarNoEsBloquear: el expulsado vuelve con el mismo código mientras el
// host no lo renueve, y renovar no migra a nadie de sala.
func TestExpulsarNoEsBloquear(t *testing.T) {
	b := salaCreada(t)
	self := b.session.Status().LocalIP
	invitado := self.Next()

	b.motor.peers = []domain.Peer{{VirtualIP: self}, {VirtualIP: invitado}}
	emiteCredencial(t, b, "humberto", "c", invitado)
	if _, err := b.session.OnPeersChanged(ctx()); err != nil {
		t.Fatal(err)
	}
	códigoAntes := b.session.Status().Room.InviteID
	if _, err := b.session.KickMember(ctx(), invitado); err != nil {
		t.Fatal(err)
	}

	// La puerta del vestíbulo NO se recortó: sigue abierta a cualquiera con el
	// código, que es lo que hace que expulsar y bloquear sean cosas distintas.
	b.control.mu.Lock()
	puerta := b.control.scope.Lobby
	b.control.mu.Unlock()
	if puerta != lobbyDe(b) {
		t.Fatal("expulsar cerró la puerta de la sala")
	}
	if b.session.Status().Room.InviteID != códigoAntes {
		t.Fatal("expulsar cambió el código")
	}

	// Y renovar es la otra operación, que no toca la red real ni migra a nadie.
	b.registry.siguiente = "B4N9PQRS"
	antesDeRenovar := b.motor.hostSpec
	if _, err := b.session.RotateInviteCode(ctx()); err != nil {
		t.Fatal(err)
	}
	después := b.motor.hostSpec
	if antesDeRenovar.RealNetworkName() != después.RealNetworkName() {
		t.Fatal("renovar el código cambió la red real: habría que migrar a todos")
	}
	if antesDeRenovar.NetworkSecret != después.NetworkSecret {
		t.Fatal("renovar el código cambió el secreto de la red")
	}
}

// TestElHostAbreElCanalDeControlDesdeQueTieneSala.
//
// Es lo que hace que el canal de la decisión 23 pueda existir: el host escucha
// en un puerto de la interfaz virtual, y la interfaz nace con deny all. Sin
// esta regla el firewall del host bloquearía su propia puerta, y el síntoma
// sería que entrar a una sala ajena no funciona nunca.
func TestElHostAbreElCanalDeControlDesdeQueTieneSala(t *testing.T) {
	b := salaCreada(t)

	var puerta bool
	for _, r := range b.firewall.estado().Rules {
		if r.IsControl() && r.Local == lobbyDe(b) {
			puerta = true
			if r.From != domain.ControlPort {
				t.Errorf("la puerta no está en el puerto del canal: %+v", r)
			}
		}
	}
	if !puerta {
		t.Fatalf("la sala del host no abrió la puerta del vestíbulo: %+v", b.firewall.estado().Rules)
	}
}

// TestSalirDeLaSalaNoDejaElCanalAbierto: el hueco vive lo que vive la sala.
func TestSalirDeLaSalaNoDejaElCanalAbierto(t *testing.T) {
	b := salaCreada(t)
	b.session.LeaveRoom(ctx())

	for _, r := range b.firewall.estado().Rules {
		if r.IsControl() {
			t.Fatalf("salir dejó el canal abierto: %+v", r)
		}
	}
}

// Volver el túnel VUELVE A ACOTAR la compuerta, y esto no es una llamada de más.
//
// Si el motor murió y volvió, los adaptadores virtuales son NUEVOS, o sea LUID
// nuevo, o sea que la compuerta se habría quedado acotada a uno que ya no
// existe. Nada falla: los filtros se emiten contra el LUID viejo, la llamada
// devuelve éxito, y la pantalla dice que la sala está contenida mientras debajo
// hay una red virtual con los permisos puestos y sin bloqueo.
//
// Se afirma sobre el ORDEN, que es donde está el fallo: acotar después de abrir
// deja un instante con permisos y sin compuerta, con gente ya dentro.
func TestVolverElTúnelVuelveAAcotarLaCompuerta(t *testing.T) {
	b := salaCreada(t)
	if _, err := b.session.ActivateProfile(ctx(), "project-zomboid"); err != nil {
		t.Fatal(err)
	}

	// El motor se cae y vuelve. Soltar la compuerta es lo que deja el estado
	// como lo dejaría un adaptador nuevo: acotada a algo que ya no sirve.
	b.firewall.UnbindRoom()
	b.firewall.abrióSinCompuerta = false

	if _, err := b.session.OnEngineEvent(ctx(), domain.EngineEvent{Kind: domain.EngineConnected}); err != nil {
		t.Fatal(err)
	}

	if b.firewall.abrióSinCompuerta {
		t.Error("al volver el túnel se aplicaron reglas con la compuerta suelta")
	}
	red, vínculo := b.firewall.alcance()
	if red != b.session.Status().Subnet {
		t.Errorf("la compuerta quedó acotada a %v y la sala es %v", red, b.session.Status().Subnet)
	}
	if vínculo != domain.BindRoomAndLobby {
		t.Errorf("el host se reacotó a %v, y sigue teniendo sala y vestíbulo", vínculo)
	}
}

// Si no se puede acotar al VOLVER EL TÚNEL, se avisa y se sigue; si no se puede
// al TERMINAR EL REINICIO, es un error. Los dos comportamientos son a propósito.
//
// El evento de conexión llega en cuanto conecta la primera de las dos redes, así
// que durante un reinicio del watchdog llega legítimamente con el vestíbulo
// todavía sin levantar. Tratarlo como fatal ahí convertía una carrera de un
// segundo en una sala que no volvía nunca: medido, se quedaba en reconectando
// con las dos redes ya arriba.
//
// `OnEngineRestarted` corre cuando el motor terminó de levantar las dos, así que
// ahí no acotar ya no es una carrera: es una sala con gente dentro y sin
// compuerta, afirmando en pantalla lo único que el producto promete.
func TestElReacotadoEsOportunistaAlConectarYExigenteAlTerminarElReinicio(t *testing.T) {
	b := salaCreada(t)
	b.firewall.errBind = errors.New("no se encontró el adaptador")

	if _, err := b.session.OnEngineEvent(ctx(), domain.EngineEvent{Kind: domain.EngineConnected}); err != nil {
		t.Fatalf("una carrera de un segundo tumbó la vuelta del túnel: %v", err)
	}
	if err := b.session.OnEngineRestarted(ctx()); err == nil {
		t.Fatal("el motor volvió entero, no se pudo acotar, y se dio por bueno")
	}
}
