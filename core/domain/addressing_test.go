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

// TestElVestíbuloNoSeEntregaComoSubredDeSala.
//
// Si coincidieran, entrar a la sala cortaría la conexión que se está usando
// para pedir la credencial. Se ocupa todo el espacio compartido menos ese /24,
// para que sea el único candidato posible.
func TestElVestíbuloNoSeEntregaComoSubredDeSala(t *testing.T) {
	// 100.64.0.0/10 partido en trozos que cubren todo salvo 100.127.255.0/24.
	local := prefijos(
		"100.64.0.0/11", "100.96.0.0/12", "100.112.0.0/13", "100.120.0.0/14",
		"100.124.0.0/15", "100.126.0.0/16", "100.127.0.0/17", "100.127.128.0/18",
		"100.127.192.0/19", "100.127.224.0/20", "100.127.240.0/21", "100.127.248.0/22",
		"100.127.252.0/23", "100.127.254.0/24",
	)
	// Con el espacio compartido ocupado el plan se va a la reserva, así que lo
	// que se prueba de verdad es pickSubnet sobre el espacio compartido.
	sub, err := pickSubnet(SharedSpace, local, lectorFijo(0xff))
	if err == nil {
		t.Fatalf("se entregó %s, que solo puede ser el /24 del vestíbulo", sub)
	}
	if !errors.Is(err, ErrNoSubnet) {
		t.Fatalf("error inesperado: %v", err)
	}
}

// TestSeAvisaSiLaCasaPisaElVestíbulo. No se puede esquivar, se puede decir.
func TestSeAvisaSiLaCasaPisaElVestíbulo(t *testing.T) {
	plan, err := PlanAddresses(prefijos("192.168.1.0/24", "100.127.255.0/24"), lectorFijo(0))
	if err != nil {
		t.Fatal(err)
	}
	if !plan.LobbyConflict.IsValid() {
		t.Fatal("no se avisó del conflicto con el vestíbulo")
	}
	// Y la sala igual se crea: crear no necesita el vestíbulo del otro.
	if !plan.Subnet.IsValid() {
		t.Fatal("el conflicto del vestíbulo impidió elegir subred de sala")
	}
}

func TestSinConflictoNoSeAvisaDeNada(t *testing.T) {
	plan, err := PlanAddresses(prefijos("192.168.1.0/24"), lectorFijo(0))
	if err != nil {
		t.Fatal(err)
	}
	if plan.LobbyConflict.IsValid() {
		t.Fatalf("aviso falso: %s", plan.LobbyConflict)
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
