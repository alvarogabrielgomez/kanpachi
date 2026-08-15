package domain

import (
	"net/netip"
	"strings"
)

// FirewallProfile son los tres perfiles del Firewall de Windows.
//
// Las reglas se aplican a los tres, siempre. No porque el adaptador pueda
// estar en cualquiera, que también, sino porque Kanpachi NO depende de lograr
// clasificar la red como privada: un adaptador sin puerta de enlace queda como
// "Red no identificada" y Windows lo mete en Público. Ver decisión 15.
type FirewallProfile uint8

const (
	ProfileDomain FirewallProfile = iota + 1
	ProfilePrivate
	ProfilePublic
)

func (p FirewallProfile) String() string {
	switch p {
	case ProfileDomain:
		return "dominio"
	case ProfilePrivate:
		return "privado"
	case ProfilePublic:
		return "público"
	default:
		return "perfil-inválido"
	}
}

// AllFirewallProfiles es a lo que se aplica todo lo que Kanpachi crea.
func AllFirewallProfiles() []FirewallProfile {
	return []FirewallProfile{ProfileDomain, ProfilePrivate, ProfilePublic}
}

// FirewallProfileState es si un perfil del firewall está encendido.
type FirewallProfileState struct {
	Profile FirewallProfile
	Enabled bool
}

// RuleClass dice DE QUÉ es una regla ajena, y de eso depende cómo la trata la
// UI.
//
// Existe porque tratarlas a todas igual era el bug: una regla de más para el
// juego molesta, y una de escritorio remoto entrega la máquina.
type RuleClass uint8

const (
	// ClassGame la dejó el instalador del juego o un diálogo de Windows. Es
	// una fila más de la lista de la decisión 12.
	ClassGame RuleClass = iota + 1
	// ClassRemoteControl da teclado, pantalla y sistema de archivos. La UI la
	// trata como BLOQUEANTE y ofrece suspenderla ANTES de abrir la sala.
	ClassRemoteControl
	// ClassOnOurAdapter es un permiso que alguien de fuera puso SOBRE UN
	// ADAPTADOR DE KANPACHI.
	//
	// # Por qué es una clase propia y no "otra"
	//
	// Porque las otras dos se deciden por el EJECUTABLE, y esta no tiene por
	// qué tener uno. La regla que motivó la clase abría cualquier protocolo
	// sobre `kanpachi0`, sin puerto, sin origen y sin aplicación: por el camino
	// del ejecutable no se clasifica, así que caía en `ClassOther` y no se
	// reportaba nunca.
	//
	// Lo que la hace distinta no es de quién es, es DÓNDE está. Un permiso
	// ajeno sobre la red virtual deshace la promesa central en la misma capa que
	// Kanpachi usa para conceder, y la compuerta no lo tapa: es un permiso, y
	// un permiso ajeno acotado a nuestro adaptador convive con nuestro bloqueo.
	//
	// Es BLOQUEANTE, igual que el control remoto. Ver [ForeignRule.Blocking].
	ClassOnOurAdapter
	ClassOther
)

// AllRuleClasses son todas las clases que el producto sabe distinguir.
//
// El conjunto es cerrado y esta lista es lo que lo vuelve ENUMERABLE, por lo
// mismo que [AllAlertKinds]: sin ella, agregar un valor al enum y olvidarse del
// resto no falla en ningún sitio. La clase viaja a la UI como "unknown", la
// pantalla la trata como una fila más, y si esa clase era bloqueante la sala se
// abre con el agujero puesto.
//
// La mantiene al día un guardián de internal/arch que cuenta las constantes del
// enum en el fuente y falla si esta lista tiene menos.
func AllRuleClasses() []RuleClass {
	return []RuleClass{ClassGame, ClassRemoteControl, ClassOnOurAdapter, ClassOther}
}

func (c RuleClass) String() string {
	switch c {
	case ClassGame:
		return "juego"
	case ClassRemoteControl:
		return "control remoto"
	case ClassOnOurAdapter:
		return "permiso ajeno sobre un adaptador de Kanpachi"
	case ClassOther:
		return "otra"
	default:
		return "clase-inválida"
	}
}

// remoteAccessExes son los ejecutables que entregan la máquina entera.
//
// # Por qué esta lista existe y por qué no es una lista de puertos
//
// La cuarentena de base bloquea el escritorio remoto ESTÁNDAR por puerto: 3389
// de RDP, 5985 y 5986 de WinRM, 445 de SMB. Eso alcanza para lo que trae
// Windows.
//
// No alcanza para estos. Parsec, Sunshine, Moonlight, RustDesk y AnyDesk
// escuchan en puertos ARBITRARIOS Y CONFIGURABLES, así que no hay lista negra
// de puertos que los cubra. Lo único estable es el ejecutable.
//
// # El agujero que cierra
//
// [port.FirewallPort.AuditForeign] recibía un [GameProfile] y buscaba por la
// ruta del ejecutable DEL JUEGO ACTIVO. Con eso, una regla permisiva de Parsec
// no se miraba nunca, y ese es el único camino conocido por el que un miembro
// de la sala consigue la máquina del host entera. El código de invitación no es
// un secreto y no hay baneo, así que el vecindario no es de confianza plena.
//
// Se compara por NOMBRE DE ARCHIVO en minúsculas, jamás por ruta completa: la
// ruta depende de dónde lo instaló cada uno.
var remoteAccessExes = [...]string{
	"parsecd.exe",
	"parsec.exe",
	"sunshine.exe",
	"moonlight.exe",
	"rustdesk.exe",
	"anydesk.exe",
	"teamviewer.exe",
	"teamviewer_service.exe",
	"vncserver.exe",
	"winvnc.exe",
	"tvnserver.exe",
	"dwservice.exe",
	"nomachine.exe",
}

// RemoteAccessExecutables es la lista que el adaptador tiene que buscar ADEMÁS
// del ejecutable del juego activo. Devuelve una copia.
func RemoteAccessExecutables() []string {
	out := make([]string, len(remoteAccessExes))
	copy(out, remoteAccessExes[:])
	return out
}

// ClassifyForeignAgainst dice qué clase es una regla, a partir de su ejecutable
// y de TODOS los ejecutables del perfil activo.
//
// Es pura y vive en el dominio a propósito: la clasificación es POLÍTICA, y el
// adaptador solo sabe leer el almacén de reglas de Windows.
//
// Recibe una lista y no un ejecutable porque [Detect.Executables] es una lista:
// un juego reparte cliente, servidor dedicado y lanzador en binarios distintos,
// y una regla permisiva puede apuntar a cualquiera de ellos. Puede ir vacía
// cuando no hay juego activo.
//
// El control remoto se comprueba PRIMERO y gana, así que un ejecutable que
// estuviera en las dos listas vuelve como lo peligroso de las dos.
func ClassifyForeignAgainst(executable string, gameExes []string) RuleClass {
	base := baseLower(executable)
	if base == "" {
		return ClassOther
	}
	for _, exe := range remoteAccessExes {
		if base == exe {
			return ClassRemoteControl
		}
	}
	for _, g := range gameExes {
		if b := baseLower(g); b != "" && base == b {
			return ClassGame
		}
	}
	return ClassOther
}

// baseLower saca el nombre de archivo en minúsculas de una ruta de Windows.
//
// Acepta las dos barras porque una regla del almacén puede traer cualquiera de
// las dos, y no usa filepath a propósito: este paquete compila y se testea en
// Linux, donde filepath.Base no corta por barra invertida.
func baseLower(ruta string) string {
	if i := strings.LastIndexAny(ruta, `\/`); i >= 0 {
		ruta = ruta[i+1:]
	}
	return strings.ToLower(strings.TrimSpace(ruta))
}

// ForeignRule es una regla de firewall que Kanpachi NO creó y que deja la
// máquina alcanzable por fuera de su control.
//
// La deja el instalador del juego, el de una herramienta de escritorio remoto,
// o un diálogo previo de Windows que el usuario despachó con "Permitir". Su
// efecto es que expulsar a alguien de la sala no lo tapa: sigue alcanzando lo
// que la regla abrió, desde la LAN de casa y desde la red virtual.
//
// La consulta va contra el ALMACÉN DE REGLAS del firewall, buscando por ruta
// de ejecutable. No enumera procesos, no detecta si el programa está corriendo
// y no le importa: la regla existe en disco haya o no partida.
type ForeignRule struct {
	Name       string
	Executable string
	Profiles   []FirewallProfile
	// Class decide el trato en la UI.
	//
	// Tres de las cuatro las decide [ClassifyForeignAgainst], que es dominio y
	// mira ejecutables. La cuarta la pone el adaptador de Windows, y tiene que
	// ser así: [ClassOnOurAdapter] no se deduce del ejecutable, se deduce de que
	// la regla nombre NUESTRA interfaz, que es un hecho del almacén de reglas de
	// Windows y no llega hasta acá. Lo que el adaptador NO decide es qué se hace
	// con cada clase, que es lo que [ForeignRule.Blocking] contesta.
	Class RuleClass
	// WasEnabled es el estado previo, para restaurar. Se persiste ANTES de
	// tocar nada, en suspended-rules.json, para poder deshacerlo tras una
	// salida sucia.
	WasEnabled bool
}

// Blocking dice si esta regla tiene que resolverse ANTES de abrir la sala.
//
// Dos clases lo son, por razones distintas y las dos del mismo peso. El control
// remoto entrega teclado, pantalla y ficheros, y eso no se despacha con una
// fila en una lista. Un permiso ajeno sobre nuestro adaptador deshace la
// promesa central en la misma capa que Kanpachi usa para conceder, y la
// compuerta no lo tapa: los dos son permisos, así que conviven.
//
// Una regla de más para el juego, en cambio, se muestra y se ofrece suspender.
func (r ForeignRule) Blocking() bool {
	return r.Class == ClassRemoteControl || r.Class == ClassOnOurAdapter
}

// BlockingForeign filtra las que hay que resolver antes de abrir la sala.
func BlockingForeign(rules []ForeignRule) []ForeignRule {
	var out []ForeignRule
	for _, r := range rules {
		if r.Blocking() {
			out = append(out, r)
		}
	}
	return out
}

// PortMapping es un mapeo que existe en el router del usuario.
//
// Es SOLO LECTURA y alimenta el módulo de alertas de la decisión 19. No hay
// tipo ni puerto para crear ni borrar mapeos, y esa ausencia es deliberada: lo
// que no existe en la interfaz no se puede llamar por error. El router del
// usuario no se toca nunca.
type PortMapping struct {
	Proto        Proto
	ExternalPort uint16
	InternalIP   netip.Addr
	InternalPort uint16
	Description  string
}

// ControlScope es dónde escucha el host y a quién le acepta qué.
//
// Son DOS direcciones porque son dos conversaciones con dos modelos de
// confianza distintos, y meterlas en una sola sería regalar la de la sala a
// cualquiera que tenga el código:
//
//	Lobby  la dirección del host en el vestíbulo. Es la PUERTA: acepta a
//	       cualquiera que haya llegado hasta ahí, o sea a cualquiera con el
//	       invite ID, y lo único que admite es un pedido de credencial. Nada
//	       más. Un mensaje de otro tipo por acá se descarta sin interpretarse.
//	Room   su IP en la red real. Acepta SOLO a los miembros presentes, y es
//	       por donde va todo lo demás: expulsión, cierre de sala, y por su
//	       sola existencia la presencia del host.
//
// Los invitados no tienen ninguna de las dos: marcan hacia afuera y no abren
// un puerto, así que su deny-all queda literalmente intacto.
//
// Este es el código que más revisión merece del proyecto: corre como SYSTEM y
// parsea entrada de gente semi-confiable. Tope de tamaño antes de
// deserializar, esquema cerrado sin tipos arbitrarios, tope de conexiones por
// dirección, y en Room el rechazo de toda IP que no esté en Members.
type ControlScope struct {
	Lobby   netip.Addr
	Room    netip.Addr
	Members []netip.Addr

	// Rendezvous es el nombre de la red de encuentro de ESTA sala, y viaja acá
	// porque es lo que el host firma al emitir una credencial.
	//
	// No es una dirección y por eso desentona en este struct. Está igual porque
	// es el único dato de la sala que el canal de control necesita saber y no
	// puede deducir: lo demás lo trae la conexión. La alternativa era un método
	// más en el puerto para decir una cadena.
	//
	// **Para qué se firma.** Los dos lados lo derivan del invite ID sin hablarse,
	// así que atarlo a la respuesta impide reusar una respuesta buena de otra
	// sala: la llave efímera del invitado vive toda la sesión, así que sin esto
	// una respuesta grabada en la sala A abriría igual en la sala B. Ver
	// [Rendezvous] y el canje del paso 5.
	RendezvousName string
}

// AlertKind es el conjunto cerrado de cosas que Kanpachi no controla y que
// anulan su promesa si nadie las mira.
type AlertKind uint8

const (
	// AlertFirewallOff: un perfil del Firewall de Windows está apagado. Con el
	// firewall apagado, las reglas de Kanpachi no restringen nada.
	AlertFirewallOff AlertKind = iota + 1
	// AlertRulesTampered: las reglas del grupo Kanpachi no están como se
	// aplicaron. Algo o alguien las cambió.
	AlertRulesTampered
	// AlertRouterMapping: el router tiene un mapeo hacia esta máquina. Kanpachi
	// no lo puso y no lo va a quitar, lo dice.
	AlertRouterMapping
	// AlertForeignRule: el propio juego dejó una regla que lo hace alcanzable
	// sin pasar por Kanpachi.
	AlertForeignRule
	// AlertLobbyConflict: una red de esta máquina pisa el /24 del vestíbulo de
	// ESTA sala. Entrar a salas ajenas puede fallar, y sin este aviso el fallo
	// sería indiagnosticable.
	//
	// El /24 dejó de ser fijo: sale de [Rendezvous.LobbySubnet], que reparte los
	// 256 de la mitad alta de RFC 2544 con un byte del identificador de red. Así
	// que el conflicto es contra la sala en la que se está, y no contra un rango
	// que valiera para todas.
	AlertLobbyConflict
	// AlertKickIncomplete: una expulsión no cerró sus dos capas.
	//
	// La lista de miembros ya no lo tiene y la máquina puede seguir
	// autorizándolo. Se avisa en vez de deshacer la mitad que sí funcionó,
	// porque deshacerla volvería a autorizar a quien el host acaba de echar.
	AlertKickIncomplete
	// AlertAuditFailed: el módulo de exposición no pudo comprobar lo que existe
	// para comprobar.
	//
	// **No dice que algo esté mal. Dice que nadie está mirando**, y esa
	// distinción es toda la alerta. Las otras cinco son hallazgos; esta es la
	// ausencia de hallazgo, que sin decirla se ve idéntica a "todo en orden".
	//
	// Existe porque el módulo que avisa que la promesa se rompió no podía avisar
	// que él mismo había dejado de mirar: con las consultas fallando, la pantalla
	// quedaba en verde. Cubre las dos comprobaciones que sostienen la promesa, el
	// estado del firewall y las reglas propias. La del router NO entra: no
	// contesta en la mayoría de las máquinas, y una alerta encendida en todas
	// partes deja de significar algo.
	AlertAuditFailed
	// AlertGateLeaking: la compuerta NO está conteniendo el adaptador, y se
	// intentó reponerla sin que aguantara.
	//
	// Es la única alerta que sale de una medición POR LA RED, y la única que ve
	// el caso que ninguna otra puede ver: el filtro existe por su clave y no
	// contiene nada. [AlertRulesTampered] pregunta si el filtro está; esta ve el
	// paquete cruzar.
	//
	// La levanta el socket propio del host y jamás el informe de un miembro. Un
	// mensaje se puede mentir; que alguien haya llegado hasta el oyente, no.
	AlertGateLeaking
	// AlertGameLost: the room reopened and its game could not be restored.
	//
	// **It is the only alert that talks about ports that are NOT open**, which is
	// why it sits oddly in this list: every other one reports too much exposure.
	// It belongs here anyway, because it shares the one thing that defines an
	// alert in this product, which is something that voids the promise if nobody
	// looks at it.
	//
	// The case behind it is the headless host. The room comes back by itself
	// after a restart and the profile it had active is no longer in the
	// catalogue, so it comes back with no game: people join, the server does not
	// answer, and the only thing saying so was a log line on a machine nobody
	// looks at. See `Session.restoreGameLocked`.
	//
	// Sticky, and it cannot be otherwise: it describes something that happened
	// while reopening, and nothing in the sweep measures it again. What clears it
	// is picking a game, which is exactly what has to be done.
	AlertGameLost
)

// AllAlertKinds son todos los valores del enum, que NO es lo mismo que las que
// el producto sabe levantar.
//
// [AlertForeignRule] está declarada, listada acá, y con nombre en la API local,
// y nadie la levanta. Puede ser hueco de función y puede ser código muerto; lo
// que no puede es que esta lista mienta sobre lo uno o lo otro. Está anotado
// sin decidir en 07-futuro.md.
//
// El conjunto es cerrado, y esta lista es lo que lo vuelve ENUMERABLE. De eso
// dependen dos cosas que no son cosméticas: el test que exige que cada alerta
// tenga nombre en la API local, y el que exige que la UI tenga un texto para
// cada una.
//
// Sin la lista, agregar un valor al enum y olvidarse del resto no falla en
// ningún sitio. La alerta viaja como "unknown", la pantalla no la pinta, y el
// módulo de exposición avisa al vacío. Ya pasó una vez, con [AlertAuditFailed].
//
// Mantenerla al día no es disciplina: un guardián de internal/arch cuenta las
// constantes del enum en el fuente y falla si esta lista tiene menos.
func AllAlertKinds() []AlertKind {
	return []AlertKind{
		AlertFirewallOff,
		AlertRulesTampered,
		AlertRouterMapping,
		AlertForeignRule,
		AlertLobbyConflict,
		AlertKickIncomplete,
		AlertAuditFailed,
		AlertGateLeaking,
		AlertGameLost,
	}
}

// Sticky dice si la alerta sobrevive al refresco del módulo de exposición.
//
// Las pegajosas describen algo que PASÓ y que nadie va a volver a medir. Las
// demás son el resultado de una comprobación puntual y se recalculan enteras en
// cada barrido, así que conservarlas mostraría hallazgos de hace diez minutos
// como si fueran de ahora.
//
// Existe como predicado del dominio, con sus casos nombrados, para que agregar
// uno sea una edición visible y no un `if` más colado dentro del barrido.
//
// [AlertAuditFailed] NO es pegajosa, y es la que más invita a equivocarse: si lo
// fuera, se quedaría encendida para siempre tras el primer fallo de COM, porque
// solo [RoomState.DropAlerts] la quitaría y nadie tiene motivo para llamarla. Se
// recalcula, así que en cuanto la consulta vuelve a contestar, el aviso se va
// solo.
// [AlertGateLeaking] SÍ es pegajosa, y por el motivo opuesto: describe algo
// que el host MIDIÓ desde la red, y nada de lo que corre en el barrido lo vuelve
// a medir. Sin pegajosa, el barrido la borraría sesenta segundos después de la
// única prueba que este producto sabe producir. Lo que la apaga es una ronda del
// canario que midió y volvió limpia.
// [AlertGameLost] IS sticky, for the same reason as the canary: it reports what
// happened at the instant of reopening the room, and the sweep reopens nothing.
// What clears it is picking a game, which is the action that fixes the case.
func (k AlertKind) Sticky() bool {
	return k == AlertLobbyConflict || k == AlertKickIncomplete ||
		k == AlertGateLeaking || k == AlertGameLost
}

// Alert es un hallazgo del módulo de exposición.
//
// Viaja dentro de Status() y no por un canal aparte: el módulo publica su
// último resultado y Status() lo arrastra, así que una alerta nunca puede
// bloquear ni retrasar una respuesta. Ninguna comprobación devuelve error
// fatal: devuelven hallazgos.
type Alert struct {
	Kind   AlertKind
	Detail string
}

// HasAlert dice si hay alguna alerta de ese tipo encendida.
func (r *RoomState) HasAlert(k AlertKind) bool {
	for _, a := range r.Alerts {
		if a.Kind == k {
			return true
		}
	}
	return false
}

// DropAlerts quita todas las alertas de un tipo.
//
// La usa el caso de uso que ARREGLA lo que la alerta denunciaba. Una alerta
// pegajosa que nadie pueda quitar se queda para siempre, y una alerta eterna
// deja de ser información.
func (r *RoomState) DropAlerts(k AlertKind) {
	if len(r.Alerts) == 0 {
		return
	}
	kept := make([]Alert, 0, len(r.Alerts))
	for _, a := range r.Alerts {
		if a.Kind != k {
			kept = append(kept, a)
		}
	}
	r.Alerts = kept
}
