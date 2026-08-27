package domain_test

import (
	"net/netip"
	"testing"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
)

func salaDePrueba(t *testing.T, code string) domain.Room {
	t.Helper()
	id, err := domain.ParseInviteID(code)
	if err != nil {
		t.Fatal(err)
	}
	return domain.Room{InviteID: id, Seed: "kanpachi.accentio.dev"}
}

func apodo(t *testing.T, s string) domain.Nickname {
	t.Helper()
	n, err := domain.ParseNickname(s)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func libro(t *testing.T, gen uint64, entradas ...domain.BookEntry) domain.CredentialBook {
	t.Helper()
	return domain.CredentialBook{
		Gen:     gen,
		Room:    salaDePrueba(t, "V59DGEL5"),
		Entries: entradas,
	}
}

func entrada(t *testing.T, ip string, id string, vence time.Time) domain.BookEntry {
	t.Helper()
	return domain.BookEntry{
		VirtualIP: netip.MustParseAddr(ip),
		ID:        domain.CredentialID(id),
		Name:      apodo(t, "pericoman"),
		MemberKey: []byte("member-key-de-prueba"),
		IssuedAt:  vence.Add(-24 * time.Hour),
		ExpiresAt: vence,
	}
}

// TestUnaCopiaViejaDelLibroNoResucitaAUnExpulsado.
//
// El sello autentica pero no protege contra reversión: sin contador, una copia
// más vieja de la misma máquina abre perfecto. Sin esto, restaurar un libro
// anterior a una expulsión le devuelve al expulsado su dirección y su ranura
// pre-autorizada en el oyente que corre como SYSTEM.
func TestUnaCopiaViejaDelLibroNoResucitaAUnExpulsado(t *testing.T) {
	ahora := time.Unix(1700000000, 0).UTC()
	sala := salaDePrueba(t, "V59DGEL5")

	vieja := libro(t, 7, entrada(t, "100.93.137.3", "c1", ahora.Add(20*time.Hour)))
	revocada := entrada(t, "100.93.137.3", "c1", ahora.Add(20*time.Hour))
	revocada.Revoked = true
	nueva := libro(t, 9, revocada)

	crudoNuevo, err := nueva.Encode()
	if err != nil {
		t.Fatal(err)
	}
	crudoViejo, err := vieja.Encode()
	if err != nil {
		t.Fatal(err)
	}

	// Con la generación conocida por debajo, el libro nuevo abre.
	leido, err := domain.DecodeCredentialBook(crudoNuevo, 7, sala, ahora)
	if err != nil {
		t.Fatal(err)
	}
	if leido.Gen != 9 || len(leido.Entries) != 1 || !leido.Entries[0].Revoked {
		t.Fatalf("no se leyó lo que se guardó: %+v", leido)
	}

	// Y la copia anterior a la expulsión se rechaza.
	if _, err := domain.DecodeCredentialBook(crudoViejo, 9, sala, ahora); err == nil {
		t.Fatal("una generación anterior a la conocida tenía que rechazarse")
	}
}

// TestElLibroDeOtraSalaNoSeCarga.
//
// Sus direcciones son válidas, así que cargarlo abriría el canal de control a
// las IP de una sala ajena. Es el mismo motivo por el que salir vacía la tabla.
func TestElLibroDeOtraSalaNoSeCarga(t *testing.T) {
	ahora := time.Unix(1700000000, 0).UTC()
	otra := salaDePrueba(t, "AAAABBBB")

	crudo, err := libro(t, 1, entrada(t, "100.93.137.3", "c1", ahora.Add(time.Hour))).Encode()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := domain.DecodeCredentialBook(crudo, 0, otra, ahora); err == nil {
		t.Fatal("se cargó el libro de otra sala")
	}
}

// TestAlCargarSePodaLoQueYaNoAutoriza.
//
// El precedente medido: setenta y tres direcciones quemadas en cuatro horas
// hasta agotar el /24. Un libro que solo crece termina en una sala sin
// direcciones libres teniendo el rango vacío.
func TestAlCargarSePodaLoQueYaNoAutoriza(t *testing.T) {
	ahora := time.Unix(1700000000, 0).UTC()
	sala := salaDePrueba(t, "V59DGEL5")

	viva := entrada(t, "100.93.137.3", "viva", ahora.Add(time.Hour))
	vencida := entrada(t, "100.93.137.4", "vencida", ahora.Add(-time.Minute))
	sinLlave := entrada(t, "100.93.137.5", "sinllave", ahora.Add(time.Hour))
	sinLlave.MemberKey = nil

	crudo, err := libro(t, 1, viva, vencida, sinLlave).Encode()
	if err != nil {
		t.Fatal(err)
	}
	leido, err := domain.DecodeCredentialBook(crudo, 0, sala, ahora)
	if err != nil {
		t.Fatal(err)
	}
	if len(leido.Entries) != 1 || leido.Entries[0].ID != "viva" {
		t.Fatalf("la poda no dejó solo lo que autoriza: %+v", leido.Entries)
	}
}

// TestElLibroNoGuardaElToken: es el único secreto de la ficha, y además es
// inútil tras reiniciar, porque las credenciales del motor mueren con él.
func TestElLibroNoGuardaElToken(t *testing.T) {
	ahora := time.Unix(1700000000, 0).UTC()
	crudo, err := libro(t, 1, entrada(t, "100.93.137.3", "c1", ahora.Add(time.Hour))).Encode()
	if err != nil {
		t.Fatal(err)
	}
	for _, prohibido := range []string{"token", "Token"} {
		if contiene(crudo, prohibido) {
			t.Fatalf("el libro serializado nombra %q", prohibido)
		}
	}
}

func contiene(b []byte, s string) bool {
	return len(b) >= len(s) && indexOf(string(b), s) >= 0
}

func indexOf(h, n string) int {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return i
		}
	}
	return -1
}
