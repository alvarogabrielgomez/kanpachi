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
//   - No existe BORRAR la cuarentena de base. El daemon la escribe y la repone
//     en cada arranque, porque no hay instalador que la ponga, y no tiene cómo
//     quitarla: [FirewallPort.ApplyBaseQuarantine] solo agrega. Por eso sigue
//     puesta con el servicio detenido. Y no cabe en un RuleSet aunque se
//     quisiera: necesita bloqueo y salida, y [domain.FirewallRule] no puede
//     expresar ninguno de los dos. Ver [domain.QuarantineRule].
//
// Lo que no existe en la interfaz no se puede llamar por error.
package port

import (
	"context"
	"errors"
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

	// RenewCredential empuja el vencimiento de una credencial ya emitida
	// CONSERVANDO su llave, y devuelve la fecha nueva.
	//
	// # Por qué no alcanza con volver a emitir
	//
	// Porque el secreto de la credencial ES la llave estática con la que el
	// invitado tiene abierta su conexión. Emitir otra vez produce una llave
	// nueva, que el invitado solo puede adoptar rehaciendo su instancia del
	// motor, o sea un corte. Renovar cambia una fecha y nada más, y es lo que
	// hace que una sala pueda durar más que una credencial sin que todos
	// reconecten por reloj.
	//
	// Solo un nodo admin, igual que emitir y revocar: quien publica la lista de
	// confianza es el único que puede decir que alguien sigue en ella.
	//
	// **Un id que no existe es un ERROR.** Es lo que contesta el motor cuando la
	// credencial se revocó o venció entre dos latidos, y quien llama tiene que
	// poder distinguirlo de que el motor no conteste, que es el caso donde
	// reintentar sirve.
	//
	// La fecha vuelve del motor en vez de calcularse acá como `ahora + ttl`. Es
	// la misma lección que la dirección del invitado: dos lados calculando lo
	// mismo por separado coinciden hasta que dejan de hacerlo, y acá la
	// discrepancia sería un host convencido de que a alguien le queda un día.
	RenewCredential(ctx context.Context, id domain.CredentialID, ttl time.Duration) (time.Time, error)

	// ListCredentials devuelve las credenciales vivas SIN dirección virtual.
	//
	// El cero en `VirtualIP` no es un descuido del adaptador: el motor no
	// conoce ese dato. La dirección la reparte el host en
	// `Session.IssueCredentialFor` y no viaja en la orden de emisión, así que
	// el motor solo puede contestar id y vencimiento.
	//
	// **Filtrar esta lista por IP no encuentra nunca a nadie**, y hacerlo costó
	// una versión entera en la que ningún invitado podía entrar: el host
	// pre-autorizaba el canal de control para una dirección cero, la regla de
	// firewall no llegaba a existir, y el invitado se quedaba esperando en un
	// `dial` que nadie contestaba. Quien necesite el lazo IP↔credencial lo tiene
	// en la sesión, no acá.
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
	// **El canal vive lo que vive el adaptador y se cierra solo al cerrarlo.**
	// Un cierre significa que el adaptador terminó, jamás que murió un proceso:
	// eso viaja como [domain.EngineDied], que es un hecho distinto.
	//
	// Esa distinción es funcional y está medida. El supervisor lee un canal
	// cerrado como muerte del motor, y lo dice en su propio código. Un
	// adaptador que devolviera un canal cerrado mientras todavía no arrancó
	// nada haría que "no existe" y "se murió" fueran el mismo hecho para quien
	// escucha, y el watchdog gastaría sus intentos reiniciando una sala que
	// nadie llegó a crear. Pasó, con el daemon de verdad, y terminó cerrando
	// una sala inexistente tras ocho intentos.
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

	// ApplyBaseQuarantine escribe la cuarentena de base y REPONE lo que falte.
	//
	// **No existe el método para borrarla, y esa ausencia es la protección.** Lo
	// que la cuarentena vale es seguir puesta con el daemon detenido, así que la
	// capacidad de quitarla no puede vivir en la interfaz que el daemon usa. Un
	// guardián en internal/arch falla si aparece un método que junte un verbo
	// destructivo con "Base" o "Quarantine".
	//
	// Es aditivo y IDEMPOTENTE: agrega lo que falta y no toca lo que ya está.
	// Llamarlo con la cuarentena entera puesta no escribe nada.
	//
	// Lo llama [NewSession] al arrancar, ANTES de PurgeOwned. Antes y no después
	// porque la purga es el instante de menos protección de todo el arranque,
	// con las reglas de la sala anterior cayendo.
	ApplyBaseQuarantine(ctx context.Context, rules []domain.QuarantineRule) error

	// PurgeOwned borra todo lo etiquetado con [domain.FirewallGroup]. Se llama al
	// arrancar el servicio, antes de aplicar nada: una muerte sucia del daemon
	// nunca deja puertos huérfanos abiertos.
	//
	// **Jamás toca [domain.FirewallGroupBase].** Esa es la cuarentena de base, y
	// es lo único que protege la máquina mientras el daemon no corre: si la purga
	// se la llevara, cada reinicio del servicio desarmaría la protección, y el
	// fallo sería invisible porque todo seguiría funcionando igual.
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

	// BindRoom acota las DOS capas a los adaptadores virtuales, y sin él la
	// compuerta no se pone nunca.
	//
	// # Por qué lo llama el caso de uso y no el adaptador
	//
	// Porque quien sabe CUÁNDO existe el adaptador es este lado. Los crea el
	// motor al levantar cada red, así que no existen cuando arranca el daemon
	// ni cuando se construye el firewall: el único momento en que se puede
	// acotar es justo después de que la red esté arriba, y eso solo lo sabe
	// quien la levantó.
	//
	// El nombre del adaptador NO viaja: son [domain.AdapterName] y
	// [domain.LobbyAdapterName], constantes del dominio. Lo que viaja son los
	// dos rangos, que se eligen en tiempo de ejecución, y cuántos adaptadores
	// hay ahora.
	//
	// `lobby` es el /24 del vestíbulo de esta sala, y solo hace falta con
	// [domain.BindRoomAndLobby]. Viaja desde que dejó de ser una constante: lo
	// deriva cada sala de su código, así que el firewall no lo puede saber solo.
	// Ver [domain.Rendezvous.LobbySubnet].
	//
	// **Falla en la cara y hay que tratarlo como fatal.** No es como los
	// ajustes del adaptador, donde un fallo degrada la partida: sin compuerta
	// la lista de permitidos vuelve a ser ADITIVA, o sea que una regla ajena de
	// escritorio remoto alcanza al usuario por la red virtual. Una sala que no
	// se abre es mejor que una que dice estar contenida y no lo está.
	BindRoom(ctx context.Context, room, lobby netip.Prefix, with domain.RoomBinding) error

	// UnbindRoom olvida los adaptadores. Se llama al salir de la sala, DESPUÉS
	// de cerrar lo que estuviera abierto: al revés quedaría un instante con
	// puertos abiertos y sin compuerta que los acote.
	UnbindRoom()
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
// Son tres archivos y tres motivos distintos:
//
//	hosted-room.json  SOLO EN EL HOST. La sala que estaba abierta. Salir limpio
//	                  lo borra y morir sucio lo deja, así que su sola presencia
//	                  al arrancar es la señal de que hubo un mal cierre.
//	last-room.json    SOLO EN INVITADOS. La última sala, para poder volver.
//	                  Lleva el código y nada que sirva para entrar sin pasar
//	                  por el host.
//	seed.txt          En los dos. El registro en el que ESTA máquina abre
//	                  salas, ELEGIDO por la persona. Ver [StateStore.LoadSeed].
//
// Los dos primeros nombres son un PAR, y eso es lo que dicen: el eje no es
// "actual contra anterior", es host contra invitado. `hosted-room.json` lo
// escribe solo quien hospeda y lleva la identidad de la red real; el otro lo
// escribe solo quien entró y no lleva nada que sirva para entrar sin el host.
//
// Que cualquiera de los tres falte NO es un error: los dos primeros porque es
// lo normal en una instalación nueva y en toda salida limpia, y el tercero
// porque nadie ha hospedado todavía.
type StateStore interface {
	LoadRoom() ([]byte, error)
	SaveRoom([]byte) error
	ClearRoom() error

	LoadLast() ([]byte, error)
	SaveLast([]byte) error
	ClearLast() error

	// LoadSeed y SaveSeed son el registro en el que esta máquina abre salas.
	//
	// # Por qué hace falta guardarlo, desde que no hay seed por defecto
	//
	// Porque al ENTRAR el registro sale del código pegado, y al CREAR no hay
	// código todavía: el código es justamente lo que el registro emite. Así que
	// crear necesita saber a quién preguntarle, y antes lo contestaba una
	// constante compilada que ya no existe.
	//
	// # Se llena de UNA sola forma, y eso es una decisión
	//
	// La persona lo escribe, una vez. Hubo una versión de este diseño donde
	// además se adoptaba solo el registro de la primera sala a la que se
	// entrara, para no preguntar nunca. Resuelve un roce real y trae algo peor:
	// **la siguiente sala que abras se hospeda en el servidor de un desconocido
	// por omisión**, y lo que el diálogo de confianza te enseña para confirmar
	// ya viene inclinado hacia ese servidor.
	//
	// La regla que queda: lo que se aprende de una sala no manda en ninguna
	// decisión que no sea de esa sala. Quien nunca hospedó pasa por la misma
	// pantalla que pasa un host, que es coherente con que hospedar suponga un
	// papel algo más técnico.
	//
	// # De dónde sale entonces el valor con el que la pantalla se prellena
	//
	// De `last-room.json`, que **ya guarda el seed de la última sala a la que se
	// entró** y no lo borra nunca. Prellenar no necesita estado nuevo, y que
	// venga de ahí es lo que mantiene los dos significados separados sin tener
	// que explicarlos: uno es "el registro de esta máquina" y el otro es
	// literalmente "la última sala a la que entraste". Ver [Session.LastRoom].
	//
	// Que falte es lo normal hasta que alguien lo escriba, y no es un error.
	LoadSeed() ([]byte, error)
	SaveSeed([]byte) error

	// LoadSeedToken, SaveSeedToken y ClearSeedToken guardan el refresh token de
	// un seed que pide password para hospedar.
	//
	// # Qué queda en disco, y qué jamás
	//
	// Queda el REFRESH, sellado y con ACL propia. **El password no**, ni acá ni
	// en ningún sitio: un disco robado entrega un token que caduca y que el
	// operador revoca cambiando el password, jamás un password que la persona
	// reusa en otros sitios.
	//
	// El access token tampoco. Vive quince minutos, así que guardarlo pondría una
	// credencial viva donde se puede robar a cambio de ahorrar un viaje después
	// de reiniciar.
	//
	// # Por qué se BORRA, a diferencia del seed
	//
	// Porque un token deja de valer solo, y el fichero no. El operador cambia el
	// password y todo lo emitido muere en el acto; lo que queda en disco es un
	// secreto caducado que nadie va a usar. Ver [domain.SeedToken].
	LoadSeedToken() ([]byte, error)
	SaveSeedToken([]byte) error
	ClearSeedToken() error
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

	// ProbeCanary marca el canario de OTRA máquina por los DOS protocolos.
	//
	// Va acá y no en una interfaz aparte, y la razón es que así los dos no
	// pueden cablearse a objetos distintos: es el mismo adaptador, con la misma
	// doctrina de devolver tipos del dominio y jamás un error. Una interfaz
	// propia le daría a Deps un campo que siempre vale lo mismo, y a validate()
	// una dependencia que no puede faltar por su cuenta.
	//
	// Los dos protocolos y no uno, porque la compuerta no mira el protocolo: sus
	// bloqueos llevan condición de adaptador y de rango, y ninguna de protocolo.
	// Comprobar los dos con una sola pregunta hace que el resultado valga
	// también para UDP, que es por donde habla la herramienta que más preocupa.
	//
	// El número hace falta por UDP, que no tiene conexión: sin él, cualquier
	// paquete suelto que llegara en ese momento se leería como que el canario
	// contestó, o sea como una fuga que no existe.
	ProbeCanary(ctx context.Context, at netip.AddrPort, nonce domain.CanaryNonce) (tcp, udp domain.ProbeOutcome)
}

// CanaryPort abre el oyente que existe PARA SER BLOQUEADO.
//
// Existe porque en Windows un puerto callado no distingue "bloqueado" de "no hay
// nadie escuchando", que está medido. Poner un oyente detrás de la puerta a
// propósito es lo único que le deja al silencio una sola lectura. Ver
// `daemon/adapter/canary`.
type CanaryPort interface {
	// Listen liga en `at`, en un puerto libre en TCP y UDP a la vez, y esquiva
	// los que `avoid` rechace.
	//
	// `at` es la dirección de la sala, y una sin especificar la rechaza el
	// adaptador: ligar en todas las interfaces abriría un puerto en la red de
	// casa del usuario, que es justo lo que este producto promete no hacer.
	//
	// `avoid` es [domain.RuleSet.Allows] del conjunto que se está aplicando. Un
	// puerto que la propia Kanpachi abrió contestaría con toda razón, y esa
	// respuesta se leería como una fuga.
	Listen(at netip.Addr, nonce domain.CanaryNonce, ttl time.Duration,
		avoid func(uint16) bool) (Canary, error)
}

// Canary es un oyente abierto.
//
// Las dos lecturas del toque se leen igual que [context.Context]: Touched como
// Done, y WasTouched como Err. Y por el mismo motivo, que una es para esperar y
// la otra para concluir después de que todo terminó.
type Canary interface {
	Port() uint16
	// Touched se cierra en el primer toque. Cerrar el canario NO lo cierra, y esa
	// omisión es de seguridad: un canal cerrado se lee como listo.
	Touched() <-chan struct{}
	// WasTouched es el hecho, y vale también después de cerrar, que es cuando el
	// host lo mira.
	WasTouched() bool
	Close() error
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

// ErrUnknownRoom es el registro diciendo que ese invite ID NO EXISTE.
//
// # Por qué es un centinela y no un error más
//
// Porque es la única respuesta del registro que AFIRMA algo. Todo lo demás que
// puede salir mal —que no haya red, que el servidor devuelva 500, que venza el
// plazo— es ausencia de información. Hoy las dos paran el ingreso, así que la
// distinción ya no está en si se sigue: está en qué se le dice a quien esperaba
// jugar, y en si insistir tiene sentido.
//
//	"no existe"    → el código está mal o venció. Volver a intentarlo no cambia
//	                 nada, hay que pedir uno nuevo.
//	no contestó    → el registro está caído. El código puede estar perfecto, y
//	                 lo que corresponde es reintentar en un rato.
//
// Confundirlos manda a la persona a hacer justo lo contrario de lo que sirve.
//
// # Lo que cambió, y por qué se pudo cambiar
//
// Antes esto era el único fallo que detenía a `JoinRoom`, con el argumento de
// que el registro es presentación y la sala vive en las máquinas de sus
// miembros. La segunda mitad sigue siendo cierta y la conclusión era falsa: la
// máquina del registro es la MISMA que hace de punto de encuentro, así que sin
// ella el vestíbulo no se forma. Ver `Session.checkRoomExists`.
//
// Eso exige que el "no" sea confiable, y por eso el registro persiste sus salas
// en disco antes que esto endureciera. Ver la cabecera de `registry/persist.go`.
var ErrUnknownRoom = errors.New("el registro no conoce esa sala")

// ErrNoOwnSeed es que esta máquina todavía no tiene registro donde abrir salas.
//
// No es un fallo de red ni una configuración rota: es el estado normal de quien
// nunca hospedó ni entró a ninguna sala, porque el registro se aprende de una de
// las dos cosas. Ver [StateStore.LoadSeed].
//
// Se separa de [ErrUnknownRoom] y de un fallo de transporte porque lo que hay
// que hacer es distinto: acá no se reintenta ni se revisa un código, se
// configura un registro una vez.
var ErrNoOwnSeed = errors.New("esta máquina no tiene registro configurado")

// ErrSeedPassword es que ese registro pide password para HOSPEDAR y esta
// máquina no tiene con qué contestarle.
//
// Cubre los tres momentos en que pasa lo mismo: nunca se escribió ninguno, el
// refresh caducó, y el operador cambió el password y botó a todos. Son uno solo
// a propósito, porque **lo que hay que hacer es idéntico** y el registro además
// se niega a decir cuál de los tres fue: distinguirlos solo le regalaría
// información a quien esté probando contraseñas.
//
// Entrar a una sala nunca lo produce. Este centinela solo aparece abriendo,
// reabriendo o renovando el código.
var ErrSeedPassword = errors.New("ese registro pide password para hospedar")

// RoomDirectories entrega el registro con el que hay que hablar en cada caso, y
// existe porque **ya no hay uno solo**.
//
// # Los dos casos, que no son el mismo
//
// Al ENTRAR, el registro sale del código que pegaron: un invite ID solo
// significa algo en el registro que lo emitió, así que preguntarle a otro
// devolvería "no existe" sobre una sala que existe perfectamente. Al CREAR no
// hay código todavía, porque el código es justo lo que el registro emite, y ahí
// manda el registro de esta máquina.
//
// Antes esto era un solo adaptador construido con una constante compilada, y por
// eso entrar con un código ajeno se saltaba la comprobación entera y perdía la
// tarjeta. Ver la decisión 16.
type RoomDirectories interface {
	// Own es el registro en el que ESTA máquina abre salas. Devuelve
	// [ErrNoOwnSeed] cuando todavía no hay ninguno.
	Own() (RoomDirectory, error)
	// For es el registro que emitió un código, tomado del código.
	//
	// El nombre llega desde `domain.ParseRoom`, o sea ya validado como nombre y
	// nunca como dirección. El adaptador vuelve a comprobar lo que resuelva, en
	// cada uso, porque un nombre impecable puede apuntar a la red de casa.
	For(seed string) (RoomDirectory, error)
	// SetOwn cambia a qué registro apunta [RoomDirectories.Own].
	//
	// **Solo la memoria**: persistir es del caso de uso, que es quien sabe si el
	// valor llegó a disco. Escribir primero y cambiar después es lo que impide
	// que un fallo del disco deje esta sesión apuntando a un sitio que el próximo
	// arranque no va a recordar.
	//
	// Cambiarlo con una sala abierta es seguro y no la toca: publicar, renovar y
	// reabrir van al registro de ESA sala, no a este. Lo que cambia es dónde se
	// abre la siguiente.
	SetOwn(seed string)
}

// RoomDirectory es el registro del seed, y es EL PUNTO DE ENCUENTRO. Sin él no
// se abre una sala ni se entra a ninguna.
//
// # Por qué esto no es "solo presentación", que es lo que decía acá
//
// Porque la tarjeta es lo de menos. La misma máquina que sirve este registro es
// la que viaja como par en la configuración del motor, o sea el sitio al que
// llegan el host y el invitado para encontrarse. Y los invite IDs los emite
// este registro: un código inventado en el cliente no lo reconoce nadie, ni
// siquiera el invitado que lo pegue. Ver `Session.publishCard`.
//
// De ahí que crear y entrar fallen rápido cuando no contesta, y que exista
// [RoomDirectory.Reachable] para saberlo antes de que alguien invierta nada.
//
// Recibe y devuelve la tarjeta CIFRADA, en bytes opacos. El cifrado ocurre en
// el dominio, con [domain.SealRoomCard], y la clave se queda en la máquina del
// host: viaja en el fragmento del enlace, que el navegador no manda al
// servidor. Que este puerto hablara de RoomCard en claro obligaría al
// adaptador a cifrar, o sea a decidir con qué, y ahí es donde se filtraría.
type RoomDirectory interface {
	// Seed es el nombre del registro con el que este adaptador habla.
	//
	// Existe por una razón concreta y una sola: un invite ID **solo significa
	// algo en el registro que lo emitió**, así que preguntarle a este por un
	// código servido por otro devuelve "no existe" sobre una sala que existe
	// perfectamente. Quien vaya a creerle a un "no existe" tiene que poder
	// comprobar antes que está preguntando en el sitio correcto. Ver el fallo
	// temprano de `JoinRoom`.
	Seed() string
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
	//
	// **Ausente llega como -1, jamás como 0.** El adaptador no inventa el cero
	// que el registro se negó a decir: son dos afirmaciones distintas, "no hay
	// nadie" y "no lo sé", y solo una de las dos es cierta cuando el contador
	// no arrancó.
	Lookup(ctx context.Context, id domain.InviteID) (sealed []byte, members int, err error)
	// Publish actualiza la tarjeta, o reabre la sala con el mismo invite ID.
	//
	// **No crea nada.** Un invite ID que este registro jamás emitió, o uno cuyo
	// fijado ya venció y se barrió, contestan que no existe. Ahí está el límite
	// de "o reabre": vale mientras el fijado siga vivo, y ese fijado es lo único
	// que impide que un ex miembro que se quedó con el código llegue primero.
	//
	// La firma sale de `identity.key`, que vive en disco con ACL propia y core
	// no conoce. Es lo que cierra el agujero que el cifrado no puede: la clave
	// de la tarjeta se deriva del enlace, así que cualquiera que lo recibió
	// puede fabricar una tarjeta que la página descifra.
	Publish(ctx context.Context, id domain.InviteID, sealed []byte) error
	// Authenticate cambia una prueba de password por la credencial con la que
	// este registro deja HOSPEDAR.
	//
	// Recibe [domain.SeedAuthProof] y jamás el password: el hash se calcula
	// antes de llegar acá, con el host del registro dentro, para que el operador
	// de un seed no aprenda un password que la persona reusa en otros sitios.
	//
	// Se llama una sola vez, cuando alguien lo escribe. El resto del tiempo el
	// adaptador renueva su credencial solo, y lo que le dice a esta capa que hay
	// que volver a preguntar es [ErrSeedPassword] saliendo de otra operación.
	//
	// En un registro abierto contesta que no pide password, y eso NO es un fallo
	// que haya que reintentar: es que no había puerta.
	Authenticate(ctx context.Context, proof string) error
	// Reachable dice si el registro CONTESTA, sin preguntarle por ninguna sala.
	//
	// # Por qué hace falta preguntarlo aparte
	//
	// Porque desde que crear y entrar fallan rápido sin registro, no tenerlo es
	// la diferencia entre poder usar Kanpachi y no poder. Y las dos operaciones
	// que lo descubren son justo las que la persona lanza DESPUÉS de elegir un
	// juego y escribir ocho caracteres. Esto se puede saber antes, con la
	// pantalla todavía vacía.
	//
	// # Qué NO es
	//
	// No es un sondeo de salas y no lleva ningún invite ID: preguntar por uno
	// inventado ensuciaría el registro y mediría otra cosa. Un error acá
	// significa "no contesta" y nunca "esa sala no existe", que es la
	// distinción que sostiene [ErrUnknownRoom].
	Reachable(ctx context.Context) error
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
