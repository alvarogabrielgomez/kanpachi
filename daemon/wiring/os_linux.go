//go:build linux

package wiring

// The Linux twin of os_windows.go. The differences are the system's: the gate
// is nftables instead of WFP, and the quarantine list is not the Windows one
// for an expensive reason — there port 22 is an optional service, here it is
// the channel the operator manages their server through.

import (
	"fmt"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/core/port"
	"github.com/accentiostudios/kanpachi/daemon/adapter/firewall"
	"github.com/accentiostudios/kanpachi/daemon/adapter/firewall/linux/nftpermits"
)

// Quarantine is which port list the base quarantine closes here.
const Quarantine = domain.QuarantineLinux

// NewFirewall composes the two Linux layers. See the Windows twin for why the
// audit is composed here.
func NewFirewall(_ string, log port.Logger, router port.ExposureAudit) (
	port.FirewallPort, port.ExposureAudit, func() error, error) {

	// The quarantine goes to its usual path and not to the data directory: the
	// systemd unit that loads it at boot points there, and the two have to say
	// the same thing or the quarantine is lost on the next reboot with nothing
	// saying so.
	fw, close, err := firewall.NewLinux(nftpermits.QuarantineFile, log)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("%w.\n"+
			"  Escribir en nftables exige CAP_NET_ADMIN, así que corriendo a mano hay que "+
			"usar sudo. El servicio lo trae en su unidad", err)
	}
	return fw, Exposure{FW: fw, Router: router}, close, nil
}
