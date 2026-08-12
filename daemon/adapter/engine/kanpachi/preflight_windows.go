//go:build windows

package kanpachi

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Preflight comprueba que esta máquina PUEDA construir un adaptador virtual,
// antes de que nadie abra una sala.
//
// # El fallo que esto existe para contar
//
// Medido el 2026-08-11 en la máquina de un invitado que no podía entrar. Tenía
// todos los archivos en su sitio, wintun cargó bien, y aun así no hubo
// adaptador. Su `kanpachi-engine.log`:
//
//	ERROR WinTun: Installing driver 0.14
//	ERROR WinTun: Could not install driver ...\wintun.inf to store:
//	      The system could not find the environment option that was entered.
//	      (Code 0x000000CB)
//	INFO  TunDeviceError("rust tun error Failed to create adapter")
//
// `0xCB` es `ERROR_ENVVAR_NOT_FOUND`: el almacén de drivers de Windows rechazó
// el `.inf`. Nada de eso es de Kanpachi, y en esa máquina WireGuard, Tailscale
// o WARP fallarían igual.
//
// Lo que se veía en cambio eran treinta segundos de nada y después «el adaptador
// no tomó la dirección», que manda a mirar el direccionamiento, que es justo lo
// único que estaba bien.
//
// # Por qué NO alcanza con mirar si los archivos están
//
// Porque en ese caso estaban. El portable ya los comprueba por nombre al
// extraerse y pasó limpio. La presencia no es la pregunta: la pregunta es si el
// driver se puede INSTALAR, y eso solo se sabe intentándolo. wintun no expone
// «¿se podría?», expone `WintunCreateAdapter`.
//
// Por eso esto crea un adaptador de mentira y lo cierra. **Medido acá el
// 2026-08-11**, elevado, con el driver ya instalado: 678 ms y 952 ms en dos
// corridas seguidas, y cero adaptadores dejados atrás según `Get-NetAdapter
// -IncludeHidden`. La instalación del driver, cuando hace falta, es el paso
// barato: en el log del invitado va de `Installing driver 0.14` al fallo en
// 160 ms.
//
// Tiene además un efecto lateral que conviene: en una máquina sana deja el
// driver instalado antes de que haga falta.
//
// Los dos pasos baratos de antes no sobran. Convierten «no se pudo crear» en
// «no se pudo crear PORQUE falta esta DLL», que es otro problema con otro
// arreglo.
//
// # Qué devuelve
//
// El error va derecho a un cuadro de diálogo, así que está escrito para leerse
// y no para grepearse, y trae DENTRO lo que dijo wintun de su propia boca. Ver
// [wintunSaid].
func Preflight(engineDir string) error {
	if err := preflightFiles(engineDir); err != nil {
		return err
	}
	return preflightAdapter(engineDir)
}

// required son los archivos sin los cuales el motor no arranca, y son estos DOS.
//
// Es la misma lista que `imprescindibles` del portable, y por el mismo criterio:
// lo que no puede faltar, no lo que se distribuye. `WinDivert64.sys` queda fuera
// a propósito. Lo arrastra la dependencia `windivert`, que Kanpachi no usa
// —corre con `proxy_cidrs` vacío y el proxy KCP apagado—, así que exigirlo sería
// inventar un fallo que no impide nada.
var required = []struct{ name, why string }{
	{"wintun.dll", "es el driver de red virtual, o sea el adaptador entero"},
	{"Packet.dll", "es una importación DURA del motor: sin ella el proceso no llega ni a arrancar, " +
		"y Windows solo dice 0xC0000135 sin nombrar cuál falta"},
}

func preflightFiles(engineDir string) error {
	for _, f := range required {
		path := filepath.Join(engineDir, f.name)
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("falta %s, que %s.\n\nDebería estar acá:\n%s\n\n"+
				"Qué hacer: si es la versión portable, vuelve a abrir el archivo que te pasaron, "+
				"porque se extrae solo. Si está instalada, reinstala Kanpachi. "+
				"Un antivirus que borra archivos de la carpeta también deja esto así",
				f.name, f.why, path)
		}
	}
	return nil
}

// ─── wintun ──────────────────────────────────────────────────────────────────

// Las firmas que se usan, de `wintun.h`:
//
//	WINTUN_ADAPTER_HANDLE WintunCreateAdapter(LPCWSTR Name, LPCWSTR TunnelType,
//	                                          const GUID *RequestedGUID);
//	void WintunCloseAdapter(WINTUN_ADAPTER_HANDLE Adapter);
//	void WintunSetLogger(WINTUN_LOGGER_CALLBACK NewLogger);
//
// `WintunCreateAdapter` devuelve NULL al fallar y deja el motivo en
// `GetLastError`.
const (
	probeName       = "Kanpachi preflight"
	probeTunnelType = "Kanpachi"
)

// preflightAdapter hace lo único que contesta la pregunta: crear uno.
func preflightAdapter(engineDir string) error {
	path := filepath.Join(engineDir, "wintun.dll")

	// Por ruta ABSOLUTA y no por nombre. Cargarla por nombre la buscaría por el
	// orden de búsqueda del proceso, que empieza por el directorio de ESTE
	// binario: el daemon y el motor viven juntos hoy, y el día que no, esto
	// estaría midiendo una DLL distinta de la que va a usar el motor.
	dll, err := windows.LoadDLL(path)
	if err != nil {
		return fmt.Errorf("no se pudo cargar %s.\n\nEl archivo está ahí y aun así Windows no lo abre. "+
			"Suele ser una copia dañada o un antivirus que lo tiene retenido.\n\n"+
			"Detalle: %v", path, err)
	}
	defer func() { _ = dll.Release() }()

	create, err := dll.FindProc("WintunCreateAdapter")
	if err != nil {
		return fmt.Errorf("%s no es la biblioteca que Kanpachi espera: no tiene WintunCreateAdapter.\n\n"+
			"Detalle: %v", path, err)
	}
	closeAdapter, err := dll.FindProc("WintunCloseAdapter")
	if err != nil {
		return fmt.Errorf("%s no es la biblioteca que Kanpachi espera: no tiene WintunCloseAdapter.\n\n"+
			"Detalle: %v", path, err)
	}

	stop := captureWintunLog(dll)
	defer stop()

	name, err := windows.UTF16PtrFromString(probeName)
	if err != nil {
		return err
	}
	kind, err := windows.UTF16PtrFromString(probeTunnelType)
	if err != nil {
		return err
	}

	// El tercer argumento es el GUID pedido, y va en cero a propósito: pedir uno
	// fijo haría que dos Kanpachi en la misma máquina se pelearan por el mismo
	// dispositivo. Que lo elija wintun.
	handle, _, lastErr := create.Call(
		uintptr(unsafe.Pointer(name)),
		uintptr(unsafe.Pointer(kind)),
		0,
	)
	if handle == 0 {
		return adapterFailed(lastErr)
	}
	// Se cierra en el acto. Este adaptador no es para usarlo, es para saber si se
	// podía. Wintun lo quita del sistema al cerrarlo.
	_, _, _ = closeAdapter.Call(handle)
	return nil
}

// errEnvVarNotFound es `ERROR_ENVVAR_NOT_FOUND`, o sea 203, o sea `0xCB`.
//
// Se escribe acá y no se toma de `x/sys/windows` para que el número quede al
// lado del motivo por el que a Kanpachi le importa, que no es evidente: en una
// instalación de driver significa que una variable de entorno del sistema no se
// pudo resolver, y la que se resuelve en ese paso es `DevicePath`.
const errEnvVarNotFound = windows.Errno(203)

// adapterFailed arma la frase con lo que dijo Windows y lo que dijo wintun.
//
// Los dos, porque dicen cosas distintas y ninguno solo alcanza. Windows contesta
// un código, que suelto no significa nada para nadie, y wintun escribe en su log
// la línea que nombra el archivo y la operación que lo rechazó.
//
// # Por qué el consejo se bifurca
//
// Porque hay dos fallos MEDIDOS y quieren cosas opuestas, y un consejo genérico
// manda a la mitad de la gente al sitio equivocado:
//
//   - `0x05`, acceso denegado. Medido acá el 2026-08-11 corriendo el sondeo sin
//     elevar: wintun ni siquiera llega a instalar, se queda en «Failed to take
//     device installation mutex». Es permisos, y decir «reinicia» no arregla
//     nada.
//   - `0xCB`, `ERROR_ENVVAR_NOT_FOUND`. Medido en la máquina de un invitado el
//     2026-08-11: wintun sí llega a instalar y el almacén de drivers rechaza el
//     `.inf`. Acá reiniciar tampoco arregla, y lo que hay que mirar es
//     `DevicePath`.
//
// Cualquier otro código cae en el consejo general, que empieza por reiniciar
// porque es barato y porque un dispositivo a medio morir de una corrida anterior
// sí se arregla así.
func adapterFailed(lastErr error) error {
	var b strings.Builder
	b.WriteString("Kanpachi no puede crear su adaptador de red virtual en esta máquina.\n\n")
	b.WriteString("No es la sala ni la conexión: es el driver de red virtual (wintun). " +
		"Mientras siga así, ningún programa de este tipo va a funcionar acá, ni Kanpachi " +
		"ni WireGuard ni Tailscale.\n\n")

	if said := wintunSaid(); said != "" {
		b.WriteString("Lo que dijo el driver:\n")
		b.WriteString(said)
		b.WriteString("\n\n")
	}
	if lastErr != nil {
		b.WriteString(fmt.Sprintf("Lo que dijo Windows:\n  %v\n\n", lastErr))
	}
	b.WriteString(whatToDo(lastErr))
	return fmt.Errorf("%s", b.String())
}

func whatToDo(lastErr error) string {
	switch {
	case errors.Is(lastErr, windows.ERROR_ACCESS_DENIED):
		return "Qué hacer: esto es permiso, no configuración. Kanpachi no está corriendo " +
			"como administrador, que es lo que hace falta para crear un adaptador de red. " +
			"En la versión portable hay que aceptar el aviso de Windows al abrirla; en la " +
			"instalada lo hace su servicio, así que si sale esto es que el servicio no está " +
			"corriendo como SYSTEM y hay que reinstalar."

	case errors.Is(lastErr, errEnvVarNotFound):
		return "Qué hacer: Windows no pudo guardar el driver en su almacén porque no resolvió " +
			"una variable del sistema. Casi siempre es DevicePath, que tiene que existir y " +
			"ser del tipo REG_EXPAND_SZ. Se mira así, en PowerShell como administrador:\n\n" +
			`  reg query "HKLM\SOFTWARE\Microsoft\Windows\CurrentVersion" /v DevicePath` +
			"\n\nTiene que contestar REG_EXPAND_SZ y %SystemRoot%\\inf. Si dice REG_SZ o no " +
			"está, eso es. C:\\Windows\\INF\\setupapi.dev.log lleva el detalle del intento."

	default:
		return "Qué hacer: lo primero es reiniciar y volver a abrirlo, que se lleva por " +
			"delante un dispositivo a medio crear de una corrida anterior. Si sigue igual, " +
			"esto va en el reporte tal cual, junto con kanpachi-engine.log."
	}
}

// ─── El log propio de wintun ─────────────────────────────────────────────────

// wintun escribe SU log por un callback, y sin registrarlo esas líneas no
// existen en ningún sitio: no van a un archivo, no van a stderr, no van al
// visor de eventos. Registrarlo es la diferencia entre «no se pudo crear el
// adaptador» y la línea que nombra el `.inf` y la operación que lo rechazó.
//
// Firma, de `wintun.h`:
//
//	typedef void (*WINTUN_LOGGER_CALLBACK)(WINTUN_LOGGER_LEVEL Level,
//	                                       DWORD64 Timestamp, LPCWSTR Message);
//
// `Timestamp` se recibe como un `uintptr` porque un `DWORD64` entra en un
// registro en amd64, que es el único destino en que se compila esto. Se ignora:
// la hora la pone el log del daemon.
var (
	wintunLogMu    sync.Mutex
	wintunLogLines []string
)

func wintunLogged(level uintptr, _ uintptr, msg *uint16) uintptr {
	if msg == nil {
		return 0
	}
	// WINTUN_LOG_INFO 0, WINTUN_LOG_WARN 1, WINTUN_LOG_ERR 2. Se guardan las tres
	// y se filtra al leer: la línea de INFO que dice «Installing driver 0.14» es
	// la que sitúa en qué paso murió.
	prefijo := map[uintptr]string{0: "", 1: "aviso: ", 2: "error: "}[level]

	wintunLogMu.Lock()
	defer wintunLogMu.Unlock()
	if len(wintunLogLines) < maxWintunLines {
		wintunLogLines = append(wintunLogLines, prefijo+windows.UTF16PtrToString(msg))
	}
	return 0
}

// maxWintunLines acota lo que se guarda. El cuadro de diálogo tiene que caber en
// una pantalla, y lo que importa son las últimas líneas antes del fallo.
const maxWintunLines = 12

// captureWintunLog engancha el callback y devuelve con qué soltarlo.
//
// El desenganche NO es opcional: el callback vive en este proceso y la DLL se
// libera al salir de [preflightAdapter]. Una wintun que siguiera con nuestro
// puntero después de eso llamaría a memoria que ya no es de nadie.
func captureWintunLog(dll *windows.DLL) func() {
	wintunLogMu.Lock()
	wintunLogLines = nil
	wintunLogMu.Unlock()

	setLogger, err := dll.FindProc("WintunSetLogger")
	if err != nil {
		// Sin log propio se sigue igual: el sondeo vale, y el mensaje sale con lo
		// que diga Windows y nada más. Es peor, no es inútil.
		return func() {}
	}
	cb := windows.NewCallback(wintunLogged)
	_, _, _ = setLogger.Call(cb)
	return func() { _, _, _ = setLogger.Call(0) }
}

// wintunSaid devuelve lo capturado, ya sangrado para el cuadro de diálogo.
func wintunSaid() string {
	wintunLogMu.Lock()
	defer wintunLogMu.Unlock()
	if len(wintunLogLines) == 0 {
		return ""
	}
	return "  " + strings.Join(wintunLogLines, "\n  ")
}
