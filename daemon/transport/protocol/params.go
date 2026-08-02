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
}

func foreignViews(rs []domain.ForeignRule) []ForeignView {
	out := make([]ForeignView, 0, len(rs))
	for _, r := range rs {
		v := ForeignView{
			Name:       r.Name,
			Executable: r.Executable,
			WasEnabled: r.WasEnabled,
			Profiles:   make([]string, 0, len(r.Profiles)),
		}
		for _, p := range r.Profiles {
			v.Profiles = append(v.Profiles, p.String())
		}
		out = append(out, v)
	}
	return out
}

// foreignRules vuelve al dominio.
//
// **Los perfiles de firewall se reconstruyen desde la tabla cerrada, jamás
// desde el número que mandó el cliente.** Un valor fuera de los tres se
// descarta en silencio, y una regla que se quede sin perfiles no se suspende:
// lo que entra por el pipe es entrada de fuera, y el peor resultado posible acá
// sería desactivar una regla del usuario que él no eligió.
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
