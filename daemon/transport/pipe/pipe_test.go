package pipe

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/daemon/transport/protocol"
)

// Los tests del pipe corren en LINUX, y eso es a propósito.
//
// Lo único que es de Windows acá es crear el named pipe, y vive solo en
// `pipe_windows.go`. Todo lo demás son plazas, plazos y cierre sobre un
// net.Listener, o sea lógica, y la lógica que solo corre en la máquina donde se
// programa es lógica sin CI.
//
// Lo que estos tests NO pueden afirmar, y hay que decirlo: que el descriptor de
// seguridad haga lo que dice, y que el prefijo protegido impida el squatting.
// Eso se mide en Windows y está medido a mano, ver el doc del paquete.

func TestSinTokenNoSeEscucha(t *testing.T) {
	// Un token vacío haría que el saludo lo pase cualquiera: la puerta abierta
	// con la cerradura puesta.
	_, err := Listen(Deps{API: &apiFalsa{}, Clock: relojReal{}, Log: logMudo{}, listen: oyenteDePrueba(t)})
	if err == nil {
		t.Fatal("se pudo escuchar sin token")
	}
	if !strings.Contains(err.Error(), "token") {
		t.Errorf("el error no explica que falta el token: %v", err)
	}
}

func TestSinDescriptorNoSeEscucha(t *testing.T) {
	// Pasar la cadena vacía no es "los permisos por defecto": el descriptor por
	// defecto de un named pipe da lectura a Everyone y a la cuenta anónima. Por
	// eso es un error y no una opción.
	if _, err := abrirPipe(`\\.\pipe\loquesea`, ""); !errors.Is(err, ErrSinDescriptor) {
		t.Errorf("abrirPipe sin descriptor devolvió %v, se esperaba ErrSinDescriptor", err)
	}
}

func TestCadaProductoTieneSuNombreBajoElPrefijoProtegido(t *testing.T) {
	// Es lo que hace imposible el squatting en vez de defenderlo con una
	// carrera: bajo este prefijo, un proceso sin elevar recibe ERROR_ACCESS_DENIED
	// al intentar crear el nombre. Medido a mano en Windows.
	const prefijo = `\\.\pipe\ProtectedPrefix\Administrators\`

	nombres := []string{Name, PortableName, ConsoleName}
	for _, n := range nombres {
		if !strings.HasPrefix(n, prefijo) {
			t.Errorf("el nombre %q no va bajo %q", n, prefijo)
		}
	}
	for i, a := range nombres {
		for _, b := range nombres[i+1:] {
			if a == b {
				t.Errorf("dos productos comparten el pipe %q: instalado, portable y consola tienen que coexistir", a)
			}
		}
	}
}

func TestElDescriptorNoLeDaTodoAlUsuarioInteractivo(t *testing.T) {
	// Con GENERIC_ALL, el usuario interactivo podría crear INSTANCIAS NUEVAS del
	// pipe, y una instancia nueva atiende conexiones como si fuera el daemon:
	// secuestrar a la UI desde una cuenta sin privilegios.
	if strings.Contains(SecurityDescriptor, "(A;;GA;;;IU)") {
		t.Error("el descriptor le da GENERIC_ALL al usuario interactivo")
	}
	if !strings.Contains(SecurityDescriptor, ";IU)") {
		t.Error("el descriptor no menciona al usuario interactivo, así que la UI no podría abrir el pipe")
	}
	// D:P es la DACL protegida: sin eso hereda permisos del objeto padre y deja
	// de ser esta lista.
	if !strings.HasPrefix(SecurityDescriptor, "D:P") {
		t.Errorf("el descriptor %q no es una DACL protegida", SecurityDescriptor)
	}
}

func TestSeAtiendeYSeContesta(t *testing.T) {
	b := nuevoBanco(t)

	c := b.marca(t)
	b.saluda(t, c)

	resp := b.pide(t, c, protocol.Request{ID: 2, Method: protocol.MethodStatus})
	if resp.Error != nil {
		t.Fatalf("status devolvió error: %v", resp.Error)
	}
}

func TestSinSaludarNoSeContestaNada(t *testing.T) {
	// El estado dice en qué sala estás y con quién, así que la puerta del saludo
	// tiene que estar antes de él.
	b := nuevoBanco(t)
	c := b.marca(t)

	resp := b.pide(t, c, protocol.Request{ID: 1, Method: protocol.MethodStatus})
	if resp.Error == nil || resp.Error.Code != protocol.CodeUnauthorized {
		t.Fatalf("se contestó sin saludar: %+v", resp)
	}
}

func TestElQueNoSaludaAtiempoSeQuedaFuera(t *testing.T) {
	// Conectarse y callarse ocupa una plaza de las ocho sin decir quién es.
	b := nuevoBancoCon(t, func(d *Deps) {})
	c := b.marca(t)

	// No se puede esperar HelloWait entero en un test, así que se comprueba lo
	// que el vigilante consulta: hasta que no hay saludo, Greeted es falso, y es
	// lo único que decide si esa conexión se corta.
	srv := protocol.NewServer(&apiFalsa{}, "token-de-prueba", relojReal{}, logMudo{})
	if srv.Greeted() {
		t.Fatal("un servidor recién creado dice que ya saludó")
	}
	b.saluda(t, c)
}

func TestElTopeDeConexionesCortaEnvezDeEncolar(t *testing.T) {
	// Encolarlas dejaría que un proceso en bucle llene la memoria de conexiones
	// que nadie va a atender.
	b := nuevoBanco(t)

	vivas := make([]net.Conn, 0, MaxConns)
	for i := 0; i < MaxConns; i++ {
		c := b.marca(t)
		b.saluda(t, c)
		vivas = append(vivas, c)
	}
	t.Cleanup(func() {
		for _, c := range vivas {
			_ = c.Close()
		}
	})

	// La de más se acepta y se corta enseguida, sin leerle nada.
	sobra, err := b.dial()
	if err != nil {
		return // el oyente ya la rechazó al marcar, que también vale
	}
	defer func() { _ = sobra.Close() }()

	_ = sobra.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 1)
	if _, err := sobra.Read(buf); err == nil {
		t.Error("la conexión que pasa del tope siguió viva")
	}
}

func TestUnPánicoNoSeLlevaElDaemon(t *testing.T) {
	// Sin el recover, cualquier ruta de la API que reviente mata el proceso, y
	// con él la sala: se cierran los puertos, se va el motor y a todos los que
	// estaban jugando se les cae la partida. Todo porque una pantalla pidió algo
	// raro.
	//
	// No es hipotético: lo encontró este mismo banco de pruebas con una API a
	// medio implementar, que es la forma exacta que tiene el daemon en esta
	// rebanada, con nueve adaptadores todavía provisionales.
	b := nuevoBancoCon(t, func(d *Deps) { d.API = apiQueRevienta{} })

	c := b.marca(t)
	b.saluda(t, c)

	w := protocol.NewWriter(c)
	if err := w.Write(protocol.Request{ID: 2, Method: protocol.MethodStatus}); err != nil {
		t.Fatal(err)
	}
	// Esa conexión se pierde, y es lo correcto. Lo que importa es lo de abajo.
	_ = c.Close()

	// El oyente sigue vivo: otra conexión entra y funciona.
	otra := b.marca(t)
	b.saluda(t, otra)
}

func TestCerrarNoSeCuelgaConConexionesAbiertas(t *testing.T) {
	// Close espera a las conversaciones en curso, así que una conexión viva que
	// no se entera del cierre colgaría el apagado del servicio entero.
	b := nuevoBanco(t)
	c := b.marca(t)
	b.saluda(t, c)

	hecho := make(chan struct{})
	go func() {
		_ = b.ln.Close()
		close(hecho)
	}()

	select {
	case <-hecho:
	case <-time.After(3 * time.Second):
		t.Fatal("Close se colgó con una conexión abierta")
	}
}

func TestCerrarDosVecesNoRompe(t *testing.T) {
	// Lo llama el camino de error del arranque, que puede correr antes de que se
	// haya abierto nada.
	b := nuevoBanco(t)
	if err := b.ln.Close(); err != nil {
		t.Fatal(err)
	}
	if err := b.ln.Close(); err != nil {
		t.Errorf("el segundo Close devolvió error: %v", err)
	}
}

// Los tests del token.

func TestElTokenEsLargoYDistintoCadaVez(t *testing.T) {
	visto := map[string]bool{}
	for i := 0; i < 50; i++ {
		tok, err := NewToken()
		if err != nil {
			t.Fatal(err)
		}
		// 32 bytes en base64url sin relleno son 43 caracteres.
		if len(tok) != 43 {
			t.Fatalf("el token mide %d caracteres, se esperaban 43: %q", len(tok), tok)
		}
		if visto[tok] {
			t.Fatalf("se repitió un token: %q", tok)
		}
		visto[tok] = true

		// Sin caracteres que haya que escapar en JSON, en un archivo o en una
		// línea de comandos.
		if strings.ContainsAny(tok, `+/= "'\`) {
			t.Errorf("el token lleva caracteres que hay que escapar: %q", tok)
		}
	}
}

func TestElTokenNoSeEscribeSiFaltaElDirectorio(t *testing.T) {
	// El directorio lo crea el instalador con una ACL propia, y esa ACL es la
	// mitad de la protección de lo que hay dentro. Crearlo por accidente acá la
	// perdería en silencio.
	err := WriteToken(filepath.Join(t.TempDir(), "que-no-existe"), "loquesea")
	if err == nil {
		t.Fatal("se escribió el token en un directorio que no existe")
	}
	if !strings.Contains(err.Error(), "directorio de datos") {
		t.Errorf("el error no explica el problema: %v", err)
	}
}

func TestElTokenSeEscribeSeLeeYSeBorra(t *testing.T) {
	dir := t.TempDir()
	tok, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteToken(dir, tok); err != nil {
		t.Fatal(err)
	}

	leido, err := ReadToken(dir)
	if err != nil {
		t.Fatal(err)
	}
	if leido != tok {
		t.Fatalf("se leyó %q y se escribió %q", leido, tok)
	}

	if err := RemoveToken(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, TokenFile)); !os.IsNotExist(err) {
		t.Error("el token sobrevivió al borrado, y un token que sobrevive al proceso " +
			"no abre nada: es un secreto muerto esperando a que alguien lo lea")
	}
	// Borrar dos veces no es un error: es lo normal en toda salida limpia.
	if err := RemoveToken(dir); err != nil {
		t.Errorf("el segundo borrado devolvió error: %v", err)
	}
}

func TestElTokenViejoNoSobreviveAUnoNuevo(t *testing.T) {
	// El daemon lo rota una vez por vida del proceso. Si el archivo conservara
	// restos del anterior, un cliente podría saludar con el viejo.
	dir := t.TempDir()
	if err := WriteToken(dir, strings.Repeat("A", 43)); err != nil {
		t.Fatal(err)
	}
	if err := WriteToken(dir, "corto"); err != nil {
		t.Fatal(err)
	}

	leido, err := ReadToken(dir)
	if err != nil {
		t.Fatal(err)
	}
	if leido != "corto" {
		t.Fatalf("quedó %q: el archivo no se truncó", leido)
	}
}

func TestUnTokenVacioNoSeEscribe(t *testing.T) {
	if err := WriteToken(t.TempDir(), ""); err == nil {
		t.Fatal("se escribió un token vacío")
	}
}

// El banco de pruebas.

type banco struct {
	ln     *Listener
	dial   func() (net.Conn, error)
	cancel context.CancelFunc
}

func nuevoBanco(t *testing.T) *banco { return nuevoBancoCon(t, nil) }

func nuevoBancoCon(t *testing.T, tocar func(*Deps)) *banco {
	t.Helper()

	oyente, dial := parDePrueba(t)
	deps := Deps{
		API:    &apiFalsa{},
		Token:  "token-de-prueba",
		Clock:  relojReal{},
		Log:    logMudo{},
		listen: func(string, string) (net.Listener, error) { return oyente, nil },
	}
	if tocar != nil {
		tocar(&deps)
	}

	ln, err := Listen(deps)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = ln.Serve(ctx) }()

	b := &banco{ln: ln, dial: dial, cancel: cancel}
	t.Cleanup(func() {
		cancel()
		_ = ln.Close()
	})
	return b
}

func (b *banco) marca(t *testing.T) net.Conn {
	t.Helper()
	c, err := b.dial()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func (b *banco) saluda(t *testing.T, c net.Conn) {
	t.Helper()
	resp := b.pide(t, c, protocol.Request{
		ID:     1,
		Method: protocol.MethodHello,
		Params: json.RawMessage(`{"token":"token-de-prueba"}`),
	})
	if resp.Error != nil {
		t.Fatalf("el saludo falló: %v", resp.Error)
	}
}

func (b *banco) pide(t *testing.T, c net.Conn, req protocol.Request) protocol.Response {
	t.Helper()

	w := protocol.NewWriter(c)
	if err := w.Write(req); err != nil {
		t.Fatalf("escribiendo %s: %v", req.Method, err)
	}

	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	linea, err := protocol.NewReader(c).ReadLine()
	if err != nil {
		t.Fatalf("leyendo la respuesta de %s: %v", req.Method, err)
	}

	var resp protocol.Response
	if err := json.Unmarshal(linea, &resp); err != nil {
		t.Fatalf("la respuesta no es JSON: %v", err)
	}
	return resp
}

// parDePrueba arma un oyente de verdad sobre loopback y su marcador.
//
// TCP en loopback y no net.Pipe: hace falta un net.Listener con Accept de
// verdad, y sobre todo hacen falta los plazos, que net.Pipe no implementa de la
// misma forma. Es el transporte más parecido al pipe que existe en Linux.
func parDePrueba(t *testing.T) (net.Listener, func() (net.Conn, error)) {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	dir := ln.Addr().String()
	return ln, func() (net.Conn, error) { return net.DialTimeout("tcp", dir, 2*time.Second) }
}

func oyenteDePrueba(t *testing.T) func(string, string) (net.Listener, error) {
	t.Helper()
	ln, _ := parDePrueba(t)
	return func(string, string) (net.Listener, error) { return ln, nil }
}

type relojReal struct{}

func (relojReal) Now() time.Time { return time.Now() }

type logMudo struct{}

func (logMudo) Info(string, ...any)  {}
func (logMudo) Warn(string, ...any)  {}
func (logMudo) Error(string, ...any) {}

// apiFalsa contesta lo mínimo para que el transporte se pueda probar.
//
// Embebe la interfaz en vez de implementarla entera: los métodos que estos
// tests no tocan entran en pánico si alguien los llama, que es justo lo que se
// quiere. Rellenar los veintitantos con ceros haría que uno nuevo pasara
// desapercibido, y este paquete no prueba la API, prueba el transporte.
type apiFalsa struct {
	protocol.API
}

func (a *apiFalsa) Status() domain.RoomState {
	return domain.RoomState{Conn: domain.StateIdle}
}

func (a *apiFalsa) MissingGame() string { return "" }
func (a *apiFalsa) InviteLink() string  { return "" }

// apiQueRevienta es la API rota, para el test del recover.
type apiQueRevienta struct{ protocol.API }

func (apiQueRevienta) Status() domain.RoomState {
	panic("el adaptador de abajo explotó")
}
