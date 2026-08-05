package supervisor

import (
	"context"
	"errors"
	"net/netip"
	"runtime"
	"testing"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// corriendo arranca el bucle y devuelve el banco con su cancelación.
func corriendo(t *testing.T) (*banco, context.CancelFunc) {
	t.Helper()
	b := nuevoBanco()
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan struct{})
	go func() { _ = b.sup.Run(ctx, ready) }()
	<-ready
	t.Cleanup(cancel)
	return b, cancel
}

// TestCadaEventoDelMotorLlegaALaSala.
func TestCadaEventoDelMotorLlegaALaSala(t *testing.T) {
	kinds := []domain.EngineEventKind{
		domain.EngineConnected, domain.EnginePeersChanged,
		domain.EngineDegraded, domain.EngineDisconnected,
	}
	for _, k := range kinds {
		t.Run(k.String(), func(t *testing.T) {
			b, _ := corriendo(t)
			b.motor.eventos <- domain.EngineEvent{Kind: k}
			esperaA(t, "el evento llegó a la sala", func() bool {
				return b.sala.veces("motor:"+k.String()) == 1
			})
		})
	}
}

// TestElLatidoLlamaAlTickAunqueNoPaseNadaMás.
//
// Es la razón de existir del latido: convertir "no pasó nada" en una decisión.
// Sin él, el contador de veinte minutos nunca vence en una sala en la que no
// ocurre ningún evento.
func TestElLatidoLlamaAlTickAunqueNoPaseNadaMás(t *testing.T) {
	b, _ := corriendo(t)
	b.latidos <- time.Now()
	esperaA(t, "el latido llamó al tick", func() bool { return b.sala.veces("tick") >= 1 })
}

// TestElBarridoVaAparteDelLatido: uno para el tiempo, otro para el mundo.
func TestElBarridoVaAparteDelLatido(t *testing.T) {
	b, _ := corriendo(t)
	b.barridos <- time.Now()
	esperaA(t, "el barrido corrió", func() bool { return b.sala.veces("alertas") >= 1 })

	if b.sala.veces("tick") != 0 {
		t.Fatal("el barrido arrastró al latido, y late al ritmo del router más lento de la casa")
	}
}

// TestUnPánicoEnUnManejadorNoSeLlevaALosDemás.
//
// El coste de un manejador roto tiene que ser UN evento perdido. Si se llevara
// el bucle, se llevaría también el latido que hace vencer el contador de
// ausencia, y eso convierte un mensaje malformado en una sala eterna.
func TestUnPánicoEnUnManejadorNoSeLlevaALosDemás(t *testing.T) {
	b, _ := corriendo(t)
	b.sala.pánicoEn = "anuncio"

	b.control.anuncios <- domain.RoomAnnounce{RoomName: "Los panas"}
	esperaA(t, "el anuncio explotó", func() bool { return b.sala.veces("anuncio") >= 1 })

	b.latidos <- time.Now()
	esperaA(t, "el latido siguió corriendo tras el pánico", func() bool {
		return b.sala.veces("tick") >= 1
	})
}

// TestElDespachadorVuelveDeVariosPánicos.
func TestElDespachadorVuelveDeVariosPánicos(t *testing.T) {
	b, _ := corriendo(t)
	b.sala.pánicoEn = "aviso"
	b.sala.máximos = 2

	for i := 0; i < 3; i++ {
		b.control.avisos <- domain.RoomNotice{Kind: domain.NoticeKicked}
	}
	esperaA(t, "los tres avisos se atendieron", func() bool { return b.sala.veces("aviso") >= 3 })

	b.latidos <- time.Now()
	esperaA(t, "el bucle sigue vivo", func() bool { return b.sala.veces("tick") >= 1 })
}

// TestElCanalDelMotorCerradoSeTrataComoMuerte.
//
// Sin canal de eventos el motor está muerto, se haya dicho o no. Se sintetiza
// el evento y pasa por el manejador normal, así el watchdog toma el mando sin
// un camino especial que probar aparte.
func TestElCanalDelMotorCerradoSeTrataComoMuerte(t *testing.T) {
	b, _ := corriendo(t)
	close(b.motor.eventos)

	esperaA(t, "se trató como muerte del motor", func() bool {
		return b.sala.veces("motor:"+domain.EngineDied.String()) >= 1
	})
}

// TestElCanalDePresenciaCerradoDaAlHostPorAusente.
func TestElCanalDePresenciaCerradoDaAlHostPorAusente(t *testing.T) {
	b, _ := corriendo(t)
	close(b.control.presencia)

	esperaA(t, "el host quedó por ausente", func() bool { return b.sala.veces("ausente") >= 1 })
}

// TestElWatchdogReintentaYAlFinalSeRinde.
//
// La escalera entera, sin dormir un solo milisegundo: el temporizador está
// inyectado y el test lo dispara a mano.
func TestElWatchdogReintentaYAlFinalSeRinde(t *testing.T) {
	b, _ := corriendo(t)

	b.motor.eventos <- domain.EngineEvent{Kind: domain.EngineDied}
	esperaA(t, "se programó el primer reintento", func() bool { return b.pendientes() == 1 })

	for i := 0; i < MaxRestarts; i++ {
		b.disparaEsperas()
		esperaA(t, "el reintento llegó al motor", func() bool { return b.motor.veces() >= i+1 })
		if i == MaxRestarts-1 {
			break
		}
		// El motor arranca y se vuelve a morir.
		b.sup.work <- item{tag: tagEngine, value: domain.EngineEvent{Kind: domain.EngineDied}}
		esperaA(t, "se programó el siguiente reintento", func() bool { return b.pendientes() == 1 })
	}

	b.sup.work <- item{tag: tagEngine, value: domain.EngineEvent{Kind: domain.EngineDied}}
	esperaA(t, "el watchdog se rindió", func() bool { return b.sala.veces("rendirse") >= 1 })
}

// TestUnaConexiónBuenaReiniciaLaEscalera.
//
// Sin esto, un motor que se cae una vez por hora agotaría los ocho intentos en
// una tarde y cerraría una sala que estaba funcionando.
func TestUnaConexiónBuenaReiniciaLaEscalera(t *testing.T) {
	b, _ := corriendo(t)

	// Dos muertes seguidas suben un escalón.
	b.motor.eventos <- domain.EngineEvent{Kind: domain.EngineDied}
	esperaA(t, "primer intento", func() bool { return b.pendientes() == 1 })
	b.disparaEsperas()
	b.motor.eventos <- domain.EngineEvent{Kind: domain.EngineDied}
	esperaA(t, "segundo intento", func() bool { return b.pendientes() == 1 })
	if d := b.últimaDemora(); d != backoff[1] {
		t.Fatalf("segundo intento programado a %v, se esperaba %v", d, backoff[1])
	}
	b.disparaEsperas()

	// Una conexión buena devuelve la escalera al primer escalón.
	b.motor.eventos <- domain.EngineEvent{Kind: domain.EngineConnected}
	esperaA(t, "la conexión llegó", func() bool {
		return b.sala.veces("motor:"+domain.EngineConnected.String()) >= 1
	})
	b.motor.eventos <- domain.EngineEvent{Kind: domain.EngineDied}
	esperaA(t, "intento tras reconectar", func() bool { return b.pendientes() == 1 })

	if d := b.últimaDemora(); d != backoff[0] {
		t.Fatalf("tras reconectar se programó a %v, se esperaba el primer escalón %v", d, backoff[0])
	}
}

// TestUnReinicioFallidoProgramaElSiguiente: rendirse tiene que llegar por la
// escalera, no por un fallo suelto.
func TestUnReinicioFallidoProgramaElSiguiente(t *testing.T) {
	b, _ := corriendo(t)
	b.motor.err = errors.New("no arrancó")

	b.motor.eventos <- domain.EngineEvent{Kind: domain.EngineDied}
	esperaA(t, "se programó el primer reintento", func() bool { return b.pendientes() == 1 })
	b.disparaEsperas()

	esperaA(t, "el fallo programó otro reintento", func() bool { return b.pendientes() == 1 })
}

// TestElLatidoSeResuscribeAUnCanalNuevo.
//
// Es el requisito de la cadena aplicado al propio supervisor: nada depende de
// que la capa anterior haya funcionado, ni siquiera el cableado. Tras un
// Restart el canal de eventos es otro, y el anterior está cerrado.
func TestElLatidoSeResuscribeAUnCanalNuevo(t *testing.T) {
	b, _ := corriendo(t)
	// Se comprueba que el drenaje inicial está en pie antes de cambiar el
	// canal, para que el test no pase por accidente al drenar solo el segundo.
	b.motor.eventos <- domain.EngineEvent{Kind: domain.EngineConnected}
	esperaA(t, "el drenaje inicial arrancó", func() bool {
		return b.sala.veces("motor:"+domain.EngineConnected.String()) >= 1
	})

	nuevoCanal := b.motor.canalNuevo()
	// El canal viejo cerrado se lee como muerte del motor.
	esperaA(t, "se vio el cierre del canal viejo", func() bool {
		return b.sala.veces("motor:"+domain.EngineDied.String()) >= 1
	})

	b.latidos <- time.Now()
	esperaA(t, "el latido volvió a suscribirse", func() bool { return b.sala.veces("tick") >= 1 })

	nuevoCanal <- domain.EngineEvent{Kind: domain.EngineDegraded}
	esperaA(t, "el canal nuevo se está drenando", func() bool {
		return b.sala.veces("motor:"+domain.EngineDegraded.String()) >= 1
	})
}

// TestLosAjustesDelAdaptadorSeReaplicanSinEventoDeWindows.
//
// Es el respaldo de la suscripción al Event ID 10000, que puede morirse sin
// avisar. Sin él, una suscripción muerta se traduce en "ayer funcionaba".
func TestLosAjustesDelAdaptadorSeReaplicanSinEventoDeWindows(t *testing.T) {
	b, _ := corriendo(t)

	for i := 0; i < AdapterReapplyEvery; i++ {
		b.latidos <- time.Now()
	}
	esperaA(t, "se reaplicó el adaptador sin que Windows avisara", func() bool {
		return b.sala.veces("adaptador") >= 1
	})
}

// TestLaIdentificaciónDeRedReaplicaElAdaptador.
func TestLaIdentificaciónDeRedReaplicaElAdaptador(t *testing.T) {
	b, _ := corriendo(t)
	b.sistema.red <- struct{}{}

	esperaA(t, "se reaplicó el adaptador", func() bool { return b.sala.veces("adaptador") >= 1 })
}

// TestDespertarRevalidaYReconecta.
//
// Fast Startup y suspender dejan endpoints muertos. El latido va primero porque
// el reloj de pared saltó y puede haber contadores ya vencidos.
func TestDespertarRevalidaYReconecta(t *testing.T) {
	b, _ := corriendo(t)
	b.sala.ponEstado(domain.RoomState{
		Conn:              domain.StateReconnecting,
		ReconnectingSince: time.Now(),
	})

	b.sistema.despertó <- struct{}{}
	esperaA(t, "despertar revalidó", func() bool {
		return b.sala.veces("tick") >= 1 &&
			b.sala.veces("adaptador") >= 1 &&
			b.sala.veces("miembros") >= 1
	})
	if b.motor.veces() == 0 {
		t.Fatal("despertar sin túnel no empujó al motor")
	}
}

// TestDespertarConTúnelVivoNoToquetaElMotor.
func TestDespertarConTúnelVivoNoToquetaElMotor(t *testing.T) {
	b, _ := corriendo(t)
	b.sala.ponEstado(domain.RoomState{Conn: domain.StateConnected})

	b.sistema.despertó <- struct{}{}
	esperaA(t, "despertar revalidó", func() bool { return b.sala.veces("miembros") >= 1 })

	if b.motor.veces() != 0 {
		t.Fatal("se reinició el motor con el túnel en pie")
	}
}

// TestCancelarElContextoParaTodoSinFugarGoroutines.
func TestCancelarElContextoParaTodoSinFugarGoroutines(t *testing.T) {
	antes := runtime.NumGoroutine()

	b := nuevoBanco()
	ctx, cancel := context.WithCancel(context.Background())
	hecho := make(chan error, 1)
	ready := make(chan struct{})
	go func() { hecho <- b.sup.Run(ctx, ready) }()
	<-ready

	b.latidos <- time.Now()
	esperaA(t, "el bucle arrancó", func() bool { return b.sala.veces("tick") >= 1 })

	cancel()
	select {
	case err := <-hecho:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run devolvió %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run no volvió tras cancelar")
	}

	esperaA(t, "las goroutines se fueron", func() bool {
		runtime.Gosched()
		return runtime.NumGoroutine() <= antes+2
	})
}

// TestUnSupervisorAMediasNoArranca. Uno sin System no reaplicaría el adaptador,
// y el síntoma sería "ayer funcionaba" un mes después.
func TestUnSupervisorAMediasNoArranca(t *testing.T) {
	_, err := New(Deps{Room: &salaFalsa{}, Log: logMudo{}})
	if !errors.Is(err, ErrNotWired) {
		t.Fatalf("arrancó a medias: %v", err)
	}
}

// TestLasCadenciasSonConstantesYNoCaben EnCero deja escrito el orden entre
// plazos, que es lo que impide que la sala se cierre a mitad de un reintento
// que iba a funcionar.
func TestElWatchdogTerminaAntesDelPlazoDeCore(t *testing.T) {
	var total time.Duration
	for _, d := range backoff {
		total += d
	}
	if total >= domain.ReconnectLimit {
		t.Fatalf("la escalera del watchdog suma %v y el plazo de core es %v: core cerraría la sala a mitad de un reintento",
			total, domain.ReconnectLimit)
	}
	if Beat >= domain.HostSilenceLimit {
		t.Fatalf("el latido (%v) no muestrea el plazo más corto (%v)", Beat, domain.HostSilenceLimit)
	}
}

// ---------------------------------------------------------------------------
// La ronda del canario
// ---------------------------------------------------------------------------

// El barrido comprueba la protección, además de refrescar las alertas.
func TestElBarridoPideUnaRondaDeCanario(t *testing.T) {
	b, _ := corriendo(t)
	b.barridos <- time.Now()

	esperaA(t, "el barrido pidió la ronda", func() bool { return b.sala.veces("canario") >= 1 })

	rondas := b.sala.rondasCorridas()
	if rondas[0] {
		t.Error("el barrido pidió la ronda como si viniera de aplicar. Esa distinción " +
			"es la que deja que una ronda corra con la alarma puesta")
	}
}

// Aplicar las reglas programa la comprobación, que es el disparador que importa:
// alguien entró, o cambió el juego.
func TestAplicarLasReglasPideUnaRondaDeCanario(t *testing.T) {
	b, _ := corriendo(t)
	b.sala.CanaryDue() // lo crea si no estaba
	b.sala.canarioDue <- struct{}{}

	esperaA(t, "se pidió la ronda", func() bool { return b.sala.veces("canario") >= 1 })

	rondas := b.sala.rondasCorridas()
	if !rondas[0] {
		t.Fatal("la ronda tras aplicar tiene que saber que viene de aplicar: es la " +
			"única que corre con la alarma puesta, y sin eso reponer no se puede comprobar")
	}
}

// EL TEST QUE JUSTIFICA CORRER LA RONDA FUERA DEL DESPACHADOR.
//
// Una ronda dura hasta diez segundos. Corriéndola dentro del despachador, que
// es de un solo hilo, el latido de quince segundos que hace vencer el corte de
// los veinte minutos se quedaría esperando a la red.
func TestUnaRondaLentaNoParaElLatido(t *testing.T) {
	b, _ := corriendo(t)
	b.sala.bloquear = make(chan struct{})
	defer close(b.sala.bloquear)

	b.barridos <- time.Now()
	esperaA(t, "la ronda arrancó", func() bool { return b.sala.veces("canario") >= 1 })

	// Con la ronda colgada, el latido tiene que seguir corriendo.
	b.latidos <- time.Now()
	esperaA(t, "el latido corrió con una ronda en vuelo", func() bool {
		return b.sala.veces("tick") >= 1
	})
}

// Dos disparos no abren dos rondas. Cada una abre un socket y le pregunta a
// todos, así que solaparlas sería multiplicar el ruido sin medir más.
func TestDosDisparosNoAbrenDosRondasALaVez(t *testing.T) {
	b, _ := corriendo(t)
	b.sala.bloquear = make(chan struct{})

	b.barridos <- time.Now()
	esperaA(t, "la primera ronda arrancó", func() bool { return b.sala.veces("canario") >= 1 })

	b.sala.CanaryDue()
	b.sala.canarioDue <- struct{}{}
	b.barridos <- time.Now()

	// Se espera a que el despachador haya tenido tiempo de atender los dos
	// disparos, comprobando algo que SÍ corre: el barrido refresca alertas.
	esperaA(t, "los disparos siguientes se atendieron", func() bool {
		return b.sala.veces("alertas") >= 2
	})
	if n := b.sala.veces("canario"); n != 1 {
		t.Fatalf("se abrieron %d rondas a la vez, se esperaba 1", n)
	}

	// Y cuando la primera termina, se vuelve a poder.
	//
	// Se insiste con el barrido en vez de mandar uno solo, y eso dice algo real
	// del diseño: un disparo que llega con una ronda en vuelo se DESCARTA, no se
	// encola. La liberación de la bandera vuelve por el mismo canal de trabajo,
	// así que un barrido mandado justo antes se atendería con la bandera todavía
	// puesta. En producción el barrido siguiente llega solo a los sesenta
	// segundos.
	b.sala.mu.Lock()
	bloqueo := b.sala.bloquear
	b.sala.bloquear = nil
	b.sala.mu.Unlock()
	close(bloqueo)

	esperaA(t, "tras terminar la primera se puede otra", func() bool {
		select {
		case b.barridos <- time.Now():
		default:
		}
		return b.sala.veces("canario") >= 2
	})
}

// Un pánico dentro de la ronda corre en una goroutine que `atender` NO cubre, y
// se llevaría el proceso entero, que corre como SYSTEM. Además tiene que
// liberar la bandera, o la comprobación queda apagada en silencio para siempre.
func TestUnPánicoEnLaRondaNoSeLlevaElBucleNiDejaLaBanderaPuesta(t *testing.T) {
	b, _ := corriendo(t)
	b.sala.pánicoEn = "canario"
	b.sala.máximos = 1

	b.barridos <- time.Now()
	esperaA(t, "la ronda explotó", func() bool { return b.sala.veces("canario") >= 1 })

	// El bucle sigue.
	b.latidos <- time.Now()
	esperaA(t, "el latido siguió tras el pánico", func() bool { return b.sala.veces("tick") >= 1 })

	// Y se puede volver a comprobar: la bandera se liberó pese al pánico.
	b.barridos <- time.Now()
	esperaA(t, "se pudo abrir otra ronda tras el pánico", func() bool {
		return b.sala.veces("canario") >= 2
	})
}

// El lado del invitado: el pedido del host llega intacto.
func TestUnPedidoDeCanarioLlegaAlInvitado(t *testing.T) {
	b, _ := corriendo(t)
	req := domain.CanaryRequest{
		Host:  netip.MustParseAddr("10.77.0.1"),
		Port:  51234,
		Nonce: domain.CanaryNonce{1, 2, 3},
	}
	b.control.pedidos <- req

	esperaA(t, "el invitado atendió el pedido", func() bool {
		return b.sala.veces("canario:pedido") >= 1
	})

	recibidos := b.sala.pedidosDeCanario()
	if recibidos[0] != req {
		t.Fatalf("el pedido llegó cambiado: %+v", recibidos[0])
	}
}

// Atender un pedido también dura segundos, así que tampoco puede correr dentro
// del despachador.
func TestUnPedidoLentoNoParaElLatido(t *testing.T) {
	b, _ := corriendo(t)
	b.sala.bloquear = make(chan struct{})
	defer close(b.sala.bloquear)

	b.control.pedidos <- domain.CanaryRequest{Port: 1, Nonce: domain.CanaryNonce{1}}
	esperaA(t, "el pedido arrancó", func() bool { return b.sala.veces("canario:pedido") >= 1 })

	b.latidos <- time.Now()
	esperaA(t, "el latido corrió con un pedido en vuelo", func() bool {
		return b.sala.veces("tick") >= 1
	})
}

// Tras reiniciar el motor se REACOTA la contención, y no antes.
//
// # Por qué es un paso propio y no basta con el evento de conexión
//
// Porque los adaptadores virtuales son NUEVOS después de un reinicio, o sea LUID
// nuevo, y una compuerta que se quede apuntando al viejo no falla en ningún
// sitio: emite sus filtros, la llamada devuelve éxito, y la pantalla dice que la
// sala está contenida.
//
// El evento de conexión llega en cuanto conecta la PRIMERA de las dos redes, así
// que durante un reinicio llega con el vestíbulo todavía sin levantar. `Restart`
// espera a que las dos tengan dirección antes de volver, y esto corre justo
// después: es el único punto donde reacotar se puede exigir.
func TestTrasReiniciarElMotorSeReacotaLaContención(t *testing.T) {
	b, _ := corriendo(t)

	// La muerte del motor PROGRAMA el reintento; el reintento corre cuando la
	// espera vence, y este banco las dispara a mano para no dormir.
	b.motor.eventos <- domain.EngineEvent{Kind: domain.EngineDied}
	esperaA(t, "se programó el reintento", func() bool { return b.pendientes() == 1 })
	b.disparaEsperas()
	esperaA(t, "el motor se reinició", func() bool { return b.motor.veces() >= 1 })
	esperaA(t, "se reacotó la contención tras el reinicio", func() bool {
		return b.sala.veces("reacotar") >= 1
	})
}

// Y si el reinicio FALLA, no se reacota nada.
//
// Reacotar con el motor caído acotaría a adaptadores que no existen, y el
// error taparía en el log al que de verdad importa, que es que el motor no
// volvió.
func TestSiElReinicioFallaNoSeReacota(t *testing.T) {
	b, _ := corriendo(t)
	b.motor.err = errors.New("el motor no arranca")

	b.motor.eventos <- domain.EngineEvent{Kind: domain.EngineDied}
	esperaA(t, "se programó el reintento", func() bool { return b.pendientes() == 1 })
	b.disparaEsperas()
	esperaA(t, "se intentó reiniciar", func() bool { return b.motor.veces() >= 1 })
	esperaA(t, "el fallo programó otro reintento", func() bool { return b.pendientes() >= 1 })

	if n := b.sala.veces("reacotar"); n != 0 {
		t.Errorf("se reacotó %d vez/veces con el motor sin volver", n)
	}
}
