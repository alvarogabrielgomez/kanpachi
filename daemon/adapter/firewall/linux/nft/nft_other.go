//go:build !linux

package nft

// La contraparte que existe para que `go build ./...` siga funcionando en
// Windows.
//
// La carpeta dice que esto es de Linux y el compilador no la lee: para Go,
// `linux/nft` es un paquete más y entraría a compilarlo igual, tropezando con
// `golang.org/x/sys/unix` que arrastra `github.com/google/nftables`. La etiqueta
// es lo que de verdad separa, y este fichero es lo que hace que la separación no
// rompa la compilación del otro lado.
//
// Falla en todo, y eso es lo correcto: una compuerta que dijera que sí sin poner
// nada deja la sala descubierta con la pantalla en verde.

import (
	"context"
	"errors"
	"net"

	"github.com/accentiostudios/kanpachi/daemon/adapter/firewall/gate"
)

const (
	TableName = "kanpachi"
	ChainName = "gate"
)

type Logger interface {
	Info(msg string, kv ...any)
	Warn(msg string, kv ...any)
	Error(msg string, kv ...any)
}

type Gate struct{}

var errSoloLinux = errors.New("la compuerta de nftables solo corre en Linux")

func Open(Logger) (*Gate, error) { return nil, errSoloLinux }

func (*Gate) Close() error { return nil }

func (*Gate) Apply(context.Context, []gate.Spec) error { return errSoloLinux }

func (*Gate) Purge(context.Context) error { return errSoloLinux }

func (*Gate) Measure(context.Context, []gate.Spec) (gate.Measurement, error) {
	return gate.Measurement{}, errSoloLinux
}

// IfaceOf sí funciona en cualquier sistema: `net.InterfaceByName` es de la
// biblioteca estándar. Se deja idéntico para que el cableado no tenga que
// distinguir, y porque un resolver que fallara acá escondería el error de verdad
// detrás de "esto es solo Linux".
func IfaceOf(name string) (uint64, error) {
	if name == "" {
		return 0, errors.New("la compuerta necesita el NOMBRE del adaptador, y llegó vacío")
	}
	i, err := net.InterfaceByName(name)
	if err != nil {
		return 0, err
	}
	if i.Index <= 0 {
		return 0, errors.New("el adaptador dio índice cero, y el cero no acota nada")
	}
	return uint64(i.Index), nil
}
