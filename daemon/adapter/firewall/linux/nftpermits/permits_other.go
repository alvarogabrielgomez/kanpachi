//go:build !linux

package nftpermits

// La contraparte que existe para que `go build ./...` siga funcionando en
// Windows.
//
// Sin ella el paquete se queda sin ningún fichero fuera de Linux, y eso no es
// "no se compila": es un error de compilación con el mensaje `build constraints
// exclude all Go files`, que rompe el árbol entero. La carpeta dice de quién es
// el código y el compilador sigue leyendo la etiqueta.
//
// Falla en lo que escribe y contesta en lo que solo lee. La asimetría es la
// misma que en el fichero de verdad: `RestoreForeign` no falla porque su
// contrato ya es cierto donde nunca se suspendió nada.

import (
	"context"
	"errors"

	"github.com/accentiostudios/kanpachi/core/domain"
)

const (
	QuarantineTable = "kanpachi-base"
	QuarantineFile  = "/etc/kanpachi/quarantine.nft"
)

type Logger interface {
	Info(msg string, kv ...any)
	Warn(msg string, kv ...any)
	Error(msg string, kv ...any)
}

type Permits struct{}

var errSoloLinux = errors.New("los permisos de nftables solo corren en Linux")

var ErrNoSuspend = errSoloLinux

func New(string, Logger) (*Permits, error) { return nil, errSoloLinux }

func (*Permits) Close() error      { return nil }
func (*Permits) SetAdapter(string) {}

func (*Permits) Apply(context.Context, domain.RuleSet) error { return errSoloLinux }
func (*Permits) PurgeOwned(context.Context) error            { return errSoloLinux }

func (*Permits) ApplyBaseQuarantine(context.Context, []domain.QuarantineRule) error {
	return errSoloLinux
}

func (*Permits) AuditForeign(context.Context, domain.GameProfile) ([]domain.ForeignRule, error) {
	return nil, errSoloLinux
}

func (*Permits) SuspendForeign(context.Context, []domain.ForeignRule) error { return errSoloLinux }
func (*Permits) RestoreForeign(context.Context) error                       { return nil }

func (*Permits) InboundBlocked(context.Context) ([]domain.FirewallBlock, error) {
	return nil, errSoloLinux
}
func (*Permits) AllowAdapters(context.Context, []domain.FirewallBlock) error { return errSoloLinux }

// WithdrawAdapters does not fail, for the same reason RestoreForeign does not:
// where nothing was ever opened, "no debts" is already true.
func (*Permits) WithdrawAdapters(context.Context) error { return nil }

func (*Permits) FirewallEnabled(context.Context) ([]domain.FirewallProfileState, error) {
	return nil, errSoloLinux
}

func (*Permits) Enforcement(context.Context) (domain.Enforcement, error) {
	return domain.Enforcement{}, errSoloLinux
}

func (*Permits) FileOnDisk() (bool, error) { return false, errSoloLinux }

func QuarantineLoaded(context.Context) (bool, error) { return false, errSoloLinux }
