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

	MethodPendingRoom        Method = "pending_room"
	MethodResumeRoom         Method = "resume_room"
	MethodDiscardPendingRoom Method = "discard_pending_room"
	MethodLastRoom           Method = "last_room"
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
	MethodPendingRoom:         true,
	MethodResumeRoom:          true,
	MethodDiscardPendingRoom:  true,
	MethodLastRoom:            true,
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
	CodeBadNickname Code = "bad_nickname"  // el nombre no cumple la decisión 21
	CodeBadCode     Code = "bad_code"      // el invite ID no tiene forma de código
	CodeBadProfile  Code = "bad_profile"   // el perfil no pasa las invariantes
	CodeUnavailable Code = "unavailable"   // el adaptador de abajo falló
	CodeInternal    Code = "internal"      // lo que no encaja en ninguno de arriba
)

// Request es lo que entra. Una línea de JSON.
type Request struct {
	ID     uint64          `json:"id"`
	Method Method          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// Response es lo que sale. Lleva Result o Error, jamás los dos.
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
	case errors.Is(err, domain.ErrNicknameEmpty), errors.Is(err, domain.ErrNicknameTooLong),
		errors.Is(err, domain.ErrNicknameSymbol):
		code = CodeBadNickname
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
