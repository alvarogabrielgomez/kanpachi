//go:build !linux

package main

import (
	"context"
	"errors"
)

// aptInstall does not exist outside Linux, and nobody reaches it: what refuses
// first is [canUpgradeHere], which looks at `runtime.GOOS` and explains that on
// Windows the update goes through the installer.
//
// It gets declared anyway so `go build ./...` compiles on Windows, which is where
// development happens, and it returns an error instead of pretending: an
// `upgrade` that said it installed without installing is worse than one that does
// not exist.
func aptInstall(context.Context, string, bool) error {
	return errors.New("installing a .deb is a Linux thing")
}
