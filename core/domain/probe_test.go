package domain

import (
	"net/netip"
	"testing"
	"time"
)

func at() time.Time { return time.Date(2026, 8, 4, 21, 30, 0, 0, time.UTC) }

func reportWith(res ...ProbeResult) ProbeReport {
	return ProbeReport{
		Target:     netip.MustParseAddr("100.64.1.1"),
		MeasuredAt: at(),
		Results:    res,
	}
}

func result(port uint16, kind ProbeKind, out ProbeOutcome) ProbeResult {
	return ProbeResult{
		ProbeTarget: ProbeTarget{Port: port, Kind: kind, Label: "x"},
		Outcome:     out,
	}
}

// Este es el guardián que mantiene el sondeo al día con la cuarentena. Sin él,
// ampliar forbiddenPorts deja el sondeo midiendo la lista de ayer y nada falla.
func TestCadaPuertoProhibidoTieneEtiquetaDeSondeo(t *testing.T) {
	for _, p := range forbiddenPorts {
		if _, ok := forbiddenProbeLabels[p]; !ok {
			t.Errorf("el puerto prohibido %d no tiene entrada en forbiddenProbeLabels. "+
				"Si es TCP, ponle nombre; si es UDP, ponle el vacío con un comentario "+
				"que diga por qué no se puede sondear", p)
		}
	}
	for p := range forbiddenProbeLabels {
		found := false
		for _, f := range forbiddenPorts {
			if f == p {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("forbiddenProbeLabels tiene el puerto %d y forbiddenPorts no. "+
				"Los puertos de herramientas van en remoteAccessProbePorts", p)
		}
	}
}

func TestUnInformeSinMedirEsCiego(t *testing.T) {
	var r ProbeReport
	if !r.Blind() {
		t.Fatal("el cero de ProbeReport tiene que ser ciego")
	}
	if got := r.Verdict(); got != VerdictBlind {
		t.Fatalf("veredicto del cero = %v, se esperaba VerdictBlind", got)
	}
	// El cero del enum es el ciego a propósito: un veredicto recién construido
	// no puede leerse como "está todo bien".
	if VerdictBlind != 0 {
		t.Fatal("VerdictBlind dejó de ser el cero, así que un informe sin medir " +
			"puede pintarse como bueno")
	}
}

func TestUnaRespuestaProhibidaEsLaAlarmaYGanaSobreTodo(t *testing.T) {
	// Sin referencia viva a propósito: la fuga no la necesita, porque la misma
	// respuesta prueba que la máquina está viva y que está abierta.
	r := reportWith(
		result(ControlPort, ProbeReference, ProbeSilent),
		result(445, ProbeForbidden, ProbeAnswered),
	)
	if got := r.Verdict(); got != VerdictLeaky {
		t.Fatalf("veredicto = %v, se esperaba VerdictLeaky", got)
	}
	if leaks := r.Leaks(); len(leaks) != 1 || leaks[0].Port != 445 {
		t.Fatalf("Leaks() = %v, se esperaba solo el 445", leaks)
	}
}

func TestUnRebotesCuentaComoLlegar(t *testing.T) {
	// El RST casi no ocurre en Windows por el modo sigiloso, y cuando ocurre
	// significa que el firewall dejó pasar. Es una fuga igual.
	r := reportWith(
		result(ControlPort, ProbeReference, ProbeAnswered),
		result(5900, ProbeForbidden, ProbeRefused),
	)
	if got := r.Verdict(); got != VerdictLeaky {
		t.Fatalf("veredicto = %v, se esperaba VerdictLeaky: un RST es el paquete llegando", got)
	}
}

func TestSinReferenciaElSondeoNoPruebaNada(t *testing.T) {
	r := reportWith(
		result(ControlPort, ProbeReference, ProbeSilent),
		result(445, ProbeForbidden, ProbeSilent),
		result(3389, ProbeForbidden, ProbeSilent),
	)
	// Todo callado se ve igual con la PC blindada y con la PC apagada. Decir
	// "cerrado" acá sería afirmar lo que no se sabe.
	if got := r.Verdict(); got != VerdictUnreachable {
		t.Fatalf("veredicto = %v, se esperaba VerdictUnreachable", got)
	}
}

func TestConReferenciaVivaYNadaProhibidoEstaCerrado(t *testing.T) {
	r := reportWith(
		result(ControlPort, ProbeReference, ProbeAnswered),
		result(445, ProbeForbidden, ProbeSilent),
	)
	if got := r.Verdict(); got != VerdictSealed {
		t.Fatalf("veredicto = %v, se esperaba VerdictSealed", got)
	}
	if leaks := r.Leaks(); len(leaks) != 0 {
		t.Fatalf("Leaks() = %v, se esperaba vacío", leaks)
	}
}

// La medición del 2026-08-04 dice que un puerto permitido sin oyente CALLA, así
// que el silencio de un puerto de juego es el estado normal con el juego
// cerrado. Un veredicto que se encendiera con eso sería una alarma falsa cada
// vez que alguien mira la pantalla.
func TestUnPuertoDeJuegoCalladoNoCambiaElVeredicto(t *testing.T) {
	r := reportWith(
		result(ControlPort, ProbeReference, ProbeAnswered),
		result(16261, ProbeGame, ProbeSilent),
	)
	if got := r.Verdict(); got != VerdictSealed {
		t.Fatalf("veredicto = %v, se esperaba VerdictSealed", got)
	}
}

func TestElFalloAlPreguntarNoEsUnaRespuesta(t *testing.T) {
	if ProbeFailed.Reached() {
		t.Fatal("no haber podido preguntar no puede contar como que el paquete llegó")
	}
	r := reportWith(
		result(ControlPort, ProbeReference, ProbeFailed),
		result(445, ProbeForbidden, ProbeFailed),
	)
	if got := r.Verdict(); got != VerdictUnreachable {
		t.Fatalf("veredicto = %v, se esperaba VerdictUnreachable", got)
	}
}

func TestLaListaLlevaLaReferenciaYLosProhibidos(t *testing.T) {
	ts := ProbeTargets(GameProfile{})

	var ref int
	for _, tg := range ts {
		if tg.Kind == ProbeReference {
			ref++
			if tg.Port != ControlPort {
				t.Errorf("la referencia es el puerto %d y tiene que ser el canal de la sala", tg.Port)
			}
		}
	}
	if ref != 1 {
		t.Fatalf("hay %d referencias y tiene que haber exactamente una: sin ella el "+
			"silencio no se puede leer, y con dos habría dos verdades", ref)
	}

	// Los que la cuarentena tapa tienen que estar, o el sondeo no comprobaría
	// nunca que la cuarentena sigue puesta.
	for _, want := range []uint16{445, 3389, 5985} {
		if !hasPort(ts, want) {
			t.Errorf("falta el puerto %d en la lista de sondeo", want)
		}
	}
	// Y los de fábrica de las herramientas, que son el agujero que la cuarentena
	// no puede tapar por puerto.
	if !hasPort(ts, 5938) {
		t.Error("falta el 5938 de TeamViewer")
	}
}

func TestLosPuertosSoloUDPNoSeSondean(t *testing.T) {
	ts := ProbeTargets(GameProfile{})
	// 137, 138 y 3702 están prohibidos y son UDP. Sondearlos por TCP daría
	// silencio siempre, o sea una fila verde que no midió nada.
	for _, udp := range []uint16{137, 138, 3702} {
		if hasPort(ts, udp) {
			t.Errorf("el puerto %d es UDP y no se puede sondear por TCP", udp)
		}
	}
}

func TestElJuegoAportaSuPrimerPuertoTCP(t *testing.T) {
	g := GameProfile{
		ID:   "zomboid",
		Name: "Project Zomboid",
		HostPorts: []PortRange{
			{Proto: ProtoUDP, From: 16261, To: 16262},
			{Proto: ProtoTCP, From: 27015, To: 27020},
		},
	}
	ts := ProbeTargets(g)

	if hasPort(ts, 16261) {
		t.Error("un rango UDP no se sondea")
	}
	if !hasPort(ts, 27015) {
		t.Fatal("falta el primero del rango TCP del juego")
	}
	if hasPort(ts, 27016) {
		t.Error("del rango va SOLO el primero: el resto son filas que dicen lo mismo")
	}
	for _, tg := range ts {
		if tg.Port == 27015 {
			if tg.Kind != ProbeGame {
				t.Errorf("el puerto del juego llegó como %v", tg.Kind)
			}
			if tg.Label != "Project Zomboid" {
				t.Errorf("etiqueta = %q, se esperaba el nombre del juego", tg.Label)
			}
		}
	}
}

// Un juego que pida uno de los puertos de fábrica de una herramienta lo tiene
// abierto A PROPÓSITO. Dejarlo como prohibido encendería la alarma sobre algo
// que el usuario pidió, que es la forma más rápida de que aprenda a ignorarla.
func TestUnPuertoQueElJuegoPideDejaDeSerProhibido(t *testing.T) {
	g := GameProfile{
		ID:        "raro",
		Name:      "Juego Raro",
		HostPorts: []PortRange{{Proto: ProtoTCP, From: 5900, To: 5900}},
	}
	ts := ProbeTargets(g)

	seen := 0
	for _, tg := range ts {
		if tg.Port != 5900 {
			continue
		}
		seen++
		if tg.Kind != ProbeGame {
			t.Errorf("el 5900 llegó como %v y el juego lo pidió", tg.Kind)
		}
	}
	if seen != 1 {
		t.Fatalf("el 5900 aparece %d veces y tiene que aparecer una sola", seen)
	}
}

// Ni el canal de la sala se puede pisar: es la referencia, y perderla dejaría
// al sondeo sin forma de distinguir una PC blindada de una apagada.
func TestElCanalDeLaSalaGanaSobreLoQuePidaElJuego(t *testing.T) {
	g := GameProfile{
		ID:        "raro",
		Name:      "Juego Raro",
		HostPorts: []PortRange{{Proto: ProtoTCP, From: ControlPort, To: ControlPort}},
	}
	for _, tg := range ProbeTargets(g) {
		if tg.Port == ControlPort && tg.Kind != ProbeReference {
			t.Fatalf("el canal de la sala llegó como %v", tg.Kind)
		}
	}
}

func TestLaListaSaleOrdenadaYEstable(t *testing.T) {
	g := GameProfile{ID: "x", Name: "X", HostPorts: []PortRange{{Proto: ProtoTCP, From: 27015, To: 27015}}}
	a := ProbeTargets(g)
	b := ProbeTargets(g)

	if len(a) != len(b) {
		t.Fatalf("dos llamadas dieron %d y %d objetivos", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("en la posición %d: %v vs %v. El recorrido de un mapa no es "+
				"estable y la pantalla parpadearía sola", i, a[i], b[i])
		}
		if i > 0 && a[i-1].Port >= a[i].Port {
			t.Fatalf("la lista no viene ordenada por puerto: %d antes de %d",
				a[i-1].Port, a[i].Port)
		}
	}
}

func hasPort(ts []ProbeTarget, port uint16) bool {
	for _, t := range ts {
		if t.Port == port {
			return true
		}
	}
	return false
}
