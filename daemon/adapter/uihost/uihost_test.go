package uihost

import (
	"testing"
	"time"

	"github.com/accentiostudios/kanpachi/core/timing"
)

// La cadena padre-hijo de Kanpachi, comprobada en el único punto que se puede
// comprobar sin arrancar procesos.
//
// El daemon mata a sus dos hijos por Job Object con `KILL_ON_JOB_CLOSE`, y eso
// lo garantiza el kernel: no hay código que probar ahí. Lo que SÍ es una
// decisión de este programa es cuándo la muerte de la interfaz se lleva por
// delante al daemon, y eso es [relanzador].

// TestUnaCaídaSueltaNoSeLlevaAlDaemon es el caso de todos los días: la interfaz
// se cierra, se relanza, y no pasa nada más.
func TestUnaCaídaSueltaNoSeLlevaAlDaemon(t *testing.T) {
	var r relanzador
	intento, rendirse := r.murió(time.Second)
	if intento != 1 {
		t.Fatalf("la primera caída tendría que ser el intento 1, y fue %d", intento)
	}
	if rendirse {
		t.Fatal("una sola caída rápida apagó el daemon, y no debería")
	}
}

// TestCuatroCaídasRápidasApaganElDaemon fija el tope, que NO es cosmético: al
// rendirse corre `OnGiveUp`, que en el daemon es `host.Shutdown()`. O sea que
// pasar de acá cierra la sala de todos los que estén jugando.
func TestCuatroCaídasRápidasApaganElDaemon(t *testing.T) {
	var r relanzador
	for i := 1; i <= maxRelaunches; i++ {
		if _, rendirse := r.murió(time.Second); rendirse {
			t.Fatalf("se rindió en la caída %d, y el tope son %d", i, maxRelaunches)
		}
	}
	intento, rendirse := r.murió(time.Second)
	if !rendirse {
		t.Fatalf("la caída %d tendría que rendirse, con el tope en %d", intento, maxRelaunches)
	}
}

// TestUnaInterfazQueAguantóDevuelveLasTresOportunidades es la mitad que impide
// que el tope se agote a lo largo de una tarde entera.
//
// Sin esto, cerrar la ventana tres veces en tres horas contaría igual que una
// interfaz que no arranca, y a la cuarta el daemon se apagaría con la sala
// abierta.
func TestUnaInterfazQueAguantóDevuelveLasTresOportunidades(t *testing.T) {
	var r relanzador
	for i := 0; i < maxRelaunches; i++ {
		r.murió(time.Second)
	}

	// Una que vivió más que `timing.UIQuickDeath`: no es un arranque que falla, es
	// alguien que cerró la ventana.
	if _, rendirse := r.murió(timing.UIQuickDeath + time.Second); rendirse {
		t.Fatal("una interfaz que aguantó se contó como caída en cadena")
	}
	intento, _ := r.murió(time.Second)
	if intento != 2 {
		t.Fatalf("tras la vida larga la cuenta tendría que ir por 2, y va por %d", intento)
	}
}

// TestLaVidaLargaLimpiaAntesDeSumar fija el ORDEN, que es lo que hace que el
// caso de arriba funcione. Si se sumara antes de limpiar, la caída larga
// quedaría contada y la cuenta arrancaría en 1 de más.
func TestLaVidaLargaLimpiaAntesDeSumar(t *testing.T) {
	var r relanzador
	r.murió(time.Second)
	r.murió(time.Second)

	intento, _ := r.murió(timing.UIQuickDeath + time.Millisecond)
	if intento != 1 {
		t.Fatalf("la caída larga tendría que reabrir la cuenta en 1, y dio %d", intento)
	}
}
