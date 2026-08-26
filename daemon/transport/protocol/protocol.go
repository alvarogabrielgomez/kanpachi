// Package protocol es la API local de Kanpachi, definida APARTE de su
// transporte.
//
// Los mensajes son JSON-RPC delimitado por líneas. El named pipe de Windows es
// una implementación de este contrato, no el contrato: la separación cuesta
// cero hoy y es lo único que hace falta para que el host headless en Linux de
// 07-futuro.md reuse la misma API sobre un socket Unix, con los mismos casos de
// uso.
//
// Por eso este paquete no conoce Windows y corre en el job de Linux de CI.
//
// # La superficie es la mitigación principal
//
// La frontera de seguridad honesta acá es la sesión del usuario, igual que en
// cualquier aplicación de escritorio: un proceso malicioso corriendo como el
// usuario puede hablarle a este pipe. Lo que lo acota no es la autenticación,
// es **lo que la API puede pedir**.
//
// La lista de métodos es CERRADA y solo puede aplicar perfiles del catálogo. No
// existe la operación "abrir un puerto arbitrario", no existe "ejecutar", no
// existe "leer un archivo". Lo peor que consigue un proceso malicioso es unirse
// a una sala y activar el perfil de un juego que ya está en el catálogo, jamás
// abrir 445 ni nada fuera de él.
package protocol

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/core/port"
	"github.com/accentiostudios/kanpachi/core/usecase"
)

// Method es el conjunto CERRADO de operaciones de la API local.
//
// Cerrado a propósito y comprobado contra una tabla, no despachado por
// reflexión: un método que no está en la tabla se rechaza sin interpretarse, y
// agregar uno es una edición visible en un solo archivo.
type Method string

const (
	// MethodHello es el saludo con el token. Tiene que ser el PRIMER mensaje de
	// cada conexión, y hasta que se conteste no se admite ningún otro.
	MethodHello Method = "hello"

	MethodCreateRoom       Method = "create_room"
	MethodJoinRoom         Method = "join_room"
	MethodLeaveRoom        Method = "leave_room"
	MethodActivateProfile  Method = "activate_profile"
	MethodKickMember       Method = "kick_member"
	MethodRotateInviteCode Method = "rotate_invite_code"
	MethodRenameRoom       Method = "rename_room"
	MethodInviteLink       Method = "invite_link"
	MethodStatus           Method = "status"

	MethodListGames     Method = "list_games"
	MethodRejectedGames Method = "rejected_games"
	MethodSaveProfile   Method = "save_profile"
	MethodImportCatalog Method = "import_catalog"
	MethodExportCatalog Method = "export_catalog"
	MethodMarkVerified  Method = "mark_verified"

	MethodForeignRules        Method = "foreign_rules_for"
	MethodSuspendForeignRules Method = "suspend_foreign_rules"
	MethodDiagReport          Method = "diag_report"
	MethodObserveGame         Method = "observe_game"
	MethodExposure            Method = "exposure"
	MethodProbeHost           Method = "probe_host"
	// MethodReapplyProtection repone la Protección Kanpachi. Es IDEMPOTENTE:
	// el firewall calcula la diferencia contra las reglas vivas, así que
	// pulsarlo con nada roto no lo toca.
	MethodReapplyProtection Method = "reapply_protection"

	// MethodQuarantine es el interruptor de la cuarentena de base, y también
	// su lectura: con `set` en "on" u "off" ES la decisión del usuario, hecha
	// verdad en la dirección que diga, y sin `set` no toca nada. En los dos
	// casos devuelve el estado ENTERO, porque lo que la pantalla dibuja
	// después es el interruptor con la medición fresca, no un acuse.
	//
	// Idempotente en las dos direcciones: encender con todo puesto repara lo
	// que falte, apagar sin nada puesto es la intención ya cumplida.
	MethodQuarantine Method = "quarantine"

	// MethodSavedRoom, MethodResumeRoom y MethodDiscardSavedRoom son la sala
	// que ESTA máquina hospeda, tal como quedó en disco.
	//
	// **Los dos nombres de cable con `pending` dentro están congelados**, y no
	// dicen ya lo que decían: la sala se reabre sola en cada arranque, así que
	// no hay nada pendiente de que alguien decida. Cambiar la cadena rompería a
	// toda ventana y todo script más viejos que el daemon que la sirve, que es
	// justo lo que un nombre de cable existe para evitar.
	MethodSavedRoom        Method = "pending_room"
	MethodResumeRoom       Method = "resume_room"
	MethodDiscardSavedRoom Method = "discard_pending_room"
	MethodLastRoom         Method = "last_room"

	// MethodForgetLastRoom borra esa última sala, a pedido.
	//
	// Es la cruz de la portada, y no un "esconder el aviso": el archivo sigue
	// en disco, así que una vuelta que solo se ocultara en pantalla volvería a
	// ofrecerse en el arranque siguiente. Idempotente: olvidar lo que no hay es
	// la intención ya cumplida.
	MethodForgetLastRoom Method = "forget_last_room"

	// MethodProgress son los pasos de la operación larga en curso.
	//
	// **Se pide por una conexión APARTE de la que está esperando.** El bucle de
	// una conexión es secuencial —leer, despachar, contestar—, así que pedirlo
	// por la misma se encolaría detrás de justo lo que se quiere observar. Hay
	// ocho plazas y la interfaz usa una.
	MethodProgress Method = "progress"

	// MethodCancel corta la operación larga que esté corriendo.
	//
	// **Se pide por una conexión APARTE**, por lo mismo que `progress`: el
	// bucle de una conexión es secuencial, así que mandarlo por la que está
	// esperando lo pondría en cola detrás de justo lo que viene a cortar.
	MethodCancel Method = "cancel"

	// Los tres del PROCESO, no de la sala. Los contesta [Host] y no [API]. Ver
	// el modelo de procesos en `docs/03`.

	// MethodShowUI enseña la ventana de la interfaz.
	//
	// Existe porque el acceso directo apunta al daemon: hacer doble clic con
	// Kanpachi ya corriendo tiene que abrir la ventana, que es lo que cualquiera
	// espera del icono.
	MethodShowUI Method = "show_ui"
	// MethodPendingInvite recoge el enlace `kanpachi://` que trajo el navegador,
	// ya resuelto contra el registro. Devolverlo lo CONSUME.
	//
	// Es una recogida y no un empuje porque el API local es petición y
	// respuesta pura: el daemon apila lo que le llegó y la interfaz pregunta.
	// El aviso de que hay algo que recoger es la ventana abriéndose, que es lo
	// que [MethodShowUI] hace justo después de guardarlo.
	//
	// **Consumirlo es parte del contrato.** Sin eso, el latido de la interfaz
	// volvería a enseñar la pantalla de confirmación cada dos segundos, incluso
	// después de que alguien la cancelara.
	MethodPendingInvite Method = "pending_invite"

	// MethodPreviewInvite resuelve un código PEGADO contra su registro, sin
	// entrar y sin tocar la sesión.
	//
	// Es el mismo trabajo que [MethodPendingInvite] hace con un enlace que trajo
	// el navegador, pedido a mano. Existe porque la pantalla de inicio pregunta
	// antes de entrar, y lo que tiene que enseñar ahí —la huella del host y qué
	// dice de ella la libreta— sale de esta consulta. Sin él, el aviso de la
	// decisión 25 solo aparecería para quien llega por enlace.
	//
	// No consume nada y se puede pedir las veces que haga falta.
	MethodPreviewInvite Method = "preview_invite"
	// MethodShutdown es "Salir de Kanpachi" del menú de la bandeja.
	//
	// **No lo coordina la interfaz.** La interfaz no controla nada de lo que
	// hay que apagar: manda la orden y se muere en el camino, con el job.
	MethodShutdown Method = "shutdown"
	// MethodAutostart lee o cambia si Kanpachi arranca con Windows.
	//
	// Uno solo para las dos cosas, con el valor opcional en los parámetros: son
	// la misma pregunta y la pantalla que la cambia necesita leerla justo
	// después para dibujar el interruptor.
	MethodAutostart Method = "autostart"
	// MethodOwnSeed lee o cambia el registro en el que esta máquina abre salas.
	//
	// Uno solo para las dos cosas, como [MethodAutostart] y por lo mismo: la
	// pantalla que lo cambia necesita releerlo justo después para dibujar lo que
	// quedó puesto.
	//
	// Devuelve además la SUGERENCIA, que es el registro de la última sala a la
	// que se entró. Van juntas porque la pantalla las pinta juntas, y separarlas
	// en dos llamadas dejaría un instante en el que enseña una y no la otra.
	MethodOwnSeed Method = "own_seed"

	// MethodNickname lee o cambia el nombre con el que esta máquina entra a las
	// salas.
	//
	// Es el tercero de la familia de [MethodAutostart] y [MethodOwnSeed], y por
	// el mismo motivo: uno solo para leer y para escribir, porque la pantalla
	// que lo cambia tiene que releerlo justo después para dibujar lo que quedó.
	//
	// Devuelve además la SUGERENCIA, derivada del nombre de esta máquina, y esa
	// derivación vive en un solo sitio a propósito. Cuando la tenía también el
	// CLI, la sugerencia se escribía en disco y dejaba de distinguirse de una
	// elección: por eso una máquina cuya ventana decía «Alvaro» entraba a las
	// salas como «AlvaroGDeskt».
	//
	// **Esto no cambia cómo el nombre llega a una sala.** `create_room` y
	// `join_room` lo siguen llevando como parámetro y el daemon sigue sin
	// persistir lo que llega por ahí: si lo hiciera, una ventana vieja que
	// reenvía su copia pisaría el nombre elegido desde la terminal.
	MethodNickname Method = "nickname"

	// MethodSettings lee o cambia lo que esta máquina recuerda de cómo se
	// presenta: si las caras narran paso a paso lo que hace el daemon, el
	// tamaño de la ventana, y la versión publicada que ya se sabe más nueva que
	// la que corre.
	//
	// Cuarto de la familia de [MethodAutostart], [MethodOwnSeed] y
	// [MethodNickname], con la misma forma y por el mismo motivo: uno solo para
	// leer y para escribir, porque la pantalla que lo cambia tiene que releerlo
	// justo después para dibujar lo que quedó.
	//
	// **Uno para las tres y no tres métodos**, al revés que los de la familia.
	// Cada uno de aquellos tiene una autoridad distinta detrás —el Administrador
	// de servicios, un registro que se sondea antes de guardarlo, un nombre que
	// se valida y del que se deriva una sugerencia— y estas tres solo se
	// escriben, en el mismo fichero.
	//
	// **Cada campo es opcional y ausente significa "no lo toques".** Sin eso,
	// una ventana que apaga la narración mandaría un tamaño de cero al lado y
	// borraría el que había.
	//
	// El nombre NO se escribe por acá, y por eso no viaja en la respuesta: lo
	// contesta [MethodNickname], que además da la sugerencia. Un escritor por
	// hecho.
	MethodSettings Method = "settings"

	// MethodEngineInfo dice qué motor lleva esta instalación: build id y
	// librería de red, leídos del fichero que el daemon va a lanzar. Solo
	// lectura, para la pantalla de Configuración; `kanpachi version` lee lo
	// mismo directo del disco sin pasar por acá.
	MethodEngineInfo Method = "engine_info"

	// MethodSeedPassword entrega el password del registro propio, para poder
	// HOSPEDAR en un seed cerrado.
	//
	// # Lo que no vuelve, y lo que no queda
	//
	// La respuesta es un acuse vacío. No devuelve tokens, no devuelve el estado
	// de la puerta y no repite lo que llegó: el password entra, se convierte en
	// una prueba con el host dentro y se olvida. Lo que sobrevive es un refresh
	// token sellado que el daemon guarda solo.
	//
	// Sus parámetros NO entran en el diario de progreso ni en el log ni en el
	// informe de diagnóstico. Es la misma clase de fuga que ya se cerró en el log
	// del motor, y falla igual de callada.
	//
	// Entrar a una sala jamás lo necesita, en ningún seed.
	MethodSeedPassword Method = "seed_password"
)

// métodos es la tabla. Su existencia es la que hace que la lista sea cerrada.
var métodos = map[Method]bool{
	MethodHello:               true,
	MethodCreateRoom:          true,
	MethodJoinRoom:            true,
	MethodLeaveRoom:           true,
	MethodActivateProfile:     true,
	MethodKickMember:          true,
	MethodRotateInviteCode:    true,
	MethodRenameRoom:          true,
	MethodInviteLink:          true,
	MethodStatus:              true,
	MethodListGames:           true,
	MethodRejectedGames:       true,
	MethodSaveProfile:         true,
	MethodImportCatalog:       true,
	MethodExportCatalog:       true,
	MethodMarkVerified:        true,
	MethodForeignRules:        true,
	MethodSuspendForeignRules: true,
	MethodDiagReport:          true,
	MethodObserveGame:         true,
	MethodExposure:            true,
	MethodProbeHost:           true,
	MethodReapplyProtection:   true,
	MethodQuarantine:          true,
	MethodSavedRoom:           true,
	MethodResumeRoom:          true,
	MethodDiscardSavedRoom:    true,
	MethodLastRoom:            true,
	MethodForgetLastRoom:      true,
	MethodProgress:            true,
	MethodCancel:              true,
	MethodShowUI:              true,
	MethodPendingInvite:       true,
	MethodPreviewInvite:       true,
	MethodShutdown:            true,
	MethodAutostart:           true,
	MethodOwnSeed:             true,
	MethodNickname:            true,
	MethodSettings:            true,
	MethodEngineInfo:          true,
	MethodSeedPassword:        true,
}

// Known dice si el método existe. Lo que no está no se interpreta.
func (m Method) Known() bool { return métodos[m] }

// Code es el conjunto cerrado de errores que la API devuelve.
//
// Códigos y no texto suelto porque la UI tiene que poder decidir qué pantalla
// mostrar sin leer castellano. El texto viaja igual, para el log y para el
// diagnóstico que el usuario copia al portapapeles.
type Code string

const (
	// CodeBadRequest es que el mensaje no se pudo interpretar: método
	// desconocido, parámetros con campos de más, JSON roto.
	CodeBadRequest Code = "bad_request"
	// CodeUnauthorized es hablar antes de saludar, o saludar con un token que
	// no es.
	CodeUnauthorized Code = "unauthorized"
	// CodeTooLarge es un mensaje que pasa del tope. Se corta la conexión.
	CodeTooLarge Code = "too_large"

	// CodeFirewallBlocks es que un firewall AJENO (ufw, firewalld) deniega la
	// entrada de los adaptadores de la sala y nadie consintió abrirlo.
	//
	// Código propio y no `busy` ni `unavailable`, por el criterio de siempre:
	// lo que la persona hace después es distinto. Acá relanza con
	// `allow_firewall` tras leer los comandos exactos, que viajan en el
	// mensaje. Ver [usecase.ErrFirewallBlocks].
	//
	// Va ARRIBA de la tabla de una línea y no dentro: un comentario en medio
	// parte el bloque de alineación de gofmt en dos, y la tabla deja de leerse
	// como tabla.
	CodeFirewallBlocks Code = "firewall_blocks"

	// CodeQuarantineUndecided es que abrir o entrar exige contestar primero la
	// pregunta de la cuarentena de base, y nadie la contestó todavía.
	//
	// Solo sale cuando el LLAMADOR pidió preguntar (`quarantine: "ask"`). La
	// ventana no lo pide y no se bloquea nunca; el CLI lo pide siempre, que es
	// la asimetría deliberada entre caras: en la terminal la pregunta es el
	// modo de interactuar, y en la ventana una modal antes de jugar es lo que
	// el producto prometió no hacer. El mensaje enumera los puertos exactos; el
	// CLI lo muestra, pregunta sin valor por defecto, y reintenta con `on` u
	// `off`.
	CodeQuarantineUndecided Code = "quarantine_undecided"

	CodeBusy        Code = "busy"          // ya hay sala
	CodeNoRoom      Code = "no_room"       // la operación necesita una y no hay
	CodeNotHost     Code = "not_host"      // solo el host puede
	CodeUnknownGame Code = "unknown_game"  // ese juego no está en el catálogo
	CodeNotAMember  Code = "not_a_member"  // esa dirección no es de nadie presente
	CodeSelfKick    Code = "self_kick"     // expulsarse a uno mismo
	CodeShadows     Code = "shadows"       // el perfil taparía uno que vino con la app
	CodeNotPlayed   Code = "not_played"    // marcar verificado algo que no se jugó
	CodeKickPartial Code = "kick_partial"  // la expulsión se aplicó a medias
	CodeProbeSelf   Code = "probe_self"    // el host no puede sondearse a sí mismo
	CodeProbeNoHost Code = "probe_no_host" // no se sabe dónde está el host
	CodeNoSavedRoom Code = "no_pending"    // no hay ninguna sala guardada en disco
	CodeNoSuchRoom  Code = "no_such_room"  // el registro dice que ese código no existe
	CodeNoRegistry  Code = "no_registry"   // el registro no contestó nada
	// CodeSeedPassword es que ese registro pide password para HOSPEDAR y esta
	// máquina no tiene con qué contestarle.
	//
	// Cubre los tres casos a la vez: nunca se escribió ninguno, el refresh
	// caducó, y el operador cambió el password. Son uno solo porque **lo que hay
	// que hacer es idéntico**, y porque el registro se niega a decir cuál fue:
	// distinguirlos solo le regalaría información a quien esté probando.
	//
	// Entrar a una sala nunca lo produce.
	CodeSeedPassword Code = "seed_password"
	// CodeNoOwnSeed es que esta máquina todavía no tiene registro donde abrir
	// salas, así que no hay a quién pedirle un código.
	//
	// Es el hermano de [CodeSeedPassword] y son dos a propósito: acá falta
	// elegir el servidor, allá falta la credencial de uno ya elegido. Llevan a
	// dos pantallas distintas y a dos comandos distintos.
	//
	// Entrar a una sala tampoco lo produce: ahí el registro viene dentro del
	// código pegado.
	CodeNoOwnSeed Code = "no_own_seed"

	// CodeSeedMissing es un código PEGADO que no dice en qué servidor vive.
	//
	// Es el tercero de la familia y hace falta que sean tres. `no_own_seed` es
	// que esta máquina no eligió dónde hospedar, `seed_password` es que el
	// servidor elegido pide credencial, y este es que lo que alguien pegó no
	// identifica una sala: ocho caracteres existen en tantas salas como
	// registros haya.
	//
	// **Sin él caía en `internal`, y eso está medido corriendo el CLI contra el
	// daemon.** `internal` es "lo que no encaja en ningún otro", y su copia le
	// dice a la persona que Kanpachi se rompió y que reinicie la app. Nada se
	// rompió y reiniciar no arregla nada: falta media línea en lo que pegó.
	//
	// No se mete en `bad_code`, que es "esto no tiene forma de código", porque
	// desharía en el cable lo que el dominio separó: el centinela propio es lo
	// que permite enseñar la forma completa en vez de un genérico.
	CodeSeedMissing Code = "seed_missing"

	CodeCanceled    Code = "canceled"     // el usuario canceló la operación
	CodeBadNickname Code = "bad_nickname" // el nombre no cumple la decisión 21
	CodeBadCode     Code = "bad_code"     // el invite ID no tiene forma de código
	CodeBadProfile  Code = "bad_profile"  // el perfil no pasa las invariantes
	CodeUnavailable Code = "unavailable"  // el adaptador de abajo falló
	CodeInternal    Code = "internal"     // lo que no encaja en ninguno de arriba
)

// Request es lo que entra. Una línea de JSON.
type Request struct {
	ID     uint64          `json:"id"`
	Method Method          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// Response es lo que sale.
//
// **Puede llevar Result y Error a la vez**, y no es un descuido: la expulsión a
// medias contesta con la sala YA sin el expulsado más el aviso de lo que no se
// pudo cerrar, porque la pantalla necesita las dos cosas. Es el único caso, y
// está en `roomWithError`. Un cliente que mire el error primero y descarte el
// resultado tira el estado que acaba de pedir.
type Response struct {
	ID     uint64          `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *Error          `json:"error,omitempty"`
}

// Error es el fallo de una operación, con su código cerrado.
type Error struct {
	Code    Code   `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return string(e.Code) + ": " + e.Message }

// errorFor traduce un error del núcleo a un código de la API.
//
// La traducción vive acá y no en los casos de uso, y esa dirección importa: el
// núcleo no sabe que existe un named pipe, así que devuelve centinelas con
// sentido de producto y este adaptador decide cómo se ven en el cable.
//
// El orden del switch importa en un caso: ErrKickPartial ENVUELVE a los otros
// dos de expulsión, así que se comprueba antes.
func errorFor(err error) *Error {
	if err == nil {
		return nil
	}
	code := CodeInternal
	switch {
	case errors.Is(err, usecase.ErrKickPartial):
		code = CodeKickPartial
	case errors.Is(err, usecase.ErrBusy):
		code = CodeBusy
	case errors.Is(err, usecase.ErrFirewallBlocked):
		code = CodeFirewallBlocks
	case errors.Is(err, usecase.ErrNoRoom):
		code = CodeNoRoom
	case errors.Is(err, usecase.ErrNotHost):
		code = CodeNotHost
	case errors.Is(err, usecase.ErrUnknownGame):
		code = CodeUnknownGame
	case errors.Is(err, usecase.ErrNotAMember):
		code = CodeNotAMember
	case errors.Is(err, usecase.ErrSelfKick):
		code = CodeSelfKick
	case errors.Is(err, usecase.ErrShadowsBuiltin):
		code = CodeShadows
	case errors.Is(err, usecase.ErrNotPlayed):
		code = CodeNotPlayed
	case errors.Is(err, usecase.ErrProbeSelf):
		code = CodeProbeSelf
	case errors.Is(err, usecase.ErrProbeNoHost):
		code = CodeProbeNoHost
	case errors.Is(err, usecase.ErrNoSavedRoom):
		code = CodeNoSavedRoom
	case errors.Is(err, usecase.ErrNoSuchRoom):
		code = CodeNoSuchRoom
	// Va DESPUÉS del de arriba y son dos códigos, no uno. El registro afirmando
	// que no conoce un código y el registro sin contestar paran las dos
	// operaciones igual, y lo que la persona tiene que hacer es lo contrario:
	// pedir un código nuevo, contra volver a intentarlo en un rato.
	case errors.Is(err, usecase.ErrNoRegistry):
		code = CodeNoRegistry
	// Va antes que los de forma de entrada: `SeedPassword` envuelve el fallo del
	// adaptador, y ese fallo trae el centinela. Sin este caso caería en
	// `internal` y la interfaz no sabría a qué pantalla ir.
	case errors.Is(err, port.ErrSeedPassword):
		code = CodeSeedPassword
	// Sin esto caía en `internal`, que es el código de "lo que no encaja en
	// ningún otro", y hospedar en una instalación recién hecha es justo lo que
	// más encaja: falta configurar el registro y hay un comando que lo hace.
	case errors.Is(err, port.ErrNoOwnSeed):
		code = CodeNoOwnSeed
	case errors.Is(err, usecase.ErrCanceled):
		code = CodeCanceled
	case errors.Is(err, domain.ErrNicknameEmpty), errors.Is(err, domain.ErrNicknameTooLong),
		errors.Is(err, domain.ErrNicknameSymbol):
		code = CodeBadNickname
	// Va ANTES que los de forma, y son dos códigos y no uno: al código le falta
	// el servidor, contra el código no tiene forma de código. Lo que la persona
	// hace después es distinto en cada caso, y ese es el criterio.
	case errors.Is(err, domain.ErrSeedMissing):
		code = CodeSeedMissing
	case errors.Is(err, domain.ErrInputShape), errors.Is(err, domain.ErrInputTooLong),
		errors.Is(err, domain.ErrSeedHost):
		code = CodeBadCode
	case errors.Is(err, domain.ErrPersistedShape):
		code = CodeBadProfile
	}
	return &Error{Code: code, Message: err.Error()}
}

// badRequest arma el error de un mensaje que no se pudo interpretar.
func badRequest(format string, args ...any) *Error {
	return &Error{Code: CodeBadRequest, Message: fmt.Sprintf(format, args...)}
}
