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
