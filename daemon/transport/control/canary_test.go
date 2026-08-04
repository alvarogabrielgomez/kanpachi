package control

import (
	"encoding/json"
	"net/netip"
	"strings"
	"testing"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/daemon/transport/wire"
)

func nonce() [domain.CanaryNonceSize]byte {
	var n [domain.CanaryNonceSize]byte
	for i := range n {
		n[i] = byte(i + 7)
	}
	return n
}

// nonceBytes es el mismo número como sector, para los mensajes del cable.
func nonceBytes() []byte {
	n := nonce()
	return n[:]
}

func pedido(port uint16) domain.CanaryRequest {
	return domain.CanaryRequest{Host: ipHost, Port: port, Nonce: nonce()}
}

// El pedido llega con el destino puesto DESDE LA CONEXIÓN.
func TestElPedidoDelCanarioLlegaConLaDireccionDelHost(t *testing.T) {
	b := nuevoBanco(t, ipUno)
	uno := b.enSala(t, ipUno)

	if err := b.host.RequestCanary(ctx(), ipUno, pedido(51023)); err != nil {
		t.Fatal(err)
	}

	req := espera(t, uno.CanaryRequests(), "el pedido del canario")
	switch {
	case req.Host != ipHost:
		t.Errorf("destino = %s, y tiene que ser el host (%s)", req.Host, ipHost)
	case req.Port != 51023:
		t.Errorf("puerto = %d", req.Port)
	case req.Nonce != nonce():
		t.Errorf("el número llegó cambiado")
	}
}

// EL TEST QUE VALE POR TODO EL MENSAJE.
//
// Un host modificado no puede pedirle a un miembro que marque a una máquina de
// fuera, porque el mensaje no tiene dónde escribir un destino. Se comprueba
// mandando el JSON a mano con un campo de dirección de más: el esquema estricto
// lo rechaza entero, y aunque no lo hiciera, el destino se rellena desde la
// conexión y ese campo no se mira.
//
// Sin esta propiedad, el canal de la sala sería un escáner de puertos por
// encargo y el tráfico saldría de las casas de los miembros.
func TestNadiePuedePedirQueSeMarqueAOtraMaquina(t *testing.T) {
	b := nuevoBanco(t, ipUno)
	uno := b.enSala(t, ipUno)

	// Un sobre a mano, con la forma buena MÁS un campo de destino.
	sobre := envelope{
		Kind: KindCanaryRequest,
		Payload: json.RawMessage(
			`{"port":51023,"nonce":"BwgJCgsMDQ4PEBESExQVFg==","host":"8.8.8.8"}`),
	}

	srv := mustHost(t, b)
	pc, ok := srv.peer(ipUno)
	if !ok {
		t.Fatal("no hay conexión con el miembro")
	}
	if err := pc.write(sobre); err != nil {
		t.Fatal(err)
	}

	// Ni siquiera llega: un campo de más rechaza el mensaje entero, que es la
	// regla 3 de este paquete.
	nadaEn(t, uno.CanaryRequests(), "un pedido con destino inventado")

	// Y la garantía de fondo, que no depende del esquema estricto: el tipo del
	// cable no tiene campo de dirección. Si alguien se lo agrega, este test lo
	// dice con todas las letras.
	if strings.Contains(formaDelPedido(), "host") || strings.Contains(formaDelPedido(), "addr") {
		t.Fatal("canaryRequestMsg tiene un campo de dirección. Eso convierte el canal " +
			"de la sala en un escáner por encargo contra terceros")
	}
}

// formaDelPedido devuelve los nombres de los campos del mensaje, para poder
// afirmar cuáles NO existen.
func formaDelPedido() string {
	sobre, err := wrap(KindCanaryRequest, canaryRequestMsg{Port: 1, Nonce: make([]byte, domain.CanaryNonceSize)})
	if err != nil {
		return ""
	}
	return string(sobre.Payload)
}

func TestElInformeLlegaConElRemitenteDeLaConexion(t *testing.T) {
	b := nuevoBanco(t, ipUno)
	uno := b.enSala(t, ipUno)

	err := uno.SendCanaryReport(ctx(), domain.CanaryReport{
		Port: 51023,
		TCP:  domain.ProbeSilent,
		UDP:  domain.ProbeAnswered,
	})
	if err != nil {
		t.Fatal(err)
	}

	rep := espera(t, b.host.CanaryReports(), "el informe del canario")
	switch {
	case rep.From != ipUno:
		t.Errorf("remitente = %s, y tiene que salir de la conexión (%s)", rep.From, ipUno)
	case rep.Port != 51023:
		t.Errorf("puerto = %d", rep.Port)
	case rep.TCP != domain.ProbeSilent:
		t.Errorf("tcp = %v", rep.TCP)
	case rep.UDP != domain.ProbeAnswered:
		t.Errorf("udp = %v", rep.UDP)
	}
}

// Un miembro no puede informar en nombre de otro, por lo mismo que arriba: el
// remitente sale de la conexión y en el mensaje no hay dónde ponerlo.
func TestUnMiembroNoPuedeInformarPorOtro(t *testing.T) {
	b := nuevoBanco(t, ipUno, ipDos)
	uno := b.enSala(t, ipUno)
	b.enSala(t, ipDos)

	if err := uno.SendCanaryReport(ctx(), domain.CanaryReport{
		Port: 51023, TCP: domain.ProbeAnswered, UDP: domain.ProbeAnswered,
	}); err != nil {
		t.Fatal(err)
	}

	rep := espera(t, b.host.CanaryReports(), "el informe")
	if rep.From == ipDos {
		t.Fatal("el informe de uno llegó atribuido a otro")
	}
	if rep.From != ipUno {
		t.Fatalf("remitente = %s", rep.From)
	}
}

// Un número del largo equivocado no se atiende. Por UDP el número es lo único
// que distingue el eco del canario de un paquete suelto, así que uno cortado
// haría que el invitado midiera contra algo que no ata nada.
func TestUnPedidoConNumeroMalNoSeAtiende(t *testing.T) {
	b := nuevoBanco(t, ipUno)
	uno := b.enSala(t, ipUno)

	sobre, err := wrap(KindCanaryRequest, canaryRequestMsg{Port: 51023, Nonce: []byte("corto")})
	if err != nil {
		t.Fatal(err)
	}
	srv := mustHost(t, b)
	pc, _ := srv.peer(ipUno)
	if err := pc.write(sobre); err != nil {
		t.Fatal(err)
	}

	nadaEn(t, uno.CanaryRequests(), "un pedido con el número cortado")
}

// Un número en ceros lo puede adivinar cualquiera, así que no ata nada.
func TestUnPedidoConNumeroEnCerosNoSeAtiende(t *testing.T) {
	b := nuevoBanco(t, ipUno)
	uno := b.enSala(t, ipUno)

	sobre, err := wrap(KindCanaryRequest, canaryRequestMsg{
		Port: 51023, Nonce: make([]byte, domain.CanaryNonceSize),
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := mustHost(t, b)
	pc, _ := srv.peer(ipUno)
	if err := pc.write(sobre); err != nil {
		t.Fatal(err)
	}

	nadaEn(t, uno.CanaryRequests(), "un pedido con el número en ceros")
}

// El host no toma un pedido de canario de un miembro. Aceptarlo dejaría que un
// cliente modificado le hiciera abrir sondeos a la máquina donde se abren los
// puertos.
func TestUnHostNoTomaPedidosDeCanarioDeNadie(t *testing.T) {
	b := nuevoBanco(t, ipUno)

	conn, err := b.red.desde(ipUno)(ctx(), netip.AddrPortFrom(ipHost, domain.ControlPort))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	sobre, _ := wrap(KindCanaryRequest, canaryRequestMsg{Port: 51023, Nonce: nonceBytes()})
	if err := wire.NewWriter(conn, MaxMessage).Write(sobre); err != nil {
		t.Fatal(err)
	}
	nadaEn(t, b.host.CanaryRequests(), "un pedido de canario que le mandaron al host")
}

// Un invitado no toma un informe: no tiene canario que comprobar, y aceptarlo
// sería aceptar mensajes de la sala en la máquina que no escucha nada.
func TestUnInvitadoNoTomaInformesDeCanario(t *testing.T) {
	b := nuevoBanco(t, ipUno)
	uno := b.enSala(t, ipUno)

	sobre, _ := wrap(KindCanaryReport, canaryReportMsg{Port: 51023, TCP: "answered", UDP: "answered"})
	srv := mustHost(t, b)
	pc, _ := srv.peer(ipUno)
	if err := pc.write(sobre); err != nil {
		t.Fatal(err)
	}

	nadaEn(t, uno.CanaryRequests(), "un informe metido por la conexión del host")
}

// Difundir el pedido a toda la sala abriría tantos sondeos como miembros contra
// un canario que espera uno.
func TestElPedidoNoSeDifundeALaSala(t *testing.T) {
	b := nuevoBanco(t, ipUno)
	b.enSala(t, ipUno)

	if err := b.host.RequestCanary(ctx(), netip.Addr{}, pedido(51023)); err == nil {
		t.Fatal("se aceptó un pedido sin destinatario")
	}
}

// Un resultado que no se sabe leer cae en "no se pudo preguntar" y JAMÁS en
// "silencio": lo desconocido tiene que restar confianza, no darla.
func TestUnResultadoDesconocidoNoSeLeeComoSilencio(t *testing.T) {
	if got := probeOutcomeFrom("vaya-usted-a-saber"); got != domain.ProbeFailed {
		t.Fatalf("resultado = %v, se esperaba ProbeFailed", got)
	}
	if got := probeOutcomeFrom(""); got != domain.ProbeFailed {
		t.Fatalf("el vacío llegó como %v", got)
	}
	// Y los cuatro que sí se conocen viajan en las dos direcciones.
	for _, o := range domain.AllProbeOutcomes() {
		nombre, ok := probeOutcomeName(o)
		if !ok {
			t.Errorf("el resultado %v no tiene nombre en el cable", o)
			continue
		}
		if probeOutcomeFrom(nombre) != o {
			t.Errorf("%v no vuelve igual: %q", o, nombre)
		}
	}
}
