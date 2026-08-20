package protocol

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/netip"
	"sync/atomic"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/core/port"
	"github.com/accentiostudios/kanpachi/core/usecase"
)

// API es lo que el protocolo puede pedirle al daemon.
//
// Es una interfaz declarada acá y no *usecase.Session, por lo mismo que en el
// supervisor: lo que no está en esta lista no se puede exponer por el pipe ni
// por accidente. Faltan a propósito `IssueCredentialFor`, `OnRoomAnnounce`,
// `OnEngineEvent` y las demás entradas del supervisor y del canal de control:
// esas llegan por otros caminos y ponerlas acá las volvería invocables desde
// cualquier proceso del usuario.
type API interface {
	Status() domain.RoomState
	MissingGame() string

	// The `replace` on both is the caller saying that leaving whatever is in the
	// way is fine. False refuses, with an error that NAMES what was in the way so
	// the caller can ask about it. See [usecase.ErrWouldDisplace].
	//
	// `allowFirewall` is the same shape for the other consent: a foreign
	// firewall (ufw, firewalld) denying inbound gets opened, with the exact
	// commands already shown to the person. False refuses naming who blocks
	// and what would open it. See [usecase.ErrFirewallBlocks].
	CreateRoom(ctx context.Context, nick domain.Nickname, roomName string, replace, allowFirewall bool) (domain.RoomState, error)
	JoinRoom(ctx context.Context, input string, nick domain.Nickname, replace, allowFirewall bool) (domain.RoomState, error)
	// QuarantineQuestion and DecideQuarantine are the door and the switch of
	// the base quarantine. The door only refuses callers that ASKED to be
	// refused (`quarantine: "ask"`), which is how the CLI makes the choice
	// mandatory while the window never blocks; the switch is the decision made
	// true in either direction, idempotent both ways. See
	// [usecase.Session.DecideQuarantine].
	QuarantineQuestion() (domain.QuarantineDecision, []uint16)
	DecideQuarantine(ctx context.Context, wanted bool) (domain.QuarantineState, error)
	LeaveRoom(ctx context.Context) domain.RoomState
	ActivateProfile(ctx context.Context, gameID string) (domain.RoomState, error)
	KickMember(ctx context.Context, ip netip.Addr) (domain.RoomState, error)
	RotateInviteCode(ctx context.Context) (domain.RoomState, error)
	RenameRoom(ctx context.Context, name string) (domain.RoomState, error)
	InviteLink() string
	// PeekInvite mira qué hay detrás de un enlace SIN entrar y sin tocar la
	// sesión. Es lo que llena la pantalla de confirmación de un `kanpachi://`.
	PeekInvite(ctx context.Context, link string) (usecase.InvitePreview, error)

	Catalog() (domain.Catalog, []domain.GameRef)
	ListGames() []domain.GameProfile
	RejectedGames() []domain.RejectedProfile
	SaveProfile(ctx context.Context, p domain.GameProfile, replace bool) (domain.GameProfile, error)
	ImportCatalog(ctx context.Context, raw []byte, chosen []string) ([]domain.ImportCandidate, error)
	ExportCatalog(at, by, version string, all bool) ([]byte, error)
	MarkVerified(ctx context.Context, gameID string, v domain.Verified) error

	ForeignRulesFor(ctx context.Context, gameID string) ([]domain.ForeignRule, error)
	SuspendForeignRules(ctx context.Context, rules []domain.ForeignRule) error
	Diagnose(ctx context.Context) (domain.NetCheck, error)
	// Exposure no devuelve error a propósito: la pantalla tiene que decir algo
	// siempre, y lo que dice cuando la medición falla ya viaja dentro del
	// informe. Ver [domain.ExposureReport].
	Exposure(ctx context.Context) domain.ExposureReport
	// ProbeHost sí devuelve error, al revés que Exposure, y la asimetría tiene
	// razón: acá los fallos son PRECONDICIONES que el usuario puede entender y
	// cambiar. Ser el host o no saber dónde está no son mediciones que salieron
	// mal, son motivos por los que no se puede medir, y cada uno lleva su
	// código para que la pantalla diga la frase correcta.
	ProbeHost(ctx context.Context) (domain.ProbeReport, error)
	// ReapplyProtection es la acción que ofrece la alarma del canario, y es
	// IDEMPOTENTE: reaplicar con nada roto no toca el firewall.
	//
	// Vale para los DOS roles. Un invitado también tiene protección, su propio
	// deny-all más la compuerta, y reponerla es el mismo acto.
	ReapplyProtection(ctx context.Context) (domain.RoomState, error)
	ObserveGame(ctx context.Context, root domain.ProcessRef, tree map[int]bool, keepSteam bool) ([]domain.PortRange, error)

	// OwnSeed, SuggestedSeed y SetOwnSeed son el registro de esta máquina.
	//
	// Están en la API y no en el host de la interfaz porque **son estado del
	// daemon**: de acá sale a quién se le pide abrir una sala, y las caras del
	// cliente, la ventana y la terminal, tienen que ver el mismo valor.
	OwnSeed() string
	SuggestedSeed() string
	// SetOwnSeed lleva contexto porque COMPRUEBA que el registro conteste antes
	// de guardarlo, y esa comprobación es una petición de red con su plazo.
	SetOwnSeed(ctx context.Context, seed string) (string, error)
	// SeedPassword entrega el password del registro propio. Ver
	// [MethodSeedPassword] para lo que no vuelve ni queda de él.
	SeedPassword(ctx context.Context, password string) error

	// Nickname, SuggestedNickname y SetNickname son el nombre de esta máquina,
	// y están acá por lo mismo que el registro: hay una máquina y tres caras,
	// así que hay un nombre. Cuando cada cara lo guardaba por su cuenta, la
	// ventana decía «Alvaro» y la terminal entraba a la sala como
	// «AlvaroGDeskt». Medido el 2026-08-18.
	//
	// El apodo SIGUE viajando como parámetro de crear y entrar, y el daemon
	// sigue sin persistir lo que llega por ahí. Lo que cambió es quién lo
	// recuerda entre sala y sala.
	Nickname() string
	SuggestedNickname() string
	// SetNickname lleva contexto por simetría con [API.SetOwnSeed], y a
	// diferencia de aquél no habla con nadie: un nombre no necesita que ningún
	// servidor conteste, así que esto funciona sin conexión.
	SetNickname(ctx context.Context, nick string) (string, error)

	SavedRoom() (domain.HostedRoom, bool)
	ResumeRoom(ctx context.Context) (domain.RoomState, error)
	DiscardSavedRoom(ctx context.Context) error
	LastRoom() (domain.LastRoom, bool)
	ForgetLastRoom(ctx context.Context) error

	// Progress son los pasos de la operación larga que esté en curso, o los de
	// la última si ya terminó.
	//
	// **No toma el candado de la sesión, y ahí está todo el punto.** Crear una
	// sala lo tiene tomado durante decenas de segundos, y esto existe para
	// poder mirar dentro de esa espera: colgándolo del mismo candado, quien
	// preguntara se quedaría esperando exactamente lo que quiere observar. Ver
	// [usecase.Journal].
	//
	// Sin error: no hay nada que pueda fallar, y un diario vacío es una
	// respuesta legítima que significa que no ha pasado nada todavía.
	Progress() domain.Progress

	// Cancel corta la operación larga en curso. Devuelve si había alguna.
	//
	// Sin error por el mismo motivo que [Progress]: no hay nada que pueda
	// fallar. "No había ninguna" es una respuesta legítima, y la más común de
	// las dos: pasa cada vez que se pulsa Cancelar en el instante en que la
	// operación estaba terminando sola.
	Cancel() bool
}

// Host es lo que el protocolo puede pedirle al PROCESO del daemon, en vez de a
// la sala.
//
// Está aparte de [API] a propósito, y no es organización: son dos superficies
// con dueños distintos. [API] es la sala, que vive en `core/usecase` y se prueba
// sin Windows. Esto es el proceso —su interfaz, su apagado, su tipo de
// arranque—, que solo el `main` puede saber montar. Meterlas juntas obligaría a
// que el caso de uso conociera al servicio que lo hospeda.
//
// Es OPCIONAL. En modo consola no hay interfaz que lanzar ni servicio que
// reconfigurar, así que sin host los tres métodos contestan que no están
// disponibles, que es la verdad y no un fallo.
type Host interface {
	// ShowUI enseña la ventana de la interfaz, lanzándola si hace falta.
	//
	// `link` es el `kanpachi://` que trajo el navegador, o vacío cuando lo que
	// se pide es solo abrir la ventana. Se GUARDA antes de mostrar nada, para
	// que la interfaz lo encuentre ya puesto cuando pregunte.
	ShowUI(link string) error
	// TakePendingInvite devuelve el enlace guardado y lo OLVIDA.
	//
	// Devolverlo y olvidarlo en el mismo acto es lo que impide que la pantalla
	// de confirmación reaparezca en cada latido después de cancelarla.
	TakePendingInvite() string
	// Shutdown apaga TODO de forma coordinada: sale de la sala, purga las
	// reglas, baja el motor y el adaptador, y al final se detiene el daemon.
	//
	// No devuelve nada y no bloquea. Quien lo pide es la interfaz, que se va a
	// morir en el camino: esperar una respuesta que nadie va a poder leer sería
	// dejar colgado el apagado a la espera de un lector muerto.
	Shutdown()
	// Autostart dice si Kanpachi arranca con Windows.
	Autostart() (bool, error)
	// SetAutostart cambia si Kanpachi arranca con Windows.
	//
	// Lo hace el DAEMON consigo mismo y no la interfaz, y esa es la razón de
	// que exista este método: cambiar el tipo de arranque de un servicio exige
	// `SERVICE_CHANGE_CONFIG`, y concedérselo al usuario para tener un
	// interruptor sería ampliar permisos por comodidad. El daemon ya es SYSTEM.
	SetAutostart(bool) error
	// EngineInfo dice qué motor va a lanzar este proceso: el build id y la
	// librería de red, leídos de los centinelas del FICHERO que tiene al lado.
	// Vacíos con un motor anterior al centinela, que es un hallazgo normal.
	// Es del Host y no de la sesión porque describe la INSTALACIÓN, igual que
	// el autostart, y quien sabe dónde vive el motor es el `main`.
	EngineInfo() (build, lib string)
}

// Server atiende una conexión.
//
// Una instancia por conexión, y ahí está el estado de autenticación: que una
// conexión haya saludado no autentica a la siguiente.
type Server struct {
	api   API
	host  Host
	token string
	clock port.Clock
	log   port.Logger

	// saludó es ATÓMICO porque se lee desde otra goroutine.
	//
	// Quien vigila el plazo del saludo no puede ser el hilo que atiende: ese
	// está bloqueado en Read, esperando justo el mensaje que tendría que
	// vigilar. Así que el vigilante corre aparte y pregunta por [Server.Greeted],
	// mientras esta goroutine lo escribe en el hello.
	//
	// Con un bool suelto sería una carrera de las que el job de Linux detecta
	// con -race, y de las que en producción se manifiestan una vez cada mil
	// arranques.
	saludó atomic.Bool
}

// NewServer arma el servidor de UNA conexión.
func NewServer(api API, token string, clock port.Clock, log port.Logger) *Server {
	return &Server{api: api, token: token, clock: clock, log: log}
}

// AttachHost le da al servidor el proceso que lo hospeda.
//
// Es un método de unión aparte del constructor porque el host es OPCIONAL y
// porque quien lo tiene es el `main`, que construye el listener y no cada
// conexión. El guardián de cableado de `internal/arch` vigila los métodos que
// empiezan por `Attach` justo por esto: `control.Attach` estuvo escrito, probado
// y sin llamar, y costó una sesión entera de diagnóstico.
func (s *Server) AttachHost(h Host) { s.host = h }

// Greeted dice si esta conexión ya saludó con el token válido.
//
// Existe para el vigilante del plazo del saludo, que vive en el transporte:
// este paquete no sabe qué hacer con una conexión que se calla, porque cerrarla
// es cosa de quien tiene el socket.
func (s *Server) Greeted() bool { return s.saludó.Load() }

// Serve atiende hasta que la conexión se cierre o el contexto se cancele.
//
// Devuelve nil en un cierre limpio. Un error de enmarcado es terminal para la
// conexión: con mensajes delimitados por líneas, uno que no cupo deja el flujo
// desincronizado, así que seguir leyendo sería interpretar la cola de un
// mensaje gigante como mensajes nuevos.
func (s *Server) Serve(ctx context.Context, rw io.ReadWriter) error {
	r := NewReader(rw)
	w := NewWriter(rw)

	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		linea, err := r.ReadLine()
		switch {
		case errors.Is(err, io.EOF):
			return nil
		case errors.Is(err, ErrTooLarge):
			// Se contesta y se corta. El aviso es cortesía para que el cliente
			// sepa por qué, y cortar no es negociable.
			_ = w.Write(Response{Error: &Error{Code: CodeTooLarge, Message: err.Error()}})
			return err
		case err != nil:
			return err
		}

		resp := s.handle(ctx, linea)
		if err := w.Write(resp); err != nil {
			return err
		}
	}
}

// handle interpreta una línea y produce la respuesta.
//
// Nunca entra en pánico hacia afuera y nunca deja de responder: un cliente sin
// respuesta es una UI colgada, que del lado del usuario se ve peor que un
// error.
func (s *Server) handle(ctx context.Context, linea []byte) Response {
	var req Request
	if e := decodeInto(linea, &req); e != nil {
		// El id viaja aunque el sobre no pase, para que el error le llegue a
		// quien preguntó en vez de quedarse sin dueño. Ver `decodeInto`.
		return Response{ID: req.ID, Error: e}
	}
	resp := Response{ID: req.ID}

	if !req.Method.Known() {
		// No se interpreta, no se registra el contenido, no se adivina. Lo que
		// no está en la tabla no existe.
		resp.Error = badRequest("método desconocido")
		return resp
	}

	// El saludo va PRIMERO y es lo único que se admite antes de él. Sin esta
	// puerta, un proceso que no tenga el token igual podría pedir el estado, y
	// el estado dice en qué sala estás y con quién.
	if req.Method == MethodHello {
		resp.Result, resp.Error = s.hello(req.Params)
		return resp
	}
	if !s.saludó.Load() {
		resp.Error = &Error{Code: CodeUnauthorized, Message: "hay que saludar con el token antes de pedir nada"}
		return resp
	}

	resp.Result, resp.Error = s.dispatch(ctx, req)
	return resp
}

// hello comprueba el token.
//
// Comparación en tiempo constante. El token vive en ProgramData con lectura
// para los usuarios de la máquina, así que esto no es lo que separa al usuario
// de sus propios datos: lo que acota la superficie es la lista cerrada de
// métodos. La comparación constante cuesta nada y evita el único ataque que
// esta capa sí puede evitar.
func (s *Server) hello(params json.RawMessage) (json.RawMessage, *Error) {
	p, e := decodeStrict[struct {
		Token string `json:"token"`
	}](params)
	if e != nil {
		return nil, e
	}
	if subtle.ConstantTimeCompare([]byte(p.Token), []byte(s.token)) != 1 {
		s.log.Warn("saludo rechazado por token inválido")
		return nil, &Error{Code: CodeUnauthorized, Message: "token inválido"}
	}
	s.saludó.Store(true)
	return result(struct {
		OK bool `json:"ok"`
	}{true})
}

func (s *Server) dispatch(ctx context.Context, req Request) (json.RawMessage, *Error) {
	switch req.Method {
	case MethodStatus:
		return s.room(s.api.Status())

	case MethodCreateRoom:
		p, e := decodeStrict[struct {
			Nickname string `json:"nickname"`
			Name     string `json:"name"`
			// Replace is the caller saying that leaving whatever is in the way is
			// fine. Its zero refuses, which is what makes forgetting to send it
			// safe rather than destructive.
			Replace bool `json:"replace,omitempty"`
			// AllowFirewall is the other consent, with the same zero-refuses
			// safety: opening a foreign firewall that blocks the room's
			// inbound. See [usecase.ErrFirewallBlocks].
			AllowFirewall bool `json:"allow_firewall,omitempty"`
			// Quarantine is the caller's stance on the base-quarantine
			// question: "" says nothing (the window's stance), "ask" demands a
			// refusal while undecided (the CLI's), and "on"/"off" IS the
			// decision, recorded and made true before the room opens.
			Quarantine string `json:"quarantine,omitempty"`
		}](req.Params)
		if e != nil {
			return nil, e
		}
		if e := s.quarantineGate(ctx, p.Quarantine); e != nil {
			return nil, e
		}
		nick, err := domain.ParseNickname(p.Nickname)
		if err != nil {
			return nil, errorFor(err)
		}
		st, err := s.api.CreateRoom(ctx, nick, p.Name, p.Replace, p.AllowFirewall)
		return s.roomOrErr(st, err)

	case MethodJoinRoom:
		p, e := decodeStrict[struct {
			Code          string `json:"code"`
			Nickname      string `json:"nickname"`
			Replace       bool   `json:"replace,omitempty"`
			AllowFirewall bool   `json:"allow_firewall,omitempty"`
			Quarantine    string `json:"quarantine,omitempty"`
		}](req.Params)
		if e != nil {
			return nil, e
		}
		if e := s.quarantineGate(ctx, p.Quarantine); e != nil {
			return nil, e
		}
		nick, err := domain.ParseNickname(p.Nickname)
		if err != nil {
			return nil, errorFor(err)
		}
		st, err := s.api.JoinRoom(ctx, p.Code, nick, p.Replace, p.AllowFirewall)
		return s.roomOrErr(st, err)

	case MethodLeaveRoom:
		return s.room(s.api.LeaveRoom(ctx))

	case MethodActivateProfile:
		p, e := decodeStrict[struct {
			Game string `json:"game"`
		}](req.Params)
		if e != nil {
			return nil, e
		}
		// El id vacío es legal y cierra todos los puertos. La frontera de "solo
		// perfiles del catálogo" la aplica el caso de uso, que es quien tiene
		// el catálogo: acá no se valida el id contra nada, se pasa tal cual.
		st, err := s.api.ActivateProfile(ctx, p.Game)
		return s.roomOrErr(st, err)

	case MethodKickMember:
		p, e := decodeStrict[struct {
			IP string `json:"ip"`
		}](req.Params)
		if e != nil {
			return nil, e
		}
		ip, err := netip.ParseAddr(p.IP)
		if err != nil {
			return nil, badRequest("esa no es una dirección: %v", err)
		}
		st, err := s.api.KickMember(ctx, ip)
		if err != nil && errors.Is(err, usecase.ErrKickPartial) {
			// Una expulsión a medias devuelve estado VÁLIDO junto al error, y
			// las dos cosas viajan: la UI necesita redibujar la lista sin el
			// expulsado y mostrar el aviso de lo que no se pudo cerrar.
			return s.roomWithError(st, errorFor(err))
		}
		return s.roomOrErr(st, err)

	case MethodRotateInviteCode:
		st, err := s.api.RotateInviteCode(ctx)
		return s.roomOrErr(st, err)

	case MethodRenameRoom:
		p, e := decodeStrict[struct {
			Name string `json:"name"`
		}](req.Params)
		if e != nil {
			return nil, e
		}
		st, err := s.api.RenameRoom(ctx, p.Name)
		return s.roomOrErr(st, err)

	case MethodInviteLink:
		return result(struct {
			Link string `json:"link"`
		}{s.api.InviteLink()})

	case MethodListGames:
		return s.games()

	case MethodRejectedGames:
		out := make([]RejectedView, 0)
		for _, r := range s.api.RejectedGames() {
			out = append(out, RejectedView{ID: r.ID, Reason: r.Reason, Origin: r.Origin.String()})
		}
		return result(out)

	case MethodSaveProfile:
		return s.saveProfile(ctx, req.Params)

	case MethodImportCatalog:
		p, e := decodeStrict[struct {
			File   []byte   `json:"file"`
			Chosen []string `json:"chosen"`
		}](req.Params)
		if e != nil {
			return nil, e
		}
		cands, err := s.api.ImportCatalog(ctx, p.File, p.Chosen)
		if err != nil {
			return nil, errorFor(err)
		}
		return result(candidateViews(cands))

	case MethodExportCatalog:
		p, e := decodeStrict[struct {
			At      string `json:"at"`
			By      string `json:"by"`
			Version string `json:"version"`
			All     bool   `json:"all"`
		}](req.Params)
		if e != nil {
			return nil, e
		}
		raw, err := s.api.ExportCatalog(p.At, p.By, p.Version, p.All)
		if err != nil {
			return nil, errorFor(err)
		}
		return result(struct {
			File []byte `json:"file"`
		}{raw})

	case MethodMarkVerified:
		p, e := decodeStrict[struct {
			Game        string `json:"game"`
			Date        string `json:"date"`
			By          string `json:"by"`
			Method      string `json:"method"`
			GameVersion string `json:"game_version"`
		}](req.Params)
		if e != nil {
			return nil, e
		}
		err := s.api.MarkVerified(ctx, p.Game, domain.Verified{
			Date: p.Date, By: p.By, Method: p.Method, GameVersion: p.GameVersion,
		})
		if err != nil {
			return nil, errorFor(err)
		}
		return result(struct{}{})

	case MethodForeignRules:
		p, e := decodeStrict[struct {
			Game string `json:"game"`
		}](req.Params)
		if e != nil {
			return nil, e
		}
		reglas, err := s.api.ForeignRulesFor(ctx, p.Game)
		if err != nil {
			return nil, errorFor(err)
		}
		return result(foreignViews(reglas))

	case MethodSuspendForeignRules:
		p, e := decodeStrict[struct {
			Rules []ForeignView `json:"rules"`
		}](req.Params)
		if e != nil {
			return nil, e
		}
		if err := s.api.SuspendForeignRules(ctx, foreignRules(p.Rules)); err != nil {
			return nil, errorFor(err)
		}
		return result(struct{}{})

	case MethodDiagReport:
		check, err := s.api.Diagnose(ctx)
		if err != nil {
			return nil, &Error{Code: CodeUnavailable, Message: err.Error()}
		}
		return result(netView(check))

	case MethodExposure:
		return result(exposureView(s.api.Exposure(ctx)))

	case MethodProbeHost:
		r, err := s.api.ProbeHost(ctx)
		if err != nil {
			return nil, errorFor(err)
		}
		return result(probeView(r))

	case MethodQuarantine:
		// Set vacío solo lee. "on" y "off" SON la decisión.
		//
		// Sin parámetros es LEER, y es la mitad más usada: `kanpachi
		// quarantine` a secas manda `params` ausente, igual que `seed` a secas,
		// y `decodeStrict` contestaba "faltan los parámetros". Mismo arreglo
		// medido que [Server.ownSeed]: que un método acepte parámetros ausentes
		// es una decisión de ESE método.
		var p struct {
			Set string `json:"set,omitempty"`
		}
		if len(req.Params) > 0 {
			leído, e := decodeStrict[struct {
				Set string `json:"set,omitempty"`
			}](req.Params)
			if e != nil {
				return nil, e
			}
			p = leído
		}
		switch p.Set {
		case "":
			return s.room(s.api.Status())
		case "on", "off":
			// El error de la operación viaja JUNTO al estado, como en la
			// expulsión a medias: la palabra quedó anotada y la pantalla tiene
			// que redibujar el interruptor con lo medido, diga lo que diga el
			// error.
			if _, err := s.api.DecideQuarantine(ctx, p.Set == "on"); err != nil {
				return s.roomWithError(s.api.Status(), errorFor(err))
			}
			return s.room(s.api.Status())
		default:
			return nil, badRequest(`set tiene que ser "on" u "off", o no venir`)
		}

	case MethodReapplyProtection:
		// Devuelve el estado ENTERO y no un acuse, porque la pantalla tiene que
		// redibujarse: reponer programa la ronda que puede apagar la alarma, y el
		// usuario acaba de pulsar el botón que la enseña.
		st, err := s.api.ReapplyProtection(ctx)
		return s.roomOrErr(st, err)

	case MethodObserveGame:
		return s.observe(ctx, req.Params)

	case MethodShowUI:
		if s.host == nil {
			return nil, sinHost()
		}
		// El enlace es OPCIONAL, y los parámetros ENTEROS también. `show_ui` a
		// secas es el doble clic en el acceso directo, que solo pide ventana, y
		// así lo pidió este método toda su vida: exigir un objeto ahora sería
		// romper a cualquier cliente que se quedara con la forma vieja.
		//
		// El tope de longitud del enlace lo pone el dominio al parsearlo, y
		// quien lo manda es el lanzador, o sea otro proceso del usuario que ya
		// pasó el saludo con token.
		var link string
		if len(req.Params) > 0 {
			p, e := decodeStrict[struct {
				Link string `json:"link"`
			}](req.Params)
			if e != nil {
				return nil, e
			}
			link = p.Link
		}
		if err := s.host.ShowUI(link); err != nil {
			return nil, &Error{Code: CodeUnavailable, Message: err.Error()}
		}
		return result(struct {
			OK bool `json:"ok"`
		}{true})

	case MethodPendingInvite:
		// Sin host no hay quién guarde enlaces, y eso NO es un error: es el
		// modo consola, donde el daemon no hospeda la interfaz. Se contesta que
		// no hay nada pendiente, que es la verdad.
		if s.host == nil {
			return result(InviteView{})
		}
		link := s.host.TakePendingInvite()
		if link == "" {
			return result(InviteView{})
		}
		return result(s.invite(ctx, link))

	case MethodPreviewInvite:
		p, e := decodeStrict[struct {
			Link string `json:"link"`
		}](req.Params)
		if e != nil {
			return nil, e
		}
		return result(s.invite(ctx, p.Link))

	case MethodShutdown:
		if s.host == nil {
			return nil, sinHost()
		}
		// Se contesta ANTES de apagar, y no es cortesía: el apagado se lleva
		// por delante la interfaz que preguntó, así que una respuesta después
		// no la leería nadie y quien la pidió vería el enlace caerse en vez de
		// una confirmación. Ver [Host.Shutdown], que no bloquea.
		s.host.Shutdown()
		return result(struct {
			OK bool `json:"ok"`
		}{true})

	case MethodAutostart:
		return s.autostart(req.Params)

	case MethodEngineInfo:
		if s.host == nil {
			return nil, sinHost()
		}
		build, lib := s.host.EngineInfo()
		return result(struct {
			Build string `json:"build"`
			Lib   string `json:"lib"`
		}{build, lib})

	case MethodOwnSeed:
		return s.ownSeed(ctx, req.Params)

	case MethodNickname:
		return s.nickname(ctx, req.Params)

	case MethodSeedPassword:
		return s.seedPassword(ctx, req.Params)

	case MethodSavedRoom:
		return s.savedRoom()

	case MethodResumeRoom:
		st, err := s.api.ResumeRoom(ctx)
		return s.roomOrErr(st, err)

	case MethodDiscardSavedRoom:
		if err := s.api.DiscardSavedRoom(ctx); err != nil {
			return nil, errorFor(err)
		}
		return result(struct{}{})

	case MethodLastRoom:
		last, hay := s.api.LastRoom()
		if !hay {
			return result(struct {
				Found bool `json:"found"`
			}{false})
		}
		return result(struct {
			Found bool         `json:"found"`
			Room  LastRoomView `json:"room"`
		}{true, LastRoomView{
			Code: last.Room.InviteID.String(), Seed: last.Room.Seed,
			Name: last.Name, Nick: last.Nick.String(), SavedAt: stamp(last.SavedAt),
			AutoReturn: last.AutoReturn,
		}})

	case MethodForgetLastRoom:
		if err := s.api.ForgetLastRoom(ctx); err != nil {
			return nil, errorFor(err)
		}
		return result(struct{}{})

	case MethodProgress:
		return result(progressView(s.api.Progress()))

	case MethodCancel:
		// Se contesta qué pasó y no un acuse a secas: la pantalla que lo pide
		// necesita saber si llegó a tiempo, porque si no llegó lo que viene
		// enseguida es el resultado de la operación que terminó sola.
		return result(struct {
			Canceled bool `json:"canceled"`
		}{s.api.Cancel()})
	}
	// Inalcanzable: Known() ya filtró. Se contesta igual en vez de caerse,
	// porque el día que alguien agregue un método a la tabla y olvide el caso,
	// el síntoma tiene que ser un error y no una UI esperando para siempre.
	return nil, badRequest("el método %q está en la tabla y no tiene manejador", req.Method)
}

// invite resuelve un enlace y lo devuelve como vista.
//
// **Nunca devuelve error**, y esa es la decisión: un enlace que no se entiende
// no es un fallo del método, es un dato que la pantalla tiene que poder
// enseñar. Contestar con error dejaría a la interfaz sin nada que decirle a
// quien acaba de pulsar un botón en su navegador y no vio pasar nada.
func (s *Server) invite(ctx context.Context, link string) InviteView {
	v := InviteView{Link: link}
	p, err := s.api.PeekInvite(ctx, link)
	if err != nil {
		s.log.Warn("llegó un enlace que no se entiende", "error", err)
		return v
	}
	v.Code = p.Room.InviteID.String()
	v.Seed = p.Room.Seed
	v.Room = p.Card.Name
	v.Host = p.Card.Host.String()
	v.Unknown = p.Unknown
	v.Fingerprint = p.Fingerprint
	if p.Verdict != domain.HostUnverified {
		v.Verdict = p.Verdict.String()
	}
	v.KnownNick = p.Known.Nick
	v.KnownFingerprint = domain.Fingerprint(p.Known.Key)
	v.KnownRooms = p.Known.Rooms
	v.Displaces = displacesView(p.Displaces)
	return v
}

func (s *Server) room(st domain.RoomState) (json.RawMessage, *Error) {
	v := roomView(st, s.api.MissingGame(), s.clock.Now())
	// El enlace se pega acá y no dentro de roomView porque la clave de la
	// tarjeta no está en el estado de la sala: vive en la sesión, que es quien
	// la generó al sellar la tarjeta. Ver [RoomView.Link].
	v.Link = s.api.InviteLink()
	return result(v)
}

func (s *Server) roomOrErr(st domain.RoomState, err error) (json.RawMessage, *Error) {
	if err != nil {
		return nil, errorFor(err)
	}
	return s.room(st)
}

// roomWithError manda el estado Y el error. Es el caso de la expulsión a
// medias, que es la única operación cuyo fallo deja un estado que la UI
// necesita.
//
// Hasta ahora además metía la vista entera de la sala DENTRO del texto del
// error, y eso existía por un solo motivo: ningún cliente leía el resultado
// cuando venía acompañado de un error, así que el estado se perdía. El cliente
// de Dart ya lee los dos, de modo que la copia dentro del mensaje es ruido en
// el log y bytes duplicados en un cable con tope de un mega.
func (s *Server) roomWithError(st domain.RoomState, e *Error) (json.RawMessage, *Error) {
	raw, err2 := s.room(st)
	if err2 != nil {
		return nil, err2
	}
	return raw, e
}

// games arma la lista, con los instalados marcados.
//
// La detección ORDENA y jamás filtra: la marca es un atajo para la pantalla, no
// una puerta. Un juego que la detección no encontró aparece igual y funciona
// igual, porque Kanpachi no tiene por qué tener la razón sobre qué hay
// instalado en la máquina.
func (s *Server) games() (json.RawMessage, *Error) {
	_, instalados := s.api.Catalog()
	índice := make(map[int]bool, len(instalados))
	for _, r := range instalados {
		if r.SteamAppID != 0 {
			índice[r.SteamAppID] = true
		}
	}
	perfiles := s.api.ListGames()
	out := make([]GameView, 0, len(perfiles))
	for _, p := range perfiles {
		out = append(out, gameView(p, índice[p.Detect.SteamAppID]))
	}
	return result(out)
}

// saveProfile es el alta manual desde el creador de perfiles.
func (s *Server) saveProfile(ctx context.Context, params json.RawMessage) (json.RawMessage, *Error) {
	p, e := decodeStrict[profileParams](params)
	if e != nil {
		return nil, e
	}
	perfil, err := domain.ParseGameProfile(p.Profile, domain.OriginMine)
	if err != nil {
		return nil, &Error{Code: CodeBadProfile, Message: err.Error()}
	}
	guardado, err := s.api.SaveProfile(ctx, perfil, p.Replace)
	if err != nil {
		return nil, errorFor(err)
	}
	return result(gameView(guardado, false))
}

// sinHost es lo que contestan los tres métodos del proceso cuando no hay quien
// los atienda, que es el modo consola.
//
// `unavailable` y no `bad_request`: el método existe, está bien escrito, y lo
// que pasa es que este daemon no tiene interfaz que lanzar ni servicio que
// reconfigurar. Decir que la petición está mal mandaría a buscar el fallo al
// sitio equivocado.
func sinHost() *Error {
	return &Error{
		Code: CodeUnavailable,
		Message: "este daemon no hospeda la interfaz, así que no puede mostrarla, " +
			"apagarla ni cambiar su arranque. Es lo normal en modo consola",
	}
}

// autostart lee o cambia si Kanpachi arranca con Windows.
//
// Un solo método para las dos cosas, con `enabled` opcional: leer y escribir
// son la misma pregunta, y la pantalla que mueve el interruptor necesita el
// valor de vuelta para dibujarlo. Con dos métodos, la UI tendría que encadenar
// dos llamadas y quedarse a medias si la segunda falla.
func (s *Server) autostart(params json.RawMessage) (json.RawMessage, *Error) {
	if s.host == nil {
		return nil, sinHost()
	}
	p, e := decodeStrict[struct {
		Enabled *bool `json:"enabled"`
	}](params)
	if e != nil {
		return nil, e
	}
	if p.Enabled != nil {
		if err := s.host.SetAutostart(*p.Enabled); err != nil {
			return nil, &Error{Code: CodeUnavailable, Message: err.Error()}
		}
	}
	// Se RELEE siempre, incluso justo después de escribir. Devolver lo que se
	// pidió sería devolver la intención en vez del estado, y un cambio que el
	// sistema no aceptó se vería como aceptado.
	puesto, err := s.host.Autostart()
	if err != nil {
		return nil, &Error{Code: CodeUnavailable, Message: err.Error()}
	}
	return result(struct {
		Enabled bool `json:"enabled"`
	}{puesto})
}

// ownSeed lee o cambia el registro de esta máquina.
//
// El valor va en los parámetros y es opcional, igual que en [Server.autostart]:
// sin él es una lectura. Y se RELEE siempre después de escribir, por lo mismo
// que allá, con un motivo extra acá: lo que se guarda es el nombre ya
// normalizado, así que devolver lo que llegó enseñaría lo que se pidió en vez de
// lo que quedó puesto.
func (s *Server) ownSeed(ctx context.Context, params json.RawMessage) (json.RawMessage, *Error) {
	// Sin parámetros es LEER, y esa es la mitad más usada del método.
	//
	// Medido corriendo `kanpachi seed` a secas contra el daemon: el cliente
	// manda `params` ausente cuando no hay host que fijar, y `decodeStrict`
	// contestaba "faltan los parámetros". O sea que la forma que la ayuda
	// documenta, `seed [host]` con el host opcional, no funcionaba.
	//
	// Se arregla acá y no aflojando [decodeStrict]: que un método acepte
	// parámetros ausentes es una decisión de ESE método, y hacerla global
	// dejaría pasar en silencio los que sí los necesitan.
	var p struct {
		Seed *string `json:"seed"`
	}
	if len(params) > 0 {
		leído, e := decodeStrict[struct {
			Seed *string `json:"seed"`
		}](params)
		if e != nil {
			return nil, e
		}
		p.Seed = leído.Seed
	}
	if p.Seed != nil {
		if _, err := s.api.SetOwnSeed(ctx, *p.Seed); err != nil {
			return nil, errorFor(err)
		}
	}
	return result(struct {
		Seed string `json:"seed"`
		// Suggested es de dónde salió la última sala a la que se entró, para
		// prellenar. No es el registro de esta máquina y no manda en nada: la
		// pantalla la enseña marcada como sugerencia.
		Suggested string `json:"suggested"`
	}{s.api.OwnSeed(), s.api.SuggestedSeed()})
}

// nickname lee o cambia el nombre con el que esta máquina entra a las salas.
//
// Copia exacta de la forma de [Server.ownSeed], parámetros opcionales incluidos:
// sin ellos es una lectura, y el valor se RELEE después de escribir porque lo
// que se guarda es el nombre ya validado.
//
// La sugerencia viaja siempre, también cuando hay nombre elegido. Cuesta una
// derivación de nada y le ahorra al que pregunta una segunda llamada el día que
// quiera enseñar de dónde saldría el nombre si se borrara el suyo.
func (s *Server) nickname(ctx context.Context, params json.RawMessage) (json.RawMessage, *Error) {
	var p struct {
		Nickname *string `json:"nickname"`
	}
	if len(params) > 0 {
		leído, e := decodeStrict[struct {
			Nickname *string `json:"nickname"`
		}](params)
		if e != nil {
			return nil, e
		}
		p.Nickname = leído.Nickname
	}
	if p.Nickname != nil {
		if _, err := s.api.SetNickname(ctx, *p.Nickname); err != nil {
			return nil, errorFor(err)
		}
	}
	return result(struct {
		Nickname string `json:"nickname"`
		// Suggested sale del nombre de ESTA máquina, y no se guarda nunca.
		// Quien entra a una sala sin nombre elegido usa este y lo dice.
		Suggested string `json:"suggested"`
	}{s.api.Nickname(), s.api.SuggestedNickname()})
}

// seedPassword entrega el password del registro propio.
//
// # Por qué la respuesta está vacía
//
// Porque no hay nada que devolver que no sea peor devolver. Los tokens se
// quedan en el daemon, el password no se repite, y decir si el registro estaba
// abierto o cerrado sería contestar una pregunta que nadie hizo sobre la máquina
// de otro. Que no haya error ES la respuesta.
//
// El parámetro no se anota en ningún sitio. Este manejador no llama al diario de
// progreso, no registra nada y devuelve el error del caso de uso tal cual, que
// ya viene sin el password dentro.
func (s *Server) seedPassword(ctx context.Context, params json.RawMessage) (json.RawMessage, *Error) {
	p, e := decodeStrict[struct {
		Password string `json:"password"`
	}](params)
	if e != nil {
		return nil, e
	}
	if err := s.api.SeedPassword(ctx, p.Password); err != nil {
		return nil, errorFor(err)
	}
	return result(struct{}{})
}

// observe es la foto de sockets, y es la ÚNICA función del programa que mira un
// proceso. La dispara un botón del usuario dentro del creador de perfiles.
func (s *Server) observe(ctx context.Context, params json.RawMessage) (json.RawMessage, *Error) {
	p, e := decodeStrict[observeParams](params)
	if e != nil {
		return nil, e
	}
	if p.Executable == "" || p.PID <= 0 {
		return nil, badRequest("hace falta el proceso a mirar")
	}
	árbol := make(map[int]bool, len(p.Tree))
	for _, pid := range p.Tree {
		if pid > 0 {
			árbol[pid] = true
		}
	}
	rangos, err := s.api.ObserveGame(ctx,
		domain.ProcessRef{PID: p.PID, Executable: p.Executable}, árbol, p.KeepSteam)
	if err != nil {
		return nil, &Error{Code: CodeUnavailable, Message: err.Error()}
	}
	return result(rangeViews(rangos))
}

func (s *Server) savedRoom() (json.RawMessage, *Error) {
	room, hay := s.api.SavedRoom()
	if !hay {
		return result(struct {
			Found bool `json:"found"`
		}{false})
	}
	return result(struct {
		Found bool          `json:"found"`
		Room  SavedRoomView `json:"room"`
	}{true, SavedRoomView{
		Code: room.Room.InviteID.String(), Seed: room.Room.Seed, Name: room.Name,
		Game: room.GameID, Subnet: room.Subnet.String(), SavedAt: stamp(room.SavedAt),
	}})
}

func stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// decodeInto interpreta el sobre del mensaje, estricto igual que los
// parámetros.
//
// # Por qué hay una segunda pasada laxa
//
// Para quedarse con el ID aunque el sobre no pase. Sin ella, un campo
// desconocido tumbaba el sobre entero y la respuesta salía con `id` cero, con
// el id perfectamente legible dentro de la línea. Un cliente empareja por id,
// así que esa respuesta no le llega a nadie y quien preguntó se cuelga hasta
// que vence su plazo, que para crear una sala son noventa segundos.
//
// Con el id recuperado, el error vuelve ATRIBUIDO y la UI falla una petición
// en vez de la conexión entera. Es el mismo patrón que el catálogo usa para
// nombrar un perfil que no pudo interpretar.
//
// El id cero se sigue usando para lo que de verdad no se puede atribuir: una
// línea que no es ni JSON, y el mensaje que pasa el tope. Ese caso es terminal
// del lado del cliente, y tiene que serlo: el flujo de bytes perdió el paso.
func decodeInto(linea []byte, req *Request) *Error {
	r, e := decodeStrict[Request](linea)
	if e != nil {
		req.ID = idSuelto(linea)
		return e
	}
	*req = r
	return nil
}

// idSuelto saca solo el id de una línea que el decodificador estricto rechazó.
// Devuelve cero cuando ni eso se puede.
func idSuelto(linea []byte) uint64 {
	var sobre struct {
		ID uint64 `json:"id"`
	}
	if err := json.Unmarshal(linea, &sobre); err != nil {
		return 0
	}
	return sobre.ID
}
