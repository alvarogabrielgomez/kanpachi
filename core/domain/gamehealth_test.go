package domain_test

import (
	"net/netip"
	"testing"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// El desvío SÍ puede apuntar a loopback, y hasta el 2026-08-26 se negaba.
//
// # Lo que se midió, y por qué cambia la respuesta
//
// La negativa venía de un hecho cierto: el núcleo tira como marciano un paquete
// que entra por un adaptador con destino en `127.0.0.0/8`. Lo que faltaba era la
// segunda mitad de ese hecho, que el propio comentario ya nombraba sin usarla:
// eso pasa **salvo que `route_localnet` esté puesto en esa interfaz**.
//
// Reproducido en un netns limpio, con la misma forma de regla que escribe el
// adaptador, un oyente UDP atado a `127.0.1.1` y el tráfico entrando por otro
// adaptador:
//
//	route_localnet=0  bytes entregados: 0
//	route_localnet=1  bytes entregados: 4
//	iif "lp0" ip daddr 10.9.9.1 udp dport 9999 counter packets 2 bytes 64 dnat to 127.0.1.1
//
// La regla casó las dos veces. Lo único que cambió fue el sysctl.
//
// # Por qué importa, y no es un caso de laboratorio
//
// Es el caso del contenedor con `hostNetwork`: la imagen de Project Zomboid
// rechaza `SERVER_IP=0.0.0.0` y cae a `hostname -i`, que compartiendo el
// espacio de red del nodo contesta `127.0.1.1`. Sin esto, la única salida era
// escribir a mano la dirección de la sala en el manifiesto, y esa dirección se
// SORTEA al crear la sala: dejaría de valer en cuanto alguien abriera una nueva.
//
// Quien pone el sysctl es el adaptador, sobre la interfaz de Kanpachi y ninguna
// otra. El dominio solo deja de mentir sobre lo que se puede desviar.
func TestElDesvíoAceptaUnDestinoDeLoopback(t *testing.T) {
	base := domain.RedirectSpec{
		Adapter: domain.AdapterName,
		RoomIP:  netip.MustParseAddr("100.93.137.1"),
		Ports:   []domain.PortRange{{Proto: domain.ProtoBoth, From: 16261, To: 16262}},
	}

	casos := []struct {
		nombre    string
		hacia     string
		entendido bool
	}{
		{"la dirección del pod, como antes", "10.42.2.151", true},
		{"el loopback al que ata la imagen con hostNetwork", "127.0.1.1", true},
		{"el loopback de siempre", "127.0.0.1", true},
	}

	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			spec := base
			spec.To = netip.MustParseAddr(c.hacia)
			if got := spec.Understood(); got != c.entendido {
				t.Fatalf("Understood() con destino %s = %v, se esperaba %v", c.hacia, got, c.entendido)
			}
		})
	}
}

// Lo que sigue sin ser un desvío, y por qué cada uno.
func TestElDesvíoSigueRechazandoLoQueNoEsUnDesvío(t *testing.T) {
	sala := netip.MustParseAddr("100.93.137.1")
	puertos := []domain.PortRange{{Proto: domain.ProtoBoth, From: 16261, To: 16262}}

	casos := map[string]domain.RedirectSpec{
		// Traducir la dirección de la sala hacia sí misma no traduce nada, y
		// dejaría una regla puesta para siempre sin efecto.
		"hacia la propia sala": {Adapter: domain.AdapterName, RoomIP: sala, To: sala, Ports: puertos},
		"sin adaptador":        {RoomIP: sala, To: netip.MustParseAddr("127.0.0.1"), Ports: puertos},
		"sin sala":             {Adapter: domain.AdapterName, To: netip.MustParseAddr("127.0.0.1"), Ports: puertos},
		"sin destino":          {Adapter: domain.AdapterName, RoomIP: sala, Ports: puertos},
		// Media regla de nat manda tráfico a cualquier parte.
		"sin puertos": {Adapter: domain.AdapterName, RoomIP: sala, To: netip.MustParseAddr("127.0.0.1")},
	}

	for nombre, spec := range casos {
		t.Run(nombre, func(t *testing.T) {
			if spec.Understood() {
				t.Fatal("se dio por entendido un desvío incompleto")
			}
		})
	}
}
