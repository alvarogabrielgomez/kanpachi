package main

// Cómo se pinta lo que contesta el daemon.
//
// # Sin color, y no es pereza
//
// La salida de esto va a acabar en un `ssh`, en un `tmux`, en un fichero por
// `> salida.txt` y pegada en un chat cuando alguien pida ayuda. Los códigos de
// color sobreviven a los cuatro sitios y solo se ven bien en el primero, así que
// lo que aportarían de más se lo cobran en el resto. Lo único que se escribe
// fuera de texto plano es el borrado de pantalla de `watch`, que sin él no es
// una pantalla.
//
// # Los enums de cable se traducen ACÁ
//
// El protocolo manda nombres estables en inglés a propósito, para que la UI no se
// rompa cuando alguien encuentre una palabra mejor. Convertirlos a lo que se lee
// es trabajo de quien pinta, que es esto. Que acá el destino sea también inglés
// no vuelve inútil la traducción: `unreachable` es un valor del protocolo y "no
// one answered, so this proves nothing" es una frase para una persona.

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/accentiostudios/kanpachi/daemon/transport/protocol"
)

// limpiarPantalla deja el cursor arriba y borra lo de abajo.
//
// Se borra hacia abajo (`ESC[J`) en vez de borrar todo y saltar: así el
// redibujado no parpadea, porque la pantalla nueva se escribe encima de la vieja
// en vez de sobre un hueco negro.
func limpiarPantalla(w io.Writer) { fmt.Fprint(w, "\033[H\033[J") }

const raya = "  ────────────────────────────────────────────────────────────────"

// pintarSala es la pantalla principal.
func pintarSala(w io.Writer, st protocol.RoomView) {
	if st.Conn == "idle" || st.Conn == "" {
		fmt.Fprintln(w, "  No room is open.")
		if st.LastExit != "" {
			fmt.Fprintf(w, "  The last one ended: %s\n", motivoDeSalida(st.LastExit))
		}
		fmt.Fprintln(w, "\n  `kanpachi host` opens one. `kanpachi join <code>` enters someone else's.")
		return
	}

	fmt.Fprintln(w, raya)
	nombre := st.Name
	if nombre == "" {
		nombre = "(unnamed)"
	}
	fmt.Fprintf(w, "  %-34s %s, %s\n", nombre, papel(st.Role), estadoDeConexión(st.Conn))
	fmt.Fprintln(w, raya)

	if st.Code != "" {
		fmt.Fprintf(w, "  Code     %s\n", conGuion(st.Code))
	}
	if st.Link != "" {
		fmt.Fprintf(w, "  Link     %s\n", st.Link)
	}
	if st.CodeLost {
		// Se dice fuerte porque la sala SIGUE funcionando para los que están
		// dentro: lo que se rompió es que entre alguien nuevo, y nada más en la
		// pantalla lo delataría.
		fmt.Fprintln(w, "  WARNING  the registry no longer knows this code: nobody new can join.")
		fmt.Fprintln(w, "           `kanpachi rotate` fixes it, and voids the links you handed out.")
	}
	if st.LocalIP != "" {
		fmt.Fprintf(w, "  Your IP  %s", st.LocalIP)
		if st.Subnet != "" {
			fmt.Fprintf(w, "  on %s", st.Subnet)
		}
		fmt.Fprintln(w)
	}
	if st.GameName != "" {
		fmt.Fprintf(w, "  Game     %s\n", st.GameName)
	} else if st.Game != "" {
		fmt.Fprintf(w, "  Game     %s\n", st.Game)
	}
	if st.MissingGame != "" {
		fmt.Fprintf(w, "  Missing  %s: it is active in the room and you do not have it installed\n",
			st.MissingGame)
	}

	if st.Role == "guest" && !st.HostPresent {
		fmt.Fprintf(w, "  Host     gone for %s\n", milis(st.HostGoneForMS))
	}
	if st.ReconnectingForMS > 0 {
		fmt.Fprintf(w, "  Tunnel   reconnecting for %s\n", milis(st.ReconnectingForMS))
	}

	fmt.Fprintln(w)
	pintarMiembros(w, st)
	pintarCanario(w, st.Canary)

	if len(st.Alerts) > 0 {
		fmt.Fprintln(w, "\n  ALERTS")
		for _, a := range st.Alerts {
			fmt.Fprintf(w, "    %-20s %s\n", nombreDeAlerta(a.Kind), a.Detail)
		}
	}
}

func pintarMiembros(w io.Writer, st protocol.RoomView) {
	if len(st.Peers) == 0 {
		fmt.Fprintln(w, "  Nobody is in the room.")
		return
	}
	fmt.Fprintf(w, "  MEMBERS (%d)\n", len(st.Peers))
	for _, p := range st.Peers {
		marcas := ""
		if p.Host {
			marcas += " [host]"
		}
		if p.Self {
			marcas += " [you]"
		}
		latencia := "-"
		if p.RTTMS > 0 {
			latencia = fmt.Sprintf("%d ms", p.RTTMS)
		}
		fmt.Fprintf(w, "    %-14s %-16s %-8s %s%s\n",
			p.Name, p.IP, camino(p.Path), latencia, marcas)
	}
}

// pintarCanario enseña la última ronda de la Protección Kanpachi.
//
// Las dos fuentes se enseñan por separado porque son dos cosas distintas: lo que
// vio el host con su propio socket no se puede falsificar, y lo que dijeron los
// miembros son mensajes, y un mensaje se puede mentir. Juntarlas en una frase
// tiraría justo lo que la hace creíble.
func pintarCanario(w io.Writer, c protocol.CanaryView) {
	if !c.Measured {
		return
	}
	fmt.Fprintf(w, "\n  PROTECTION   %s", veredictoDelCanario(c.Verdict))
	if c.Port != 0 {
		fmt.Fprintf(w, "  (port %d)", c.Port)
	}
	fmt.Fprintln(w)
	if c.Touched {
		fmt.Fprintln(w, "    the host saw traffic come in there, with its own socket")
	}
	for _, a := range c.Answers {
		fmt.Fprintf(w, "    %-14s tcp %s, udp %s\n", a.From, resultado(a.TCP), resultado(a.UDP))
	}
}

func pintarJuegos(w io.Writer, juegos []protocol.GameView) {
	if len(juegos) == 0 {
		fmt.Fprintln(w, "  The catalog is empty.")
		return
	}
	// Ordenados por nombre: el daemon los devuelve en el orden del catálogo, que
	// no es el que espera quien busca uno con la vista.
	sort.Slice(juegos, func(i, j int) bool {
		return strings.ToLower(juegos[i].Name) < strings.ToLower(juegos[j].Name)
	})
	fmt.Fprintf(w, "  %-24s %-34s %s\n", "ID", "NAME", "")
	for _, g := range juegos {
		marcas := []string{}
		if g.Installed {
			marcas = append(marcas, "installed")
		}
		if g.Verified {
			marcas = append(marcas, "verified")
		}
		fmt.Fprintf(w, "  %-24s %-34s %s\n", g.ID, g.Name, strings.Join(marcas, ", "))
	}
	fmt.Fprintln(w, "\n  `kanpachi game <id>` activates one.")
}

// pintarExposicion enseña qué está abierto.
//
// # Por qué lo primero es si se pudo mirar
//
// Porque una lista vacía significa dos cosas muy distintas —no hay nada abierto,
// o Kanpachi no pudo leer lo que tiene puesto— y confundirlas es enseñar
// tranquilidad donde hay ceguera. El protocolo trae un booleano explícito para
// eso y acá se respeta.
func pintarExposicion(w io.Writer, v protocol.ExposureView) {
	if !v.Measured {
		fmt.Fprintln(w, "  Kanpachi could NOT read what it has in the firewall.")
		fmt.Fprintln(w, "  This does not say nothing is open: it says nobody knows.")
		return
	}
	fmt.Fprintf(w, "  Gate: %s\n", estadoDeCompuerta(v.Gate))
	if len(v.Ports) == 0 {
		fmt.Fprintln(w, "  Kanpachi has no port open.")
	}
	for _, p := range v.Ports {
		rango := fmt.Sprintf("%d", p.From)
		if p.To != p.From {
			rango = fmt.Sprintf("%d-%d", p.From, p.To)
		}
		qué := "game"
		if p.Control {
			qué = "room channel"
		}
		hacia := strings.Join(append(append([]string{}, p.Members...), p.Nets...), ", ")
		if hacia == "" {
			// Vacío JAMÁS significa "para cualquiera": el dominio no puede
			// expresar eso. Se dice así para que nadie lo lea al revés.
			hacia = "nobody"
		}
		estado := "applied"
		if !p.Applied {
			estado = "ASKED FOR AND NOT APPLIED"
		}
		fmt.Fprintf(w, "    %-4s %-12s %-14s toward %s [%s]\n", p.Proto, rango, qué, hacia, estado)
	}
	for _, u := range v.Unexpected {
		fmt.Fprintf(w, "    RULE NOBODY ASKED FOR: %s\n", u)
	}
}

func pintarRed(w io.Writer, v protocol.NetView) {
	if v.NATKind != "" {
		fmt.Fprintf(w, "  NAT      %s\n", v.NATKind)
	}
	fmt.Fprintf(w, "  UDP      %s\n", map[bool]string{true: "blocked", false: "gets through"}[v.UDPBlocked])
	if v.MTU > 0 {
		fmt.Fprintf(w, "  MTU      %d\n", v.MTU)
	}
	if v.Subnet != "" {
		fmt.Fprintf(w, "  Subnet   %s", v.Subnet)
		if v.SubnetReason != "" {
			fmt.Fprintf(w, "  (%s)", v.SubnetReason)
		}
		fmt.Fprintln(w)
	}
	for seed, rtt := range v.SeedRTTMS {
		fmt.Fprintf(w, "  Registry %s: %d ms\n", seed, rtt)
	}
}

// pintarSondeo enseña la única medición del producto que atraviesa la red de
// verdad, y por eso la que más cuidado pide al leerla.
func pintarSondeo(w io.Writer, v protocol.ProbeView) {
	if !v.Measured {
		fmt.Fprintln(w, "  Nothing was measured.")
		return
	}
	fmt.Fprintf(w, "  Probed from %s (%s): %s\n", v.Name, v.Target, veredictoDelSondeo(v.Verdict))
	for _, r := range v.Results {
		fmt.Fprintf(w, "    %-6d %-11s %-24s %s\n",
			r.Port, claseDeSondeo(r.Kind), r.Label, resultado(r.Outcome))
	}
}

// ─── Los enums de cable, para leerlos ────────────────────────────────────────

func estadoDeConexión(s string) string {
	switch s {
	case "idle":
		return "no room"
	case "resolving":
		return "resolving"
	case "connecting":
		return "connecting"
	case "connected":
		return "connected"
	case "degraded":
		return "degraded"
	case "reconnecting":
		return "reconnecting"
	default:
		// Lo que no se reconoce se enseña TAL CUAL en vez de traducirse a algo
		// tranquilizador. Un cliente viejo hablando con un daemon nuevo tiene que
		// enseñar la palabra que no entiende, no inventarse otra.
		return s
	}
}

func papel(s string) string {
	switch s {
	case "host":
		return "you are the host"
	case "guest":
		return "you are a guest"
	default:
		return s
	}
}

func camino(s string) string {
	switch s {
	case "direct":
		return "direct"
	case "relay":
		return "relayed"
	case "self":
		return "you"
	default:
		return "?"
	}
}

func resultado(s string) string {
	switch s {
	case "answered":
		return "answered"
	case "refused":
		return "refused"
	case "silent":
		return "silent"
	case "failed":
		return "failed"
	default:
		return s
	}
}

func estadoDeCompuerta(s string) string {
	switch s {
	case "present":
		return "up"
	case "absent":
		return "NOT UP"
	default:
		return "unchecked"
	}
}

func veredictoDelSondeo(s string) string {
	switch s {
	case "leaky":
		// Prueba POSITIVA de exposición: algo que nadie pidió contestó.
		return "LEAK: something nobody opened answered from outside"
	case "unreachable":
		// No prueba nada, y decirlo importa: se ve igual con la máquina blindada
		// que con la máquina apagada.
		return "nobody answered, not even the room channel, so this proves nothing"
	case "sealed":
		return "sealed: the channel answers and nothing forbidden does"
	default:
		return "not measured"
	}
}

func veredictoDelCanario(s string) string {
	switch s {
	case "leaking":
		return "LEAKING"
	case "clean":
		return "clean"
	case "unconfirmed":
		return "unconfirmed"
	case "mismatch":
		return "answers that do not agree"
	default:
		return "unchecked"
	}
}

func claseDeSondeo(s string) string {
	switch s {
	case "reference":
		return "reference"
	case "forbidden":
		return "forbidden"
	case "game":
		return "of the game"
	default:
		return s
	}
}

func motivoDeSalida(s string) string {
	switch s {
	case "user":
		return "you closed it"
	case "kicked":
		return "you were kicked"
	case "host_gone":
		return "the host left"
	case "room_closed":
		return "the host closed it"
	case "failed":
		return "it failed"
	case "tunnel_lost":
		return "the tunnel was lost"
	default:
		return s
	}
}

func nombreDeAlerta(s string) string {
	switch s {
	case "firewall_off":
		return "firewall off"
	case "rules_tampered":
		return "rules tampered with"
	case "router_mapping":
		return "mapping on the router"
	case "foreign_rule":
		return "foreign rule"
	case "lobby_conflict":
		return "lobby clash"
	case "kick_incomplete":
		return "partial kick"
	case "audit_failed":
		return "audit failed"
	case "canary_leaking":
		return "leak detected"
	default:
		return s
	}
}

// ─── Formatos ────────────────────────────────────────────────────────────────

// conGuion parte el código en dos mitades, que es como se lee en voz alta y como
// lo enseña la página de invitación.
func conGuion(code string) string {
	if len(code) != 8 {
		return code
	}
	return code[:4] + "-" + code[4:]
}

func milis(ms int64) string {
	switch {
	case ms < 1000:
		return fmt.Sprintf("%d ms", ms)
	case ms < 60_000:
		return fmt.Sprintf("%d s", ms/1000)
	default:
		return fmt.Sprintf("%d min", ms/60_000)
	}
}
