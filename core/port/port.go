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
//   - No existe aplicar la cuarentena de base. La pone el instalador y el daemon
//     jamás la toca, así que sigue puesta con el servicio detenido. Tampoco
//     podría: necesita bloqueo y salida, y [domain.FirewallRule] no tiene cómo
//     expresar ninguno de los dos. Ver [domain.FirewallGroupBase].
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

	// Restart vuelve a levantar el motor con la ÚLTIMA especificación con la
	// que se arrancó.
	//
	// Es el MECANISMO del watchdog. Cuántas veces y cada cuánto es POLÍTICA y
	// vive en el supervisor: el adaptador sabe cómo arrancarlo, no sabe cuándo
	// hay que rendirse.
	//
	// La especificación no vuelve a core, y por eso existe este método en vez
	// de repetir HostNetwork: el secreto de la red real se queda dentro del
	// adaptador, que es el único sitio del programa que lo necesita.
	//
	// Sin arranque previo devuelve error y no inventa ninguna sala.
	Restart(ctx context.Context) error

	Peers(ctx context.Context) ([]domain.Peer, error)
	// Events es el canal por el que el motor empuja. El supervisor lo escucha
	// y traduce cada evento a una transición de la máquina de estados.
	//
	// Devuelve el canal del proceso ACTUAL. Tras un Restart hay un canal nuevo
	// y el anterior se cierra, así que quien escuche tiene que volver a
	// pedirlo. El supervisor lo hace en cada latido, comparando por identidad.
	Events() <-chan domain.EngineEvent
	Diagnostics(ctx context.Context) (domain.NetCheck, error)
}

// FirewallPort es declarativo. Recibe el estado deseado y calcula la
// diferencia contra lo aplicado: no hay "agregar regla" imperativo suelto, así
// que un cambio de miembros o de juego no puede dejar nada colgando del
// cálculo anterior.
type FirewallPort interface {
	// Apply lleva el firewall al conjunto deseado.
	//
	// **La diferencia se calcula contra las reglas VIVAS del grupo Kanpachi,
	// enumeradas del sistema en CADA llamada, jamás contra una copia en memoria
	// de lo último que se pidió.** De eso dependen dos cosas, y las dos son
	// funcionales y no estéticas: que Apply sea idempotente, y que reaplicar el
	// mismo conjunto REPARE lo que alguien haya borrado o agregado por fuera.
	//
	// Con un recuerdo en memoria, reaplicar un conjunto igual sería un no-op y
	// la autorreparación del módulo de exposición no existiría.
	Apply(ctx context.Context, desired domain.RuleSet) error
	// PurgeOwned borra todo lo etiquetado con [domain.FirewallGroup]. Se llama al
	// arrancar el servicio, antes de aplicar nada: una muerte sucia del daemon
	// nunca deja puertos huérfanos abiertos.
	//
	// **Jamás toca [domain.FirewallGroupBase].** Esa es la cuarentena que puso el
	// instalador, y es lo único que protege la máquina mientras el daemon no
	// corre: si la purga se la llevara, cada reinicio del servicio desarmaría la
	// protección, y el fallo sería invisible porque todo seguiría funcionando
	// igual.
	//
	// La comparación es por IGUALDAD EXACTA del grupo, jamás por prefijo.
	// "Kanpachi" es prefijo de "Kanpachi-base", así que un HasPrefix acá borra la
	// cuarentena. Lo vigila un guardián en internal/arch.
	PurgeOwned(ctx context.Context) error

	// AuditForeign busca reglas permisivas que Kanpachi no creó. Consulta el
	// almacén de reglas por ruta de ejecutable: no enumera procesos y no sabe
	// si el programa está corriendo.
	//
	// **Busca DOS cosas, y la segunda es la que importa de verdad.** El
	// ejecutable del perfil activo, que es el caso obvio, y además todos los de
	// [domain.RemoteAccessExecutables].
	//
	// Este método recibía SOLO el perfil, así que una regla de Parsec o de
	// Sunshine no se miraba nunca. Y ese es el único camino conocido por el que
	// alguien de la sala consigue teclado, pantalla y sistema de archivos del
	// host: la cuarentena tapa el escritorio remoto ESTÁNDAR por puerto, y
	// estas herramientas escuchan donde el usuario les diga.
	//
	// `p` puede ir vacío cuando no hay juego activo, y la auditoría de control
	// remoto sigue valiendo igual. Cada hallazgo llega clasificado por
	// [domain.ClassifyForeign], que es dominio: el adaptador lee Windows y no
	// decide qué es peligroso.
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

// StateStore guarda lo que tiene que sobrevivir a un arranque.
//
// Devuelve bytes crudos y no structs, por lo mismo que [CatalogStore]: el
// decodificador estricto vive en el dominio, que es el único sitio con las
// invariantes. Un adaptador que decidiera qué es una sala válida movería la
// política fuera de core.
//
// Son dos archivos y dos motivos distintos:
//
//	room.json       SOLO EN EL HOST. La sala que estaba abierta. Salir limpio
//	                lo borra y morir sucio lo deja, así que su sola presencia
//	                al arrancar es la señal de que hubo un mal cierre.
//	last-room.json  SOLO EN INVITADOS. La última sala, para poder volver. Lleva
//	                el código y nada que sirva para entrar sin pasar por el
//	                host.
//
// Que cualquiera de los dos falte NO es un error: es lo normal en una
// instalación nueva y en toda salida limpia.
type StateStore interface {
	LoadRoom() ([]byte, error)
	SaveRoom([]byte) error
	ClearRoom() error

	LoadLast() ([]byte, error)
	SaveLast([]byte) error
	ClearLast() error
}

// SystemEvents son las cosas que le pasan a la MÁQUINA y que invalidan lo que
// Kanpachi dejó puesto. No son eventos del motor ni de la sala.
//
// Tres canales y no uno con un enum, a diferencia de [domain.EngineEvent]:
// aquel viene de una sola fuente, el proceso hijo, y estos vienen de tres
// subsistemas de Windows que no se conocen entre sí, el visor de eventos, la
// bomba de mensajes de una ventana oculta y el aviso de cambio de red. Un canal
// por fuente hace que una suscripción muerta se VEA, porque su canal se cierra
// y los otros dos siguen.
//
// Ninguno de los tres es fiable, y el supervisor no los trata como si lo
// fueran: reaplica los ajustes del adaptador cada tantos latidos aunque no
// llegue ningún evento. Una suscripción muerta sin ese respaldo se traduce en
// "ayer funcionaba".
type SystemEvents interface {
	// NetworkIdentified emite cada vez que Windows identifica una red, o sea el
	// Event ID 10000 de Microsoft-Windows-NetworkProfile/Operational. Es cuando
	// Windows revierte la métrica del adaptador, la categoría y las rutas.
	NetworkIdentified() <-chan struct{}
	// Resumed emite al despertar de suspensión o hibernación, y tras Fast
	// Startup. Deja endpoints muertos y sesiones colgadas.
	Resumed() <-chan struct{}
	// NetworkChanged emite cuando cambia la conectividad: WiFi a cable, cable a
	// LTE. Una IP pública nueva obliga a renegociar sin perder la sala.
	NetworkChanged() <-chan struct{}
	// Close corta las suscripciones y cierra los tres canales.
	//
	// Es idempotente y NUNCA espera a que alguien lea. Un adaptador que
	// bloqueara en Close esperando lector colgaría el apagado del servicio.
	Close() error
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

	// Enforcement devuelve lo que el sistema tiene puesto AHORA, medido en las
	// dos capas: las reglas vivas del grupo propio y el filtro de paquetes
	// acotado al adaptador virtual.
	//
	// **Mide, jamás juzga.** Quien decide si eso es lo que se pidió es
	// [domain.Enforcement.Diff], que es dominio y se testea sin Windows. Esto
	// reemplaza a un `OwnRulesIntact(bool)` que no podía decir QUÉ falta, y
	// resulta que esa era la frase que la pantalla necesitaba.
	//
	// Que falle levanta [domain.AlertAuditFailed]. Un adaptador que devuelva el
	// cero de [domain.Enforcement] está diciendo que no hay nada puesto y que
	// la compuerta no se pudo comprobar, que es lo correcto para el que todavía
	// no existe.
	Enforcement(ctx context.Context) (domain.Enforcement, error)
	// RouterMappings es la excepción de SOLO LECTURA a "el router no se toca
	// nunca". No hay método para crear ni para borrar, y esa ausencia es
	// deliberada.
	RouterMappings(ctx context.Context) ([]domain.PortMapping, error)
}

// Prober marca un puerto TCP de OTRA máquina y dice qué contestó.
//
// Es el único puerto del proyecto que sale a la red por su cuenta, y por eso su
// firma es tan chica: recibe una dirección y un puerto, y devuelve qué pasó.
// No manda datos, no lee nada, y cierra en cuanto sabe. Un `Send([]byte)` acá
// convertiría el botón de diagnóstico en un cliente de cualquier cosa.
//
// # Por qué devuelve un tipo del dominio y no un error
//
// Porque distinguir un apretón de manos de un RST y de un silencio es LEER el
// sistema operativo, que es exactamente el trabajo de un adaptador. Lo que
// significa cada uno de los tres es política, y eso se queda en
// [domain.ProbeReport.Verdict], que se testea sin red.
//
// Un `error` obligaría a quien llama a interpretar cadenas de texto del sistema
// para separar "no contestó" de "no se pudo preguntar", y esas dos cosas dicen
// lo contrario la una de la otra.
type Prober interface {
	// Probe marca y espera como mucho [domain.ProbeDeadline], o hasta que el
	// contexto se cancele.
	//
	// La duración solo tiene sentido con [domain.ProbeAnswered]: en el silencio
	// lo que mide es el plazo, que ya se sabe de antemano.
	Probe(ctx context.Context, at netip.AddrPort) (domain.ProbeOutcome, time.Duration)
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

	// AnnounceCode le reparte el invite ID NUEVO a los miembros presentes, tras
	// renovarlo. SOLO el host.
	//
	// Existe porque renovar tenía un efecto secundario que nadie había
	// nombrado: los que están dentro se quedan con el código viejo guardado.
	// Siguen en la sala y la partida no se entera, y el día que quieran volver
	// tienen un código muerto. La confianza ya está dada, están dentro porque
	// el host los dejó entrar.
	//
	// **El adaptador lo SELLA contra la llave pública de cada miembro**, la
	// misma que llegó en su pedido de credencial. Core no ve llaves. No es una
	// llave nueva ni un almacén nuevo: vive lo que dura la sesión de esa
	// persona y se descarta al salir, así que no es identidad persistida y no
	// habilita ningún baneo.
	AnnounceCode(ctx context.Context, r domain.Room) error
	// Codes es el lado del invitado. Igual que los otros dos canales, el
	// adaptador solo emite lo que llegó por la conexión al host.
	Codes() <-chan domain.Room

	// RequestCanary le pide a UN miembro que marque al canario del host. SOLO
	// el host, y a uno por llamada.
	//
	// **En el cable NO viaja ninguna dirección**, y esa ausencia es la
	// invariante: el invitado marca a la dirección de la conexión que ya tiene
	// abierta contra el host. Con un campo de destino, este mensaje convertiría
	// el canal de la sala en un escáner de puertos por encargo contra terceros,
	// y el tráfico saldría de las casas de los miembros.
	RequestCanary(ctx context.Context, to netip.Addr, req domain.CanaryRequest) error
	// CanaryReports es el lado del HOST. El remitente lo pone el adaptador
	// desde la conexión, así que un miembro no puede informar por otro.
	//
	// Lo que llega por acá es una PISTA. Lo que el host da por cierto es lo que
	// vio su propio canario. Ver [domain.CanaryCheck].
	CanaryReports() <-chan domain.CanaryReport

	// CanaryRequests es el lado del INVITADO. Cada pedido llega con la
	// dirección del host ya puesta desde la conexión.
	CanaryRequests() <-chan domain.CanaryRequest
	// SendCanaryReport contesta por la conexión de la sala. No espera acuse:
	// perder un informe cuesta que esa ronda quede sin confirmar, que ya es un
	// estado del dominio.
	SendCanaryReport(ctx context.Context, r domain.CanaryReport) error

	// RequestCredential es el paso 5 del canje. El adaptador rellena la llave
	// pública desde identity.key antes de firmar: esa llave vive en disco con
	// ACL propia y core no la conoce, que es justo lo que la decisión 25
	// necesita para que robarla sea la única forma de suplantar a alguien.
	RequestCredential(ctx context.Context, req domain.CredentialRequest) (domain.Credential, error)
	// Close es IDEMPOTENTE, por lo mismo que Leave: lo llama el camino de
	// error, que puede correr antes de que se haya abierto nada.
	//
	// **NUNCA espera a que alguien lea sus canales, y sus emisores están
	// amortiguados.** No es higiene, es lo que evita un abrazo mortal real: el
	// caso de uso llama a Close con el candado de la sesión tomado, y si Close
	// esperara a su goroutine emisora mientras esa goroutine está bloqueada
	// escribiendo en HostPresence, las dos se esperarían para siempre y el
	// daemon quedaría colgado con la sala a medio cerrar.
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
