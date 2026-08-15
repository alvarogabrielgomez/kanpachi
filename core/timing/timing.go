// Package timing es TODOS los relojes de Kanpachi, en un solo sitio.
//
// Cada plazo, cada cadencia, cada vencimiento y cada margen de gracia del
// producto se declara acá y en ningún otro lado. Lo importan el daemon, el
// registro del seed, la interfaz de terminal y las herramientas, así que
// cambiar un número es editar una línea, y los tests leen exactamente el mismo
// valor que corre en producción.
//
// # Por qué juntos, si cada uno pertenece a un caso de uso distinto
//
// Porque repartidos no se pueden comparar, y compararlos es justo lo que hace
// falta para no romper nada. Media docena de estos plazos SOLO son correctos en
// relación con otro: [AnnounceInterval] tiene que caber tres veces en
// [HostSilenceLimit], [ReconnectLimit] tiene que ser más corto que
// [HostAbsenceLimit], [CanaryTTL] tiene que ser más largo que
// [CanaryRoundDeadline], [RegistryDialTimeout] tiene que caber dentro de
// [RegistryRequestTimeout], y [SeedCheckTimeout] tiene que ser más corto que el
// plazo del pipe o el fallo llega con el nombre equivocado. Con los números en
// nueve paquetes, esas relaciones se sostenían leyendo seis ficheros.
//
// Juntarlos encontró además dos que estaban escritos dos veces con dos nombres:
// el apagado ordenado y el refresco de una vista en vivo.
//
// # Son constantes de COMPILACIÓN, y eso es una invariante
//
// Una `var` se puede reasignar desde cualquier init del programa; una `const` no
// existe en tiempo de ejecución. De eso depende que ningún agente externo pueda
// alargar un corte automático y quedarse conectado para siempre, y por eso no
// hay aquí ni un solo setter, ni una lectura de fichero, ni un valor que salga
// de otro identificador. Lo vigila `internal/arch/corte_test.go`.
//
// "Configurable" quiere decir fácil de encontrar y editar en el fuente, jamás
// ajustable desde fuera. Un fichero que ajustara [ReturnInterval] sería una
// cadencia contra el registro que alguien deja en un segundo.
//
// # Qué NO va acá
//
// Lo que no se mide en tiempo. Los topes de bytes, los de conexiones y los de
// intentos se quedan junto al código que los aplica: `MaxConns` acota plazas de
// un pipe, `maxRelaunches` cuenta caídas, `CanaryRepairLimit` cuenta
// reposiciones. Meterlos convertiría esto en el cajón de las constantes, que es
// otra cosa y peor.
//
// La excepción es [AdapterReapplyEvery], que se cuenta en latidos: son dos
// minutos escritos como ocho, así que es un reloj disfrazado de número.
//
// # Por qué en core/ y no en internal/
//
// Porque los plazos son reglas del producto, no de quien lo publica. En
// `internal/` viven la identidad de quien publica (`brand`) y los nombres que
// dos binarios escriben en disco (`layout`); esto es conducta. Y así el
// guardián de pureza de `internal/arch`, que recorre `core/` entero, lo cubre
// sin que nadie lo agregue a una lista: este paquete no importa nada más que
// `time`, que es lo que mantiene a core corriendo en CI sin privilegios, sin
// red y sin Windows.
package timing

import "time"

// ─── La sala: presencia del host y cortes automáticos ────────────────────────

// HostAbsenceLimit es la salida automática de la decisión 20.
//
// Veinte minutos y no dos: el margen tiene que cubrir un reinicio del host con
// holgura. Una salida inmediata echaría a todo el mundo cada vez que al host
// se le cierra el juego, que es el caso más común de todos.
//
// Es política LOCAL pura. No hay mensaje, no hay coordinación y no hay que
// confiar en nadie: cada máquina decide sobre sí misma a partir de un hecho
// que no se puede falsificar, que su socket al host no está.
const HostAbsenceLimit = 20 * time.Minute

// HostSilenceLimit es el respaldo del socket que miente.
//
// Que la conexión de control esté caída es la señal buena, y es una señal que
// puede no llegar NUNCA: un socket TCP medio abierto sobrevive horas a una
// máquina apagada de golpe, sin FIN y sin RST. Sin esto, ese caso deja la
// presencia clavada en true y el contador de veinte minutos no arranca jamás.
//
// Seis minutos son tres anuncios perdidos, o sea tres [AnnounceInterval].
// Vencer no saca a nadie de la sala: marca al host como ausente, que es lo que
// arranca [HostAbsenceLimit].
const HostSilenceLimit = 6 * time.Minute

// ReconnectLimit es cuánto se tolera estar sin túnel antes de rendirse.
//
// Diez minutos y no veinte, y la asimetría con [HostAbsenceLimit] tiene razón.
// La ausencia del host es que falta la persona con la red impecable, y ahí
// esperar un reinicio completo vale la pena. Esto es lo contrario, esta máquina
// no tiene túnel, y sostener una sala sin red veinte minutos solo consigue que
// el usuario mire una pantalla que miente.
//
// Es el RESPALDO del watchdog del supervisor, no su competidor. El watchdog
// agota sus reintentos bastante antes, y ese orden importa: si se cruzaran, la
// sala se cerraría a mitad de un reintento que iba a funcionar.
const ReconnectLimit = 10 * time.Minute

// ─── Anuncios y republicación ────────────────────────────────────────────────

// AnnounceInterval es cada cuánto el host REPITE el anuncio de sala.
//
// No es un latido nuevo y no contradice la decisión 23: es el mismo mensaje que
// ya se manda en cada cambio de juego, de nombre y de miembros, repetido. La
// conexión sigue siendo el latido para el caso normal, y esto existe para el
// caso en que la conexión miente, o sea un socket TCP medio abierto que
// sobrevive horas a una máquina apagada de golpe.
//
// Dos minutos son unos pocos cientos de bytes por miembro y por hora, y tres
// anuncios perdidos caben dentro de [HostSilenceLimit].
const AnnounceInterval = 2 * time.Minute

// RepublishInterval es cada cuánto el host vuelve a subirle la tarjeta al
// registro mientras la sala siga abierta.
//
// Publicar refresca el fijado de veintiún días que reserva el invite ID, o sea
// [RoomTTL]: este latido es lo que mantiene la sala viva y el código en manos
// de su host mientras la sala esté abierta.
//
// Una hora contra veintiún días deja un margen enorme antes de que un fallo
// importe: para que la sala se barra de verdad, el registro tiene que estar
// inalcanzable casi tres semanas. Y es tráfico despreciable contra su límite de
// treinta peticiones por minuto: una por hora está cuatro órdenes de magnitud
// debajo.
const RepublishInterval = time.Hour

// ─── Credenciales ────────────────────────────────────────────────────────────

// CredentialTTL es cuánto vale una credencial emitida.
//
// Larga a propósito. No es un control de seguridad: lo que saca a alguien de la
// sala es la revocación, que es inmediata y no cooperativa, y lo que cierra la
// puerta es renovar el código. Un vencimiento corto solo lograría echar a
// alguien a mitad de partida por haber entrado temprano.
//
// Existe igual porque una credencial sin fecha es una que sobrevive a la sala,
// y la sala deja de existir cuando se va el último nodo.
const CredentialTTL = 24 * time.Hour

// RenewInterval es cada cuánto el host empuja el vencimiento de las
// credenciales de quienes están dentro.
//
// # Qué arregla, que hasta ahora no lo arreglaba nadie
//
// Nada renovaba, así que a las [CredentialTTL] exactas de haber entrado, el
// motor del host sacaba a cada invitado de su lista de confianza y le cerraba la
// sesión. Uno por uno, sin que nadie hubiera tocado nada. Mientras una sala
// duraba una tarde no se veía; desde que la sala sobrevive al apagado y dura
// semanas, es seguro.
//
// Una hora contra veinticuatro da VEINTICUATRO intentos antes de que un fallo
// importe, que es el mismo criterio que [RepublishInterval] contra la ventana
// del registro. Para que alguien se caiga de verdad, el motor tiene que estar
// sin contestar casi un día entero, y a esa altura hace rato que no hay sala.
//
// El plazo se cuenta desde AHORA y no desde el vencimiento anterior, así que
// renovar no acumula: una credencial renovada cada hora vale siempre
// [CredentialTTL] desde la última renovación, nunca más.
const RenewInterval = time.Hour

// ArrivalGrace es cuánto se le da a quien acaba de recibir credencial para
// aparecer en la sala antes de que su ausencia signifique algo.
//
// Existe porque hay una ventana en la que alguien tiene credencial y NO está en
// la tabla de miembros, y no se ha ido: su ingreso está en curso. Entre recibir
// la credencial y aparecer hay una comprobación de subred, una salida del
// vestíbulo, el arranque de la red real, la espera a que el adaptador tome la
// dirección y el marcado del canal de control. Los plazos propios de ese camino
// suman alrededor de minuto y medio, y diez minutos son casi siete veces eso.
//
// Lo único que decide es a quién renueva el latido. Pasarse por arriba cuesta
// renovar de más a alguien que ya se fue, y su credencial vence igual en la
// vuelta siguiente. Quedarse corto cuesta dejar sin renovar a alguien que estaba
// entrando, que es el lado caro.
//
// # Por qué NO se usa para devolverle la dirección a quien vuelve
//
// Porque la única forma de reconocerlo sería el apodo, y el apodo no autentica
// nada: lo elige quien pide. El pedido de credencial lleva además una llave
// EFÍMERA, generada al pedir y descartada al salir, así que este producto no
// tiene con qué reconocer a nadie entre dos ingresos, y eso es deliberado: es lo
// mismo que hace que expulsar no sea banear.
//
// Reusar la dirección exige revocar la credencial anterior, porque si no quedan
// dos vivas para la misma dirección. Con el apodo como llave, eso le da a
// CUALQUIERA con el código la capacidad de tumbarle la credencial a un miembro
// real con solo ponerse su nombre, y le basta con que el host no lo esté viendo
// en ese instante.
//
// El precio de no hacerlo es cosmético: quien sale y vuelve recibe otra
// dirección mientras su reserva anterior siga viva. No se acumula, porque el
// latido solo renueva a los presentes y la reserva de un ausente vence sola.
const ArrivalGrace = 10 * time.Minute

// ─── Expulsar, y avisarle a quien perdió su credencial ───────────────────────

// KickGrace es cuánto se ignora a un expulsado que el motor todavía reporta.
//
// La revocación tarda alrededor de un segundo, medido. Treinta segundos son
// treinta veces eso, que es margen de sobra para una máquina cargada, y siguen
// siendo poco para el otro caso que cubre esta ventana: que el expulsado vuelva
// a entrar con un código que el host no renovó, cosa que es legítima y tiene
// que funcionar. Pasada la ventana, si está, está.
const KickGrace = 30 * time.Second

// StaleNoticeCooldown es cada cuánto se le repite a un miembro que el host no
// tiene su credencial, cuando el aviso SÍ salió.
//
// Hace falta porque el disparador es un cambio de miembros, y el motor los emite
// en ráfagas: un invitado sin credencial produce además un parpadeo de un
// segundo, entrando y saliendo de la tabla. Sin freno, eso serían decenas de
// avisos por minuto a la misma máquina.
//
// Treinta segundos son de sobra: reingresar tarda unos diez, así que un solo
// aviso suele bastar y el siguiente solo llega si el primero no sirvió de nada.
const StaleNoticeCooldown = 30 * time.Second

// StaleNoticeRetry es cada cuánto se reintenta cuando el aviso NO salió.
//
// # Por qué no vale el enfriamiento largo, medido
//
// El 2026-08-11 el host volvió a las 20:21:23. El motor le puso a los dos
// invitados en la tabla a las 20:21:22.365, y ahí mismo se les intentó avisar y
// los dos fallaron con "no hay canal abierto". Es lo ESPERABLE y no un
// accidente: el host acaba de arrancar, su servidor de control lleva
// milisegundos escuchando y los invitados todavía no lo redialaron. Uno de ellos
// reconectó trece segundos después.
//
// O sea que el primer intento del caso que esto existe para arreglar falla
// SIEMPRE, y sin un reintento corto el aviso no llega nunca. Aquella medición
// dio recuperaciones de 1m51s y 1m56s, que fue exactamente lo que tardó el
// invitado en darse cuenta solo: el aviso no aportó nada.
//
// Dos segundos alcanzan porque lo que se está esperando es que el otro lado
// redial, y el coste de errar es una línea de log.
const StaleNoticeRetry = 2 * time.Second

// ─── Volver a pedir credencial, estando en la sala ───────────────────────────

// RejoinAfter es cuánto tiene que llevar ausente el host antes del PRIMER
// intento de canje.
//
// # Sin esto, esto rompe más de lo que arregla
//
// La presencia del host se apaga en cuanto se cae el socket de control, y ese
// socket se vuelve a marcar solo, con una escalera que empieza en UN segundo.
// O sea que un corte momentáneo de red apaga la presencia y la enciende un
// segundo después, sin que nada estuviera mal.
//
// El reingreso, en cambio, REEMPLAZA la instancia de sala del motor. Disparado
// en ese primer segundo, tiraría un túnel que funcionaba para rehacerlo igual,
// con dirección nueva, cada vez que la WiFi parpadea. La cura sería peor.
//
// Dos minutos separan las dos cosas sin ambigüedad. El remarcado ordinario, si
// va a funcionar, funciona en treinta segundos como mucho, que es donde se
// aplana su escalera. Y si el host reinició y ya no reconoce la llave de nadie,
// no va a funcionar nunca, porque sin confianza no hay ruta sobre la que marcar:
// ahí lo único que arregla es volver a pedir credencial.
//
// Quedan dieciocho de los veinte minutos de [HostAbsenceLimit], o sea entre
// veinte y treinta intentos. De sobra.
const RejoinAfter = 2 * time.Minute

// RejoinInterval y RejoinJitter son cada cuánto se reintenta el canje.
//
// # Por qué éste SÍ lleva jitter y las escaleras locales del proyecto no
//
// La del watchdog dice textual "sin jitter: hay una máquina reiniciando un hijo
// local, no hay manada que dispersar", y para aquello es cierto. Acá es al
// revés: hay tantos invitados como miembros tenga la sala, todos empezaron a
// reintentar por el MISMO evento, que es el host desapareciendo, y todos van a
// acertar en el mismo instante, que es el host volviendo.
//
// Sin dispersar, el host recibe todos los canjes a la vez. Emitir una credencial
// toma el candado de la sesión durante un viaje de ida y vuelta al motor, así
// que se serializan de todas formas, y lo único que se gana apretándolos es
// dejar al daemon sin atender nada más mientras tanto.
//
// Treinta más hasta treinta reparten los intentos entre medio minuto y uno, o
// sea entre veinte y cuarenta oportunidades dentro de [HostAbsenceLimit]. De
// sobra para no necesitar escalera creciente.
const (
	RejoinInterval = 30 * time.Second
	RejoinJitter   = 30 * time.Second
)

// ─── Volver a la sala, NO estando en ninguna ─────────────────────────────────

// ReturnInterval is how often a guest tries to get back into the last room it
// was in.
//
// **There is no attempt cap.** What ends this is the room ceasing to exist, or a
// person saying so. The ladder this replaced ran four rungs over seven and a
// half minutes and then gave up, justified by "the button to go back is on the
// home screen" — which is not true of a headless host, and is a poor answer even
// with a screen: a friend's server that comes back at noon should find everybody
// in it, not everybody waiting to be told.
const ReturnInterval = 5 * time.Minute

// ReturnJitter spreads out everybody who was in the same room.
//
// A host going down takes its whole room with it, so without this they all come
// back at the registry on the same second, forever. Same reason [RejoinJitter]
// exists.
const ReturnJitter = 30 * time.Second

// ─── El canario ──────────────────────────────────────────────────────────────

// CanaryRoundDeadline es lo que dura una ronda como mucho.
//
// Diez segundos porque un invitado tarda hasta seis en contestar, que son los
// dos sondeos de [ProbeDeadline], y hace falta margen para el viaje de ida y
// vuelta por el canal de la sala. Una ronda real medida contra el droplet tardó
// 3376 ms con el ida y vuelta de ssh incluido.
const CanaryRoundDeadline = 10 * time.Second

// CanaryTTL es lo que el socket puede quedar abierto, y va POR ENCIMA de
// [CanaryRoundDeadline] a propósito.
//
// El tope del adaptador es la red de seguridad para el caso de que este proceso
// se muera a mitad de una ronda, jamás la espera normal. Si fuera menor que el
// plazo de ronda, cerraría canarios vivos en el camino bueno.
const CanaryTTL = 12 * time.Second

// CanaryTTLMax es lo máximo que el canario puede quedar abierto, mirado desde
// el adaptador.
//
// Existe como tope duro y no como sugerencia: el oyente lo abre el daemon, que
// corre como SYSTEM, y un plazo que dependa de que alguien llame a Close deja el
// socket vivo cuando quien tenía que cerrarlo se murió. Con esto, el peor caso
// es medio minuto.
const CanaryTTLMax = 30 * time.Second

// ─── El sondeo de puertos ────────────────────────────────────────────────────

// ProbeDeadline es cuánto se espera a cada puerto.
//
// Tres segundos porque el silencio es el resultado más común y es el que hay que
// esperar entero: el RTT medido por el túnel a otra máquina anda en las décimas
// de segundo, así que esto es más de diez veces el camino bueno. Bajarlo
// convertiría un peer lento en un falso "cerrado".
const ProbeDeadline = 3 * time.Second

// ─── El supervisor ───────────────────────────────────────────────────────────

// SupervisorBeat es cada cuánto se evalúan los vencimientos.
//
// Quince segundos es la granularidad de los treinta de [KickGrace], y son
// veinticuatro muestras dentro del plazo más corto que se vigila, los seis
// minutos de [HostSilenceLimit].
const SupervisorBeat = 15 * time.Second

// SupervisorSweep es cada cuánto corre el módulo de exposición.
//
// Va aparte del latido porque hace siempre tres llamadas al sistema, una al IGD
// del router, que en la mayoría de los routers termina en timeout. Un latido
// para el tiempo, un barrido para el mundo.
const SupervisorSweep = 60 * time.Second

// MeshBeat es cada cuánto se le pregunta al motor quién hay en la malla.
//
// Un segundo: lo que se persigue son los veintiún segundos que un invitado pasa
// marcando al host, y con [SupervisorBeat] eso serían una o dos muestras. Cuesta
// un mensaje por la tubería del motor, que es barato.
const MeshBeat = 1 * time.Second

// AdapterReapplyEvery es cada cuántos [SupervisorBeat] se reaplican los ajustes
// del adaptador aunque Windows no haya avisado nada.
//
// Es el respaldo de la suscripción al Event ID 10000. Ocho latidos son dos
// minutos, y sin esto una suscripción muerta se traduce exactamente en "ayer
// funcionaba".
//
// Se cuenta en latidos y no en segundos, que es la única razón por la que un
// número pelado vive en un paquete de relojes.
const AdapterReapplyEvery = 8

// ─── El registro del seed, visto desde el cliente ────────────────────────────

// RegistryRequestTimeout y RegistryDialTimeout son el presupuesto de una llamada
// al registro, y son dos números por un motivo.
//
// El de marcado va muy dentro del de petición por dónde corren estas llamadas:
// publicar y abrir se llaman con el candado de la sesión tomado, y pintar la
// pantalla toma ese mismo candado. Un seed que se quedó callado es el caso
// MEDIDO en Windows, que no rebota, no dice nada. Sin el marcado más corto, la
// interfaz se congelaría los diez segundos enteros esperando a una máquina que
// no va a contestar nunca.
const (
	RegistryRequestTimeout = 10 * time.Second
	RegistryDialTimeout    = 4 * time.Second
)

// SeedCheckTimeout es cuánto se espera a que el registro conteste antes de
// negarse a guardarlo como el registro propio.
//
// Más corto que [PipeDefaultTimeout] a propósito: si esto lo agotara, el cliente
// se rendiría primero y el fallo llegaría como "el daemon no contestó", que
// manda a sospechar del daemon en vez del nombre que se acaba de escribir.
const SeedCheckTimeout = 6 * time.Second

// SeedReachWait es cuánto se espera a que el seed acepte la conexión, desde las
// herramientas de medición.
//
// Cinco segundos: el seed está en un droplet al otro lado de internet, así que
// un segundo daría falsos negativos desde una conexión lenta, y quince
// convertirían la comprobación en una espera que nadie termina de mirar.
const SeedReachWait = 5 * time.Second

// ─── El registro del seed, visto desde adentro ───────────────────────────────

// RoomTTL is how long a room outlives silence from its host.
//
// # It used to be two windows, and one of them was measuring the wrong thing
//
// There was a `CardTTL` of six hours next to this, and `lookup` refused to
// resolve a room whose card had aged past it. The name said the card's life and
// the effect was the room's: a host whose VPS spent the night down came back to
// a code that had been answering "no such room" to everybody holding it, with
// three weeks of pin still to go and the room perfectly reopenable.
//
// It is gone. **A room stops existing for exactly two reasons**, and neither is
// a clock the host cannot see: somebody closed it, or nobody republished it for
// this long and the sweep took it. Everything else — the tunnel, the NAT hole,
// the engine, the host's whole machine — is disposable, and coming back from any
// of it is the product working.
//
// A live room never gets near this: it republishes every [RepublishInterval],
// which is what pushes the deadline forward.
//
// # What the pin buys, and why it is the same number
//
// The entry survives so the invite ID keeps BELONGING to the key that first
// claimed it. Without that, an ex member who kept the code would race the host
// on reopening and win by being more attentive. So "how long the room lives" and
// "how long the code stays reserved" stopped being two questions the day the
// card stopped gating resolution, and one constant answers both.
const RoomTTL = 21 * 24 * time.Hour

// AccessTTL y RefreshTTL son la vida de los dos tokens de un seed cerrado.
//
// El de acceso es corto porque viaja en cada mutación y no gana nada viviendo
// más: renovar cuesta un viaje de ida y vuelta contra un seed con el que el
// cliente ya está hablando.
//
// El de refresco es largo y NO desliza. Renovar emite un acceso nuevo y devuelve
// el mismo refresco, así que la sesión tiene un techo duro en vez de uno que un
// token robado pueda estirar para siempre. Treinta días después la persona
// escribe el password otra vez, que es además el único momento en que la
// interfaz tiene que preguntar.
const (
	AccessTTL  = 15 * time.Minute
	RefreshTTL = 30 * 24 * time.Hour
)

// ThrottleStep y MaxThrottle son el freno progresivo de los intentos fallidos
// contra la puerta del seed.
const (
	ThrottleStep = 100 * time.Millisecond
	MaxThrottle  = 2 * time.Second
)

// RoomCloseSkew is how far off the signer's clock is allowed to be when it asks
// the registry to close a room.
//
// Five minutes in both directions. There is nothing to synchronise between the
// two machines and a desktop clock drifts by seconds without anybody noticing,
// so a narrow margin would turn closing into something that fails depending on
// which PC asks. Wider buys nothing: what this bounds is how long a recorded
// copy is worth anything, and five minutes is nothing next to the three weeks a
// room lives.
const RoomCloseSkew = 5 * time.Minute

// ReleaseCheck es cada cuánto el seed le vuelve a preguntar a GitHub por el
// release.
//
// Un día. Publicar una versión pasa unas pocas veces al mes, así que preguntar
// veinticuatro veces diarias es gastar cuota para enterarse antes de algo que no
// cambia. Era una hora, y su único consumidor ahora es la página: el cliente
// dejó de preguntarle al registro por la versión, y `/api/version` se borró al
// quedarse sin llamadores.
//
// La espera no se nota al publicar: `kanpseed upgrade` reinicia el servicio, y
// un proceso nuevo arranca sin nada cacheado.
const ReleaseCheck = 24 * time.Hour

// ReleaseCheckAfterFailure es cuándo se reintenta cuando GitHub no contestó.
//
// # Por qué hace falta un plazo aparte
//
// Porque el refresco toca el sello de tiempo también cuando falla, a propósito:
// sin eso, un GitHub caído dispararía una consulta nueva con cada visita a la
// página. Con [ReleaseCheck] en un día, un fallo costaría un día de desfase, o
// sea que una caída de cinco minutos dejaría la página sin versión hasta mañana.
const ReleaseCheckAfterFailure = 10 * time.Minute

// ReleaseTimeout acota la consulta. GitHub colgado no puede colgar la página.
const ReleaseTimeout = 5 * time.Second

// ─── El canal de control de la sala ──────────────────────────────────────────

// NoticeAckWait es cuánto se espera el acuse de un aviso antes de devolver.
//
// Acotado y no indefinido, y el tope es lo importante: esperar sin límite
// convertiría la expulsión en cooperativa, que es justo lo que la decisión 22
// evita. Vencido el plazo se devuelve igual y lo único que se pierde es que el
// otro se entere.
//
// Sin ninguna espera queda una ventana real: mandar devuelve cuando los bytes
// entraron al búfer local, no cuando llegaron, y un segundo después la
// revocación mata la conexión con lo que quedara sin salir.
const NoticeAckWait = 1 * time.Second

// ControlWriteWait es cuánto se espera a que el otro lado reciba un mensaje.
//
// Existe por la misma razón que [NoticeAckWait] y protege lo mismo: escribir en
// un socket bloquea mientras el otro no recibe, y quien manda es el host con el
// candado de la sesión tomado. Sin plazo, un miembro que abre la conexión y deja
// de leer traba la sesión entera sin mandar un solo mensaje inválido.
const ControlWriteWait = 2 * time.Second

// CredentialWait es cuánto espera el que entra a que el host le conteste.
//
// Con tope por lo mismo que [NoticeAckWait]: sin él, alguien que acepte la
// conexión y no conteste nunca deja el ingreso colgado para siempre, y del lado
// del usuario eso es una pantalla que no dice nada.
const CredentialWait = 10 * time.Second

// DoorHelloWait es cuánto tiene una conexión de la puerta para decir a qué vino.
//
// Una que llega y calla no es un cliente lento, es una conexión ocupando un
// hueco de los dieciséis.
const DoorHelloWait = 5 * time.Second

// InitialRoomDialWait e InitialRoomRetryWait acotan cada intento de la conexión
// INICIAL y dejan que el relay termine su handshake antes del siguiente.
//
// La reconexión de una sesión ya establecida usa su propia escalera.
const (
	InitialRoomDialWait  = 5 * time.Second
	InitialRoomRetryWait = 2 * time.Second
)

// ─── El pipe local, entre el daemon y sus caras ──────────────────────────────

// PipeHelloWait es lo que se espera al saludo antes de cortar. Quien se conecta
// y no saluda está ocupando una plaza de las ocho sin decir quién es.
const PipeHelloWait = 5 * time.Second

// PipeIdleWait es lo que aguanta una conversación sin que llegue nada. La
// interfaz pregunta el estado seguido, así que diez minutos de silencio es una
// interfaz que murió sin cerrar el socket.
const PipeIdleWait = 10 * time.Minute

// PipeWriteWait es por MENSAJE y no por escritura, igual que
// [ControlWriteWait]. Un cliente que deja de leer no puede congelar al daemon.
const PipeWriteWait = 5 * time.Second

// PipeDefaultTimeout es cuánto espera el CLIENTE de Go una respuesta.
//
// **La ventana tiene su gemelo y no se puede compartir**, porque es otro
// lenguaje: `kDefaultTimeout` y su tabla `kMethodTimeouts`, en
// `ui/lib/core/timing/app_timing.dart`. Es el mismo pipe y la misma paciencia,
// escrita dos veces. Si se toca una, se toca la otra.
//
// Noventa segundos, y el número no es prudencia genérica: el techo real lo pone
// el motor, con [AddressDeadline] por adaptador, y crear una sala levanta DOS
// (la sala y el vestíbulo) más el sondeo del MTU. Con el plazo de diez segundos
// que tenía `pipeprobe`, `create_room` cortaba del lado del cliente con el
// daemon trabajando bien, y el síntoma era un `i/o timeout` con la sala ya
// creada.
const PipeDefaultTimeout = 90 * time.Second

// PipeDialTimeout es cuánto se espera a que el canal ACEPTE.
//
// Mucho más corto que [PipeDefaultTimeout] y por otro motivo: conectarse a un
// canal local es inmediato o no va a pasar. Esperar más solo alarga el rato en
// que el CLI parece colgado cuando lo que hay es un daemon que no arrancó.
const PipeDialTimeout = 5 * time.Second

// SocketProbeTimeout es lo que se espera a saber si el socket de al lado está
// vivo, antes de decidir si se borra.
//
// Generoso a propósito. Lo que se decide con esta respuesta es si se BORRA el
// socket, y equivocarse por impaciencia significaría echar a un daemon que está
// corriendo. Un `connect` local contesta en microsegundos, así que dos segundos
// solo se agotan cuando pasa algo raro, y ahí lo correcto es no tocar nada.
const SocketProbeTimeout = 2 * time.Second

// ─── El motor ────────────────────────────────────────────────────────────────

// EngineCallTimeout es lo que se espera una respuesta del motor antes de darla
// por perdida.
//
// Existe porque el motor podría no contestar nunca, y sin plazo el llamador se
// queda colgado con la sala a medio abrir. Es holgado a propósito: hospedar crea
// un adaptador virtual, que en una máquina cargada tarda segundos.
const EngineCallTimeout = 45 * time.Second

// AddressDeadline is how long a start command waits for the virtual address.
//
// Generous on purpose. Creating a wintun adapter, naming it and configuring an
// address is several privileged operations, and on a cold machine the driver
// gets loaded on the way. Failing early here would report a broken network on a
// machine that was merely slow.
const AddressDeadline = 30 * time.Second

// AddressPoll is how often the interface is re-read while waiting.
const AddressPoll = 250 * time.Millisecond

// ─── Arranque, apagado y relanzamiento ───────────────────────────────────────

// ShutdownGrace es cuánto se le da al cierre ordenado antes de rendirse.
//
// El Administrador de servicios de Windows da treinta segundos antes de matar el
// proceso. Rendirse a los veinte deja diez para anotar por qué, que es la
// diferencia entre un log que explica y un proceso que desapareció.
//
// Lo usan el daemon y `roomprobe`, que corre la misma sala fuera del servicio:
// cerrar una sala es avisar a los miembros, escribir en el firewall por COM y
// bajar dos redes virtuales, y ninguna de las tres es instantánea. Estuvo
// escrito dos veces, con un comentario en uno de los dos diciendo que tenía que
// valer lo mismo que el otro.
const ShutdownGrace = 20 * time.Second

// CleanupGrace es cuánto se le da a la limpieza de una operación abortada.
//
// Es un tope y no una espera: lo normal es que cierre en milisegundos. Existe
// para que un adaptador colgado no deje el candado de la sesión tomado para
// siempre, que sería una app que no puede volver a intentar nada.
const CleanupGrace = 30 * time.Second

// UIQuickDeath es qué se considera "la interfaz se cayó enseguida".
//
// Una que vivió más que esto y se cerró es un caso normal, así que el contador
// de caídas seguidas se reinicia y se vuelven a tener las tres oportunidades.
const UIQuickDeath = 60 * time.Second

// UIRelaunchGrace es lo que se espera antes de volver a lanzar la interfaz.
//
// Corto: quien mira la bandeja la quiere de vuelta. Suficiente para que la que
// acaba de morir suelte lo suyo, empezando por el evento con nombre de la
// instancia única, que es lo que la nueva se toparía consigo misma.
const UIRelaunchGrace = 1500 * time.Millisecond

// LauncherProbeWait es lo que el lanzador espera a la sonda del pipe.
//
// Corto a propósito: alguien acaba de hacer doble clic y está mirando. Si el
// daemon está vivo, contesta en milisegundos; si no está, el fallo es inmediato.
// Lo único que este plazo cubre es una máquina con el disco ocupado.
const LauncherProbeWait = 750 * time.Millisecond

// LauncherCallWait es lo que el lanzador espera a la respuesta de un método.
const LauncherCallWait = 5 * time.Second

// LauncherReadyWait y LauncherReadyRetry son cuánto se insiste con el pipe
// después de mandar arrancar el servicio.
//
// Un servicio recién arrancado tarda: purga el firewall y levanta el motor antes
// de abrir el pipe, y hasta que no lo abre no hay a quién hablarle. Este plazo
// solo se paga en el camino de "estaba arrancando cuando llegué", que es una
// carrera rara, no el caso normal.
const (
	LauncherReadyWait  = 20 * time.Second
	LauncherReadyRetry = 400 * time.Millisecond
)

// ─── El router y el diagnóstico ──────────────────────────────────────────────

// Los plazos del IGD. Salen de que esto alimenta una pantalla: el usuario ya
// pulsó algo y está esperando.
const (
	// IGDSearchWait es cuánto se escuchan respuestas al multicast. Los routers
	// contestan en decenas de milisegundos; el resto del tiempo es margen para
	// uno lento, no para esperar a uno que no está.
	IGDSearchWait = 2 * time.Second
	// IGDHTTPTimeout cubre la descripción del dispositivo y cada llamada SOAP.
	IGDHTTPTimeout = 3 * time.Second
	// IGDWholeBudget es el techo de todo junto, por si la suma de plazos chicos
	// se va de las manos con un router que contesta lento a cada paso.
	IGDWholeBudget = 8 * time.Second
)

// PreflightProbeTimeout acota cada comprobación del arranque que sale del
// proceso.
const PreflightProbeTimeout = 5 * time.Second

// ─── Las caras y las herramientas ────────────────────────────────────────────

// LiveViewRefresh es cada cuánto se redibuja una vista en vivo de terminal.
//
// Un segundo y no menos: lo que se mira ahí son plazos de minutos, y redibujar
// más rápido solo consigue que la consola parpadee. Va muy por debajo de
// [SupervisorBeat] a propósito: la vista enseña el estado ya cambiado, no
// compite con quien lo cambia.
//
// Lo usan `kanpachi watch` y el menú de `roomprobe`, que estuvieron escritos por
// separado con el mismo número y casi el mismo párrafo.
const LiveViewRefresh = 1 * time.Second

// UpgradeTimeout es el plazo total de un `upgrade`. Generoso porque incluye
// bajar decenas de MB por la red de un servidor cualquiera.
const UpgradeTimeout = 5 * time.Minute

// ─── Las dos escaleras ───────────────────────────────────────────────────────

// Son FUNCIONES y no variables, y no es capricho.
//
// Go no tiene arrays constantes, así que la alternativa era un `var` a nivel de
// paquete. Un slice exportado en un paquete que importa medio repositorio lo
// puede reasignar cualquier `init`, y una escalera de reintentos puesta en un
// siglo es exactamente la forma de apagar un watchdog sin tocar su lógica: la
// misma propiedad que los guardianes de `internal/arch` protegen para los
// plazos. Devolviéndolas, los números quedan en un solo sitio y nadie los mueve.
//
// Devuelven un ARRAY y no un slice, que además es lo que hace falta: un array es
// un valor, así que quien lo recibe se lleva una copia, y `len` sobre él sigue
// siendo una expresión constante. De eso depende que `MaxRestarts` en el
// supervisor pueda seguir siendo `const`.
//
// Ninguna de las dos lleva jitter, y las dos lo dicen abajo por separado: son
// máquinas reintentando contra UNA sola, no una manada. Lo contrario son
// [RejoinJitter] y [ReturnJitter].

// EngineRestartBackoff es la escalera del watchdog del motor.
//
// Arranca corto porque el caso común es que el motor se muera por un adaptador
// que Windows quitó y vuelve en segundos, y se aplana en un minuto porque más
// allá de ahí reintentar no es lo que arregla nada.
//
// Sin jitter: hay una máquina reiniciando un hijo local, no hay manada que
// dispersar.
//
// La suma son 198 segundos, tres minutos y dieciocho, que entra holgada dentro
// de [ReconnectLimit]. Ese orden importa y no es casualidad: el plazo de core es
// el RESPALDO de este watchdog, no su competidor, y si se cruzaran la sala se
// cerraría a mitad de un reintento que iba a funcionar.
func EngineRestartBackoff() [8]time.Duration {
	return [8]time.Duration{
		1 * time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second,
		20 * time.Second, 40 * time.Second, 60 * time.Second, 60 * time.Second,
	}
}

// ControlReconnectBackoff es la escalera con la que el invitado vuelve a marcar
// el canal de control.
//
// No es un plazo de corte y no alarga nada: mientras reintenta, [HostAbsenceLimit]
// corre igual, porque lo que ese contador mide es la ausencia y no los intentos.
// Arranca corto porque la causa común es un enlace del motor que se rehace en
// segundos, y se aplana en medio minuto porque más rápido no arregla nada y
// llena el log.
//
// Que empiece en UN segundo es lo que obliga a que exista [RejoinAfter]: sin
// esperar, un parpadeo de la WiFi apagaría la presencia del host y dispararía un
// reingreso que tira un túnel que funcionaba.
func ControlReconnectBackoff() [6]time.Duration {
	return [6]time.Duration{
		1 * time.Second,
		2 * time.Second,
		5 * time.Second,
		10 * time.Second,
		20 * time.Second,
		30 * time.Second,
	}
}
