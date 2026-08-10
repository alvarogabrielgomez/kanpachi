package wfp

import (
	"net/netip"
	"testing"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/daemon/adapter/firewall/gate"
)

// Los mismos ayudantes que en el paquete del modelo. Se repiten en vez de
// exportarse: un ayudante de test exportado es API pública que alguien acaba
// usando en produccion.
func addr(s string) netip.Addr  { return netip.MustParseAddr(s) }
func pfx(s string) netip.Prefix { return netip.MustParsePrefix(s) }

func TestAnAddressBecomesTheNumberItReads(t *testing.T) {
	// El orden de bytes es el error más silencioso de toda esta capa: al revés
	// sale un filtro perfectamente válido que casa con OTRA red. Nada falla, y la
	// pantalla dice que el puerto está abierto para el miembro correcto.
	cases := []struct {
		texto  string
		quiero uint32
	}{
		{"0.0.0.0", 0x00000000},
		{"1.2.3.4", 0x01020304},
		{"100.64.1.1", 0x64400101},
		{"255.255.255.255", 0xFFFFFFFF},
	}
	for _, c := range cases {
		got, err := v4(addr(c.texto))
		if err != nil {
			t.Fatalf("%s: %v", c.texto, err)
		}
		if got != c.quiero {
			t.Errorf("%s dio 0x%08X y tenía que dar 0x%08X", c.texto, got, c.quiero)
		}
	}
}

func TestAnIPv6AddressIsRefused(t *testing.T) {
	if _, err := v4(addr("fe80::1")); err == nil {
		t.Fatal("se aceptó una dirección IPv6 donde la compuerta compara IPv4")
	}
}

func TestAPrefixBecomesItsMask(t *testing.T) {
	// Un corrimiento de un bit multiplica por dos el alcance de un bloqueo, y el
	// código se lee igual de bien con el error puesto.
	cases := []struct {
		texto string
		red   uint32
		masc  uint32
	}{
		{"100.64.1.0/24", 0x64400100, 0xFFFFFF00},
		{"100.64.0.0/10", 0x64400000, 0xFFC00000},
		{"10.99.5.7/32", 0x0A630507, 0xFFFFFFFF},
		// Sin enmascarar: los bits de host se caen, que es lo que WFP espera.
		{"100.64.1.77/24", 0x64400100, 0xFFFFFF00},
	}
	for _, c := range cases {
		red, masc, err := v4Mask(pfx(c.texto))
		if err != nil {
			t.Fatalf("%s: %v", c.texto, err)
		}
		if red != c.red || masc != c.masc {
			t.Errorf("%s dio 0x%08X/0x%08X y tenía que dar 0x%08X/0x%08X",
				c.texto, red, masc, c.red, c.masc)
		}
	}
}

func TestAPrefixOfZeroBitsIsRefused(t *testing.T) {
	// `0.0.0.0/0` es un prefijo perfectamente válido y como alcance de un bloqueo
	// duro casa con TODA dirección local de la máquina: el usuario se queda sin
	// la entrada de su red de casa. El campo está puesto y el tipo es correcto,
	// así que no lo caza nada más que esto.
	if _, _, err := v4Mask(pfx("0.0.0.0/0")); err == nil {
		t.Fatal("se aceptó un prefijo de cero bits como alcance")
	}
}

func TestASinglePortIsEqualityAndARangeIsARange(t *testing.T) {
	// Un rango pedido como igualdad abriría solo el primer puerto, y un juego no
	// funciona a medias: no funciona.
	suelto, err := Expand(gate.Conditions{Iface: 1, LocalPortFrom: 16261, LocalPortTo: 16261})
	if err != nil {
		t.Fatal(err)
	}
	c := soloDe(t, suelto, FieldLocalPort)
	if c.Match != MatchEqual || c.Kind != ValueNum || c.Num != 16261 || c.Width != Width16 {
		t.Errorf("un puerto suelto salió como %+v", c)
	}

	ancho, err := Expand(gate.Conditions{Iface: 1, LocalPortFrom: 27000, LocalPortTo: 27100})
	if err != nil {
		t.Fatal(err)
	}
	c = soloDe(t, ancho, FieldLocalPort)
	if c.Match != MatchRange || c.Kind != ValuePortRange || c.From != 27000 || c.To != 27100 {
		t.Errorf("un rango salió como %+v", c)
	}
}

func TestEveryMemberIsItsOwnCondition(t *testing.T) {
	// WFP une con O las condiciones del MISMO campo, así que tres miembros son
	// tres condiciones y significan "cualquiera de estos". Una sola condición con
	// varios valores no existe, y creer que sí produciría un permiso que abre
	// solo para el primero.
	cs, err := Expand(gate.Conditions{
		Iface: 1,
		Remote: []netip.Addr{
			addr("100.64.1.5"), addr("100.64.1.6"), addr("100.64.1.7"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	remotas := 0
	for _, c := range cs {
		if c.Field == FieldRemoteAddress {
			remotas++
		}
	}
	if remotas != 3 {
		t.Errorf("tres miembros dieron %d condiciones remotas", remotas)
	}
}

func TestTheAdapterGoesAsSixtyFourBits(t *testing.T) {
	// De esto depende que la capa de Windows lo pase POR PUNTERO. Con el ancho
	// mal, la condición compara basura y el filtro no aplica a nada, en silencio,
	// que es justo el modo de fallo que la compuerta entera existe para no tener.
	cs, err := Expand(gate.Conditions{Iface: 0x47008000000000})
	if err != nil {
		t.Fatal(err)
	}
	c := soloDe(t, cs, FieldLocalInterface)
	if c.Kind != ValueNum || c.Width != Width64 || c.Num != 0x47008000000000 {
		t.Errorf("el adaptador salió como %+v", c)
	}
}

func TestConditionsWithNothingInThemAreRefused(t *testing.T) {
	// Un filtro sin condiciones aplica a TODOS los adaptadores de la máquina.
	if _, err := Expand(gate.Conditions{}); err == nil {
		t.Fatal("se aceptaron unas condiciones vacías")
	}
}

func TestBothProtocolIsRefused(t *testing.T) {
	// [domain.BuildRuleSet] lo expande en dos reglas antes de llegar acá. Si
	// llegara, traducirlo a un número inventado abriría un protocolo que nadie
	// pidió.
	if _, err := Expand(gate.Conditions{Iface: 1, Proto: domain.ProtoBoth}); err == nil {
		t.Fatal("se aceptó el protocolo both")
	}
}

func TestTheProtocolNumbersAreTheOnesInThePacket(t *testing.T) {
	for _, c := range []struct {
		proto  domain.Proto
		quiero uint8
	}{{domain.ProtoTCP, 6}, {domain.ProtoUDP, 17}} {
		got, err := ipProto(c.proto)
		if err != nil {
			t.Fatal(err)
		}
		if got != c.quiero {
			t.Errorf("%v dio %d y es %d", c.proto, got, c.quiero)
		}
	}
}

// soloDe devuelve la única condición de un campo, y falla si hay otra cuenta.
func soloDe(t *testing.T, cs []Condition, f Field) Condition {
	t.Helper()

	var out []Condition
	for _, c := range cs {
		if c.Field == f {
			out = append(out, c)
		}
	}
	if len(out) != 1 {
		t.Fatalf("se esperaba una condición de %s y hay %d", f, len(out))
	}
	return out[0]
}
