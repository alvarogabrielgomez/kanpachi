//go:build linux

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

// aptInstall hands the package to apt.
//
// # The three environment variables are not decoration
//
// `DEBIAN_FRONTEND=noninteractive` stops dpkg from opening a full-screen dialog
// asking about a configuration file. That dialog, inside `kanpachi upgrade`,
// leaves the terminal waiting for a key whoever typed the command does not
// expect to have to press, and over `ssh` with the session half gone it turns
// into a half-configured package.
//
// The two dpkg ones answer in advance the question that does matter: what to do
// with a file in `/etc` the operator edited. `confold` keeps theirs, which is
// right for `/etc/kanpachi/quarantine.nft`: the daemon writes that file with THIS
// machine's quarantine, and letting the package write over it would mean changing
// which ports are closed with nobody asking for it.
//
// The output goes to the terminal live, uncaptured: apt takes a while, and a
// command that goes quiet for half a minute looks hung. Besides, when it fails,
// what has to be read is what apt says.
//
// # And the fourth option turns off a sandbox that protects nothing here
//
// Without it, every `kanpachi upgrade` on Linux ends with this paragraph stuck
// to the end, measured on the droplet on 2026-08-18 over 0.6.0:
//
//	N: Download is performed unsandboxed as root as file
//	'/var/lib/kanpachi/kanpachi-amd64.deb' couldn't be accessed by user '_apt'
//
// It is true and it is not a failure. The package gets written into the state
// directory, which is 0700 root on purpose, since anybody writes in `/tmp` and
// this gets installed as root a second later, so the `_apt` user apt downloads
// things as cannot read it, and apt gives up its sandbox and says so.
//
// What needs fixing is not the permission, it is the surprise: nothing is
// DOWNLOADED here. The file is already on disk and its SHA-256 has already been
// checked against the release manifest, so the download sandbox has nothing to
// isolate. Saying that up front is the difference between a written decision and
// a warning that shows up every time and that somebody is going to have to go
// investigate.
//
// # And `reinstall` exists because apt compares numbers, not content
//
// `apt-get install` over a package whose version is already in place answers
// «kanpachi is already the newest version» and does nothing, even when the file
// it was handed is different bytes. It really happens: a version republished over
// a fix carries the same number as the one already installed. `--reinstall` is
// apt's way of saying «that package, again», and it only gets passed when whoever
// typed the command asked for `--force`.
func aptInstall(ctx context.Context, path string, reinstall bool) error {
	env := append(os.Environ(),
		"DEBIAN_FRONTEND=noninteractive",
		"DPKG_FORCE=confold",
	)
	args := []string{"install", "-y",
		"-o", "Dpkg::Options::=--force-confold",
		"-o", "APT::Sandbox::User=root"}
	if reinstall {
		args = append(args, "--reinstall")
	}
	return runVisible(ctx, env, "apt-get", append(args, path)...)
}

// runVisible runs a command letting it talk through the terminal.
//
// It is the opposite of [runCmd], which captures the output to put it inside an
// error, and both shapes are justified by how long they take: the `doctor` fixes
// are instant and what matters about them is the final message; an install with
// apt takes a while, and watching it move is the difference between waiting and
// thinking it hung.
func runVisible(ctx context.Context, env []string, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s failed: %w", name, err)
	}
	return nil
}
