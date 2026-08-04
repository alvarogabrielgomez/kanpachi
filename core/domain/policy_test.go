package domain

import (
	"errors"
	"net/netip"
	"testing"
)

var (
	ipLocal = netip.MustParseAddr("100.87.3.1")
	ipUno   = netip.MustParseAddr("100.87.3.2")
	ipDos   = netip.MustParseAddr("100.87.3.3")
)

func perfilEstrella() GameProfile {
	p, err := NewGameProfile(perfilValido())
	if err != nil {
		panic(err)
	}
	return p
}

func perfilMalla() GameProfile {
	base := perfilValido()
	base.ID = "juego-de-malla"
	base.ClientPorts = []PortRange{{Proto: ProtoBoth, From: 7777, To: 7777}}
	p, err := NewGameProfile(base)
	if err != nil {
		panic(err)
	}
	return p
}

// TestSinMiembrosNoHayReglas es la invariante 3 dicha al revés.
//
// RemoteAddresses son SIEMPRE los miembros presentes y no existe forma de
// expresar "cualquiera", así que cero miembros no puede producir una regla con
// el campo vacío: tiene que producir cero reglas. Una regla sin destinatarios
// en el Firewall de Windows abre el puerto a todo el mundo.
func TestSinMiembrosNoHayReglas(t *testing.T) {
	rs, err := BuildRuleSet(perfilEstrella(), RoleHost, ipLocal, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !rs.IsEmpty() {
		t.Fatalf("una sala vacía produjo %d reglas: %+v", len(rs.Rules), rs.Rules)
	}
}

// TestSoloYoEnLaSalaNoAbreNada: el motor reporta la propia máquina como un
// peer más, y sin descartarla una sala de una persona abriría puertos hacia sí
// misma y parecería que el producto funciona.
func TestSoloYoEnLaSalaNoAbreNada(t *testing.T) {
	rs, err := BuildRuleSet(perfilEstrella(), RoleHost, ipLocal, []netip.Addr{ipLocal})
	if err != nil {
		t.Fatal(err)
	}
	if !rs.IsEmpty() {
		t.Fatalf("estar solo produjo reglas: %+v", rs.Rules)
	}
}

func TestSinJuegoActivoNoHayReglas(t *testing.T) {
	rs, err := BuildRuleSet(GameProfile{}, RoleHost, ipLocal, []netip.Addr{ipUno})
	if err != nil {
		t.Fatal(err)
	}
	if !rs.IsEmpty() {
		t.Fatalf("una sala sin juego produjo reglas: %+v", rs.Rules)
	}
}

// TestElInvitadoDeUnJuegoDeEstrellaNoAbreNada es la topología del campo
// client_ports, que es el más delicado del esquema. Vacío significa que nadie
// alcanza la máquina de nadie.
func TestElInvitadoDeUnJuegoDeEstrellaNoAbreNada(t *testing.T) {
	rs, err := BuildRuleSet(perfilEstrella(), RoleGuest, ipLocal, []netip.Addr{ipUno, ipDos})
	if err != nil {
		t.Fatal(err)
	}
	if !rs.IsEmpty() {
		t.Fatalf("un invitado en estrella abrió %d reglas: %+v", len(rs.Rules), rs.Rules)
	}
}

func TestElHostAbreSusPuertosHaciaLosPresentes(t *testing.T) {
	rs, err := BuildRuleSet(perfilEstrella(), RoleHost, ipLocal, []netip.Addr{ipDos, ipUno, ipUno})
	if err != nil {
		t.Fatal(err)
	}
	if len(rs.Rules) != 1 {
		t.Fatalf("se esperaba una regla, salieron %d: %+v", len(rs.Rules), rs.Rules)
	}
	r := rs.Rules[0]
	if r.Proto != ProtoUDP || r.From != 16261 || r.To != 16262 {
		t.Errorf("la regla no coincide con el perfil: %+v", r)
	}
	if r.Local != ipLocal {
		t.Errorf("la regla no está anclada al adaptador: %v", r.Local)
	}
	if len(r.Remote) != 2 || r.Remote[0] != ipUno || r.Remote[1] != ipDos {
		t.Errorf("los destinatarios salieron mal o sin ordenar: %v", r.Remote)
	}
}

// TestMallaExpandeLosDosProtocolos: una regla del Firewall de Windows tiene un
// protocolo y solo uno, así que "both" tiene que salir del dominio ya partido.
func TestMallaExpandeLosDosProtocolos(t *testing.T) {
	rs, err := BuildRuleSet(perfilMalla(), RoleGuest, ipLocal, []netip.Addr{ipUno})
	if err != nil {
		t.Fatal(err)
	}
	if len(rs.Rules) != 2 {
		t.Fatalf("both tenía que dar dos reglas, dio %d", len(rs.Rules))
	}
	for _, r := range rs.Rules {
		if r.Proto == ProtoBoth {
			t.Errorf("una regla salió con protocolo both: %+v", r)
		}
	}
}

// TestElConjuntoEsUnaFunciónPuraDeLaEntrada protege el diff declarativo: si el
// orden cambiara entre dos cálculos iguales, netfw vería cambios donde no los
// hay y reescribiría el firewall en cada latido.
func TestElConjuntoEsUnaFunciónPuraDeLaEntrada(t *testing.T) {
	a, err := BuildRuleSet(perfilMalla(), RoleGuest, ipLocal, []netip.Addr{ipUno, ipDos})
	if err != nil {
		t.Fatal(err)
	}
	b, err := BuildRuleSet(perfilMalla(), RoleGuest, ipLocal, []netip.Addr{ipDos, ipUno})
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Rules) != len(b.Rules) {
		t.Fatalf("dos cálculos con los mismos miembros dieron %d y %d reglas", len(a.Rules), len(b.Rules))
	}
	for i := range a.Rules {
		if a.Rules[i].Name != b.Rules[i].Name {
			t.Fatalf("el orden depende de cómo llegaron los miembros: %q contra %q",
				a.Rules[i].Name, b.Rules[i].Name)
		}
	}
}

// TestLaPolíticaVuelveAComprobarLosPuertosProhibidos.
//
// No debería poder pasar, porque un GameProfile solo existe validado. Se
// comprueba igual porque este es el sitio donde un puerto se abre de verdad, y
// una invariante de seguridad tiene que vivir también donde ocurre el acto.
// Aquí se construye el perfil por la puerta de atrás para simular el bug.
func TestLaPolíticaVuelveAComprobarLosPuertosProhibidos(t *testing.T) {
	roto := GameProfile{
		ID:        "perfil-imposible",
		Name:      "Imposible",
		Origin:    OriginMine,
		HostPorts: []PortRange{{Proto: ProtoTCP, From: 440, To: 450}},
		Connect:   ConnectHint{Kind: ConnectDirectIP},
	}
	_, err := BuildRuleSet(roto, RoleHost, ipLocal, []netip.Addr{ipUno})
	if !errors.Is(err, ErrRuleForbiddenPort) {
		t.Fatalf("la política dejó pasar el 445 dentro de 440-450: %v", err)
	}
}

func TestSinIPDeAdaptadorNoSeCalculaNada(t *testing.T) {
	if _, err := BuildRuleSet(perfilEstrella(), RoleHost, netip.Addr{}, []netip.Addr{ipUno}); err == nil {
		t.Fatal("se calcularon reglas sin IP de adaptador a la que anclarlas")
	}
}

func TestMemberIPsDescartaLaPropia(t *testing.T) {
	peers := []Peer{
		{VirtualIP: ipLocal, Self: true},
		{VirtualIP: ipUno},
		{VirtualIP: netip.Addr{}},
	}
	got := MemberIPs(peers)
	if len(got) != 1 || got[0] != ipUno {
		t.Fatalf("MemberIPs = %v", got)
	}
}

// TestElHuecoDelCanalNoDependeDeQueHayaJuego.
//
// Es la razón por la que ControlRules vive aparte de BuildRuleSet: aquella
// devuelve vacío sin juego activo, que es el estado normal de una sala recién
// creada, así que el host se quedaría escuchando detrás de su propio deny-all.
func TestElHuecoDelCanalNoDependeDeQueHayaJuego(t *testing.T) {
	juego, err := BuildRuleSet(GameProfile{}, RoleHost, ipLocal, []netip.Addr{ipUno})
	if err != nil {
		t.Fatal(err)
	}
	if !juego.IsEmpty() {
		t.Fatalf("sin juego activo apareció una regla de juego: %+v", juego.Rules)
	}

	canal, err := ControlRules(RoleHost, RendezvousHostAddress, ipLocal, []netip.Addr{ipUno})
	if err != nil {
		t.Fatal(err)
	}
	if len(canal) != 2 {
		t.Fatalf("reglas del canal = %d, se esperaban la puerta y la sala: %+v", len(canal), canal)
	}
	for _, r := range canal {
		if r.From != ControlPort || r.To != ControlPort || r.Proto != ProtoTCP {
			t.Errorf("la regla del canal no es el puerto del canal: %+v", r)
		}
		if !r.IsControl() {
			t.Errorf("la regla del canal no se reconoce como tal: %q", r.Name)
		}
	}
}

// TestLaPuertaAbreAlVestíbuloYLaSalaSoloALosPresentes: son dos alcances porque
// son dos conversaciones con dos modelos de confianza distintos.
func TestLaPuertaAbreAlVestíbuloYLaSalaSoloALosPresentes(t *testing.T) {
	canal, err := ControlRules(RoleHost, RendezvousHostAddress, ipLocal, []netip.Addr{ipUno, ipDos})
	if err != nil {
		t.Fatal(err)
	}

	puerta, sala := canal[0], canal[1]
	if puerta.Local != RendezvousHostAddress || len(puerta.Nets) != 1 || puerta.Nets[0] != RendezvousSubnet {
		t.Fatalf("la puerta no está anclada al vestíbulo: %+v", puerta)
	}
	if len(puerta.Remote) != 0 {
		t.Errorf("la puerta lista direcciones, y quien toca todavía no tiene ninguna conocida: %v", puerta.Remote)
	}
	if sala.Local != ipLocal || len(sala.Nets) != 0 {
		t.Fatalf("la sala no está anclada a la IP del host, o abre un prefijo: %+v", sala)
	}
	if len(sala.Remote) != 2 || sala.Remote[0] != ipUno || sala.Remote[1] != ipDos {
		t.Errorf("la sala no está acotada a los presentes: %v", sala.Remote)
	}
}

// TestConLaSalaVacíaSoloQuedaLaPuerta.
//
// Sin nadie a quien nombrar no hay regla de sala posible, igual que en las
// reglas de juego: una regla sin destinatarios abre el puerto a todo el mundo.
// La puerta sí queda, porque justamente existe para quien todavía no llegó.
func TestConLaSalaVacíaSoloQuedaLaPuerta(t *testing.T) {
	canal, err := ControlRules(RoleHost, RendezvousHostAddress, ipLocal, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(canal) != 1 || canal[0].Local != RendezvousHostAddress {
		t.Fatalf("con la sala vacía las reglas del canal son %+v", canal)
	}
}

// TestUnInvitadoNoAbreElCanal: no escucha, así que su deny-all queda intacto.
func TestUnInvitadoNoAbreElCanal(t *testing.T) {
	canal, err := ControlRules(RoleGuest, RendezvousHostAddress, ipLocal, []netip.Addr{ipUno})
	if err != nil {
		t.Fatal(err)
	}
	if len(canal) != 0 {
		t.Fatalf("un invitado abrió %d reglas de canal: %+v", len(canal), canal)
	}
}

// TestElPrefijoDeLaPuertaNoPuedeSerOtro.
//
// Nets es el único campo del tipo que acepta un prefijo, y sin este guardián
// sería la forma de escribir "cualquiera" que el resto de FirewallRule se
// esfuerza en no tener.
func TestElPrefijoDeLaPuertaNoPuedeSerOtro(t *testing.T) {
	_, err := ControlRules(RoleHost, netip.MustParseAddr("192.168.1.10"), ipLocal, []netip.Addr{ipUno})
	if !errors.Is(err, ErrRuleWideOpen) {
		t.Fatalf("anclar la puerta fuera del vestíbulo dio %v", err)
	}
}

// TestElCanalNoPuedeCaerEnUnPuertoProhibido: el puerto es una constante, así
// que esto vigila que nadie la mueva a 445 en un refactor.
func TestElCanalNoPuedeCaerEnUnPuertoProhibido(t *testing.T) {
	rango := PortRange{Proto: ProtoTCP, From: ControlPort, To: ControlPort}
	if bad, hit := rango.hitsForbidden(); hit {
		t.Fatalf("el puerto del canal es %d, que está prohibido", bad)
	}
}

// TestLasReglasDelCanalNoSeMezclanConLasDelJuego: el conjunto que se aplica
// lleva las dos, y hace falta poder afirmar que la sala no abrió puertos de
// juego sin que el hueco del canal lo tape.
func TestLasReglasDelCanalNoSeMezclanConLasDelJuego(t *testing.T) {
	rs, err := BuildRuleSet(perfilEstrella(), RoleHost, ipLocal, []netip.Addr{ipUno})
	if err != nil {
		t.Fatal(err)
	}
	canal, err := ControlRules(RoleHost, RendezvousHostAddress, ipLocal, []netip.Addr{ipUno})
	if err != nil {
		t.Fatal(err)
	}
	rs.Add(canal...)

	if len(rs.GameRules()) != 1 {
		t.Fatalf("reglas de juego = %d: %+v", len(rs.GameRules()), rs.GameRules())
	}
	for i := 1; i < len(rs.Rules); i++ {
		if rs.Rules[i-1].Name > rs.Rules[i].Name {
			t.Fatalf("el conjunto quedó sin ordenar, y el firewall lo reescribiría en cada latido: %+v", rs.Rules)
		}
	}
}

// ---------------------------------------------------------------------------
// RuleSet.Allows, que es lo que evita que el canario se ligue a un puerto que
// la propia Kanpachi abrió
// ---------------------------------------------------------------------------

func TestUnPuertoDelJuegoActivoCuentaComoPermitido(t *testing.T) {
	rs, err := BuildRuleSet(perfilEstrella(), RoleHost, ipLocal, []netip.Addr{ipUno})
	if err != nil {
		t.Fatalf("BuildRuleSet: %v", err)
	}

	// El perfil de estrella pide 16261-16262. Los bordes de adentro tienen que
	// contar, y los de afuera no: un rango mal leído por uno deja al canario
	// eligiendo justo el puerto que el juego abrió.
	for _, p := range []uint16{16261, 16262} {
		if !rs.Allows(p) {
			t.Errorf("Allows(%d) = false, y el juego activo lo tiene abierto", p)
		}
	}
	for _, p := range []uint16{16260, 16263} {
		if rs.Allows(p) {
			t.Errorf("Allows(%d) = true, y está fuera del rango del perfil", p)
		}
	}
}

// El conjunto vacío es el caso NORMAL: una sala sin juego elegido. Si acá
// dijera que sí, el canario no encontraría ningún puerto aceptable y no se
// abriría nunca.
func TestElConjuntoVacíoNoPermiteNada(t *testing.T) {
	var rs RuleSet
	for _, p := range []uint16{0, 1, 49152, 57623, 65535} {
		if rs.Allows(p) {
			t.Errorf("Allows(%d) = true sobre un conjunto sin reglas", p)
		}
	}
}

// El hueco del canal de la sala también cuenta, y este test existe porque ese
// puerto es el que más caro sale pisar: el canario ligándose ahí competiría con
// el oyente de la sala, y su respuesta se leería como una fuga.
func TestElHuecoDelCanalDeLaSalaCuentaComoPermitido(t *testing.T) {
	canal, err := ControlRules(RoleHost, RendezvousHostAddress, ipLocal, []netip.Addr{ipUno})
	if err != nil {
		t.Fatalf("ControlRules: %v", err)
	}
	var rs RuleSet
	rs.Add(canal...)

	if !rs.Allows(ControlPort) {
		t.Fatalf("Allows(%d) = false, y es el puerto del canal de la sala", ControlPort)
	}
}
