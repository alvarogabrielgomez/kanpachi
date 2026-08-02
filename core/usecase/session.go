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
	Clock     port.Clock
	Log       port.Logger

	// Rand es de dónde salen el invite ID de respaldo, la identidad de la red
	// real, la subred y la clave de la tarjeta. Entra por parámetro y no se
	// toma de crypto/rand directamente para que los tests puedan fijar un
	// resultado sin dejar de ejercitar el mismo camino.
	Rand io.Reader
}

// validate comprueba que estén todos los puertos.
//
// Ninguno es opcional. Un daemon al que le falte el firewall arrancaría feliz
// y abriría una sala sin cuarentena, que es el peor fallo posible de este
// producto y el más difícil de notar desde fuera.
func (d Deps) validate() error {
	faltan := make([]string, 0, 14)
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
	// kicked son los expulsados de hace poco, con cuándo.
	//
	// Existe porque revocar tarda alrededor de un segundo y el motor sigue
	// reportando al expulsado durante esa ventana. Sin la lista, el primer
	// evento de cambio de miembros lo devuelve a los presentes y le reabre el
	// puerto, deshaciendo justo la mitad de la expulsión que era inmediata.
	kicked map[netip.Addr]time.Time

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

	// tamperRepairs son las veces que se repusieron las reglas propias en esta
	// sala.
	//
	// Existe para dejar de insistir. Reponerlas una vez arregla el toque
	// puntual de alguien mirando la consola del firewall; reponerlas en bucle
	// es pelearse con un antivirus a golpe de COM, y eso no lo gana nadie.
	tamperRepairs int

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
	s := &Session{deps: d}

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
	desired, err := domain.BuildRuleSet(
		s.state.Game,
		s.state.Role,
		s.state.LocalIP,
		domain.MemberIPs(s.state.Peers),
	)
	if err != nil {
		return err
	}
	if err := s.deps.Firewall.Apply(ctx, desired); err != nil {
		return fmt.Errorf("aplicando las reglas de firewall: %w", err)
	}
	// Aplicar bien cierra la alerta de expulsión a medias. Una alerta pegajosa
	// que nadie pueda quitar se queda para siempre, y una alerta eterna deja de
	// ser información.
	s.state.DropAlerts(domain.AlertKickIncomplete)
	s.deps.Log.Info("reglas aplicadas",
		"juego", s.state.Game.ID, "rol", s.state.Role.String(), "reglas", len(desired.Rules))
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
