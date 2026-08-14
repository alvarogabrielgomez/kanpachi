//go:build linux

package preflight

import (
	"context"
	"os/exec"
	"strings"
)

// DaemonService is the systemd unit name, without the `.service`. See the
// Windows twin for why the name is per system.
const DaemonService = "kanpachid"

// DaemonServiceRunning asks systemd. `is-active` answers on stdout and by exit
// code; the text is enough and does not need the exit code's help.
func DaemonServiceRunning() (bool, error) {
	out, err := exec.CommandContext(context.Background(), "systemctl", "is-active", DaemonService).Output()
	estado := strings.TrimSpace(string(out))
	if estado == "active" || estado == "activating" {
		return true, nil
	}
	// `inactive` and `unknown` exit non-zero: that is an answer, not a failure.
	if estado != "" {
		return false, nil
	}
	return false, err
}
