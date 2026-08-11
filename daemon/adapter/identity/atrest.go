package identity

import (
	"crypto/ed25519"
	"crypto/hkdf"
	"crypto/sha256"
	"fmt"
)

// StateKeyLen is the length of the key that seals what sits on disk.
const StateKeyLen = 32

// stateKeyInfo binds the derived key to ONE use, and the version in it is what
// lets that use change later without silently reading old bytes with a new
// meaning.
//
// HKDF's `info` is exactly the field for this: two keys derived from the same
// secret with different `info` are unrelated. So the key that seals the room
// file cannot open anything else derived from this identity, today or later.
const stateKeyInfo = "kanpachi/state-at-rest/v1"

// StateKey derives the key that seals the saved state, from this machine's
// identity.
//
// # Why derived and not stored
//
// Because a stored key is one more file to protect, and it would have to sit
// beside the thing it protects, which is no protection at all. The identity is
// already the one file this product defends hardest: on Windows it carries its
// own ACL and on Linux mode 0600, and losing it already means this machine
// stops being itself.
//
// # What this does and does not buy
//
// It does NOT protect against somebody who can read `identity.key`. Whoever has
// that can derive this and open the file, and that is correct: at that point
// they can also sign as this host, which is worse.
//
// What it does buy is Windows, and that is the case that made this necessary.
// `ProgramData\Kanpachi` grants read to every user of the machine, on purpose,
// because the interface reads from there without elevating. `identity.key` is
// the exception with its own ACL. So before this, any user of the PC could open
// `room.json` and read `NetworkSecret`, which is the real network's identity,
// and `CardKey`. Now they read a blob, and the key to it is behind the one ACL
// that never let them through.
//
// The whole private key goes in as the secret, seed and public half together.
// It is what `ed25519.NewKeyFromSeed` returns and it is deterministic from the
// seed, so the derived key is stable across restarts, which is the only
// property that matters here: a key that changed would leave every previously
// saved room unopenable.
func StateKey(priv ed25519.PrivateKey) ([StateKeyLen]byte, error) {
	var key [StateKeyLen]byte
	if len(priv) != ed25519.PrivateKeySize {
		return key, fmt.Errorf("la llave de esta instalación mide %d bytes y tiene que medir %d",
			len(priv), ed25519.PrivateKeySize)
	}
	// Sin sal, y a propósito: la sal de HKDF sirve cuando el secreto es de baja
	// entropía o compartido entre partes, y acá es una llave privada de 32 bytes
	// aleatorios que no sale de esta máquina. Una sal habría que guardarla, o sea
	// un fichero más que proteger, que es justo lo que esto evita.
	derivada, err := hkdf.Key(sha256.New, priv, nil, stateKeyInfo, StateKeyLen)
	if err != nil {
		return key, fmt.Errorf("derivando la llave del estado en reposo: %w", err)
	}
	copy(key[:], derivada)
	return key, nil
}
