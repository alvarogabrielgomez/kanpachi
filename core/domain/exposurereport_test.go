package domain

import (
	"net/netip"
	"testing"
	"time"
)

func ahoraDePrueba() time.Time { return time.Date(2026, 8, 4, 20, 0, 0, 0, time.UTC) }

func reglaExpuesta(nombre string, puerto uint16) FirewallRule {
	return FirewallRule{
		Name:   nombre,
		Proto:  ProtoUDP,
		From:   puerto,
		To:     puerto,
		Local:  netip.MustParseAddr("100.64.1.1"),
		Remote: []netip.Addr{netip.MustParseAddr("100.64.1.5")},
	}
}

func puestas(nombres ...string) []AppliedRule {
	out := make([]AppliedRule, 0, len(nombres))
	for _, n := range nombres {
		out = append(out, AppliedRule{Name: n, Layer: LayerFirewallRules, Enabled: true})
	}
	return out
}

func TestElInformeDiceLoMedidoYNoLoPedido(t *testing.T) {
	// Pedir un puerto y que esté abierto son dos cosas, y esta pantalla existe
	// justamente para no confundirlas. Una regla pedida que el sistema no tiene
	// significa que alguien no va a poder entrar.
	var rs RuleSet
	rs.Rules = append(rs.Rules, reglaExpuesta("a", 16261), reglaExpuesta("b", 16262))

	e := Enforcement{Rules: puestas("a"), Gate: GatePresent}
	r := NewExposureReport(rs, e, true, ahoraDePrueba())

	if len(r.Ports) != 2 {
		t.Fatalf("se informaron %d puertos y se pidieron dos", len(r.Ports))
	}
	if !r.Ports[0].Applied {
		t.Error("la regla que el sistema tiene puesta se informó como no aplicada")
	}
	if r.Ports[1].Applied {
		t.Error("la regla que el sistema NO tiene se informó como aplicada, así que la " +
			"pantalla diría que el puerto está abierto teniéndolo cerrado")
	}
}

func TestUnaReglaQueNadiePidioSeInforma(t *testing.T) {
	// Kanpachi es el único que escribe en su grupo, así que una de más significa
	// que alguien lo tocó, o que quedó de una salida sucia.
	var rs RuleSet
	rs.Rules = append(rs.Rules, reglaExpuesta("a", 16261))

	e := Enforcement{Rules: puestas("a", "sobrante"), Gate: GatePresent}
	r := NewExposureReport(rs, e, true, ahoraDePrueba())

	if len(r.Unexpected) != 1 || r.Unexpected[0] != "sobrante" {
		t.Errorf("las reglas de más son %v", r.Unexpected)
	}
}

func TestUnInformeCiegoNoEnseñaLaListaBuena(t *testing.T) {
	// Es la doctrina de AlertAuditFailed: lo que no se pudo medir se dice, y no
	// se rellena con lo último que se supo. Una lista vieja pintada de verde es
	// peor que una pantalla que admite no saber.
	var rs RuleSet
	rs.Rules = append(rs.Rules, reglaExpuesta("a", 16261))

	e := Enforcement{Rules: puestas("a"), Gate: GatePresent}
	r := NewExposureReport(rs, e, true, time.Time{})

	if !r.Blind() {
		t.Fatal("un informe sin hora de medición no se declaró ciego")
	}
	if len(r.Ports) != 0 {
		t.Errorf("el informe ciego enseñó %d puertos", len(r.Ports))
	}
	if r.Gate != GateUnknown {
		t.Errorf("el informe ciego dice que la compuerta está %v, y no se comprobó", r.Gate)
	}
}

func TestElInformeVacioNoDiceQueNoHayNadaAbierto(t *testing.T) {
	// El cero de la estructura tiene que leerse como ceguera y jamás como
	// tranquilidad. Es el mismo motivo por el que GateUnknown es el valor cero.
	var r ExposureReport
	if !r.Blind() {
		t.Error("el cero del informe se lee como una medición buena")
	}
	if r.Gate != GateUnknown {
		t.Error("el cero del informe da la compuerta por comprobada")
	}
}

func TestElHuecoDelCanalSeDistingueDeUnPuertoDeJuego(t *testing.T) {
	// Uno lo pidió el juego que el usuario eligió y el otro lo abre Kanpachi por
	// su cuenta. Enseñarlos iguales haría que el usuario buscara en su perfil un
	// puerto que su perfil no pide.
	var rs RuleSet
	rs.Rules = append(rs.Rules, reglaExpuesta("kanpachi-udp-16261", 16261))

	canal, err := ControlRules(
		RoleHost,
		RendezvousSubnet.Addr().Next(),
		netip.MustParseAddr("100.64.1.1"),
		[]netip.Addr{netip.MustParseAddr("100.64.1.5")},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(canal) == 0 {
		t.Fatal("este test no prueba nada: no salió ninguna regla de canal")
	}
	rs.Add(canal...)

	r := NewExposureReport(rs, Enforcement{Gate: GatePresent}, true, ahoraDePrueba())

	juego, control := 0, 0
	for _, p := range r.Ports {
		if p.Control {
			control++
		} else {
			juego++
		}
	}
	if juego != 1 || control != len(canal) {
		t.Errorf("se distinguieron %d de juego y %d de canal, y son 1 y %d",
			juego, control, len(canal))
	}
}

func TestElOrdenDelInformeEsEstable(t *testing.T) {
	// Dos lecturas seguidas con el mismo estado tienen que producir la misma
	// pantalla. Sin esto la lista parpadea sola.
	var rs RuleSet
	rs.Rules = append(rs.Rules,
		reglaExpuesta("z", 3), reglaExpuesta("a", 1), reglaExpuesta("m", 2))

	r := NewExposureReport(rs, Enforcement{Gate: GatePresent}, true, ahoraDePrueba())
	for i := 1; i < len(r.Ports); i++ {
		if r.Ports[i-1].Name > r.Ports[i].Name {
			t.Fatalf("el orden no es estable: %v", r.Ports)
		}
	}
}

func TestElAlcanceRemotoViajaEntero(t *testing.T) {
	// Que esté vacío JAMÁS significa cualquiera, y por eso la pantalla lo tiene
	// que poder decir: un puerto abierto sin decir para quién es la mitad de la
	// información que importa.
	var rs RuleSet
	rs.Rules = append(rs.Rules, reglaExpuesta("a", 16261))

	r := NewExposureReport(rs, Enforcement{Gate: GatePresent}, true, ahoraDePrueba())
	if len(r.Ports[0].Members) != 1 {
		t.Fatalf("los miembros llegaron como %v", r.Ports[0].Members)
	}
	if r.Ports[0].Members[0] != netip.MustParseAddr("100.64.1.5") {
		t.Errorf("el miembro es %v", r.Ports[0].Members[0])
	}
}
