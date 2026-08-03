package usecase

import (
	"errors"
	"net/netip"
	"testing"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// Los tests de la CADENA de respaldos.
//
// La regla que comprueban todos, dicha una vez: ninguna capa depende de que la
// anterior haya funcionado. Cada test apaga una y exige que la de abajo siga
// haciendo su trabajo.

// TestExpulsarAplicaLasDosCapasAunqueFalleLaPrimera.
//
// La decisión 22 promete dos capas que fallan por motivos distintos. El código
// las tenía en serie: un error del motor devolvía antes de recortar el firewall,
// así que un bug del motor le dejaba la sesión abierta Y el puerto autorizado,
// que es exactamente lo contrario de lo prometido.
func TestExpulsarAplicaLasDosCapasAunqueFalleLaPrimera(t *testing.T) {
	b, invitado := salaConDosYJuego(t)
	b.motor.errRevocar = errors.New("el motor no contesta")

	st, err := b.sesión.KickMember(ctx(), invitado)
	if !errors.Is(err, ErrKickPartial) || !errors.Is(err, ErrRevokeFailed) {
		t.Fatalf("error = %v, se esperaba una expulsión a medias por fallo del motor", err)
	}
	if st.Conn != domain.StateConnected {
		t.Fatal("el estado devuelto vino vacío, y la UI necesita el bueno")
	}

	for _, p := range st.Peers {
		if p.VirtualIP == invitado {
			t.Fatal("el expulsado sigue en la lista de miembros")
		}
	}
	for _, r := range b.firewall.estado().Rules {
		for _, remoto := range r.Remote {
			if remoto == invitado {
				t.Fatalf("el firewall sigue autorizando a %s tras fallar la revocación", invitado)
			}
		}
	}
	for _, a := range b.control.alcanceActual() {
		if a == invitado {
			t.Fatal("el expulsado sigue pudiendo hablarle al código que corre como SYSTEM")
		}
	}
}

// TestUnaExpulsiónAMediasSeVeEnLasAlertasYSobreviveAlBarrido.
//
// Se avisa en vez de deshacer: deshacer volvería a autorizar a quien el host
// acaba de echar. El aviso tiene que sobrevivir al refresco del módulo de
// exposición, que recalcula el resto de las alertas enteras.
func TestUnaExpulsiónAMediasSeVeEnLasAlertasYSobreviveAlBarrido(t *testing.T) {
	b, invitado := salaConDosYJuego(t)
	b.motor.errRevocar = errors.New("el motor no contesta")

	if _, err := b.sesión.KickMember(ctx(), invitado); err == nil {
		t.Fatal("la expulsión a medias no devolvió error")
	}
	if !tieneAlerta(b.sesión.Status(), domain.AlertKickIncomplete) {
		t.Fatal("la expulsión a medias no dejó alerta")
	}

	st := b.sesión.RefreshAlerts(ctx())
	if !tieneAlerta(st, domain.AlertKickIncomplete) {
		t.Fatal("el barrido se llevó por delante la alerta de expulsión a medias")
	}
}

// TestElExpulsadoVuelveAEntrarConElMismoCódigo es la decisión 22 dicha como
// test.
//
// Un kick NO es un ban. Kanpachi no guarda identidad por peer y no va a
// guardarla, así que la puerta del vestíbulo sigue abierta para el expulsado
// hasta que el host renueve el código, que es la otra operación y la única que
// cierra de verdad.
func TestElExpulsadoVuelveAEntrarConElMismoCódigo(t *testing.T) {
	b, invitado := salaConDosYJuego(t)

	if _, err := b.sesión.KickMember(ctx(), invitado); err != nil {
		t.Fatal(err)
	}
	// La puerta del vestíbulo no se recortó: sigue siendo la dirección fija del
	// host, abierta a cualquiera que tenga el código.
	if b.control.scope.Lobby != domain.RendezvousHostAddress {
		t.Fatalf("expulsar tocó la puerta del vestíbulo: %v", b.control.scope.Lobby)
	}

	// Y el que vuelve recibe credencial. Se emite pasada la ventana de gracia,
	// que existe para que el sondeo del motor no lo devuelva a la lista, no
	// para bloquearlo.
	b.reloj.avanza(KickGrace + time.Minute)
	b.motor.credenciales = func() domain.Credential { return domain.Credential{ID: "c2", Token: "t2"} }

	cred, err := b.sesión.IssueCredentialFor(ctx(), domain.CredentialRequest{Name: nick(t, "humberto")})
	if err != nil {
		t.Fatalf("el expulsado no pudo volver a entrar: %v", err)
	}
	if cred.Token == "" {
		t.Fatal("la credencial del que vuelve salió sin token")
	}
}

// TestCadaEventoDelMotorLlegaASuTransición.
//
// Antes de esto, StateDegraded y StateReconnecting eran inalcanzables desde
// fuera de core: nadie consumía el canal del motor y la máquina de estados solo
// se movía desde crear, entrar y salir.
func TestCadaEventoDelMotorLlegaASuTransición(t *testing.T) {
	casos := []struct {
		kind   domain.EngineEventKind
		quiero domain.ConnState
	}{
		{domain.EngineDegraded, domain.StateDegraded},
		{domain.EngineDisconnected, domain.StateReconnecting},
		{domain.EngineDied, domain.StateReconnecting},
		{domain.EngineConnected, domain.StateConnected},
	}
	for _, c := range casos {
		t.Run(c.kind.String(), func(t *testing.T) {
			b := salaCreada(t)
			st, err := b.sesión.OnEngineEvent(ctx(), domain.EngineEvent{Kind: c.kind, Reason: "prueba"})
			if err != nil {
				t.Fatal(err)
			}
			if st.Conn != c.quiero {
				t.Fatalf("%v llevó a %s, se esperaba %s", c.kind, st.Conn, c.quiero)
			}
		})
	}
}

// TestDegradadoNoArrancaNingúnPlazo. Degradado es que el túnel sigue en pie y
// va peor, normalmente por relay, que es un caso soportado y no un fallo.
func TestDegradadoNoArrancaNingúnPlazo(t *testing.T) {
	b := salaCreada(t)
	if _, err := b.sesión.OnEngineEvent(ctx(), domain.EngineEvent{Kind: domain.EngineDegraded}); err != nil {
		t.Fatal(err)
	}
	b.reloj.avanza(domain.ReconnectLimit + time.Hour)
	if st := b.sesión.Tick(ctx()); st.Conn == domain.StateIdle {
		t.Fatal("estar degradado sacó de la sala, y el túnel seguía en pie")
	}
}

// TestSinTúnelHayUnPlazoYAlVencerSeCierraTodo.
//
// Es el respaldo del watchdog del supervisor, para el caso de que el watchdog
// tampoco esté. Sostener una sala sin red durante veinte minutos solo consigue
// que el usuario mire una pantalla que miente.
func TestSinTúnelHayUnPlazoYAlVencerSeCierraTodo(t *testing.T) {
	b := salaCreada(t)
	if _, err := b.sesión.ActivateProfile(ctx(), "project-zomboid"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.sesión.OnEngineEvent(ctx(), domain.EngineEvent{Kind: domain.EngineDied}); err != nil {
		t.Fatal(err)
	}

	b.reloj.avanza(domain.ReconnectLimit - time.Minute)
	if st := b.sesión.Tick(ctx()); st.Conn != domain.StateReconnecting {
		t.Fatalf("se rindió antes de tiempo: %s", st.Conn)
	}
	b.reloj.avanza(2 * time.Minute)

	st := b.sesión.Tick(ctx())
	if st.Conn != domain.StateIdle {
		t.Fatalf("no salió tras el plazo: %s", st.Conn)
	}
	if st.LastExit != domain.ExitTunnelLost {
		t.Fatalf("motivo de salida = %v, se esperaba que el túnel no volvió", st.LastExit)
	}
	if !b.firewall.estado().IsEmpty() {
		t.Fatalf("quedaron reglas abiertas tras rendirse: %+v", b.firewall.estado().Rules)
	}
}

// TestRendirseCierraLaSalaYPurga es la otra mitad, la que llama el supervisor
// cuando su watchdog agota los reintentos.
func TestRendirseCierraLaSalaYPurga(t *testing.T) {
	b := salaCreada(t)
	purgasAntes := b.firewall.purgas

	st := b.sesión.OnEngineGaveUp(ctx(), "el motor no volvió tras ocho intentos")
	if st.Conn != domain.StateIdle || st.LastExit != domain.ExitTunnelLost {
		t.Fatalf("estado tras rendirse: %s, %v", st.Conn, st.LastExit)
	}
	if b.firewall.purgas != purgasAntes+1 {
		t.Fatal("rendirse no purgó el grupo Kanpachi")
	}
}

// TestElHostCalladoCuentaDesdeLoÚltimoQueSeOyó.
//
// El caso que cubre no tiene flanco de socket: la máquina del host se apagó de
// golpe, sin FIN y sin RST, y el socket TCP medio abierto sobrevive horas. Nada
// llama a SetHostPresent, y aun así hay que salir.
//
// Y hay que salir a los VEINTE minutos, no a los veintiséis. La ausencia se
// fecha en la última prueba de vida y no en el instante en que este código se
// dio cuenta.
func TestElHostCalladoCuentaDesdeLoÚltimoQueSeOyó(t *testing.T) {
	b := salaConInvitado(t)
	inicio := b.reloj.Now()

	// Vence el silencio. Marca ausente y no saca a nadie.
	b.reloj.avanza(domain.HostSilenceLimit + time.Minute)
	st := b.sesión.Tick(ctx())
	if st.HostPresent {
		t.Fatal("el host sigue presente tras pasarse del límite de silencio")
	}
	if st.Conn != domain.StateConnected {
		t.Fatal("el silencio sacó de la sala, y eso lo hace el contador de veinte minutos")
	}

	// Un minuto antes de los veinte contados desde la última señal.
	b.reloj.ahora = inicio.Add(domain.HostAbsenceLimit - time.Minute)
	if st := b.sesión.Tick(ctx()); st.Conn == domain.StateIdle {
		t.Fatal("salió antes de los veinte minutos")
	}
	// Y justo a los veinte.
	b.reloj.ahora = inicio.Add(domain.HostAbsenceLimit)
	st = b.sesión.Tick(ctx())
	if st.Conn != domain.StateIdle {
		t.Fatalf("no salió a los veinte minutos: %s", st.Conn)
	}
	if st.LastExit != domain.ExitHostGone {
		t.Fatalf("motivo de salida = %v", st.LastExit)
	}
}

// TestUnAnuncioCuentaComoPruebaDeVida: es la señal que llega sola cuando la
// caída del socket es una señal que nunca va a llegar.
func TestUnAnuncioCuentaComoPruebaDeVida(t *testing.T) {
	b := salaConInvitado(t)

	b.reloj.avanza(domain.HostSilenceLimit - time.Minute)
	if _, err := b.sesión.OnRoomAnnounce(ctx(), domain.RoomAnnounce{RoomName: "Los panas"}); err != nil {
		t.Fatal(err)
	}
	b.reloj.avanza(2 * time.Minute)

	if st := b.sesión.Tick(ctx()); !st.HostPresent {
		t.Fatal("el anuncio no contó como prueba de vida y el host quedó por ausente")
	}
}

// TestElHostQueDesapareceDeLaTablaDePeersSeDaPorAusente.
//
// Es la capa que sigue funcionando con el canal de control roto, colgado o sin
// arrancar: el motor propaga la tabla entera, así que la .1 está o no está sin
// que el canal opine.
func TestElHostQueDesapareceDeLaTablaDePeersSeDaPorAusente(t *testing.T) {
	b := salaConInvitado(t)
	if !b.sesión.Status().HostPresent {
		t.Fatal("el invitado no arrancó con el host presente")
	}

	b.motor.peers = []domain.Peer{
		{VirtualIP: netip.MustParseAddr("100.87.3.5"), Name: nick(t, "humberto")},
	}
	st, err := b.sesión.OnPeersChanged(ctx())
	if err != nil {
		t.Fatal(err)
	}
	if st.HostPresent {
		t.Fatal("el host desapareció de la red y sigue marcado presente")
	}
}

// TestLaTablaDePeersNoEnciendeLaPresencia.
//
// La asimetría es el punto entero. Que el motor reporte al host prueba que su
// nodo está en la red, y NO prueba que su canal de control funcione. Encenderla
// desde ahí desarmaría el contador con evidencia que no lo respalda, y el caso
// real que rompe es el host que dejó la máquina encendida con Kanpachi colgado.
func TestLaTablaDePeersNoEnciendeLaPresencia(t *testing.T) {
	b := salaConInvitado(t)
	b.sesión.SetHostPresent(false)

	st, err := b.sesión.OnPeersChanged(ctx())
	if err != nil {
		t.Fatal(err)
	}
	if st.HostPresent {
		t.Fatal("la tabla de peers encendió la presencia del host")
	}
}

// TestLosVencimientosCorrenAunqueNadieLlameAlLatido.
//
// Es la prueba de que la capa no depende de su disparador. Acá el supervisor
// está muerto y nunca llama a Tick: lo único que entra es un cambio de miembros.
func TestLosVencimientosCorrenAunqueNadieLlameAlLatido(t *testing.T) {
	b := salaConInvitado(t)
	b.sesión.SetHostPresent(false)
	b.reloj.avanza(domain.HostAbsenceLimit + time.Minute)

	st, err := b.sesión.OnPeersChanged(ctx())
	if err != nil {
		t.Fatal(err)
	}
	if st.Conn != domain.StateIdle || st.LastExit != domain.ExitHostGone {
		t.Fatalf("sin latido no venció el contador: %s, %v", st.Conn, st.LastExit)
	}
}

// TestElBarridoDeExposiciónTambiénHaceVencerLosPlazos: otra puerta más, por lo
// mismo.
func TestElBarridoDeExposiciónTambiénHaceVencerLosPlazos(t *testing.T) {
	b := salaConInvitado(t)
	b.sesión.SetHostPresent(false)
	b.reloj.avanza(domain.HostAbsenceLimit + time.Minute)

	if st := b.sesión.RefreshAlerts(ctx()); st.Conn != domain.StateIdle {
		t.Fatalf("el barrido no hizo vencer el contador: %s", st.Conn)
	}
}

// TestUnHostNoSeEchaDeSuPropiaSalaPorNingunaCapa.
func TestUnHostNoSeEchaDeSuPropiaSalaPorNingunaCapa(t *testing.T) {
	b := salaCreada(t)
	b.sesión.SetHostPresent(false)
	b.reloj.avanza(domain.HostAbsenceLimit + domain.HostSilenceLimit + time.Hour)

	if st := b.sesión.Tick(ctx()); st.Conn != domain.StateConnected {
		t.Fatalf("el host se echó de su propia sala: %s", st.Conn)
	}
}

// TestElHostRepiteElAnuncioCadaDosMinutos.
//
// Sin la repetición, el silencio del otro lado no es medible y la capa de
// arriba no existe.
func TestElHostRepiteElAnuncioCadaDosMinutos(t *testing.T) {
	b := salaCreada(t)
	if _, err := b.sesión.ActivateProfile(ctx(), "project-zomboid"); err != nil {
		t.Fatal(err)
	}
	antes := len(b.control.anuncios)

	b.reloj.avanza(AnnounceInterval - time.Second)
	b.sesión.Tick(ctx())
	if len(b.control.anuncios) != antes {
		t.Fatal("anunció antes de tiempo")
	}

	b.reloj.avanza(2 * time.Second)
	b.sesión.Tick(ctx())
	if len(b.control.anuncios) != antes+1 {
		t.Fatalf("anuncios = %d, se esperaba uno más que %d", len(b.control.anuncios), antes)
	}
}

// TestUnInvitadoNoAnuncia. Anunciar es del host, y un invitado que anunciara
// podría cambiarle el juego activo a la máquina donde se abren los puertos.
func TestUnInvitadoNoAnuncia(t *testing.T) {
	b := salaConInvitado(t)
	antes := len(b.control.anuncios)
	b.reloj.avanza(AnnounceInterval * 3)
	b.sesión.Tick(ctx())

	if len(b.control.anuncios) != antes {
		t.Fatal("un invitado anunció")
	}
}

// TestReglasAlteradasSeReponenSolas.
//
// Un hallazgo que se denuncia y no se repara deja la promesa rota mientras el
// usuario lee el aviso. Reponer no es arreglarle la máquina al usuario: es
// volver a hacer cierta la propia declaración de Kanpachi.
func TestReglasAlteradasSeReponenSolas(t *testing.T) {
	b := salaCreada(t)
	antes := b.firewall.veces()
	b.auditoría.tamper()

	st := b.sesión.RefreshAlerts(ctx())
	if b.firewall.veces() != antes+1 {
		t.Fatalf("aplicaciones = %d, se esperaba una reposición", b.firewall.veces()-antes)
	}
	if tieneAlerta(st, domain.AlertRulesTampered) {
		t.Fatal("se avisó de reglas alteradas después de haberlas repuesto")
	}
}

// TestTrasTresReposicionesSeAvisaEnVezDeInsistir.
//
// Distingue el toque puntual de alguien mirando la consola del firewall de algo
// que las quita en bucle. Contra lo segundo, insistir es pelearse con un
// antivirus a golpe de COM.
func TestTrasTresReposicionesSeAvisaEnVezDeInsistir(t *testing.T) {
	b := salaCreada(t)
	b.auditoría.tamper()

	for i := 0; i < TamperRepairLimit; i++ {
		b.sesión.RefreshAlerts(ctx())
	}
	antes := b.firewall.veces()

	st := b.sesión.RefreshAlerts(ctx())
	if b.firewall.veces() != antes {
		t.Fatal("siguió reponiendo pasado el límite")
	}
	if !tieneAlerta(st, domain.AlertRulesTampered) {
		t.Fatal("dejó de reponer y no avisó, que es lo peor de los dos mundos")
	}
}

// TestSinSalaLasReglasAlteradasSeAvisanYNoSeReponen: sin sala el conjunto
// deseado es el vacío, así que reaplicar sería pedirle al firewall que borre lo
// que la purga del arranque ya borró.
func TestSinSalaLasReglasAlteradasSeAvisanYNoSeReponen(t *testing.T) {
	b := nuevoBanco(t)
	antes := b.firewall.veces()
	b.auditoría.tamper()

	st := b.sesión.RefreshAlerts(ctx())
	if b.firewall.veces() != antes {
		t.Fatal("repuso reglas sin sala")
	}
	if !tieneAlerta(st, domain.AlertRulesTampered) {
		t.Fatal("no avisó de las reglas alteradas")
	}
}

// salaConDosYJuego deja una sala de host con un invitado dentro, el juego
// activo y una credencial emitida para poder expulsar.
func salaConDosYJuego(t *testing.T) (*banco, netip.Addr) {
	t.Helper()
	b := salaCreada(t)
	self := b.sesión.Status().LocalIP
	invitado := self.Next()

	b.motor.peers = []domain.Peer{
		{VirtualIP: self, Name: nick(t, "alvaro")},
		{VirtualIP: invitado, Name: nick(t, "humberto"), Path: domain.PathDirect},
	}
	b.motor.credentials = []domain.Credential{{ID: "c1", Token: "t", VirtualIP: invitado}}
	if _, err := b.sesión.OnPeersChanged(ctx()); err != nil {
		t.Fatal(err)
	}
	if _, err := b.sesión.ActivateProfile(ctx(), "project-zomboid"); err != nil {
		t.Fatal(err)
	}
	return b, invitado
}

func tieneAlerta(st domain.RoomState, k domain.AlertKind) bool {
	for _, a := range st.Alerts {
		if a.Kind == k {
			return true
		}
	}
	return false
}
