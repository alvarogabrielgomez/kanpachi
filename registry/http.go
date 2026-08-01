package registry

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
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
	limiter *limiter
}

func NewServer(s *Store, c *Counter, p *Page) *Server {
	return &Server{store: s, counter: c, page: p, limiter: newLimiter(30, time.Minute)}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/rooms", s.limitado(s.emitir))
	mux.HandleFunc("GET /api/i/{id}", s.limitado(s.resolver))
	mux.HandleFunc("PUT /api/i/{id}", s.limitado(s.publicar))
	mux.HandleFunc("GET /healthz", s.salud)
	mux.HandleFunc("/", s.servirPagina)
	return cabecerasSeguras(mux)
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
		fallo(w, http.StatusBadRequest, err.Error())
		return
	}
	id, err := s.store.Issue(llave, tarjeta, firma)
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
		fallo(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.store.Publish(id, llave, tarjeta, firma); err != nil {
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
	sala, err := s.store.Lookup(id)
	if err != nil {
		traducir(w, err)
		return
	}
	responder(w, http.StatusOK, s.vista(sala))
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
	// El contador se omite si nunca se pudo hablar con EasyTier. Un cero sería
	// una afirmación falsa, "no hay nadie"; ausente dice la verdad, "no lo sé".
	if n, ok := s.counter.For(sala.Network); ok {
		v["members"] = n
	}
	return v
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

func (s *Server) servirPagina(w http.ResponseWriter, r *http.Request) {
	ruta := strings.Trim(r.URL.Path, "/")

	// Cualquier ruta sirve la misma página, que lee el invite ID de la URL y
	// decide qué mostrar. Con una excepción: si la ruta ES un invite ID vivo,
	// se le inyecta el estado ya resuelto, así la tarjeta llega armada y sin
	// una petición de ida y vuelta.
	var estado any
	if id, err := domain.ParseInviteID(ruta); err == nil {
		if sala, err := s.store.Lookup(id); err == nil {
			estado = s.vista(sala)
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
		// autoriza exactamente una petición, la de este registro.
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
		fallo(w, http.StatusBadRequest, "eso no tiene forma de invite ID")
		return domain.InviteID{}, false
	}
	return id, true
}

func leerJSON(w http.ResponseWriter, r *http.Request, destino any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(destino); err != nil {
		fallo(w, http.StatusBadRequest, "cuerpo inválido")
		return false
	}
	return true
}

func traducir(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		fallo(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrPinned):
		// 403 y no 409: el invite ID existe y no es tuyo. Es la respuesta a un
		// miembro que intenta sobrescribir la tarjeta del host.
		fallo(w, http.StatusForbidden, err.Error())
	case errors.Is(err, ErrBadSig):
		fallo(w, http.StatusForbidden, err.Error())
	case errors.Is(err, ErrCardTooBig):
		fallo(w, http.StatusRequestEntityTooLarge, err.Error())
	default:
		fallo(w, http.StatusServiceUnavailable, "el registro no pudo atender eso")
	}
}

func fallo(w http.ResponseWriter, codigo int, mensaje string) {
	responder(w, codigo, map[string]string{"error": mensaje})
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
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.limiter.permite(ipDe(r), time.Now()) {
			w.Header().Set("Retry-After", "60")
			fallo(w, http.StatusTooManyRequests, "demasiadas consultas")
			return
		}
		h(w, r)
	}
}

// ipDe saca la IP del cliente. Confía en X-Forwarded-For porque este proceso
// SOLO escucha en loopback, detrás del nginx del droplet, que es quien la
// pone. Publicarlo directo a internet invalidaría esta suposición y el límite
// de tasa entero, así que el bind por defecto es 127.0.0.1 a propósito.
func ipDe(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i >= 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}
