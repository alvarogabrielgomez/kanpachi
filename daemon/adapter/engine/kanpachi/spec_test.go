package kanpachi

import (
	"encoding/json"
	"net/netip"
	"strings"
	"testing"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// Lo que este fichero afirma, y por qué el guardián de arquitectura lo exige.
//
// `internal/arch/motor_test.go` lee cadenas literales y no puede afirmar el
// VALOR de nada en el caso general. La garantía de verdad vive acá: se evalúa lo
// que la función GENERA, no lo que el fuente dice. Mismo patrón que
// `registry/setup/setup_test.go`, que es el que cubre el arranque del seed.

func TestTheHostRequestCarriesTheRealNetworkAndNoSeedName(t *testing.T) {
	spec := domain.HostSpec{
		NetworkID:     [16]byte{0xa1, 0xb2},
		NetworkSecret: [32]byte{0xde, 0xad, 0xbe, 0xef},
		Name:          nick(t, "Alvaro"),
		Subnet:        netip.MustParsePrefix("100.64.7.0/24"),
	}
	req := hostRequest(1, spec, []string{"tcp://203.0.113.9:11010"}, "")

	if req.Cmd.Host == nil {
		t.Fatal("la orden no es un host")
	}
	h := req.Cmd.Host

	if h.NetworkName != spec.RealNetworkName() {
		t.Errorf("nombre de red %q, se esperaba %q", h.NetworkName, spec.RealNetworkName())
	}
	if !strings.HasPrefix(h.NetworkSecret, "deadbeef") {
		t.Errorf("el secreto salió como %q, y tiene que ser el hexadecimal de los 32 bytes", h.NetworkSecret)
	}
	if h.IPv4 != "100.64.7.1/24" {
		t.Errorf("el host tomó %q, y le toca la primera utilizable de su subred", h.IPv4)
	}
	if h.Common.DevName != RoomDevice {
		t.Errorf("adaptador %q, la sala va en %q", h.Common.DevName, RoomDevice)
	}
	// El nombre del seed no puede viajar: el motor lo resolvería por su cuenta y
	// la comprobación del adaptador no gobernaría nada.
	for _, p := range h.Common.Peers {
		if !strings.HasPrefix(p, "tcp://") {
			t.Errorf("el peer %q no es una URI con dirección", p)
		}
	}
}

func TestTheGuestRequestCarriesNoNetworkSecret(t *testing.T) {
	spec := domain.GuestSpec{
		Credential: domain.Credential{
			ID:          "cred-7",
			Token:       "c2VjcmV0bw==",
			NetworkName: "kanpachi-a1b2",
		},
		Name: nick(t, "Gabriel"),
	}
	req := guestRequest(2, spec, []string{"tcp://203.0.113.9:11010"}, "")

	if req.Cmd.Join == nil {
		t.Fatal("la orden no es un join")
	}

	// La afirmación central de la decisión 2. Se comprueba sobre el JSON de
	// verdad y no sobre los campos de la estructura, porque lo que importa es lo
	// que sale por el tubo: un campo agregado más adelante que se llamara
	// `network_secret` pasaría un test que solo mirase los campos que hoy
	// existen.
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "network_secret") {
		t.Errorf("la orden del invitado nombra el secreto de la red:\n  %s", raw)
	}
	if strings.Contains(string(raw), "credential_id") {
		t.Errorf("la orden del invitado manda un credential_id que el motor ignora:\n  %s", raw)
	}
	if req.Cmd.Join.CredentialSecret != spec.Credential.Token {
		t.Error("la credencial no llegó a la orden")
	}
}

func TestTheLobbyGoesOnItsOwnAdapter(t *testing.T) {
	// El alfabeto del código deja fuera los símbolos que se confunden al
	// dictarlo por voz, el `1` entre ellos.
	id, err := domain.ParseInviteID("A7K2M9QX")
	if err != nil {
		t.Fatal(err)
	}
	rdv := domain.DeriveRendezvous(id)
	spec := domain.RendezvousSpec{
		Rendezvous: rdv,
		Address:    rdv.LobbyHostAddress(),
		Name:       nick(t, "Alvaro"),
	}
	req := lobbyRequest(3, spec, []string{"tcp://203.0.113.9:11010"}, "")

	if req.Cmd.JoinRendezvous == nil {
		t.Fatal("la orden no es un join_rendezvous")
	}
	l := req.Cmd.JoinRendezvous

	// Son dos redes a la vez, así que son dos adaptadores. Compartir nombre haría
	// que el segundo arranque pisara al primero, y el invitado que suelta el
	// vestíbulo se quedaría sin la sala.
	if l.Common.DevName == RoomDevice {
		t.Errorf("el vestíbulo usa el adaptador de la sala, %q", l.Common.DevName)
	}
	if l.Common.DevName != LobbyDevice {
		t.Errorf("adaptador del vestíbulo %q, se esperaba %q", l.Common.DevName, LobbyDevice)
	}
	if l.NetworkName != rdv.NetworkName() {
		t.Errorf("red del vestíbulo %q, se esperaba %q", l.NetworkName, rdv.NetworkName())
	}
	// El host toma la .1 del vestíbulo de SU sala, con máscara de /24. Ya no es
	// una dirección fija: cada sala deriva su vestíbulo del código, que es lo
	// que permite moverlo cuando le choca a alguien. Ver
	// [domain.Rendezvous.LobbySubnet].
	esperada := netip.PrefixFrom(rdv.LobbyHostAddress(), domain.RoomPrefixBits).String()
	if l.IPv4 != esperada {
		t.Errorf("el host tomó %q en el vestíbulo, y le tocaba %q", l.IPv4, esperada)
	}
	if !domain.LobbySpace.Contains(rdv.LobbyHostAddress()) {
		t.Errorf("el vestíbulo cayó fuera de %v", domain.LobbySpace)
	}
}

// El fallo que este test existe para impedir: un nombre impecable que resuelve a
// la red de casa del usuario. Sin esta comprobación, un código de invitación
// fabricado convierte al daemon, que corre como SYSTEM, en un escáner de la LAN
// de quien lo pegue.
func TestAResolvedSeedInsideTheHomeNetworkIsRejected(t *testing.T) {
	casos := map[string]string{
		"LAN privada":     "192.168.1.1",
		"loopback":        "127.0.0.1",
		"link local":      "169.254.169.254",
		"IPv4 disfrazada": "::ffff:10.0.0.1",
	}
	for nombre, ip := range casos {
		t.Run(nombre, func(t *testing.T) {
			_, err := seedURIs([]string{"seed.example.com"}, func(string) ([]netip.Addr, error) {
				return []netip.Addr{netip.MustParseAddr(ip)}, nil
			})
			if err == nil {
				t.Fatalf("%s se aceptó como seed", ip)
			}
		})
	}
}

// enrutable es una dirección que de verdad pasa la comprobación.
//
// NO se pueden usar acá los rangos de documentación, `203.0.113.0/24` y los
// suyos: `domain.CheckSeedAddr` los rechaza, y con razón, porque un seed que
// apunta ahí no lleva a ninguna parte. Se supo corriendo el test, que es la
// forma en que se aprende que la comprobación cubre más de lo que uno recordaba.
const enrutable = "93.184.216.34"

func TestAPublicSeedResolvesToATcpURI(t *testing.T) {
	uris, err := seedURIs([]string{"seed.example.com"}, func(string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr(enrutable)}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(uris) != 1 || uris[0] != "tcp://"+enrutable+":11010" {
		t.Errorf("salió %v", uris)
	}
}

// Un seed que resuelve a varias direcciones donde una es privada no se descarta
// entero: se usan las buenas. Descartarlo entero dejaría sin sala a quien tenga
// un DNS con split horizon.
func TestAMixedSeedKeepsOnlyTheUsableAddresses(t *testing.T) {
	uris, err := seedURIs([]string{"seed.example.com"}, func(string) ([]netip.Addr, error) {
		return []netip.Addr{
			netip.MustParseAddr("10.0.0.5"),
			netip.MustParseAddr(enrutable),
		}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(uris) != 1 || uris[0] != "tcp://"+enrutable+":11010" {
		t.Errorf("salió %v", uris)
	}
}

// Un camino que este daemon no conoce es un ERROR y no un valor por defecto.
// Elegir "directo" en silencio pintaría de verde un camino que va por relay.
func TestAnUnknownPathIsAnError(t *testing.T) {
	if _, err := toPeers([]peerOut{{Path: "quantum"}}); err == nil {
		t.Fatal("un camino desconocido pasó como bueno")
	}
}

// Un nombre que no pasa la validación no rompe la lista: se queda vacío.
//
// Las dos mitades importan, y las dos son de seguridad. El nombre llega por la
// red desde la máquina de otra persona y se pinta en la pantalla de todo el
// mundo, así que tiene que pasar por `ParseNickname`. Y devolver error dejaría a
// la sala sin lista de miembros porque UNA persona puso algo raro.
func TestAHostileNameIsDroppedWithoutBreakingTheList(t *testing.T) {
	peers, err := toPeers([]peerOut{
		{Path: "direct", VirtualIP: "100.64.7.2", Hostname: "Alvaro​Falso"},
		{Path: "direct", VirtualIP: "100.64.7.3", Hostname: "Gabriel"},
	})
	if err != nil {
		t.Fatalf("un nombre raro tumbó la lista entera: %v", err)
	}
	if len(peers) != 2 {
		t.Fatalf("se esperaban 2 miembros, salieron %d", len(peers))
	}
	if !peers[0].Name.IsZero() {
		t.Errorf("el nombre hostil %q entró a la lista", peers[0].Name)
	}
	if peers[1].Name.String() != "Gabriel" {
		t.Errorf("el nombre bueno se perdió: %q", peers[1].Name)
	}
}

// Un miembro sin dirección no es un miembro, y este test dice primero POR QUÉ.
//
// El motor reportaba como miembro al seed, que releva para la sala sin vivir en
// su espacio de direcciones, así que venía sin IP. El daemon lo aceptaba y
// guardaba una dirección cero: en la pantalla salía un miembro llamado
// "invalid IP", y todo lo que se hace con un miembro se hace POR su IP (abrirle
// las reglas del firewall, expulsarlo). El hueco silencioso era el fallo.
func TestAMemberWithNoAddressIsRefused(t *testing.T) {
	// La consecuencia primero: con la dirección vacía aceptada, esto era un
	// miembro con dirección cero en vez de un error.
	peers, err := toPeers([]peerOut{{Path: "direct", VirtualIP: ""}})
	if err == nil {
		t.Fatalf("un miembro sin dirección entró a la lista: %+v", peers)
	}

	// Y el caso bueno sigue pasando, para que el rechazo no sea rechazarlo todo.
	if _, err := toPeers([]peerOut{{Path: "direct", VirtualIP: "10.99.7.2"}}); err != nil {
		t.Fatalf("un miembro con dirección buena fue rechazado: %v", err)
	}
}

func TestAnUnknownEventIsAnError(t *testing.T) {
	if _, err := toEventKind("teleported"); err == nil {
		t.Fatal("un evento desconocido pasó como bueno")
	}
	// Y `died` no viene del motor: lo levanta el adaptador cuando el proceso
	// hijo muere, porque un motor muerto no puede avisar de nada.
	if _, err := toEventKind("died"); err == nil {
		t.Fatal("el motor no puede reportar su propia muerte, y este daemon lo aceptó")
	}
}

// nick construye un Nickname o falla el test.
func nick(t *testing.T, s string) domain.Nickname {
	t.Helper()
	n, err := domain.ParseNickname(s)
	if err != nil {
		t.Fatal(err)
	}
	return n
}
