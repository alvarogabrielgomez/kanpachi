package domain

import (
	"testing"
	"time"
)

func cuando() time.Time { return time.Date(2026, 8, 4, 22, 0, 0, 0, time.UTC) }

// El hecho propio del host gana sobre lo que diga el invitado. Es toda la
// defensa contra un miembro que miente, y es la única afirmación de este archivo
// que se hace con certeza.
func TestLoQueVioElHostLeGanaAlInformeDelInvitado(t *testing.T) {
	c := CanaryCheck{
		MeasuredAt:  cuando(),
		Touched:     true,
		Answered:    true,
		ReportedTCP: ProbeSilent,
		ReportedUDP: ProbeSilent,
	}

	if got := c.Verdict(); got != CanaryLeaking {
		t.Fatalf("veredicto = %v, se esperaba CanaryLeaking. El invitado dijo silencio "+
			"y al canario lo tocaron: el paquete cruzó, lo diga quien lo diga", got)
	}
	if !c.NeedsAttention() {
		t.Error("una fuga tiene que llegar a la pantalla")
	}
}

func TestSinTocarYConSilencioInformadoNoHayEvidenciaDeFuga(t *testing.T) {
	c := CanaryCheck{
		MeasuredAt:  cuando(),
		Touched:     false,
		Answered:    true,
		ReportedTCP: ProbeSilent,
		ReportedUDP: ProbeSilent,
	}

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
	c := CanaryCheck{MeasuredAt: cuando(), Answered: false}

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
	c := CanaryCheck{
		MeasuredAt:  cuando(),
		Touched:     false,
		Answered:    true,
		ReportedTCP: ProbeAnswered,
		ReportedUDP: ProbeSilent,
	}

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
	c := CanaryCheck{
		MeasuredAt:  cuando(),
		Touched:     true,
		Answered:    true,
		ReportedTCP: ProbeSilent,
		ReportedUDP: ProbeAnswered,
	}
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
	c := CanaryCheck{
		MeasuredAt:  cuando(),
		Touched:     false,
		Answered:    true,
		ReportedTCP: ProbeFailed,
		ReportedUDP: ProbeFailed,
	}

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
	c := CanaryCheck{
		MeasuredAt:  cuando(),
		Answered:    true,
		ReportedTCP: ProbeFailed,
		ReportedUDP: ProbeSilent,
	}
	if !c.ReportedMeasured() {
		t.Fatal("el silencio de UDP es una medición")
	}
	if got := c.Verdict(); got != CanaryClean {
		t.Fatalf("veredicto = %v, se esperaba CanaryClean", got)
	}
}
