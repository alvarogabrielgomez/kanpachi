// Package identity keeps the long-lived key of this installation.
//
// # Why it is a package of its own instead of a helper inside the client
//
// Because the key has TWO consumers and only one of them exists today. The
// registry client signs the room card with it, and [port.ControlChannel]'s
// `RequestCredential` promises to fill the public key from `identity.key`
// before signing, which is decision 25.
//
// A second loader written next to that second consumer is the exact way this
// breaks: two writers, one of them regenerating on a read error, and the pins
// the registry holds for 21 days die in silence along with the fingerprint
// everyone who has played with this host already knows. So this package
// CREATES, and every consumer, present or future, consumes.
package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// IdentityFile is the name on disk.
//
// Exported for the same reason the other adapters export theirs: so that
// `docs/03-arquitectura.md` and the actual directory listing cannot drift apart
// without someone noticing.
const IdentityFile = "identity.key"

// Protector puts the file's own ACL on it.
//
// It is injected because it is the only Windows-specific thing this package
// needs, and injecting it keeps the package pure and testable on Linux CI,
// where the whole decision of WHEN to call it is what actually matters.
//
// `docs/03-arquitectura.md` makes this file the one exception in ProgramData:
// SYSTEM and Administrators only, no read for users. A file mode of 0600 does
// nothing about that on Windows, where the directory ACL is what governs, and
// that directory grants read to every user of the machine.
type Protector func(path string) error

// LoadOrCreate returns the installation key, creating it on first use.
//
// # A key that is there and unreadable is an ERROR, never a regeneration
//
// Regenerating would rotate this installation's identity without anyone asking
// for it. Concretely: every invite ID this host has pinned in the registry
// becomes unreclaimable for the remaining stretch of its 21 days, and once
// decision 25 lands in full, the host loses the face that everyone who has
// already played with them recognises. A corrupt file is a thing to look at,
// not a thing to paper over.
//
// The seed is stored raw, 32 bytes and nothing else: no container, no header,
// no encoding. There is no parser to get wrong.
func LoadOrCreate(dir string, protect Protector) (ed25519.PrivateKey, error) {
	if dir == "" {
		return nil, errors.New("no hay directorio de datos donde guardar la llave de esta instalación")
	}
	// Demanded rather than defaulted: a nil protector would write the key with
	// whatever ACL the directory hands down, which is readable by every user of
	// the machine. Making it a parameter means nobody can forget it by omission.
	if protect == nil {
		return nil, errors.New("no se puede escribir la llave de esta instalación sin quien le ponga sus permisos")
	}

	ruta := filepath.Join(dir, IdentityFile)
	seed, err := os.ReadFile(ruta)
	switch {
	case err == nil:
		if len(seed) != ed25519.SeedSize {
			return nil, fmt.Errorf("la llave de esta instalación, %s, mide %d bytes y tiene que medir %d.\n"+
				"  No se regenera sola a propósito: una llave nueva perdería las salas que este equipo "+
				"tiene reservadas en el registro, y la cara con la que lo conocen quienes ya jugaron con él",
				ruta, len(seed), ed25519.SeedSize)
		}
		return ed25519.NewKeyFromSeed(seed), nil
	case !os.IsNotExist(err):
		return nil, fmt.Errorf("leyendo la llave de esta instalación: %w", err)
	}

	seed = make([]byte, ed25519.SeedSize)
	if _, err := rand.Read(seed); err != nil {
		return nil, fmt.Errorf("generando la llave de esta instalación: %w", err)
	}
	if err := write(ruta, seed, protect); err != nil {
		return nil, err
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

// write puts the seed on disk with no window in which it is readable.
//
// # The order, and why each step is where it is
//
// This does NOT use `safewrite.File`, and the difference is the whole point.
// That helper writes the bytes first and fixes the permissions afterwards,
// which is right for a room file and wrong for this one: between the write and
// the chmod the seed sits on disk inheriting the directory's ACL, and this
// directory grants read to every user of the machine. For the one file whose
// theft IS impersonation, a window measured in microseconds is still a window.
//
// So: create the temporary EMPTY, put its ACL on while there is nothing inside
// it, and only then write. The ACL travels with the rename, so the final name
// never exists unprotected either.
func write(ruta string, seed []byte, protect Protector) error {
	dir := filepath.Dir(ruta)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creando %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, IdentityFile+".tmp-*")
	if err != nil {
		return fmt.Errorf("creando el temporal de la llave: %w", err)
	}
	nombre := tmp.Name()
	limpiar := func(causa error) error {
		tmp.Close()
		// The removal is not checked: there is already an error to report, and
		// chaining this one would hide the real cause behind a symptom.
		_ = os.Remove(nombre)
		return causa
	}

	// While the file is still zero bytes long.
	if err := protect(nombre); err != nil {
		return limpiar(fmt.Errorf("poniéndole permisos propios a la llave de esta instalación: %w", err))
	}
	if _, err := tmp.Write(seed); err != nil {
		return limpiar(fmt.Errorf("escribiendo la llave de esta instalación: %w", err))
	}
	if err := tmp.Sync(); err != nil {
		return limpiar(fmt.Errorf("forzando la llave a disco: %w", err))
	}
	// Does nothing about the Windows ACL, which `protect` already handled, and
	// is what governs on Linux, where the tests and a headless host run.
	if err := tmp.Chmod(0o600); err != nil {
		return limpiar(fmt.Errorf("ajustando permisos de la llave: %w", err))
	}
	if err := tmp.Close(); err != nil {
		return limpiar(fmt.Errorf("cerrando el temporal de la llave: %w", err))
	}
	if err := os.Rename(nombre, ruta); err != nil {
		_ = os.Remove(nombre)
		return fmt.Errorf("dejando la llave en %s: %w", ruta, err)
	}
	return nil
}
