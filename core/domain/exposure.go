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
	return []RuleClass{ClassGame, ClassRemoteControl, ClassOther}
}

func (c RuleClass) String() string {
	switch c {
	case ClassGame:
		return "juego"
	case ClassRemoteControl:
		return "control remoto"
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

// ClassifyForeign dice qué clase es una regla, a partir de su ejecutable.
//
// Es pura y vive en el dominio a propósito: la clasificación es POLÍTICA, y el
// adaptador solo sabe leer el almacén de reglas de Windows.
//
// `gameExe` es la ruta del ejecutable del perfil activo, y puede ir vacía
// cuando no hay juego activo. El control remoto gana sobre el juego: un
// ejecutable que esté en las dos listas es lo peligroso de las dos.
func ClassifyForeign(executable, gameExe string) RuleClass {
	return ClassifyForeignAgainst(executable, []string{gameExe})
}

// ClassifyForeignAgainst es la misma decisión contra TODOS los ejecutables de
// un perfil.
//
// Existe porque [Detect.Executables] es una lista: un juego reparte cliente,
// servidor dedicado y lanzador en binarios distintos, y una regla permisiva
// puede apuntar a cualquiera de ellos. Recorrer esa lista es política y por eso
// vive acá: el adaptador lee el almacén de reglas de Windows y no decide qué es
// peligroso.
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
	// Class decide el trato en la UI. La calcula [ClassifyForeign], que es
	// dominio, y jamás el adaptador.
	Class RuleClass
	// WasEnabled es el estado previo, para restaurar. Se persiste ANTES de
	// tocar nada, en suspended-rules.json, para poder deshacerlo tras una
	// salida sucia.
	WasEnabled bool
}

// Blocking dice si esta regla tiene que resolverse ANTES de abrir la sala.
//
// Solo el control remoto lo es. Una regla de más para el juego se muestra y se
// ofrece suspender; una que entrega teclado y ficheros no se despacha con una
// fila en una lista.
func (r ForeignRule) Blocking() bool { return r.Class == ClassRemoteControl }

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
	// AlertLobbyConflict: una red de esta máquina pisa el /24 fijo del
	// vestíbulo. Entrar a salas ajenas puede fallar, y sin este aviso el fallo
	// sería indiagnosticable. Ver "El vestíbulo tiene un /24 fijo" en
	// 03-arquitectura.md.
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
)

// AllAlertKinds son todas las alertas que el producto sabe levantar.
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
	}
}

// Sticky dice si la alerta sobrevive al refresco del módulo de exposición.
//
// Las pegajosas describen algo que PASÓ y que nadie va a volver a medir. Las
// demás son el resultado de una comprobación puntual y se recalculan enteras en
// cada barrido, así que conservarlas mostraría hallazgos de hace diez minutos
// como si fueran de ahora.
//
// Existe como predicado del dominio, con sus dos casos nombrados, para que
// agregar un tercero sea una edición visible y no un `if` más colado dentro del
// barrido.
//
// [AlertAuditFailed] NO es pegajosa, y es la que más invita a equivocarse: si lo
// fuera, se quedaría encendida para siempre tras el primer fallo de COM, porque
// solo [RoomState.DropAlerts] la quitaría y nadie tiene motivo para llamarla. Se
// recalcula, así que en cuanto la consulta vuelve a contestar, el aviso se va
// solo.
func (k AlertKind) Sticky() bool {
	return k == AlertLobbyConflict || k == AlertKickIncomplete
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
