package domain_test

import (
	"net/netip"
	"testing"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
)

func addr(t *testing.T, s string) netip.Addr {
	t.Helper()
	a, err := netip.ParseAddr(s)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// TestLaPresenciaLlevaSuProcedencia.
//
// «El motor no me lo dice hace cuarenta segundos» y «se fue» son cosas
// distintas, y hasta el 2026-08-25 las dos se veían como ausencia. Esa
// confusión dejó a un host con una regla de firewall abierta hacia alguien que
// ya no estaba, durante horas.
//
// Las fuentes tampoco se sustituyen. El motor sabe el camino y el RTT; el canal
// de control prueba presencia de primera mano y ANTES que el motor, medido con
// dos máquinas el 2026-08-13; el libro conoce a quien todavía no llegó.
func TestLaPresenciaLlevaSuProcedencia(t *testing.T) {
	t0 := time.Unix(1700000000, 0).UTC()
	m := &domain.Member{IP: addr(t, "100.93.137.2")}

	m.NoteMesh(t0, domain.PathRelay, 42*time.Millisecond)

	if !m.Presence.InMesh {
		t.Fatal("verlo en la malla tenía que marcarlo presente")
	}
	if !m.Presence.MeshAt.Equal(t0) {
		t.Fatal("no guardó CUÁNDO lo vio, que es la mitad del dato")
	}
	if m.Presence.HasChannel {
		t.Fatal("verlo en la malla no prueba que tenga canal")
	}
	if m.Path != domain.PathRelay || m.RTT != 42*time.Millisecond {
		t.Fatalf("el camino y el RTT salen de la malla y de ningún otro sitio: %v %v", m.Path, m.RTT)
	}

	// Dejar de verlo NO borra cuándo se le vio. Eso es lo que separa «hace
	// cuarenta segundos que no lo dice» de «se fue».
	t1 := t0.Add(40 * time.Second)
	m.NoteOutOfMesh()
	if m.Presence.InMesh {
		t.Fatal("salir de la tabla tenía que quitarle la presencia")
	}
	if !m.Presence.MeshAt.Equal(t0) {
		t.Fatal("perdió cuándo se le vio por última vez")
	}
	if got := m.AwayFor(t1); got != 40*time.Second {
		t.Fatalf("cuánto lleva fuera = %v, se esperaba 40s", got)
	}

	// El canal es su propia fuente, con su propio reloj.
	m.NoteChannel(t1)
	if !m.Presence.HasChannel || !m.Presence.ChannelSince.Equal(t1) {
		t.Fatal("el canal abierto no quedó anotado con su hora")
	}
	if m.Presence.InMesh {
		t.Fatal("tener canal no lo devuelve a la tabla del motor")
	}
}

// TestQuiénEntraEnCadaPuerta: los dos predicados que deciden firewall viven en
// el dominio y en un solo sitio, en vez de repartidos por los casos de uso.
func TestQuiénEntraEnCadaPuerta(t *testing.T) {
	t0 := time.Unix(1700000000, 0).UTC()
	viva := &domain.Credential{ExpiresAt: t0.Add(time.Hour)}

	casos := []struct {
		nombre string
		m      domain.Member
		quiero bool
	}{
		{"presente con ficha", domain.Member{Cred: viva, Presence: domain.Presence{InMesh: true}}, true},
		{"ausente con ficha viva conserva su silla", domain.Member{Cred: viva}, true},
		{"presente sin ficha, que es un host reiniciado", domain.Member{Presence: domain.Presence{InMesh: true}}, true},
		{"expulsado", domain.Member{Cred: viva, Presence: domain.Presence{InMesh: true, Kicked: true}}, false},
		{"ficha vencida y fuera de la malla", domain.Member{Cred: &domain.Credential{ExpiresAt: t0.Add(-time.Minute)}}, false},
		{"ficha revocada y fuera de la malla", domain.Member{Cred: &domain.Credential{ExpiresAt: t0.Add(time.Hour), Revoked: true}}, false},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			if got := c.m.IsMember(t0); got != c.quiero {
				t.Fatalf("IsMember = %v, se esperaba %v", got, c.quiero)
			}
		})
	}
}
