package domain

import (
	"errors"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func salaGuardadaDePrueba(t *testing.T) PersistedRoom {
	t.Helper()
	room, err := ParseRoom("A7K2M9QX")
	if err != nil {
		t.Fatal(err)
	}
	host, err := ParseNickname("alvaro")
	if err != nil {
		t.Fatal(err)
	}
	p := PersistedRoom{
		Room:    room,
		Name:    "Los panas",
		Host:    host,
		Subnet:  netip.MustParsePrefix("100.87.3.0/24"),
		GameID:  "project-zomboid",
		SavedAt: time.Date(2026, 8, 2, 20, 0, 0, 0, time.UTC),
	}
	for i := range p.NetworkID {
		p.NetworkID[i] = byte(i)
	}
	for i := range p.NetworkSecret {
		p.NetworkSecret[i] = byte(255 - i)
	}
	for i := range p.CardKey {
		p.CardKey[i] = byte(i * 3)
	}
	return p
}

func TestLaSalaGuardadaVaYVuelveIgual(t *testing.T) {
	quiero := salaGuardadaDePrueba(t)

	raw, err := quiero.Encode()
	if err != nil {
		t.Fatal(err)
	}
	tengo, err := DecodePersistedRoom(raw)
	if err != nil {
		t.Fatal(err)
	}
	if tengo != quiero {
		t.Fatalf("la vuelta cambió la sala:\n%+v\n%+v", tengo, quiero)
	}
}

// TestElEsquemaGuardadoNoAdmiteNadaQueNoSeaIdentidad.
//
// Es la invariante del archivo. Lleva identidad y referencias, jamás política:
// si un puerto, una regla o un plazo no se pueden escribir, un archivo
// manipulado no abre nada ni compra tiempo de más en ninguna sala.
func TestElEsquemaGuardadoNoAdmiteNadaQueNoSeaIdentidad(t *testing.T) {
	base, err := salaGuardadaDePrueba(t).Encode()
	if err != nil {
		t.Fatal(err)
	}

	colados := []string{
		`"host_absence_limit": 0`,
		`"host_ports": [{"proto":"tcp","range":"445"}]`,
		`"firewall_rules": []`,
		`"members": ["100.87.3.9"]`,
		`"credential": {"token":"t"}`,
		`"cualquier_cosa": true`,
	}
	for _, extra := range colados {
		t.Run(extra, func(t *testing.T) {
			roto := strings.Replace(string(base), "{\n", "{\n  "+extra+",\n", 1)
			if _, err := DecodePersistedRoom([]byte(roto)); !errors.Is(err, ErrPersistedShape) {
				t.Fatalf("se aceptó un campo que el esquema no tiene: %v", err)
			}
		})
	}
}

func TestUnaSalaGuardadaRotaSeRechazaEntera(t *testing.T) {
	casos := map[string]string{
		"json cortado":        `{"invite_id":"A7K2M9QX","seed":`,
		"dos objetos pegados": mustEncode(t) + `{"invite_id":"A7K2M9QX"}`,
		"código imposible":    strings.Replace(mustEncode(t), `"A7K2-M9QX"`, `"no-es-un-codigo"`, 1),
		"secreto corto":       strings.Replace(mustEncode(t), `"network_secret": "`, `"network_secret": "AAAA`, 1),
	}
	for nombre, texto := range casos {
		t.Run(nombre, func(t *testing.T) {
			if _, err := DecodePersistedRoom([]byte(texto)); !errors.Is(err, ErrPersistedShape) {
				t.Fatalf("se aceptó %s: %v", nombre, err)
			}
		})
	}
}

// TestLaSubredGuardadaSeComprueba: es la forma de que un archivo editado a mano
// mande el tráfico de la sala a otra parte.
func TestLaSubredGuardadaSeComprueba(t *testing.T) {
	casos := map[string]string{
		"fuera de los rangos de Kanpachi": "192.168.1.0/24",
		"el rango del vestíbulo":          RendezvousSubnet.String(),
		"un prefijo que no es /24":        "100.87.0.0/16",
	}
	for nombre, subred := range casos {
		t.Run(nombre, func(t *testing.T) {
			roto := strings.Replace(mustEncode(t), `"100.87.3.0/24"`, `"`+subred+`"`, 1)
			if _, err := DecodePersistedRoom([]byte(roto)); !errors.Is(err, ErrPersistedShape) {
				t.Fatalf("se aceptó la subred %q: %v", subred, err)
			}
		})
	}
}

// TestUnJuegoGuardadoConIdInválidoSeRechaza. Un id que no cumple las reglas del
// catálogo no puede casar con ningún perfil, así que se rechaza acá en vez de
// dejar que la sala reabra sin juego sin decir por qué.
func TestUnJuegoGuardadoConIdInválidoSeRechaza(t *testing.T) {
	roto := strings.Replace(mustEncode(t), `"project-zomboid"`, `"Project Zomboid"`, 1)
	if _, err := DecodePersistedRoom([]byte(roto)); !errors.Is(err, ErrPersistedShape) {
		t.Fatalf("se aceptó un id de juego inválido: %v", err)
	}
}

func TestLaSalaGuardadaAdmiteNoTenerJuego(t *testing.T) {
	p := salaGuardadaDePrueba(t)
	p.GameID = ""
	raw, err := p.Encode()
	if err != nil {
		t.Fatal(err)
	}
	tengo, err := DecodePersistedRoom(raw)
	if err != nil {
		t.Fatalf("una sala sin juego no se pudo releer: %v", err)
	}
	if tengo.GameID != "" {
		t.Fatalf("apareció un juego de la nada: %q", tengo.GameID)
	}
}

func TestLaÚltimaSalaVaYVuelveIgual(t *testing.T) {
	room, err := ParseRoom("A7K2M9QX@seed.midominio.com")
	if err != nil {
		t.Fatal(err)
	}
	nick, err := ParseNickname("humberto")
	if err != nil {
		t.Fatal(err)
	}
	quiero := LastRoom{
		Room:    room,
		Name:    "Los panas",
		Nick:    nick,
		SavedAt: time.Date(2026, 8, 2, 20, 0, 0, 0, time.UTC),
	}

	raw, err := quiero.Encode()
	if err != nil {
		t.Fatal(err)
	}
	// El seed viaja pegado al código, porque un invite ID solo significa algo
	// en el registro que lo emitió. Perderlo mandaría a volver a otra sala.
	if !strings.Contains(string(raw), "seed.midominio.com") {
		t.Fatalf("el seed no se guardó:\n%s", raw)
	}
	tengo, err := DecodeLastRoom(raw)
	if err != nil {
		t.Fatal(err)
	}
	if tengo != quiero {
		t.Fatalf("la vuelta cambió la última sala:\n%+v\n%+v", tengo, quiero)
	}
}

func TestLaÚltimaSalaTampocoAdmiteCamposDeMás(t *testing.T) {
	roto := `{"invite_id":"A7K2M9QX","seed":"kanpachi.accentio.dev","name":"x","nick":"humberto",` +
		`"saved_at":"2026-08-02T20:00:00Z","credential":{"token":"t"}}`
	if _, err := DecodeLastRoom([]byte(roto)); !errors.Is(err, ErrPersistedShape) {
		t.Fatalf("se aceptó una credencial dentro de la última sala: %v", err)
	}
}

func mustEncode(t *testing.T) string {
	t.Helper()
	raw, err := salaGuardadaDePrueba(t).Encode()
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
