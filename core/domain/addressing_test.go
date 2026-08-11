package domain

import (
	"bytes"
	"errors"
	"net/netip"
	"strings"
	"testing"
)

// lectorFijo devuelve siempre el mismo byte. Sirve para fijar el punto de
// arranque del barrido y hacer el resultado predecible sin desactivar el
// camino real.
func lectorFijo(b byte) *bytes.Reader {
	return bytes.NewReader(bytes.Repeat([]byte{b}, 4096))
}

func prefijos(ss ...string) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(ss))
	for _, s := range ss {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}

// TestUnaCasaNormalVaAlEspacioCompartido: el caso más común, con la LAN en
// 192.168 y el router haciendo CGNAT del lado WAN, no aparece en la tabla del
// PC y no dispara nada.
func TestUnaCasaNormalVaAlEspacioCompartido(t *testing.T) {
	plan, err := PlanAddresses(prefijos("192.168.1.0/24", "169.254.0.0/16"), lectorFijo(0))
	if err != nil {
		t.Fatal(err)
	}
	if !SharedSpace.Overlaps(plan.Subnet) {
		t.Fatalf("la sala fue a %s, fuera del espacio compartido", plan.Subnet)
	}
	if plan.Subnet.Bits() != RoomPrefixBits {
		t.Errorf("la sala no es un /24: %s", plan.Subnet)
	}
}

// TestElISPQueRepartCGNATEnLaLANMandaLaSalaALaReserva es el conflicto que
// obliga a tener salida de emergencia.
//
// No hace falta que el /24 elegido choque: basta con que la máquina ya viva en
// 100.64.0.0/10, porque cualquier /24 de ahí compite con la ruta del router
// del usuario aunque hoy no se solapen.
func TestElISPQueRepartCGNATEnLaLANMandaLaSalaALaReserva(t *testing.T) {
	plan, err := PlanAddresses(prefijos("100.64.12.0/24"), lectorFijo(0))
	if err != nil {
		t.Fatal(err)
	}
	if !FallbackSpace.Overlaps(plan.Subnet) {
		t.Fatalf("con CGNAT en la LAN la sala fue a %s en vez de a %s", plan.Subnet, FallbackSpace)
	}
	if !strings.Contains(plan.Reason, "100.64.12.0/24") {
		t.Errorf("el motivo no dice qué prefijo forzó el cambio: %q", plan.Reason)
	}
}

// TestOtraVPNEnElEspacioCompartidoTambiénLoDispara: el otro caso real es el
// usuario que ya corre Tailscale, que usa el mismo rango y por la misma razón.
func TestOtraVPNEnElEspacioCompartidoTambiénLoDispara(t *testing.T) {
	plan, err := PlanAddresses(prefijos("192.168.0.0/24", "100.100.0.0/16"), lectorFijo(7))
	if err != nil {
		t.Fatal(err)
	}
	if !FallbackSpace.Overlaps(plan.Subnet) {
		t.Fatalf("la sala fue a %s teniendo otra VPN en el espacio compartido", plan.Subnet)
	}
}

// TestLaSalaNoPisaUnaRutaQueYaExiste dentro del propio espacio de reserva.
func TestLaSalaNoPisaUnaRutaQueYaExiste(t *testing.T) {
	// Se ocupa el espacio compartido para forzar la reserva, y dentro de la
	// reserva se ocupa justo el /24 en que arrancaría el barrido.
	local := prefijos("100.64.0.0/10", "10.99.0.0/24")
	plan, err := PlanAddresses(local, lectorFijo(0))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range local {
		if p.Overlaps(plan.Subnet) {
			t.Fatalf("la sala fue a %s, que pisa %s", plan.Subnet, p)
		}
	}
}

func TestSiNoQuedaNadaLibreSeDiceEnVezDeForzar(t *testing.T) {
	_, err := PlanAddresses(prefijos("100.64.0.0/10", "10.99.0.0/16"), lectorFijo(0))
	if !errors.Is(err, ErrNoSubnet) {
		t.Fatalf("con los dos espacios ocupados se esperaba ErrNoSubnet, salió %v", err)
	}
}

// TestElBarridoEsCircular: arrancar cerca del final y no encontrar nada
// significaría que el barrido no da la vuelta, y con la reserva casi llena
// devolvería "no hay sitio" habiendo uno.
func TestElBarridoEsCircular(t *testing.T) {
	// Todo 10.99 ocupado menos 10.99.0.0/24, y el arranque cae en la mitad
	// alta por el byte 0xff del lector.
	local := prefijos("100.64.0.0/10", "10.99.128.0/17", "10.99.1.0/24", "10.99.2.0/23",
		"10.99.4.0/22", "10.99.8.0/21", "10.99.16.0/20", "10.99.32.0/19", "10.99.64.0/18")
	plan, err := PlanAddresses(local, lectorFijo(0xff))
	if err != nil {
		t.Fatalf("el barrido no dio la vuelta: %v", err)
	}
	if plan.Subnet != netip.MustParsePrefix("10.99.0.0/24") {
		t.Fatalf("se esperaba el único /24 libre, salió %s", plan.Subnet)
	}
}

func TestElHostSeQuedaConLaUno(t *testing.T) {
	got := HostAddress(netip.MustParsePrefix("100.87.3.0/24"))
	if got != netip.MustParseAddr("100.87.3.1") {
		t.Fatalf("HostAddress = %s", got)
	}
}

// TestSeIgnoranLosPrefijosIPv6: la tabla de rutas trae de todo, y comparar un
// /64 con un /24 de IPv4 no significa nada.
func TestSeIgnoranLosPrefijosIPv6(t *testing.T) {
	if _, err := PlanAddresses(prefijos("fe80::/64", "2001:db8::/32"), lectorFijo(3)); err != nil {
		t.Fatalf("los prefijos IPv6 rompieron el plan: %v", err)
	}
}

// TestNingunaSalaCaeEnElEspacioDeVestíbulos.
//
// Si una sala cayera dentro, entrar cortaría la conexión que se está usando para
// pedir la credencial. Antes hacía falta saltar un /24 concreto dentro del
// espacio compartido; ahora lo garantiza que los vestíbulos vivan en otro
// espacio, y esto es lo que vigila que siga siendo cierto si alguien mueve
// cualquiera de los tres rangos.
func TestNingunaSalaCaeEnElEspacioDeVestíbulos(t *testing.T) {
	if LobbySpace.Overlaps(SharedSpace) {
		t.Fatalf("%v y %v se solapan, así que una sala puede caer en un vestíbulo",
			LobbySpace, SharedSpace)
	}
	if LobbySpace.Overlaps(FallbackSpace) {
		t.Fatalf("%v y %v se solapan, así que una sala puede caer en un vestíbulo",
			LobbySpace, FallbackSpace)
	}
}

// TestElEspacioDeVestíbulosNoEsCGNAT es la regresión del 2026-08-11.
//
// El vestíbulo vivía en 100.127.255.0/24, dentro del espacio que reparten los
// ISP con CGNAT y del que Tailscale saca las IP de sus nodos. Un invitado en
// Venezuela se quedó colgado esperando a que su adaptador tomara una dirección
// de ahí. Que no vuelva a estar en ese espacio es la mitad del arreglo.
func TestElEspacioDeVestíbulosNoEsCGNAT(t *testing.T) {
	if LobbySpace.Overlaps(SharedSpace) {
		t.Fatalf("los vestíbulos volvieron a %v, que es CGNAT", SharedSpace)
	}
}

// TestSeAvisaSiLaCasaPisaElVestíbulo. No se puede esquivar, se puede decir.
func TestSeAvisaSiLaCasaPisaElVestíbulo(t *testing.T) {
	lobby := netip.MustParsePrefix("198.19.7.0/24")
	if p := LobbyOverlap(prefijos("192.168.1.0/24", "198.19.7.0/24"), lobby); !p.IsValid() {
		t.Fatal("no se avisó del conflicto con el vestíbulo")
	}
	// Y una red que solo lo CONTIENE no cuenta, porque pierde por prefijo más
	// largo. Es el caso de Tailscale, que instala 100.64.0.0/10 en cada nodo: sin
	// esta distinción, toda máquina con Tailscale daría conflicto.
	if p := LobbyOverlap(prefijos("198.18.0.0/15"), lobby); p.IsValid() {
		t.Fatalf("se marcó conflicto con %v, que es más ancho que el vestíbulo", p)
	}
}

func TestSinConflictoNoSeAvisaDeNada(t *testing.T) {
	p := LobbyOverlap(prefijos("192.168.1.0/24"), netip.MustParsePrefix("198.19.7.0/24"))
	if p.IsValid() {
		t.Fatalf("aviso falso: %s", p)
	}
}

// TestElVestíbuloSeMueveConElCódigo es la otra mitad del arreglo del
// 2026-08-11, y la que de verdad da salida a un conflicto.
//
// Elegir bien el rango baja la probabilidad y no la anula: no hay forma de saber
// qué tiene la máquina de cada invitado. Lo que convierte "este producto no te
// sirve" en "que el host renueve el código" es que dos códigos distintos den
// vestíbulos distintos.
func TestElVestíbuloSeMueveConElCódigo(t *testing.T) {
	uno, err := ParseInviteID("ABCD-2345")
	if err != nil {
		t.Fatal(err)
	}
	otro, err := ParseInviteID("WXYZ-6789")
	if err != nil {
		t.Fatal(err)
	}
	a, b := DeriveRendezvous(uno).LobbySubnet(), DeriveRendezvous(otro).LobbySubnet()
	if a == b {
		t.Fatalf("los dos códigos dan el mismo vestíbulo (%v), así que renovar no mueve nada", a)
	}
	// Y los dos dentro del espacio, que es lo que la compuerta exige.
	for _, p := range []netip.Prefix{a, b} {
		if !LobbySpace.Contains(p.Addr()) {
			t.Fatalf("el vestíbulo %v cayó fuera de %v", p, LobbySpace)
		}
		if p.Bits() != RoomPrefixBits {
			t.Fatalf("el vestíbulo %v no es un /%d", p, RoomPrefixBits)
		}
	}
}

// TestElMismoCódigoDaSiempreElMismoVestíbulo, que es lo que permite que las dos
// máquinas se encuentren sin hablarse.
func TestElMismoCódigoDaSiempreElMismoVestíbulo(t *testing.T) {
	id, err := ParseInviteID("ABCD-2345")
	if err != nil {
		t.Fatal(err)
	}
	if a, b := DeriveRendezvous(id).LobbySubnet(), DeriveRendezvous(id).LobbySubnet(); a != b {
		t.Fatalf("la derivación no es estable: %v y %v", a, b)
	}
}

// TestUnEspacioMásChicoQueUnVeinticuatroNoRevienta: el desplazamiento sería
// negativo, que en Go es un panic.
func TestUnEspacioMásChicoQueUnVeinticuatroNoRevienta(t *testing.T) {
	if _, err := pickSubnet(netip.MustParsePrefix("10.0.0.0/25"), nil, lectorFijo(0)); err == nil {
		t.Fatal("se aceptó un espacio que no contiene ningún /24")
	}
	if _, err := pickSubnet(netip.MustParsePrefix("fd00::/32"), nil, lectorFijo(0)); err == nil {
		t.Fatal("se aceptó un espacio IPv6")
	}
}
