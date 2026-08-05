//go:build !windows

package routes

import (
	"context"
	"errors"
	"net/netip"

	"github.com/accentiostudios/kanpachi/core/port"
)

// Kanpachi's client is Windows only. This file exists so the rest of the
// package keeps compiling and being tested on the Linux CI job, which is where
// `routes.go` proves the filtering that decides whether a room can be opened at
// all.
type Table struct{}

func New() *Table { return &Table{} }

var _ port.RoutingTable = (*Table)(nil)

// LocalPrefixes fails rather than returning an empty list, and the difference
// is not cosmetic. With no local prefixes the address planner concludes that
// NOTHING collides with the user's home network and picks a range that does.
func (*Table) LocalPrefixes(context.Context) ([]netip.Prefix, error) {
	return nil, errors.New("la tabla de rutas de Kanpachi solo se lee en Windows")
}
