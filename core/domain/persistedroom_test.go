package domain

import (
	"errors"
	"fmt"
	"net/netip"
	"reflect"
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
		Card:    []byte("una tarjeta sellada de mentira"),
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
	// DeepEqual y no `!=`: la tarjeta sellada es un slice, así que el struct
	// dejó de ser comparable. Que lo dijera el compilador es la señal correcta.
	if !reflect.DeepEqual(tengo, quiero) {
		t.Fatalf("la vuelta cambió la sala:\n%+v\n%+v", tengo, quiero)
	}
}

// Un archivo escrito ANTES de que existiera la tarjeta carga igual.
//
// Es lo que hace que agregar el campo no sea una migración: quien venía de una
// versión anterior reabre su sala como siempre, y lo único que no pasa es la
// republicación, porque no hay bytes que republicar.
func TestUnaSalaGuardadaSinTarjetaCargaIgual(t *testing.T) {
	quiero := salaGuardadaDePrueba(t)
	quiero.Card = nil

	raw, err := quiero.Encode()
	if err != nil {
		t.Fatal(err)
	}
	tengo, err := DecodePersistedRoom(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(tengo.Card) != 0 {
		t.Errorf("apareció una tarjeta de la nada: %q", tengo.Card)
	}
	if !reflect.DeepEqual(tengo, quiero) {
		t.Fatalf("la vuelta cambió la sala:\n%+v\n%+v", tengo, quiero)
	}
}

// Y una tarjeta pasada del tope se rechaza ACÁ, no del otro lado.
//
// El registro la rechazaría igual, con un 413, pero para entonces ya se habría
// hablado con él. El tope del dominio es el mismo número, así que un archivo
// inflado a mano no llega ni a salir de la máquina.
func TestUnaTarjetaGuardadaPasadaDelTopeSeRechaza(t *testing.T) {
	quiero := salaGuardadaDePrueba(t)
	quiero.Card = make([]byte, MaxCardBytes+1)

	raw, err := quiero.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodePersistedRoom(raw); !errors.Is(err, ErrPersistedShape) {
		t.Fatalf("una tarjeta de %d bytes se aceptó: %v", MaxCardBytes+1, err)
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

// TestNadaQueLleveSecretosSeImprimeEntero.
//
// Sale de la auditoría: los logs son locales y el usuario los copia al
// portapapeles con el botón de diagnóstico para pegarlos en el grupo. Un `%+v`
// de cualquiera de estos tipos en un mensaje de error manda ahí la identidad de
// la red real, que es portadora de acceso a la sala.
//
// Se comprueban los dos verbos porque son dos caminos distintos de fmt, y el que
// se escribe sin pensar es justamente `%+v`.
func TestNadaQueLleveSecretosSeImprimeEntero(t *testing.T) {
	secreto := "5eba57ianoesunsecretodeverdad"
	rdv := DeriveRendezvous(InviteID{})

	sala := PersistedRoom{
		Room:    Room{Seed: DefaultSeedHost},
		Name:    "Los panas",
		Subnet:  netip.MustParsePrefix("100.87.3.0/24"),
		GameID:  "project-zomboid",
		Card:    []byte("una tarjeta sellada de mentira"),
		SavedAt: time.Now(),
	}
	copy(sala.NetworkSecret[:], secreto)
	copy(sala.CardKey[:], secreto)

	host := HostSpec{Rendezvous: rdv, Subnet: sala.Subnet}
	copy(host.NetworkSecret[:], secreto)

	invitado := GuestSpec{Credential: Credential{ID: "cred-1", Token: secreto}}

	casos := map[string]any{
		"PersistedRoom":  sala,
		"HostSpec":       host,
		"GuestSpec":      invitado,
		"RendezvousSpec": RendezvousSpec{Rendezvous: rdv},
		"Credential":     invitado.Credential,
	}
	for nombre, v := range casos {
		for _, verbo := range []string{"%v", "%+v"} {
			salida := fmt.Sprintf(verbo, v)
			if strings.Contains(salida, secreto) {
				t.Errorf("%s con %s filtró el secreto: %s", nombre, verbo, salida)
			}
			// Los bytes crudos también: un arreglo se imprime como lista de
			// números, y ahí el secreto viaja igual de entero.
			if strings.Contains(salida, strings.Trim(fmt.Sprint([]byte(secreto)[:8]), "[]")) {
				t.Errorf("%s con %s filtró el secreto en crudo: %s", nombre, verbo, salida)
			}
		}
	}
}
