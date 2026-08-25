package supervisor

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/core/port"
	"github.com/accentiostudios/kanpachi/core/timing"
)

// El vigía de la malla, que estuvo un tiempo SOLO en `roomprobe`.
//
// Estaba mal que así fuera, y no por elegancia: para conseguir el único dato
// que contesta "¿los dos motores llegaron a verse?" había que pedirle a la
// persona que corriera otra herramienta y reprodujera el fallo. El binario que
// ya está corriendo tiene que anotarlo solo. Viviendo acá lo heredan los tres
// —instalado, portable y roomprobe— sin que ninguno se acuerde de nada.

// malla es lo poco que el vigía necesita del motor.
type Malla interface {
	Peers(context.Context) ([]domain.Peer, error)
}

// VigiaDeMalla vigila la malla del motor y anota CADA CAMBIO.
//
// # Por qué le pregunta al motor y no a la sesión
//
// Porque durante un ingreso la sesión tiene el candado tomado de punta a punta:
// su lista de miembros no se refresca hasta que la operación termina, y el
// supervisor se queda esperando detrás. El resultado se midió el 2026-08-08. Un
// invitado marcó al host durante veintiún segundos, se rindió, y NINGUNO de los
// dos logs guardó lo único que hacía falta: si los dos motores llegaron a verse
// en la red de la sala. Los dos volcados de miembros se tomaron después del
// desmontaje, o sea de una sala que ya no existía.
//
// Sin ese dato, "el firewall no deja pasar" y "todavía no hay camino" se ven
// idénticos desde fuera, y son arreglos opuestos.
//
// El adaptador del motor habla por su propia tubería y no toca el candado de la
// sesión, así que esto sigue vivo justo cuando todo lo demás está bloqueado.
//
// # Qué NO es
//
// No es un latido: se anota el cambio, no el tick. Sin cambios no escribe una
// sola línea, que es lo que lo hace legible durante una espera larga.
//
// # Qué SÍ es, desde el 2026-08-25
//
// Además de anotar, avisa. Sondeaba el motor una vez por segundo, comparaba
// contra la muestra anterior, y tiraba la respuesta en una línea de log. Ese
// mismo trabajo es lo único del árbol que ve la tabla del motor YA
// CONVERGIDA, así que es la red de seguridad del evento `peers_changed`, que
// llega antes de que la ruta lleve dirección. Ver [VigiaDeMalla.Cambios].
type VigiaDeMalla struct {
	Motor Malla
	Log   port.Logger

	// tics reemplaza el reloj del bucle, y SOLO lo ponen los tests de este
	// paquete. Nil es el ticker de verdad, a [timing.MeshBeat]. Es el mismo
	// asiento que el supervisor tiene en `beats` y `sweeps`, y por lo mismo: sin
	// él, un test que necesita cuatro vueltas tarda cuatro segundos y no puede
	// afirmar nada sobre lo que NO pasó entre dos de ellas.
	tics <-chan time.Time

	// una protege la creación perezosa de `cambios`, para que pedir el canal y
	// arrancar el vigía no compitan.
	una sync.Once
	// cambios avisa de que la malla cambió. Uno solo cabe, y el envío NUNCA
	// bloquea. Ver [VigiaDeMalla.Cambios].
	cambios chan struct{}
}

// Cambios es por donde el supervisor se entera de que la malla cambió.
//
// # Amortiguado a uno, y sin bloquear
//
// A uno porque lo que se manda no es un dato, es «vuelve a mirar»: diez avisos
// seguidos piden exactamente lo mismo que uno.
//
// Sin bloquear porque este vigía existe para seguir vivo mientras la sesión
// tiene el candado tomado de punta a punta, que es justo cuando el despachador
// puede estar esperando detrás. Un envío bloqueante acá convertiría al testigo
// en otra víctima. Es la misma regla que `drenar` un escalón más abajo.
func (m *VigiaDeMalla) Cambios() <-chan struct{} {
	m.una.Do(func() { m.cambios = make(chan struct{}, 1) })
	return m.cambios
}

// avisar empuja el «vuelve a mirar» si hay quien lo escuche y no hay uno ya en
// la cola.
func (m *VigiaDeMalla) avisar() {
	if m.cambios == nil {
		return
	}
	select {
	case m.cambios <- struct{}{}:
	default: // ya hay uno esperando, y pide lo mismo
	}
}

func (m *VigiaDeMalla) Correr(ctx context.Context) {
	// Crea el canal si nadie lo pidió todavía, para que arrancar antes que el
	// supervisor no deje al vigía mudo para siempre.
	m.Cambios()

	tics := m.tics
	if tics == nil {
		t := time.NewTicker(timing.MeshBeat)
		defer t.Stop()
		tics = t.C
	}

	// El arranque no cuenta como cambio: sin sala no hay malla, y anunciarlo
	// sería una línea de ruido en cada arranque.
	firma := "sin sala"
	for {
		select {
		case <-ctx.Done():
			return
		case <-tics:
		}

		peers, err := m.Motor.Peers(ctx)
		if err != nil {
			// "there is no room running" es la respuesta NORMAL fuera de una
			// sala, y tratarla como fallo llenaría el log de errores durante
			// todo el tiempo que alguien pasa en el menú.
			firma = "sin sala"
			continue
		}

		nueva := firmaDeMalla(peers)
		if nueva == firma {
			continue
		}
		anterior := firma
		firma = nueva

		// El aviso va ACÁ y no al final: lo que sigue son líneas de log, y quien
		// espera para abrir la compuerta no tiene por qué esperarlas.
		//
		// Se avisa también cuando la malla se queda vacía, que es el caso que
		// sale por el `continue` de abajo. Vaciarse es el cambio más gordo que
		// hay, y hasta el 2026-08-25 solo escribía un Warn.
		m.avisar()

		if len(peers) == 0 {
			// Que se vacíe con la sala en pie es un hecho, y de los gordos.
			if anterior != "sin sala" {
				m.Log.Warn("la malla se quedó vacía: el motor ya no ve a nadie más")
			}
			continue
		}
		for _, p := range peers {
			if p.Self {
				continue
			}
			m.Log.Info("MALLA: el motor ve a alguien", "ip", p.VirtualIP.String(),
				"nombre", p.Name.String(), "camino", p.Path.String(),
				"rtt", p.RTT.String(), "host", p.Host)
		}
	}
}

// firmaDeMalla resume la malla para poder comparar dos muestras.
//
// Lleva el CAMINO además de la dirección: que un miembro pase de relay a
// directo es un cambio que interesa, y con la dirección sola sería invisible.
// No lleva el RTT, que se mueve solo y convertiría esto en un latido.
func firmaDeMalla(peers []domain.Peer) string {
	partes := make([]string, 0, len(peers))
	for _, p := range peers {
		partes = append(partes, p.VirtualIP.String()+"/"+p.Path.String())
	}
	sort.Strings(partes)
	return strings.Join(partes, ",")
}
