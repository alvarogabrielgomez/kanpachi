package domain

import (
	"net/netip"
	"sort"
	"time"
)

// El sondeo desde otra máquina: lo único que mide la promesa por fuera.
//
// # Por qué hace falta si ya está [ExposureReport]
//
// Porque aquel lee lo que ESTA máquina tiene configurado, preguntándole a la
// misma Windows que aplica las reglas. Es una medición honesta y no puede
// contestar la pregunta del producto, que es si desde otra PC se llega. Un
// alcance de filtro que dejó de casar, una regla del sistema que nadie miró, o
// una herramienta que abrió su propio hueco, se ven todos igual desde dentro:
// verde.
//
// Este es el único camino que atraviesa la red de verdad.
//
// # Cómo se lee lo que contesta Windows, MEDIDO y no supuesto
//
// El 2026-08-04, con el droplet marcando por Tailscale a esta máquina, sobre el
// puerto 45999 y con el firewall encendido:
//
//	sin regla, sin oyente       silencio
//	CON regla, sin oyente       silencio
//	CON regla, con oyente       conecta
//
// La segunda fila es la que manda: Windows NO devuelve RST hacia dentro aunque
// el firewall permita el puerto, que es el modo sigiloso del Firewall de
// Windows. Y de ahí sale la regla de lectura de todo este archivo:
//
//	conecta    el firewall lo dejó pasar Y hay un programa escuchando
//	silencio   o el firewall lo bloquea, o no hay nada escuchando. NO SE
//	           DISTINGUEN, y por eso el silencio no prueba casi nada
//
// La consecuencia de diseño es fuerte y hay que respetarla: **un puerto de juego
// callado NO significa que esté cerrado**, significa que el juego no está
// abierto. Una versión anterior de esto tenía un veredicto "faltan puertos" que
// se habría encendido en falso cada vez que alguien mirara la pantalla sin el
// juego arrancado. Se borró cuando la medición dijo que no se puede distinguir.
//
// # Lo que sí prueba, y por qué alcanza
//
// Una respuesta en un puerto que nadie pidió es prueba POSITIVA de exposición, y
// no depende de distinguir nada. Ese es el hallazgo que este sondeo existe para
// dar, y la primera corrida ya lo dio: el 445 de esta máquina contestó desde
// otra PC, o sea el compartir archivos de Windows alcanzable por la red virtual.
//
// # Solo TCP, y hay que decirlo
//
// Por UDP no hay pregunta que hacer sin hablar el idioma del programa que
// escucha: un puerto bloqueado y uno abierto que no contesta se ven idénticos.
// Sondear UDP produciría silencio siempre, o sea un verde que no midió nada.
//
// Lo que se mide por TCP se traslada a UDP por una razón concreta y verificable
// en el código: los bloqueos de la compuerta se emiten con condición de
// adaptador y de rango, y NINGUNA de protocolo (ver `SpecsFor` en
// daemon/adapter/firewall/wfp). La compuerta no mira el protocolo, así que lo
// que le hace a un TCP se lo hace igual a un UDP. Eso NO cubre las reglas del
// Firewall de Windows, que sí son por protocolo, y por eso la pantalla lo dice.

// ProbeKind dice POR QUÉ un puerto está en la lista, que es lo mismo que decir
// qué prueba su respuesta.
type ProbeKind uint8

const (
	// ProbeReference es el canal de la sala. Es el único que TIENE que
	// contestar, porque el host lo escucha mientras la sala esté abierta.
	//
	// Es lo que convierte al resto en una medición: sin una referencia viva,
	// "no contesta nadie" se lee igual estando la PC apagada que estando
	// blindada, y la pantalla estaría afirmando lo que no sabe.
	ProbeReference ProbeKind = iota + 1
	// ProbeForbidden es lo que NO tiene que contestar: los puertos que la
	// cuarentena tapa y los que traen de fábrica las herramientas de escritorio
	// remoto. Una respuesta acá es LA ALARMA.
	ProbeForbidden
	// ProbeGame es un puerto que el juego activo pidió abrir. Es informativo y
	// nunca cambia el veredicto: que conteste prueba que el camino funciona de
	// punta a punta, y que calle no prueba nada porque el juego puede estar
	// cerrado.
	ProbeGame
)

func (k ProbeKind) String() string {
	switch k {
	case ProbeReference:
		return "referencia"
	case ProbeForbidden:
		return "prohibido"
	case ProbeGame:
		return "juego"
	default:
		return "clase-inválida"
	}
}

// AllProbeKinds son todas las clases, y esta lista es lo que las vuelve
// ENUMERABLES.
//
// Por lo mismo que [AllRuleClasses]: sin ella, agregar un valor al enum y
// olvidarse del resto no falla en ningún sitio. La mantiene al día un guardián
// de internal/arch.
func AllProbeKinds() []ProbeKind {
	return []ProbeKind{ProbeReference, ProbeForbidden, ProbeGame}
}

// ProbeOutcome es lo que contestó un puerto.
type ProbeOutcome uint8

const (
	// ProbeAnswered es que hubo apretón de manos completo.
	ProbeAnswered ProbeOutcome = iota + 1
	// ProbeRefused es un RST: el paquete llegó a la pila y rebotó.
	//
	// **En Windows con el firewall encendido esto casi no ocurre**, y está
	// medido: el modo sigiloso calla en vez de rebotar. Se conserva porque el
	// dominio no puede depender de que un sistema operativo se comporte de una
	// manera, y porque cuenta como prueba de que el paquete LLEGÓ, que es lo que
	// mide el veredicto.
	ProbeRefused
	// ProbeSilent es que no contestó nadie antes del plazo.
	ProbeSilent
	// ProbeFailed es que el intento no se pudo ni hacer. No es un resultado
	// sobre el otro extremo: es que esta máquina no pudo preguntar.
	ProbeFailed
)

func (o ProbeOutcome) String() string {
	switch o {
	case ProbeAnswered:
		return "contesta"
	case ProbeRefused:
		return "rebota"
	case ProbeSilent:
		return "silencio"
	case ProbeFailed:
		return "no se pudo preguntar"
	default:
		return "resultado-inválido"
	}
}

// AllProbeOutcomes son todos los resultados, por lo mismo que [AllProbeKinds].
func AllProbeOutcomes() []ProbeOutcome {
	return []ProbeOutcome{ProbeAnswered, ProbeRefused, ProbeSilent, ProbeFailed}
}

// Reached dice si el paquete llegó a la pila TCP del otro lado.
//
// Es la única lectura que el veredicto usa, y es la que no depende de si hay un
// programa escuchando: contestar y rebotar son las dos formas de que el firewall
// haya dejado pasar.
func (o ProbeOutcome) Reached() bool { return o == ProbeAnswered || o == ProbeRefused }

// ProbeTarget es un puerto de la lista, con por qué está y cómo se llama para
// quien lee.
//
// NO tiene campo de protocolo, y la ausencia es deliberada: se sondea TCP y
// nada más. Un campo invitaría a poner UDP ahí, y por UDP el resultado sería
// silencio siempre, o sea un verde que no midió nada.
type ProbeTarget struct {
	Port  uint16
	Kind  ProbeKind
	Label string
}

// ProbeResult es un objetivo con lo que contestó.
type ProbeResult struct {
	ProbeTarget
	Outcome ProbeOutcome
	// RTT es cuánto tardó en contestar. Solo vale con [ProbeAnswered]: en el
	// silencio lo que mide es el plazo, que ya se sabe.
	RTT time.Duration
}

// ProbeVerdict es la frase de una línea que resume el sondeo.
type ProbeVerdict uint8

const (
	// VerdictBlind es el CERO: no se midió. Que sea el cero es deliberado, igual
	// que en [BlindExposure]: un informe recién construido no puede leerse como
	// "está todo bien".
	VerdictBlind ProbeVerdict = iota
	// VerdictLeaky es que algo que nadie pidió contestó. Es prueba positiva de
	// exposición y gana sobre todo lo demás.
	VerdictLeaky
	// VerdictUnreachable es que ni la referencia contestó, así que el sondeo no
	// prueba nada. NO es lo mismo que "está cerrado", y confundirlos sería
	// exactamente la mentira que este tipo existe para impedir.
	VerdictUnreachable
	// VerdictSealed es que la referencia contesta y nada de lo prohibido lo
	// hace.
	VerdictSealed
)

func (v ProbeVerdict) String() string {
	switch v {
	case VerdictLeaky:
		return "hay algo abierto"
	case VerdictUnreachable:
		return "no se pudo alcanzar"
	case VerdictSealed:
		return "cerrado"
	default:
		return "sin medir"
	}
}

// AllProbeVerdicts son todos los veredictos, por lo mismo que [AllProbeKinds].
func AllProbeVerdicts() []ProbeVerdict {
	return []ProbeVerdict{VerdictBlind, VerdictLeaky, VerdictUnreachable, VerdictSealed}
}

// ProbeReport es un sondeo entero contra una máquina.
type ProbeReport struct {
	// Target es la IP virtual a la que se marcó, y Name quién es para que la
	// pantalla no tenga que enseñar un número.
	Target netip.Addr
	Name   Nickname

	// MeasuredAt en CERO significa que no se midió, igual que en
	// [ExposureReport]. Es lo que separa una pantalla que informa de una que
	// miente.
	MeasuredAt time.Time
	Results    []ProbeResult
}

// Blind dice si esto es un sondeo o una ceguera.
func (r ProbeReport) Blind() bool { return r.MeasuredAt.IsZero() }

// Verdict resume el sondeo, y el ORDEN de las tres preguntas es la parte que
// importa.
//
// Primero la fuga, porque una respuesta en un puerto prohibido es prueba
// positiva y no necesita referencia: si algo contestó, la máquina está viva y
// está abierta, y las dos cosas se saben de la misma respuesta.
//
// Después la referencia, porque sin ella el silencio de todo lo demás no
// distingue una PC blindada de una apagada.
//
// Los puertos de juego no entran en el veredicto NUNCA, y la razón está medida
// arriba: su silencio significa que el juego está cerrado, que es el estado
// normal mientras alguien mira esta pantalla.
func (r ProbeReport) Verdict() ProbeVerdict {
	if r.Blind() {
		return VerdictBlind
	}

	reference := false
	for _, res := range r.Results {
		switch res.Kind {
		case ProbeForbidden:
			if res.Outcome.Reached() {
				return VerdictLeaky
			}
		case ProbeReference:
			if res.Outcome.Reached() {
				reference = true
			}
		}
	}
	if !reference {
		return VerdictUnreachable
	}
	return VerdictSealed
}

// Leaks son los puertos prohibidos que contestaron, que es la lista que la
// pantalla enseña cuando el veredicto es [VerdictLeaky].
func (r ProbeReport) Leaks() []ProbeResult {
	out := make([]ProbeResult, 0, len(r.Results))
	for _, res := range r.Results {
		if res.Kind == ProbeForbidden && res.Outcome.Reached() {
			out = append(out, res)
		}
	}
	return out
}

// forbiddenProbeLabels le pone nombre legible a cada puerto de
// [forbiddenPorts], y el vacío marca los que NO se pueden sondear por TCP.
//
// Es un mapa sobre aquella lista y no una copia suya, y esa dependencia es a
// propósito: un guardián de este paquete falla si un puerto prohibido nuevo
// llega acá sin entrada. Sin eso, ampliar la cuarentena dejaría el sondeo
// midiendo la de ayer, en silencio.
var forbiddenProbeLabels = map[uint16]string{
	22:   "SSH",
	135:  "RPC de Windows",
	137:  "", // NetBIOS de nombres, UDP
	138:  "", // NetBIOS de datagramas, UDP
	139:  "NetBIOS",
	445:  "compartir archivos (SMB)",
	3389: "Escritorio remoto",
	3702: "", // WS-Discovery, UDP
	5357: "descubrimiento de dispositivos",
	5358: "descubrimiento de dispositivos",
	5985: "administración remota (WinRM)",
	5986: "administración remota (WinRM)",
}

// remoteAccessProbePorts son los puertos DE FÁBRICA de las herramientas de
// [remoteAccessExes], y hay que entender qué son y qué no.
//
// **No son una lista negra y no sirven para bloquear.** Esas herramientas
// escuchan donde el usuario les diga, y por eso lo que se audita de ellas es el
// ejecutable y no el puerto. Esto es otra cosa: un muestreo. Encuentra al que
// dejó la configuración por defecto, que es la enorme mayoría, y no encuentra al
// que la cambió.
//
// Decirlo en la pantalla es obligatorio. Un "no hay nada abierto" que en verdad
// significa "no probé el puerto donde estaba" es la clase de verde que hace que
// toda la pantalla deje de valer.
//
// Parsec no está y no es un olvido: habla UDP, y por UDP no hay pregunta que
// hacer. Es la limitación que la pantalla nombra con todas las letras.
var remoteAccessProbePorts = [...]struct {
	Port uint16
	Tool string
}{
	{4000, "NoMachine"},
	{5900, "VNC"},
	{5938, "TeamViewer"},
	{7070, "AnyDesk"},
	{21116, "RustDesk"},
	{21118, "RustDesk"},
	{47984, "Sunshine"},
	{47989, "Sunshine"},
	{48010, "Sunshine"},
}

// ProbeTargets arma la lista de puertos a marcar contra el HOST de la sala.
//
// Contra el host y no contra cualquiera, y la razón es la referencia: el canal
// de la sala solo escucha ahí. Sondear a un invitado daría silencio en todo,
// que es a la vez lo que tiene que pasar y lo que pasa con la PC apagada, y el
// sondeo no tendría cómo distinguirlos.
//
// `game` es el perfil activo, y en cero está bien: una sala sin juego se sondea
// igual, con la referencia y la lista de prohibidos, que es de donde sale el
// valor.
//
// La lista es corta y FIJA a propósito. Esto no es un escáner de puertos: son
// los que la promesa del producto nombra, y ampliarlo a barrer rangos
// convertiría un botón de diagnóstico en otra cosa.
func ProbeTargets(game GameProfile) []ProbeTarget {
	// La precedencia es referencia, juego, prohibido, y resuelve un choque real:
	// un juego que pida uno de los puertos de fábrica de arriba lo tiene ABIERTO
	// a propósito, y dejarlo como prohibido encendería la alarma sobre algo que
	// el usuario pidió. Los prohibidos de verdad, los de [forbiddenPorts],
	// ningún perfil los puede pedir, así que ese choque no existe.
	byPort := make(map[uint16]ProbeTarget, len(forbiddenProbeLabels)+len(remoteAccessProbePorts)+8)

	for port, label := range forbiddenProbeLabels {
		if label == "" {
			continue
		}
		byPort[port] = ProbeTarget{Port: port, Kind: ProbeForbidden, Label: label}
	}
	for _, t := range remoteAccessProbePorts {
		byPort[t.Port] = ProbeTarget{Port: t.Port, Kind: ProbeForbidden, Label: t.Tool}
	}

	for _, r := range game.HostPorts {
		if r.Proto != ProtoTCP && r.Proto != ProtoBoth {
			continue
		}
		// El primero del rango y no el rango entero: un rango largo llenaría la
		// pantalla de filas que dicen lo mismo, y alargaría el sondeo sin
		// agregar información. Que un puerto del rango conteste ya prueba que el
		// camino funciona.
		byPort[r.From] = ProbeTarget{Port: r.From, Kind: ProbeGame, Label: game.Name}
	}

	byPort[ControlPort] = ProbeTarget{
		Port:  ControlPort,
		Kind:  ProbeReference,
		Label: "el canal de la sala",
	}

	out := make([]ProbeTarget, 0, len(byPort))
	for _, t := range byPort {
		out = append(out, t)
	}
	// Orden estable: dos sondeos seguidos contra la misma máquina tienen que
	// producir la misma pantalla, y el recorrido de un mapa en Go no lo es.
	sort.Slice(out, func(i, j int) bool { return out[i].Port < out[j].Port })
	return out
}
