//go:build !windows && !linux

package preflight

import "errors"

// DaemonService: outside Windows and Linux nothing registers the daemon; the
// name of the Unix world is answered so callers can print something.
const DaemonService = "kanpachid"

func DaemonServiceRunning() (bool, error) {
	return false, errors.New("no hay gestor de servicios que preguntar en este sistema")
}
