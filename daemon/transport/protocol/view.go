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
	return v
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
	default:
		// Que este caso exista no lo vuelve aceptable: una alerta que llega como
		// "unknown" no la pinta nadie, así que el módulo de exposición avisa al
		// vacío. Lo impide TestCadaAlertaTieneNombreEnLaAPI, que recorre
		// domain.AllAlertKinds() entera.
		return "unknown"
	}
}
