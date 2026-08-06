package directory

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// The budget of a call to the registry, and why there are two numbers.
//
// They are constants and not fields on purpose: `internal/arch/corte_test.go`
// forbids exported setters over deadlines, and this is the same idea one layer
// out. Nothing outside this package gets to widen them.
//
// `dialTimeout` is well inside `requestTimeout` because of where these calls
// run from: `Open` and `Publish` are called with the session lock held, and
// `Status()` takes that same lock to paint the screen. A seed that has gone
// silent is the measured case on Windows, which does not bounce, it says
// nothing. Without the shorter dial the UI would freeze for the whole ten
// seconds waiting on a host that will never answer.
const (
	requestTimeout   = 10 * time.Second
	dialTimeout      = 4 * time.Second
	maxResponseBytes = 64 << 10
)

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

type openedBody struct {
	InviteID string `json:"invite_id"`
}

type lookupBody struct {
	Card    string `json:"card"`
	HostKey string `json:"host_key"`
	// A POINTER, and that is the whole design of this field. The registry omits
	// the count when it has never managed to talk to the engine, because a zero
	// would be the claim "there is nobody" and it would be false. Absent says
	// "I do not know". Decoding into a plain int would turn one into the other.
	Members *int `json:"members"`
}

type errorBody struct {
	Error string `json:"error"`
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
		Timeout: requestTimeout,
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
			TLSHandshakeTimeout:   dialTimeout,
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
func (d *Directory) do(ctx context.Context, method, ruta string, in, out any, esperado int) error {
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

// errorDelRegistro turns a status code into something a person can act on.
//
// The 429 says explicitly that nothing is being retried. The registry's rate
// limit counts failures too, so a client that retries on being throttled is a
// client that turns one stumble into a locked door.
func errorDelRegistro(código int, raw []byte) error {
	var e errorBody
	_ = json.Unmarshal(raw, &e)
	detalle := e.Error
	if detalle == "" {
		detalle = http.StatusText(código)
	}

	switch código {
	case http.StatusNotFound:
		return fmt.Errorf("el registro no conoce esa sala (%s)", detalle)
	case http.StatusForbidden:
		return fmt.Errorf("el registro no acepta esta llave para esa sala, "+
			"así que la reservó otro equipo (%s)", detalle)
	case http.StatusRequestEntityTooLarge:
		return fmt.Errorf("la tarjeta de la sala no le entra al registro (%s)", detalle)
	case http.StatusTooManyRequests:
		return fmt.Errorf("el registro está frenando las consultas de esta red, "+
			"y no se reintenta porque reintentar es lo que alarga el freno (%s)", detalle)
	default:
		return fmt.Errorf("el registro contestó %d: %s", código, detalle)
	}
}
