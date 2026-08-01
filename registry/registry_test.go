package registry

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// ---- utilidades -----------------------------------------------------------

type host struct {
	pub  ed25519.PublicKey
	priv ed25519.PrivateKey
}

// nuevoHost produce una llave DETERMINISTA a partir de una semilla. Los tests
// no necesitan aleatoriedad real, y así un fallo se reproduce igual siempre.
func nuevoHost(t *testing.T, semilla byte) host {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(bytes.NewReader(bytes.Repeat([]byte{semilla}, 64)))
	if err != nil {
		t.Fatalf("generando llave: %v", err)
	}
	return host{pub: pub, priv: priv}
}

func (h host) firma(card []byte) []byte { return ed25519.Sign(h.priv, card) }

// relojFalso permite mover el tiempo a mano, que es la única forma honesta de
// probar TTLs sin dormir en un test.
type relojFalso struct{ t time.Time }

func (r *relojFalso) ahora() time.Time       { return r.t }
func (r *relojFalso) avanza(d time.Duration) { r.t = r.t.Add(d) }

func storeDePrueba(t *testing.T) (*Store, *relojFalso) {
	t.Helper()
	reloj := &relojFalso{t: time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)}
	// Aleatoriedad determinista para los invite IDs.
	fuente := rand.New(rand.NewSource(1))
	return NewStore(reloj.ahora, func() (domain.InviteID, error) {
		return domain.NewInviteID(fuente)
	}), reloj
}

// ---- el almacén -----------------------------------------------------------

func TestEmitirYResolver(t *testing.T) {
	s, _ := storeDePrueba(t)
	h := nuevoHost(t, 1)
	card := []byte("tarjeta cifrada")

	id, err := s.Issue(h.pub, card, h.firma(card))
	if err != nil {
		t.Fatalf("Issue falló: %v", err)
	}
	if len(id.Raw()) != domain.InviteIDLen {
		t.Errorf("el invite ID emitido mide %d, se esperaban %d", len(id.Raw()), domain.InviteIDLen)
	}

	sala, err := s.Lookup(id)
	if err != nil {
		t.Fatalf("Lookup falló: %v", err)
	}
	if !bytes.Equal(sala.Card, card) {
		t.Errorf("la tarjeta volvió distinta: %q", sala.Card)
	}
	if want := domain.DeriveRendezvous(id).NetworkName(); sala.Network != want {
		t.Errorf("red = %q, se esperaba %q: el registro tiene que derivarla, no creerle al host", sala.Network, want)
	}
}

// TestElRegistroNoPuedeLeerLaTarjeta es el test que sostiene la propiedad que
// justifica todo el diseño de la decisión 24: el operador del seed guarda y
// sirve el nombre de una sala sin poder leerlo.
func TestElRegistroNoPuedeLeerLaTarjeta(t *testing.T) {
	s, _ := storeDePrueba(t)
	h := nuevoHost(t, 1)
	// Lo que el host publica de verdad son bytes cifrados. El registro no puede
	// distinguirlos de ruido, y este test comprueba que ni lo intenta: no hay
	// ningún camino en el que el nombre de la sala aparezca en claro.
	card := []byte{0x9f, 0x00, 0x1b, 0xff, 0x42}

	id, _ := s.Issue(h.pub, card, h.firma(card))
	sala, _ := s.Lookup(id)

	if !bytes.Equal(sala.Card, card) {
		t.Fatal("la tarjeta se transformó: el registro debe tratarla como bytes opacos")
	}
	if strings.Contains(sala.Network, "La Guarida") {
		t.Fatal("el nombre de la sala se filtró al identificador de red")
	}
}

func TestFirmaInvalidaSeRechaza(t *testing.T) {
	s, _ := storeDePrueba(t)
	h := nuevoHost(t, 1)
	otro := nuevoHost(t, 2)
	card := []byte("tarjeta")

	if _, err := s.Issue(h.pub, card, otro.firma(card)); err != ErrBadSig {
		t.Errorf("Issue con firma ajena dio %v, se esperaba ErrBadSig", err)
	}
	if _, err := s.Issue(h.pub, card, []byte("no es una firma")); err != ErrBadSig {
		t.Errorf("Issue con firma basura dio %v, se esperaba ErrBadSig", err)
	}
	// Firma válida sobre OTRA tarjeta: es el caso de reusar una firma vista.
	if _, err := s.Issue(h.pub, []byte("otra cosa"), h.firma(card)); err != ErrBadSig {
		t.Errorf("Issue con firma de otra tarjeta dio %v, se esperaba ErrBadSig", err)
	}
}

// TestUnMiembroNoPuedeSobrescribirLaTarjeta es el ataque concreto que la firma
// existe para parar.
//
// La clave que cifra la tarjeta se deriva del enlace, así que TODO el que
// recibió el enlace puede producir una tarjeta que la página descifra. El
// registro es HTTP plano y no conoce la sala, o sea que no tiene forma de
// distinguir a un miembro del host. Lo único que puede exigir es la firma de
// la llave que fijó ese invite ID la primera vez.
func TestUnMiembroNoPuedeSobrescribirLaTarjeta(t *testing.T) {
	s, _ := storeDePrueba(t)
	humberto := nuevoHost(t, 1)
	santiago := nuevoHost(t, 2)

	card := []byte("la de Humberto")
	id, err := s.Issue(humberto.pub, card, humberto.firma(card))
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	suya := []byte("la de Santiago")
	if err := s.Publish(id, santiago.pub, suya, santiago.firma(suya)); err != ErrPinned {
		t.Fatalf("Santiago pudo publicar con su propia llave: %v", err)
	}

	sala, _ := s.Lookup(id)
	if !bytes.Equal(sala.Card, card) {
		t.Errorf("la tarjeta cambió a %q: el fijado no está protegiendo nada", sala.Card)
	}

	// Y el host sí puede, que es la otra mitad de la propiedad.
	nueva := []byte("Humberto renombró la sala")
	if err := s.Publish(id, humberto.pub, nueva, humberto.firma(nueva)); err != nil {
		t.Fatalf("el host no pudo actualizar su propia tarjeta: %v", err)
	}
}

// TestReabrirConElMismoIDSigueSiendoDelHost cubre la asimetría de TTLs, que es
// la parte del diseño más fácil de romper sin notarlo.
func TestReabrirConElMismoIDSigueSiendoDelHost(t *testing.T) {
	s, reloj := storeDePrueba(t)
	humberto := nuevoHost(t, 1)
	exMiembro := nuevoHost(t, 2)

	card := []byte("La Guarida")
	id, _ := s.Issue(humberto.pub, card, humberto.firma(card))

	// La sala muere: la tarjeta caduca y deja de resolverse.
	reloj.avanza(CardTTL + time.Minute)
	if _, err := s.Lookup(id); err != ErrNotFound {
		t.Fatalf("la tarjeta caducada sigue resolviéndose: %v", err)
	}

	// Un ex miembro conserva el invite ID y se adelanta al host. No puede.
	suya := []byte("mía ahora")
	if err := s.Publish(id, exMiembro.pub, suya, exMiembro.firma(suya)); err != ErrPinned {
		t.Fatalf("un ex miembro se apropió del invite ID: %v", err)
	}

	// El host reabre con el mismo ID, que es lo prometido.
	if err := s.Publish(id, humberto.pub, card, humberto.firma(card)); err != nil {
		t.Fatalf("el host no pudo reabrir su sala: %v", err)
	}
	if _, err := s.Lookup(id); err != nil {
		t.Fatalf("tras reabrir, la sala no resuelve: %v", err)
	}
}

func TestElFijadoTambienCaduca(t *testing.T) {
	s, reloj := storeDePrueba(t)
	h := nuevoHost(t, 1)
	card := []byte("x")
	id, _ := s.Issue(h.pub, card, h.firma(card))

	reloj.avanza(PinTTL + time.Hour)
	if n := s.Sweep(); n != 1 {
		t.Errorf("Sweep descartó %d salas, se esperaba 1", n)
	}
	if s.Len() != 0 {
		t.Errorf("quedan %d entradas tras el barrido", s.Len())
	}
	// Y el invite ID vuelve a estar libre para cualquiera.
	if err := s.Publish(id, h.pub, card, h.firma(card)); err != ErrNotFound {
		t.Errorf("tras caducar el fijado, Publish dio %v, se esperaba ErrNotFound", err)
	}
}

func TestTarjetaDemasiadoGrande(t *testing.T) {
	s, _ := storeDePrueba(t)
	h := nuevoHost(t, 1)
	card := bytes.Repeat([]byte("A"), MaxCardBytes+1)
	if _, err := s.Issue(h.pub, card, h.firma(card)); err != ErrCardTooBig {
		t.Errorf("Issue con tarjeta enorme dio %v, se esperaba ErrCardTooBig", err)
	}
}

func TestNetworksSoloListaSalasVivas(t *testing.T) {
	s, reloj := storeDePrueba(t)
	h := nuevoHost(t, 1)
	card := []byte("x")
	s.Issue(h.pub, card, h.firma(card))

	if got := len(s.Networks()); got != 1 {
		t.Fatalf("Networks devolvió %d, se esperaba 1", got)
	}
	reloj.avanza(CardTTL + time.Minute)
	if got := len(s.Networks()); got != 0 {
		t.Errorf("Networks devolvió %d tras caducar la tarjeta, se esperaba 0", got)
	}
}

// ---- la página ------------------------------------------------------------

func paginaDePrueba(t *testing.T) *Page {
	t.Helper()
	p, err := NewPage(filepath.Join("..", "invite", "index.html"))
	if err != nil {
		t.Fatalf("cargando la página real: %v", err)
	}
	return p
}

// TestLaPaginaRealTieneElHueco ata el binario al HTML. Si alguien quita el
// bloque de estado al editar la página, el registro deja de poder resolver en
// el servidor, y sin este test el síntoma sería un parpadeo en producción.
func TestLaPaginaRealTieneElHueco(t *testing.T) {
	p := paginaDePrueba(t)

	vacia, err := p.Render(nil)
	if err != nil {
		t.Fatalf("Render(nil): %v", err)
	}
	if !bytes.Contains(vacia, []byte(MarcaAbre+"null"+MarcaCierra)) {
		t.Error("sin estado, el hueco debe quedar en null para que la página lo pida por su cuenta")
	}

	con, err := p.Render(map[string]any{"members": 4})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !bytes.Contains(con, []byte(`{"members":4}`)) {
		t.Error("el estado no se incrustó en el hueco")
	}
}

// TestElEstadoIncrustadoNoPuedeRomperElBloque cubre una inyección real: la
// tarjeta la escribió un desconocido y termina dentro de un <script>.
func TestElEstadoIncrustadoNoPuedeRomperElBloque(t *testing.T) {
	p := paginaDePrueba(t)
	veneno := map[string]any{"card": "</script><script>alert(1)</script>"}

	html, err := p.Render(veneno)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	// El cierre inyectado no puede aparecer literal. El legítimo del propio
	// bloque sí, así que se cuentan las apariciones ANTES del hueco de cierre.
	i := bytes.Index(html, []byte(MarcaAbre))
	j := bytes.Index(html[i:], []byte(MarcaCierra))
	dentro := html[i+len(MarcaAbre) : i+j]
	if bytes.Contains(bytes.ToLower(dentro), []byte("</script")) {
		t.Fatalf("el estado incrustado cierra el bloque: %q", dentro)
	}
	if !bytes.Contains(dentro, []byte(`\u003c`)) {
		t.Error("los signos de menor que deberían quedar escapados")
	}
}

func TestPaginaSinHuecoFallaAlArrancar(t *testing.T) {
	ruta := filepath.Join(t.TempDir(), "index.html")
	os.WriteFile(ruta, []byte("<html><body>sin hueco</body></html>"), 0o600)
	if _, err := NewPage(ruta); err == nil {
		t.Fatal("NewPage aceptó una página sin el hueco: tiene que fallar al arrancar, no en producción")
	}
}

// ---- la API ---------------------------------------------------------------

func servidorDePrueba(t *testing.T) (*Server, *Store) {
	t.Helper()
	s, _ := storeDePrueba(t)
	// Un contador que jamás habló con EasyTier: es el estado real al arrancar,
	// y el que revela si la API publica un cero falso.
	return NewServer(s, NewCounter("no-existe", "127.0.0.1:1"), paginaDePrueba(t)), s
}

func TestAPIResuelveYOmiteElContadorSiNoLoSabe(t *testing.T) {
	srv, store := servidorDePrueba(t)
	h := nuevoHost(t, 1)
	card := []byte("tarjeta")
	id, _ := store.Issue(h.pub, card, h.firma(card))

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/i/"+id.String(), nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("código %d: %s", rec.Code, rec.Body)
	}
	var cuerpo map[string]any
	json.Unmarshal(rec.Body.Bytes(), &cuerpo)

	if _, hay := cuerpo["members"]; hay {
		t.Error("members está presente sin haber hablado con EasyTier: un cero es la afirmación " +
			"falsa 'no hay nadie', y ausente dice la verdad 'no lo sé'")
	}
	if cuerpo["card"] != base64.RawURLEncoding.EncodeToString(card) {
		t.Errorf("card = %v", cuerpo["card"])
	}
}

func TestAPIRechazaEntradaHostil(t *testing.T) {
	srv, _ := servidorDePrueba(t)
	casos := []struct {
		nombre string
		metodo string
		ruta   string
		cuerpo string
		quiero int
	}{
		{"invite ID con forma inválida", http.MethodGet, "/api/i/NOPE", "", http.StatusBadRequest},
		{"invite ID del largo viejo", http.MethodGet, "/api/i/KANP7X4MB2QF", "", http.StatusBadRequest},
		{"invite ID válido, sala inexistente", http.MethodGet, "/api/i/A7K2M9QX", "", http.StatusNotFound},
		{"cuerpo que no es JSON", http.MethodPost, "/api/rooms", "{", http.StatusBadRequest},
		{"campos desconocidos", http.MethodPost, "/api/rooms", `{"host_key":"AA","sorpresa":1}`, http.StatusBadRequest},
		{"llave que no es Ed25519", http.MethodPost, "/api/rooms", `{"host_key":"AA","card":"AA","sig":"AA"}`, http.StatusBadRequest},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			r := httptest.NewRequest(c.metodo, c.ruta, strings.NewReader(c.cuerpo))
			rec := httptest.NewRecorder()
			srv.Handler().ServeHTTP(rec, r)
			if rec.Code != c.quiero {
				t.Errorf("código %d, se esperaba %d. Cuerpo: %s", rec.Code, c.quiero, rec.Body)
			}
		})
	}
}

func TestAPIEmitePorHTTP(t *testing.T) {
	srv, _ := servidorDePrueba(t)
	h := nuevoHost(t, 1)
	card := []byte("tarjeta")

	cuerpo, _ := json.Marshal(cuerpoPublicar{
		HostKey: aB64(h.pub), Card: aB64(card), Sig: aB64(h.firma(card)),
	})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/rooms", bytes.NewReader(cuerpo)))

	if rec.Code != http.StatusCreated {
		t.Fatalf("código %d: %s", rec.Code, rec.Body)
	}
	var res struct {
		InviteID string `json:"invite_id"`
	}
	json.Unmarshal(rec.Body.Bytes(), &res)
	if _, err := domain.ParseInviteID(res.InviteID); err != nil {
		t.Errorf("el invite ID emitido no parsea: %q, %v", res.InviteID, err)
	}
}

// TestLaRutaDeUnaSalaVivaLlegaResuelta comprueba la promesa de la decisión 17,
// que la página llega armada y sin depender de un viaje de ida y vuelta.
func TestLaRutaDeUnaSalaVivaLlegaResuelta(t *testing.T) {
	srv, store := servidorDePrueba(t)
	h := nuevoHost(t, 1)
	card := []byte("tarjeta")
	id, _ := store.Issue(h.pub, card, h.firma(card))

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/"+id.String(), nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("código %d", rec.Code)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(aB64(card))) {
		t.Error("la ruta de una sala viva debería traer su tarjeta ya incrustada")
	}

	// Una ruta que no es una sala viva sirve la misma página con el hueco en
	// null, para que el JavaScript decida. Nunca un 404 de servidor: la página
	// explica mejor qué pasó de lo que puede hacerlo un código de estado.
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/A7K2M9QZ", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("una sala inexistente devolvió %d, la página debe servirse igual", rec.Code)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(MarcaAbre+"null")) {
		t.Error("sin sala viva, el hueco debe quedar en null")
	}
}

func TestLimiteDeTasa(t *testing.T) {
	// Es la defensa que reemplazó a los 60 bits de entropía del diseño
	// anterior: 40 bits son enumerables sin freno y seguros con freno.
	l := newLimiter(3, time.Minute)
	base := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)

	for i := 0; i < 3; i++ {
		if !l.permite("1.2.3.4", base) {
			t.Fatalf("la petición %d debió pasar", i+1)
		}
	}
	if l.permite("1.2.3.4", base) {
		t.Error("la cuarta petición debió frenarse")
	}
	if !l.permite("5.6.7.8", base) {
		t.Error("el freno debe ser por IP, no global")
	}
	if !l.permite("1.2.3.4", base.Add(2*time.Minute)) {
		t.Error("pasada la ventana, la misma IP vuelve a pasar")
	}
}

func TestCabecerasDeSeguridad(t *testing.T) {
	srv, _ := servidorDePrueba(t)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	csp := rec.Header().Get("Content-Security-Policy")
	for _, quiero := range []string{"default-src 'none'", "connect-src 'self'", "frame-ancestors 'none'"} {
		if !strings.Contains(csp, quiero) {
			t.Errorf("la CSP no lleva %q: %s", quiero, csp)
		}
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("falta nosniff")
	}
}
