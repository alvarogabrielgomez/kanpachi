//go:build linux

package main

// The Linux checks, which are the ones that really matter: here the product is a
// service on a server and nobody is looking at a window.

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/accentiostudios/kanpachi/daemon/adapter/firewall/linux/nftpermits"
	"github.com/accentiostudios/kanpachi/daemon/transport/pipe"
)

// engineLinux is where the package puts the engine.
//
// Repeated by hand and not imported from the daemon's wiring, which is `package
// main` and nobody imports. It is the same limitation that led to `daemon/paths`,
// and this path did not move down there because ONE thing looks at it: the daemon
// builds it from its own binary's directory, so there are not two places that can
// drift apart, there is this one and a construction.
const engineLinux = "/usr/libexec/kanpachi/kanpachi-engine"

func systemChecks() []check {
	return []check{
		tunCheck(),
		kernelCheck(),
		unitCheck("kanpachid", "the service"),
		quarantineCheck(),
		channelDirCheck(),
		channelCheck(),
		engineCheck(engineLinux),
		foreignFirewallCheck(),
	}
}

func elevationHint() string { return "Try sudo: the channel and the token belong to root." }

// ─── /dev/net/tun ────────────────────────────────────────────────────────────

// tunMajor and tunMinor are the numbers Linux assigns to the TUN node.
//
// They get checked as well as the file existing because a node with the wrong
// numbers exists, opens, and fails when it gets configured, which is much harder
// to read than "it is not there".
const (
	tunPath  = "/dev/net/tun"
	tunMajor = 10
	tunMinor = 200
)

func tunCheck() check {
	return check{
		name: "/dev/net/tun",
		look: func(context.Context, options) verdict {
			info, err := os.Stat(tunPath)
			if os.IsNotExist(err) {
				return bad("it is missing, and without it there is no virtual network").
					withFix("modprobe tun\n" +
						"mknod /dev/net/tun c 10 200 && chmod 0666 /dev/net/tun")
			}
			if err != nil {
				return unknown("could not look at it: %v", err)
			}
			if info.Mode()&os.ModeCharDevice == 0 {
				return bad("it exists and is not a character device").
					withFix("rm /dev/net/tun && mknod /dev/net/tun c 10 200")
			}
			st, ready := info.Sys().(*syscall.Stat_t)
			if !ready {
				return unknown("its numbers could not be read")
			}
			ma, mi := majorMinor(uint64(st.Rdev))
			if ma != tunMajor || mi != tunMinor {
				return bad("it is %d/%d and has to be %d/%d", ma, mi, tunMajor, tunMinor).
					withFix("rm /dev/net/tun && mknod /dev/net/tun c 10 200")
			}
			return good("%d/%d, as it has to be", ma, mi)
		},
		fix: func(ctx context.Context, _ options) error {
			// `modprobe` first: in most cases the node is missing because the
			// module is not loaded, and there the kernel creates it itself with
			// the right numbers. Creating it by hand first covers up the cause.
			_ = runCmd(ctx, "modprobe", "tun")
			if _, err := os.Stat(tunPath); err == nil {
				return nil
			}
			if err := os.MkdirAll("/dev/net", 0o755); err != nil {
				return err
			}
			return runCmd(ctx, "mknod", tunPath, "c", "10", "200")
		},
	}
}

// majorMinor splits an rdev into its two numbers, with Linux's encoding.
//
// It is not just a shift: the minor's bits sit in TWO pieces, and a naive
// version (`rdev>>8`, `rdev&0xff`) gets `10/200` right by luck and fails as soon
// as the minor goes past 255.
func majorMinor(rdev uint64) (uint32, uint32) {
	major := uint32((rdev >> 8) & 0xfff)
	minor := uint32(rdev&0xff) | uint32((rdev>>12)&^uint64(0xff))
	return major, minor
}

// ─── The kernel ──────────────────────────────────────────────────────────────

// kernelCheck looks at whether the kernel brings what is needed.
//
// **It does not get fixed**, and not out of caution: changing the kernel's
// configuration means recompiling it or changing kernels, which is the
// operator's machine and not ours. Its absence also shows up somewhere else
// first, so this mostly serves to put a name on a failure that would otherwise
// look like Kanpachi's.
func kernelCheck() check {
	return check{
		name: "the kernel",
		look: func(context.Context, options) verdict {
			cfg, where, err := kernelConfig()
			if err != nil {
				// The configuration being absent is normal on many distributions,
				// and it is not a failure: it says it is not known, which is the
				// truth.
				return unknown("could not read the kernel configuration: %v", err)
			}
			missing := []string{}
			for _, option := range []string{"CONFIG_TUN", "CONFIG_NF_TABLES", "CONFIG_NF_TABLES_INET"} {
				if !hasOption(cfg, option) {
					missing = append(missing, option)
				}
			}
			if len(missing) > 0 {
				return bad("it is missing %s (according to %s)", strings.Join(missing, ", "), where).
					withFix("This is the kernel configuration, so Kanpachi does not touch it.")
			}
			return good("TUN and nftables, according to %s", where)
		},
	}
}

// kernelConfig returns whichever kernel configuration is at hand.
//
// The two usual places, in the order they tend to exist: the file the kernel's
// package leaves in `/boot`, and the one the kernel itself exposes compressed
// when it was built with `CONFIG_IKCONFIG_PROC`.
func kernelConfig() (string, string, error) {
	var uts syscall.Utsname
	if err := syscall.Uname(&uts); err == nil {
		release := toString(uts.Release[:])
		path := "/boot/config-" + release
		if b, err := os.ReadFile(path); err == nil {
			return string(b), path, nil
		}
	}

	f, err := os.Open("/proc/config.gz")
	if err != nil {
		return "", "", fmt.Errorf("neither /boot/config-<version> nor /proc/config.gz")
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", "", err
	}
	defer func() { _ = gz.Close() }()
	b, err := io.ReadAll(gz)
	if err != nil {
		return "", "", err
	}
	return string(b), "/proc/config.gz", nil
}

// hasOption accepts `=y` and `=m`: built in or as a module, both count.
func hasOption(cfg, name string) bool {
	return strings.Contains(cfg, "\n"+name+"=y") ||
		strings.Contains(cfg, "\n"+name+"=m") ||
		strings.HasPrefix(cfg, name+"=")
}

func toString(b []int8) string {
	var sb strings.Builder
	for _, c := range b {
		if c == 0 {
			break
		}
		sb.WriteByte(byte(c))
	}
	return sb.String()
}

// ─── The units ───────────────────────────────────────────────────────────────

func unitCheck(unit, name string) check {
	return check{
		name: name,
		look: func(ctx context.Context, _ options) verdict {
			active := outputOf(ctx, "systemctl", "is-active", unit)
			enabled := outputOf(ctx, "systemctl", "is-enabled", unit)
			switch active {
			case "active":
				if enabled != "enabled" {
					// Running and not starting on its own: it works today and not
					// tomorrow. It is a warning and not a failure, because it may
					// be what the operator wanted.
					return warn("running, and does NOT start with the machine (%s)", enabled)
				}
				return good("running, and starts with the machine")
			case "":
				return unknown("there is no systemctl to ask")
			default:
				return bad("%s (%s)", active, enabled).
					withFix("systemctl enable --now " + unit)
			}
		},
		fix: func(ctx context.Context, _ options) error {
			return runCmd(ctx, "systemctl", "enable", "--now", unit)
		},
	}
}

// quarantineCheck is THE important check, and the least visible one.
//
// # Why looking at the unit is not enough
//
// Because what protects the machine is the TABLE loaded in the kernel, and the
// two really do come apart: the unit is `oneshot` with `RemainAfterExit`, so it
// shows as active from the moment `nft -f` finished once, and a later `nft flush
// ruleset` takes the table away without touching the unit's state. There
// `systemctl status` would say everything is fine with the game's ports open to
// the internet, and nothing else on the system would say otherwise.
func quarantineCheck() check {
	return check{
		name: "the base quarantine",
		look: func(ctx context.Context, _ options) verdict {
			if _, err := os.Stat(nftpermits.QuarantineFile); os.IsNotExist(err) {
				// With no file nothing is broken: the quarantine is the user's
				// DECISION now that it stopped applying itself, and the file only
				// exists together with the decision. It says where that gets
				// decided.
				return warn("not in place. It is this machine's decision now: " +
					"`kanpachi quarantine on` closes file sharing and Remote Desktop " +
					"INTO this machine on every network (recommended)")
			}
			loaded, err := nftpermits.QuarantineLoaded(ctx)
			if err != nil {
				return unknown("could not read the ruleset: %v", err)
			}
			if !loaded {
				return bad("the file is there and the table is NOT loaded: " +
					"the game ports are open from the internet").
					withFix("systemctl restart kanpachi-quarantine")
			}
			return good("table inet %s loaded", nftpermits.QuarantineTable)
		},
		fix: func(ctx context.Context, _ options) error {
			return runCmd(ctx, "systemctl", "restart", "kanpachi-quarantine")
		},
	}
}

// ─── The channel ─────────────────────────────────────────────────────────────

// channelDirCheck looks at what actually protects the socket.
//
// The directory and not the socket, because the directory is the analogue of
// Windows's `ProtectedPrefix`: with no permission to enter, nobody gets to look
// at the socket whatever state it is in, and with permission to enter the
// socket's mode is all that is left. Both get looked at, starting with the one
// in charge.
func channelDirCheck() check {
	dir := "/run/kanpachi"
	return check{
		name: "the channel permissions",
		look: func(context.Context, options) verdict {
			info, err := os.Lstat(dir)
			if os.IsNotExist(err) {
				return warn("%s does not exist yet: the daemon creates it on start", dir)
			}
			if err != nil {
				return unknown("could not look at %s: %v", dir, err)
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return bad("%s is a symlink, so its permissions mean "+
					"nothing", dir)
			}
			if p := info.Mode().Perm(); p&0o077 != 0 {
				return bad("%s is %04o and lets the group or others in", dir, p).
					withFix("chmod 0700 " + dir)
			}
			// The socket only gets looked at if the directory is already right:
			// with the directory open, a socket at 0600 protects nothing, and
			// naming both failures at once hides which one is in charge.
			if s, err := os.Stat(pipe.Name); err == nil {
				if p := s.Mode().Perm(); p&0o077 != 0 {
					return bad("%s is %04o", pipe.Name, p).
						withFix("chmod 0600 " + pipe.Name)
				}
			}
			return good("%s at 0700, socket at 0600", dir)
		},
		fix: func(_ context.Context, _ options) error {
			if err := os.Chmod(dir, 0o700); err != nil {
				return err
			}
			if _, err := os.Stat(pipe.Name); err == nil {
				return os.Chmod(pipe.Name, 0o600)
			}
			return nil
		},
	}
}

// ─── The operator's things, looked at and almost never touched ───────────────

// foreignFirewallCheck looks for what may be closing things above us.
//
// # This check holds the ONLY exception to "what is not ours does not get fixed"
//
// A manager that is going to swallow the inbound of the room's adapters is BAD,
// with a fix: `--fix` runs exactly the commands the verdict names, down the same
// recorded path the `kanpachi host` question uses, and what gets opened gets
// closed when the room ends or on the next start. Typing `--fix` after reading
// the verdict IS the consent, as explicit as answering the question. See
// decision 36.
//
// The rest carries on as always: an active manager that is not blocking today,
// or a Docker chain, gets named and left alone. Netfilter still does not let us
// open above somebody else's `drop` from our own table; opening means asking the
// manager's CLI for it, never writing over it.
func foreignFirewallCheck() check {
	return check{
		name: "firewalls that are not ours",
		look: func(ctx context.Context, _ options) verdict {
			blocks, err := nftpermits.InboundBlocks(ctx)
			if err != nil {
				return unknown("could not read their posture: %v", err)
			}
			if len(blocks) > 0 {
				parts := make([]string, 0, len(blocks))
				var fixes []string
				for _, b := range blocks {
					parts = append(parts, b.String())
					fixes = append(fixes, b.Fix...)
				}
				return bad("%s. A room would assemble and nobody would get in",
					strings.Join(parts, "; ")).
					withFix("By hand: " + strings.Join(fixes, " && ") + "\n" +
						"`kanpachi doctor --fix` runs exactly that, writes it down, and it is\n" +
						"undone when the room closes or on the next service start.")
			}

			found := []string{}
			if strings.Contains(outputOf(ctx, "ufw", "status"), "Status: active") {
				found = append(found, "ufw")
			}
			if outputOf(ctx, "systemctl", "is-active", "firewalld") == "active" {
				found = append(found, "firewalld")
			}
			if strings.Contains(outputOf(ctx, "nft", "list", "ruleset"), "DOCKER-USER") ||
				strings.Contains(outputOf(ctx, "iptables", "-S"), "DOCKER-USER") {
				found = append(found, "Docker")
			}
			if len(found) == 0 {
				return good("none is active")
			}
			return warn("there is %s, not blocking the room today. Its policy can change",
				strings.Join(found, ", ")).
				withFix("Look yourself: ufw status verbose / firewall-cmd --list-all / nft list ruleset\n" +
					"Kanpachi only ever touches these to let its own two adapters in, with consent.")
		},
		fix: func(ctx context.Context, _ options) error {
			blocks, err := nftpermits.InboundBlocks(ctx)
			if err != nil {
				return err
			}
			return nftpermits.AllowBlocked(ctx, blocks, doctorLog{})
		},
	}
}

// doctorLog is the sliver of logger AllowBlocked wants. The doctor's answer is
// the re-measured verdict, so the daemon-style log lines go nowhere on purpose:
// printing them twice would say more than what was measured.
type doctorLog struct{}

func (doctorLog) Info(string, ...any)  {}
func (doctorLog) Warn(string, ...any)  {}
func (doctorLog) Error(string, ...any) {}

// ─── Running things ──────────────────────────────────────────────────────────

// outputOf runs a command and returns its output with the spaces trimmed, or
// empty.
//
// **It swallows the error on purpose**, and it is the one place in the
// repository where that is right: here the failure IS the answer. `systemctl
// is-active` exits non-zero exactly when the unit is not active, and `ufw
// status` fails when ufw is not installed, which is the good answer. The caller
// tells them apart by the text, not by the code.
func outputOf(ctx context.Context, name string, args ...string) string {
	out, _ := exec.CommandContext(ctx, name, args...).Output()
	return strings.TrimSpace(string(out))
}

// runCmd runs a fix and returns what it said if it failed.
//
// The command's output travels INSIDE the error: `systemctl` explains why it
// could not, and keeping only "exit status 1" throws away exactly the part that
// helps.
func runCmd(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(out))
		if text == "" {
			return fmt.Errorf("%s: %w", name, err)
		}
		return fmt.Errorf("%s: %w: %s", name, err, text)
	}
	return nil
}

// enginePath is the same path the doctor looks at, for `kanpachi version`.
func enginePath() string { return engineLinux }
