package usecase

import (
	"fmt"
	"testing"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// TestClaimAnuncioNoEsCadencia mide la frecuencia REAL del anuncio, que es lo
// que decidiría cuántas rondas de canario habría si el canario colgara de ahí.
//
// Dos escenarios: una sala de seis formándose con el reloj QUIETO, y diez
// minutos sin que pase nada con el latido del supervisor.
func TestClaimAnuncioNoEsCadencia(t *testing.T) {
	b := salaCreada(t)
	self := b.sesión.Status().LocalIP

	peers := []domain.Peer{{VirtualIP: self, Name: nick(t, "alvaro"), Host: true}}
	antes := len(b.control.anuncios)

	// Cinco invitados entrando uno a uno. El reloj NO se mueve: todo esto pasa
	// dentro del mismo instante simulado.
	ip := self
	for i := 0; i < 5; i++ {
		ip = ip.Next()
		peers = append(peers, domain.Peer{
			VirtualIP: ip,
			Name:      nick(t, fmt.Sprintf("invitado%d", i)),
			Path:      domain.PathDirect,
		})
		b.motor.peers = append([]domain.Peer(nil), peers...)
		if _, err := b.sesión.OnPeersChanged(ctx()); err != nil {
			t.Fatal(err)
		}
	}
	ráfaga := len(b.control.anuncios) - antes
	t.Logf("sala de 6 formándose, reloj quieto: %d anuncios", ráfaga)
	if ráfaga < 5 {
		t.Fatalf("REFUTADO: la ráfaga de entradas no produjo un anuncio por entrada, fueron %d", ráfaga)
	}

	// Un evento del motor repetido, que es lo que documenta engineevent.go:
	// "El motor los emite de a ráfagas".
	antes = len(b.control.anuncios)
	for i := 0; i < 3; i++ {
		if _, err := b.sesión.OnPeersChanged(ctx()); err != nil {
			t.Fatal(err)
		}
	}
	repetidos := len(b.control.anuncios) - antes
	t.Logf("tres eventos de miembros SIN cambio real: %d anuncios", repetidos)

	// Ahora diez minutos quietos, latiendo cada 15s como el supervisor.
	antes = len(b.control.anuncios)
	for pasado := time.Duration(0); pasado < 10*time.Minute; pasado += 15 * time.Second {
		b.reloj.avanza(15 * time.Second)
		b.sesión.Tick(ctx())
	}
	quietos := len(b.control.anuncios) - antes
	t.Logf("diez minutos sin que pase nada: %d anuncios (AnnounceInterval=%s)", quietos, AnnounceInterval)
	if quietos != 5 {
		t.Fatalf("el suelo periódico no dio 5 en diez minutos, dio %d", quietos)
	}

	// Y que el suelo se REINICIA con cada evento: un anuncio por evento retrasa
	// el periódico, o sea que 2m es un mínimo entre anuncios y no un período.
	b.reloj.avanza(90 * time.Second)
	antes = len(b.control.anuncios)
	if _, err := b.sesión.RenameRoom(ctx(), "otro nombre"); err != nil {
		t.Fatal(err)
	}
	b.reloj.avanza(45 * time.Second) // ya pasaron 2m15s desde el último periódico
	b.sesión.Tick(ctx())
	trasEvento := len(b.control.anuncios) - antes
	t.Logf("renombrar + latido 45s después: %d anuncios (1 = el evento reinició el suelo)", trasEvento)
}

// TestClaimCuantoTardaUnaRonda pone en números lo que costaría esperar un
// informe: el caso BUENO es el lento, porque limpio significa que los dos
// sondeos del invitado agotan su plazo.
func TestClaimCuantoTardaUnaRonda(t *testing.T) {
	t.Logf("ProbeDeadline por protocolo = %s", domain.ProbeDeadline)
	t.Logf("una ronda limpia (TCP silencioso + UDP silencioso, en serie) = %s de sondeo",
		2*domain.ProbeDeadline)
	t.Logf("Beat del supervisor = 15s, AnnounceInterval = %s", AnnounceInterval)
}
