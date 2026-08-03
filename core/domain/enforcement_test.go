package domain

import (
	"net/netip"
	"reflect"
	"testing"
)

func reglaDeseada(nombre string) FirewallRule {
	return FirewallRule{
		Name:  nombre,
		Proto: ProtoUDP,
		From:  16261,
		To:    16261,
		Local: netip.MustParseAddr("100.64.1.1"),
	}
}

func deseadas(nombres ...string) RuleSet {
	var rs RuleSet
	for _, n := range nombres {
		rs.Add(reglaDeseada(n))
	}
	return rs
}

func puesta(nombre string) AppliedRule {
	return AppliedRule{Name: nombre, Layer: LayerFirewallRules, Enabled: true}
}

func TestLoQueFaltaYLoQueSobra(t *testing.T) {
	e := Enforcement{
		Rules: []AppliedRule{puesta("a"), puesta("c")},
		Gate:  GatePresent,
	}

	d := e.Diff(deseadas("a", "b"), true)

	if !reflect.DeepEqual(d.Missing, []string{"b"}) {
		t.Errorf("falta = %v, quiere [b]", d.Missing)
	}
	if !reflect.DeepEqual(d.Extra, []string{"c"}) {
		t.Errorf("sobra = %v, quiere [c].\n"+
			"  Kanpachi es el único que escribe en su grupo, así que una de más "+
			"significa que alguien lo tocó", d.Extra)
	}
	if d.Intact() {
		t.Error("con una que falta y otra que sobra, no está intacto")
	}
}

func TestUnaReglaApagadaCuentaComoQueNoEsta(t *testing.T) {
	// El efecto de una regla apagada es el mismo que el de una ausente, y
	// reponerla es lo mismo. Tratarla como presente dejaría el puerto cerrado
	// con la pantalla en verde.
	e := Enforcement{
		Rules: []AppliedRule{{Name: "a", Layer: LayerFirewallRules, Enabled: false}},
		Gate:  GatePresent,
	}

	d := e.Diff(deseadas("a"), true)
	if !reflect.DeepEqual(d.Missing, []string{"a"}) {
		t.Errorf("falta = %v, quiere [a]", d.Missing)
	}
	if len(d.Extra) != 0 {
		t.Errorf("sobra = %v, y una regla apagada que SÍ se pidió no sobra", d.Extra)
	}
}

func TestElFiltroDePaquetesNoEntraEnElDiffDeReglas(t *testing.T) {
	// Las dos capas se miden juntas y se juzgan por separado. Un filtro con el
	// mismo nombre que una regla no puede hacer creer que la regla está puesta.
	e := Enforcement{
		Rules: []AppliedRule{{Name: "a", Layer: LayerPacketFilter, Enabled: true}},
		Gate:  GatePresent,
	}

	d := e.Diff(deseadas("a"), true)
	if !reflect.DeepEqual(d.Missing, []string{"a"}) {
		t.Errorf("falta = %v, quiere [a]: el filtro no abre nada, solo cierra", d.Missing)
	}
}

func TestLaCompuertaAusenteNoEsLoMismoQueSinComprobar(t *testing.T) {
	rs := deseadas("a")
	medidas := []AppliedRule{puesta("a")}

	ausente := Enforcement{Rules: medidas, Gate: GateAbsent}.Diff(rs, true)
	if !ausente.GateMissing || ausente.GateUnchecked {
		t.Errorf("ausente: falta=%v sinComprobar=%v", ausente.GateMissing, ausente.GateUnchecked)
	}
	if ausente.Intact() {
		t.Error("sin compuerta la lista de permitidos es aditiva, así que no está intacto")
	}

	ciego := Enforcement{Rules: medidas, Gate: GateUnknown}.Diff(rs, true)
	if ciego.GateMissing || !ciego.GateUnchecked {
		t.Errorf("sin comprobar: falta=%v sinComprobar=%v", ciego.GateMissing, ciego.GateUnchecked)
	}
	if !ciego.Intact() {
		t.Error("no haber podido mirar no es lo mismo que estar alterado.\n" +
			"  Mezclarlos haría que una auditoría caída se lea como manipulación")
	}
}

func TestSinSalaNoSeExigeCompuerta(t *testing.T) {
	// Sin sala abierta no hay adaptador virtual. Exigir la compuerta ahí
	// dejaría una alerta encendida en reposo, que es la forma más rápida de que
	// el usuario aprenda a ignorar la pantalla.
	e := Enforcement{Gate: GateAbsent}

	d := e.Diff(RuleSet{}, false)
	if d.GateMissing || d.GateUnchecked {
		t.Errorf("en reposo no se juzga la compuerta: falta=%v sinComprobar=%v",
			d.GateMissing, d.GateUnchecked)
	}
	if !d.Intact() {
		t.Error("sin sala y sin reglas, el estado correcto es intacto")
	}
}

func TestElOrdenEsEstable(t *testing.T) {
	// Esto alimenta el texto de una alerta. Dos latidos con la misma entrada
	// tienen que producir el mismo texto, o la pantalla parpadea sola por el
	// recorrido aleatorio de un mapa.
	e := Enforcement{
		Rules: []AppliedRule{puesta("z"), puesta("m"), puesta("a")},
		Gate:  GatePresent,
	}
	rs := deseadas("q", "b")

	primero := e.Diff(rs, true)
	for i := 0; i < 50; i++ {
		if d := e.Diff(rs, true); !reflect.DeepEqual(d, primero) {
			t.Fatalf("la corrida %d dio %+v y la primera dio %+v", i, d, primero)
		}
	}
	if !reflect.DeepEqual(primero.Extra, []string{"a", "m", "z"}) {
		t.Errorf("sobra = %v, quiere [a m z]", primero.Extra)
	}
}

func TestTodoPuestoEsIntacto(t *testing.T) {
	e := Enforcement{
		Rules: []AppliedRule{puesta("a"), puesta("b")},
		Gate:  GatePresent,
	}
	d := e.Diff(deseadas("a", "b"), true)
	if !d.Intact() {
		t.Errorf("no está intacto: %+v", d)
	}
	if len(d.Missing) != 0 || len(d.Extra) != 0 {
		t.Errorf("falta=%v sobra=%v, y las dos tienen que estar vacías", d.Missing, d.Extra)
	}
}

func TestLasCapasYLaCompuertaSeNombran(t *testing.T) {
	for _, l := range []EnforcementLayer{LayerFirewallRules, LayerPacketFilter} {
		if s := l.String(); s == "" || s == "capa-inválida" {
			t.Errorf("la capa %d se muestra como %q", l, s)
		}
	}
	if s := EnforcementLayer(0).String(); s != "capa-inválida" {
		t.Errorf("el cero se muestra como %q, y tiene que verse que está mal", s)
	}
	for _, g := range []GateState{GateUnknown, GateAbsent, GatePresent} {
		if s := g.String(); s == "" {
			t.Errorf("el estado %d de la compuerta no se nombra", g)
		}
	}
	// El cero es "sin comprobar" a propósito: el valor por defecto de un
	// adaptador que no contestó nunca puede leerse como que todo está bien.
	if GateUnknown != 0 {
		t.Error("GateUnknown tiene que ser el valor cero")
	}
}
