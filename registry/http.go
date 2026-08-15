package registry

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/core/timing"
	"github.com/accentiostudios/kanpachi/internal/selfupdate"
)

// maxBody acota el cuerpo de cualquier petición antes de leerlo. El endpoint
// de publicación está abierto a internet y el registro vive en memoria.
const maxBody = 4 << 10

func randomInviteID() (domain.InviteID, error) { return domain.NewInviteID(rand.Reader) }

// Server ata el registro, el contador y la página en un solo puerto.
//
// Un solo puerto a propósito: la página y su API comparten origen, así que no
// hay CORS que configurar ni una segunda cosa que exponer. El nginx del
// droplet apunta acá y termina el TLS.
type Server struct {
	store   *Store
	counter *Counter
	page    *Page
	release *Release
	limiter *limiter
	auth    *Auth
	// authLimiter is far narrower than the general one, and it has to be. The
	// general limit exists to make enumerating 40-bit invite IDs pointless;
	// this one guards a password that is allowed to be four characters long.
	authLimiter *limiter
	slow        *throttle
}

// NewServer ties everything to one port. auth may be nil, and that means a
// permanently open seed, which is what a registry without a state directory is.
func NewServer(s *Store, c *Counter, p *Page, auth *Auth) *Server {
	if auth == nil {
		auth = &Auth{now: time.Now}
	}
	return &Server{
		store:       s,
		counter:     c,
		page:        p,
		release:     NewRelease(),
		limiter:     newLimiter(30, time.Minute),
		auth:        auth,
		authLimiter: newLimiter(5, time.Minute),
		slow:        &throttle{},
	}
}

// Handler wires the routes, and the wiring IS the policy of what a closed seed
// closes.
//
// # What needs a token, and what never does
//
// Opening a room, publishing to one and renewing its code go through
// [Server.guarded]. Looking a room up, /healthz and the page do not, ever.
// Entering a room asks for nothing on any seed, open or closed: the friction of
// the guest is what this product exists to remove.
//
// /healthz being outside is not a leftover. The unit beats by asking for it
// with WatchdogSec=30s, so covering it with auth would restart the seed every
// thirty seconds and the BindsTo would take the engine down with it. An
// availability failure wearing someone else's clothes, like the OOM was.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/rooms", s.limitado(s.guarded(s.emitir)))
	mux.HandleFunc("GET /api/i/{id}", s.limitado(s.resolver))
	mux.HandleFunc("PUT /api/i/{id}", s.limitado(s.guarded(s.publicar)))
	mux.HandleFunc("DELETE /api/i/{id}", s.limitado(s.guarded(s.cerrar)))
	mux.HandleFunc("POST /api/auth/token", s.authLimited(s.login))
	mux.HandleFunc("POST /api/auth/refresh", s.authLimited(s.refresh))
	mux.HandleFunc("GET /healthz", s.salud)
	// Todo lo que cuelgue de /api/ y no case arriba muere acá, con el sobre de
	// error y no con la página.
	//
	// **Medido contra el despliegue, y era un fallo de verdad.** Sin esta línea,
	// `/api/version` contestaba 200 con el HTML de la invitación, porque caía en
	// el comodín de abajo. Ese endpoint se borró en este arco, así que los
	// clientes viejos que lo piden reciben una página donde esperan JSON, y una
	// ruta mal escrita contesta que todo va bien. La decisión del sobre de error
	// existe justo para que la API no explique nada en prosa, y una página entera
	// es la prosa más larga posible.
	//
	// El comodín se queda porque es como se sirve la invitación: `/{CÓDIGO}` es
	// una ruta legítima. Lo que se acota es el prefijo de la API, que sí es un
	// espacio cerrado.
	mux.HandleFunc("/api/", s.apiNoExiste)
	// **The page is rate limited too, and the line above used to say it could
	// not be enumerated.** That was false, and it was measured against the
	// deployment: `/{CÓDIGO}` resolves an invite ID and embeds the SAME view
	// that `GET /api/i/{id}` serves, so a live code came back with the card and
	// a dead one came back with `"room":null` — the same existence oracle as the
	// endpoint that does have a brake, reachable at whatever rate anyone liked.
	// Enumerating simply moved one route over.
	//
	// The SAME limiter as the API on purpose, not one of its own. A budget of
	// its own would hand an enumerator thirty a minute here plus thirty there;
	// sharing it means the ceiling is thirty per IP no matter which door gets
	// knocked on. It costs an honest visitor nothing: opening an invitation is
	// one request for the page, the browser's implicit `/favicon.ico`, and the
	// fallback `fetch` only when the server did not resolve the card already.
	mux.HandleFunc("/", s.limitado(s.servirPagina))
	return cabecerasSeguras(mux)
}

// apiNoExiste contesta el sobre de error a cualquier ruta de API que no exista.
//
// No dice cuál era la ruta ni sugiere parecidas: eso es contestarle a quien está
// probando el espacio, que es lo mismo que el 401 se niega a hacer.
func (s *Server) apiNoExiste(w http.ResponseWriter, r *http.Request) {
	respondError(w, http.StatusNotFound, CodeNotFound, "")
}

// ---- API ------------------------------------------------------------------

type cuerpoPublicar struct {
	HostKey string `json:"host_key"`
	Card    string `json:"card"`
	Sig     string `json:"sig"`
}

func (c cuerpoPublicar) decodificar() (ed25519.PublicKey, []byte, []byte, error) {
	llave, err := deB64(c.HostKey)
	if err != nil || len(llave) != ed25519.PublicKeySize {
		return nil, nil, nil, errors.New("host_key no es una llave Ed25519")
	}
	tarjeta, err := deB64(c.Card)
	if err != nil {
		return nil, nil, nil, errors.New("card no es base64url")
	}
	firma, err := deB64(c.Sig)
	if err != nil || len(firma) != ed25519.SignatureSize {
		return nil, nil, nil, errors.New("sig no es una firma Ed25519")
	}
	return ed25519.PublicKey(llave), tarjeta, firma, nil
}

func (s *Server) emitir(w http.ResponseWriter, r *http.Request) {
	var c cuerpoPublicar
	if !leerJSON(w, r, &c) {
		return
	}
	llave, tarjeta, firma, err := c.decodificar()
	if err != nil {
		respondError(w, http.StatusBadRequest, CodeBadRequest, "")
		return
	}
	// The request's context reaches the derivation brake. Someone who hung up
	// while queued behind another room does not get 64 MiB spent on them.
	id, err := s.store.Issue(r.Context(), llave, tarjeta, firma)
	if err != nil {
		traducir(w, err)
		return
	}
	responder(w, http.StatusCreated, map[string]any{"invite_id": id.String()})
}

func (s *Server) publicar(w http.ResponseWriter, r *http.Request) {
	id, ok := idDeRuta(w, r)
	if !ok {
		return
	}
	var c cuerpoPublicar
	if !leerJSON(w, r, &c) {
		return
	}
	llave, tarjeta, firma, err := c.decodificar()
	if err != nil {
		respondError(w, http.StatusBadRequest, CodeBadRequest, "")
		return
	}
	if err := s.store.publish(id, llave, tarjeta, firma); err != nil {
		traducir(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// cuerpoCerrar is what the host sends to close its room.
//
// It carries no card, so the signature cannot go over one the way publishing
// does: it goes over [domain.RoomCloseMessage], which binds it to THAT invite ID
// and THAT instant. `ts` travels separately because whoever checks has to
// rebuild the same bytes, and with the stamp both inside and outside the
// signature there would be no way to know which one was signed.
type cuerpoCerrar struct {
	HostKey string `json:"host_key"`
	Sig     string `json:"sig"`
	TS      int64  `json:"ts"`
}

// cerrar expires the card of a room its host has just closed.
//
// # Why it exists, measured
//
// Without it the room ended on the host's machine and the registry never heard:
// the code kept answering "it exists" until the card expired, which is up to six
// hours. Whoever pasted that code brought up the lobby, dialled a host that was
// gone, and ate the operating system's long timeout to land on a message about
// reconnecting. Checked against the deployment on 2026-08-15: twenty-two
// seconds, twenty-one of them the dial to a door nobody was going to open.
//
// # What it does NOT do, and that is half the design
//
// It does not delete the entry. It expires the card and leaves the pin standing,
// so the invite ID still belongs to its host and reopening the same room under
// the same code keeps working. See [Store.retire].
func (s *Server) cerrar(w http.ResponseWriter, r *http.Request) {
	id, ok := idDeRuta(w, r)
	if !ok {
		return
	}
	var c cuerpoCerrar
	if !leerJSON(w, r, &c) {
		return
	}
	llave, err := deB64(c.HostKey)
	if err != nil || len(llave) != ed25519.PublicKeySize {
		respondError(w, http.StatusBadRequest, CodeBadRequest, "")
		return
	}
	firma, err := deB64(c.Sig)
	if err != nil || len(firma) != ed25519.SignatureSize {
		respondError(w, http.StatusBadRequest, CodeBadRequest, "")
		return
	}
	// The clock is checked BEFORE the signature. The order matters little for
	// security and some for cost: verifying Ed25519 is cheap, and there is still
	// no reason to spend it on a message that is going to be rejected for age.
	if err := domain.CheckRoomCloseTime(time.Unix(c.TS, 0), time.Now()); err != nil {
		respondError(w, http.StatusBadRequest, CodeBadRequest, "")
		return
	}
	if !ed25519.Verify(ed25519.PublicKey(llave), domain.RoomCloseMessage(id, time.Unix(c.TS, 0)), firma) {
		respondError(w, http.StatusForbidden, CodeBadSig, "")
		return
	}
	if err := s.store.retire(id, ed25519.PublicKey(llave)); err != nil {
		traducir(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) resolver(w http.ResponseWriter, r *http.Request) {
	id, ok := idDeRuta(w, r)
	if !ok {
		return
	}
	sala, err := s.store.lookup(id)
	if err != nil {
		traducir(w, err)
		return
	}
	responder(w, http.StatusOK, s.vista(sala))
}

// OlvidarVersión hace que la próxima visita vuelva a preguntar qué versión hay,
// y recarga la credencial del operador.
//
// Existe para que el SIGHUP que relee la página pueda hacer las tres cosas: son
// el mismo hecho, alguien tocó lo que este proceso sirve sin reiniciarlo. La
// credencial la escribe `kanpseed password`, que es OTRO proceso y no puede
// escribir en la memoria de este. Ver [Release.Olvidar] y [Auth.Reload].
//
// Devuelve el fallo de recargar la credencial y no el de la versión, porque
// solo uno de los dos deja al seed comportándose distinto de lo que el operador
// acaba de pedir.
func (s *Server) OlvidarVersión() error {
	s.release.Olvidar()
	return s.auth.Reload()
}

// vista arma lo que se publica de una sala. Es todo lo que el registro sabe, y
// no incluye la red de encuentro: el cliente la deriva del invite ID por su
// cuenta, así que llegar al vestíbulo no depende de que esta API esté viva ni
// de que diga la verdad.
func (s *Server) vista(sala Room) map[string]any {
	v := map[string]any{
		"card":     aB64(sala.Card),
		"host_key": aB64(sala.HostKey),
	}
	// The signature goes out ONLY when there is one. A room written before the
	// registry started keeping it comes back from disk without one, and omitting
	// it tells the truth, "I do not have it", while an empty string would say
	// "it is unsigned", which is a different claim and a false one.
	if len(sala.Sig) > 0 {
		v["sig"] = aB64(sala.Sig)
	}
	// El contador se omite si nunca se pudo hablar con EasyTier. Un cero sería
	// una afirmación falsa, "no hay nadie"; ausente dice la verdad, "no lo sé".
	if n, ok := s.counter.For(sala.RendezvousName); ok {
		v["members"] = n
	}
	return v
}

// ---- La puerta ------------------------------------------------------------

// guarded refuses a mutation when the seed is closed and the bearer does not
// hold a live access token.
//
// An open seed lets everything through, so a seed with no password behaves
// exactly as it did before any of this existed.
func (s *Server) guarded(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.auth.Allows(bearer(r)) {
			// The header is what tells a client the door exists at all. Without
			// it, a first-time host against a closed seed gets a 401 with
			// nothing to distinguish it from being throttled.
			w.Header().Set("WWW-Authenticate", "Bearer")
			respondError(w, http.StatusUnauthorized, CodeUnauthorized, SubReauth)
			return
		}
		h(w, r)
	}
}

// bearer pulls the token out of the header, and accepts nothing else. Not a
// query parameter and not a cookie: a token in a URL ends up in an access log
// on a machine somebody else runs.
func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(h) <= len(prefix) || !strings.EqualFold(h[:len(prefix)], prefix) {
		return ""
	}
	return h[len(prefix):]
}

type loginBody struct {
	Proof string `json:"proof"`
}

type refreshBody struct {
	Refresh string `json:"refresh_token"`
}

// tokenResponse omits the refresh token when there is not a new one, which is
// every call to /api/auth/refresh. The refresh does not slide; see [Auth.Refresh].
type tokenResponse struct {
	Access    string `json:"access_token"`
	Refresh   string `json:"refresh_token,omitempty"`
	ExpiresIn int    `json:"expires_in"`
}

// login trades a proof for a pair of tokens.
//
// The order of the three brakes is the whole point, and it is the order the
// plan asked for after measuring: rate limit, growing delay, derivation slot.
// A throttled attempt never reaches Argon2id, and a queued one gives up with
// its request instead of holding a socket for work nobody is waiting on.
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if !s.auth.Closed() {
		respondError(w, http.StatusConflict, CodeSeedOpen, "")
		return
	}
	var c loginBody
	if !leerJSON(w, r, &c) {
		return
	}

	if err := s.slow.wait(r.Context()); err != nil {
		return
	}
	if err := acquireDerivation(r.Context()); err != nil {
		return
	}
	tokens, err := s.auth.Login(c.Proof)
	releaseDerivation()

	if err != nil {
		if errors.Is(err, ErrOpenSeed) {
			respondError(w, http.StatusConflict, CodeSeedOpen, "")
			return
		}
		s.slow.failed()
		// `sub` says what to DO and never what went wrong. The password is what
		// has to be typed again, and whether it was wrong or malformed is not
		// something to hand back to whoever is guessing.
		respondError(w, http.StatusUnauthorized, CodeUnauthorized, SubPassword)
		return
	}
	s.slow.succeeded()
	responder(w, http.StatusOK, tokenResponse{
		Access: tokens.Access, Refresh: tokens.Refresh, ExpiresIn: tokens.ExpiresIn,
	})
}

// refresh mints a new access token, and does NOT go through the derivation
// brake: verifying a refresh token is one HMAC, and there is nothing to guess
// at 128 bits of unforgeable MAC. The rate limit still applies, because an
// endpoint anybody can reach is an endpoint anybody can flood.
func (s *Server) refresh(w http.ResponseWriter, r *http.Request) {
	if !s.auth.Closed() {
		respondError(w, http.StatusConflict, CodeSeedOpen, "")
		return
	}
	var c refreshBody
	if !leerJSON(w, r, &c) {
		return
	}
	access, ttl, err := s.auth.Refresh(c.Refresh)
	if err != nil {
		// SubPassword and not SubReauth: refreshing is what just failed, so the
		// only move left is typing the password. Whether the token expired or
		// was never valid leads to the same place, which is why nothing here
		// tells them apart.
		respondError(w, http.StatusUnauthorized, CodeUnauthorized, SubPassword)
		return
	}
	responder(w, http.StatusOK, tokenResponse{Access: access, ExpiresIn: ttl})
}

func (s *Server) salud(w http.ResponseWriter, r *http.Request) {
	estado := map[string]any{"rooms": s.store.Len()}
	codigo := http.StatusOK
	if err := s.counter.Err(); err != nil {
		// Que EasyTier no conteste degrada el contador y nada más: resolver un
		// invite ID sigue funcionando, y entrar a una sala nunca pasó por acá.
		estado["counter"] = err.Error()
		codigo = http.StatusServiceUnavailable
	}
	responder(w, codigo, estado)
}

// ---- La página ------------------------------------------------------------

// estadoDeLaPagina es lo que se incrusta en el hueco de la página.
//
// # Por qué la versión y el repositorio viajan acá y no en una API
//
// Porque así la página **no consulta ninguna URL**. La versión costaba un
// `fetch("/api/version")`, y el repositorio estaba escrito dentro del HTML como
// una segunda copia de una constante de Go. Sirviéndolos con la página, no hay
// dominio que configurar para que sepa dónde mirar, y un fork cambia la
// constante en un solo sitio. A `connect-src` le queda un solo uso, resolver un
// invite ID, que es exactamente lo que la decisión 24 autoriza.
//
// `Room` es puntero para que su ausencia se distinga de una sala vacía: en las
// rutas que no son un invite ID vivo llega `null`, que es lo que la página lee
// como "no hay tarjeta que enseñar".
type estadoDeLaPagina struct {
	Room    *map[string]any `json:"room"`
	Version string          `json:"version,omitempty"`
	Repo    string          `json:"repo,omitempty"`
}

func (s *Server) servirPagina(w http.ResponseWriter, r *http.Request) {
	ruta := strings.Trim(r.URL.Path, "/")

	// El estado va SIEMPRE, y no solo en las rutas de invitación.
	//
	// Antes se inyectaba nada más cuando la ruta era un invite ID vivo, y la
	// versión y el repositorio los resolvía la propia página: la primera con un
	// `fetch("/api/version")`, el segundo con la cadena escrita dentro del
	// HTML. Las dos cosas se arreglan con el mismo hueco. La versión deja de
	// costar una petición, y el repositorio deja de estar copiado, así que un
	// fork que cambie la constante de Go ya no reparte una página que apunta al
	// original.
	estado := estadoDeLaPagina{
		Version: s.release.Latest(),
		Repo:    selfupdate.Repo,
	}
	if id, err := domain.ParseInviteID(ruta); err == nil {
		if sala, err := s.store.lookup(id); err == nil {
			v := s.vista(sala)
			estado.Room = &v
		}
	}

	html, err := s.page.Render(estado)
	if err != nil {
		http.Error(w, "página no disponible", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Sin caché: el contador y la tarjeta cambian, y una portada cacheada
	// mostrando una sala muerta es peor que un viaje de más.
	w.Header().Set("Cache-Control", "no-store")
	w.Write(html)
}

// ---- Plomería -------------------------------------------------------------

func cabecerasSeguras(siguiente http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// La misma CSP del nginx.conf, servida por quien tiene la última
		// palabra. connect-src 'self' y no 'none' porque la decisión 24
		// autoriza las peticiones a ESTE registro, y a ningún otro origen.
		// Es UNA: resolver un invite ID. La otra, preguntar la versión, se
		// fue al hueco de estado y ya no cuesta ninguna petición. Sigue en
		// esto a `api.github.com`; ver [Release].
		w.Header().Set("Content-Security-Policy",
			"default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; "+
				"connect-src 'self'; form-action 'none'; base-uri 'none'; frame-ancestors 'none'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		siguiente.ServeHTTP(w, r)
	})
}

func idDeRuta(w http.ResponseWriter, r *http.Request) (domain.InviteID, bool) {
	id, err := domain.ParseInviteID(r.PathValue("id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, CodeBadRequest, "")
		return domain.InviteID{}, false
	}
	return id, true
}

func leerJSON(w http.ResponseWriter, r *http.Request, destino any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(destino); err != nil {
		respondError(w, http.StatusBadRequest, CodeBadRequest, "")
		return false
	}
	return true
}

// ---- El sobre de error ----------------------------------------------------

// The wire codes. Every failure of this API is one of these, and NOTHING else
// travels with it.
//
// # Why the prose left
//
// The body used to be `{"error": "<texto en claro>"}`, which is the shape that
// cannot survive a password. A message that explains is a message that
// distinguishes, and on the auth path the distinction between "that token
// expired" and "that token was never valid" is free information for whoever is
// guessing. Rather than remembering to be careful on two routes out of six,
// there is nowhere left to put the sentence.
//
// The prose did not disappear from the product: it moved to the two human faces
// of the client, which know the language of the person reading and can say what
// to do about it. `--json` prints the code and nothing else, on purpose.
const (
	CodeBadRequest   = "bad_request"
	CodeNotFound     = "not_found"
	CodePinned       = "pinned"
	CodeBadSig       = "bad_sig"
	CodeCardTooBig   = "card_too_big"
	CodeExhausted    = "exhausted"
	CodeRateLimited  = "rate_limited"
	CodeUnauthorized = "unauthorized"
	CodeUnavailable  = "unavailable"
	CodeSeedOpen     = "seed_open"
)

// The sub-codes. `sub` says what the client should DO, never what went wrong.
//
// Two of them and not three, and the missing one is the point: there is no
// sub-code for "expired". A token that ran out and a token that was forged
// both lead to the same next move.
const (
	// SubReauth: refresh, and if that fails ask for the password.
	SubReauth = "reauth"
	// SubPassword: go straight to asking for the password. Refreshing is what
	// just failed, or was never possible.
	SubPassword = "password"
)

// wireError is the whole body of a failure.
type wireError struct {
	Code string `json:"code"`
	Sub  string `json:"sub,omitempty"`
}

func traducir(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		respondError(w, http.StatusNotFound, CodeNotFound, "")
	case errors.Is(err, ErrPinned):
		// 403 y no 409: el invite ID existe y no es tuyo. Es la respuesta a un
		// miembro que intenta sobrescribir la tarjeta del host.
		respondError(w, http.StatusForbidden, CodePinned, "")
	case errors.Is(err, ErrBadSig):
		respondError(w, http.StatusForbidden, CodeBadSig, "")
	case errors.Is(err, ErrCardTooBig):
		respondError(w, http.StatusRequestEntityTooLarge, CodeCardTooBig, "")
	case errors.Is(err, ErrExhausted):
		respondError(w, http.StatusServiceUnavailable, CodeExhausted, "")
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		// Nobody is listening. Writing a body here is work for a socket that is
		// already gone, and the status is only for the log of whatever proxy
		// happens to still be in the middle.
		w.WriteHeader(499)
	default:
		respondError(w, http.StatusServiceUnavailable, CodeUnavailable, "")
	}
}

func respondError(w http.ResponseWriter, codigo int, code, sub string) {
	responder(w, codigo, wireError{Code: code, Sub: sub})
}

func responder(w http.ResponseWriter, codigo int, cuerpo any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(codigo)
	json.NewEncoder(w).Encode(cuerpo)
}

func aB64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func deB64(s string) ([]byte, error) { return base64.RawURLEncoding.DecodeString(s) }

// ---- Límite de tasa -------------------------------------------------------

// limiter es un contador por IP con ventana fija.
//
// Es la defensa que reemplazó a los 60 bits de entropía del diseño anterior.
// Un invite ID son 40 bits: enumerable si se deja consultar sin freno, y
// perfectamente seguro con freno. Ver la decisión 24.
//
// Ventana fija y no token bucket porque acá el ataque a frenar es un barrido
// de millones de intentos, no una ráfaga de tres. La imprecisión en el borde
// de la ventana no cambia nada a esa escala, y el código que hay que revisar
// es la mitad.
type limiter struct {
	mu       sync.Mutex
	visto    map[string]*ventana
	tope     int
	duracion time.Duration
}

type ventana struct {
	n     int
	desde time.Time
}

func newLimiter(tope int, duracion time.Duration) *limiter {
	return &limiter{visto: map[string]*ventana{}, tope: tope, duracion: duracion}
}

func (l *limiter) permite(ip string, ahora time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	v, hay := l.visto[ip]
	if !hay || ahora.Sub(v.desde) > l.duracion {
		// Se aprovecha el paso para descartar ventanas viejas. Sin esto el mapa
		// crece con cada IP que aparezca una sola vez, que es su propia fuga.
		if len(l.visto) > 4096 {
			for k, w := range l.visto {
				if ahora.Sub(w.desde) > l.duracion {
					delete(l.visto, k)
				}
			}
		}
		l.visto[ip] = &ventana{n: 1, desde: ahora}
		return true
	}
	v.n++
	return v.n <= l.tope
}

func (s *Server) limitado(h http.HandlerFunc) http.HandlerFunc {
	return withLimit(s.limiter, h)
}

// authLimited is the same machinery with a much smaller number.
//
// Five a minute, against thirty for everything else. The general limit exists
// so that enumerating a 40-bit space is pointless; this one guards a password
// that the rule allows to be four characters long, and no honest operator types
// it five times in a minute.
func (s *Server) authLimited(h http.HandlerFunc) http.HandlerFunc {
	return withLimit(s.authLimiter, h)
}

func withLimit(l *limiter, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !l.permite(ipDe(r), time.Now()) {
			w.Header().Set("Retry-After", "60")
			respondError(w, http.StatusTooManyRequests, CodeRateLimited, "")
			return
		}
		h(w, r)
	}
}

// throttle is the global growing delay, and it is the half of the brute-force
// defence that a per-IP limit cannot cover.
//
// # Why global, when everything else counts per IP
//
// Because there are no accounts. With one shared password there is nobody to
// lock out, so a per-account block would be a denial of service against every
// host of the seed at once. What is left is per-IP counting, which a botnet
// walks straight around, plus a cost that grows with how wrong the guessing has
// been going overall.
//
// # Why it never closes all the way
//
// The cap is what keeps this from becoming the outage it is meant to prevent.
// An operator whose seed is under a guessing run still gets in, just slowly, and
// a run that has to wait [timing.MaxThrottle] between attempts is a run that will not
// finish. A success resets it, so a single fat-fingered password costs the next
// attempt a tenth of a second and nothing more.
type throttle struct {
	mu       sync.Mutex
	failures int
}

// wait sleeps for what the recent failures have earned. It returns the context
// error when the caller hangs up mid-wait, and the handler answers nothing:
// there is nobody left to answer.
func (t *throttle) wait(ctx context.Context) error {
	t.mu.Lock()
	n := t.failures
	t.mu.Unlock()

	wait := time.Duration(n) * timing.ThrottleStep
	if wait > timing.MaxThrottle {
		wait = timing.MaxThrottle
	}
	if wait == 0 {
		return nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (t *throttle) failed() {
	t.mu.Lock()
	defer t.mu.Unlock()
	// Capped where the delay caps, so a long run cannot bank a counter that
	// takes hours to drain once it stops.
	if t.failures < int(timing.MaxThrottle/timing.ThrottleStep) {
		t.failures++
	}
}

func (t *throttle) succeeded() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.failures = 0
}

// ipDe saca la IP del cliente.
//
// # X-Forwarded-For solo se cree desde loopback, y antes se creía siempre
//
// La suposición era que este proceso escucha únicamente en loopback detrás del
// nginx del droplet, que es quien pone la cabecera. **`--addr` es una bandera**,
// y con seeds que levanta cualquiera alguien lo va a publicar directo a
// internet. Con la cabecera creída sin condición, el freno de tasa se salta
// escribiéndola, y desde que hay password eso deja de ser diluir un límite de
// enumeración para ser saltarse el de fuerza bruta.
//
// La regla que queda: la cabecera vale cuando la conexión viene de loopback, o
// sea de un proxy que corre en esta misma máquina. Desde cualquier otro sitio se
// usa `RemoteAddr`, que es lo único que el otro extremo no puede escribir.
//
// Un proxy en OTRA máquina de la red interna deja de contar por IP real. Es la
// forma correcta de equivocarse: se agrupan visitantes que deberían separarse,
// que frena de más, en vez de separar a quien debería agruparse, que no frena
// nada.
func ipDe(r *http.Request) string {
	direct := peerIP(r.RemoteAddr)
	if !isLoopback(direct) {
		return direct
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i >= 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	return direct
}

func peerIP(remoto string) string {
	ip, _, err := net.SplitHostPort(remoto)
	if err != nil {
		return remoto
	}
	return ip
}

func isLoopback(ip string) bool {
	a, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}
	return a.Unmap().IsLoopback()
}
