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
	Engine    port.EnginePort
	Firewall  port.FirewallPort
	NetCfg    port.NetConfigPort
	Routes    port.RoutingTable
	Store     port.CatalogStore
	State     port.StateStore
	Library   port.GameLibrary
	Directory port.RoomDirectory
	Control   port.ControlChannel
	Audit     port.ExposureAudit
	Inspector port.SocketInspector
	Prober    port.Prober
	Canary    port.CanaryPort
	Clock     port.Clock
	Log       port.Logger

	// Rand es de dónde salen el invite ID de respaldo, la identidad de la red
	// real, la subred y la clave de la tarjeta. Entra por parámetro y no se
	// toma de crypto/rand directamente para que los tests puedan fijar un
	// resultado sin dejar de ejercitar el mismo camino.
	Rand io.Reader

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
	nombrar("Directory", d.Directory != nil)
	nombrar("Control", d.Control != nil)
	nombrar("Audit", d.Audit != nil)
	nombrar("Inspector", d.Inspector != nil)
	nombrar("Canary", d.Canary != nil)
	nombrar("Prober", d.Prober != nil)
	nombrar("Clock", d.Clock != nil)
	nombrar("Log", d.Log != nil)
	nombrar("Rand", d.Rand != nil)

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

	// appliedRules es la firma del último conjunto que se aplicó, y existe solo
	// para no repetir la misma línea de log. Ver [Session.applyRuleSetLocked].
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
	// Es lo que hace medible el CardTTL desde este lado. El registro deja de
	// resolver una sala a las seis horas de la última publicación, y hasta que
	// esto existió nada la refrescaba en vida de la sala: una partida abierta
	// desde la tarde dejaba de aceptar gente nueva a la noche, sin un solo
	// error de este lado y con un "ese código no existe" del otro.
	lastPublish time.Time

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

	// pending es la sala que quedó abierta en el arranque anterior, si la hubo.
	//
	// Se lee al construir la sesión y NO se actúa sobre ella. Reanudar es una
	// decisión del usuario dentro de la app, nunca un efecto de arrancar el
	// servicio.
	pending    domain.PersistedRoom
	hasPending bool

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
		d.Progress = NewJournal(d.Clock)
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
	if err := d.Firewall.ApplyBaseQuarantine(ctx, domain.BaseQuarantine()); err != nil {
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
	s.reloadCatalog(ctx)
	s.loadPending()

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

// loadPending lee la sala que quedó abierta en el arranque anterior.
//
// Que no haya archivo es el caso NORMAL: toda salida limpia lo borra. Que haya
// uno ilegible tampoco es un error del arranque, porque quedarse sin daemon por
// un JSON cortado sería peor que perder una sala que de todas formas hay que
// confirmar a mano.
func (s *Session) loadPending() {
	raw, err := s.deps.State.LoadRoom()
	if err != nil {
		s.deps.Log.Info("no hay sala pendiente del arranque anterior", "detalle", err)
		return
	}
	room, err := domain.DecodePersistedRoom(raw)
	if err != nil {
		s.deps.Log.Warn("la sala guardada no se pudo interpretar y se ignora", "error", err)
		return
	}
	s.pending = room
	s.hasPending = true
	s.deps.Log.Info("hay una sala del arranque anterior sin cerrar",
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
	if plan.LobbyConflict.IsValid() {
		// No hay nada que corregir: el /24 del vestíbulo es el mismo en las dos
		// máquinas por necesidad. Lo único que se puede hacer es decirlo, y
		// decirlo vale, porque el síntoma sin aviso es "entrar a salas ajenas no
		// funciona" sin ninguna pista de por qué.
		s.state.Alerts = append(s.state.Alerts, domain.Alert{
			Kind: domain.AlertLobbyConflict,
			Detail: fmt.Sprintf(
				"una red de esta máquina (%s) usa el mismo rango que el vestíbulo de Kanpachi (%s), entrar a salas ajenas puede fallar",
				plan.LobbyConflict, domain.RendezvousSubnet),
		})
		s.deps.Log.Warn("conflicto de rango con el vestíbulo",
			"local", plan.LobbyConflict.String(), "vestíbulo", domain.RendezvousSubnet.String())
	}
	return plan, nil
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

// desiredRuleSetLocked calcula el estado deseado sin aplicarlo.
//
// Existe separado porque hay DOS consumidores del mismo cálculo: aplicarlo, y
// compararlo contra lo que el sistema tiene puesto. Que salgan del mismo lugar
// es lo que hace que la comprobación de la decisión 19 mida lo que de verdad se
// pidió, en vez de una segunda versión del cálculo que puede separarse.
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
			domain.RendezvousHostAddress,
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
	// La firma se arma con `%v` sobre las reglas y no con un campo aparte:
	// vienen ordenadas por nombre, y sus destinatarios por dirección, así que
	// el mismo conjunto imprime siempre igual. Un hash sería lo mismo con más
	// código.
	if firma := fmt.Sprintf("%v", desired.Rules); firma != s.appliedRules {
		s.appliedRules = firma
		s.deps.Log.Info("cambiaron las reglas del firewall",
			"juego", s.state.Game.ID, "rol", s.state.Role.String(), "reglas", len(desired.Rules))
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
	if err := s.deps.Firewall.Apply(ctx, domain.RuleSet{}); err != nil {
		s.deps.Log.Error("no se pudieron cerrar los puertos al salir", "error", err)
	}
	if err := s.deps.Firewall.RestoreForeign(ctx); err != nil {
		s.deps.Log.Error("no se pudieron restaurar las reglas ajenas", "error", err)
	}
	// La compuerta se suelta DESPUÉS de cerrar los puertos, y ese orden es el
	// mismo argumento de arriba llevado a la otra capa: al revés quedaría un
	// instante con puertos abiertos y sin nada que los acote.
	//
	// Soltarla no es dejar los filtros puestos: el `Apply` con el conjunto vacío
	// de dos líneas más arriba ya barrió las ranuras.
	s.deps.Firewall.UnbindRoom()
	if err := s.deps.NetCfg.RevertTweaks(ctx); err != nil {
		s.deps.Log.Error("no se pudieron revertir los ajustes del adaptador", "error", err)
	}
	if err := s.deps.Control.Close(); err != nil {
		s.deps.Log.Error("no se pudo cerrar el canal de control", "error", err)
	}
	if err := s.deps.Engine.Leave(ctx); err != nil {
		s.deps.Log.Error("el motor no salió limpio", "error", err)
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
