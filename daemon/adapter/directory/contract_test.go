package directory

import (
	"context"
	"crypto/rand"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/registry"
)

// El contrato con el registro, hablado de punta a punta EN PROCESO.
//
// # Por qué esto existe y qué congela
//
// El cliente y el servidor viven en este mismo repositorio y hablan un
// protocolo que nadie más va a implementar. Eso permite algo que casi nunca se
// puede: probar el contrato entero sin red y sin despliegue, con las DOS puntas
// de verdad. Un cambio en cualquiera de las dos que la otra no acompañe sale en
// CI, en Ubuntu, en el mismo empujón que lo introdujo.
//
// Lo que se ejercita es el camino de PRODUCCIÓN completo: se resuelve el
// nombre, cada dirección pasa por `domain.CheckSeedAddr`, y solo el dial crudo
// se desvía al servidor de prueba. La firma, el base64, el pin de la llave y
// los códigos de estado son los de verdad.
//
// Cada test levanta un servidor NUEVO: el límite de tasa del registro es de
// treinta peticiones por minuto y por servidor, así que compartirlo haría que
// unos tests envenenaran a otros con un 429 que no tiene nada que ver.
func servidorReal(t *testing.T) *httptest.Server {
	t.Helper()
	return servidorRealEspiado(t, nil)
}

// servidorRealEspiado es lo mismo, con un gancho para mirar QUÉ ruta llegó. El
// espía envuelve al handler de verdad: lo que contesta sigue siendo el registro.
func servidorRealEspiado(t *testing.T, ver func(ruta string)) *httptest.Server {
	t.Helper()
	store := registry.NewStore(time.Now, func() (domain.InviteID, error) {
		return domain.NewInviteID(rand.Reader)
	})
	// Un contador que jamás habló con el motor, que es el estado real al
	// arrancar y el que hace que `members` llegue ausente.
	contador := registry.NewCounter("no-existe", "127.0.0.1:1")
	pagina, err := registry.NewPage(filepath.Join("..", "..", "..", "invite", "index.html"))
	if err != nil {
		t.Fatalf("cargando la página real: %v", err)
	}

	real := registry.NewServer(store, contador, pagina).Handler()
	h := real
	if ver != nil {
		h = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ver(r.URL.Path)
			real.ServeHTTP(w, r)
		})
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

// clienteReal arma el adaptador contra ese servidor, por el camino entero.
func clienteReal(t *testing.T, srv *httptest.Server, datos string) *Directory {
	t.Helper()
	d, err := New(Deps{
		DataDir: datos,
		Seed:    "seed.ejemplo",
		Log:     logMudo{},
		Protect: protectorMudo,
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
	return d
}

func tarjetaDePrueba(t *testing.T, sala string) ([]byte, [domain.CardKeyLen]byte) {
	t.Helper()
	nick, err := domain.ParseNickname("Alvaro")
	if err != nil {
		t.Fatal(err)
	}
	sellada, clave, err := domain.SealRoomCard(domain.RoomCard{Host: nick, Room: sala}, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return sellada, clave
}

// C1. El círculo entero: abrir, buscar, descifrar, publicar, volver a buscar.
//
// Cierra el camino host → registro → página. El descifrado con la clave real es
// lo que prueba que el blob sobrevivió el viaje byte a byte: el registro guarda
// bytes que no puede leer, y si el base64 de una punta no fuera el de la otra,
// esto sería lo único que lo notaría.
func TestContratoAbrirBuscarYPublicar(t *testing.T) {
	srv := servidorReal(t)
	d := clienteReal(t, srv, t.TempDir())
	ctx := context.Background()

	sellada, clave := tarjetaDePrueba(t, "Los panas")
	room, err := d.Open(ctx, sellada)
	if err != nil {
		t.Fatal(err)
	}
	if room.Seed != "seed.ejemplo" {
		t.Errorf("el seed que volvió es %q, y lo pega el adaptador", room.Seed)
	}
	if room.InviteID.Raw() == "" {
		t.Fatal("el registro no emitió invite ID")
	}

	vuelta, miembros, err := d.Lookup(ctx, room.InviteID)
	if err != nil {
		t.Fatal(err)
	}
	if string(vuelta) != string(sellada) {
		t.Error("la tarjeta volvió distinta de como se subió")
	}
	// El contador nunca habló con el motor, así que ausente, y ausente es -1.
	// Un cero acá sería la afirmación "no hay nadie" y sería falsa.
	if miembros != -1 {
		t.Errorf("miembros = %d, y el contador no sabe nada, así que tiene que ser -1", miembros)
	}
	tarjeta, err := domain.OpenRoomCard(vuelta, clave)
	if err != nil {
		t.Fatal(err)
	}
	if tarjeta.Room != "Los panas" {
		t.Errorf("la tarjeta descifrada dice %q", tarjeta.Room)
	}

	// Y publicar encima la reemplaza.
	otra, otraClave := tarjetaDePrueba(t, "Los panas 2")
	if err := d.Publish(ctx, room.InviteID, otra); err != nil {
		t.Fatal(err)
	}
	vuelta2, _, err := d.Lookup(ctx, room.InviteID)
	if err != nil {
		t.Fatal(err)
	}
	tarjeta2, err := domain.OpenRoomCard(vuelta2, otraClave)
	if err != nil {
		t.Fatal(err)
	}
	if tarjeta2.Room != "Los panas 2" {
		t.Errorf("tras publicar, la tarjeta dice %q", tarjeta2.Room)
	}
}

// C2. Otra llave NO pisa un invite ID ajeno.
//
// Es lo que el cifrado no puede cerrar por sí solo: la clave de la tarjeta se
// deriva del enlace, así que TODO el que recibió el enlace puede fabricar una
// tarjeta que la página descifra, y el registro no tiene forma de distinguir a
// un miembro del host mirando la tarjeta. Lo que sí puede es exigir la firma de
// la llave que fijó ese invite ID la primera vez.
func TestContratoOtraLlaveNoPisaElID(t *testing.T) {
	srv := servidorReal(t)
	dueño := clienteReal(t, srv, t.TempDir())
	ctx := context.Background()

	sellada, _ := tarjetaDePrueba(t, "Los panas")
	room, err := dueño.Open(ctx, sellada)
	if err != nil {
		t.Fatal(err)
	}

	// Otro directorio de datos es otra llave: es exactamente un ex miembro que
	// se quedó con el código e intenta adelantarse.
	intruso := clienteReal(t, srv, t.TempDir())
	otra, _ := tarjetaDePrueba(t, "sala secuestrada")
	err = intruso.Publish(ctx, room.InviteID, otra)
	if err == nil {
		t.Fatal("una llave ajena pisó la tarjeta del host")
	}
	if !strings.Contains(err.Error(), "otro equipo") {
		t.Errorf("el error no explica que la sala es de otro: %v", err)
	}

	// Y la del dueño sigue siendo la que está.
	vuelta, _, err := dueño.Lookup(ctx, room.InviteID)
	if err != nil {
		t.Fatal(err)
	}
	if string(vuelta) != string(sellada) {
		t.Error("la tarjeta cambió pese al rechazo")
	}
}

// C3. Reabrir con la MISMA llave funciona.
//
// Es el camino de `ResumeRoom` congelado: el daemon se murió, arranca otro, y
// la llave que carga del disco es la que el registro tiene fijada.
func TestContratoReabrirConLaMismaLlave(t *testing.T) {
	srv := servidorReal(t)
	datos := t.TempDir()
	ctx := context.Background()

	primero := clienteReal(t, srv, datos)
	sellada, _ := tarjetaDePrueba(t, "Los panas")
	room, err := primero.Open(ctx, sellada)
	if err != nil {
		t.Fatal(err)
	}

	// Otro adaptador, mismo directorio de datos: es el daemon reiniciado.
	segundo := clienteReal(t, srv, datos)
	if err := segundo.Publish(ctx, room.InviteID, sellada); err != nil {
		t.Fatalf("el daemon reiniciado no pudo republicar su propia tarjeta: %v", err)
	}
}

// C4. Publicar sobre un invite ID que el registro jamás emitió NO existe.
//
// Marca el límite exacto de la promesa "o reabre la sala con el mismo invite
// ID": vale mientras el fijado siga vivo, y no inventa salas. Importa para el
// camino de respaldo de crear, donde el ID se generó acá porque el registro no
// contestó: esa tarjeta no tiene dónde volver, y por eso no se persiste.
func TestContratoPublicarUnIDJamásEmitidoNoExiste(t *testing.T) {
	srv := servidorReal(t)
	d := clienteReal(t, srv, t.TempDir())

	id, err := domain.NewInviteID(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sellada, _ := tarjetaDePrueba(t, "sala fantasma")
	err = d.Publish(context.Background(), id, sellada)
	if err == nil {
		t.Fatal("se publicó sobre un invite ID que nadie emitió")
	}
	if !strings.Contains(err.Error(), "no conoce esa sala") {
		t.Errorf("el error no dice que la sala no existe: %v", err)
	}
}

// C5. Una tarjeta pasada de tamaño la rechaza el registro.
//
// El dominio ya la corta antes de sellarla, así que este test mide la otra
// punta: que el tope de las dos coincida. Si alguien sube uno de los dos, esto
// lo dice.
func TestContratoUnaTarjetaQuePasaDelTopeSeRechaza(t *testing.T) {
	srv := servidorReal(t)
	d := clienteReal(t, srv, t.TempDir())

	gorda := make([]byte, domain.MaxCardBytes+1)
	_, err := d.Open(context.Background(), gorda)
	if err == nil {
		t.Fatal("el registro aceptó una tarjeta pasada del tope")
	}
	if !strings.Contains(err.Error(), "no le entra") {
		t.Errorf("el error no habla del tamaño: %v", err)
	}
}

// C6. El invite ID viaja SIN guiones en la ruta y vuelve canónico.
//
// El registro normaliza las dos formas, así que esto no puede fallar por
// casualidad: lo que congela es que el adaptador manda `Raw()` y parsea la
// forma con guion que devuelve el POST, que son dos representaciones del mismo
// valor y confundirlas daría un 404 intermitente.
func TestContratoElIDViajaCrudoYVuelveCanónico(t *testing.T) {
	var rutas []string
	srv := servidorRealEspiado(t, func(ruta string) { rutas = append(rutas, ruta) })
	d := clienteReal(t, srv, t.TempDir())
	ctx := context.Background()

	sellada, _ := tarjetaDePrueba(t, "Los panas")
	room, err := d.Open(ctx, sellada)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := d.Lookup(ctx, room.InviteID); err != nil {
		t.Fatal(err)
	}

	// Lo que la gente ve lleva guion; lo que viaja en la ruta, no.
	if !strings.Contains(room.InviteID.String(), "-") {
		t.Error("la forma canónica no lleva guion, y es la que se le enseña a la gente")
	}
	var consulta string
	for _, r := range rutas {
		if strings.HasPrefix(r, "/api/i/") {
			consulta = r
		}
	}
	if consulta == "" {
		t.Fatal("no se vio ninguna consulta por invite ID")
	}
	if strings.Contains(consulta, "-") {
		t.Errorf("la ruta fue %q y lleva guion; lo que viaja es Raw()", consulta)
	}
	if !strings.HasSuffix(consulta, room.InviteID.Raw()) {
		t.Errorf("la ruta fue %q y el ID crudo es %q", consulta, room.InviteID.Raw())
	}
}
