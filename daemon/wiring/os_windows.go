//go:build windows

package wiring

// What this package chooses on Windows. The twin files are os_linux.go and
// os_other.go; declaring the choice per system means that a third system
// without its own file is a LINK error, never a `default` that inherits
// another system's list.

import (
	"fmt"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/core/port"
	"github.com/accentiostudios/kanpachi/daemon/adapter/firewall"
)

// Quarantine is which port list the base quarantine closes here.
const Quarantine = domain.QuarantineWindows

// NewFirewall opens the two containment layers and returns the three faces the
// wiring needs: the port that writes, the one that measures, and the close.
//
// # Why the audit is composed here
//
// [port.ExposureAudit] has three questions and the firewall can only answer
// two. `RouterMappings` talks to the user's ROUTER over IGD, another protocol
// on another network, and it is the single place in the product that looks
// outside the machine. The firewall adapter refuses to implement the whole
// port on purpose: answering `nil, nil` to the router's question would make
// "there are no mappings" and "nobody looked" indistinguishable, on the one
// screen whose job is telling those apart.
func NewFirewall(dataDir string, log port.Logger, router port.ExposureAudit) (
	port.FirewallPort, port.ExposureAudit, func() error, error) {

	fw, close, err := firewall.NewWindows(dataDir, log)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("%w.\n"+
			"  Escribir en el firewall exige administrador, así que en modo consola hay "+
			"que abrir la terminal elevada", err)
	}
	return fw, Exposure{FW: fw, Router: router}, close, nil
}
