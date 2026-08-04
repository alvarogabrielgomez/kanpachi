package domain

import (
	"fmt"
	"net/netip"
	"time"
)

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

// CanaryRequest es lo que el host le pide a un miembro: que le marque.
//
// # El campo que NO viaja, y es la invariante entera
//
// [CanaryRequest.Host] la rellena el ADAPTADOR con la dirección de la conexión
// que el invitado ya tiene abierta contra el host. **En el cable no existe un
// campo de dirección.**
//
// Sin esa regla, este mensaje convertiría el canal de la sala en un escáner de
// puertos por encargo: cualquiera de la sala le pediría a los demás que marcaran
// a una máquina de fuera, y el tráfico saldría de las casas de otros. Lo que
// impide eso no es una comprobación, es que no hay forma de expresarlo.
type CanaryRequest struct {
	// Host es a quién marcar, y sale de la CONEXIÓN. Ver arriba.
	Host netip.Addr
	Port uint16
	// Nonce ata la pregunta con la respuesta, y hace falta por UDP, que no
	// tiene conexión. Ver `daemon/adapter/canary`.
	Nonce CanaryNonce
}

// CanaryNonceSize es el largo del número, y es fijo a propósito: por UDP se lee
// exactamente esta cantidad, así que nada de lo que llegue de fuera decide
// cuánta memoria se toca.
const CanaryNonceSize = 16

// CanaryNonce es el número que ata la pregunta con la respuesta.
//
// Tipo con nombre y no un `[16]byte` suelto, para que la firma del puerto diga
// qué espera: un arreglo anónimo del mismo largo lo satisface cualquier cosa que
// pase por ahí, y este número decide si un datagrama cuenta como toque.
type CanaryNonce [CanaryNonceSize]byte

// Valid comprueba lo que el adaptador no puede dar por bueno.
//
// Un puerto en cero abriría un sondeo a "cualquier puerto", y una dirección sin
// rellenar significa que el adaptador no supo de qué conexión venía, que es
// justo el caso en el que este mensaje no se puede atender.
func (r CanaryRequest) Valid() error {
	if !r.Host.IsValid() {
		return fmt.Errorf("canario: sin dirección del host. Tiene que salir de la conexión, " +
			"y que llegue vacía significa que no se supo de qué conexión venía")
	}
	if r.Port == 0 {
		return fmt.Errorf("canario: puerto cero")
	}
	if r.Nonce == (CanaryNonce{}) {
		// Un número en ceros lo puede adivinar cualquiera, y por UDP el número
		// es lo único que distingue el eco del canario de un paquete suelto.
		return fmt.Errorf("canario: el número viene en ceros")
	}
	return nil
}

// CanaryReport es lo que el miembro contesta.
//
// [CanaryReport.From] también la rellena el adaptador desde la conexión, por lo
// mismo: sin eso, un miembro podría informar en nombre de otro y el host no
// tendría cómo saber quién midió.
//
// Es una PISTA y no una prueba. Lo que el host da por cierto es lo que vio él,
// que es [CanaryCheck.Touched].
type CanaryReport struct {
	From netip.Addr
	Port uint16
	TCP  ProbeOutcome
	UDP  ProbeOutcome
}

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

// CanaryAsked es alguien a quien se le preguntó en una ronda.
//
// Lleva la DIRECCIÓN y no solo el apodo, y esa elección es de seguridad. El
// apodo lo elige quien entra: [Nickname] valida largo y alfabeto y NO valida
// unicidad, así que un miembro puede tomar el de otro. Como clave para saber
// quién contestó, un apodo lo controla el atacante. La dirección la rellena el
// adaptador desde la conexión, y esa no se puede pedir.
type CanaryAsked struct {
	At   netip.Addr
	Name Nickname
}

// CanaryAnswer es lo que contestó UNO de los preguntados.
type CanaryAnswer struct {
	// From es la conexión por la que llegó, y es la clave de identidad.
	From netip.Addr
	// Name es para la pantalla, que tiene que poder decir "desde la PC de
	// Fulano". No sirve para identificar. Ver [CanaryAsked].
	Name Nickname

	TCP ProbeOutcome
	UDP ProbeOutcome
}

// Reach dice si este informe afirma que ALGÚN protocolo llegó.
func (a CanaryAnswer) Reach() bool { return a.TCP.Reached() || a.UDP.Reached() }

// Measured dice si este informe MIDIÓ algo.
//
// [ProbeFailed] en los dos protocolos significa que esa máquina no pudo ni
// preguntar: su adaptador estaba caído, no había ruta, o el intento no llegó a
// hacerse. Eso NO es silencio, es la ausencia de medición, y las dos cosas dicen
// lo contrario la una de la otra.
func (a CanaryAnswer) Measured() bool { return a.TCP != ProbeFailed || a.UDP != ProbeFailed }

// CanaryCheck es una comprobación entera, con las dos fuentes por separado.
type CanaryCheck struct {
	// MeasuredAt en CERO es que no se comprobó, igual que en [ExposureReport].
	MeasuredAt time.Time

	// Port es el puerto que se abrió, para poder decirlo en un log.
	Port uint16

	// Asked son TODOS a los que se preguntó, y el plural es del diseño.
	//
	// Preguntarle a uno sorteado lo elige un adversario que sostenga varias
	// membresías, porque el código de invitación no es secreto, no hay baneo y
	// volver a entrar es gratis.
	Asked []CanaryAsked

	// Touched es lo que vio el HOST. Hecho propio, no falsificable.
	Touched bool

	// Answers son los informes admitidos, COMO MUCHO UNO por dirección
	// preguntada. Esa unicidad la sostiene [CanaryCheck.Record] y la comprueba
	// [CanaryCheck.AllAnswered].
	Answers []CanaryAnswer
}

// Blind dice si esto es una comprobación o nada.
func (c CanaryCheck) Blind() bool { return c.MeasuredAt.IsZero() }

// Record admite un informe y dice si lo admitió. Es LA PUERTA, y sus tres
// negativas son de seguridad.
//
//	puerto ajeno      es el informe tardío de un canario ya cerrado
//	no preguntado     nadie informa por una pregunta que no se le hizo
//	ya contestó       es lo que sostiene [CanaryCheck.AllAnswered]
//
// La tercera cierra un agujero de verdad. La ronda cierra temprano cuando
// contestaron todos, así que sin deduplicar, UN miembro que mande tantos
// informes como miembros haya llena el contador, la ronda cierra en
// milisegundos, y los honestos nunca llegan a marcar. Eso no fabrica una alarma
// falsa: **esconde una fuga real**, que es la peor clase de fallo de este
// producto.
//
// Se queda con el PRIMERO por dirección, así que una inundación tampoco puede
// pisar una respuesta honesta que ya llegó.
func (c *CanaryCheck) Record(r CanaryReport) bool {
	if r.Port != c.Port {
		return false
	}
	name, asked := c.askedName(r.From)
	if !asked {
		return false
	}
	for _, a := range c.Answers {
		if a.From == r.From {
			return false
		}
	}
	c.Answers = append(c.Answers, CanaryAnswer{From: r.From, Name: name, TCP: r.TCP, UDP: r.UDP})
	return true
}

func (c CanaryCheck) askedName(at netip.Addr) (Nickname, bool) {
	for _, q := range c.Asked {
		if q.At == at {
			return q.Name, true
		}
	}
	return Nickname{}, false
}

// Answered dice si contestó ALGUIEN.
//
// Existe aparte de los resultados porque "no contestó" y "contestó que hubo
// silencio" son cosas distintas: la primera no midió nada y la segunda sí dice
// algo, por dicho que sea.
func (c CanaryCheck) Answered() bool { return len(c.Answers) > 0 }

// AllAnswered exige un REMITENTE DISTINTO por cada uno a los que se preguntó.
//
// Cuenta direcciones, jamás informes ni apodos, y la ronda la usa para cerrar
// temprano. Contar informes le dejaría a UN miembro cerrar la ronda solo. Contar
// apodos vale lo mismo que nada, porque un miembro puede tomar el de otro.
//
// La comprobación vive acá y no solo en el sitio que llama a propósito: quien
// agregue a Answers mañana sin pasar por [CanaryCheck.Record] reabriría el
// agujero en silencio, y un comentario no lo impide.
func (c CanaryCheck) AllAnswered() bool {
	if len(c.Asked) == 0 {
		return false
	}
	seen := make(map[netip.Addr]bool, len(c.Answers))
	for _, a := range c.Answers {
		seen[a.From] = true
	}
	for _, q := range c.Asked {
		if !seen[q.At] {
			return false
		}
	}
	return true
}

// ReportedReach dice si ALGUIEN afirma que llegó.
func (c CanaryCheck) ReportedReach() bool {
	for _, a := range c.Answers {
		if a.Reach() {
			return true
		}
	}
	return false
}

// ReportedMeasured dice si ALGUIEN midió.
//
// Existe porque no estaba y era un fallo real: una ronda en la que nadie pudo
// preguntar caía en la misma rama que una ronda callada y salía como "sin
// evidencia de fuga". Lo encontró una revisión adversaria del diseño el
// 2026-08-04, leyendo el código y no ejecutándolo.
func (c CanaryCheck) ReportedMeasured() bool {
	for _, a := range c.Answers {
		if a.Measured() {
			return true
		}
	}
	return false
}

// Verdict concluye, y el ORDEN es todo el diseño.
//
// Primero el hecho propio, porque es el único que no se puede mentir. Después el
// informe, y solo para distinguir "no hay evidencia" de "no se comprobó".
//
// [CanaryCheck.AllAnswered] NO entra acá, y es deliberado: exigir que contesten
// todos para dar por buena una ronda le daría a un miembro callado el veto sobre
// cada ronda, que es justo la palanca que el plural existe para quitar.
func (c CanaryCheck) Verdict() CanaryVerdict {
	if c.Blind() {
		return CanaryBlind
	}
	// El hecho del host gana SIEMPRE. Un invitado que informe silencio habiendo
	// conectado queda desmentido acá mismo.
	if c.Touched {
		return CanaryLeaking
	}
	if !c.Answered() {
		return CanaryUnconfirmed
	}
	// Todos contestaron "no pude ni preguntar". Eso no es silencio: es una ronda
	// que no midió, y contarla como buena sería sumar tranquilidad de una
	// comprobación que no ocurrió.
	if !c.ReportedMeasured() {
		return CanaryUnconfirmed
	}
	// Alguien dijo que llegó y al canario no lo tocó nadie. No puede ser una
	// fuga, porque no llegó nada; y tampoco cuenta como comprobación buena.
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

// ClearsAlarm dice si esta comprobación puede APAGAR la alarma.
//
// Solo una ronda que MIDIÓ y volvió limpia. Borrar una medición con una
// no-medición es esconder, y esconder es justo lo que el resto de este archivo
// existe para impedir.
//
// Deja fuera a [CanaryUnconfirmed], a [CanaryMismatch] y al cero, y los tres
// importan por separado:
//
//   - Sin lo primero, un miembro apaga una fuga YA DEMOSTRADA con solo quedarse
//     callado: su silencio deja la ronda sin confirmar y la alerta se iría. Eso
//     invierte la doctrina de arriba, donde el techo de daño de callarse es
//     esconder información y jamás borrarla.
//   - Sin lo último, una ronda que ni llegó a abrirse produce el cero, y el cero
//     apagaría la alarma él solo.
func (c CanaryCheck) ClearsAlarm() bool { return c.Verdict() == CanaryClean }
