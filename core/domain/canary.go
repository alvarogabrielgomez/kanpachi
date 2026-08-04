package domain

import "time"

// La comprobación con canario, y por qué el informe del invitado NO es prueba.
//
// # Qué se comprueba
//
// El host abre un oyente en un puerto que la compuerta tiene que bloquear, y le
// pide a alguien de la sala que lo marque. Como se sabe con certeza que hay
// alguien detrás de esa puerta, el silencio deja de tener dos lecturas.
//
// Eso quita la ambigüedad medida el 2026-08-04: en Windows un puerto permitido y
// sin oyente calla igual que un puerto bloqueado. Ver el paquete
// `daemon/adapter/canary`.
//
// # Las dos fuentes, y solo una es un hecho
//
//	Touched    lo vio el HOST. Alguien llegó hasta el socket. NO se puede
//	           falsificar desde fuera: o el paquete cruzó, o no cruzó
//	Reported   lo dijo el INVITADO. Es un mensaje, y un mensaje se puede mentir
//
// Todo el diseño de [CanaryCheck.Verdict] sale de esa asimetría.
//
// # Lo que se puede afirmar y lo que no
//
// **Que la compuerta está rota se puede afirmar con certeza.** Si el canario fue
// tocado, el paquete cruzó, lo diga quien lo diga. Un invitado que informe
// "silencio" habiendo conectado queda desmentido por el propio host.
//
// **Que la compuerta funciona NO se puede afirmar con la misma fuerza**, y hay
// que decirlo en vez de esconderlo. "Nadie tocó el canario" es cierto tanto si
// la compuerta paró el paquete como si el invitado nunca marcó. Un invitado que
// colabora para ocultar una fuga puede quedarse quieto y contar que hubo
// silencio.
//
// Lo que acota ese hueco:
//
//   - Se le puede preguntar a VARIOS. Tendrían que mentir todos a la vez.
//   - Callarse no le da acceso a nadie: le quita información al host sobre un
//     acceso que ese invitado ya tendría. El techo de daño es esconder, jamás
//     abrir.
//   - Y la acción que se ofrece es idempotente, así que el host puede reponer la
//     protección sin necesidad de creerle a nadie.
//
// Por eso el estado bueno se llama [CanaryClean] y no "bloqueando": describe que
// no hay evidencia de fuga, que es lo que de verdad se sabe.

// CanaryVerdict es la conclusión de una comprobación con canario.
type CanaryVerdict uint8

const (
	// CanaryBlind es el CERO: no se comprobó. Que sea el cero es deliberado,
	// igual que en [VerdictBlind]: recién construido no puede leerse como bueno.
	CanaryBlind CanaryVerdict = iota
	// CanaryLeaking es que el paquete CRUZÓ. La compuerta no está conteniendo el
	// adaptador. Es lo único de este archivo que se afirma con certeza.
	CanaryLeaking
	// CanaryClean es que no hay evidencia de fuga y el invitado dice haber
	// marcado. Es el estado bueno y es más débil de lo que parece: ver arriba.
	CanaryClean
	// CanaryUnconfirmed es que nadie contestó, o que no había a quién
	// preguntarle. No dice nada de la compuerta.
	CanaryUnconfirmed
	// CanaryMismatch es que el informe del invitado NO cuadra con lo que vio el
	// host: dijo que conectó y al canario no lo tocó nadie.
	//
	// Se distingue en vez de meterlo en [CanaryUnconfirmed] porque significa una
	// cosa muy concreta, que alguien de la sala le está contando al daemon algo
	// que no pasó. No acusa a nadie por sí solo: también sale de una carrera
	// entre el aviso y el cierre del canario. Lo que hace es que la comprobación
	// no cuente como buena.
	CanaryMismatch
)

func (v CanaryVerdict) String() string {
	switch v {
	case CanaryLeaking:
		return "la compuerta no está bloqueando"
	case CanaryClean:
		return "sin evidencia de fuga"
	case CanaryUnconfirmed:
		return "sin confirmar"
	case CanaryMismatch:
		return "el informe no cuadra"
	default:
		return "sin comprobar"
	}
}

// AllCanaryVerdicts son todos, y esta lista es lo que los vuelve enumerables.
// Por lo mismo que [AllProbeKinds]: la mantiene al día un guardián de
// internal/arch.
func AllCanaryVerdicts() []CanaryVerdict {
	return []CanaryVerdict{CanaryBlind, CanaryLeaking, CanaryClean, CanaryUnconfirmed, CanaryMismatch}
}

// CanaryCheck es una comprobación entera, con las dos fuentes por separado.
type CanaryCheck struct {
	// MeasuredAt en CERO es que no se comprobó, igual que en [ExposureReport].
	MeasuredAt time.Time

	// Port es el puerto que se abrió, para poder decirlo en un log.
	Port uint16
	// Asked es a quién se le pidió. Va para que la pantalla pueda decir "desde
	// la PC de Fulano", que es lo que hace la frase creíble.
	Asked Nickname

	// Touched es lo que vio el HOST. Hecho propio, no falsificable.
	Touched bool

	// Answered es si el invitado llegó a contestar el mensaje.
	//
	// Va aparte de los dos resultados porque "no contestó" y "contestó que hubo
	// silencio" son cosas distintas: la primera no midió nada y la segunda sí
	// dice algo, aunque sea un dicho.
	Answered bool
	// ReportedTCP y ReportedUDP son lo que DIJO el invitado. Una pista.
	ReportedTCP ProbeOutcome
	ReportedUDP ProbeOutcome
}

// Blind dice si esto es una comprobación o nada.
func (c CanaryCheck) Blind() bool { return c.MeasuredAt.IsZero() }

// ReportedReach dice si el invitado afirma que ALGÚN protocolo llegó.
func (c CanaryCheck) ReportedReach() bool {
	return c.ReportedTCP.Reached() || c.ReportedUDP.Reached()
}

// Verdict concluye, y el ORDEN es todo el diseño.
//
// Primero el hecho propio, porque es el único que no se puede mentir. Después el
// informe, y solo para distinguir "no hay evidencia" de "no se comprobó".
func (c CanaryCheck) Verdict() CanaryVerdict {
	if c.Blind() {
		return CanaryBlind
	}
	// El hecho del host gana SIEMPRE. Un invitado que informe silencio habiendo
	// conectado queda desmentido acá mismo.
	if c.Touched {
		return CanaryLeaking
	}
	if !c.Answered {
		return CanaryUnconfirmed
	}
	// Dijo que llegó y al canario no lo tocó nadie. No puede ser una fuga,
	// porque no llegó nada; y tampoco cuenta como comprobación buena.
	if c.ReportedReach() {
		return CanaryMismatch
	}
	return CanaryClean
}

// NeedsAttention dice si esto tiene que llegar a la pantalla del usuario.
//
// [CanaryUnconfirmed] NO entra, y es a propósito: no haber podido comprobar es
// el estado normal de una sala donde todavía no hay nadie más, y encender un
// aviso ahí enseña al usuario a ignorar los avisos.
func (c CanaryCheck) NeedsAttention() bool {
	v := c.Verdict()
	return v == CanaryLeaking || v == CanaryMismatch
}
