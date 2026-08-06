//go:build windows

package uihost

import (
	"errors"
	"fmt"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/accentiostudios/kanpachi/core/port"
)

// Con qué token se lanza la interfaz, y con qué API.
//
// # Por qué hay dos caminos, y no es una alternativa de estilo
//
// **`WTSQueryUserToken` exige `SE_TCB_NAME`, que solo tiene LocalSystem.** Es
// textual en la documentación. Es el camino del daemon INSTALADO, que es un
// servicio como SYSTEM en la sesión 0 y necesita cruzar a la sesión de quien
// está usando la máquina.
//
// El daemon de una carpeta portable NO es SYSTEM: es este mismo binario
// relanzado por el Control de cuentas de usuario, o sea el usuario elevado, y
// ya vive DENTRO de su propia sesión. Ahí esa llamada falla con "A required
// privilege is not held by the client", y falla de la peor forma posible: el
// daemon arranca, abre su pipe, y se queda corriendo sin nada en pantalla, que
// es exactamente la forma que la invariante de `docs/03` prohíbe. Medido en el
// primer arranque portable de verdad.
type entrega struct {
	tok    windows.Token
	sesión uint32

	// conToken pide usar `CreateProcessWithTokenW` en vez de
	// `CreateProcessAsUser`, y la diferencia es de PRIVILEGIOS.
	//
	// `CreateProcessAsUser` exige `SE_ASSIGNPRIMARYTOKEN`, que un
	// administrador elevado NO tiene: lo tiene SYSTEM. `CreateProcessWithTokenW`
	// exige `SE_IMPERSONATE`, que un administrador elevado SÍ tiene. Cada
	// camino usa el que puede.
	conToken bool
}

// tokenDelUsuario consigue con qué lanzar la interfaz, por el camino que haya.
func tokenDelUsuario(log port.Logger) (entrega, error) {
	sesión := windows.WTSGetActiveConsoleSessionId()
	// 0xFFFFFFFF es "no hay ninguna", que pasa entre cerrar sesión y abrir otra.
	if sesión == 0xFFFFFFFF {
		return entrega{}, fmt.Errorf("no hay ninguna sesión de usuario abierta donde mostrar la interfaz")
	}

	var tok windows.Token
	err := windows.WTSQueryUserToken(sesión, &tok)
	if err == nil {
		return entrega{tok: tok, sesión: sesión}, nil
	}
	// Cualquier otro fallo se cuenta como es. Solo la falta de privilegio
	// significa "este daemon no es SYSTEM", que es el caso portable.
	if !errors.Is(err, windows.ERROR_PRIVILEGE_NOT_HELD) {
		return entrega{}, fmt.Errorf("pidiendo el token del usuario de la sesión %d: %w", sesión, err)
	}

	nuestra := sesión
	if err := windows.ProcessIdToSessionId(windows.GetCurrentProcessId(), &nuestra); err != nil {
		log.Warn("no se pudo averiguar la sesión de este proceso, se usa la de la consola activa",
			"error", err)
		nuestra = sesión
	}

	// `CreateProcessWithTokenW` lo exige, y lo exige ENCENDIDO. Ver
	// [habilitarPrivilegio].
	if err := habilitarPrivilegio("SeImpersonatePrivilege"); err != nil {
		log.Warn("no se pudo encender el privilegio de suplantación, "+
			"lanzar la interfaz puede fallar", "error", err)
	}

	shell, err := tokenDelShell(nuestra)
	if err != nil {
		return entrega{}, err
	}
	log.Info("este daemon no es SYSTEM, así que la interfaz se lanza con el token del Explorador",
		"sesión", nuestra)
	return entrega{tok: shell, sesión: nuestra, conToken: true}, nil
}

// shellExe es el proceso del que se toma prestado el token.
const shellExe = "explorer.exe"

// tokenDelShell toma prestado el token del Explorador de esta sesión.
//
// # Por qué el del Explorador y no el gemelo de este proceso
//
// Porque el gemelo NO SIRVE, y eso está medido. La vía obvia es pedir el token
// enlazado del propio proceso elevado, que es el limitado que UAC fabrica en el
// inicio de sesión. La documentación de `TokenLinkedToken` dice que ese token
// vuelve como PRIMARIO solo si quien pregunta tiene `SeTcbPrivilege`, y como
// token de suplantación de nivel identificación si no. Un administrador elevado
// no tiene ese privilegio, así que vuelve el de identificación, y
// `DuplicateTokenEx` a primario falla con ERROR_BAD_IMPERSONATION_LEVEL. Salió
// corriéndolo: "Either a required impersonation level was not provided, or the
// provided impersonation level is invalid".
//
// El Explorador es el proceso que está corriendo AHORA con el token que se
// quiere: el mismo usuario, sin elevar, en esta sesión. Su token es primario,
// así que duplicarlo sí funciona.
//
// **Se busca por sesión y no solo por nombre.** Con varias sesiones abiertas,
// el primer `explorer.exe` de la lista puede ser el de otra persona, y lanzar la
// interfaz con su token la pondría en el escritorio de quien no la pidió.
func tokenDelShell(sesión uint32) (windows.Token, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return 0, fmt.Errorf("listando los procesos para encontrar el Explorador: %w", err)
	}
	defer func() { _ = windows.CloseHandle(snap) }()

	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		if !strings.EqualFold(windows.UTF16ToString(e.ExeFile[:]), shellExe) {
			continue
		}
		var suya uint32
		if err := windows.ProcessIdToSessionId(e.ProcessID, &suya); err != nil || suya != sesión {
			continue
		}
		// LIMITED_INFORMATION alcanza para abrir el token y es lo mínimo:
		// `PROCESS_QUERY_INFORMATION` a secas pide más de lo que hace falta.
		h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, e.ProcessID)
		if err != nil {
			continue
		}
		var tok windows.Token
		err = windows.OpenProcessToken(h, windows.TOKEN_DUPLICATE|windows.TOKEN_QUERY, &tok)
		_ = windows.CloseHandle(h)
		if err != nil {
			continue
		}
		return tok, nil
	}

	return 0, fmt.Errorf("no se encontró %s en la sesión %d, así que no hay token de usuario sin elevar "+
		"con el que abrir la interfaz", shellExe, sesión)
}

// accesoQueHaceFalta son los derechos que se le piden al token duplicado.
//
// **Explícitos, y no el cero que pide "los mismos que el original".** El cero
// funcionaba con el token que devuelve `WTSQueryUserToken`, que viene con acceso
// total, y no con el del Explorador, que se abre con lo mínimo para poder
// duplicarlo. El duplicado heredaba ese mínimo, se quedaba sin
// `TOKEN_ASSIGN_PRIMARY`, y lanzar fallaba con un "Access is denied" que no
// nombra al token por ningún lado. Medido en el segundo intento portable.
//
// Pedirlos explícitos no es un permiso que se conceda: el duplicado se evalúa
// contra el descriptor de seguridad del token, y lo único que hace falta sobre
// el handle de origen es `TOKEN_DUPLICATE`.
const accesoQueHaceFalta = windows.TOKEN_ASSIGN_PRIMARY | windows.TOKEN_DUPLICATE |
	windows.TOKEN_QUERY | windows.TOKEN_ADJUST_DEFAULT | windows.TOKEN_ADJUST_SESSIONID

// habilitarPrivilegio enciende un privilegio que el token ya tiene.
//
// # Tenerlo y usarlo son dos cosas
//
// Un privilegio vive en el token en uno de dos estados, presente y apagado o
// presente y encendido, y la comprobación de las APIs mira el segundo. Un
// administrador elevado TIENE `SeImpersonatePrivilege` por pertenecer al grupo,
// y le llega apagado: nada en el arranque de un programa en Go lo enciende.
//
// Es best effort a propósito. `AdjustTokenPrivileges` contesta que sí aunque no
// haya asignado nada, así que quien decide si esto sirvió es la llamada que
// venía a hacerse, no esta.
func habilitarPrivilegio(nombre string) error {
	var t windows.Token
	if err := windows.OpenProcessToken(
		windows.CurrentProcess(),
		windows.TOKEN_ADJUST_PRIVILEGES|windows.TOKEN_QUERY,
		&t,
	); err != nil {
		return fmt.Errorf("abriendo el token de este proceso: %w", err)
	}
	defer func() { _ = t.Close() }()

	n, err := windows.UTF16PtrFromString(nombre)
	if err != nil {
		return err
	}
	var luid windows.LUID
	if err := windows.LookupPrivilegeValue(nil, n, &luid); err != nil {
		return fmt.Errorf("buscando el privilegio %s: %w", nombre, err)
	}

	tp := windows.Tokenprivileges{PrivilegeCount: 1}
	tp.Privileges[0] = windows.LUIDAndAttributes{Luid: luid, Attributes: windows.SE_PRIVILEGE_ENABLED}
	if err := windows.AdjustTokenPrivileges(t, false, &tp, 0, nil, nil); err != nil {
		return fmt.Errorf("encendiendo el privilegio %s: %w", nombre, err)
	}
	return nil
}

// procCreateProcessWithToken es `CreateProcessWithTokenW`, que x/sys no envuelve.
var procCreateProcessWithToken = windows.NewLazySystemDLL("advapi32.dll").NewProc("CreateProcessWithTokenW")

// crearProcesoConToken lanza con un token sin necesitar `SE_ASSIGNPRIMARYTOKEN`.
//
// La firma no lleva descriptores de seguridad ni bandera de herencia, a
// diferencia de `CreateProcessAsUser`: esta API nunca hereda handles, que acá da
// igual porque tampoco se quiere heredar nada entre sesiones.
func crearProcesoConToken(
	tok windows.Token,
	exe, cmd *uint16,
	banderas uint32,
	entorno *uint16,
	si *windows.StartupInfo,
	pi *windows.ProcessInformation,
) error {
	r, _, err := procCreateProcessWithToken.Call(
		uintptr(tok),
		0, // sin LOGON_WITH_PROFILE: el perfil de esta persona ya está cargado
		uintptr(unsafe.Pointer(exe)),
		uintptr(unsafe.Pointer(cmd)),
		uintptr(banderas),
		uintptr(unsafe.Pointer(entorno)),
		0, // directorio de trabajo: el de este proceso
		uintptr(unsafe.Pointer(si)),
		uintptr(unsafe.Pointer(pi)),
	)
	if r == 0 {
		return err
	}
	return nil
}
