//go:build windows

package main

import (
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"

	"github.com/accentiostudios/kanpachi/daemon/transport/pipe"
)

// marcarPipe abre el pipe de producción, o falla rápido si no hay nadie.
func marcarPipe(plazo time.Duration) (net.Conn, error) {
	return winio.DialPipe(pipe.Name, &plazo)
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
