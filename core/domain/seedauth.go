package domain

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"unicode/utf8"
)

// The seed password: the rule it has to meet, and the value that travels.
//
// It lives here because both sides need the same answer and neither can ask the
// other. The daemon computes the proof before it has a connection worth using,
// the registry checks the rule before it stores anything, and the interface
// mirrors the rule in Dart with a lockstep test.
//
// # A shared password, and no accounts
//
// One password for every host of a seed. The operator's way of throwing
// everybody out is to change it, which is why nothing here identifies a person.
// See decision 33 of docs/02-decisiones-de-diseno.md.

// The length rule, and why the two ends are so far apart.
//
// Four is low on purpose. What guards this door is the rate limit and the
// one-slot Argon2id brake in the registry, not the entropy of what a person
// types: a seed shared with five friends is the common case, and refusing to
// let it be easy would push the operator to disable the password instead.
//
// The ceiling is technical and nothing else. It is what keeps a megabyte from
// reaching a hash function; anyone who wants a passphrase has all the room they
// could use. Counted in RUNES and not in bytes, so that a rule stated in
// characters is enforced in characters.
const (
	MinSeedPasswordLen = 4
	MaxSeedPasswordLen = 128
)

// ErrSeedPassword is what a password that breaks the rule comes back as. It is
// a sentinel so the interface can tell it apart from a wrong password, which is
// the same shape of failure and a completely different thing to say.
var ErrSeedPassword = errors.New("el password del seed no cumple la regla")

// ValidateSeedPassword applies the rule of [MinSeedPasswordLen] and
// [MaxSeedPasswordLen].
func ValidateSeedPassword(password string) error {
	n := utf8.RuneCountInString(password)
	switch {
	case n < MinSeedPasswordLen:
		return fmt.Errorf("%w: hacen falta al menos %d caracteres", ErrSeedPassword, MinSeedPasswordLen)
	case n > MaxSeedPasswordLen:
		return fmt.Errorf("%w: el tope son %d caracteres", ErrSeedPassword, MaxSeedPasswordLen)
	}
	return nil
}

// seedAuthLabel keeps this hash from ever colliding with another use of the
// same password, present or future. Versioned, like the rendezvous salts.
const seedAuthLabel = "kanpachi/seed-auth/v1"

// SeedAuthProofLen is how many characters a proof is once encoded. The registry
// checks it before spending any work, which is what keeps a body of garbage
// from reaching Argon2id.
const SeedAuthProofLen = 43 // base64url sin relleno de 32 bytes

// SeedAuthProof is what the client sends instead of the password.
//
// # Why the password never travels, when TLS is already there
//
// TLS gives confidentiality on the wire, and that is not the problem this
// solves. The problem is the OPERATOR: a seed is a machine someone else runs,
// and people reuse passwords. Hashing here means the operator of any seed
// learns a value that is useless everywhere else, including in their own logs
// and in a stolen state file.
//
// # Why the host goes inside
//
// Because to the server this proof IS the password: whatever authenticates once
// authenticates again. Without the host bound in, a proof captured on one seed
// would open rooms on every other seed the same person uses, which is exactly
// the reuse that hashing was meant to stop. Binding it makes the value worth
// nothing anywhere except the machine it was computed for.
//
// # Why this hash is fast, on purpose
//
// The expensive one belongs on the side that can size itself for it. The
// registry runs Argon2id over this value, under the same single-slot brake as
// the rendezvous derivation, because a seed that can be knocked over by a
// handful of login attempts is a seed that does not need to be broken into.
//
// The host is parsed and normalized here rather than trusted, so that a
// difference in case or a trailing dot cannot turn into a password that works
// on one screen and fails on the next.
func SeedAuthProof(host, password string) (string, error) {
	h, err := parseSeedHost(host)
	if err != nil {
		return "", err
	}
	if err := ValidateSeedPassword(password); err != nil {
		return "", err
	}

	// The zero byte separates the host from the rest, and a hostname cannot
	// contain one. The label is a fixed constant, so the reading of these bytes
	// is unambiguous without any length prefixes.
	sum := sha256.New()
	sum.Write([]byte(h))
	sum.Write([]byte{0})
	sum.Write([]byte(seedAuthLabel))
	sum.Write([]byte(password))
	return base64.RawURLEncoding.EncodeToString(sum.Sum(nil)), nil
}

// SeedToken is what survives a restart of a machine that hosts on a closed seed.
//
// The seed travels WITH the token, and it is not decoration. A token only means
// something on the seed that minted it, so a stored token whose seed no longer
// matches the configured one has to be discarded rather than sent: sending it
// would put a credential of one operator in front of another one.
//
// The access token is deliberately absent. It lives fifteen minutes, so writing
// it to disk would put a live credential where it can be stolen in exchange for
// saving one round trip after a reboot.
type SeedToken struct {
	Seed    string `json:"seed"`
	Refresh string `json:"refresh"`
}

// MaxSeedTokenBytes acota lo que se acepta leer del disco. Un token son unos
// cien caracteres, y el fichero vive en un directorio que en Windows todos los
// usuarios pueden leer y algunos escribir.
const MaxSeedTokenBytes = 4 << 10

// EncodeSeedToken serializes it for the state store.
func EncodeSeedToken(t SeedToken) ([]byte, error) {
	if t.Seed == "" || t.Refresh == "" {
		return nil, fmt.Errorf("%w: un token guardado necesita seed y refresh", ErrInputShape)
	}
	return json.Marshal(t)
}

// DecodeSeedToken reads one back, treating it as hostile input like every other
// file this daemon loads.
func DecodeSeedToken(raw []byte) (SeedToken, error) {
	if len(raw) > MaxSeedTokenBytes {
		return SeedToken{}, fmt.Errorf("%w: el token guardado mide %d bytes", ErrInputShape, len(raw))
	}
	var t SeedToken
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&t); err != nil {
		return SeedToken{}, fmt.Errorf("%w: el token guardado no parsea: %v", ErrInputShape, err)
	}
	seed, err := parseSeedHost(t.Seed)
	if err != nil {
		return SeedToken{}, err
	}
	if t.Refresh == "" {
		return SeedToken{}, fmt.Errorf("%w: el token guardado no trae refresh", ErrInputShape)
	}
	return SeedToken{Seed: seed, Refresh: t.Refresh}, nil
}
