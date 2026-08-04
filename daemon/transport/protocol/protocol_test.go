package protocol

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/core/usecase"
)

const tokenDePrueba = "un-token-de-prueba-largo"

func ctx() context.Context { return context.Background() }

// TestHayQueSaludarAntesDePedirNada.
//
// Sin esta puerta, un proceso sin el token igual podría pedir el estado, y el
// estado dice en qué sala estás y con quién.
func TestHayQueSaludarAntesDePedirNada(t *testing.T) {
	s, _ := servidor(t)
	resp := pide(t, s, `{"id":1,"method":"status"}`)

	if resp.Error == nil || resp.Error.Code != CodeUnauthorized {
		t.Fatalf("se contestó el estado sin saludar: %+v", resp)
	}
}

func TestSaludarConElTokenAbreLaSesión(t *testing.T) {
	s, _ := servidor(t)

	if resp := saluda(t, s, tokenDePrueba); resp.Error != nil {
		t.Fatalf("el saludo bueno falló: %v", resp.Error)
	}
	if resp := pide(t, s, `{"id":2,"method":"status"}`); resp.Error != nil {
		t.Fatalf("el estado falló tras saludar: %v", resp.Error)
	}
}

func TestUnTokenQueNoEsNoAbreNada(t *testing.T) {
	s, _ := servidor(t)

	if resp := saluda(t, s, "otra-cosa"); resp.Error == nil || resp.Error.Code != CodeUnauthorized {
		t.Fatalf("entró con un token que no es: %+v", resp)
	}
	if resp := pide(t, s, `{"id":2,"method":"status"}`); resp.Error == nil {
		t.Fatal("un saludo fallido dejó la sesión abierta")
	}
}

// TestUnMétodoDesconocidoNoSeInterpreta.
//
// La lista es cerrada, y esa es la mitigación principal de esta superficie.
func TestUnMétodoDesconocidoNoSeInterpreta(t *testing.T) {
	s, _ := servidor(t)
	saluda(t, s, tokenDePrueba)

	for _, m := range []string{"open_port", "exec", "read_file", "Status", ""} {
		t.Run(m, func(t *testing.T) {
			body, _ := json.Marshal(Request{ID: 3, Method: Method(m)})
			resp := pide(t, s, string(body))
			if resp.Error == nil || resp.Error.Code != CodeBadRequest {
				t.Fatalf("se aceptó el método %q: %+v", m, resp)
			}
		})
	}
}

// TestNoExisteLaOperaciónDeAbrirUnPuerto.
//
// Es la invariante que sostiene la superficie entera, y merece un test que la
// afirme en vez de un comentario. Un puerto solo se abre eligiendo un juego del
// catálogo.
func TestNoExisteLaOperaciónDeAbrirUnPuerto(t *testing.T) {
	// Ninguna de estas puede existir jamás. La lista está escrita a mano y no
	// por subcadenas: `suspend_foreign_rules` lleva reglas legítimamente, y solo
	// puede DESACTIVAR reglas que ya existen, nunca crear una.
	jamás := []Method{
		"open_port", "close_port", "add_rule", "apply_rules", "set_firewall",
		"exec", "run_command", "read_file", "write_file", "set_registry",
		"set_config", "set_timeout", "join_network", "set_network_secret",
	}
	for _, m := range jamás {
		if m.Known() {
			t.Errorf("la API expone %q, que es exactamente lo que no puede exponer", m)
		}
	}

	// Y el único método que abre puertos solo acepta un id de juego. Un intento
	// de colar un puerto por sus parámetros se rechaza entero.
	s, api := servidor(t)
	saluda(t, s, tokenDePrueba)

	resp := pide(t, s, `{"id":5,"method":"activate_profile","params":{"game":"x","ports":["445"]}}`)
	if resp.Error == nil || resp.Error.Code != CodeBadRequest {
		t.Fatalf("activate_profile aceptó un puerto en sus parámetros: %+v", resp)
	}
	if api.activados != 0 {
		t.Fatal("el intento llegó al núcleo")
	}
}

// TestLosParámetrosSeInterpretanEstricto: un campo de más no es un cliente
// amable con extensiones, es un cliente que cree estar pidiendo algo que este
// daemon no hace.
func TestLosParámetrosSeInterpretanEstricto(t *testing.T) {
	s, _ := servidor(t)
	saluda(t, s, tokenDePrueba)

	casos := map[string]string{
		"campo de más":    `{"id":4,"method":"create_room","params":{"nickname":"alvaro","name":"x","game":"doom"}}`,
		"json roto":       `{"id":4,"method":"create_room","params":{"nickname":}}`,
		"sin parámetros":  `{"id":4,"method":"create_room"}`,
		"sobre con extra": `{"id":4,"method":"status","extra":1}`,
		"basura detrás":   `{"id":4,"method":"status"}{"id":5,"method":"status"}`,
	}
	for nombre, linea := range casos {
		t.Run(nombre, func(t *testing.T) {
			resp := pide(t, s, linea)
			if resp.Error == nil || resp.Error.Code != CodeBadRequest {
				t.Fatalf("se aceptó %s: %+v", nombre, resp)
			}
		})
	}
}

// TestUnMensajeGigantesCortaLaConexión.
//
// El tope va ANTES de deserializar, que es la mitad del punto: uno aplicado
// después ya pagó el coste de parsear. Y cortar no es negociable: con líneas,
// un mensaje que no cupo deja el flujo desincronizado.
func TestUnMensajeGiganteCortaLaConexión(t *testing.T) {
	s, _ := servidor(t)

	entrada := `{"id":1,"method":"status","params":{"x":"` + strings.Repeat("a", MaxMessage) + `"}}` + "\n"
	var salida bytes.Buffer
	err := s.Serve(ctx(), &conexión{r: strings.NewReader(entrada), w: &salida})

	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("no se cortó por tamaño: %v", err)
	}
	if !strings.Contains(salida.String(), string(CodeTooLarge)) {
		t.Fatalf("no se avisó del corte: %q", salida.String())
	}
}

// TestElEstadoViajaConLosEnumsComoCadenas.
//
// Con el número del iota, agregar un estado en medio del bloque le cambiaría el
// significado a todos los de abajo en una UI ya instalada.
func TestElEstadoViajaConLosEnumsComoCadenas(t *testing.T) {
	s, api := servidor(t)
	saluda(t, s, tokenDePrueba)

	api.estado = domain.RoomState{
		Conn: domain.StateDegraded,
		Role: domain.RoleGuest,
		Peers: []domain.Peer{
			{VirtualIP: netip.MustParseAddr("100.87.3.1"), Name: nick(t, "alvaro"), Path: domain.PathRelay, Host: true},
		},
		Alerts:   []domain.Alert{{Kind: domain.AlertKickIncomplete, Detail: "a medias"}},
		LastExit: domain.ExitTunnelLost,
	}
	var v RoomView
	lee(t, pide(t, s, `{"id":9,"method":"status"}`), &v)

	switch {
	case v.Conn != "degraded":
		t.Errorf("conn = %q", v.Conn)
	case v.Role != "guest":
		t.Errorf("role = %q", v.Role)
	case len(v.Peers) != 1 || v.Peers[0].Path != "relay":
		t.Errorf("peers = %+v", v.Peers)
	case len(v.Alerts) != 1 || v.Alerts[0].Kind != "kick_incomplete":
		t.Errorf("alertas = %+v", v.Alerts)
	case v.LastExit != "tunnel_lost":
		t.Errorf("last_exit = %q", v.LastExit)
	}
}

// TestLosPlazosViajanComoDuraciónYaCalculada: la UI no tiene que restar contra
// un reloj que puede no ser el mismo.
func TestLosPlazosViajanComoDuraciónYaCalculada(t *testing.T) {
	s, api := servidor(t)
	saluda(t, s, tokenDePrueba)

	api.estado = domain.RoomState{
		Conn:              domain.StateReconnecting,
		HostGoneSince:     api.ahora.Add(-3 * time.Minute),
		ReconnectingSince: api.ahora.Add(-90 * time.Second),
	}
	var v RoomView
	lee(t, pide(t, s, `{"id":9,"method":"status"}`), &v)

	if v.HostGoneForMS != (3 * time.Minute).Milliseconds() {
		t.Errorf("host_gone_for_ms = %d", v.HostGoneForMS)
	}
	if v.ReconnectingForMS != (90 * time.Second).Milliseconds() {
		t.Errorf("reconnecting_for_ms = %d", v.ReconnectingForMS)
	}
}

// TestCadaErrorDelNúcleoTieneSuCódigo.
func TestCadaErrorDelNúcleoTieneSuCódigo(t *testing.T) {
	casos := []struct {
		err    error
		quiero Code
	}{
		{usecase.ErrBusy, CodeBusy},
		{usecase.ErrNoRoom, CodeNoRoom},
		{usecase.ErrNotHost, CodeNotHost},
		{usecase.ErrUnknownGame, CodeUnknownGame},
		{usecase.ErrNotAMember, CodeNotAMember},
		{usecase.ErrSelfKick, CodeSelfKick},
		{usecase.ErrShadowsBuiltin, CodeShadows},
		{usecase.ErrNotPlayed, CodeNotPlayed},
		{usecase.ErrNoPendingRoom, CodeNoPending},
		{domain.ErrNicknameEmpty, CodeBadNickname},
		{domain.ErrInputShape, CodeBadCode},
		{errors.New("algo que nadie previó"), CodeInternal},
	}
	for _, c := range casos {
		if got := errorFor(c.err).Code; got != c.quiero {
			t.Errorf("errorFor(%v) = %q, se esperaba %q", c.err, got, c.quiero)
		}
	}
}

// TestUnaExpulsiónAMediasDevuelveEstadoYError.
//
// Es la única operación cuyo fallo deja un estado que la UI necesita: el
// expulsado ya no está en las reglas, así que la lista tiene que redibujarse
// sin él aunque la operación haya devuelto error.
func TestUnaExpulsiónAMediasDevuelveEstadoYError(t *testing.T) {
	s, api := servidor(t)
	saluda(t, s, tokenDePrueba)
	api.errKick = usecase.ErrKickPartial
	api.estado = domain.RoomState{Conn: domain.StateConnected, Role: domain.RoleHost}

	resp := pide(t, s, `{"id":7,"method":"kick_member","params":{"ip":"100.87.3.5"}}`)
	if resp.Error == nil || resp.Error.Code != CodeKickPartial {
		t.Fatalf("código = %+v", resp.Error)
	}
	if len(resp.Result) == 0 {
		t.Fatal("una expulsión a medias no devolvió el estado")
	}
}

// TestUnaDirecciónQueNoEsSeRechazaAntesDeLlegarAlNúcleo.
func TestUnaDirecciónQueNoEsSeRechazaAntesDeLlegarAlNúcleo(t *testing.T) {
	s, api := servidor(t)
	saluda(t, s, tokenDePrueba)

	resp := pide(t, s, `{"id":7,"method":"kick_member","params":{"ip":"no-es-una-ip"}}`)
	if resp.Error == nil || resp.Error.Code != CodeBadRequest {
		t.Fatalf("resp = %+v", resp)
	}
	if api.kicks != 0 {
		t.Fatal("una dirección inválida llegó al núcleo")
	}
}

// TestLosPerfilesDeFirewallSeReconstruyenDeLaTablaCerrada.
//
// Lo que entra por el pipe es entrada de fuera, y el peor resultado posible acá
// sería desactivarle al usuario una regla que él no eligió.
func TestLosPerfilesDeFirewallSeReconstruyenDeLaTablaCerrada(t *testing.T) {
	reglas := foreignRules([]ForeignView{
		{Name: "a", Executable: "C:\\j.exe", Profiles: []string{"privado", "inventado"}},
		{Name: "b", Executable: "C:\\k.exe", Profiles: []string{"nada válido"}},
		{Name: "c", Executable: "", Profiles: []string{"privado"}},
	})
	if len(reglas) != 1 {
		t.Fatalf("reglas aceptadas = %d, se esperaba solo la primera: %+v", len(reglas), reglas)
	}
	if len(reglas[0].Profiles) != 1 || reglas[0].Profiles[0] != domain.ProfilePrivate {
		t.Fatalf("perfiles = %+v", reglas[0].Profiles)
	}
}

// TestUnPerfilQueEntraPorElPipePasaPorLasMismasInvariantes.
//
// No se decodifica a mano: se pasa por el único decodificador estricto de
// perfiles del programa, así que un perfil que pide 445 se rechaza igual venga
// de un archivo o de la UI.
func TestUnPerfilQueEntraPorElPipePasaPorLasMismasInvariantes(t *testing.T) {
	s, api := servidor(t)
	saluda(t, s, tokenDePrueba)

	perfil := `{"id":"malicioso","schema":2,"name":"Malicioso",` +
		`"host_ports":[{"proto":"tcp","range":"440-450"}],` +
		`"connect_hint":{"kind":"direct_ip","text_es":"x"}}`
	body, _ := json.Marshal(Request{
		ID: 8, Method: MethodSaveProfile,
		Params: json.RawMessage(`{"profile":` + perfil + `,"replace":false}`),
	})

	resp := pide(t, s, string(body))
	if resp.Error == nil || resp.Error.Code != CodeBadProfile {
		t.Fatalf("se aceptó un perfil que abarca el 445: %+v", resp)
	}
	if api.guardados != 0 {
		t.Fatal("el perfil llegó al catálogo")
	}
}

// TestLasLíneasVacíasSeIgnoran: un cliente que mande un salto de más no merece
// un error.
func TestLasLíneasVacíasSeIgnoran(t *testing.T) {
	entrada := "\n\n" + `{"id":1,"method":"hello","params":{"token":"` + tokenDePrueba + `"}}` + "\n"
	s, _ := servidor(t)

	var salida bytes.Buffer
	if err := s.Serve(ctx(), &conexión{r: strings.NewReader(entrada), w: &salida}); err != nil {
		t.Fatalf("Serve devolvió %v", err)
	}
	if !strings.Contains(salida.String(), `"ok":true`) {
		t.Fatalf("el saludo no se atendió: %q", salida.String())
	}
}

// TestUnaRespuestaPorPedido: sin esto, una UI se cuelga esperando.
func TestUnaRespuestaPorPedido(t *testing.T) {
	entrada := strings.Join([]string{
		`{"id":1,"method":"hello","params":{"token":"` + tokenDePrueba + `"}}`,
		`{"id":2,"method":"status"}`,
		`{"id":3,"method":"no_existe"}`,
		`{"id":4,"method":"invite_link"}`,
	}, "\n") + "\n"
	s, _ := servidor(t)

	var salida bytes.Buffer
	if err := s.Serve(ctx(), &conexión{r: strings.NewReader(entrada), w: &salida}); err != nil {
		t.Fatalf("Serve devolvió %v", err)
	}
	líneas := strings.Split(strings.TrimSpace(salida.String()), "\n")
	if len(líneas) != 4 {
		t.Fatalf("respuestas = %d, pedidos = 4:\n%s", len(líneas), salida.String())
	}
	for i, l := range líneas {
		var resp Response
		if err := json.Unmarshal([]byte(l), &resp); err != nil {
			t.Fatalf("respuesta %d no es JSON: %v", i, err)
		}
		if resp.ID != uint64(i+1) {
			t.Errorf("respuesta %d trae id %d", i, resp.ID)
		}
	}
}

// TestTodoMétodoDeLaTablaTieneManejador.
//
// El día que alguien agregue uno a la tabla y olvide el caso, el síntoma tiene
// que ser este test y no una UI esperando para siempre.
func TestTodoMétodoDeLaTablaTieneManejador(t *testing.T) {
	for m := range métodos {
		if m == MethodHello {
			continue
		}
		t.Run(string(m), func(t *testing.T) {
			s, _ := servidor(t)
			saluda(t, s, tokenDePrueba)

			body, _ := json.Marshal(Request{ID: 1, Method: m, Params: json.RawMessage(`{}`)})
			resp := pide(t, s, string(body))
			if resp.Error != nil && strings.Contains(resp.Error.Message, "no tiene manejador") {
				t.Fatalf("el método %q está en la tabla y nadie lo atiende", m)
			}
		})
	}
}

// Los dobles.

type conexión struct {
	r io.Reader
	w io.Writer
}

func (c *conexión) Read(p []byte) (int, error)  { return c.r.Read(p) }
func (c *conexión) Write(p []byte) (int, error) { return c.w.Write(p) }

func servidor(t *testing.T) (*Server, *apiFalsa) {
	t.Helper()
	api := &apiFalsa{ahora: time.Date(2026, 8, 2, 20, 0, 0, 0, time.UTC)}
	return NewServer(api, tokenDePrueba, api, logMudo{}), api
}

// pide manda una línea y devuelve la respuesta. Cada llamada reusa el mismo
// servidor, así el estado de autenticación sobrevive entre pedidos.
func pide(t *testing.T, s *Server, linea string) Response {
	t.Helper()
	var salida bytes.Buffer
	if err := s.Serve(ctx(), &conexión{r: strings.NewReader(linea + "\n"), w: &salida}); err != nil {
		if !errors.Is(err, ErrTooLarge) {
			t.Fatalf("Serve devolvió %v", err)
		}
	}
	var resp Response
	if err := json.Unmarshal(bytes.TrimSpace(salida.Bytes()), &resp); err != nil {
		t.Fatalf("respuesta ilegible %q: %v", salida.String(), err)
	}
	return resp
}

func saluda(t *testing.T, s *Server, token string) Response {
	t.Helper()
	body, _ := json.Marshal(Request{
		ID: 1, Method: MethodHello,
		Params: json.RawMessage(`{"token":` + strconvQuote(token) + `}`),
	})
	return pide(t, s, string(body))
}

func strconvQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func lee(t *testing.T, resp Response, v any) {
	t.Helper()
	if resp.Error != nil {
		t.Fatalf("la respuesta trae error: %v", resp.Error)
	}
	if err := json.Unmarshal(resp.Result, v); err != nil {
		t.Fatalf("resultado ilegible: %v", err)
	}
}

func nick(t *testing.T, s string) domain.Nickname {
	t.Helper()
	n, err := domain.ParseNickname(s)
	if err != nil {
		t.Fatalf("nick %q: %v", s, err)
	}
	return n
}

type logMudo struct{}

func (logMudo) Info(string, ...any)  {}
func (logMudo) Warn(string, ...any)  {}
func (logMudo) Error(string, ...any) {}

// apiFalsa hace de daemon. También hace de reloj, para que el test fije el
// ahora contra el que se calculan las duraciones.
type apiFalsa struct {
	ahora  time.Time
	estado domain.RoomState

	kicks     int
	guardados int
	activados int
	errKick   error

	// exposición nil devuelve el informe CIEGO, que es lo correcto para un
	// falso que no está ejercitando esta capa: el cero no puede leerse como
	// "no hay nada abierto".
	exposición *domain.ExposureReport

	// sondeo nil devuelve el informe CIEGO, por lo mismo que exposición.
	sondeo    *domain.ProbeReport
	errSondeo error
}

func (a *apiFalsa) Now() time.Time { return a.ahora }

func (a *apiFalsa) Status() domain.RoomState { return a.estado }
func (a *apiFalsa) MissingGame() string      { return "" }

func (a *apiFalsa) CreateRoom(context.Context, domain.Nickname, string) (domain.RoomState, error) {
	return a.estado, nil
}

func (a *apiFalsa) JoinRoom(context.Context, string, domain.Nickname) (domain.RoomState, error) {
	return a.estado, nil
}

func (a *apiFalsa) LeaveRoom(context.Context) domain.RoomState { return a.estado }

func (a *apiFalsa) ActivateProfile(context.Context, string) (domain.RoomState, error) {
	a.activados++
	return a.estado, nil
}

func (a *apiFalsa) KickMember(context.Context, netip.Addr) (domain.RoomState, error) {
	a.kicks++
	return a.estado, a.errKick
}

func (a *apiFalsa) RotateInviteCode(context.Context) (domain.RoomState, error) {
	return a.estado, nil
}

func (a *apiFalsa) RenameRoom(context.Context, string) (domain.RoomState, error) {
	return a.estado, nil
}

func (a *apiFalsa) InviteLink() string { return "" }

func (a *apiFalsa) Catalog() (domain.Catalog, []domain.GameRef) {
	return domain.Catalog{}, nil
}

func (a *apiFalsa) ListGames() []domain.GameProfile         { return nil }
func (a *apiFalsa) RejectedGames() []domain.RejectedProfile { return nil }

func (a *apiFalsa) SaveProfile(_ context.Context, p domain.GameProfile, _ bool) (domain.GameProfile, error) {
	a.guardados++
	return p, nil
}

func (a *apiFalsa) ImportCatalog(context.Context, []byte, []string) ([]domain.ImportCandidate, error) {
	return nil, nil
}

func (a *apiFalsa) ExportCatalog(string, string, string, bool) ([]byte, error)  { return nil, nil }
func (a *apiFalsa) MarkVerified(context.Context, string, domain.Verified) error { return nil }

func (a *apiFalsa) ForeignRulesFor(context.Context, string) ([]domain.ForeignRule, error) {
	return nil, nil
}

func (a *apiFalsa) SuspendForeignRules(context.Context, []domain.ForeignRule) error { return nil }

func (a *apiFalsa) Diagnose(context.Context) (domain.NetCheck, error) {
	return domain.NetCheck{NATKind: "cone"}, nil
}

func (a *apiFalsa) Exposure(context.Context) domain.ExposureReport {
	if a.exposición != nil {
		return *a.exposición
	}
	return domain.BlindExposure()
}

func (a *apiFalsa) ProbeHost(context.Context) (domain.ProbeReport, error) {
	if a.errSondeo != nil {
		return domain.ProbeReport{}, a.errSondeo
	}
	if a.sondeo != nil {
		return *a.sondeo, nil
	}
	return domain.ProbeReport{}, nil
}

func (a *apiFalsa) ObserveGame(context.Context, domain.ProcessRef, map[int]bool, bool) ([]domain.PortRange, error) {
	return nil, nil
}

func (a *apiFalsa) PendingRoom() (domain.PersistedRoom, bool)            { return domain.PersistedRoom{}, false }
func (a *apiFalsa) ResumeRoom(context.Context) (domain.RoomState, error) { return a.estado, nil }
func (a *apiFalsa) DiscardPendingRoom(context.Context) error             { return nil }
func (a *apiFalsa) LastRoom() (domain.LastRoom, bool)                    { return domain.LastRoom{}, false }

// TestLaSesiónDeVerdadSatisfaceLaAPI.
//
// Es la comprobación que evita el fallo aburrido y caro: que la interfaz de
// este paquete y la sesión se separen sin que nadie lo note hasta cablear el
// servicio. Vive en un test para que el paquete de producción no dependa de
// usecase por una aserción.
func TestLaSesiónDeVerdadSatisfaceLaAPI(t *testing.T) {
	var _ API = (*usecase.Session)(nil)
}

// TestLaExposiciónCiegaNoViajaConLista.
//
// Es el campo que decide cómo se pinta la pantalla entera. Si un informe ciego
// pudiera llevar puertos, la UI enseñaría la última lista buena sobre una
// medición que no ocurrió, que es justo la mentira que el tipo existe para
// impedir.
func TestLaExposiciónCiegaNoViajaConLista(t *testing.T) {
	s, api := servidor(t)
	saluda(t, s, tokenDePrueba)

	ciego := domain.BlindExposure()
	api.exposición = &ciego

	var v ExposureView
	lee(t, pide(t, s, `{"id":30,"method":"exposure"}`), &v)

	switch {
	case v.Measured:
		t.Error("un informe ciego viajó como medido")
	case len(v.Ports) != 0:
		t.Errorf("un informe ciego viajó con %d puertos", len(v.Ports))
	case v.MeasuredAtMS != 0:
		t.Errorf("un informe ciego viajó con hora de medición %d", v.MeasuredAtMS)
	case v.Gate != "unknown":
		t.Errorf("un informe ciego dice que la compuerta está %q", v.Gate)
	}
}

// TestLaExposiciónMedidaLlevaPuertosYAlcance.
//
// Un puerto abierto sin decir para quién es la mitad de la información que
// importa, y es justo la mitad que sostiene la promesa del producto.
func TestLaExposiciónMedidaLlevaPuertosYAlcance(t *testing.T) {
	s, api := servidor(t)
	saluda(t, s, tokenDePrueba)

	var rs domain.RuleSet
	rs.Rules = append(rs.Rules, domain.FirewallRule{
		Name:   "kanpachi-udp-16261",
		Proto:  domain.ProtoUDP,
		From:   16261,
		To:     16262,
		Local:  netip.MustParseAddr("100.64.1.1"),
		Remote: []netip.Addr{netip.MustParseAddr("100.64.1.5")},
	})
	informe := domain.NewExposureReport(
		rs,
		domain.Enforcement{
			Rules: []domain.AppliedRule{{
				Name: "kanpachi-udp-16261", Layer: domain.LayerFirewallRules, Enabled: true,
			}},
			Gate: domain.GatePresent,
		},
		true,
		api.ahora,
	)
	api.exposición = &informe

	var v ExposureView
	lee(t, pide(t, s, `{"id":31,"method":"exposure"}`), &v)

	switch {
	case !v.Measured:
		t.Fatal("un informe medido viajó como ciego")
	case v.Gate != "present":
		t.Errorf("gate = %q", v.Gate)
	case v.MeasuredAtMS != api.ahora.UnixMilli():
		t.Errorf("measured_at_ms = %d", v.MeasuredAtMS)
	case len(v.Ports) != 1:
		t.Fatalf("puertos = %+v", v.Ports)
	}

	p := v.Ports[0]
	switch {
	case p.Proto != "udp" || p.From != 16261 || p.To != 16262:
		t.Errorf("el rango viajó como %s/%d-%d", p.Proto, p.From, p.To)
	case !p.Applied:
		t.Error("un puerto que el sistema tiene puesto viajó como no aplicado")
	case len(p.Members) != 1 || p.Members[0] != "100.64.1.5":
		t.Errorf("el alcance remoto viajó como %v", p.Members)
	}
}

// TestCadaEstadoDeLaCompuertaTieneNombreEnLaAPI.
//
// Por lo mismo que las alertas: un estado que llega como "unknown" sin serlo
// hace que la pantalla diga "no se pudo comprobar" sobre algo que sí se
// comprobó, y el usuario aprende a ignorarla.
func TestCadaEstadoDeLaCompuertaTieneNombreEnLaAPI(t *testing.T) {
	quiero := map[domain.GateState]string{
		domain.GatePresent: "present",
		domain.GateAbsent:  "absent",
		domain.GateUnknown: "unknown",
	}
	for estado, nombre := range quiero {
		if got := gateName(estado); got != nombre {
			t.Errorf("gateName(%v) = %q, se esperaba %q", estado, got, nombre)
		}
	}
}

// TestElSondeoCiegoNoViajaConResultados.
//
// Por lo mismo que la exposición ciega: si un sondeo sin medir pudiera llevar
// filas, la pantalla enseñaría el resultado de una comprobación que no ocurrió.
func TestElSondeoCiegoNoViajaConResultados(t *testing.T) {
	s, api := servidor(t)
	saluda(t, s, tokenDePrueba)
	api.sondeo = &domain.ProbeReport{}

	var v ProbeView
	lee(t, pide(t, s, `{"id":40,"method":"probe_host"}`), &v)

	switch {
	case v.Measured:
		t.Error("un sondeo ciego viajó como medido")
	case len(v.Results) != 0:
		t.Errorf("un sondeo ciego viajó con %d resultados", len(v.Results))
	case v.Verdict != "blind":
		t.Errorf("veredicto = %q, se esperaba blind", v.Verdict)
	case v.Target != "":
		t.Errorf("un sondeo ciego viajó con destino %q", v.Target)
	}
}

// TestElSondeoConFugaViajaConSuVeredicto.
//
// Es la fila que da todo el valor de la pantalla: un puerto que nadie pidió
// contestando desde otra máquina.
func TestElSondeoConFugaViajaConSuVeredicto(t *testing.T) {
	s, api := servidor(t)
	saluda(t, s, tokenDePrueba)

	api.sondeo = &domain.ProbeReport{
		Target:     netip.MustParseAddr("100.64.1.1"),
		MeasuredAt: api.ahora,
		Results: []domain.ProbeResult{
			{
				ProbeTarget: domain.ProbeTarget{
					Port: domain.ControlPort, Kind: domain.ProbeReference, Label: "el canal de la sala",
				},
				Outcome: domain.ProbeAnswered,
				RTT:     12 * time.Millisecond,
			},
			{
				ProbeTarget: domain.ProbeTarget{
					Port: 445, Kind: domain.ProbeForbidden, Label: "compartir archivos (SMB)",
				},
				Outcome: domain.ProbeAnswered,
				RTT:     9 * time.Millisecond,
			},
		},
	}

	var v ProbeView
	lee(t, pide(t, s, `{"id":41,"method":"probe_host"}`), &v)

	switch {
	case !v.Measured:
		t.Fatal("un sondeo medido viajó como ciego")
	case v.Verdict != "leaky":
		t.Fatalf("veredicto = %q, se esperaba leaky", v.Verdict)
	case v.Target != "100.64.1.1":
		t.Errorf("destino = %q", v.Target)
	case v.MeasuredAtMS != api.ahora.UnixMilli():
		t.Errorf("measured_at_ms = %d", v.MeasuredAtMS)
	case len(v.Results) != 2:
		t.Fatalf("resultados = %+v", v.Results)
	}

	fuga := v.Results[1]
	switch {
	case fuga.Port != 445 || fuga.Kind != "forbidden":
		t.Errorf("la fuga viajó como %d/%s", fuga.Port, fuga.Kind)
	case fuga.Outcome != "answered":
		t.Errorf("outcome = %q", fuga.Outcome)
	case fuga.Label != "compartir archivos (SMB)":
		t.Errorf("label = %q", fuga.Label)
	case fuga.RTTMS != 9:
		t.Errorf("rtt_ms = %d", fuga.RTTMS)
	}
}

// TestElSilencioNoLlevaTiempoDeRespuesta.
//
// En el silencio lo que se mediría es el plazo, que ya se sabe de antemano.
// Enseñarlo haría creer que hubo respuesta.
func TestElSilencioNoLlevaTiempoDeRespuesta(t *testing.T) {
	s, api := servidor(t)
	saluda(t, s, tokenDePrueba)

	api.sondeo = &domain.ProbeReport{
		Target:     netip.MustParseAddr("100.64.1.1"),
		MeasuredAt: api.ahora,
		Results: []domain.ProbeResult{{
			ProbeTarget: domain.ProbeTarget{Port: 445, Kind: domain.ProbeForbidden, Label: "SMB"},
			Outcome:     domain.ProbeSilent,
			RTT:         domain.ProbeDeadline,
		}},
	}

	var v ProbeView
	lee(t, pide(t, s, `{"id":42,"method":"probe_host"}`), &v)

	if v.Results[0].RTTMS != 0 {
		t.Fatalf("un silencio viajó con rtt_ms = %d", v.Results[0].RTTMS)
	}
	if v.Verdict != "unreachable" {
		t.Fatalf("veredicto = %q: sin referencia viva no se puede afirmar nada", v.Verdict)
	}
}

// TestElHostQueSeSondeaASiMismoRecibeSuCodigo.
//
// Con un código propio y no "internal", porque la pantalla tiene que decir la
// frase correcta: esto lo pulsa otro.
func TestElHostQueSeSondeaASiMismoRecibeSuCodigo(t *testing.T) {
	s, api := servidor(t)
	saluda(t, s, tokenDePrueba)
	api.errSondeo = usecase.ErrProbeSelf

	res := pide(t, s, `{"id":43,"method":"probe_host"}`)
	if res.Error == nil {
		t.Fatal("no llegó error")
	}
	if res.Error.Code != CodeProbeSelf {
		t.Fatalf("código = %q, se esperaba %q", res.Error.Code, CodeProbeSelf)
	}
}

// TestCadaClaseYResultadoDelSondeoTieneNombreEnLaAPI.
//
// Por lo mismo que los estados de la compuerta. Y los respaldos importan más
// acá: una clase sin nombre viaja como PROHIBIDA y un resultado sin nombre como
// FALLO, que son los dos lados ruidosos. Al revés, una clase nueva se pintaría
// como puerto de juego y su respuesta pasaría por normal.
func TestCadaClaseYResultadoDelSondeoTieneNombreEnLaAPI(t *testing.T) {
	clases := map[domain.ProbeKind]string{
		domain.ProbeReference: "reference",
		domain.ProbeForbidden: "forbidden",
		domain.ProbeGame:      "game",
	}
	for k, nombre := range clases {
		if got := probeKindName(k); got != nombre {
			t.Errorf("probeKindName(%v) = %q, se esperaba %q", k, got, nombre)
		}
	}
	if len(clases) != len(domain.AllProbeKinds()) {
		t.Errorf("hay %d clases en el dominio y %d nombradas acá",
			len(domain.AllProbeKinds()), len(clases))
	}

	resultados := map[domain.ProbeOutcome]string{
		domain.ProbeAnswered: "answered",
		domain.ProbeRefused:  "refused",
		domain.ProbeSilent:   "silent",
		domain.ProbeFailed:   "failed",
	}
	for o, nombre := range resultados {
		if got := probeOutcomeName(o); got != nombre {
			t.Errorf("probeOutcomeName(%v) = %q, se esperaba %q", o, got, nombre)
		}
	}
	if len(resultados) != len(domain.AllProbeOutcomes()) {
		t.Errorf("hay %d resultados en el dominio y %d nombrados acá",
			len(domain.AllProbeOutcomes()), len(resultados))
	}

	veredictos := map[domain.ProbeVerdict]string{
		domain.VerdictBlind:       "blind",
		domain.VerdictLeaky:       "leaky",
		domain.VerdictUnreachable: "unreachable",
		domain.VerdictSealed:      "sealed",
	}
	for v, nombre := range veredictos {
		if got := verdictName(v); got != nombre {
			t.Errorf("verdictName(%v) = %q, se esperaba %q", v, got, nombre)
		}
	}
	if len(veredictos) != len(domain.AllProbeVerdicts()) {
		t.Errorf("hay %d veredictos en el dominio y %d nombrados acá",
			len(domain.AllProbeVerdicts()), len(veredictos))
	}

	// Los respaldos, que son la parte que decide si un enum nuevo hace ruido o
	// pasa desapercibido.
	if got := probeKindName(domain.ProbeKind(99)); got != "forbidden" {
		t.Errorf("una clase desconocida viajó como %q, y tiene que hacer ruido", got)
	}
	if got := probeOutcomeName(domain.ProbeOutcome(99)); got != "failed" {
		t.Errorf("un resultado desconocido viajó como %q, y no puede leerse como cerrado", got)
	}
	if got := verdictName(domain.ProbeVerdict(99)); got != "blind" {
		t.Errorf("un veredicto desconocido viajó como %q, y no puede afirmar nada", got)
	}
}
