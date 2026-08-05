package protocol

import (
	"encoding/json"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// Las piezas de cable que necesitan ida y vuelta, o sea las que la UI también
// manda.

// ForeignView es una regla de firewall que Kanpachi no creó.
//
// Viaja en las dos direcciones: sale en la consulta y vuelve en la orden de
// suspender. Vuelve entera y no por id porque el firewall de Windows no tiene
// uno estable: la regla se identifica por su nombre y su ejecutable.
type ForeignView struct {
	Name       string   `json:"name"`
	Executable string   `json:"executable"`
	Profiles   []string `json:"profiles"`
	WasEnabled bool     `json:"was_enabled"`
	// Class es DE QUÉ es la regla, y decide cómo la trata la pantalla: una de
	// más para el juego es una fila de una lista, y una de escritorio remoto
	// entrega la máquina y bloquea abrir la sala.
	//
	// **Sale, jamás entra.** Ver [foreignRules].
	Class string `json:"class"`
	// Blocking es el veredicto del dominio, ya calculado.
	//
	// Viaja hecho en vez de dejar que la UI lo deduzca de Class, y no es
	// redundancia: el día que una segunda clase pase a ser bloqueante, la UI de
	// una versión vieja seguiría dibujando un aviso despachable. La regla del
	// producto se decide en un solo sitio, que es [domain.ForeignRule.Blocking].
	Blocking bool `json:"blocking"`
}

func foreignViews(rs []domain.ForeignRule) []ForeignView {
	out := make([]ForeignView, 0, len(rs))
	for _, r := range rs {
		v := ForeignView{
			Name:       r.Name,
			Executable: r.Executable,
			WasEnabled: r.WasEnabled,
			Class:      ruleClassName(r.Class),
			Blocking:   r.Blocking(),
			Profiles:   make([]string, 0, len(r.Profiles)),
		}
		for _, p := range r.Profiles {
			v.Profiles = append(v.Profiles, p.String())
		}
		out = append(out, v)
	}
	return out
}

// ruleClassName es el nombre de cable de una clase.
//
// Nombres estables y en inglés, como el resto del protocolo, y jamás el
// String() del dominio: aquel es castellano para el log y cambiarlo por una
// palabra mejor rompería la UI en silencio.
//
// Lo recorre entero TestCadaClaseDeReglaTieneNombreEnLaAPI: una clase que
// llegue como "unknown" la pinta la pantalla como si fuera cualquier cosa.
func ruleClassName(c domain.RuleClass) string {
	switch c {
	case domain.ClassGame:
		return "game"
	case domain.ClassRemoteControl:
		return "remote_control"
	case domain.ClassOnOurAdapter:
		return "on_our_adapter"
	case domain.ClassOther:
		return "other"
	default:
		return "unknown"
	}
}

// foreignRules vuelve al dominio.
//
// **Los perfiles de firewall se reconstruyen desde la tabla cerrada, jamás
// desde el número que mandó el cliente.** Un valor fuera de los tres se
// descarta en silencio, y una regla que se quede sin perfiles no se suspende:
// lo que entra por el pipe es entrada de fuera, y el peor resultado posible acá
// sería desactivar una regla del usuario que él no eligió.
//
// **La clase NO se reconstruye, y esa ausencia es deliberada.** Es política del
// dominio, calculada por [domain.ClassifyForeignAgainst] sobre lo que el
// firewall tiene puesto. Aceptarla del cliente dejaría que quien habla por el
// pipe reetiquetara una regla de escritorio remoto como una del juego, que es
// justo la distinción que decide si la sala se puede abrir. Nadie río abajo la
// necesita: suspender empareja por nombre y ejecutable.
func foreignRules(vs []ForeignView) []domain.ForeignRule {
	out := make([]domain.ForeignRule, 0, len(vs))
	for _, v := range vs {
		r := domain.ForeignRule{
			Name:       v.Name,
			Executable: v.Executable,
			WasEnabled: v.WasEnabled,
		}
		for _, p := range v.Profiles {
			if perfil, ok := firewallProfile(p); ok {
				r.Profiles = append(r.Profiles, perfil)
			}
		}
		if len(r.Profiles) == 0 || r.Executable == "" {
			continue
		}
		out = append(out, r)
	}
	return out
}

func firewallProfile(s string) (domain.FirewallProfile, bool) {
	for _, p := range domain.AllFirewallProfiles() {
		if p.String() == s {
			return p, true
		}
	}
	return 0, false
}

// netView es el diagnóstico de red.
func netView(c domain.NetCheck) NetView {
	v := NetView{
		NATKind:      c.NATKind,
		UDPBlocked:   c.UDPBlocked,
		MTU:          c.MTU,
		SubnetReason: c.SubnetReason,
	}
	if c.Subnet.IsValid() {
		v.Subnet = c.Subnet.String()
	}
	if len(c.SeedRTT) > 0 {
		v.SeedRTTMS = make(map[string]int64, len(c.SeedRTT))
		for k, d := range c.SeedRTT {
			v.SeedRTTMS[k] = d.Milliseconds()
		}
	}
	return v
}

// CandidateView es un perfil que llegó en un archivo compartido, con qué le
// pasa. La pantalla de importar necesita las tres cosas: si se puede, si
// colisiona, y con qué.
type CandidateView struct {
	Game      GameView `json:"game"`
	Rejected  bool     `json:"rejected"`
	Reason    string   `json:"reason,omitempty"`
	Collides  bool     `json:"collides"`
	Suggested bool     `json:"suggested"`
	// ExistingVerified dice si el que ya está fue verificado, que es la
	// diferencia entre "ya lo tienes" y "el tuyo está probado y este no".
	ExistingVerified bool `json:"existing_verified"`
}

func candidateViews(cs []domain.ImportCandidate) []CandidateView {
	out := make([]CandidateView, 0, len(cs))
	for _, c := range cs {
		out = append(out, CandidateView{
			Game:             gameView(c.Profile, false),
			Rejected:         c.Rejected,
			Reason:           c.Reason,
			Collides:         c.Collides,
			Suggested:        c.Suggested,
			ExistingVerified: c.Existing.Verified != nil,
		})
	}
	return out
}

// profileParams es un perfil llegando DESDE la UI, o sea entrada de fuera.
//
// No se decodifica a domain.GameProfile a mano: se vuelve a serializar al JSON
// del catálogo y se pasa por [domain.ParseGameProfile], que es el único
// decodificador estricto de perfiles del programa. Eso es lo que hace que un
// perfil que entra por el pipe pase por las MISMAS invariantes que uno que
// entra por un archivo, puertos prohibidos incluidos.
type profileParams struct {
	Profile json.RawMessage `json:"profile"`
	Replace bool            `json:"replace"`
}

// observeParams son los argumentos del creador de perfiles.
type observeParams struct {
	PID        int    `json:"pid"`
	Executable string `json:"executable"`
	// Tree son los PIDs del árbol que arranca en ese ejecutable. Muchos juegos
	// arrancan desde un launcher y el servidor es un proceso hijo.
	Tree []int `json:"tree"`
	// KeepSteam conserva los puertos de Steam, que el asistente normalmente
	// descarta porque no son del juego.
	KeepSteam bool `json:"keep_steam"`
}
