//go:build windows

package netcfg

import (
	"context"
	"os/exec"
	"syscall"
)

// silentCommand builds a child process that shows no window.
//
// # Why every spawn in this package must go through here
//
// The product promises the user never sees a console flash. Windows honours
// that only if it is asked: a console-subsystem program like `netsh.exe` or
// `dism.exe` allocates a console of its own unless the parent says otherwise,
// and `exec.Command` says nothing by default.
//
// The daemon runs as a service, so today that console would be allocated in
// session 0 where nobody can see it. That is a reason it has not bitten yet,
// not a reason to leave it: `--console` mode is how this repository is
// developed and there the flash is real, and a design that is only correct
// because of where it happens to run is one bad day from being wrong.
//
// `HideWindow` sets STARTF_USESHOWWINDOW with SW_HIDE, which is the flag Go's
// syscall package exposes and the same one the engine's own spawn already uses.
func silentCommand(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd
}
