//go:build !windows && !linux

package wiring

// Outside Windows and Linux there is no firewall adapter, and there will not
// be one: the Windows one is Windows Firewall rules plus a WFP session, the
// Linux one is nftables. This file exists so that `go build ./...` keeps
// compiling in the Linux CI job. It returns an error instead of a stub that
// pretends: a firewall that says it purged without purging makes the
// quarantine unverifiable.

import (
	"fmt"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/core/port"
)

// Quarantine stays zero, which is what `validate` rejects with its own
// message. Correct: there is no quarantine to write here, and picking one of
// the two lists would be pretending there is.
const Quarantine domain.QuarantineSystem = 0

func NewFirewall(string, port.Logger, port.ExposureAudit) (
	port.FirewallPort, port.ExposureAudit, func() error, error) {

	return nil, nil, nil, fmt.Errorf("el firewall de Kanpachi son las reglas del Firewall " +
		"de Windows y una sesión de WFP, así que este binario solo sirve en Windows y en Linux")
}
