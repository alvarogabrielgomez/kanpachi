package protocol

import (
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// Las vistas de cable.
//
// # Por qué existen, si el proyecto no usa DTOs entre capas
//
// No son DTOs entre capas: son el FORMATO DE SERIALIZACIÓN de este adaptador,
// y viven donde vive el adaptador. La regla que prohíbe los DTOs habla de
// mapear structs entre anillos que corren en el mismo proceso, que es ceremonia
// sin retorno. Esto cruza una frontera de procesos y de lenguajes, y ahí el
// formato es un contrato con Flutter que tiene que poder no moverse cuando el
// dominio se mueve.
//
// Lo concreto que se gana, y que serializar los tipos del dominio a secas no
// da: los enums viajan como CADENAS y no como el número de un iota. Con el
// número, agregar un estado en medio del bloque de constantes le cambiaría el
// significado a todos los de abajo en una UI ya instalada, y el síntoma sería
// una pantalla que dice "degradado" cuando el daemon dijo "reconectando".
//
// Y las duraciones viajan en milisegundos. Los nanosegundos de Go son un número
// que ningún cliente espera.

// RoomView es el estado de la sala tal como lo ve la UI.
type RoomView struct {
	Conn string `json:"conn"`
	Role string `json:"role"`

	Code string `json:"code,omitempty"`
	Seed string `json:"seed,omitempty"`
	Name string `json:"name,omitempty"`

	Peers []PeerView `json:"peers"`

	HostPresent bool `json:"host_present"`
	// HostGoneForMS es cuánto lleva ausente. Va calculado y no como marca de
	// tiempo para que la UI no tenga que restar contra un reloj que puede no
	// ser el mismo. Cero es que está presente.
	HostGoneForMS int64 `json:"host_gone_for_ms"`
	// ReconnectingForMS es lo mismo para el túnel.
	ReconnectingForMS int64 `json:"reconnecting_for_ms"`

	Game        string `json:"game,omitempty"`
	GameName    string `json:"game_name,omitempty"`
	MissingGame string `json:"missing_game,omitempty"`

	LocalIP string `json:"local_ip,omitempty"`
	Subnet  string `json:"subnet,omitempty"`

	Net    NetView     `json:"net"`
	Alerts []AlertView `json:"alerts"`

	// Canary es la última ronda de la Protección Kanpachi.
	//
	// Va dentro del estado y no detrás de un método propio como el sondeo, y la
	// asimetría tiene motivo: aquel es una medición que el USUARIO pide, y esto
	// lo produjo el daemon solo, igual que las alertas. Nadie va a pulsar un
	// botón para descubrir que su protección se cayó.
	Canary CanaryView `json:"canary"`

	// LastExit es por qué se volvió a la pantalla de inicio. Vacío es que no
	// hay nada que explicar.
	LastExit string `json:"last_exit,omitempty"`
}

type PeerView struct {
	IP    string `json:"ip"`
	Name  string `json:"name"`
	Path  string `json:"path"`
	RTTMS int64  `json:"rtt_ms"`
	Self  bool   `json:"self"`
	Host  bool   `json:"host"`
}

type NetView struct {
	NATKind      string           `json:"nat_kind,omitempty"`
	UDPBlocked   bool             `json:"udp_blocked"`
	MTU          int              `json:"mtu,omitempty"`
	Subnet       string           `json:"subnet,omitempty"`
	SubnetReason string           `json:"subnet_reason,omitempty"`
	SeedRTTMS    map[string]int64 `json:"seed_rtt_ms,omitempty"`
}

type AlertView struct {
	Kind   string `json:"kind"`
	Detail string `json:"detail"`
}

// roomView convierte el estado del dominio a la vista.
//
// Recibe el ahora porque los dos campos de duración se calculan contra él. No
// lee el reloj: quien serializa no decide qué hora es.
func roomView(st domain.RoomState, missing string, now time.Time) RoomView {
	v := RoomView{
		Conn:        connName(st.Conn),
		Role:        roleName(st.Role),
		Name:        st.Name,
		Peers:       make([]PeerView, 0, len(st.Peers)),
		HostPresent: st.HostPresent,
		Game:        st.Game.ID,
		GameName:    st.Game.Name,
		MissingGame: missing,
		Net: NetView{
			NATKind:      st.Net.NATKind,
			UDPBlocked:   st.Net.UDPBlocked,
			MTU:          st.Net.MTU,
			SubnetReason: st.Net.SubnetReason,
		},
		Alerts:   make([]AlertView, 0, len(st.Alerts)),
		LastExit: exitName(st.LastExit),
	}
	if !st.Room.InviteID.IsZero() {
		v.Code = st.Room.InviteID.String()
		v.Seed = st.Room.Seed
	}
	if st.LocalIP.IsValid() {
		v.LocalIP = st.LocalIP.String()
	}
	if st.Subnet.IsValid() {
		v.Subnet = st.Subnet.String()
	}
	if st.Net.Subnet.IsValid() {
		v.Net.Subnet = st.Net.Subnet.String()
	}
	if len(st.Net.SeedRTT) > 0 {
		v.Net.SeedRTTMS = make(map[string]int64, len(st.Net.SeedRTT))
		for k, d := range st.Net.SeedRTT {
			v.Net.SeedRTTMS[k] = d.Milliseconds()
		}
	}
	if !st.HostGoneSince.IsZero() {
		v.HostGoneForMS = now.Sub(st.HostGoneSince).Milliseconds()
	}
	if !st.ReconnectingSince.IsZero() {
		v.ReconnectingForMS = now.Sub(st.ReconnectingSince).Milliseconds()
	}
	for _, p := range st.Peers {
		v.Peers = append(v.Peers, PeerView{
			IP:    p.VirtualIP.String(),
			Name:  p.Name.String(),
			Path:  p.Path.String(),
			RTTMS: p.RTT.Milliseconds(),
			Self:  p.Self,
			Host:  p.Host,
		})
	}
	for _, a := range st.Alerts {
		v.Alerts = append(v.Alerts, AlertView{Kind: alertName(a.Kind), Detail: a.Detail})
	}
	v.Canary = canaryView(st.Canary)
	return v
}

// CanaryView es la última comprobación de la Protección Kanpachi.
//
// # Las dos fuentes viajan por separado, y esa separación es el tipo entero
//
//	Touched   lo vio el HOST, con su propio socket. No se puede falsificar
//	Answers   lo dijeron los miembros. Son mensajes, y un mensaje se puede mentir
//
// La pantalla puede enseñar las dos porque son dos cosas distintas. Juntarlas en
// un booleano tiraría justo lo que hace creíble la frase.
type CanaryView struct {
	Measured bool `json:"measured"`
	// MeasuredAtMS es cuándo, en milisegundos desde la época. Cero si no se
	// midió, igual que en [ExposureView] y [ProbeView].
	MeasuredAtMS int64 `json:"measured_at_ms,omitempty"`
	// Port es el puerto que se abrió, para poder nombrarlo en la pantalla.
	Port uint16 `json:"port,omitempty"`

	// Verdict es "blind", "leaking", "clean", "unconfirmed" o "mismatch".
	Verdict string `json:"verdict"`
	// Touched es el hecho propio del host. Es lo único de acá que se afirma con
	// certeza.
	Touched bool `json:"touched"`

	// Asked son los nombres de TODOS a los que se preguntó, y Answers lo que
	// contestó cada uno. Que se pregunte a todos es lo que impide que un
	// adversario con varias membresías se haga elegir.
	Asked   []string           `json:"asked,omitempty"`
	Answers []CanaryAnswerView `json:"answers"`
}

type CanaryAnswerView struct {
	From string `json:"from"`
	// TCP y UDP son "answered", "refused", "silent" o "failed", igual que en
	// [ProbeResultView].
	TCP string `json:"tcp"`
	UDP string `json:"udp"`
}

func canaryView(c domain.CanaryCheck) CanaryView {
	v := CanaryView{
		Measured: !c.Blind(),
		Verdict:  canaryVerdictName(c.Verdict()),
		Answers:  make([]CanaryAnswerView, 0, len(c.Answers)),
	}
	if c.Blind() {
		// Ni puerto, ni hora, ni preguntados. Por lo mismo que en [exposureView]:
		// este es el formato que cruza a otro proceso, y lo que no se pueda
		// expresar mal, mejor.
		return v
	}
	v.MeasuredAtMS = c.MeasuredAt.UnixMilli()
	v.Port = c.Port
	v.Touched = c.Touched

	for _, a := range c.Asked {
		v.Asked = append(v.Asked, a.Name.String())
	}
	for _, a := range c.Answers {
		v.Answers = append(v.Answers, CanaryAnswerView{
			From: a.Name.String(),
			TCP:  probeOutcomeName(a.TCP),
			UDP:  probeOutcomeName(a.UDP),
		})
	}
	return v
}

func canaryVerdictName(v domain.CanaryVerdict) string {
	switch v {
	case domain.CanaryLeaking:
		return "leaking"
	case domain.CanaryClean:
		return "clean"
	case domain.CanaryUnconfirmed:
		return "unconfirmed"
	case domain.CanaryMismatch:
		return "mismatch"
	case domain.CanaryBlind:
		// Explícito y no metido en el default, aunque devuelvan lo mismo, por lo
		// mismo que en [gateName]: el candado que compara esta función con el
		// enum de Dart corta el fuente en el `default`, así que un valor que solo
		// viva ahí no entra en el contrato.
		return "blind"
	default:
		// Un veredicto nuevo sin nombre acá viaja como sin comprobar, que es el
		// lado seguro. Jamás como "clean": un valor desconocido no puede leerse
		// como que no hay evidencia de fuga.
		return "blind"
	}
}

// ExposureView es lo que Kanpachi tiene abierto, para la pantalla que lo
// enseña.
//
// # El campo que decide cómo se pinta la pantalla entera
//
// `Measured`. En false, la lista de puertos va VACÍA y la pantalla dice que
// Kanpachi no pudo leer lo que tiene puesto, jamás la última lista buena. Un
// booleano explícito y no "la lista está vacía", porque una lista vacía también
// es el estado normal de una sala sin juego activo, y confundir esas dos cosas
// es enseñar tranquilidad donde hay ceguera.
type ExposureView struct {
	Measured bool `json:"measured"`
	// MeasuredAtMS es cuándo se midió, en milisegundos desde la época. Cero si
	// no se midió. Va absoluto y no como "hace tanto" porque el que lo pinta
	// tiene reloj y sabe cuánto tarda en llegar.
	MeasuredAtMS int64 `json:"measured_at_ms,omitempty"`

	Ports []ExposedPortView `json:"ports"`
	// Gate es "present", "absent" o "unknown". Es la segunda fila de la
	// pantalla, y sin ella la lista de arriba es cierta y engañosa a la vez.
	Gate string `json:"gate"`
	// Unexpected son reglas del grupo propio que nadie pidió.
	Unexpected []string `json:"unexpected,omitempty"`
}

type ExposedPortView struct {
	Proto string `json:"proto"`
	From  uint16 `json:"from"`
	To    uint16 `json:"to"`
	// Members y Nets son para quién está abierto. Que vengan vacíos JAMÁS
	// significa cualquiera: el dominio no puede expresar eso.
	Members []string `json:"members,omitempty"`
	Nets    []string `json:"nets,omitempty"`
	// Applied es si el sistema lo tiene puesto AHORA. False significa que
	// Kanpachi lo pidió y no está, o sea que alguien no va a poder entrar.
	Applied bool `json:"applied"`
	// Control distingue el hueco del canal de la sala de un puerto de juego.
	Control bool `json:"control"`
}

func exposureView(r domain.ExposureReport) ExposureView {
	v := ExposureView{
		Measured: !r.Blind(),
		Gate:     gateName(r.Gate),
		Ports:    make([]ExposedPortView, 0, len(r.Ports)),
	}
	if r.Blind() {
		// Ni puertos ni hora. Que el informe ciego no pueda llevar una lista es
		// invariante del dominio, y se repite acá porque este es el formato que
		// cruza a otro proceso: lo que no se pueda expresar mal, mejor.
		return v
	}
	v.MeasuredAtMS = r.MeasuredAt.UnixMilli()
	v.Unexpected = r.Unexpected

	for _, p := range r.Ports {
		pv := ExposedPortView{
			Proto:   p.Proto.String(),
			From:    p.From,
			To:      p.To,
			Applied: p.Applied,
			Control: p.Control,
		}
		for _, m := range p.Members {
			pv.Members = append(pv.Members, m.String())
		}
		for _, n := range p.Nets {
			pv.Nets = append(pv.Nets, n.String())
		}
		v.Ports = append(v.Ports, pv)
	}
	return v
}

func gateName(g domain.GateState) string {
	switch g {
	case domain.GatePresent:
		return "present"
	case domain.GateAbsent:
		return "absent"
	case domain.GateUnknown:
		// Explícito y no metido en el default, aunque devuelvan lo mismo. "Sin
		// comprobar" es un estado de verdad del dominio, y el `default` es el
		// respaldo para lo que no se reconoce: el candado que compara esta
		// función con el enum de Dart corta justo ahí, así que esconderlo abajo
		// haría que la UI no pudiera declararlo sin romper el candado.
		return "unknown"
	default:
		// Un estado nuevo sin nombre acá viaja como sin comprobar, que es el
		// lado seguro: la pantalla dice que no lo sabe en vez de afirmar algo
		// que no midió.
		return "unknown"
	}
}

// ProbeView es el sondeo desde otra máquina, la única medición del producto que
// atraviesa la red de verdad.
//
// # El campo que decide cómo se pinta
//
// `Verdict`, y sus cuatro valores dicen cosas muy distintas:
//
//	leaky        algo que nadie pidió contestó. Prueba POSITIVA de exposición
//	unreachable  ni el canal de la sala contestó, así que esto no prueba nada
//	sealed       el canal contesta y nada de lo prohibido lo hace
//	blind        no se midió
//
// "unreachable" y "sealed" no se pueden confundir, y por eso son valores
// distintos en vez de un booleano: "no contesta nadie" se ve igual con la
// máquina blindada que con la máquina apagada.
type ProbeView struct {
	Measured bool `json:"measured"`
	// Target es la IP virtual a la que se marcó, y Name quién es.
	Target string `json:"target,omitempty"`
	Name   string `json:"name,omitempty"`
	// MeasuredAtMS es cuándo, en milisegundos desde la época. Cero si no se
	// midió, igual que en [ExposureView].
	MeasuredAtMS int64 `json:"measured_at_ms,omitempty"`

	Verdict string            `json:"verdict"`
	Results []ProbeResultView `json:"results"`
}

type ProbeResultView struct {
	Port uint16 `json:"port"`
	// Kind es "reference", "forbidden" o "game": POR QUÉ está en la lista, que
	// es lo mismo que decir qué prueba su respuesta.
	Kind string `json:"kind"`
	// Label es lo que el usuario lee: el nombre de la herramienta, del juego, o
	// del canal de la sala.
	Label string `json:"label"`
	// Outcome es "answered", "refused", "silent" o "failed".
	Outcome string `json:"outcome"`
	// RTTMS solo tiene sentido con "answered". En el silencio lo que mide es el
	// plazo, que ya se sabe de antemano.
	RTTMS int64 `json:"rtt_ms,omitempty"`
}

func probeView(r domain.ProbeReport) ProbeView {
	v := ProbeView{
		Measured: !r.Blind(),
		Verdict:  verdictName(r.Verdict()),
		Results:  make([]ProbeResultView, 0, len(r.Results)),
	}
	if r.Blind() {
		// Ni resultados ni hora ni destino, por lo mismo que en [exposureView]:
		// este es el formato que cruza a otro proceso, y lo que no se pueda
		// expresar mal, mejor.
		return v
	}
	v.Target = r.Target.String()
	v.Name = r.Name.String()
	v.MeasuredAtMS = r.MeasuredAt.UnixMilli()

	for _, res := range r.Results {
		rv := ProbeResultView{
			Port:    res.Port,
			Kind:    probeKindName(res.Kind),
			Label:   res.Label,
			Outcome: probeOutcomeName(res.Outcome),
		}
		if res.Outcome == domain.ProbeAnswered {
			rv.RTTMS = res.RTT.Milliseconds()
		}
		v.Results = append(v.Results, rv)
	}
	return v
}

func verdictName(v domain.ProbeVerdict) string {
	switch v {
	case domain.VerdictLeaky:
		return "leaky"
	case domain.VerdictUnreachable:
		return "unreachable"
	case domain.VerdictSealed:
		return "sealed"
	case domain.VerdictBlind:
		// Explícito y no en el default, por lo mismo que [gateName]: "sin medir"
		// es un estado de verdad y el candado con el enum de Dart corta en el
		// default.
		return "blind"
	default:
		// Un veredicto nuevo sin nombre acá viaja como sin medir, que es el lado
		// seguro: la pantalla no afirma lo que no sabe.
		return "blind"
	}
}

func probeKindName(k domain.ProbeKind) string {
	switch k {
	case domain.ProbeReference:
		return "reference"
	case domain.ProbeForbidden:
		return "forbidden"
	case domain.ProbeGame:
		return "game"
	default:
		// Lo desconocido viaja como prohibido a propósito, que es el lado
		// ruidoso: una clase nueva que se pintara como "juego" haría que una
		// respuesta pasara por normal.
		return "forbidden"
	}
}

func probeOutcomeName(o domain.ProbeOutcome) string {
	switch o {
	case domain.ProbeAnswered:
		return "answered"
	case domain.ProbeRefused:
		return "refused"
	case domain.ProbeSilent:
		return "silent"
	case domain.ProbeFailed:
		return "failed"
	default:
		// Lo desconocido viaja como fallo, no como silencio: un resultado que no
		// se sabe leer no puede contarse como "está cerrado".
		return "failed"
	}
}

// GameView es un perfil del catálogo, para la lista de juegos.
type GameView struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Origin      string      `json:"origin"`
	Verified    bool        `json:"verified"`
	Installed   bool        `json:"installed"`
	HostPorts   []RangeView `json:"host_ports"`
	ClientPorts []RangeView `json:"client_ports"`
	HintKind    string      `json:"hint_kind,omitempty"`
	HintText    string      `json:"hint_text,omitempty"`
}

type RangeView struct {
	Proto string `json:"proto"`
	From  uint16 `json:"from"`
	To    uint16 `json:"to"`
}

func gameView(p domain.GameProfile, instalado bool) GameView {
	v := GameView{
		ID:        p.ID,
		Name:      p.Name,
		Origin:    p.Origin.String(),
		Verified:  p.Verified != nil,
		Installed: instalado,
		HintKind:  p.Connect.Kind.String(),
		HintText:  p.Connect.TextES,
	}
	v.HostPorts = rangeViews(p.HostPorts)
	v.ClientPorts = rangeViews(p.ClientPorts)
	return v
}

func rangeViews(rs []domain.PortRange) []RangeView {
	out := make([]RangeView, 0, len(rs))
	for _, r := range rs {
		out = append(out, RangeView{Proto: r.Proto.String(), From: r.From, To: r.To})
	}
	return out
}

// RejectedView es un perfil que el catálogo rechazó, con su motivo. La UI los
// muestra para que un archivo mal escrito sea arreglable en vez de invisible.
type RejectedView struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
	Origin string `json:"origin"`
}

// PendingView es la sala que quedó abierta en el arranque anterior.
type PendingView struct {
	Code    string `json:"code"`
	Seed    string `json:"seed"`
	Name    string `json:"name"`
	Game    string `json:"game,omitempty"`
	Subnet  string `json:"subnet"`
	SavedAt string `json:"saved_at,omitempty"`
}

// LastRoomView es la última sala a la que se entró como invitado.
type LastRoomView struct {
	Code    string `json:"code"`
	Seed    string `json:"seed"`
	Name    string `json:"name"`
	Nick    string `json:"nick"`
	SavedAt string `json:"saved_at,omitempty"`
}

// Los nombres de los enums. Cadenas y no números, para que agregar una
// constante en medio de un bloque de iota no le cambie el significado a las de
// abajo en una UI que ya está instalada.

func connName(c domain.ConnState) string {
	switch c {
	case domain.StateIdle:
		return "idle"
	case domain.StateResolving:
		return "resolving"
	case domain.StateConnecting:
		return "connecting"
	case domain.StateConnected:
		return "connected"
	case domain.StateDegraded:
		return "degraded"
	case domain.StateReconnecting:
		return "reconnecting"
	default:
		return "unknown"
	}
}

func roleName(r domain.Role) string {
	switch r {
	case domain.RoleHost:
		return "host"
	case domain.RoleGuest:
		return "guest"
	default:
		return ""
	}
}

func exitName(e domain.ExitReason) string {
	switch e {
	case domain.ExitUser:
		return "user"
	case domain.ExitKicked:
		return "kicked"
	case domain.ExitHostGone:
		return "host_gone"
	case domain.ExitRoomClosed:
		return "room_closed"
	case domain.ExitFailed:
		return "failed"
	case domain.ExitTunnelLost:
		return "tunnel_lost"
	default:
		return ""
	}
}

func alertName(k domain.AlertKind) string {
	switch k {
	case domain.AlertFirewallOff:
		return "firewall_off"
	case domain.AlertRulesTampered:
		return "rules_tampered"
	case domain.AlertRouterMapping:
		return "router_mapping"
	case domain.AlertForeignRule:
		return "foreign_rule"
	case domain.AlertLobbyConflict:
		return "lobby_conflict"
	case domain.AlertKickIncomplete:
		return "kick_incomplete"
	case domain.AlertAuditFailed:
		return "audit_failed"
	case domain.AlertCanaryLeaking:
		return "canary_leaking"
	default:
		// Que este caso exista no lo vuelve aceptable: una alerta que llega como
		// "unknown" no la pinta nadie, así que el módulo de exposición avisa al
		// vacío. Lo impide TestCadaAlertaTieneNombreEnLaAPI, que recorre
		// domain.AllAlertKinds() entera.
		return "unknown"
	}
}
