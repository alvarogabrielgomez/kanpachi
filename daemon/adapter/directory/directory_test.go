package directory

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// enrutable es una dirección pública de verdad.
//
// NO se pueden usar acá los rangos de documentación, `203.0.113.0/24` y los
// suyos: `domain.CheckSeedAddr` los rechaza, que es justo lo que estos tests
// ejercitan del otro lado. Misma constante y misma lección que `spec_test.go`.
const enrutable = "93.184.216.34"

type logMudo struct{}

func (logMudo) Info(string, ...any)  {}
func (logMudo) Warn(string, ...any)  {}
func (logMudo) Error(string, ...any) {}

func protectorMudo(string) error { return nil }

// banco arma un adaptador contra un servidor de prueba, por el camino de
// producción entero: el nombre se resuelve, cada dirección pasa por
// `CheckSeedAddr`, y solo entonces el dial va al servidor.
type banco struct {
	dir       *Directory
	srv       *httptest.Server
	resueltas []string // los nombres que se pidió resolver
	marcadas  []string // lo que recibió el dial, que tiene que ser SIEMPRE una IP
	mu        sync.Mutex
}

func nuevoBanco(t *testing.T, h http.HandlerFunc) *banco {
	t.Helper()
	return nuevoBancoResolviendo(t, h, enrutable)
}

func nuevoBancoResolviendo(t *testing.T, h http.HandlerFunc, ips ...string) *banco {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	b := &banco{srv: srv}
	dir, err := New(Deps{
		DataDir: t.TempDir(),
		Seed:    "seed.ejemplo",
		Log:     logMudo{},
		Protect: protectorMudo,
		Resolve: func(host string) ([]netip.Addr, error) {
			b.mu.Lock()
			b.resueltas = append(b.resueltas, host)
			b.mu.Unlock()
			var out []netip.Addr
			for _, s := range ips {
				a, err := netip.ParseAddr(s)
				if err != nil {
					return nil, err
				}
				out = append(out, a)
			}
			return out, nil
		},
		base: "http://seed.ejemplo",
		connect: func(ctx context.Context, network, addr string) (net.Conn, error) {
			b.mu.Lock()
			b.marcadas = append(b.marcadas, addr)
			b.mu.Unlock()
			// La dirección ya comprobada se cambia por la del servidor de
			// prueba. Lo que se afirma es QUÉ recibió esta función, no a dónde
			// terminó el socket.
			var d net.Dialer
			return d.DialContext(ctx, network, strings.TrimPrefix(srv.URL, "http://"))
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	b.dir = dir
	return b
}

func (b *banco) loMarcado() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.marcadas...)
}

// U1. Un seed que resuelve a la red de casa no se marca NUNCA.
//
// Es el caso que `CheckSeedAddr` existe para cerrar: nada impide registrar un
// dominio cuyo registro A apunte a `192.168.1.1`, y este proceso corre como
// SYSTEM. Lo que se afirma no es que falle, es que el dial no llegó a ocurrir.
func TestUnSeedQueResuelveALaRedDeCasaNoSeMarca(t *testing.T) {
	casos := []string{"192.168.1.1", "127.0.0.1", "169.254.169.254", "::ffff:10.0.0.1", "100.64.1.1"}
	for _, ip := range casos {
		t.Run(ip, func(t *testing.T) {
			b := nuevoBancoResolviendo(t, func(w http.ResponseWriter, r *http.Request) {
				t.Error("se llegó a hablar con el servidor")
			}, ip)
			if _, err := b.dir.Open(context.Background(), []byte("tarjeta")); err == nil {
				t.Fatalf("se aceptó un seed que resuelve a %s", ip)
			}
			if n := len(b.loMarcado()); n != 0 {
				t.Errorf("se marcó %d vez/veces a una dirección prohibida", n)
			}
		})
	}
}

// U2. Un seed MIXTO conserva las buenas en vez de descartarse entero.
//
// Descartarlo entero dejaría sin tarjeta a quien tenga un DNS con split
// horizon, que es una configuración normal en una casa.
func TestUnSeedMixtoMarcaSoloALasDireccionesBuenas(t *testing.T) {
	b := nuevoBancoResolviendo(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{"invite_id": "A7K2-M9QX"})
	}, "10.0.0.5", enrutable)

	if _, err := b.dir.Open(context.Background(), []byte("tarjeta")); err != nil {
		t.Fatal(err)
	}
	marcadas := b.loMarcado()
	if len(marcadas) != 1 {
		t.Fatalf("se marcó %d vez/veces: %v", len(marcadas), marcadas)
	}
	if !strings.HasPrefix(marcadas[0], enrutable+":") {
		t.Errorf("se marcó %q, y la única dirección buena era %s", marcadas[0], enrutable)
	}
}

// U3. Sin ninguna dirección utilizable, el error NOMBRA los rechazos.
//
// "no se pudo" a secas manda a alguien a mirar su router. El motivo por
// dirección manda a mirar su DNS, que es donde está el problema.
func TestSinDirecciónUtilizableElErrorNombraLosRechazos(t *testing.T) {
	b := nuevoBancoResolviendo(t, func(http.ResponseWriter, *http.Request) {}, "192.168.1.1")
	_, err := b.dir.Open(context.Background(), []byte("tarjeta"))
	if err == nil {
		t.Fatal("no falló")
	}
	if !strings.Contains(err.Error(), "192.168.1.1") {
		t.Errorf("el error no dice qué dirección se rechazó: %v", err)
	}
}

// U14. El dial recibe la IP YA COMPROBADA y jamás el nombre.
//
// Si el nombre llegara al transporte, lo resolvería él por su cuenta y la
// comprobación no gobernaría nada: entre nuestra consulta y la suya el DNS
// puede contestar otra cosa, y esa otra cosa puede ser la LAN de la casa.
func TestElDialRecibeLaDirecciónComprobadaYNoElNombre(t *testing.T) {
	b := nuevoBanco(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{"invite_id": "A7K2-M9QX"})
	})
	if _, err := b.dir.Open(context.Background(), []byte("tarjeta")); err != nil {
		t.Fatal(err)
	}
	for _, m := range b.loMarcado() {
		host, _, err := net.SplitHostPort(m)
		if err != nil {
			t.Fatalf("el dial recibió %q, que no es dirección y puerto", m)
		}
		if _, err := netip.ParseAddr(host); err != nil {
			t.Errorf("el dial recibió el NOMBRE %q, así que el transporte lo resolvería "+
				"otra vez y la comprobación no gobierna nada", host)
		}
	}
}

// U4. Una redirección no se sigue, y el destino ni siquiera se resuelve.
func TestUnaRedirecciónNoSeSigue(t *testing.T) {
	b := nuevoBanco(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://otro.ejemplo/api/rooms", http.StatusFound)
	})
	if _, err := b.dir.Open(context.Background(), []byte("tarjeta")); err == nil {
		t.Fatal("se siguió una redirección")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, h := range b.resueltas {
		if h == "otro.ejemplo" {
			t.Error("se resolvió el destino de la redirección")
		}
	}
}

// U5. Una respuesta gigante se CORTA, y cortarse es lo que hay que afirmar.
//
// # Por qué no alcanza con comprobar que da error
//
// La primera versión de este test solo miraba el error, y su veneno no mordía:
// quitando el `LimitReader` el cliente se tragaba los ocho megas enteros y
// DESPUÉS los medía, así que devolvía el mismo error y el test daba verde sobre
// un cliente que ya se había comido lo que decía rechazar. El daño de una
// respuesta sin tope no es el valor que se devuelve, es la memoria que se
// reservó para llegar hasta él.
//
// Así que se mide del lado del servidor: cuántos bytes le ACEPTARON. Con tope,
// el cliente deja de leer y el servidor se queda escribiendo contra una tubería
// que se cierra; sin tope, escribe los ocho megas sin despeinarse.
func TestLaRespuestaGiganteSeCorta(t *testing.T) {
	const total = 8 << 20
	const trozo = 32 << 10
	var escritos int64
	listo := make(chan struct{})

	b := nuevoBanco(t, func(w http.ResponseWriter, r *http.Request) {
		defer close(listo)
		w.Header().Set("Content-Length", fmt.Sprint(total))
		w.WriteHeader(http.StatusOK)
		buf := make([]byte, trozo)
		for i := 0; i < total/trozo; i++ {
			n, err := w.Write(buf)
			atomic.AddInt64(&escritos, int64(n))
			if err != nil {
				return
			}
		}
	})

	_, err := b.dir.Lookup(context.Background(), idDePrueba(t))
	if err == nil {
		t.Fatal("se aceptó una respuesta de ocho megas")
	}
	if !strings.Contains(err.Error(), "bytes") {
		t.Errorf("el error no habla del tope: %v", err)
	}
	<-listo
	if n := atomic.LoadInt64(&escritos); n >= total {
		t.Errorf("el servidor colocó los %d bytes enteros, o sea que el cliente se los "+
			"tragó antes de medirlos. El tope tiene que cortar la LECTURA, no juzgarla después", n)
	}
}

// U6. `members` ausente es -1 y presente es el número.
//
// Un cero afirmaría "no hay nadie" y sería falso: el registro omite el campo
// cuando nunca pudo hablar con el motor, o sea cuando no lo sabe.
func TestMembersAusenteEsMenosUnoYPresenteEsElNúmero(t *testing.T) {
	casos := []struct {
		nombre string
		cuerpo map[string]any
		quiero int
	}{
		{"ausente", map[string]any{"card": b64([]byte("x")), "host_key": b64([]byte("k"))}, -1},
		{"presente", map[string]any{"card": b64([]byte("x")), "host_key": b64([]byte("k")), "members": 3}, 3},
		{"cero de verdad", map[string]any{"card": b64([]byte("x")), "host_key": b64([]byte("k")), "members": 0}, 0},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			b := nuevoBanco(t, func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(c.cuerpo)
			})
			vista, err := b.dir.Lookup(context.Background(), idDePrueba(t))
			if err != nil {
				t.Fatal(err)
			}
			if vista.Members != c.quiero {
				t.Errorf("miembros = %d, se esperaba %d", vista.Members, c.quiero)
			}
		})
	}
}

// U7. Un 429 no se reintenta.
//
// El límite de tasa del registro cuenta TAMBIÉN las peticiones que fallan, así
// que reintentar al chocar con él es la forma de convertir un tropiezo en una
// puerta cerrada durante un minuto.
func TestElLímiteDeTasaNoSeReintenta(t *testing.T) {
	var veces int
	b := nuevoBanco(t, func(w http.ResponseWriter, r *http.Request) {
		veces++
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]any{"error": "demasiadas consultas"})
	})
	if _, err := b.dir.Open(context.Background(), []byte("tarjeta")); err == nil {
		t.Fatal("un 429 no dio error")
	}
	if veces != 1 {
		t.Errorf("se pidió %d veces, y un 429 no se reintenta", veces)
	}
}

// U10. Un campo desconocido en la respuesta es un error.
//
// Las dos puntas viven en este repositorio y el test de contrato las sostiene
// juntas, así que estricto acá no es frágil: es lo que hace visible en CI que
// una punta se movió sin la otra.
func TestUnCampoDesconocidoEnLaRespuestaEsError(t *testing.T) {
	b := nuevoBanco(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{"invite_id": "A7K2-M9QX", "sorpresa": 1})
	})
	if _, err := b.dir.Open(context.Background(), []byte("tarjeta")); err == nil {
		t.Fatal("se aceptó una respuesta con un campo que nadie declaró")
	}
}

// U12. Sin `base` de prueba, el esquema es https y el TLS se verifica.
func TestLaBasePorDefectoEsHttpsYElTLSSeVerifica(t *testing.T) {
	d, err := New(Deps{
		DataDir: t.TempDir(),
		Seed:    "kanpachi.ejemplo",
		Log:     logMudo{},
		Protect: protectorMudo,
	})
	if err != nil {
		t.Fatal(err)
	}
	if d.base != "https://kanpachi.ejemplo" {
		t.Errorf("la base es %q, y tiene que ser https contra el nombre del seed", d.base)
	}
	tr, ok := d.client.Transport.(*http.Transport)
	if !ok {
		t.Fatal("el transporte no es un *http.Transport")
	}
	if tr.Proxy != nil {
		t.Error("hay proxy configurado, y una variable de entorno no elige a dónde marca SYSTEM")
	}
	if cfg := tr.TLSClientConfig; cfg != nil && cfg.InsecureSkipVerify {
		t.Error("el TLS no se verifica")
	}
}

// U13. Dos caminos al primer uso dejan UNA sola llave.
//
// Sin el candado, los dos encuentran que no hay fichero, los dos generan, y la
// que queda en disco no es la que ya firmó: el registro fijaría una llave que
// esta máquina no tiene.
func TestDosCaminosAlPrimerUsoDejanUnaSolaLlave(t *testing.T) {
	var mu sync.Mutex
	vistas := map[string]bool{}
	b := nuevoBanco(t, func(w http.ResponseWriter, r *http.Request) {
		var c publishBody
		json.NewDecoder(r.Body).Decode(&c)
		mu.Lock()
		vistas[c.HostKey] = true
		mu.Unlock()
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{"invite_id": "A7K2-M9QX"})
	})

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b.dir.Open(context.Background(), []byte("tarjeta"))
		}()
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if len(vistas) != 1 {
		t.Errorf("salieron %d llaves distintas al cable", len(vistas))
	}
	entradas, err := os.ReadDir(b.dir.deps.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entradas) != 1 {
		t.Errorf("quedaron %d ficheros en el directorio de datos", len(entradas))
	}
}

// La llave sobrevive al reinicio del daemon: un adaptador nuevo sobre el mismo
// directorio manda la MISMA llave pública.
func TestUnAdaptadorNuevoSobreElMismoDirectorioMandaLaMismaLlave(t *testing.T) {
	datos := t.TempDir()
	var vistas []string
	var mu sync.Mutex

	hacerYPedir := func() {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var c publishBody
			json.NewDecoder(r.Body).Decode(&c)
			mu.Lock()
			vistas = append(vistas, c.HostKey)
			mu.Unlock()
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]any{"invite_id": "A7K2-M9QX"})
		}))
		defer srv.Close()

		d, err := New(Deps{
			DataDir: datos, Seed: "seed.ejemplo", Log: logMudo{}, Protect: protectorMudo,
			Resolve: func(string) ([]netip.Addr, error) {
				return []netip.Addr{netip.MustParseAddr(enrutable)}, nil
			},
			base: "http://seed.ejemplo",
			connect: func(ctx context.Context, network, addr string) (net.Conn, error) {
				var dl net.Dialer
				return dl.DialContext(ctx, network, strings.TrimPrefix(srv.URL, "http://"))
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := d.Open(context.Background(), []byte("tarjeta")); err != nil {
			t.Fatal(err)
		}
	}

	hacerYPedir()
	hacerYPedir()

	if len(vistas) != 2 || vistas[0] != vistas[1] {
		t.Errorf("el reinicio cambió la llave: %v", vistas)
	}
	if _, err := os.Stat(filepath.Join(datos, "identity.key")); err != nil {
		t.Errorf("no quedó llave en disco: %v", err)
	}
}

// New exige lo que no puede inventar. Un protector por omisión escribiría la
// llave con la ACL heredada del directorio, que da lectura a todo el mundo.
func TestNewExigeLoQueNoPuedeInventar(t *testing.T) {
	base := func() Deps {
		return Deps{DataDir: t.TempDir(), Seed: "s.ejemplo", Log: logMudo{}, Protect: protectorMudo}
	}
	casos := map[string]func(*Deps){
		"sin directorio": func(d *Deps) { d.DataDir = "" },
		"sin seed":       func(d *Deps) { d.Seed = "" },
		"sin log":        func(d *Deps) { d.Log = nil },
		"sin protector":  func(d *Deps) { d.Protect = nil },
	}
	for nombre, romper := range casos {
		t.Run(nombre, func(t *testing.T) {
			d := base()
			romper(&d)
			if _, err := New(d); err == nil {
				t.Errorf("se construyó %s", nombre)
			}
		})
	}
}

func idDePrueba(t *testing.T) domain.InviteID {
	t.Helper()
	id, err := domain.ParseInviteID("A7K2-M9QX")
	if err != nil {
		t.Fatal(err)
	}
	return id
}
