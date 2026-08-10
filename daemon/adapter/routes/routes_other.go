//go:build !windows && !linux

package routes

import (
	"context"
	"errors"
	"net/netip"

	"github.com/accentiostudios/kanpachi/core/port"
)

// Kanpachi runs on Windows and on Linux. This file exists so the rest of the
// package keeps compiling anywhere else, and `routes.go`, which is where the
// filtering that decides whether a room can be opened at all lives, does not
// depend on any one system to be read.
type Table struct{}

func New() *Table { return &Table{} }

var _ port.RoutingTable = (*Table)(nil)

// LocalPrefixes fails rather than returning an empty list, and the difference
// is not cosmetic. With no local prefixes the address planner concludes that
// NOTHING collides with the user's home network and picks a range that does.
func (*Table) LocalPrefixes(context.Context) ([]netip.Prefix, error) {
	return nil, errors.New("la tabla de rutas de Kanpachi se lee en Windows y en Linux, " +
		"y este binario no es de ninguno de los dos")
}
