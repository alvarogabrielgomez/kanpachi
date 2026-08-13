//go:build !linux

package cli

// matchOwner does nothing off Linux. The seed only installs on systemd, and
// this file exists so the package still builds on the machine where it is
// written and tested. See owner_linux.go for what it does where it matters.
func matchOwner(path, dir string) error { return nil }
