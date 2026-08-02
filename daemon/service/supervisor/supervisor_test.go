package supervisor

import (
	"context"
	"errors"
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
	go func() { _ = b.sup.Run(ctx) }()
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
	go func() { hecho <- b.sup.Run(ctx) }()

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
