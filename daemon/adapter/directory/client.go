package directory

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/core/port"
	"github.com/accentiostudios/kanpachi/core/timing"
)

// The budget of a call to the registry lives in [timing], as
// [timing.RegistryRequestTimeout] and [timing.RegistryDialTimeout]. What stays
// here is the only limit of the three that is not measured in time.
const maxResponseBytes = 64 << 10

// connectFunc is the raw TCP dial, AFTER the address has been resolved and
// checked. Injected so the tests can exercise the production path whole.
type connectFunc func(ctx context.Context, network, addr string) (net.Conn, error)

// The wire bodies. `base64.RawURLEncoding` on both sides, matching
// `registry.aB64`; padded base64 is a 400 from the server.
type publishBody struct {
	HostKey string `json:"host_key"`
	Card    string `json:"card"`
	Sig     string `json:"sig"`
}

// closeBody carries no card, because closing has none to carry. What the
// signature covers is `domain.RoomCloseMessage`, and `ts` travels beside it so
// that the other end can rebuild the same bytes.
type closeBody struct {
	HostKey string `json:"host_key"`
	Sig     string `json:"sig"`
	TS      int64  `json:"ts"`
}

type openedBody struct {
	InviteID string `json:"invite_id"`
}

type lookupBody struct {
	Card    string `json:"card"`
	HostKey string `json:"host_key"`
	// Sig is the signature the card was published with. Absent for a room the
	// registry wrote before it started keeping it, which is why it is not an
	// error to be missing: it comes back as "unverified" and never as "forged".
	Sig string `json:"sig"`
	// A POINTER, and that is the whole design of this field. The registry omits
	// the count when it has never managed to talk to the engine, because a zero
	// would be the claim "there is nobody" and it would be false. Absent says
	// "I do not know". Decoding into a plain int would turn one into the other.
	Members *int `json:"members"`
}

// errorBody is the whole of what a failure says, and it says no prose.
//
// The registry used to answer `{"error": "<texto en claro>"}` and that message
// went into what a person read. It does not any more, on purpose: a message
// that explains is a message that distinguishes, and on the auth path telling
// "that token expired" from "that token was never valid" is free information
// for whoever is guessing a password. The sentences live on this side now,
// where the language of the reader is known.
type errorBody struct {
	Code string `json:"code"`
	Sub  string `json:"sub"`
}

// The sub-codes this client acts on. Mirrors of `registry.SubReauth` and
// `registry.SubPassword`, and the contract test is what keeps them together.
const (
	subReauth   = "reauth"
	subPassword = "password"
)

// The two bodies of the auth endpoints.
type authBody struct {
	Proof string `json:"proof"`
}

type refreshRequest struct {
	Refresh string `json:"refresh_token"`
}

type tokenBody struct {
	Access    string `json:"access_token"`
	Refresh   string `json:"refresh_token"`
	ExpiresIn int    `json:"expires_in"`
}

func b64(raw []byte) string { return base64.RawURLEncoding.EncodeToString(raw) }

func deB64(s string) ([]byte, error) { return base64.RawURLEncoding.DecodeString(s) }

// newClient builds the one HTTP client this adapter uses.
//
// Every setting here is a refusal, and `docs/03-arquitectura.md` asks for them
// by name: fixed scheme and port, no redirects followed, caps on time and on
// response size, and `CheckSeedAddr` over every resolved address.
func newClient(dial connectFunc) *http.Client {
	return &http.Client{
		Timeout: timing.RegistryRequestTimeout,
		// A redirect is how a well-formed name reaches somewhere else entirely,
		// and following one would step around the address check that the dialer
		// just performed. The registry never redirects, so this only ever fires
		// on something that is not the registry.
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return fmt.Errorf("el registro del seed no redirige, así que una redirección " +
				"significa que del otro lado no está el registro")
		},
		Transport: &http.Transport{
			// Nil and not ProxyFromEnvironment. This process runs as SYSTEM, and
			// an environment variable is not allowed to choose where it dials.
			Proxy:                 nil,
			DialContext:           dial,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          2,
			IdleConnTimeout:       30 * time.Second,
			TLSHandshakeTimeout:   timing.RegistryDialTimeout,
			ExpectContinueTimeout: time.Second,
		},
	}
}

// dialVerified resolves the name, checks every address, and dials the ones that
// survive. It is the mirror of `seedURIs` in the engine adapter.
//
// # Why the transport is never allowed to resolve
//
// Checking is not enough on its own. If the name reached the dialer, the
// transport would resolve it again and the check would govern nothing: between
// our lookup and its lookup the DNS can answer differently, which is a TOCTOU
// with `192.168.1.1` on the other end of it. So the name is resolved HERE,
// checked HERE, and what goes down to TCP is an address that was already
// approved. The name still lives in the URL, so TLS verifies against it and the
// Host header is right, which is exactly the split that is wanted.
//
// A name that resolves to several addresses does not get discarded whole when
// one of them is bad: the good ones are kept, in the order they resolved, and
// the first that connects wins. Someone with split-horizon DNS at home still
// gets to play.
func dialVerified(
	ctx context.Context,
	addr string,
	resolve func(string) ([]netip.Addr, error),
	connect connectFunc,
	network string,
) (net.Conn, error) {
	host, puerto, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("no se entiende a dónde marcar, %q: %w", addr, err)
	}
	p, err := strconv.ParseUint(puerto, 10, 16)
	if err != nil {
		return nil, fmt.Errorf("el puerto del registro, %q, no es un puerto: %w", puerto, err)
	}

	direcciones, err := resolve(host)
	if err != nil {
		return nil, fmt.Errorf("%s no resolvió: %w", host, err)
	}
	if len(direcciones) == 0 {
		return nil, fmt.Errorf("%s no resolvió a ninguna dirección", host)
	}

	var buenas []netip.Addr
	var rechazadas []string
	for _, a := range direcciones {
		if err := domain.CheckSeedAddr(a); err != nil {
			rechazadas = append(rechazadas, fmt.Sprintf("%s → %s: %v", host, a, err))
			continue
		}
		buenas = append(buenas, a)
	}
	if len(buenas) == 0 {
		return nil, fmt.Errorf("ninguna dirección de %s quedó utilizable: %v", host, rechazadas)
	}

	var último error
	for _, a := range buenas {
		conn, err := connect(ctx, network, netip.AddrPortFrom(a, uint16(p)).String())
		if err == nil {
			return conn, nil
		}
		último = err
	}
	return nil, fmt.Errorf("no se pudo conectar con el registro de %s: %w", host, último)
}

// do is the single request path. Everything goes through here so that the caps
// and the strict decoding cannot be forgotten in one method out of three.
//
// # The one retry, and why it is not a retry policy
//
// A 401 that asks to reauthenticate costs one refresh and ONE repeat of the
// call. That is not the retry loop this adapter refuses to have: it does not
// fire on a timeout, on a 5xx or on being throttled, and it cannot happen twice
// for the same call. What it covers is the one case where the first answer was
// not about the request at all, which is an access token that ran out fifteen
// minutes into a room that is still open.
//
// Without it, every renewal of a code on a closed seed would surface as a
// failure that the person fixes by typing a password they already typed.
func (d *Directory) do(ctx context.Context, method, ruta string, in, out any, esperado int) error {
	// The bearer that is ABOUT to be sent, captured before the call. If the
	// refresh finds a different one, somebody else already renewed while this
	// call was queued behind it, and there is nothing left to do but retry.
	stale := d.accessToken()

	if err := d.attempt(ctx, method, ruta, in, out, esperado); err != nil {
		if !errors.Is(err, errReauth) {
			return err
		}
		if err := d.refreshAccess(ctx, stale); err != nil {
			return err
		}
		return d.attempt(ctx, method, ruta, in, out, esperado)
	}
	return nil
}

// errReauth never leaves this package. It is the marker that says "the answer
// was about the token, not about the request", and [Directory.do] is the only
// thing that ever sees it.
var errReauth = errors.New("el registro pide reautenticar")

func (d *Directory) attempt(ctx context.Context, method, ruta string, in, out any, esperado int) error {
	var cuerpo io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("armando el pedido al registro: %w", err)
		}
		cuerpo = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, method, d.base+ruta, cuerpo)
	if err != nil {
		return fmt.Errorf("armando el pedido al registro: %w", err)
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	// The bearer only goes out when there is one. An open seed never asks, and
	// sending a token to a seed that did not ask hands a credential to whoever
	// runs it.
	if tok := d.accessToken(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("hablando con el registro de %s: %w", d.deps.Seed, err)
	}
	defer resp.Body.Close()

	// One byte over the cap is how the cap gets noticed instead of silently
	// truncating a body into something that still parses.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("leyendo la respuesta del registro: %w", err)
	}
	if len(raw) > maxResponseBytes {
		return fmt.Errorf("el registro contestó más de %d bytes, así que del otro lado "+
			"no está el registro", maxResponseBytes)
	}
	if resp.StatusCode != esperado {
		return errorDelRegistro(resp.StatusCode, raw)
	}
	if out == nil {
		return nil
	}

	// Strict, like everything else this project decodes. The server refuses
	// unknown fields too, and both ends live in this repository with a contract
	// test holding them together, so strict here is lockstep and not brittle.
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("el registro contestó algo que no se entiende: %w", err)
	}
	return nil
}

// errorDelRegistro turns a coded answer into something a person can act on.
//
// The prose is written HERE and not by the registry, which is the point of the
// coded envelope: a seed that a stranger runs no longer chooses what appears on
// somebody's screen, and a code cannot leak the distinction between an expired
// token and an invalid one because there is only one code for both.
//
// The 429 says explicitly that nothing is being retried. The registry's rate
// limit counts failures too, so a client that retries on being throttled is a
// client that turns one stumble into a locked door.
func errorDelRegistro(status int, raw []byte) error {
	var e errorBody
	_ = json.Unmarshal(raw, &e)

	switch status {
	case http.StatusUnauthorized:
		// The two ways out, and the sub-code is what picks between them. Reauth
		// means an access token that ran out, which one refresh fixes without
		// anybody noticing; password means the refresh is gone too, and the only
		// move left is a person typing.
		if e.Sub == subReauth {
			return errReauth
		}
		return fmt.Errorf("%w: %s", port.ErrSeedPassword, codeText(status, e))
	case http.StatusNotFound:
		// Envuelto en el centinela, y es la única respuesta del registro que lo
		// lleva. Es lo que permite que entrar a una sala falle en el primer
		// segundo con un status que no existe, sin que un registro caído
		// impida entrar a una que sí. Ver [port.ErrUnknownRoom].
		return fmt.Errorf("%w (%s)", port.ErrUnknownRoom, codeText(status, e))
	case http.StatusForbidden:
		return fmt.Errorf("el registro no acepta esta llave para esa sala, "+
			"así que la reservó otro equipo (%s)", codeText(status, e))
	case http.StatusRequestEntityTooLarge:
		return fmt.Errorf("la tarjeta de la sala no le entra al registro (%s)", codeText(status, e))
	case http.StatusTooManyRequests:
		// Wrapped in the sentinel so whatever is on a cadence can slow down
		// instead of hammering: the limit counts failures, so insisting is what
		// keeps the door shut. See [port.ErrSeedThrottled].
		return fmt.Errorf("%w, y reintentar al mismo ritmo es lo que alarga el freno (%s)",
			port.ErrSeedThrottled, codeText(status, e))
	case http.StatusConflict:
		return fmt.Errorf("el registro contestó que no pide password (%s)", codeText(status, e))
	default:
		return fmt.Errorf("el registro contestó %d: %s", status, codeText(status, e))
	}
}

// codeText names what came back without pretending it is a sentence. The
// code is a token of this repository's own vocabulary, so printing it is
// printing a fact; falling back to the HTTP text covers a body that never
// arrived or never parsed.
func codeText(status int, e errorBody) string {
	if e.Code == "" {
		return http.StatusText(status)
	}
	if e.Sub == "" {
		return e.Code
	}
	return e.Code + "/" + e.Sub
}
