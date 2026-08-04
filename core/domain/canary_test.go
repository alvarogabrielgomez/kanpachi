package domain

import (
	"net/netip"
	"testing"
	"time"
)

func cuando() time.Time { return time.Date(2026, 8, 4, 22, 0, 0, 0, time.UTC) }

// puertoDelCanario es un efímero cualquiera. Fijo para que un informe con otro
// puerto se pueda escribir sin ambigüedad.
const puertoDelCanario = 51234

func apodo(t *testing.T, s string) Nickname {
	t.Helper()
	n, err := ParseNickname(s)
	if err != nil {
		t.Fatalf("apodo %q inválido: %v", s, err)
	}
	return n
}

// miembro devuelve a alguien de la sala, con su dirección y su apodo.
func miembro(t *testing.T, n int) CanaryAsked {
	t.Helper()
	nombres := []string{"humberto", "marisol", "ignacio"}
	return CanaryAsked{
		At:   netip.AddrFrom4([4]byte{10, 77, 0, byte(n + 2)}),
		Name: apodo(t, nombres[n]),
	}
}

// ronda arma una comprobación con UN preguntado que ya contestó lo que se le
// diga. Es la forma de casi todos los tests de veredicto.
func ronda(t *testing.T, tcp, udp ProbeOutcome) CanaryCheck {
	t.Helper()
	m := miembro(t, 0)
	return CanaryCheck{
		MeasuredAt: cuando(),
		Port:       puertoDelCanario,
		Asked:      []CanaryAsked{m},
		Answers:    []CanaryAnswer{{From: m.At, Name: m.Name, TCP: tcp, UDP: udp}},
	}
}

// El hecho propio del host gana sobre lo que diga el invitado. Es toda la
// defensa contra un miembro que miente, y es la única afirmación de este archivo
// que se hace con certeza.
func TestLoQueVioElHostLeGanaAlInformeDelInvitado(t *testing.T) {
	c := ronda(t, ProbeSilent, ProbeSilent)
	c.Touched = true

	if got := c.Verdict(); got != CanaryLeaking {
		t.Fatalf("veredicto = %v, se esperaba CanaryLeaking. El invitado dijo silencio "+
			"y al canario lo tocaron: el paquete cruzó, lo diga quien lo diga", got)
	}
	if !c.NeedsAttention() {
		t.Error("una fuga tiene que llegar a la pantalla")
	}
}

func TestSinTocarYConSilencioInformadoNoHayEvidenciaDeFuga(t *testing.T) {
	c := ronda(t, ProbeSilent, ProbeSilent)

	if got := c.Verdict(); got != CanaryClean {
		t.Fatalf("veredicto = %v, se esperaba CanaryClean", got)
	}
	if c.NeedsAttention() {
		t.Error("lo normal no puede encender un aviso")
	}
}

// Se llama "sin evidencia" y no "bloqueando" a propósito, y este test fija esa
// distinción: un invitado que no marque y diga que hubo silencio produce
// exactamente lo mismo que la compuerta funcionando. No se puede afirmar más.
func TestElEstadoBuenoNoAfirmaQueLaCompuertaFunciona(t *testing.T) {
	if CanaryClean.String() == "bloqueando" {
		t.Fatal("el estado bueno no puede decir que la compuerta bloquea: no se sabe. " +
			"Un invitado que se quede quieto produce este mismo estado")
	}
	if CanaryClean.String() != "sin evidencia de fuga" {
		t.Errorf("texto = %q", CanaryClean.String())
	}
}

func TestSiElInvitadoNoContestaNoSeComproboNada(t *testing.T) {
	c := CanaryCheck{
		MeasuredAt: cuando(),
		Port:       puertoDelCanario,
		Asked:      []CanaryAsked{miembro(t, 0)},
	}

	if got := c.Verdict(); got != CanaryUnconfirmed {
		t.Fatalf("veredicto = %v, se esperaba CanaryUnconfirmed", got)
	}
	// Y no enciende nada: es el estado normal de una sala donde todavía no hay
	// nadie más, y avisar ahí enseña a ignorar los avisos.
	if c.NeedsAttention() {
		t.Error("no haber podido comprobar no es un problema que enseñar")
	}
}

// Dice que conectó y al canario no lo tocó nadie. No puede ser fuga, porque no
// llegó nada, y tampoco cuenta como comprobación buena.
func TestUnInformeQueNoCuadraNoCuentaComoBueno(t *testing.T) {
	c := ronda(t, ProbeAnswered, ProbeSilent)

	if got := c.Verdict(); got != CanaryMismatch {
		t.Fatalf("veredicto = %v, se esperaba CanaryMismatch", got)
	}
	if !c.NeedsAttention() {
		t.Error("un informe que no cuadra tiene que verse: alguien le está contando " +
			"al daemon algo que no pasó")
	}
}

func TestUnaComprobacionSinHacerEsElCero(t *testing.T) {
	var c CanaryCheck
	if !c.Blind() {
		t.Fatal("el cero tiene que ser ciego")
	}
	if got := c.Verdict(); got != CanaryBlind {
		t.Fatalf("veredicto del cero = %v", got)
	}
	if CanaryBlind != 0 {
		t.Fatal("CanaryBlind dejó de ser el cero, así que una comprobación sin hacer " +
			"puede leerse como buena")
	}
	if c.NeedsAttention() {
		t.Error("no haber comprobado todavía no es un aviso")
	}
}

// El eco de UDP cuenta igual que el apretón de manos de TCP: los dos significan
// que el paquete llegó. Es el protocolo por el que habla justo la herramienta
// que más preocupa.
func TestUnaFugaSoloPorUDPCuentaIgual(t *testing.T) {
	c := ronda(t, ProbeSilent, ProbeAnswered)
	c.Touched = true

	if got := c.Verdict(); got != CanaryLeaking {
		t.Fatalf("veredicto = %v, se esperaba CanaryLeaking", got)
	}
	if !c.ReportedReach() {
		t.Error("un eco de UDP es el paquete llegando")
	}
}

// Una ronda en la que el invitado NO PUDO preguntar no midió nada, así que no
// puede contarse como buena.
//
// Lo encontró una revisión adversaria del diseño, leyendo el código: el fallo
// local caía en la misma rama que el silencio y salía como CanaryClean. Son lo
// contrario la una de la otra.
func TestUnaRondaEnLaQueNadiePudoPreguntarNoCuentaComoBuena(t *testing.T) {
	c := ronda(t, ProbeFailed, ProbeFailed)

	if got := c.Verdict(); got != CanaryUnconfirmed {
		t.Fatalf("veredicto = %v, se esperaba CanaryUnconfirmed. Sumar tranquilidad "+
			"de una comprobación que no ocurrió es la mentira que este tipo evita", got)
	}
	if c.ReportedMeasured() {
		t.Error("un fallo en los dos protocolos no midió nada")
	}
}

// Pero si UNO de los dos sí midió, la ronda vale por ese.
func TestSiUnProtocoloMidioLaRondaVale(t *testing.T) {
	c := ronda(t, ProbeFailed, ProbeSilent)

	if !c.ReportedMeasured() {
		t.Fatal("el silencio de UDP es una medición")
	}
	if got := c.Verdict(); got != CanaryClean {
		t.Fatalf("veredicto = %v, se esperaba CanaryClean", got)
	}
}

// ---------------------------------------------------------------------------
// Lo que solo existe porque la ronda es PLURAL
// ---------------------------------------------------------------------------

// La ronda NO vota. Un informe que no cuadra con lo que vio el host ensucia la
// ronda entera, por muchos honestos que haya.
//
// Caso real: dos miembros que contestan silencio y uno que dice haber conectado
// a un canario que nadie tocó.
func TestUnSoloInformeQueDiceQueLlegoEnsuciaLaRonda(t *testing.T) {
	a, b, c := miembro(t, 0), miembro(t, 1), miembro(t, 2)
	check := CanaryCheck{
		MeasuredAt: cuando(),
		Port:       puertoDelCanario,
		Asked:      []CanaryAsked{a, b, c},
		Answers: []CanaryAnswer{
			{From: a.At, Name: a.Name, TCP: ProbeSilent, UDP: ProbeSilent},
			{From: b.At, Name: b.Name, TCP: ProbeSilent, UDP: ProbeSilent},
			{From: c.At, Name: c.Name, TCP: ProbeAnswered, UDP: ProbeSilent},
		},
	}

	if got := check.Verdict(); got != CanaryMismatch {
		t.Fatalf("veredicto = %v, se esperaba CanaryMismatch. Una mayoría honesta no "+
			"limpia un informe que no cuadra: los dos silencios y el dicho de que "+
			"llegó no pueden ser ciertos a la vez", got)
	}
}

// Que uno solo de tres conteste no invalida la ronda. Sin esto, la ronda queda
// de rehén del miembro más lento, o del que tenga la app minimizada.
func TestSiUnoSoloDeTresContestaLaRondaValeIgual(t *testing.T) {
	a, b, c := miembro(t, 0), miembro(t, 1), miembro(t, 2)
	check := CanaryCheck{
		MeasuredAt: cuando(),
		Port:       puertoDelCanario,
		Asked:      []CanaryAsked{a, b, c},
		Answers:    []CanaryAnswer{{From: a.At, Name: a.Name, TCP: ProbeSilent, UDP: ProbeSilent}},
	}

	if check.AllAnswered() {
		t.Fatal("faltan dos por contestar")
	}
	if got := check.Verdict(); got != CanaryClean {
		t.Fatalf("veredicto = %v, se esperaba CanaryClean", got)
	}
}

// EL TEST QUE CIERRA EL AGUJERO.
//
// La ronda cierra temprano cuando contestaron todos. Si el contador contara
// informes en vez de remitentes, un miembro que mande tres de golpe cerraría la
// ronda en milisegundos y los otros dos nunca llegarían a marcar. Eso no
// fabrica una alarma falsa: esconde una fuga real.
func TestUnMiembroNoPuedeCerrarLaRondaContestandoTresVeces(t *testing.T) {
	a, b, c := miembro(t, 0), miembro(t, 1), miembro(t, 2)
	check := CanaryCheck{
		MeasuredAt: cuando(),
		Port:       puertoDelCanario,
		Asked:      []CanaryAsked{a, b, c},
	}

	admitidos := 0
	for i := 0; i < 3; i++ {
		if check.Record(CanaryReport{From: c.At, Port: puertoDelCanario, TCP: ProbeSilent, UDP: ProbeSilent}) {
			admitidos++
		}
	}

	if admitidos != 1 {
		t.Errorf("se admitieron %d informes del mismo miembro, se esperaba 1", admitidos)
	}
	if len(check.Answers) != 1 {
		t.Errorf("Answers tiene %d, se esperaba 1", len(check.Answers))
	}
	if check.AllAnswered() {
		t.Fatal("la ronda se dio por completa con un solo miembro contestando tres veces. " +
			"Así, quien esté adentro cierra la ronda antes de que los honestos midan, " +
			"y una fuga real no se detecta nunca")
	}

	// Y los honestos que llegan después sí se cuentan.
	for _, m := range []CanaryAsked{a, b} {
		if !check.Record(CanaryReport{From: m.At, Port: puertoDelCanario, TCP: ProbeSilent, UDP: ProbeSilent}) {
			t.Fatalf("se rechazó el informe honesto de %v", m.At)
		}
	}
	if !check.AllAnswered() {
		t.Error("con los tres remitentes distintos la ronda sí está completa")
	}
}

func TestUnInformeDeQuienNoFuePreguntadoNoCuenta(t *testing.T) {
	check := CanaryCheck{
		MeasuredAt: cuando(),
		Port:       puertoDelCanario,
		Asked:      []CanaryAsked{miembro(t, 0)},
	}

	intruso := netip.MustParseAddr("10.77.0.99")
	if check.Record(CanaryReport{From: intruso, Port: puertoDelCanario, TCP: ProbeSilent, UDP: ProbeSilent}) {
		t.Fatal("se admitió un informe de alguien a quien no se le preguntó nada")
	}
	if len(check.Answers) != 0 {
		t.Error("no tendría que haber quedado registrado")
	}
}

func TestUnInformeDeOtroCanarioNoCuenta(t *testing.T) {
	m := miembro(t, 0)
	check := CanaryCheck{
		MeasuredAt: cuando(),
		Port:       puertoDelCanario,
		Asked:      []CanaryAsked{m},
	}

	if check.Record(CanaryReport{From: m.At, Port: puertoDelCanario + 1, TCP: ProbeSilent, UDP: ProbeSilent}) {
		t.Fatal("se admitió el informe tardío de un canario ya cerrado")
	}
}

// ---------------------------------------------------------------------------
// Qué puede APAGAR la alarma
// ---------------------------------------------------------------------------

// Callarse no borra una medición.
//
// Un miembro que se quede quieto deja la ronda sin confirmar. Si eso apagara la
// alarma, quedarse callado pasaría de esconder información a BORRAR una fuga ya
// demostrada, que es lo contrario del techo de daño que este archivo promete.
func TestUnaRondaQueNadieContestaNoApagaLaAlarma(t *testing.T) {
	c := CanaryCheck{
		MeasuredAt: cuando(),
		Port:       puertoDelCanario,
		Asked:      []CanaryAsked{miembro(t, 0)},
	}

	if c.Verdict() != CanaryUnconfirmed {
		t.Fatalf("veredicto = %v, se esperaba CanaryUnconfirmed", c.Verdict())
	}
	if c.ClearsAlarm() {
		t.Fatal("una ronda que nadie contestó apagó la alarma. Así, un miembro borra " +
			"una fuga ya demostrada con solo quedarse quieto")
	}
}

// Una ronda que ni llegó a abrirse produce el cero, y el cero no puede apagar
// nada. Es el caso de `canaryPlanLocked` negándose.
func TestUnaRondaQueNiSeAbrioNoApagaLaAlarma(t *testing.T) {
	var c CanaryCheck
	if c.ClearsAlarm() {
		t.Fatal("el cero apagó la alarma. Una ronda que no ocurrió no mide nada")
	}
}

func TestUnInformeQueNoCuadraTampocoApagaLaAlarma(t *testing.T) {
	c := ronda(t, ProbeAnswered, ProbeSilent)
	if c.Verdict() != CanaryMismatch {
		t.Fatalf("veredicto = %v", c.Verdict())
	}
	if c.ClearsAlarm() {
		t.Error("alguien contando algo que no pasó no puede limpiar una alarma")
	}
}

func TestSoloUnaRondaLimpiaApagaLaAlarma(t *testing.T) {
	c := ronda(t, ProbeSilent, ProbeSilent)
	if !c.ClearsAlarm() {
		t.Fatalf("veredicto = %v, una ronda limpia tiene que poder apagar la alarma", c.Verdict())
	}
}
