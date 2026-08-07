//go:build windows

package main

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"

	"github.com/accentiostudios/kanpachi/daemon/transport/pipe"
)

// marcarPipe abre el canal del producto al que pertenece ESTE ejecutable.
//
// El marcador portable vive junto al binario, así que la decisión no depende
// de argumentos que una de las tres rutas de arranque pueda olvidar.
func marcarPipe(plazo time.Duration) (net.Conn, error) {
	nombre := pipe.Name
	if esPortable() {
		nombre = pipe.PortableName
	}
	return winio.DialPipe(nombre, &plazo)
}

// ArrancarServicio le pide al Administrador de servicios que levante el daemon.
//
// Devuelve `yaEstaba` en true si el servicio ya estaba corriendo o arrancando,
// que NO es un error: es el caso de hacer doble clic con Kanpachi ya abierto.
//
// # El permiso, y por qué el instalador tiene que concederlo
//
// Arrancar un servicio exige `SERVICE_START`, que por defecto no tiene el
// usuario interactivo. El instalador se lo concede sobre ESTE servicio con un
// `sc sdset`, y esa concesión es lo que sostiene la promesa de un solo UAC en
// toda la vida del producto: sin ella, cada doble clic pediría elevación.
//
// El gestor se abre con `SC_MANAGER_CONNECT` y NO con `mgr.Connect`, que pide
// `SC_MANAGER_ALL_ACCESS` y por eso falla sin elevar. El servicio se abre con
// los dos permisos que hacen falta y ninguno más.
func ArrancarServicio(args []string) (yaEstaba bool, err error) {
	scm, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return false, fmt.Errorf("abriendo el Administrador de servicios: %w", err)
	}
	defer func() { _ = windows.CloseServiceHandle(scm) }()

	nombre, err := windows.UTF16PtrFromString(ServiceName)
	if err != nil {
		return false, err
	}
	h, err := windows.OpenService(scm, nombre, windows.SERVICE_START|windows.SERVICE_QUERY_STATUS)
	if err != nil {
		if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
			return false, fmt.Errorf("el servicio de Kanpachi no está registrado.\n" +
				"  Lo registra el instalador. Si moviste los archivos a mano, vuelve a instalar")
		}
		if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			return false, fmt.Errorf("no hay permiso para arrancar el servicio de Kanpachi.\n" +
				"  Lo concede el instalador. Vuelve a instalar, o arráncalo desde Servicios")
		}
		return false, fmt.Errorf("abriendo el servicio %s: %w", ServiceName, err)
	}
	s := &mgr.Service{Name: ServiceName, Handle: h}
	defer func() { _ = s.Close() }()

	// Preguntar antes de arrancar convierte la carrera en un caso normal: entre
	// esta consulta y el arranque puede aparecer otro, y para eso está el
	// tratamiento de ERROR_SERVICE_ALREADY_RUNNING de abajo.
	if estado, err := s.Query(); err == nil {
		if estado.State == svc.Running || estado.State == svc.StartPending {
			return true, nil
		}
	}

	if err := s.Start(args...); err != nil {
		if errors.Is(err, windows.ERROR_SERVICE_ALREADY_RUNNING) {
			return true, nil
		}
		if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			return false, fmt.Errorf("no hay permiso para arrancar el servicio de Kanpachi.\n" +
				"  Lo concede el instalador. Vuelve a instalar, o arráncalo desde Servicios")
		}
		return false, fmt.Errorf("arrancando el servicio %s: %w", ServiceName, err)
	}
	return false, nil
}

// ArrancarSuelto relanza este mismo binario, elevado, como daemon portable.
//
// # Por qué ShellExecute y no CreateProcess
//
// Porque el verbo `runas` es la única forma documentada de pedir elevación
// desde un proceso que no la tiene: `CreateProcess` no eleva, devuelve
// ERROR_ELEVATION_REQUIRED y ya. Con `runas`, Windows enseña el diálogo de
// Control de cuentas de usuario y arranca el proceso nuevo con el token de
// administrador.
//
// El proceso hijo es INDEPENDIENTE: este lanzador se muere enseguida y el
// daemon se queda. Es lo mismo que pasa por el camino del servicio, donde quien
// sobrevive es el Administrador de servicios.
//
// # El caso que hay que nombrar
//
// Que el usuario diga que no al diálogo. Windows devuelve ERROR_CANCELLED, y
// eso no es un fallo del programa: es una respuesta. Se dice con esas palabras,
// porque "acceso denegado" delante de alguien que acaba de pulsar «No» es
// hablarle de otra cosa.
func ArrancarSuelto(args []string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("averiguando la ruta de este ejecutable: %w", err)
	}

	// La bandera del papel va SIEMPRE, y las que pidieran detrás. Es lo que
	// hace que el proceso nuevo sea el daemon en vez de otro lanzador, que
	// volvería a sondear el pipe y a relanzarse.
	línea := strings.Join(append([]string{ArgDaemon}, args...), " ")

	verbo, err := windows.UTF16PtrFromString("runas")
	if err != nil {
		return err
	}
	ruta, err := windows.UTF16PtrFromString(exe)
	if err != nil {
		return err
	}
	params, err := windows.UTF16PtrFromString(línea)
	if err != nil {
		return err
	}
	// El directorio de trabajo es el de la carpeta portable. No lo usa nadie
	// —todo se resuelve contra `os.Executable()`— y se pone igual para que un
	// volcado no aparezca en system32.
	cwd, err := windows.UTF16PtrFromString(filepath.Dir(exe))
	if err != nil {
		return err
	}

	// SW_HIDE: el daemon no tiene ventana, y el binario va con `-H windowsgui`.
	// Pedir una la dejaría parpadear.
	if err := windows.ShellExecute(0, verbo, ruta, params, cwd, windows.SW_HIDE); err != nil {
		if errors.Is(err, windows.ERROR_CANCELLED) {
			return fmt.Errorf("hace falta permiso de administrador para arrancar Kanpachi desde esta carpeta.\n" +
				"  Es lo que pedía la ventana de Control de cuentas de usuario.\n" +
				"  Solo lo pide esta versión portable: la instalada lo pide una vez, al instalar")
		}
		return fmt.Errorf("relanzando Kanpachi con permisos de administrador: %w", err)
	}
	return nil
}

// avisar enseña el error en una ventana.
//
// Hace falta porque este binario está enlazado con `-H windowsgui`: quien hace
// doble clic no tiene consola, así que un `Fprintln` a stderr no lo lee nadie y
// el síntoma sería un icono que no hace nada al pulsarlo. Cuando SÍ hay consola
// —lo llamaron desde una terminal— el mensaje ya se imprimió, y esto se calla.
func avisar(msg string) {
	if hayConsola() {
		return
	}
	texto, err := windows.UTF16PtrFromString(msg)
	if err != nil {
		return
	}
	título, err := windows.UTF16PtrFromString("Kanpachi")
	if err != nil {
		return
	}
	// MB_ICONERROR | MB_OK, y MB_SETFOREGROUND para que no nazca detrás de lo
	// que el usuario tenga delante.
	_, _ = windows.MessageBox(0, texto, título, windows.MB_OK|windows.MB_ICONERROR|windows.MB_SETFOREGROUND)
}
