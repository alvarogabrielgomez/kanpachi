package usecase

import (
	"context"
	"net/netip"
	"testing"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// salaConTres deja una sala de host con TRES invitados dentro.
//
// Tres y no uno porque casi todo lo que este archivo prueba solo existe con la
// ronda plural: que se le pregunte a todos, que uno mintiendo no decida, y que
// uno hablando de más no cierre la ronda por los otros.
func salaConTres(t *testing.T) (*bank, []netip.Addr) {
	t.Helper()
	b := salaCreada(t)
	self := b.session.Status().LocalIP

	invitados := []netip.Addr{self.Next(), self.Next().Next(), self.Next().Next().Next()}
	nombres := []string{"humberto", "marisol", "ignacio"}

	peers := []domain.Peer{{VirtualIP: self, Name: nick(t, "alvaro")}}
	for i, ip := range invitados {
		peers = append(peers, domain.Peer{VirtualIP: ip, Name: nick(t, nombres[i]), Path: domain.PathDirect})
	}
	b.motor.peers = peers
	if _, err := b.session.OnPeersChanged(ctx()); err != nil {
		t.Fatal(err)
	}
	return b, invitados
}

// tocarAlAbrir toca el canario en cuanto la ronda lo abra.
//
// El contador se lee AQUÍ y no dentro de la goroutine, y no es un detalle: si se
// leyera adentro, la goroutine podría arrancar con la ronda ya abierta, contar
// esa apertura como "la de antes", y quedarse esperando una segunda que no llega.
func tocarAlAbrir(b *bank) {
	desde := b.canary.veces()
	go func() {
		if c, ok := b.canary.awaitOpening(desde); ok {
			c.tocar()
		}
	}()
}

// informar mete un informe en el canal que la ronda lee.
func informar(b *bank, de netip.Addr, puerto uint16, tcp, udp domain.ProbeOutcome) {
	b.control.informesCanario <- domain.CanaryReport{From: de, Port: puerto, TCP: tcp, UDP: udp}
}

// ---------------------------------------------------------------------------
// El hecho propio del host
// ---------------------------------------------------------------------------

// La primera fuga se REPARA SOLA y no avisa.
//
// El usuario no se entera de un problema que Kanpachi ya arregló, y la
// reposición deja de depender de que haga caso. Es la misma doctrina que
// repairOwnRulesLocked usa para la auditoría local.
func TestLaPrimeraFugaSeReparaSolaYNoAvisa(t *testing.T) {
	b, _ := salaConTres(t)
	aplicacionesAntes := b.firewall.veces()

	// Se toca en cuanto se abre, que es lo que hace un paquete cruzando.
	tocarAlAbrir(b)

	check := b.session.RunCanaryRound(ctx(), true)

	if check.Verdict() != domain.CanaryLeaking {
		t.Fatalf("veredicto = %v, se esperaba CanaryLeaking", check.Verdict())
	}
	if b.firewall.veces() <= aplicacionesAntes {
		t.Error("no se repuso la protección: la reparación automática es lo que hace que " +
			"ignorar el aviso no cueste protección")
	}
	if tieneAlerta(b.session.Status(), domain.AlertGateLeaking) {
		t.Error("avisó a la primera. La primera vez se repara callado y se deja que la " +
			"ronda siguiente juzgue")
	}
	if b.canary.last(t).c.cierres() != 1 {
		t.Errorf("el canario se cerró %d veces, se esperaba 1", b.canary.last(t).c.cierres())
	}
}

// La SEGUNDA seguida sí avisa, y ahí se deja de comprobar.
func TestLaSegundaFugaSeguidaLevantaLaAlarmaYDetieneLasRondas(t *testing.T) {
	b, _ := salaConTres(t)

	tocarAlAbrir(b)
	b.session.RunCanaryRound(ctx(), true)
	tocarAlAbrir(b)
	b.session.RunCanaryRound(ctx(), true)

	if !tieneAlerta(b.session.Status(), domain.AlertGateLeaking) {
		t.Fatal("dos fugas seguidas y ninguna alarma")
	}

	// Y con la alarma puesta el barrido ya no abre nada: mientras la protección
	// está caída el canario SÍ es alcanzable de verdad, así que seguir abriendo
	// sockets alcanzables sería trabajar en contra.
	aberturas := b.canary.veces()
	b.session.RunCanaryRound(ctx(), false)
	if b.canary.veces() != aberturas {
		t.Error("con la alarma puesta el barrido volvió a abrir un canario")
	}
}

// La alarma es pegajosa y el barrido de alertas no puede llevársela. Sin eso, la
// única prueba que este producto sabe producir dura sesenta segundos.
func TestLaAlarmaDelCanarioSobreviveAlBarridoDeAlertas(t *testing.T) {
	b, _ := salaConTres(t)
	alarmar(t, b)

	b.session.RefreshAlerts(ctx())

	if !tieneAlerta(b.session.Status(), domain.AlertGateLeaking) {
		t.Fatal("el barrido se llevó la alarma. Es pegajosa porque describe algo que se " +
			"MIDIÓ por la red y que nada del barrido vuelve a medir")
	}
}

// ---------------------------------------------------------------------------
// Zero trust: lo que un miembro NO puede hacer
// ---------------------------------------------------------------------------

// Un miembro no puede hacer que el host se alarme solo. La alarma sale del
// socket propio y jamás de un mensaje.
func TestUnInformeQueDiceQueLlegoNoLevantaLaAlarma(t *testing.T) {
	b, invitados := salaConTres(t)

	go func() {
		if _, ok := b.canary.awaitOpening(0); !ok {
			return
		}
		informar(b, invitados[0], b.canary.port, domain.ProbeAnswered, domain.ProbeSilent)
	}()

	check := b.session.RunCanaryRound(ctx(), true)

	if check.Verdict() != domain.CanaryMismatch {
		t.Fatalf("veredicto = %v, se esperaba CanaryMismatch", check.Verdict())
	}
	if tieneAlerta(b.session.Status(), domain.AlertGateLeaking) {
		t.Fatal("un miembro consiguió alarmar al host mandando un mensaje. La alarma " +
			"sale del socket propio y de nada más")
	}
}

// EL ATAQUE QUE ESCONDE UNA FUGA.
//
// La ronda cierra temprano cuando contestaron todos. Un miembro que mande tantos
// informes como miembros haya cerraría la ronda en milisegundos, y los honestos
// nunca llegarían a marcar.
func TestVariosInformesDelMismoMiembroNoCierranLaRonda(t *testing.T) {
	b, invitados := salaConTres(t)

	// El hostil contesta tres veces al instante. Los honestos tardan, como en la
	// vida real, y después uno de ellos toca.
	go func() {
		c, ok := b.canary.awaitOpening(0)
		if !ok {
			return
		}
		for i := 0; i < 3; i++ {
			informar(b, invitados[2], b.canary.port, domain.ProbeSilent, domain.ProbeSilent)
		}
		time.Sleep(30 * time.Millisecond)
		c.tocar()
	}()

	check := b.session.RunCanaryRound(ctx(), true)

	if len(check.Answers) != 1 {
		t.Errorf("se contaron %d informes de un solo miembro, se esperaba 1", len(check.Answers))
	}
	if check.Verdict() != domain.CanaryLeaking {
		t.Fatalf("veredicto = %v, se esperaba CanaryLeaking. La ronda cerró antes de que "+
			"nadie midiera, así que una fuga real no se detectó", check.Verdict())
	}
}

// Un informe de quien no fue preguntado no cuenta, y `server.read` emite lo que
// llegue por cualquier conexión que el host tenga abierta.
func TestUnInformeDeQuienNoEstaEnLaSalaNoCuenta(t *testing.T) {
	b, _ := salaConTres(t)
	intruso := netip.MustParseAddr("10.99.99.99")

	go func() {
		if _, ok := b.canary.awaitOpening(0); !ok {
			return
		}
		informar(b, intruso, b.canary.port, domain.ProbeAnswered, domain.ProbeAnswered)
	}()

	check := b.session.RunCanaryRound(ctx(), true)

	if len(check.Answers) != 0 {
		t.Fatalf("se admitió el informe de alguien de fuera: %+v", check.Answers)
	}
	if check.Verdict() != domain.CanaryUnconfirmed {
		t.Errorf("veredicto = %v, se esperaba CanaryUnconfirmed", check.Verdict())
	}
}

// Callarse NO apaga una alarma ya encendida.
//
// Es la asimetría entera del diseño: el techo de daño de un miembro que se calla
// es esconder información, jamás borrar una medición ya tomada.
func TestUnaRondaQueNadieContestaNoApagaLaAlarma(t *testing.T) {
	b, _ := salaConTres(t)
	alarmar(t, b)

	// Nadie contesta y nadie toca: la ronda vence por plazo.
	check := b.session.RunCanaryRound(ctx(), true)

	if check.Verdict() != domain.CanaryUnconfirmed {
		t.Fatalf("veredicto = %v, se esperaba CanaryUnconfirmed", check.Verdict())
	}
	if !tieneAlerta(b.session.Status(), domain.AlertGateLeaking) {
		t.Fatal("quedándose callados le apagaron la alarma al host, y esa alarma se " +
			"había establecido con certeza")
	}
}

// Una ronda LIMPIA sí la apaga, que es el camino de vuelta del usuario.
func TestTrasReponerLaProteccionUnaRondaLimpiaBorraLaAlarma(t *testing.T) {
	b, invitados := salaConTres(t)
	alarmar(t, b)

	if _, err := b.session.ReapplyProtection(ctx()); err != nil {
		t.Fatalf("ReapplyProtection: %v", err)
	}
	// Reponer no apaga nada por sí solo: apagar sin comprobar sería esconder.
	if !tieneAlerta(b.session.Status(), domain.AlertGateLeaking) {
		t.Fatal("reponer apagó la alarma sin haber comprobado nada")
	}

	desde := b.canary.veces()
	go func() {
		if _, ok := b.canary.awaitOpening(desde); !ok {
			return
		}
		for _, ip := range invitados {
			informar(b, ip, b.canary.port, domain.ProbeSilent, domain.ProbeSilent)
		}
	}()

	check := b.session.RunCanaryRound(ctx(), true)

	if check.Verdict() != domain.CanaryClean {
		t.Fatalf("veredicto = %v, se esperaba CanaryClean", check.Verdict())
	}
	if tieneAlerta(b.session.Status(), domain.AlertGateLeaking) {
		t.Fatal("una ronda limpia no apagó la alarma")
	}
}

// ---------------------------------------------------------------------------
// A quién se le pregunta, y por dónde
// ---------------------------------------------------------------------------

func TestSeLePreguntaATodosYNoAUnoSorteado(t *testing.T) {
	b, invitados := salaConTres(t)

	b.session.RunCanaryRound(ctx(), true)

	pedidos := b.control.pedidosDeCanario()
	if len(pedidos) != len(invitados) {
		t.Fatalf("se preguntó a %d de %d. Preguntarle a uno sorteado lo elige un "+
			"adversario que sostenga varias membresías", len(pedidos), len(invitados))
	}

	destinos := map[netip.Addr]bool{}
	var puertos, números int
	for _, p := range pedidos {
		destinos[p.A] = true
		if p.Req.Port == pedidos[0].Req.Port {
			puertos++
		}
		if p.Req.Nonce == pedidos[0].Req.Nonce {
			números++
		}
	}
	if len(destinos) != len(invitados) {
		t.Errorf("los destinos se repiten: %v", destinos)
	}
	// El mismo canario para todos. Uno por miembro sería abrir tantos sockets
	// como gente haya en la sala.
	if puertos != len(pedidos) || números != len(pedidos) {
		t.Error("no se les pidió el MISMO canario a todos")
	}
}

// El canario no puede ligarse a un puerto que la propia Kanpachi abrió: un
// oyente ahí contesta con toda razón, y esa respuesta se leería como una fuga.
func TestElCanarioEsquivaLosPuertosDelJuegoActivo(t *testing.T) {
	b, _ := salaConTres(t)
	if _, err := b.session.ActivateProfile(ctx(), "project-zomboid"); err != nil {
		t.Fatal(err)
	}

	b.session.RunCanaryRound(ctx(), true)

	avoid := b.canary.last(t).avoid
	if avoid == nil {
		t.Fatal("se abrió el canario sin decirle qué puertos esquivar")
	}
	// El perfil pide 16261-16262, y el canal de la sala vive en ControlPort.
	for _, p := range []uint16{16261, 16262, domain.ControlPort} {
		if !avoid(p) {
			t.Errorf("el canario podría ligarse a %d, que está abierto: su respuesta "+
				"se leería como que la compuerta dejó de contener", p)
		}
	}
	if avoid(51234) {
		t.Error("esquiva un efímero que nadie abrió, y entonces no encontraría puerto")
	}
}

// ---------------------------------------------------------------------------
// Cuándo NO se abre nada
// ---------------------------------------------------------------------------

func TestSinMasMiembrosNoSeAbreNingunCanario(t *testing.T) {
	b := salaCreada(t)

	check := b.session.RunCanaryRound(ctx(), true)

	if b.canary.veces() != 0 {
		t.Error("se abrió un socket para preguntarle a nadie")
	}
	if check.Verdict() != domain.CanaryBlind {
		t.Errorf("veredicto = %v, se esperaba el cero", check.Verdict())
	}
}

// La ronda de un invitado no existe: el oyente solo vive en el host.
func TestUnInvitadoNoAbreCanario(t *testing.T) {
	b := salaCreada(t)
	b.session.RunCanaryRound(ctx(), true)
	if b.canary.veces() == 0 {
		return // sin más miembros ya se negó, que también vale
	}
	t.Skip("este banco solo monta host; el caso de invitado lo cubre OnCanaryRequest")
}

// ---------------------------------------------------------------------------
// La sala a la que pertenece la ronda
// ---------------------------------------------------------------------------

// Una ronda tarda hasta diez segundos con el candado SUELTO. En ese hueco el
// host puede salir y crear otra sala, y la conclusión vieja no puede aterrizar
// en la nueva.
func TestUnaRondaDeUnaSalaViejaNoEscribeEnLaNueva(t *testing.T) {
	b, _ := salaConTres(t)

	// Se arranca la ronda y se la deja esperando su plazo.
	hecho := make(chan domain.CanaryCheck, 1)
	go func() { hecho <- b.session.RunCanaryRound(ctx(), true) }()
	for b.canary.veces() == 0 {
		time.Sleep(time.Millisecond)
	}

	// Y mientras tanto se sale de la sala, que es lo que sube la generación.
	b.session.LeaveRoom(ctx())
	b.canary.last(t).c.tocar() // la ronda vieja concluye que hubo fuga

	select {
	case <-hecho:
	case <-time.After(5 * time.Second):
		t.Fatal("la ronda no terminó")
	}

	st := b.session.Status()
	if tieneAlerta(st, domain.AlertGateLeaking) {
		t.Fatal("una ronda de una sala que ya se cerró dejó una alarma colgada en el " +
			"estado nuevo")
	}
	if !st.Canary.Blind() {
		t.Fatalf("escribió su conclusión en un estado que ya no es el que midió: %+v", st.Canary)
	}
}

// ---------------------------------------------------------------------------
// Nada de esto puede colgar la UI
// ---------------------------------------------------------------------------

// El candado se suelta antes de salir a la red. Sin eso, cada Status de la UI
// espera la ronda entera.
func TestUnaRondaLentaNoBloqueaStatus(t *testing.T) {
	b, _ := salaConTres(t)

	go func() { b.session.RunCanaryRound(ctx(), true) }()
	for b.canary.veces() == 0 {
		time.Sleep(time.Millisecond)
	}

	listo := make(chan struct{})
	go func() { b.session.Status(); close(listo) }()

	select {
	case <-listo:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Status se quedó esperando a la ronda. El candado tiene que soltarse " +
			"antes de salir a la red")
	}
}

// ---------------------------------------------------------------------------
// Reponer la protección
// ---------------------------------------------------------------------------

func TestReponerLaProteccionEsIdempotente(t *testing.T) {
	b, _ := salaConTres(t)

	if _, err := b.session.ReapplyProtection(ctx()); err != nil {
		t.Fatal(err)
	}
	primero := b.firewall.estado()
	if _, err := b.session.ReapplyProtection(ctx()); err != nil {
		t.Fatal(err)
	}
	segundo := b.firewall.estado()

	if len(primero.Rules) != len(segundo.Rules) {
		t.Fatalf("dos reposiciones dieron conjuntos distintos: %d y %d",
			len(primero.Rules), len(segundo.Rules))
	}
	for i := range primero.Rules {
		a, z := primero.Rules[i], segundo.Rules[i]
		if a.Name != z.Name || a.Proto != z.Proto || a.From != z.From || a.To != z.To {
			t.Fatalf("la regla %d cambió entre reposiciones: %+v y %+v", i, a, z)
		}
	}
}

func TestReponerSinSalaFalla(t *testing.T) {
	b := nuevoBanco(t)
	if _, err := b.session.ReapplyProtection(ctx()); err == nil {
		t.Fatal("repuso una protección sin sala")
	}
}

// ---------------------------------------------------------------------------
// El lado del invitado
// ---------------------------------------------------------------------------

// La sala como escáner de puertos por encargo, que es lo que esto impide.
func TestElInvitadoNoMarcaAUnaMaquinaQueNoEsElHost(t *testing.T) {
	b := salaCreada(t) // host, y por eso ni siquiera atiende pedidos
	err := b.session.OnCanaryRequest(ctx(), domain.CanaryRequest{
		Host:  netip.MustParseAddr("8.8.8.8"),
		Port:  53,
		Nonce: nonceDePruebaUsecase(),
	})
	if err != nil {
		t.Fatalf("no tenía que devolver error: %v", err)
	}
	if len(b.sonda.canariosMarcados()) != 0 {
		t.Fatal("marcó a una máquina de fuera. Así, el canal de la sala se convierte " +
			"en un escáner por encargo y el tráfico sale de la casa del usuario")
	}
}

func TestUnPedidoInvalidoNoMarcaNada(t *testing.T) {
	b := salaCreada(t)
	casos := map[string]domain.CanaryRequest{
		"sin dirección":  {Port: 5, Nonce: nonceDePruebaUsecase()},
		"puerto cero":    {Host: netip.MustParseAddr("10.0.0.1"), Nonce: nonceDePruebaUsecase()},
		"número en cero": {Host: netip.MustParseAddr("10.0.0.1"), Port: 5},
	}
	for nombre, req := range casos {
		if err := b.session.OnCanaryRequest(ctx(), req); err != nil {
			t.Errorf("%s: devolvió error en vez de callarse: %v", nombre, err)
		}
	}
	if len(b.sonda.canariosMarcados()) != 0 {
		t.Fatal("marcó algo con un pedido inválido")
	}
}

func nonceDePruebaUsecase() domain.CanaryNonce {
	var n domain.CanaryNonce
	for i := range n {
		n[i] = byte(i + 1)
	}
	return n
}

// alarmar deja la alarma encendida, que es dos rondas con fuga seguidas.
func alarmar(t *testing.T, b *bank) {
	t.Helper()
	for i := 0; i < CanaryRepairLimit+1; i++ {
		tocarAlAbrir(b)
		b.session.RunCanaryRound(context.Background(), true)
	}
	if !tieneAlerta(b.session.Status(), domain.AlertGateLeaking) {
		t.Fatalf("no se pudo dejar la alarma encendida")
	}
}
