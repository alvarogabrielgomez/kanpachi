// Package engineid reads which engine a FILE is, without executing it.
//
// The engine seals two sentinels into its binary at compile time — see
// src/build_id.rs in its repository: KANPACHI-ENGINE-BUILD-ID{...} for the
// engine code and KANPACHI-ENGINE-LIB{...} for the network stack inside it.
// Scanning for them is what lets `kanpachi version`, `doctor` and the daemon
// answer "which engine is this" on Linux, where a bare executable has no
// VERSIONINFO to ask, and on Windows without depending on COM.
//
// The running PROCESS reports the same values itself, as `engine_build` and
// `engine_lib` in its diagnostics: the file on disk can have been replaced
// since it started, so the two answers are deliberately separate readings.
package engineid

// Reading which engine a FILE is, without executing it.
//
// The engine seals a `KANPACHI-ENGINE-BUILD-ID{<version>+<provenance>}`
// sentinel into its binary at compile time — see `src/build_id.rs` in the
// engine's repository. Scanning for it is what lets `kanpachi version` and
// `doctor` answer "which engine is this" on Linux, where a bare executable
// has no VERSIONINFO to ask, and on Windows without depending on COM.
//
// The running PROCESS reports the same value itself, as `engine_build` in its
// diagnostics: the file on disk can have been replaced since it started, so
// the two answers are deliberately separate readings.

import (
	"fmt"
	"io"
	"os"
	"regexp"
)

// The two sentinels, each bounded because this runs over untrusted bytes: a
// corrupt file with an opening brace and no closing one must not make the
// scanner swallow the rest of the binary. They answer different questions —
// BUILD-ID says which engine code, LIB says which network stack is inside it.
var (
	engineMarkRe = regexp.MustCompile(`KANPACHI-ENGINE-BUILD-ID\{([^{}]{1,64})\}`)
	engineLibRe  = regexp.MustCompile(`KANPACHI-ENGINE-LIB\{([^{}]{1,64})\}`)
)

// Identity is both sentinels read off one pass over the file.
type Identity struct {
	// Build is `0.1.0+g9486f08cd21a`, or empty on an engine older than the
	// sentinel — a normal find, never an error.
	Build string
	// Lib is `easytier@v2.6.4-kanpachi.1`, with the same absence rule.
	Lib string
}

// Scan reads the file for the engine sentinels.
//
// Zero values with nil error mean the file exists and carries no sentinel,
// which is every engine built before the mark existed: the caller says
// "unknown" rather than failing, because an old engine is a normal find.
func Scan(path string) (Identity, error) {
	f, err := os.Open(path)
	if err != nil {
		return Identity{}, err
	}
	defer func() { _ = f.Close() }()

	// Chunked, with an overlap so a sentinel split across two reads is still
	// seen. The binary is ~30 MB and the marks sit wherever the linker put
	// them, so reading it whole would be the simpler wrong: this is called by
	// `doctor`, which people run precisely when a machine is misbehaving.
	const chunk = 4 << 20
	const overlap = 128
	buf := make([]byte, chunk+overlap)
	carry := 0
	var id Identity
	for {
		n, err := f.Read(buf[carry:])
		if n > 0 {
			window := buf[:carry+n]
			if id.Build == "" {
				if m := engineMarkRe.FindSubmatch(window); m != nil {
					id.Build = string(m[1])
				}
			}
			if id.Lib == "" {
				if m := engineLibRe.FindSubmatch(window); m != nil {
					id.Lib = string(m[1])
				}
			}
			if id.Build != "" && id.Lib != "" {
				return id, nil
			}
			keep := min(carry+n, overlap)
			copy(buf, buf[carry+n-keep:carry+n])
			carry = keep
		}
		if err == io.EOF {
			return id, nil
		}
		if err != nil {
			return Identity{}, fmt.Errorf("reading %s: %w", path, err)
		}
	}
}

// String is the human form: `0.1.0+g9486f08cd21a (easytier@v2.6.4-kanpachi.1)`.
func (id Identity) String() string {
	switch {
	case id.Build == "" && id.Lib == "":
		return ""
	case id.Lib == "":
		return id.Build
	case id.Build == "":
		return "(" + id.Lib + ")"
	default:
		return id.Build + " (" + id.Lib + ")"
	}
}
