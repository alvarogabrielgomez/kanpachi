package firewall

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/daemon/adapter/firewall/wfp"
)

// Los dobles anotan en un diario COMPARTIDO, y de eso depende todo este archivo:
// el orden entre las dos capas es lo que se está probando, y con un diario por
// capa el orden entre ellas no se podría observar.
type diario struct{ pasos []string }

func (d *diario) anota(paso string) { d.pasos = append(d.pasos, paso) }

func (d *diario) contiene(paso string) bool {
	for _, p := range d.pasos {
		if p == paso {
			return true
		}
	}
	return false
}

// antes dice si a ocurrió antes que b. Falla si falta alguno.
func (d *diario) antes(t *testing.T, a, b string) bool {
	t.Helper()

	ia, ib := -1, -1
	for i, p := range d.pasos {
		if p == a && ia < 0 {
			ia = i
		}
		if p == b && ib < 0 {
			ib = i
		}
	}
	if ia < 0 || ib < 0 {
		t.Fatalf("el diario es %v y falta %q o %q", d.pasos, a, b)
	}
	return ia < ib
}

type permisosFalsos struct {
	d           *diario
	adaptador   string
	fallaApply  error
	fallaPurga  error
	fallaMedida error
	reglas      []domain.AppliedRule
	aplicado    domain.RuleSet
}

func (p *permisosFalsos) Apply(_ context.Context, desired domain.RuleSet) error {
	p.d.anota("permisos.apply")
	p.aplicado = desired
	return p.fallaApply
}

func (p *permisosFalsos) PurgeOwned(context.Context) error {
	p.d.anota("permisos.purga")
	return p.fallaPurga
}

func (p *permisosFalsos) AuditForeign(context.Context, domain.GameProfile) ([]domain.ForeignRule, error) {
	p.d.anota("permisos.auditoria")
	return nil, nil
}
func (p *permisosFalsos) SuspendForeign(context.Context, []domain.ForeignRule) error { return nil }
func (p *permisosFalsos) RestoreForeign(context.Context) error                       { return nil }
func (p *permisosFalsos) SetAdapter(name string)                                     { p.adaptador = name }

func (p *permisosFalsos) FirewallEnabled(context.Context) ([]domain.FirewallProfileState, error) {
	return nil, nil
}

func (p *permisosFalsos) Enforcement(context.Context) (domain.Enforcement, error) {
	if p.fallaMedida != nil {
		return domain.Enforcement{}, p.fallaMedida
	}
	// Ausente a propósito: desde la capa de permisos no hay forma de saberlo.
	return domain.Enforcement{Rules: p.reglas, Gate: domain.GateAbsent}, nil
}

type compuertaFalsa struct {
	d           *diario
	fallaApply  error
	fallaPurga  error
	fallaMedida error
	medida      wfp.Measurement
	pedido      []wfp.FilterSpec
	preguntado  []wfp.FilterSpec
}

func (g *compuertaFalsa) Apply(_ context.Context, want []wfp.FilterSpec) error {
	g.d.anota("compuerta.apply")
	if g.fallaApply != nil {
		return g.fallaApply
	}
	g.pedido = want
	return nil
}

func (g *compuertaFalsa) Purge(context.Context) error {
	g.d.anota("compuerta.purga")
	return g.fallaPurga
}

func (g *compuertaFalsa) Measure(_ context.Context, want []wfp.FilterSpec) (wfp.Measurement, error) {
	g.d.anota("compuerta.medida")
	g.preguntado = want
	if g.fallaMedida != nil {
		return wfp.Measurement{}, g.fallaMedida
	}
	return g.medida, nil
}

type logMudo struct{}

func (logMudo) Info(string, ...any)  {}
func (logMudo) Warn(string, ...any)  {}
func (logMudo) Error(string, ...any) {}

const luidDePrueba = 0x47008000000000

func salaDePrueba() netip.Prefix { return netip.MustParsePrefix("100.64.1.0/24") }

func reglaDePrueba() domain.FirewallRule {
	return domain.FirewallRule{
		Name:   "kanpachi-udp-16261",
		Proto:  domain.ProtoUDP,
		From:   16261,
		To:     16261,
		Local:  netip.MustParseAddr("100.64.1.1"),
		Remote: []netip.Addr{netip.MustParseAddr("100.64.1.5")},
	}
}

func armado(t *testing.T) (*Firewall, *diario, *permisosFalsos, *compuertaFalsa) {
	t.Helper()

	d := &diario{}
	p := &permisosFalsos{d: d}
	g := &compuertaFalsa{d: d}
	fw, err := New(p, p, g, logMudo{})
	if err != nil {
		t.Fatal(err)
	}
	return fw, d, p, g
}

func conSala(t *testing.T, fw *Firewall) {
	t.Helper()
	if err := fw.SetScope("kanpachi0", luidDePrueba, salaDePrueba()); err != nil {
		t.Fatal(err)
	}
}

func conjunto() domain.RuleSet {
	var rs domain.RuleSet
	rs.Rules = append(rs.Rules, reglaDePrueba())
	return rs
}

func TestTheGateGoesUpBeforeThePermits(t *testing.T) {
	// Es intersección, así que durante el instante entre las dos llamadas hay una
	// capa nueva y la otra vieja. Compuerta primero deja CERRADO lo que sobra;
	// permisos primero deja permisos sin nada que los acote, o sea la lista
	// aditiva otra vez y el agujero de escritorio remoto abierto.
	fw, d, _, _ := armado(t)
	conSala(t, fw)

	if err := fw.Apply(context.Background(), conjunto()); err != nil {
		t.Fatal(err)
	}
	if !d.antes(t, "compuerta.apply", "permisos.apply") {
		t.Errorf("el diario es %v, y la compuerta tiene que ir primero", d.pasos)
	}
}

func TestIfTheGateFailsNothingIsOpened(t *testing.T) {
	// La compuerta se aplica dentro de una transacción, así que un fallo deja la
	// anterior intacta. Escribir los permisos igual sería producir a propósito el
	// caso de arriba: permisos sin la compuerta que les corresponde.
	fw, d, p, g := armado(t)
	conSala(t, fw)
	g.fallaApply = errors.New("la transacción no confirmó")

	err := fw.Apply(context.Background(), conjunto())
	if err == nil {
		t.Fatal("un fallo de la compuerta no se reportó")
	}
	if !strings.Contains(err.Error(), "no se abrió nada") {
		t.Errorf("el error no dice qué pasó con los permisos: %v", err)
	}
	if d.contiene("permisos.apply") {
		t.Errorf("se abrieron los permisos con la compuerta caída. Diario: %v", d.pasos)
	}
	if len(p.aplicado.Rules) != 0 {
		t.Error("la capa de permisos recibió un conjunto igual")
	}
}

func TestThePermitsGoDownBeforeTheGate(t *testing.T) {
	// El inverso de Apply, y por lo mismo: quitar la compuerta primero dejaría,
	// durante ese instante, permisos sin nada encima.
	fw, d, _, _ := armado(t)
	conSala(t, fw)

	if err := fw.PurgeOwned(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !d.antes(t, "permisos.purga", "compuerta.purga") {
		t.Errorf("el diario es %v, y los permisos tienen que irse primero", d.pasos)
	}
}

func TestIfThePermitsCannotBePurgedTheGateStays(t *testing.T) {
	// El único caso donde dejar algo a medias es lo correcto: unos permisos
	// huérfanos bajo una compuerta siguen acotados, y los mismos permisos sin
	// ella son el agujero.
	fw, d, p, _ := armado(t)
	conSala(t, fw)
	p.fallaPurga = errors.New("una regla la usa alguien más")

	if err := fw.PurgeOwned(context.Background()); err == nil {
		t.Fatal("un fallo purgando los permisos no se reportó")
	}
	if d.contiene("compuerta.purga") {
		t.Errorf("se quitó la compuerta dejando permisos sin purgar. Diario: %v", d.pasos)
	}
}

func TestWithNoAdapterThePermitsStillApplyAndTheGateIsPurged(t *testing.T) {
	// El adaptador nace DESPUÉS del daemon, así que el primer Apply de una sesión
	// puede correr legítimamente sin él. Dejar la compuerta anterior puesta sería
	// peor que quitarla: filtros de una sala que ya no existe no protegen nada y
	// no se ven en ninguna herramienta del sistema.
	fw, d, _, g := armado(t)

	if err := fw.Apply(context.Background(), conjunto()); err != nil {
		t.Fatal(err)
	}
	if !d.contiene("permisos.apply") {
		t.Error("sin adaptador tampoco se aplicaron los permisos")
	}
	if !d.contiene("compuerta.purga") {
		t.Errorf("no se purgó la compuerta vieja. Diario: %v", d.pasos)
	}
	if g.pedido != nil {
		t.Error("se le pidió a la compuerta un conjunto sin tener dónde ponerlo")
	}
}

func TestLeavingTheRoomTakesTheGateDown(t *testing.T) {
	// Salir de la sala es olvidar el adaptador y reaplicar. Si la compuerta se
	// quedara puesta, quedarían filtros de una sala muerta que ninguna
	// herramienta del sistema muestra.
	fw, d, _, _ := armado(t)
	conSala(t, fw)
	if err := fw.Apply(context.Background(), conjunto()); err != nil {
		t.Fatal(err)
	}

	fw.ClearScope()
	d.pasos = nil
	if err := fw.Apply(context.Background(), domain.RuleSet{}); err != nil {
		t.Fatal(err)
	}
	if !d.contiene("compuerta.purga") {
		t.Errorf("la compuerta sobrevivió a la sala. Diario: %v", d.pasos)
	}
}

func TestAnIdleMachineDoesNotSweepTheGateOnEveryBeat(t *testing.T) {
	// El supervisor reaplica cada tantos latidos. Barrer las cuarenta ranuras en
	// cada uno es una transacción contra el motor de filtrado por latido a cambio
	// de nada, en una máquina que no tiene sala abierta.
	fw, d, _, _ := armado(t)

	for i := 0; i < 5; i++ {
		if err := fw.Apply(context.Background(), domain.RuleSet{}); err != nil {
			t.Fatal(err)
		}
	}

	purgas := 0
	for _, p := range d.pasos {
		if p == "compuerta.purga" {
			purgas++
		}
	}
	if purgas != 1 {
		t.Errorf("cinco latidos en reposo dieron %d barridos, y el único que hace falta "+
			"es el primero, por si un daemon anterior murió sucio", purgas)
	}
}

func TestAScopeThatDoesNotNarrowIsRefused(t *testing.T) {
	// Si las dos capas discreparan sobre qué adaptador es la sala, los permisos
	// irían sobre uno y el bloqueo sobre otro: un adaptador con permisos y sin
	// compuerta, con las dos capas contestando que sí.
	fw, _, p, _ := armado(t)

	casos := []struct {
		nombre    string
		adaptador string
		luid      uint64
		sala      netip.Prefix
	}{
		{"sin adaptador", "", luidDePrueba, salaDePrueba()},
		{"sin LUID", "kanpachi0", 0, salaDePrueba()},
		{"todo internet", "kanpachi0", luidDePrueba, netip.MustParsePrefix("0.0.0.0/0")},
		{"la LAN de casa", "kanpachi0", luidDePrueba, netip.MustParsePrefix("192.168.1.0/24")},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			if err := fw.SetScope(c.adaptador, c.luid, c.sala); err == nil {
				t.Fatal("se aceptó un alcance que no acota")
			}
			if p.adaptador != "" {
				t.Errorf("los permisos se quedaron apuntando a %q", p.adaptador)
			}
		})
	}
}

func TestEnforcementMeasuresBothLayers(t *testing.T) {
	fw, _, p, g := armado(t)
	conSala(t, fw)

	p.reglas = []domain.AppliedRule{
		{Name: "kanpachi-udp-16261", Layer: domain.LayerFirewallRules, Enabled: true},
	}
	g.medida = wfp.Measurement{
		Gate: domain.GatePresent,
		Rules: []domain.AppliedRule{
			{Name: "kanpachi-udp-16261", Layer: domain.LayerPacketFilter, Enabled: true},
		},
	}

	e, err := fw.Enforcement(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if e.Gate != domain.GatePresent {
		t.Errorf("la compuerta se reportó %v, y la capa de permisos la da siempre por "+
			"ausente: la medición de verdad tiene que pisarla", e.Gate)
	}
	if len(e.Rules) != 2 {
		t.Fatalf("se midieron %d reglas y son dos, una por capa", len(e.Rules))
	}

	capas := map[domain.EnforcementLayer]int{}
	for _, r := range e.Rules {
		capas[r.Layer]++
	}
	if capas[domain.LayerFirewallRules] != 1 || capas[domain.LayerPacketFilter] != 1 {
		t.Errorf("las capas medidas son %v", capas)
	}
}

func TestEnforcementAsksTheGateAboutWhatWasApplied(t *testing.T) {
	// Las ranuras son posiciones y no nombres, así que sin el conjunto pedido no
	// se puede decir QUÉ falta. Quien contesta si el filtro está sigue siendo el
	// sistema.
	fw, _, _, g := armado(t)
	conSala(t, fw)

	if err := fw.Apply(context.Background(), conjunto()); err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Enforcement(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(g.preguntado) != len(g.pedido) || len(g.preguntado) == 0 {
		t.Fatalf("se aplicaron %d filtros y se preguntó por %d", len(g.pedido), len(g.preguntado))
	}
	for i := range g.pedido {
		if g.preguntado[i].Key != g.pedido[i].Key {
			t.Errorf("se preguntó por una clave distinta de la que se puso, en la ranura %d", i)
		}
	}
}

func TestAFailedMeasurementIsNotHalfAGreenScreen(t *testing.T) {
	// Devolver la mitad medida y la otra en cero es indistinguible de "esa mitad
	// no tiene nada puesto", que es la lectura opuesta. Misma doctrina que
	// AlertAuditFailed.
	t.Run("falla la compuerta", func(t *testing.T) {
		fw, _, p, g := armado(t)
		conSala(t, fw)
		p.reglas = []domain.AppliedRule{{Name: "a", Layer: domain.LayerFirewallRules, Enabled: true}}
		g.fallaMedida = errors.New("no se pudo leer el filtro")

		e, err := fw.Enforcement(context.Background())
		if err == nil {
			t.Fatal("una medición caída se devolvió como buena")
		}
		if len(e.Rules) != 0 || e.Gate != domain.GateUnknown {
			t.Errorf("se devolvió media medición: %+v", e)
		}
	})

	t.Run("fallan los permisos", func(t *testing.T) {
		fw, _, p, _ := armado(t)
		conSala(t, fw)
		p.fallaMedida = errors.New("no se pudo enumerar")

		if _, err := fw.Enforcement(context.Background()); err == nil {
			t.Fatal("una medición caída se devolvió como buena")
		}
	})
}

func TestClearScopeForgetsBothLayers(t *testing.T) {
	fw, _, p, _ := armado(t)
	conSala(t, fw)
	fw.ClearScope()

	if p.adaptador != "" {
		t.Errorf("los permisos siguen apuntando a %q", p.adaptador)
	}
	if err := fw.scope.Valid(); err == nil {
		t.Error("la compuerta sigue creyendo que hay sala")
	}
}
