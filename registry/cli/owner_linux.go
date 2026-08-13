//go:build linux

package cli

import (
	"fmt"
	"os"
	"syscall"
)

// matchOwner hands a file to whoever owns the directory it lives in.
//
// # Why it is needed at all
//
// `kanpseed password` runs as root and the registry runs as the ephemeral user
// systemd invents for it with DynamicUser=yes. A credential written with the
// default ownership is a 0600 file that root can read and the service cannot,
// so the seed would come up open with the operator convinced it was closed.
//
// systemd does fix the ownership of the state directory on every start, and
// leaning on that would mean a RESTART to change a password. Matching the owner
// here is what lets a SIGHUP be enough, which matters because a restart of this
// unit takes the engine down with it through the BindsTo.
func matchOwner(path, dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("no se pudo mirar %s: %w", dir, err)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}
	if err := os.Chown(path, int(st.Uid), int(st.Gid)); err != nil {
		return fmt.Errorf("no se pudo poner %s a nombre del uid %d: %w", path, st.Uid, err)
	}
	return nil
}
