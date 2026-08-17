// Package usecase orquesta las intenciones del producto, una por archivo.
//
// Recibe los puertos por constructor y no construye ningún adaptador: el
// cableado vive en daemon/service, que es el único sitio del proyecto que
// conoce a la vez el dominio y las implementaciones concretas.
//
// Todo lo de acá corre sin admin, sin red y sin Windows, con adaptadores
// falsos. Esa es la métrica que dice si la arquitectura sigue sana.
package usecase

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/core/port"
)

var (
	// ErrBusy es que ya hay sala. Entrar a otra exige salir de esta, y hacerlo
	// solo sería cortar el túnel con la partida viva.
	ErrBusy = errors.New("ya estás en una sala")
	// ErrNoRoom es que no hay sala y la operación necesita una.
	ErrNoRoom = errors.New("no estás en ninguna sala")
	// ErrNotHost cubre las tres operaciones que solo el host puede pedir. Un
	// invitado que las invoque recibe esto por la API, no un error de red.
	ErrNotHost = errors.New("solo el host puede hacer eso")
	// ErrUnknownGame es pedir un juego que no está en el catálogo efectivo. La
	// API solo puede aplicar perfiles del catálogo, y esta es esa frontera: no
	// existe la operación "abrir puerto arbitrario".
	ErrUnknownGame = errors.New("ese juego no está en el catálogo")
	// ErrNotAMember es intentar expulsar a alguien que no está.
	ErrNotAMember = errors.New("esa dirección no es de ningún miembro presente")
	// ErrSelfKick es intentar expulsarse a uno mismo. Se rechaza en vez de
	// interpretarse como salir: son dos intenciones distintas y confundirlas
	// haría que un clic mal dado en la lista te eche de tu propia sala.
	ErrSelfKick = errors.New("no puedes expulsarte a ti mismo")
	// ErrShadowsBuiltin es guardar un perfil propio con el id de uno que vino
	// con la app. No se sobreescribe en silencio: la UI pregunta.
	ErrShadowsBuiltin = errors.New("ya hay un perfil de ese juego que vino con la app")
	// ErrNotPlayed es marcar como verificado un juego que esta máquina no vio
	// jugarse. Es la puerta de "no se puede marcar a mano".
	ErrNotPlayed = errors.New("ese juego no estuvo activo en una sala con más gente")

	// ErrNoSuchRoom es que el registro dice que ese código NO EXISTE.
	//
	// Es una respuesta y no un fallo: el registro contestó, y contestó que
	// nunca emitió ese invite ID, o que su fijado venció y se barrió. Merece
	// centinela propio porque es lo único que permite fallar en el primer
	// segundo en vez de al final de un minuto de reintentos contra un vestíbulo
	// donde no espera nadie.
	ErrNoSuchRoom = errors.New("ese código no existe en el registro")

	// ErrNoRegistry es que el registro NO CONTESTA, que es distinto de contestar
	// que no.
	//
	// # Por qué corta, si el registro es solo presentación
	//
	// Porque dejó de serlo, y hay que decirlo entero. El seed es el punto de
	// encuentro: los códigos los emite él, los resuelve él, y su máquina es la
	// que aparece como par en la configuración del motor. Sin él no hay adónde
	// llegar.
	//
	// Lo que había antes era peor de lo que parece. Crear una sala con el
	// registro caído generaba un invite ID aquí mismo, y ese código **no le
	// servía a nadie**: el invitado le pregunta al mismo registro por defecto,
	// le contestan que no existe, y lo rechaza antes de arrancar el motor. El
	// host se quedaba con un código de aspecto normal, lo repartía, y nadie
	// entraba.
	//
	// Vale para las dos puntas, crear y entrar, y por eso vive acá y no en uno
	// de los dos ficheros.
	ErrNoRegistry = errors.New("el registro de Kanpachi no contesta")

	// ErrCanceled es que el usuario canceló la operación mientras corría.
	//
	// No es un error de nada: es la respuesta a un botón. Va aparte para que la
	// pantalla pueda distinguirlo y NO pintar un aviso de fallo encima de algo
	// que la persona acaba de pedir.
	ErrCanceled = errors.New("la operación se canceló")

	// ErrKickPartial es que la expulsión se aplicó a medias. Envuelve a los dos
	// de abajo, y NO significa que no haya pasado nada: significa que una de
	// las dos capas de la decisión 22 quedó sin cerrar y la otra sí cerró.
	ErrKickPartial = errors.New("la expulsión se aplicó a medias")
	// ErrRevokeFailed es que el motor no pudo cerrarle la sesión. Sigue sin
	// alcanzar ningún puerto, porque el firewall ya no lo autoriza.
	ErrRevokeFailed = errors.New("el motor no pudo cerrarle la sesión")
	// ErrRulesFailed es que las reglas no se recalcularon. Ya salió de la red,
	// porque su credencial sí se revocó.
	ErrRulesFailed = errors.New("las reglas del firewall no se recalcularon")
)

// Deps son los puertos que la sesión necesita.
//
// Struct y no once parámetros posicionales: con once, agregar uno rompe todas
// las llamadas y equivocarse de orden entre dos interfaces compila igual.
type Deps struct {
	Engine   port.EnginePort
	Firewall port.FirewallPort
	NetCfg   port.NetConfigPort
	Routes   port.RoutingTable
	Store    port.CatalogStore
	State    port.StateStore
	Library  port.GameLibrary
	// Directories entrega el registro con el que hablar en cada caso, y no es un
	// registro suelto porque ya no hay uno solo: al entrar manda el del código y
	// al crear el de esta máquina. Ver [port.RoomDirectories].
	Directories port.RoomDirectories
	Control     port.ControlChannel
	Audit       port.ExposureAudit
	Inspector   port.SocketInspector
	Prober      port.Prober
	Canary      port.CanaryPort
	Clock       port.Clock
	Log         port.Logger

	// Rand es de dónde salen el invite ID de respaldo, la identidad de la red
	// real, la subred y la clave de la tarjeta. Entra por parámetro y no se
	// toma de crypto/rand directamente para que los tests puedan fijar un
	// resultado sin dejar de ejercitar el mismo camino.
	Rand io.Reader

	// Quarantine es en qué sistema corre esto, y de eso depende qué puertos
	// cierra la cuarentena de base.
	//
	// # Por qué se inyecta en vez de mirarse
	//
	// Porque este paquete no mira el sistema: es la regla que hace que core
	// corra en el CI de Linux sin privilegios. Lo mismo que ya pasa con el
	// resolver de adaptadores del firewall, que también es un hecho del sistema
	// y también entra por parámetro.
	//
	// # Por qué el valor cero NO se completa solo
	//
	// Porque los dos sistemas cierran listas distintas, y elegir una por defecto
	// significa que un cableado que se olvide de ponerlo aplica la del otro sin
	// que nada falle. En Linux eso cerraría el puerto por el que el operador
	// administra su servidor. Falta esto y el daemon no arranca. Ver `validate`.
	Quarantine domain.QuarantineSystem

	// Progress es el diario de la operación larga en curso. Ver [Journal].
	//
	// **Es el ÚNICO opcional de esta lista**, y por eso no está en `validate`:
	// nada del producto depende de él, se mira cuando algo tardó de más o
	// falló. Nil se sustituye por uno propio en [NewSession] para que nadie
	// tenga que comprobarlo en cada llamada.
	//
	// Entra por aquí y no se crea adentro porque los ADAPTADORES escriben en
	// el mismo diario: quien sabe que el motor tardó doce segundos en tomar
	// dirección es el adaptador del motor, no este paquete.
	Progress *Journal
}

// validate comprueba que estén todos los puertos.
//
// Ninguno es opcional. Un daemon al que le falte el firewall arrancaría feliz
// y abriría una sala sin cuarentena, que es el peor fallo posible de este
// producto y el más difícil de notar desde fuera.
func (d Deps) validate() error {
	faltan := make([]string, 0, 15)
	nombrar := func(nombre string, presente bool) {
		if !presente {
			faltan = append(faltan, nombre)
		}
	}
	nombrar("Engine", d.Engine != nil)
	nombrar("Firewall", d.Firewall != nil)
	nombrar("NetCfg", d.NetCfg != nil)
	nombrar("Routes", d.Routes != nil)
	nombrar("Store", d.Store != nil)
	nombrar("State", d.State != nil)
	nombrar("Library", d.Library != nil)
	nombrar("Directories", d.Directories != nil)
	nombrar("Control", d.Control != nil)
	nombrar("Audit", d.Audit != nil)
	nombrar("Inspector", d.Inspector != nil)
	nombrar("Canary", d.Canary != nil)
	nombrar("Prober", d.Prober != nil)
	nombrar("Clock", d.Clock != nil)
	nombrar("Log", d.Log != nil)
	nombrar("Rand", d.Rand != nil)
	// El único que no se comprueba contra nil: es un entero, y su cero significa
	// "nadie lo puso". Dejarlo pasar aplicaría la cuarentena del otro sistema.
	nombrar("Quarantine", d.Quarantine != 0)

	if len(faltan) > 0 {
		return fmt.Errorf("usecase: el cableado no está completo, faltan %v", faltan)
	}
	return nil
}

// Session es el estado del daemon y la única fuente de verdad del producto.
//
// Cerrar la ventana no cierra la sala, así que este estado sobrevive a la UI.
// La UI lo lee por [Session.Status] y persiste únicamente cosas de
// presentación: guardarlo también del lado de Flutter crearía dos verdades que
// se desincronizan justo en el caso que el producto promete soportar, que es
// cerrar la ventana con la partida viva.
type Session struct {
	deps Deps

	// mu protege todo lo de abajo. El named pipe atiende varias conexiones y
	// el supervisor empuja eventos del motor al mismo tiempo, así que dos
	// escrituras concurrentes al estado son el caso normal, no el raro.
	mu      sync.Mutex
	state   domain.RoomState
	catalog domain.Catalog

	// installed es la última detección de la biblioteca. Se guarda porque
	// ordena la lista y consultar Steam en cada ListGames sería releer el
	// disco para pintar una pantalla.
	installed []domain.GameRef

	// inFlight es la operación larga en curso, y cómo abortarla.
	//
	// **Con candado PROPIO, y no es una preferencia de estilo.** Crear una sala
	// tiene tomado `mu` durante todo el minuto que tarda, que es exactamente el
	// rato en el que alguien puede pulsar Cancelar. Un cancelador que pidiera
	// `mu` esperaría a que termine justo lo que viene a cortar. Mismo motivo
	// por el que el diario de progreso tiene el suyo.
	inFlight longOp

	// hostSpec es la identidad de la red REAL, y solo la llena el host.
	//
	// Se guarda entera y no solo su nombre porque hacen falta las dos cosas:
	// el nombre viaja dentro de cada credencial que emite, y los 16 + 32 bytes
	// son lo que se persiste para poder reabrir LA MISMA sala tras un apagón.
	// Sin ellos, reabrir sería otra red y ninguna credencial emitida valdría.
	hostSpec domain.HostSpec
	// cardKey es la clave con que se cifró la tarjeta de esta sala. Se guarda
	// para poder rearmar el enlace de invitación sin volver a publicar.
	cardKey [domain.CardKeyLen]byte
	// sealedCard es esa misma tarjeta, ya cifrada, tal cual la ACEPTÓ el
	// registro. Sirve para volver a subirla al reabrir sin re-sellar nada, que
	// es lo que conserva válidos los enlaces ya repartidos.
	//
	// **Se escribe SIEMPRE junto a cardKey**, y esa es su invariante: la clave
	// tiene que abrir el blob. Separarlos deja en disco un enlace que no
	// descifra la tarjeta que el registro tiene.
	//
	// Vacío cuando el registro no aceptó nada, o sea en el respaldo de crear y
	// de renovar. Un ID que el registro nunca emitió no tiene tarjeta suya que
	// restaurar, y republicarla sería pedirle que reabra una sala que no conoce.
	sealedCard []byte
	// kicked son los expulsados de hace poco, con cuándo.
	//
	// Existe porque revocar tarda alrededor de un segundo y el motor sigue
	// reportando al expulsado durante esa ventana. Sin la lista, el primer
	// evento de cambio de miembros lo devuelve a los presentes y le reabre el
	// puerto, deshaciendo justo la mitad de la expulsión que era inmediata.
	kicked map[netip.Addr]time.Time

	// appliedRules es la firma del último conjunto que se aplicó CON ÉXITO.
	//
	// Nació para no repetir la misma línea de log y hoy hace dos cosas: esa, y
	// decidir si el evento de miembros tiene algo que aplicar. Ver
	// [Session.applyRuleSetLocked] y [Session.applyPolicyIfChanged].
	//
	// Que solo se escriba tras un Apply exitoso es lo que hace gratis el
	// reintento: un fallo deja la firma vieja puesta y el evento siguiente
	// vuelve a aplicar. Se limpia al salir de la sala y al reatar la compuerta
	// a otro adaptador, ver [Session.bindRoomLocked].
	appliedRules string

	// issued son las credenciales que emitió esta sala, por la dirección que se
	// le asignó a cada una.
	//
	// **Es el único sitio donde existe ese lazo.** La dirección la elige el host
	// en [Session.IssueCredentialFor] y no baja al motor, así que
	// `Engine.ListCredentials` devuelve id y vencimiento con la IP en cero.
	// De este mapa dependen las dos cosas que necesitan traducir una dirección a
	// una credencial: pre-autorizar el canal de control de quien acaba de
	// recibirla, y encontrar qué revocar al expulsar.
	//
	// Se vacía al salir de la sala, en [Session.leaveLocked]. Vive solo en
	// memoria: ver el precio en [Session.credentialFor].
	issued map[netip.Addr]domain.Credential

	// verificables son los juegos que SÍ se pueden marcar como verificados, con
	// la fecha en que se salió de la sala donde estuvieron activos.
	//
	// Se llena al salir de una sala en la que hubo dos personas o más, que es
	// la única condición que 06-catalogo.md admite, y se vacía al usarla. Sin
	// esta lista, "no se puede marcar a mano" no significa nada: la API
	// aceptaría la marca de quien la pidiera y la etiqueta que sostiene la
	// confianza del catálogo compartido dejaría de valer.
	verificables map[string]string

	// announcedGame es el último juego que anunció el host, tal como lo dijo.
	//
	// Se guarda aunque no esté en el catálogo propio, y por eso: es lo que
	// permite decirle al usuario "el host está jugando X y no tienes ese
	// perfil" en vez de dejar la pantalla en blanco, y es lo que se reintenta
	// cuando importa el perfil que le faltaba.
	announcedGame string

	// lastAnnounce es cuándo el host anunció por última vez.
	//
	// El anuncio periódico es lo que hace medible el silencio del otro lado: un
	// socket TCP medio abierto sobrevive horas a una máquina apagada, así que
	// el borde de la conexión es una señal que puede no llegar nunca.
	lastAnnounce time.Time

	// lastPublish es cuándo el registro aceptó por última vez la tarjeta de
	// esta sala.
	//
	// It is what makes the registry's deadline measurable from this side. A room
	// nobody republishes for `RoomTTL` gets swept and its invite ID goes back in
	// the pool, so republishing is what keeps a long-lived room's code its own.
	//
	// It used to be answering a much tighter deadline: the registry stopped
	// resolving six hours after the last publish, so a game opened in the
	// afternoon quietly stopped accepting new people by night, with no error on
	// this side and a "that code does not exist" on the other. That deadline is
	// gone and this clock stayed, now against three weeks instead of six hours.
	lastPublish time.Time

	// lastRenew es cuándo el host empujó por última vez el vencimiento de las
	// credenciales de los presentes.
	//
	// Es el tercer reloj de esta familia, y los tres refrescan un plazo que
	// vence solo: [lastAnnounce] el silencio que ve el otro lado, [lastPublish]
	// la ventana del registro, y este la vida de las credenciales. Estaban los
	// dos primeros y faltaba éste, así que una sala que durase más de
	// [timing.CredentialTTL] echaba a sus miembros uno por uno.
	lastRenew time.Time

	// staleProxAviso es, por miembro, DESDE CUÁNDO se le puede volver a decir
	// que este host no tiene su credencial.
	//
	// Guarda el próximo intento y no el último, y esa elección es lo que arregla
	// el fallo medido el 2026-08-11: guardando el último, un aviso que no salió
	// quemaba igual el enfriamiento entero. Ver
	// [Session.tellStaleMembersLocked], que pone plazos distintos según haya
	// salido o no.
	staleProxAviso map[netip.Addr]avisoStale

	// lastRejoin y rejoinWait son el reloj del reingreso del INVITADO.
	//
	// La espera se guarda en vez de ser una constante porque lleva jitter, y el
	// jitter tiene que fijarse al programar el intento y no al comprobarlo: si
	// se sorteara en cada latido, la condición cambiaría cada quince segundos y
	// la dispersión no dispersaría nada. Ver [timing.RejoinInterval].
	lastRejoin time.Time
	rejoinWait time.Duration

	// rejoinStreak y lastRejoinSuccess son el freno de los reingresos
	// encadenados: la racha cuenta éxitos sin calma de por medio, y llena
	// significa que la sala no se sostiene y hay que dejar de masticarla. Ver
	// [maxRejoinStreak] y [timing.RejoinCalmAfter].
	rejoinStreak      int
	lastRejoinSuccess time.Time

	// credencialMuerta dice que el HOST avisó de que no tiene la credencial de
	// esta máquina. Solo del lado del invitado.
	//
	// Es un motivo de reingreso APARTE de la ausencia, y tiene que serlo porque
	// la ausencia no lo cubre: el aviso viaja por un canal de control vivo, así
	// que llega con el host presente y con su reloj de ausencia recién puesto a
	// cero por la prueba de vida que es el propio mensaje. Reusar aquel reloj
	// para esto, que fue el primer intento, resultó en un aviso que llegaba y no
	// hacía nada. Ver [Session.rejoinDueLocked].
	credencialMuerta bool

	// myCredential is the GUEST's live credential, held in memory and nowhere
	// else.
	//
	// It is what a reattach rides: when the engine's data path dies while the
	// host is fine and the credential is still good, rebuilding the engine
	// instance with the SAME credential keeps the same virtual IP and skips
	// the lobby entirely. See [Session.reattachLocked].
	//
	// It never touches the disk, and that is a promise, not an omission: the
	// guest's saved state carries nothing that enters a room without passing
	// through the host. See [Session.saveLastRoomLocked].
	myCredential domain.Credential

	// memberKey es la identidad de este invitado DENTRO de la sala actual: con
	// ella firma su pedido de credencial y el host le devuelve lo suyo al
	// volver. Nace en el primer ingreso, se reusa al volver a LA MISMA sala, y
	// su semilla persiste sellada en la última sala guardada según lo que
	// signifique esa salida. Ver [domain.MemberKey] y
	// [Session.ensureMemberKeyLocked].
	memberKey domain.MemberKey

	// cardPublishFailing dice si la última republicación falló.
	//
	// Existe solo para no repetir el mismo aviso en cada latido. Se avisa en el
	// flanco, igual que la ausencia del host: una vez al empezar a fallar y una
	// vez al recuperarse. Sin esto, un seed caído deja una advertencia por hora
	// en el diario para siempre.
	cardPublishFailing bool

	// tamperRepairs son las veces que se repusieron las reglas propias en esta
	// sala.
	//
	// Existe para dejar de insistir. Reponerlas una vez arregla el toque
	// puntual de alguien mirando la consola del firewall; reponerlas en bucle
	// es pelearse con un antivirus a golpe de COM, y eso no lo gana nadie.
	tamperRepairs int

	// canaryRepairs son las veces que se repuso la protección tras un TOQUE del
	// canario en esta sala.
	//
	// Existe por lo mismo que tamperRepairs, con una diferencia que decide el
	// tope: acá la evidencia es mucho más fuerte. Aquella dice "falta una clave",
	// que sale en falso por carreras normales; esta dice que un paquete cruzó de
	// verdad, medido desde otra máquina. Ver [CanaryRepairLimit].
	canaryRepairs int

	// canaryDue avisa de que se acaba de aplicar la protección y toca comprobarla.
	//
	// Amortiguado a UNO y escrito sin bloquear, porque se emite con el candado
	// tomado desde applyRuleSetLocked: no puede esperar a nadie, y diez Apply
	// seguidos tienen que programar una ronda en vez de diez.
	canaryDue chan struct{}

	// saved is the room THIS machine hosts, as it was left on disk.
	//
	// Read when the session is built and acted upon right away: the room comes
	// back by itself on every start. What is kept here is what the faces need to
	// offer reopening it by hand when that fails, and to forget it.
	saved    domain.HostedRoom
	hasSaved bool

	// last is the saved last room, cached in memory, and the clocks of the
	// attempt to get back into it.
	//
	// **The room is cached and not read from disk on demand**, because whether
	// this machine is going back is answered on every snapshot, and a snapshot
	// goes out every couple of seconds to every face. Reading a sealed file that
	// often to answer a question whose answer barely changes is work for nothing.
	// [Session.saveLastRoomLocked] and [Session.forgetLastRoomLocked] keep it in
	// step with what is on disk.
	//
	// The clocks are the only part of returning that is real state. Whether it is
	// returning at all is derived; see [domain.ReturnAttempt].
	last    domain.LastRoom
	hasLast bool

	returnNextAt   time.Time
	returnAttempts int
	returnReason   string

	// returnRunning claims the turn while an attempt is in flight, because the
	// lock is released across it: without this a second caller would find the
	// deadline passed and dial the same room twice.
	returnRunning bool

	// returnCancel cuts the attempt in flight. It is what makes a person asking
	// for something beat a clock that just went off. Nil when nothing is running.
	returnCancel context.CancelFunc

	// nick es el nombre propio de esta instalación en la sala actual.
	//
	// Se guarda aparte en vez de sacarlo del peer propio cada vez que hace
	// falta. El peer propio se reconstruye desde el motor en cada cambio de
	// miembros, y si el motor no lo reportara con nombre, renovar el código
	// publicaría una tarjeta sin host. Lo que el usuario escribió es de la
	// sesión, no de la lista de peers.
	nick domain.Nickname

	// published es lo que lee Status sin tocar el candado.
	//
	// Existe porque el candado se sostiene durante llamadas a los adaptadores,
	// y algunas son lentas de verdad: aplicar reglas por COM, sondear el MTU,
	// hablar con el registro. Sin esto, un Status llegado a mitad de un cambio
	// de juego se quedaría esperando a que Windows termine, y la promesa de
	// que nada bloquea ni retrasa una respuesta se rompería justo cuando la UI
	// más necesita pintar algo.
	//
	// Es una copia, jamás una segunda verdad: solo lo escribe snapshot, que
	// corre con el candado tomado. Lo peor que puede pasar es que un Status
	// devuelva el estado de hace un instante.
	published atomic.Pointer[domain.RoomState]

	// publishedLink es el enlace de invitación completo, publicado junto al
	// estado y por el mismo motivo.
	//
	// **No se puede derivar de lo publicado**, y por eso existe un segundo
	// puntero en vez de armarlo al leer: la clave de la tarjeta vive en
	// `cardKey`, que el candado protege. Componerlo al vuelo obligaría a tomar
	// el candado dentro de `Status`, que es exactamente lo que el puntero de
	// arriba existe para evitar. Pasó: el enlace entró en la vista de la sala,
	// el latido de la interfaz empezó a pedirlo cada dos segundos, y crear una
	// sala dejaba a la ventana esperando el minuto entero que dura la
	// operación por leer un campo de texto.
	publishedLink atomic.Pointer[string]
}

// NewSession construye la sesión y purga lo que haya quedado de una ejecución
// anterior.
//
// La purga va acá y no en el primer CreateRoom porque el caso que arregla es
// justamente el que no pasa por CreateRoom: una muerte sucia del daemon deja
// reglas de firewall y reglas ajenas suspendidas, y el usuario abre la app sin
// crear ninguna sala. Purgar al arrancar es lo que garantiza que nunca queden
// puertos huérfanos abiertos.
func NewSession(ctx context.Context, d Deps) (*Session, error) {
	// Se comprueba antes de tocar nada. Un puerto en nil no falla acá, falla
	// media hora después dentro de una operación del usuario, con un panic
	// como mensaje de error y el firewall a medio aplicar.
	if err := d.validate(); err != nil {
		return nil, err
	}
	if d.Progress == nil {
		d.Progress = NewJournal(d.Clock, d.Log)
	}
	// `issued` se crea acá y no perezosamente como `kicked` o `verificables`,
	// porque emitir una credencial escribe en él sin comprobar: en nil, la
	// primera persona que pide entrar no recibe un error, tira un pánico dentro
	// del canal de control del host.
	s := &Session{
		deps:      d,
		canaryDue: make(chan struct{}, 1),
		issued:    make(map[netip.Addr]domain.Credential),
	}

	// La cuarentena de base va ANTES de la purga, y el orden es de seguridad.
	//
	// La purga es el instante de menos protección de todo el arranque: se lleva
	// las reglas de la sala anterior y todavía no hay ninguna nueva. Poner la
	// cuarentena primero es lo que hace que ese hueco esté cubierto.
	//
	// Y es fatal, igual que el fallo de la purga tres líneas más abajo. Un
	// daemon que no pudo escribir la cuarentena es un daemon con la promesa
	// apagada, y seguir arrancando dejaría al usuario con la app abierta,
	// diciendo que todo está bien, sobre una máquina sin lo único que la protege
	// con el servicio detenido.
	if err := d.Firewall.ApplyBaseQuarantine(ctx, domain.BaseQuarantineFor(d.Quarantine)); err != nil {
		return nil, fmt.Errorf("escribiendo la cuarentena de base: %w", err)
	}
	if err := d.Firewall.PurgeOwned(ctx); err != nil {
		return nil, fmt.Errorf("purgando las reglas de la ejecución anterior: %w", err)
	}
	if err := d.Firewall.RestoreForeign(ctx); err != nil {
		// No es fatal: lo que queda es una regla del juego desactivada que el
		// usuario puede volver a encender a mano. Abortar el arranque del
		// servicio por esto dejaría a alguien sin Kanpachi por un archivo de
		// estado que se pudo corromper.
		d.Log.Warn("no se pudieron restaurar las reglas ajenas suspendidas", "error", err)
	}
	if err := d.Firewall.WithdrawAdapters(ctx); err != nil {
		// Same regime as RestoreForeign, same tolerance: a dirty death left
		// doors open in the operator's firewall, and paying that debt cannot
		// be the reason the service refuses to start. What could not be closed
		// stays in the book and is retried on the next start.
		d.Log.Warn("no se pudo cerrar lo abierto en el firewall ajeno", "error", err)
	}
	s.reloadCatalog(ctx)
	s.loadSavedRoom()
	s.loadLast()

	// La primera publicación se hace acá y no en el primer Status.
	//
	// Sin esto, Status toma el candado hasta que alguien publique, y el candado
	// se sostiene durante llamadas lentas a los adaptadores. O sea que el
	// primer Status de la UI, que llega justo mientras se crea una sala, se
	// quedaría esperando a que Windows termine. Es el único camino por el que
	// la promesa de que nada retrasa una respuesta se rompía.
	s.mu.Lock()
	s.snapshot()
	s.mu.Unlock()
	return s, nil
}

// loadSavedRoom reads the room this machine hosts, as it was left on disk.
//
// Having no file is the NORMAL case: nobody has hosted, or the room was closed.
// An unreadable one is not a startup error either, because being left without a
// daemon over a truncated JSON would be worse than losing a room that can be
// opened again by hand.
func (s *Session) loadSavedRoom() {
	raw, err := s.deps.State.LoadRoom()
	if err != nil {
		s.deps.Log.Info("no hay ninguna sala guardada en disco", "detalle", err)
		return
	}
	room, err := domain.DecodeHostedRoom(raw)
	if err != nil {
		s.deps.Log.Warn("la sala guardada no se pudo interpretar y se ignora", "error", err)
		return
	}
	s.saved = room
	s.hasSaved = true
	s.deps.Log.Info("hay una sala guardada, se va a reabrir",
		"código", room.Room.InviteID.String(), "guardada", room.SavedAt.Format(time.RFC3339))
}

// Status es lo único que la UI consulta, y arrastra las alertas del módulo de
// exposición. No hay notificación aparte ni evento especial: el módulo publica
// su último resultado y esto lo lleva, así que una alerta nunca puede bloquear
// ni retrasar una respuesta.
func (s *Session) Status() domain.RoomState {
	if last := s.published.Load(); last != nil {
		// La copia se hace por llamada y no una sola vez al publicar. Copiar
		// el struct deja los slices compartidos entre TODOS los que hayan
		// llamado a Status, así que a quien reordenara su lista de miembros se
		// le movería la de los demás, y peor, la del siguiente Status. Un
		// puntero publicado es de solo lectura, y esto es lo que lo hace
		// cierto también para quien lo recibe.
		return last.Clone()
	}
	// Antes de la primera publicación no hay nada que leer, y eso es una sala
	// vacía, no un error.
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshot()
}

// IssuedAddresses son las direcciones que este host repartió, ordenadas.
//
// Existe para poder CONTRASTAR: el hueco del canal de control de la sala se
// calcula desde esta misma lista, así que una dirección emitida que no aparece
// entre los destinatarios de la regla es un invitado que se va a quedar
// esperando en un dial que nadie contesta. Ese es exactamente el fallo de la
// v0.1.6, y sin las dos listas juntas no se distingue de una sala recién
// abierta a la que todavía no entró nadie.
//
// Vacía en un invitado, que no emite ninguna.
func (s *Session) IssuedAddresses() []netip.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]netip.Addr, 0, len(s.issued))
	for ip := range s.issued {
		out = append(out, ip)
	}
	slices.SortFunc(out, func(a, b netip.Addr) int { return a.Compare(b) })
	return out
}

// snapshot copia lo que se puede mutar desde fuera y publica el resultado para
// que Status no tenga que tomar el candado.
//
// La copia de los slices no es paranoia: devolver el de peers tal cual dejaría
// que quien lo recibe lo reordene bajo los pies del supervisor, y el
// destinatario habitual es la serialización del named pipe, que lo recorre
// mientras el motor puede estar empujando un cambio de miembros.
//
// Asume el candado tomado.
func (s *Session) snapshot() domain.RoomState {
	out := s.state.Clone()
	// Both of these are DERIVED and derived HERE, on the way out, never stored in
	// the live state. See [domain.RoomState.Returning].
	out.Returning = s.returningLocked()
	out.Displaces = s.displacementLocked()
	s.published.Store(&out)
	// El enlace se publica ACÁ, que es el único sitio donde el estado y la
	// clave de la tarjeta se leen juntos con el candado tomado.
	enlace := ""
	if !s.state.Room.InviteID.IsZero() {
		enlace = s.state.Room.InviteLink(s.cardKey)
	}
	s.publishedLink.Store(&enlace)
	return out.Clone()
}

// Catalog devuelve la lista efectiva de juegos, con los instalados arriba.
func (s *Session) Catalog() (domain.Catalog, []domain.GameRef) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.catalog, append([]domain.GameRef(nil), s.installed...)
}

// reloadCatalog lee las dos capas y aplica precedencia.
//
// Ninguna de las dos es obligatoria. Un local.json corrupto se ignora entero,
// con aviso, y Kanpachi sigue con los builtin; si también falla el builtin,
// queda un catálogo vacío, que es raro y sigue siendo un estado en el que la
// app arranca. Nunca se queda sin catálogo por un archivo mal escrito.
func (s *Session) reloadCatalog(ctx context.Context) {
	var (
		profiles []domain.GameProfile
		rejected []domain.RejectedProfile
	)

	if raw, err := s.deps.Store.LoadBuiltin(); err != nil {
		s.deps.Log.Warn("no se pudo leer el catálogo que vino con la app", "error", err)
	} else if ps, bad, err := domain.ParseCatalogLayer(raw, domain.OriginBuiltin); err != nil {
		s.deps.Log.Warn("el catálogo que vino con la app no se pudo interpretar", "error", err)
	} else {
		profiles = append(profiles, ps...)
		rejected = append(rejected, bad...)
	}

	if raw, err := s.deps.Store.LoadLocal(); err != nil {
		s.deps.Log.Info("no hay catálogo local todavía", "detalle", err)
	} else if ps, bad, err := domain.ParseCatalogLayer(raw, domain.OriginMine); err != nil {
		s.deps.Log.Warn("el catálogo local está corrupto y se ignora entero", "error", err)
	} else {
		profiles = append(profiles, ps...)
		rejected = append(rejected, bad...)
	}

	s.catalog = domain.BuildCatalog(profiles, rejected)

	// La detección es falible por diseño y su fallo no puede impedir nada: un
	// error devuelve lista vacía y el catálogo queda sin ordenar por
	// instalados, que es exactamente lo que pasa en una máquina sin Steam.
	if refs, err := s.deps.Library.Installed(ctx); err != nil {
		s.deps.Log.Info("no se pudo detectar la biblioteca instalada", "detalle", err)
		s.installed = nil
	} else {
		s.installed = refs
	}
}

// planSubnet elige el /24 esquivando lo que ya existe en la máquina.
//
// Se consulta al crear o al entrar, no al instalar: la LAN de una laptop
// cambia entre la casa y la oficina, y un rango elegido en la instalación
// sería correcto solo el primer día.
func (s *Session) planSubnet(ctx context.Context) (domain.AddressPlan, error) {
	local, err := s.deps.Routes.LocalPrefixes(ctx)
	if err != nil {
		// Sin la tabla de rutas no se puede esquivar nada, y elegir a ciegas
		// dentro del espacio compartido es justo el caso que rompe la
		// conectividad de quien está detrás de CGNAT.
		return domain.AddressPlan{}, fmt.Errorf("leyendo la tabla de rutas: %w", err)
	}
	plan, err := domain.PlanAddresses(local, s.deps.Rand)
	if err != nil {
		return domain.AddressPlan{}, err
	}
	return plan, nil
}

// warnLobbyConflictLocked mira si esta máquina pisa el /24 del vestíbulo de esta
// sala, y lo dice.
//
// # Por qué lo necesitan los dos roles, y antes solo se miraba al crear
//
// Porque el vestíbulo dejó de ser el mismo para todas las salas. Antes su /24
// era una constante y el conflicto solo rompía al invitado, así que el aviso
// vivía, mal, en el único camino donde no cambiaba nada. Ahora cada sala deriva
// el suyo del código, y eso reparte el riesgo: al invitado le impide entrar a
// ESA sala, y al host le impide levantar su propia puerta.
//
// Lo que se ve sin esto, medido el 2026-08-11 en la máquina de un invitado:
// "esperando a que kanpachi1 tome la dirección 100.127.255.102" y treinta
// segundos de nada.
//
// **Avisa y sigue.** No aborta porque el solape no garantiza el fallo: depende
// de qué haga el sistema con dos interfaces que reclaman el mismo rango, y
// negarle la entrada a alguien a quien quizá le funcione es peor que dejarlo
// intentar con el aviso puesto.
//
// Asume el candado tomado.
func (s *Session) warnLobbyConflictLocked(ctx context.Context, lobby netip.Prefix) {
	local, err := s.deps.Routes.LocalPrefixes(ctx)
	if err != nil {
		// Sin la tabla de rutas no se puede comprobar. No es motivo para no
		// seguir: esto es un diagnóstico, y quedarse sin él deja las cosas como
		// estaban antes de que existiera.
		s.deps.Log.Warn("no se pudo comprobar el conflicto con el vestíbulo", "error", err)
		return
	}
	s.noteLobbyConflictLocked(domain.LobbyOverlap(local, lobby))
}

// noteLobbyConflictLocked levanta la alerta si hay solape. El cero no es
// conflicto y no dice nada.
//
// En un solo sitio para que los dos caminos digan lo MISMO: son el mismo hecho
// sobre la misma máquina, y dos textos distintos para él serían dos defectos
// distintos a los ojos de quien lo lea.
//
// Asume el candado tomado.
func (s *Session) noteLobbyConflictLocked(conflicto netip.Prefix) {
	if !conflicto.IsValid() {
		return
	}
	// El /24 no se puede cambiar desde esta máquina: los dos lados lo derivan del
	// mismo código y tienen que llegar al mismo. Lo que sí se puede es DECIRLO, y
	// decirlo ahora vale doble, porque hay una salida real que nombrar: el código
	// nuevo mueve el vestíbulo. Ver [domain.Rendezvous.LobbySubnet].
	detalle := fmt.Sprintf(
		"una red de esta máquina (%s) usa el mismo rango que el vestíbulo de esta sala, "+
			"que puede fallar. Pídele al host que renueve el código: eso mueve el vestíbulo",
		conflicto)
	s.state.Alerts = append(s.state.Alerts, domain.Alert{
		Kind:   domain.AlertLobbyConflict,
		Detail: detalle,
	})
	s.deps.Log.Warn("conflicto de rango con el vestíbulo", "local", conflicto.String())
	// Y al diario, que es lo que la pantalla enseña mientras esto pasa. La
	// alerta sola llega tarde: se lee en otra pantalla, y para entonces el
	// intento ya falló.
	s.deps.Progress.Step(domain.ScopeNetwork, detalle)
}

// applyPolicy regenera el RuleSet completo y aplica la diferencia.
//
// Se llama tras CADA cambio de miembros y tras cada cambio de juego, que son
// la misma operación desde acá. No hay camino incremental: el conjunto se
// calcula entero desde el perfil, el rol y los miembros presentes, así que no
// puede quedar nada colgando de un cálculo anterior.
//
// Asume el candado tomado.
func (s *Session) applyPolicy(ctx context.Context) error {
	desired, err := s.desiredRuleSetLocked()
	if err != nil {
		return err
	}
	return s.applyRuleSetLocked(ctx, desired)
}

// applyPolicyIfChanged aplica solo si el conjunto deseado no es el mismo que se
// aplicó la última vez, y contesta si aplicó.
//
// # Por qué existe al lado de applyPolicy, y no dentro
//
// Porque [Session.applyPolicy] es también el que REPARA. `Apply` calcula su
// diferencia contra las reglas VIVAS del sistema, así que reaplicar el mismo
// conjunto repone lo que alguien borró por fuera, y de eso dependen el barrido
// del canario y la comprobación de la decisión 19. Un salto metido dentro de
// applyPolicy apagaría la autorreparación entera, que es lo contrario de lo que
// se busca. Por eso hay dos métodos y el de siempre queda intacto: los dieciséis
// llamadores que reparan siguen llamando al que aplica siempre, y solo el sitio
// de alta frecuencia llama a este.
//
// # El sitio de alta frecuencia, medido
//
// [Session.onPeersChangedLocked], que corre por cada evento del motor. Medido
// el 2026-08-17 en el droplet: crear una sala son 19 aplicaciones de nftables,
// hasta 31 dentro de un mismo segundo de reloj, separadas por lo que tarda cada
// una (16 ms con 3 reglas, 23 ms con 7), todas con el conjunto idéntico. En
// reposo son cero, así que el gasto está concentrado entero en las transiciones,
// que es justo cuando el daemon tiene trabajo de verdad que hacer. En Windows
// cada aplicación enumera la tienda ENTERA de reglas por COM, dos veces, y eso
// son 152 ms medidos sobre 1157 reglas: casi seis segundos de leer el firewall
// de punta a punta por transición.
//
// # Por qué la firma es la condición correcta
//
// El conjunto deseado se calcula entero desde el perfil, el rol y los miembros
// presentes, y las reglas del canal de control van DENTRO, con sus
// destinatarios. O sea que la firma cambia ante todo lo que tiene que provocar
// un cambio, y no cambia cuando el motor repite lo que ya se sabía.
//
// El estado es `s.appliedRules`, que ya existía para el log y solo se escribe
// tras un `Apply` EXITOSO: un fallo deja la firma vieja puesta y el evento
// siguiente reintenta gratis. Se limpia al salir de la sala, y también al
// reatar la compuerta a un adaptador nuevo, ver [Session.bindRoomLocked].
//
// Asume el candado tomado.
func (s *Session) applyPolicyIfChanged(ctx context.Context) (bool, error) {
	desired, err := s.desiredRuleSetLocked()
	if err != nil {
		return false, err
	}
	if ruleSignature(desired) == s.appliedRules {
		return false, nil
	}
	if err := s.applyRuleSetLocked(ctx, desired); err != nil {
		return false, err
	}
	return true, nil
}

// bindRoomLocked ata la compuerta a los adaptadores de esta sala y OLVIDA la
// firma aplicada.
//
// El olvido es la mitad que importa. La firma describe un conjunto de reglas, y
// las reglas se aplican contra el adaptador al que la compuerta está atada: tras
// reatar, el mismo texto describe otra cosa. Sin limpiarla, el primer evento
// después de un reatado casaría con la firma vieja y se saltaría la aplicación
// que pone las reglas donde ahora hay que ponerlas. Un contador de reataduras
// sería lo mismo con un campo más y una forma más de olvidarse de subirlo.
//
// Los siete sitios que atan pasan por acá para que ese olvido no dependa de que
// alguien lo recuerde.
//
// Asume el candado tomado.
func (s *Session) bindRoomLocked(
	ctx context.Context, room, lobby netip.Prefix, with domain.RoomBinding,
) error {
	if err := s.deps.Firewall.BindRoom(ctx, room, lobby, with); err != nil {
		return err
	}
	s.appliedRules = ""
	return nil
}

// ruleSignature es cómo se compara un conjunto de reglas contra otro.
//
// Una sola versión del cálculo, compartida por el gate y por el log, porque dos
// serían las dos versiones que derivan: el día que una cambiara, el log diría
// que las reglas cambiaron y el gate creería que no, y el síntoma sería una
// sala sin las reglas que su propio log afirma haber puesto.
//
// `%v` sobre las reglas y no un hash: vienen ordenadas por nombre, y sus
// destinatarios por dirección, así que el mismo conjunto imprime siempre igual.
// Un hash sería lo mismo con más código.
func ruleSignature(rs domain.RuleSet) string { return fmt.Sprintf("%v", rs.Rules) }

// desiredRuleSetLocked calcula el estado deseado sin aplicarlo.
//
// Existe separado porque hay DOS consumidores del mismo cálculo: aplicarlo, y
// compararlo contra lo que el sistema tiene puesto. Que salgan del mismo lugar
// es lo que hace que la comprobación de la decisión 19 mida lo que de verdad se
// pidió, en vez de una segunda versión del cálculo que puede separarse.
//
// # Las dos listas de miembros que hay acá, y por qué siguen siendo dos
//
// Los puertos del juego se abren hacia `MemberIPs(s.state.Peers)`, o sea hacia
// quien ESTÁ. El canal de control se abre además hacia quien acaba de recibir
// credencial y todavía no entró, que es lo que evita que su primer intento de
// conexión rebote contra el firewall. Ver [Session.authorizedControlIPsLocked].
//
// La diferencia es deliberada y ahora es la única que queda. Hasta el
// 2026-08-13 había otra, sin querer: la tabla del motor contaba de menos en el
// host, así que un invitado ya dentro tenía canal de control y no tenía puertos
// de juego. Eso lo cierra [Session.withAdmittedLocked], que le suma a la lista
// de miembros a quien tiene el canal de la sala abierto.
//
// Asume el candado tomado.
func (s *Session) desiredRuleSetLocked() (domain.RuleSet, error) {
	desired, err := domain.BuildRuleSet(
		s.state.Game,
		s.state.Role,
		s.state.LocalIP,
		domain.MemberIPs(s.state.Peers),
	)
	if err != nil {
		return domain.RuleSet{}, err
	}

	// El canal de control va en el MISMO conjunto y no en una llamada aparte.
	// El firewall calcula la diferencia contra lo que está vivo, así que dos
	// llamadas serían dos estados deseados que se pisan: la segunda borraría lo
	// que puso la primera por no encontrarlo en su conjunto.
	//
	// Que el hueco del canal se recalcule con los miembros presentes tiene una
	// consecuencia buena y gratis: expulsar lo cierra en el firewall, y no solo
	// en la lista del oyente.
	if s.state.Conn.InRoom() {
		canal, err := domain.ControlRules(
			s.state.Role,
			s.lobbyHostAddrLocked(),
			s.state.LocalIP,
			s.authorizedControlIPsLocked(),
		)
		if err != nil {
			return domain.RuleSet{}, err
		}
		desired.Add(canal...)
	}
	return desired, nil
}

// rango pinta un par de puertos como lo escribiría una persona. Solo para el
// log: un `57623-57623` se lee dos veces antes de entender que es uno solo.
func rango(desde, hasta uint16) string {
	if desde == hasta {
		return strconv.Itoa(int(desde))
	}
	return strconv.Itoa(int(desde)) + "-" + strconv.Itoa(int(hasta))
}

func (s *Session) applyRuleSetLocked(ctx context.Context, desired domain.RuleSet) error {
	if err := s.deps.Firewall.Apply(ctx, desired); err != nil {
		return fmt.Errorf("aplicando las reglas de firewall: %w", err)
	}
	// Aplicar bien cierra la alerta de expulsión a medias. Una alerta pegajosa
	// que nadie pueda quitar se queda para siempre, y una alerta eterna deja de
	// ser información.
	s.state.DropAlerts(domain.AlertKickIncomplete)

	// Se anota el CAMBIO, no la aplicación.
	//
	// Cada evento del motor recalcula y reaplica el conjunto entero, así que
	// abrir una sala escribía cinco veces la misma línea en un segundo. Y una
	// línea repetida es peor que ninguna: la que de verdad importa, la vez que
	// el conjunto cambió porque entró alguien o se eligió un juego, queda
	// enterrada entre cuatro que dicen lo mismo de antes.
	//
	// La firma sale de [ruleSignature], que es la MISMA que decide si hace falta
	// aplicar. Ver allá por qué no puede haber dos.
	if firma := ruleSignature(desired); firma != s.appliedRules {
		s.appliedRules = firma
		s.deps.Log.Info("cambiaron las reglas del firewall",
			"juego", s.state.Game.ID, "rol", s.state.Role.String(), "reglas", len(desired.Rules))
		// Y CADA regla, con a quién abre.
		//
		// El número solo no sirve, y eso está medido: la v0.1.6 escribía
		// `reglas 1` cada vez que el host emitía una credencial, y esa línea es
		// compatible con las dos realidades opuestas. Con la regla de la sala
		// puesta hay sala; sin ella nadie puede entrar y el invitado se queda en
		// un `dial` que nadie contesta. Distinguirlas costó horas de leer código
		// porque el log decía lo mismo en los dos casos.
		//
		// Lo que hace falta para no repetirlo es el DESTINATARIO: una regla del
		// canal de control con `remote` vacío es exactamente el fallo, y se ve de
		// un vistazo.
		for _, r := range desired.Rules {
			s.deps.Log.Info("  regla", "nombre", r.Name, "proto", r.Proto.String(),
				"puertos", rango(r.From, r.To), "local", r.Local.String(),
				"remotos", r.Remote, "redes", r.Nets)
		}
	}

	// Y se programa una ronda del canario, que es la ÚNICA comprobación que sale
	// a la red a ver si la compuerta contiene de verdad.
	//
	// Acá y no en cada llamador: así lo heredan los diez sitios que llaman a
	// applyPolicy y todos los futuros, sin que nadie tenga que acordarse. El
	// envío es no bloqueante sobre un canal amortiguado a UNO, así que esto corre
	// con el candado tomado sin poder esperar a nadie, y diez Apply seguidos
	// programan una ronda en vez de diez.
	select {
	case s.canaryDue <- struct{}{}:
	default:
	}
	return nil
}

// applyAdapter reaplica los ajustes del adaptador.
//
// Lo llama el supervisor en cada evento de identificación de red, que es
// cuando Windows revierte la métrica, la categoría y las rutas. Sin esto los
// ajustes se pierden solos y el usuario ve que "ayer funcionaba".
func (s *Session) applyAdapter(ctx context.Context) error {
	s.mu.Lock()
	want := domain.AdapterStateFor(s.state.LocalIP, s.state.Subnet, s.state.Net.MTU, s.state.Game)
	s.mu.Unlock()

	if !want.Address.IsValid() {
		return nil // sin sala no hay adaptador que ajustar
	}
	return s.deps.NetCfg.ApplyAdapter(ctx, want)
}

// ReapplyAdapter es applyAdapter para el supervisor, que vive fuera de core.
func (s *Session) ReapplyAdapter(ctx context.Context) error { return s.applyAdapter(ctx) }

// teardown deshace todo lo de la sala, en el orden en que importa.
//
// El firewall primero. Si algo falla a mitad del apagado, el estado en que se
// queda la máquina es "sin puertos abiertos y con el motor colgado", que es
// molesto; al revés sería "sin motor y con puertos abiertos hacia direcciones
// que ya no existen", que es exactamente lo que la cuarentena por defecto
// existe para impedir.
//
// Ningún error corta la secuencia: se registran todos y se sigue, porque
// abortar en el primero dejaría lo demás sin deshacer y no hay un segundo
// intento después de salir de la sala.
func (s *Session) teardown(ctx context.Context) {
	// **Los pasos van acá y no en quien llama, y eso es lo que los hace ciertos.**
	//
	// Este cuerpo lo comparten las cinco salidas: el usuario, la expulsión, el
	// cierre del host, los dos cortes automáticos y el apagado del daemon.
	// Narrarlo desde `LeaveRoom` habría descrito lo que el código de al lado
	// pretende hacer; narrarlo desde aquí describe lo que se hizo.
	//
	// Sin diario abierto, [Journal.Step] los ignora, así que los caminos que no
	// vienen de una orden del usuario no pagan nada por estar instrumentados.
	s.deps.Progress.Step(domain.ScopeFirewall, "cerrando los puertos del juego")
	if err := s.deps.Firewall.Apply(ctx, domain.RuleSet{}); err != nil {
		s.deps.Log.Error("no se pudieron cerrar los puertos al salir", "error", err)
		s.deps.Progress.Step(domain.ScopeFirewall, "no se pudieron cerrar los puertos: "+err.Error())
	}
	s.deps.Progress.Step(domain.ScopeFirewall, "restaurando las reglas ajenas que se habían suspendido")
	if err := s.deps.Firewall.RestoreForeign(ctx); err != nil {
		s.deps.Log.Error("no se pudieron restaurar las reglas ajenas", "error", err)
		s.deps.Progress.Step(domain.ScopeFirewall, "no se pudieron restaurar las reglas ajenas: "+err.Error())
	}
	// The doors opened in the FOREIGN firewall close with the room, next to
	// RestoreForeign because it is the same debt regime: what got touched with
	// consent goes back the moment the reason is gone. A failure stays in the
	// book and the next service start retries it.
	s.deps.Progress.Step(domain.ScopeFirewall, "cerrando lo abierto en el firewall ajeno")
	if err := s.deps.Firewall.WithdrawAdapters(ctx); err != nil {
		s.deps.Log.Error("no se pudo cerrar lo abierto en el firewall ajeno", "error", err)
		s.deps.Progress.Step(domain.ScopeFirewall, "no se pudo cerrar lo abierto en el firewall ajeno: "+err.Error())
	}
	// La compuerta se suelta DESPUÉS de cerrar los puertos, y ese orden es el
	// mismo argumento de arriba llevado a la otra capa: al revés quedaría un
	// instante con puertos abiertos y sin nada que los acote.
	//
	// Soltarla no es dejar los filtros puestos: el `Apply` con el conjunto vacío
	// de dos líneas más arriba ya barrió las ranuras.
	s.deps.Firewall.UnbindRoom()
	s.deps.Progress.Step(domain.ScopeFirewall, "soltada la compuerta")

	s.deps.Progress.Step(domain.ScopeNetwork, "revirtiendo los ajustes del adaptador")
	if err := s.deps.NetCfg.RevertTweaks(ctx); err != nil {
		s.deps.Log.Error("no se pudieron revertir los ajustes del adaptador", "error", err)
		s.deps.Progress.Step(domain.ScopeNetwork, "no se pudieron revertir los ajustes: "+err.Error())
	}
	s.deps.Progress.Step(domain.ScopeDaemon, "cerrando el canal de la sala")
	if err := s.deps.Control.Close(); err != nil {
		s.deps.Log.Error("no se pudo cerrar el canal de control", "error", err)
		s.deps.Progress.Step(domain.ScopeDaemon, "no se pudo cerrar el canal: "+err.Error())
	}
	// El último y el que más tarda: bajar la red virtual es esperar a que otro
	// proceso termine. Es el paso en el que se queda la pantalla cuando alguien
	// se pregunta si esto se colgó.
	s.deps.Progress.Step(domain.ScopeEngine, "bajando la red cifrada")
	if err := s.deps.Engine.Leave(ctx); err != nil {
		s.deps.Log.Error("el motor no salió limpio", "error", err)
		s.deps.Progress.Step(domain.ScopeEngine, "el motor no salió limpio: "+err.Error())
	}
}

// bindingLocked dice a cuántos adaptadores hay que acotar la compuerta AHORA.
//
// Lo decide el ROL y no un campo que alguien pueda dejar viejo. El host se queda
// en el vestíbulo mientras la sala esté abierta, porque es su puerta: quien
// entra todavía no es miembro y lo que viene a pedir es justamente el permiso
// para serlo. El invitado lo suelta al entrar, a propósito, porque quedarse ahí
// mantendría abierta una vía por la que un desconocido con el código ve que esta
// máquina está en esa sala.
//
// Asume el candado tomado.
func (s *Session) bindingLocked() domain.RoomBinding {
	if s.state.IsHost() {
		return domain.BindRoomAndLobby
	}
	return domain.BindRoomOnly
}

// lobbyNetLocked es el /24 del vestíbulo de esta sala, o el cero si esta máquina
// no tiene vestíbulo levantado.
//
// Sale de [Session.hostSpec], que solo llena el host, y ahí está la simetría con
// [Session.bindingLocked]: el vestíbulo lo sostiene quien hospeda, y el invitado
// lo suelta en cuanto tiene su credencial. Un invitado devuelve cero y pide
// [domain.BindRoomOnly], que es justo el caso donde el rango no hace falta.
//
// Se lee de lo ya calculado y no se vuelve a derivar del código: la derivación
// pasa por Argon2id con memoria dura, y esto lo llama cada cambio de miembros.
//
// Asume el candado tomado.
func (s *Session) lobbyNetLocked() netip.Prefix {
	if !s.state.IsHost() {
		return netip.Prefix{}
	}
	return s.hostSpec.Rendezvous.LobbySubnet()
}

// lobbyHostAddrLocked es la .1 del vestíbulo de esta sala, o el cero si esta
// máquina no hospeda.
//
// El cero es un valor con significado y no un hueco: [domain.ControlRules] se
// salta las direcciones inválidas sin error, y un invitado no tiene puerta que
// abrir. Devolver la .1 de un vestíbulo derivado de un [domain.HostSpec] vacío
// sería una dirección que parece legítima y no es de nadie.
//
// Asume el candado tomado.
func (s *Session) lobbyHostAddrLocked() netip.Addr {
	if !s.state.IsHost() {
		return netip.Addr{}
	}
	return s.hostSpec.Rendezvous.LobbyHostAddress()
}

// requireHost es la comprobación de las tres operaciones del host. Asume el
// candado tomado.
func (s *Session) requireHost() error {
	if !s.state.Conn.InRoom() {
		return ErrNoRoom
	}
	if !s.state.IsHost() {
		return ErrNotHost
	}
	return nil
}

// seedsFor devuelve las semillas a las que apuntar el motor. Hoy es el seed de
// la sala y nada más; existe como función para que agregar el respaldo por IP
// compilada del paso 3 del flujo sea un cambio de un sitio.
func seedsFor(room domain.Room) []string {
	return []string{room.Seed}
}

// virtualIPOf busca a un miembro por su IP. Asume el candado tomado.
func (s *Session) virtualIPOf(ip netip.Addr) (domain.Peer, bool) {
	for _, p := range s.state.Peers {
		if p.VirtualIP == ip {
			return p, true
		}
	}
	return domain.Peer{}, false
}
