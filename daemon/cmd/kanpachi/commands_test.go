package main

import (
	"testing"

	"github.com/accentiostudios/kanpachi/daemon/transport/protocol"
)

// TestExpulsarPorIPDiceAQuiénEstásEchando.
//
// `kanpachi kick 100.93.137.5` casaba la dirección contra la lista viva y
// expulsaba a quien la tuviera EN ESE MOMENTO, en silencio y con cara de éxito.
// La única guarda que había protege el sentido contrario: un NOMBRE repetido se
// rechaza y te manda a escribir la IP, o sea que trata la IP como lo
// inequívoco. Y no lo es: en la sala real `pericoman` y `jorungador`
// compartieron la .2 en cinco días, porque el reparto recicla las direcciones
// de quien se va.
//
// El arreglo no es prohibir la IP, que es lo que desambigua un nombre repetido.
// Es decir a quién estás echando antes de hacerlo.
func TestExpulsarPorIPDiceAQuiénEstásEchando(t *testing.T) {
	st := protocol.RoomView{Peers: []protocol.PeerView{
		{IP: "100.93.137.1", Name: "alvaro", Self: true, Host: true},
		{IP: "100.93.137.2", Name: "pericoman"},
		{IP: "100.93.137.5", Name: "jorungador"},
	}}

	t.Run("por IP hay que confirmar, y con el nombre resuelto", func(t *testing.T) {
		p, confirmar, err := kickTarget(st, "100.93.137.5")
		if err != nil {
			t.Fatal(err)
		}
		if !confirmar {
			t.Fatal("expulsó por dirección sin decir de quién es")
		}
		if p.Name != "jorungador" {
			t.Fatalf("resolvió el nombre mal: %q", p.Name)
		}
	})

	t.Run("por nombre no hay nada que confirmar", func(t *testing.T) {
		p, confirmar, err := kickTarget(st, "pericoman")
		if err != nil {
			t.Fatal(err)
		}
		if confirmar {
			t.Fatal("pidió confirmar un nombre, que es lo que la persona leyó en pantalla")
		}
		if p.IP != "100.93.137.2" {
			t.Fatalf("resolvió la dirección mal: %q", p.IP)
		}
	})

	t.Run("un nombre repetido sigue siendo un error", func(t *testing.T) {
		dos := protocol.RoomView{Peers: []protocol.PeerView{
			{IP: "100.93.137.2", Name: "pericoman"},
			{IP: "100.93.137.7", Name: "pericoman"},
		}}
		if _, _, err := kickTarget(dos, "pericoman"); err == nil {
			t.Fatal("eligió uno de dos con el mismo nombre")
		}
	})

	t.Run("uno mismo sigue siendo un error", func(t *testing.T) {
		if _, _, err := kickTarget(st, "alvaro"); err == nil {
			t.Fatal("se dejó expulsar a uno mismo")
		}
	})
}

// TestElHuecoDelPingLoOcupaElOffline.
//
// La columna donde va la latencia es la que contesta «¿este miembro está?». Si
// no hay medición porque no está, el hueco tiene que decirlo en vez de dejar un
// guion que se lee como «todavía no se midió».
//
// Decía `AFK` hasta el 2026-08-26, y `AFK` afirma algo que este host no midió:
// que la persona se levantó de la silla. Lo medido es que el motor dejó de
// verla, que es igual de compatible con un WiFi caído.
func TestElHuecoDelPingLoOcupaElOffline(t *testing.T) {
	casos := []struct {
		nombre string
		p      protocol.PeerView
		quiero string
	}{
		{"presente con medición", protocol.PeerView{RTTMS: 42}, "42 ms"},
		{"presente sin medición todavía", protocol.PeerView{}, "-"},
		{"ausente hace minutos", protocol.PeerView{Away: true, AwayForMS: 3 * 60 * 1000}, "offline 3m"},
		{"ausente hace segundos", protocol.PeerView{Away: true, AwayForMS: 42 * 1000}, "offline 42s"},
		{"ausente hace horas", protocol.PeerView{Away: true, AwayForMS: 5 * 3600 * 1000}, "offline 5h"},
		{"ausente sin saber desde cuándo", protocol.PeerView{Away: true}, "offline"},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			if got := peerLatency(c.p); got != c.quiero {
				t.Fatalf("peerLatency = %q, se esperaba %q", got, c.quiero)
			}
		})
	}
}

// TestElAsistenteDiceQuiénNoEstá.
//
// La lista de expulsar es la única pantalla del asistente donde se elige a una
// persona concreta, así que es donde importa saber si esa persona está. Sin
// eso, echar a alguien porque «no responde» es echar a alguien que se fue a
// buscar café.
func TestElAsistenteDiceQuiénNoEstá(t *testing.T) {
	presente := protocol.PeerView{Name: "pericoman", IP: "100.93.137.2", RTTMS: 42}
	if got, quiero := kickLabel(presente), "Kick pericoman (100.93.137.2)"; got != quiero {
		t.Fatalf("kickLabel = %q, se esperaba %q", got, quiero)
	}

	ausente := protocol.PeerView{Name: "wololo", IP: "100.93.137.4", Away: true, AwayForMS: 3 * 60 * 1000}
	if got, quiero := kickLabel(ausente), "Kick wololo (100.93.137.4) [offline 3m]"; got != quiero {
		t.Fatalf("kickLabel = %q, se esperaba %q", got, quiero)
	}
}
