//go:build windows

package preflight

import (
	"errors"
	"fmt"

	"golang.org/x/sys/windows"
)

// DaemonService is how the installer registers the daemon HERE. Per system on
// purpose: the Linux unit is `kanpachid`, and one constant for two names would
// be wrong on one of the two. It used to be declared three times, one of them
// with a comment admitting the copy.
const DaemonService = "kanpachi-daemon"

// DaemonServiceRunning asks the service manager whether the installed daemon
// is alive right now. A missing service answers false and no error: most
// machines this runs on do not have Kanpachi installed at all.
//
// What the CALLER does with a yes is the caller's: roomprobe refuses to start
// because building its session would purge the daemon's firewall rules with
// somebody's room open behind them, and doctor merely reports.
func DaemonServiceRunning() (bool, error) {
	gestor, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return false, fmt.Errorf("abriendo el gestor de servicios: %w", err)
	}
	defer func() { _ = windows.CloseServiceHandle(gestor) }()

	nombre, err := windows.UTF16PtrFromString(DaemonService)
	if err != nil {
		return false, err
	}
	h, err := windows.OpenService(gestor, nombre, windows.SERVICE_QUERY_STATUS)
	if err != nil {
		if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
			return false, nil
		}
		return false, fmt.Errorf("abriendo el servicio %s: %w", DaemonService, err)
	}
	defer func() { _ = windows.CloseServiceHandle(h) }()

	var estado windows.SERVICE_STATUS
	if err := windows.QueryServiceStatus(h, &estado); err != nil {
		return false, fmt.Errorf("preguntando el estado de %s: %w", DaemonService, err)
	}
	return estado.CurrentState != windows.SERVICE_STOPPED, nil
}
