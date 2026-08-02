// Package port declara las interfaces que el dominio necesita del mundo.
//
// Se declaran ACÁ y se implementan fuera, en daemon/adapter. Esa inversión es
// lo único que hace que el dominio no sepa que existe Windows, y su métrica no
// es la pureza de capas: es que los tests de core corran sin admin, sin red y
// sin Windows. Si eso deja de ser cierto, la arquitectura se rompió.
//
// Lo que NO hay acá, y su ausencia es la parte importante:
//
//   - No existe "abrir un puerto arbitrario". FirewallPort recibe un RuleSet
//     que solo se puede construir desde un perfil del catálogo.
//   - No existe crear ni borrar mapeos en el router. ExposureAudit solo lee.
//   - No existe observar procesos. SocketInspector saca una foto puntual y
//     únicamente lo llama el creador de perfiles.
//
// Lo que no existe en la interfaz no se puede llamar por error.
package port

import (
	"context"
	"net/netip"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// EnginePort es el motor de red. Su única implementación ejecuta EasyTier como
// proceso hijo, y es el único sitio del proyecto que lo menciona.
type EnginePort interface {
	// HostNetwork arranca la red como nodo admin: el único que conoce el
	// secreto de la red real y el único que puede emitir credenciales.
	HostNetwork(ctx context.Context, spec domain.HostSpec) error
	// JoinRendezvous entra al VESTÍBULO, sin credencial, que es el paso 4 del
	// flujo de conexión. Es un método aparte y no una variante de
	// JoinWithCredential porque son dos redes distintas con dos modelos de
	// confianza distintos: el vestíbulo es público y desechable, lo único que
	// se hace ahí es pedirle la credencial al host, y no se abre un puerto.
	//
	// Llamarlo otra vez REEMPLAZA el vestíbulo anterior. Es lo que hace que
	// renovar el código funcione: el nombre del vestíbulo deriva del invite ID,
	// así que un código nuevo es un vestíbulo nuevo, y quedarse en el viejo
	// produciría un código por el que nadie puede entrar.
	JoinRendezvous(ctx context.Context, spec domain.RendezvousSpec) error
	// LeaveRendezvous sale SOLO del vestíbulo.
	//
	// Existe aparte de Leave porque el host está en dos redes a la vez, y un
	// único "salir" sería ambiguo justo donde no puede serlo: al invitado le
	// toca abandonar el vestíbulo y quedarse en la sala, y confundirlo con
	// Leave lo echaría de la sala a la que acaba de entrar.
	LeaveRendezvous(ctx context.Context) error
	// JoinWithCredential entra a la red REAL como nodo temporal. Nunca recibe
	// el secreto.
	JoinWithCredential(ctx context.Context, spec domain.GuestSpec) error
	// Leave sale de todo: de la sala y del vestíbulo si estuviera en él.
	//
	// Es IDEMPOTENTE. Lo llama el camino de error de crear y de entrar, que
	// puede dispararse antes de que el motor haya arrancado, y un error por
	// "no estabas en ninguna red" convertiría un fallo en dos.
	Leave(ctx context.Context) error

	// Los tres siguientes solo tienen sentido en un nodo admin. Ver decisiones
	// 2 y 22.
	IssueCredential(ctx context.Context, req domain.CredentialRequest) (domain.Credential, error)
	RevokeCredential(ctx context.Context, id domain.CredentialID) error
	ListCredentials(ctx context.Context) ([]domain.Credential, error)

	Peers(ctx context.Context) ([]domain.Peer, error)
	// Events es el canal por el que el motor empuja. El supervisor lo escucha
	// y traduce cada evento a una transición de la máquina de estados.
	Events() <-chan domain.EngineEvent
	Diagnostics(ctx context.Context) (domain.NetCheck, error)
}

// FirewallPort es declarativo. Recibe el estado deseado y calcula la
// diferencia contra lo aplicado: no hay "agregar regla" imperativo suelto, así
// que un cambio de miembros o de juego no puede dejar nada colgando del
// cálculo anterior.
type FirewallPort interface {
	Apply(ctx context.Context, desired domain.RuleSet) error
	// PurgeOwned borra todo lo etiquetado con el grupo Kanpachi. Se llama al
	// arrancar el servicio, antes de aplicar nada: una muerte sucia del daemon
	// nunca deja puertos huérfanos abiertos.
	PurgeOwned(ctx context.Context) error

	// AuditForeign busca reglas permisivas que dejó el instalador del juego o
	// un diálogo previo de Windows. Consulta el almacén de reglas por ruta de
	// ejecutable: no enumera procesos y no sabe si el juego está corriendo.
	AuditForeign(ctx context.Context, p domain.GameProfile) ([]domain.ForeignRule, error)
	// SuspendForeign las desactiva, jamás las borra, y persiste el estado
	// previo antes de tocar nada. Siempre con confirmación del usuario.
	SuspendForeign(ctx context.Context, rules []domain.ForeignRule) error
	// RestoreForeign las devuelve como estaban. Se llama al salir de la sala y
	// también al arrancar el servicio, por si una salida sucia dejó algo.
	RestoreForeign(ctx context.Context) error
}

// NetConfigPort mantiene los ajustes del adaptador que Windows revierte solo.
type NetConfigPort interface {
	ApplyAdapter(ctx context.Context, want domain.AdapterState) error
	// RevertTweaks deshace lo que pidió el perfil del juego. Separado de
	// ApplyAdapter porque los ajustes por juego se revierten al salir de la
	// sala y la métrica del adaptador no.
	RevertTweaks(ctx context.Context) error
	// ProbeMTU sondea el camino con ping de no fragmentar. Sin esto el síntoma
	// es cruel: el túnel levanta, el ping anda, la partida conecta, y el mundo
	// no termina de cargar.
	ProbeMTU(ctx context.Context) (int, error)

	// SetDirectPlay enciende o apaga el componente legado de Windows.
	//
	// Va aparte de ApplyAdapter porque no es un ajuste del adaptador: es una
	// característica opcional del sistema, y meterla en el estado declarativo
	// haría que cada evento de identificación de red, que ocurre varias veces
	// por sesión, tocara la instalación de características de Windows.
	//
	// Lo pide un perfil y se revierte al salir de la sala, igual que las rutas.
	// Solo lo necesitan juegos previos a 2005, aproximadamente.
	SetDirectPlay(ctx context.Context, want bool) error
}

// RoutingTable es lo que hace falta para esquivar el conflicto CGNAT: las
// rutas y direcciones de todos los adaptadores, consultadas al crear o entrar
// a una sala y no al instalar.
type RoutingTable interface {
	LocalPrefixes(ctx context.Context) ([]netip.Prefix, error)
}

// CatalogStore es el almacenamiento del catálogo. Devuelve bytes crudos y no
// perfiles: quien valida es el dominio, y un adaptador que decidiera qué es un
// perfil válido movería la política fuera de core.
type CatalogStore interface {
	// LoadBuiltin lee el archivo de solo lectura que vino en el instalador.
	LoadBuiltin() ([]byte, error)
	// LoadLocal lee los propios y los importados. Un archivo corrupto devuelve
	// error y quien llama ignora la capa entera: nunca se queda sin catálogo.
	LoadLocal() ([]byte, error)
	// SaveLocal escribe, con respaldo de la escritura anterior.
	SaveLocal([]byte) error
}

// GameLibrary detecta qué está instalado. El resultado ORDENA la lista, jamás
// la filtra: la detección es falible por diseño y ninguna de sus fallas puede
// impedir crear una sala. Un error acá devuelve lista vacía.
type GameLibrary interface {
	Installed(ctx context.Context) ([]domain.GameRef, error)
}

// SocketInspector saca una foto puntual de las tablas de sockets.
//
// SOLO lo usa el creador de perfiles, disparado por un botón del usuario.
// Fuera de ese asistente nunca se invoca: no hay espera de fondo, no queda
// nada corriendo al cerrarlo, y durante el juego normal Kanpachi jamás
// consulta procesos.
type SocketInspector interface {
	Snapshot(ctx context.Context, root domain.ProcessRef) ([]domain.Listener, error)
}

// ExposureAudit alimenta el módulo de alertas de la decisión 19.
//
// Cada método responde una pregunta que Kanpachi no controla y que anula su
// promesa si nadie la mira. Ninguno bloquea: devuelven hallazgos, jamás
// errores fatales que impidan entrar a una sala.
type ExposureAudit interface {
	FirewallEnabled(ctx context.Context) ([]domain.FirewallProfileState, error)
	OwnRulesIntact(ctx context.Context) (bool, error)
	// RouterMappings es la excepción de SOLO LECTURA a "el router no se toca
	// nunca". No hay método para crear ni para borrar, y esa ausencia es
	// deliberada.
	RouterMappings(ctx context.Context) ([]domain.PortMapping, error)
}

// RendezvousProvider resuelve un invite ID a la identidad de ENCUENTRO, jamás
// a la red real. La red real solo llega por el canje de credencial con el host.
//
// La v1 es derivación local: Argon2id sobre el invite ID, sin red y sin
// preguntarle a nadie. Un proveedor remoto daría salas con identidad de
// encuentro rotativa sin tocar UI ni daemon.
type RendezvousProvider interface {
	Resolve(input string) (domain.Room, domain.Rendezvous, error)
}

// RoomDirectory es el registro del seed, y es SOLO PRESENTACIÓN. Que falle no
// impide entrar a ninguna sala: lo que se pierde es la tarjeta.
//
// Recibe y devuelve la tarjeta CIFRADA, en bytes opacos. El cifrado ocurre en
// el dominio, con [domain.SealRoomCard], y la clave se queda en la máquina del
// host: viaja en el fragmento del enlace, que el navegador no manda al
// servidor. Que este puerto hablara de RoomCard en claro obligaría al
// adaptador a cifrar, o sea a decidir con qué, y ahí es donde se filtraría.
type RoomDirectory interface {
	// Open pide un invite ID nuevo. Lo emite el registro porque es quien puede
	// garantizar unicidad dentro de su espacio, y emitir evita el ida y vuelta
	// de proponer y ser rechazado.
	//
	// Devuelve la Room entera, con el seed pegado, y no el ID solo: un invite
	// ID solo significa algo en el registro que lo emitió, y quien sabe cuál es
	// ese registro es este adaptador. Que lo rellenara el caso de uso lo
	// obligaría a suponer el seed por defecto, y quien configuró el suyo en
	// Avanzado repartiría códigos que apuntan al servidor equivocado.
	Open(ctx context.Context, sealed []byte) (domain.Room, error)
	// Lookup devuelve la tarjeta cifrada y cuánta gente hay. El contador puede
	// venir ausente, y ausente dice la verdad: el registro omite el número si
	// nunca pudo hablar con el motor, porque un cero afirmaría que no hay
	// nadie y sería falso.
	Lookup(ctx context.Context, id domain.InviteID) (sealed []byte, members int, err error)
	// Publish actualiza la tarjeta, o reabre la sala con el mismo invite ID.
	Publish(ctx context.Context, id domain.InviteID, sealed []byte) error
}

// ControlChannel es el canal de la sala de la decisión 23.
//
// Solo escucha en la máquina del host. Los invitados únicamente marcan hacia
// afuera y nunca abren un puerto, así que su deny-all queda literalmente
// intacto. Es el código que más revisión merece del proyecto: corre como
// SYSTEM y parsea mensajes de gente que está en la sala.
type ControlChannel interface {
	// Serve arranca el oyente. SOLO el host lo llama, y en dos direcciones con
	// dos alcances distintos: ver [domain.ControlScope]. Llamarlo otra vez
	// reemplaza el alcance, que es lo que hace que expulsar a alguien lo saque
	// también de la lista de quién puede hablar.
	Serve(ctx context.Context, scope domain.ControlScope) error
	// Dial conecta al host. Llamarlo otra vez REEMPLAZA la conexión anterior:
	// el invitado marca primero al vestíbulo para pedir la credencial y después
	// a la dirección del host en la sala, que es la que tiene que quedar viva.
	//
	// Que esta conexión esté caída es lo que alimenta
	// HostPresent y el contador de veinte minutos, y es información confiable
	// sin confiar en nadie: no es un mensaje falsificable, es la ausencia de
	// un socket.
	Dial(ctx context.Context, host netip.Addr) error
	// HostPresence emite true al conectar y false al caerse.
	HostPresence() <-chan bool

	// Announce lo llama SOLO el host, por la dirección de la sala, para
	// contarles a los presentes cómo se llama y qué juego está activo.
	Announce(ctx context.Context, a domain.RoomAnnounce) error
	// Announcements es el lado del invitado. El adaptador solo emite lo que
	// llegó por la conexión al host: un miembro no puede anunciar nada.
	Announcements() <-chan domain.RoomAnnounce

	// Notify le manda un aviso a un miembro. SOLO el host. Una dirección en
	// cero es a todos los presentes.
	//
	// Se llama ANTES de cortarle nada a quien se está expulsando, y ese es el
	// único orden en que sirve: después, el mensaje no tiene por dónde llegar.
	// Que se pueda mandar primero sin regalar nada es porque el aviso NO es lo
	// que expulsa: es cortesía, para que del otro lado la app cierre limpio.
	Notify(ctx context.Context, to netip.Addr, n domain.RoomNotice) error
	// Notices es el lado del invitado. Igual que Announcements, el adaptador
	// solo emite lo que llegó por la conexión al host.
	Notices() <-chan domain.RoomNotice

	// RequestCredential es el paso 5 del canje. El adaptador rellena la llave
	// pública desde identity.key antes de firmar: esa llave vive en disco con
	// ACL propia y core no la conoce, que es justo lo que la decisión 25
	// necesita para que robarla sea la única forma de suplantar a alguien.
	RequestCredential(ctx context.Context, req domain.CredentialRequest) (domain.Credential, error)
	// Close es IDEMPOTENTE, por lo mismo que Leave: lo llama el camino de
	// error, que puede correr antes de que se haya abierto nada.
	Close() error
}

// Clock existe porque hay backoff y vencimientos que testear, y esperar veinte
// minutos en un test no es una opción. Es la única interfaz "de comodidad" del
// proyecto: un StringFormatter no existiría.
type Clock interface {
	Now() time.Time
}

// Logger es la salida de diagnóstico. Texto plano, local, sin telemetría: los
// logs no salen de la máquina salvo que el usuario los copie al portapapeles
// con el botón de diagnóstico.
type Logger interface {
	Info(msg string, kv ...any)
	Warn(msg string, kv ...any)
	Error(msg string, kv ...any)
}
