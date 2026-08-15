package registry

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
	"golang.org/x/crypto/argon2"
)

// The operator credential of a seed, and the tokens it mints.
//
// A seed can be closed with ONE shared password, and only for hosting. Looking
// a room up is never asked for anything: the friction of the guest is what the
// product exists to remove, and opening a room already assumes a more technical
// role. See decision 33 of docs/02-decisiones-de-diseno.md.
//
// # What is stored, and what is not
//
// The password is not recoverable by anybody, the operator included: what
// lands on disk is Argon2id over the proof the client computed, next to the
// salt that produced it. The signing key is the one piece of real secret
// material here, because whoever reads it MINTS tokens without knowing any
// password. It never reaches a log, a diagnosis or a state dump.
//
// # How everybody gets thrown out
//
// Changing the password rotates the signing key, so every token ever issued
// stops verifying at that instant, with nothing to enumerate and no table to
// walk. That is the whole revocation story, and it is the reason tokens are
// signed rather than stored: an issued-token store is a thing to sweep, and a
// sweep that falls behind is a door that stays open.

// authFile is the name inside the state directory. It sits next to rooms.json
// and shares its fate: no state directory means no credential, so a seed that
// cannot remember rooms cannot be closed either.
const authFile = "auth.json"

const authVersion = 1

// The lifetimes of the two tokens.
//
// Access is short because it travels on every mutation and buys nothing by
// living longer: a refresh costs one round trip against a seed the client is
// already talking to.
//
// Refresh is long and does NOT slide. Refreshing mints a new access token and
// hands back the same refresh, so the session has a hard ceiling instead of one
// that a stolen token can extend forever. Thirty days later the person types
// the password again, which is also the only moment the interface has to ask.
const (
	AccessTTL  = 15 * time.Minute
	RefreshTTL = 30 * 24 * time.Hour
)

// The Argon2id cost of checking a password.
//
// The memory is domain.ArgonMemoryKiB and not a number of its own, and that is
// load-bearing: the systemd unit sizes MemoryMax at four times that constant,
// and authentication shares the SAME one-slot brake as the rendezvous
// derivation. Two independent brakes would put the peak at two derivations and
// make the unit's arithmetic false, which is the failure mode that already
// killed this service once by OOM.
//
// Time and threads travel INSIDE the credential, so a future change verifies an
// old credential with the parameters that produced it. Nothing here is frozen
// the way the rendezvous parameters are: only one machine ever computes this
// hash, against a salt it stored itself.
const (
	authArgonTime    = 3
	authArgonThreads = 4
	authHashLen      = 32
	authSaltLen      = 16
	signingKeyLen    = 32
)

// storedCredential is the whole file. Treated as hostile input on the way in,
// like everything else this process reads off a disk that outlives its binary.
type storedCredential struct {
	Version int    `json:"version"`
	Salt    []byte `json:"salt"`
	Hash    []byte `json:"hash"`
	Signing []byte `json:"signing"`
	Time    uint32 `json:"time"`
	Memory  uint32 `json:"memory"`
	Threads uint8  `json:"threads"`
}

// Auth is the door. A nil credential means the seed is open, which is what
// every seed is until somebody sets a password.
type Auth struct {
	mu   sync.RWMutex
	path string
	cred *storedCredential
	now  func() time.Time
}

// ErrNoStateDir is what closing a volatile seed comes back as. Refusing is the
// honest answer: a password that disappears on the next restart is worse than
// no password, because the operator believes the door is shut.
var ErrNoStateDir = errors.New("un seed sin directorio de estado no se puede cerrar con password")

// ErrOpenSeed says there is no password to check against. It is a distinct
// answer on purpose: a client that asked for a token on an open seed is a
// client that should stop asking, not one that should retry.
var ErrOpenSeed = errors.New("este seed no pide password")

// OpenAuth reads the credential out of dir, if there is one.
//
// An empty dir gives a permanently open seed, which is what the tests and a
// `kanpseed serve` without state directory get. A file that does not parse is
// NOT ignored: unlike a room, a credential that fails to load would silently
// reopen a seed the operator closed, so it comes back as an error and the
// caller decides how loudly to say it.
func OpenAuth(dir string) (*Auth, error) {
	a := &Auth{now: time.Now}
	if dir == "" {
		return a, nil
	}
	a.path = filepath.Join(dir, authFile)
	return a, a.Reload()
}

// Reload re-reads the credential from disk.
//
// It hangs off the same SIGHUP as the page and the cached version, because it
// is the same fact: somebody touched what this process serves without
// restarting it. `kanpseed password` is a different process and cannot write
// into this one's memory.
func (a *Auth) Reload() error {
	if a.path == "" {
		return nil
	}
	raw, err := os.ReadFile(a.path)
	if os.IsNotExist(err) {
		a.mu.Lock()
		a.cred = nil
		a.mu.Unlock()
		return nil
	}
	if err != nil {
		return fmt.Errorf("leyendo %s: %w", a.path, err)
	}

	var c storedCredential
	if err := json.Unmarshal(raw, &c); err != nil {
		return fmt.Errorf("la credencial guardada en %s no parsea: %w", a.path, err)
	}
	if err := c.valid(); err != nil {
		return fmt.Errorf("la credencial guardada en %s no se sostiene: %w", a.path, err)
	}

	a.mu.Lock()
	a.cred = &c
	a.mu.Unlock()
	return nil
}

// valid checks a credential read off disk. Every field is load-bearing: a zero
// memory cost would make Argon2id panic, and an empty signing key would sign
// every token with the same nothing.
func (c storedCredential) valid() error {
	switch {
	case c.Version != authVersion:
		return fmt.Errorf("es de la versión %d y esta entiende la %d", c.Version, authVersion)
	case len(c.Salt) != authSaltLen:
		return errors.New("la sal no mide lo que tiene que medir")
	case len(c.Hash) != authHashLen:
		return errors.New("el hash no mide lo que tiene que medir")
	case len(c.Signing) != signingKeyLen:
		return errors.New("la clave de firma no mide lo que tiene que medir")
	case c.Time == 0 || c.Memory == 0 || c.Threads == 0:
		return errors.New("los parámetros de derivación están vacíos")
	}
	return nil
}

// Closed reports whether hosting on this seed needs a token.
func (a *Auth) Closed() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.cred != nil
}

// SetPassword closes the seed, or changes the password of one already closed.
//
// The proof is what the client would send, so the operator's password never
// reaches this process either. host is what the clients type, and it is bound
// into the proof: setting a password against the wrong host produces a
// credential that nobody can satisfy.
//
// Rotating the signing key here is what throws every host out. It is not a side
// effect to be tidied away later: it IS the revocation mechanism.
func (a *Auth) SetPassword(host, password string) error {
	if a.path == "" {
		return ErrNoStateDir
	}
	proof, err := domain.SeedAuthProof(host, password)
	if err != nil {
		return err
	}

	salt := make([]byte, authSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return fmt.Errorf("no se pudo generar la sal: %w", err)
	}
	signing := make([]byte, signingKeyLen)
	if _, err := rand.Read(signing); err != nil {
		return fmt.Errorf("no se pudo generar la clave de firma: %w", err)
	}

	c := storedCredential{
		Version: authVersion,
		Salt:    salt,
		Signing: signing,
		Time:    authArgonTime,
		Memory:  domain.ArgonMemoryKiB,
		Threads: authArgonThreads,
	}
	c.Hash = c.derive(proof)

	if err := a.save(&c); err != nil {
		return err
	}
	a.mu.Lock()
	a.cred = &c
	a.mu.Unlock()
	return nil
}

// Open reopens the seed by removing the credential. Every issued token dies
// with it, which is correct: they were minted to prove something that is no
// longer being asked.
func (a *Auth) Open() error {
	if a.path == "" {
		return nil
	}
	if err := os.Remove(a.path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("borrando %s: %w", a.path, err)
	}
	a.mu.Lock()
	a.cred = nil
	a.mu.Unlock()
	return nil
}

// derive is the expensive half. It runs with the caller holding the shared
// one-slot brake; see [acquireDerivation].
func (c storedCredential) derive(proof string) []byte {
	return argon2.IDKey([]byte(proof), c.Salt, c.Time, c.Memory, c.Threads, authHashLen)
}

// Tokens is what a successful login hands back.
type Tokens struct {
	Access    string
	Refresh   string
	ExpiresIn int
}

// Login checks a proof and mints a pair.
//
// The caller is responsible for having taken the rate limit AND the derivation
// brake before getting here, in that order. Both matter: a rejected attempt
// must never reach Argon2id, and a queued one must be able to give up with its
// request instead of holding a socket open.
//
// The comparison is constant time. The salt is public and the hash is not a
// secret worth much on its own; the habit is what protects the places where it
// is, and a `==` here would be the one that gets copied.
func (a *Auth) Login(proof string) (Tokens, error) {
	a.mu.RLock()
	c := a.cred
	a.mu.RUnlock()
	if c == nil {
		return Tokens{}, ErrOpenSeed
	}
	if len(proof) != domain.SeedAuthProofLen {
		return Tokens{}, ErrBadCredential
	}
	if subtle.ConstantTimeCompare(c.derive(proof), c.Hash) != 1 {
		return Tokens{}, ErrBadCredential
	}
	return a.mint(c)
}

func (a *Auth) mint(c *storedCredential) (Tokens, error) {
	access, err := c.sign(kindAccess, a.now().Add(AccessTTL))
	if err != nil {
		return Tokens{}, err
	}
	refresh, err := c.sign(kindRefresh, a.now().Add(RefreshTTL))
	if err != nil {
		return Tokens{}, err
	}
	return Tokens{Access: access, Refresh: refresh, ExpiresIn: int(AccessTTL.Seconds())}, nil
}

// Refresh mints a fresh access token from a live refresh token.
//
// It hands back only the access token, and that is the design: the refresh does
// not slide, so a session has a hard ceiling of [RefreshTTL] from the moment
// the password was typed. A sliding refresh would let a stolen token renew
// itself forever, and the only thing that would end it is the operator
// noticing.
func (a *Auth) Refresh(token string) (string, int, error) {
	a.mu.RLock()
	c := a.cred
	a.mu.RUnlock()
	if c == nil {
		return "", 0, ErrOpenSeed
	}
	if !c.verify(token, kindRefresh, a.now()) {
		return "", 0, ErrBadCredential
	}
	access, err := c.sign(kindAccess, a.now().Add(AccessTTL))
	if err != nil {
		return "", 0, err
	}
	return access, int(AccessTTL.Seconds()), nil
}

// ErrBadCredential is the single answer to every rejected password and every
// rejected bearer, and it says nothing about why.
//
// **One sentinel and not two on purpose.** A wrong password, a malformed one,
// an expired token and one that was never valid all lead to the same place,
// which is typing the password again. Two errors with the same text is an
// invitation to branch on them, and a branch here is a distinction handed to
// whoever is guessing. See [respondError].
var ErrBadCredential = errors.New("credencial inválida")

// Allows says whether this bearer may mutate. An open seed allows everything,
// which is what every seed does until somebody closes it.
func (a *Auth) Allows(token string) bool {
	a.mu.RLock()
	c := a.cred
	a.mu.RUnlock()
	if c == nil {
		return true
	}
	return c.verify(token, kindAccess, a.now())
}

// ---- Tokens ---------------------------------------------------------------

// The token format. Opaque and signed, not a JWT: the client reads nothing out
// of it, so a format that invites reading is surface for no gain.
//
//	kind(1) ‖ expiry(8, big endian, unix seconds) ‖ nonce(16) ‖ mac(32)
//
// The kind is INSIDE the signed bytes, so a refresh token cannot be presented
// where an access token is expected. The nonce makes two tokens minted in the
// same second different from each other, which keeps a token from being a
// recognizable value that leaks when it was issued.
type tokenKind byte

const (
	kindAccess  tokenKind = 1
	kindRefresh tokenKind = 2
)

const (
	tokenNonceLen = 16
	tokenMACLen   = 32
	tokenBodyLen  = 1 + 8 + tokenNonceLen
	tokenLen      = tokenBodyLen + tokenMACLen
)

// tokenLabel keeps this MAC from ever meaning anything else signed with the
// same key. Versioned, like every other domain separator in this project.
const tokenLabel = "kanpachi/seed-token/v1"

func (c storedCredential) sign(k tokenKind, expira time.Time) (string, error) {
	body := make([]byte, tokenBodyLen)
	body[0] = byte(k)
	binary.BigEndian.PutUint64(body[1:9], uint64(expira.Unix()))
	if _, err := rand.Read(body[9:]); err != nil {
		return "", fmt.Errorf("no se pudo generar el token: %w", err)
	}
	tok := append(body, c.mac(body)...)
	return base64.RawURLEncoding.EncodeToString(tok), nil
}

func (c storedCredential) verify(token string, k tokenKind, ahora time.Time) bool {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != tokenLen {
		return false
	}
	body, mac := raw[:tokenBodyLen], raw[tokenBodyLen:]
	if !hmac.Equal(mac, c.mac(body)) {
		return false
	}
	if tokenKind(body[0]) != k {
		return false
	}
	return ahora.Unix() < int64(binary.BigEndian.Uint64(body[1:9]))
}

func (c storedCredential) mac(body []byte) []byte {
	m := hmac.New(sha256.New, c.Signing)
	m.Write([]byte(tokenLabel))
	m.Write(body)
	return m.Sum(nil)
}

// ---- Disco ----------------------------------------------------------------

// guardar writes the credential atomically, with the same pattern and the same
// reasoning as [keeper.save]: temporary in the same directory, sync, rename. A
// credential written halfway is a seed that locks its operator out.
//
// 0600 because of the signing key. Everything else in this file is a hash and a
// salt; that field is material that mints tokens, and it is worth exactly as
// much as the password and never expires.
func (a *Auth) save(c *storedCredential) error {
	data, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("serializando la credencial: %w", err)
	}

	dir := filepath.Dir(a.path)
	tmp, err := os.CreateTemp(dir, ".auth-*.json")
	if err != nil {
		return fmt.Errorf("creando el temporal de la credencial en %s: %w", dir, err)
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("ajustando los permisos del temporal: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("escribiendo la credencial: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sincronizando la credencial: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("cerrando el temporal de la credencial: %w", err)
	}
	if err := os.Rename(name, a.path); err != nil {
		return fmt.Errorf("reemplazando %s: %w", a.path, err)
	}
	return nil
}

// Path is where the credential lives, so that `kanpseed password` can hand the
// file to the user that runs the service. Empty on a volatile seed.
func (a *Auth) Path() string { return a.path }
