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
