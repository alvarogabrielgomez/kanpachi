//go:build windows

package main

// The Windows checks, which are FEWER than the Linux ones on purpose.
//
// # Why fewer, and why that is not a to-do
//
// Because on Windows the normal client is the window, and the window already
// shows most of this on its own screens: the state of the service, the exposure,
// Kanpachi Protection. Doctor exists here for the case where somebody is in a
// terminal, and what is needed there is the same as on Linux: knowing whether the
// channel answers and whether the pieces are where they have to be.
//
// What does NOT get replicated is the operating system's configuration, because
// on Windows those are not files anybody can look at and they have no fix that
// fits on one line. Inventing checks that answer "cannot tell" would fill the
// screen with noise and hide the ones that do answer.
//
// # What that reasoning claimed too much of, and got proved wrong
//
// It said that on Windows there is NO system level to look at, which is why
// there was nothing here equivalent to `/dev/net/tun`. There is: the virtual
// network driver, which gets installed into the driver store and can refuse to
// go in. Measured on 2026-08-11 on a guest's machine, with every file in place
// and no possible adapter. See `virtualAdapterCheck`.

import (
	"context"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"

	kanpachiengine "github.com/accentiostudios/kanpachi/daemon/adapter/engine/kanpachi"
	"github.com/accentiostudios/kanpachi/daemon/preflight"
)

func systemChecks() []check {
	return []check{
		serviceCheck(),
		dataDirCheck(),
		channelCheck(),
		measuredQuarantineCheck(),
		engineCheck(engineNextToDaemon()),
		virtualAdapterCheck(),
	}
}

// serviceCheck asks the same question roomprobe asks before running, from the
// same code, so the two can never disagree about what "running" means.
func serviceCheck() check {
	return check{
		name: "the service",
		look: func(context.Context, options) verdict {
			running, err := preflight.DaemonServiceRunning()
			if err != nil {
				return unknown("could not ask the service manager: %v", err)
			}
			if !running {
				return bad("%s is not running", preflight.DaemonService).
					withFix("Start-Service " + preflight.DaemonService)
			}
			return good("%s is running", preflight.DaemonService)
		},
	}
}

// virtualAdapterCheck is the Windows analogue of `/dev/net/tun`.
//
// It runs the SAME probe the daemon runs before bringing the engine up, and that
// is what makes it worth having: a check written separately would answer about
// some other, similar thing, and the one that matters is the one the product is
// really going to do.
//
// # Why it gives up without elevation instead of failing
//
// Because creating an adapter is a privileged operation, and without permission
// the probe returns access denied. That does NOT mean the machine is broken, it
// means this run cannot know. Doctor gets read when something is wrong, and a
// red line that only says you ran it as yourself would send people looking for a
// problem that does not exist. It is the one check in this binary that asks for
// elevation, which is why it says so instead of assuming it.
//
// **It has no fix**, and that is the usual rule: Windows's driver store belongs
// to the system and not to us. It gets named, what to look at gets said, and it
// stops there.
func virtualAdapterCheck() check {
	return check{
		name: "the virtual adapter",
		look: func(context.Context, options) verdict {
			if !windows.GetCurrentProcessToken().IsElevated() {
				return unknown("creating an adapter needs elevation, so this one " +
					"cannot be answered from here").
					withFix("Run this in an elevated terminal to have it checked.")
			}
			dir := filepath.Dir(engineNextToDaemon())
			if err := kanpachiengine.Preflight(dir); err != nil {
				return bad("this machine cannot build one").withFix(err.Error())
			}
			return good("built one and took it down again")
		},
	}
}

func elevationHint() string {
	return "On Windows any user of the machine can read the token,\n" +
		"so this should not need elevation. If it does, the ACL on\n" +
		"ProgramData\\Kanpachi is not the one the installer put there."
}

// engineNextToDaemon is where the engine lives in a Windows install.
//
// It gets looked for next to THIS binary because the package puts them together,
// which is the same thing the daemon does to launch it. On the portable build
// that is still true and on the installed one too, so there is no need to read
// the registry.
func engineNextToDaemon() string {
	exe, err := os.Executable()
	if err != nil {
		return "kanpachi-engine.exe"
	}
	return filepath.Join(filepath.Dir(exe), "kanpachi-engine.exe")
}

// dataDirCheck looks that it is there, and does NOT create it.
//
// Creating it would be the wrong fix: the installer creates it with an ACL of
// its own, and that ACL is half the protection of everything inside. A directory
// made here would come out with the inherited ACL and lose that in silence,
// which is worse than not having it. That is why this check carries no `fix`.
func dataDirCheck() check {
	return check{
		name: "the data directory",
		look: func(_ context.Context, op options) verdict {
			info, err := os.Stat(op.data)
			if os.IsNotExist(err) {
				return bad("%s is not there", op.data).
					withFix("The installer creates it, with an ACL of its own. Reinstalling puts it back.")
			}
			if err != nil {
				return unknown("could not look at %s: %v", op.data, err)
			}
			if !info.IsDir() {
				return bad("%s exists and is not a directory", op.data)
			}
			return good("%s", op.data)
		},
	}
}

// enginePath is the same path the doctor looks at, for `kanpachi version`.
func enginePath() string { return engineNextToDaemon() }
