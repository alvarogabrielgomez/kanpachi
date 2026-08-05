package netcfg

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/accentiostudios/kanpachi/daemon/adapter/safewrite"
)

// TweakFile is where the book lives, next to the firewall's own.
const TweakFile = "applied-tweaks.json"

// tweaks is the book of everything netcfg changed that OUTLIVES the process.
//
// # Why a book, and why only these two
//
// Kanpachi's rule is that every persistent change either carries a label the
// system can enumerate, or is written down here with what was there before.
// Anything that meets neither is not written at all.
//
// Most of what netcfg does needs no book, because it dies on its own: the
// adapter, its address, its metric, its MTU and its routes all belong to a
// virtual adapter that the engine creates per room and that disappears with it.
// Measured: after killing the engine no adapter is left.
//
// These two are the exception. They are machine-wide settings that survive a
// reboot, so a dirty death would leave the user with IPv4 forced for good and
// an optional Windows feature installed, with nothing anywhere saying why.
//
// # Why the PREVIOUS value and not what was done
//
// Because "we enabled DirectPlay" does not say whether to disable it later. If
// the user already had it on, turning it off on the way out would break
// something of theirs that Kanpachi never owned.
type tweaks struct {
	// DirectPlayWas is the state before Kanpachi touched it. Nil means never
	// touched, which is what a fresh install and every room without an old
	// game look like.
	DirectPlayWas *bool `json:"direct_play_was,omitempty"`
	// PrefixPolicyAdded is whether Kanpachi added the RFC 6724 row. This one is
	// a presence and not a value: the row is either in the table or it is not.
	PrefixPolicyAdded bool `json:"prefix_policy_added,omitempty"`
}

// empty reports whether there is nothing left to undo.
//
// It decides whether the file is deleted rather than written with an empty
// object. A file that exists says "something is pending" to the next start, and
// saying it falsely costs a revert of things nobody changed.
func (t tweaks) empty() bool {
	return t.DirectPlayWas == nil && !t.PrefixPolicyAdded
}

// book reads and writes the tweak file.
type book struct{ path string }

func newBook(dataDir string) book {
	return book{path: filepath.Join(dataDir, TweakFile)}
}

// load reads the book. A missing file is the NORMAL case and not an error: it
// is what a fresh install and every clean exit look like.
func (b book) load() (tweaks, error) {
	raw, err := os.ReadFile(b.path)
	if os.IsNotExist(err) {
		return tweaks{}, nil
	}
	if err != nil {
		return tweaks{}, fmt.Errorf("leyendo %s: %w", b.path, err)
	}

	var t tweaks
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&t); err != nil {
		// A corrupt book is reported and NOT treated as empty. Treating it as
		// empty would silently drop the only record of what has to be undone,
		// and the caller decides whether that is fatal.
		return tweaks{}, fmt.Errorf("el libro de ajustes %s no se pudo interpretar: %w", b.path, err)
	}
	return t, nil
}

// save writes the book, or removes it when there is nothing left to undo.
func (b book) save(t tweaks) error {
	if t.empty() {
		if err := os.Remove(b.path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("borrando %s: %w", b.path, err)
		}
		return nil
	}

	raw, err := json.Marshal(t)
	if err != nil {
		return fmt.Errorf("serializando el libro de ajustes: %w", err)
	}
	// Same atomic write as the firewall's suspended rules, and for the same
	// reason: a half-written book in ProgramData is worse than none, because it
	// looks valid.
	return safewrite.File(b.path, raw, 0o600)
}
