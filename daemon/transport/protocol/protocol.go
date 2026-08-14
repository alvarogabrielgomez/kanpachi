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

	MethodPendingRoom        Method = "pending_room"
	MethodResumeRoom         Method = "resume_room"
	MethodDiscardPendingRoom Method = "discard_pending_room"
	MethodLastRoom           Method = "last_room"

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
	MethodPendingRoom:         true,
	MethodResumeRoom:          true,
	MethodDiscardPendingRoom:  true,
	MethodLastRoom:            true,
	MethodProgress:            true,
	MethodCancel:              true,
	MethodShowUI:              true,
	MethodPendingInvite:       true,
	MethodPreviewInvite:       true,
	MethodShutdown:            true,
	MethodAutostart:           true,
	MethodOwnSeed:             true,
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
	CodeNoPending   Code = "no_pending"    // no hay sala del arranque anterior
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

	CodeCanceled     Code = "canceled"     // el usuario canceló la operación
	CodeBadNickname  Code = "bad_nickname" // el nombre no cumple la decisión 21
	CodeBadCode      Code = "bad_code"     // el invite ID no tiene forma de código
	CodeBadProfile   Code = "bad_profile"  // el perfil no pasa las invariantes
	CodeUnavailable  Code = "unavailable"  // el adaptador de abajo falló
	CodeInternal     Code = "internal"     // lo que no encaja en ninguno de arriba
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
	case errors.Is(err, usecase.ErrNoPendingRoom):
		code = CodeNoPending
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
