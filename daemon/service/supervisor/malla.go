package supervisor

import (
	"context"
	"sort"
	"strings"
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
type VigiaDeMalla struct {
	Motor Malla
	Log   port.Logger
}

func (m *VigiaDeMalla) Correr(ctx context.Context) {
	t := time.NewTicker(timing.MeshBeat)
	defer t.Stop()

	// El arranque no cuenta como cambio: sin sala no hay malla, y anunciarlo
	// sería una línea de ruido en cada arranque.
	firma := "sin sala"
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
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
