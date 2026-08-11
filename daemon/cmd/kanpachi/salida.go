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
// # Traducir los enums de cable a castellano se hace ACÁ
//
// El protocolo manda nombres estables en inglés a propósito, para que la UI no
// se rompa cuando alguien encuentre una palabra mejor. Convertirlos a lo que se
// lee es trabajo de quien pinta, que es esto.

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
		fmt.Fprintln(w, "  No hay ninguna sala abierta.")
		if st.LastExit != "" {
			fmt.Fprintf(w, "  La última terminó: %s\n", motivoDeSalida(st.LastExit))
		}
		fmt.Fprintln(w, "\n  `kanpachi host` abre una. `kanpachi join <código>` entra en la de otro.")
		return
	}

	fmt.Fprintln(w, raya)
	nombre := st.Name
	if nombre == "" {
		nombre = "(sin nombre)"
	}
	fmt.Fprintf(w, "  %-34s %s, %s\n", nombre, papel(st.Role), estadoDeConexión(st.Conn))
	fmt.Fprintln(w, raya)

	if st.Code != "" {
		fmt.Fprintf(w, "  Código   %s\n", conGuion(st.Code))
	}
	if st.Link != "" {
		fmt.Fprintf(w, "  Enlace   %s\n", st.Link)
	}
	if st.CodeLost {
		// Se dice fuerte porque la sala SIGUE funcionando para los que están
		// dentro: lo que se rompió es que entre alguien nuevo, y nada más en la
		// pantalla lo delataría.
		fmt.Fprintln(w, "  AVISO    el registro ya no conoce este código: no puede entrar nadie nuevo.")
		fmt.Fprintln(w, "           Lo arregla `kanpachi rotate`, que invalida los enlaces repartidos.")
	}
	if st.LocalIP != "" {
		fmt.Fprintf(w, "  Tu IP    %s", st.LocalIP)
		if st.Subnet != "" {
			fmt.Fprintf(w, "  en %s", st.Subnet)
		}
		fmt.Fprintln(w)
	}
	if st.GameName != "" {
		fmt.Fprintf(w, "  Juego    %s\n", st.GameName)
	} else if st.Game != "" {
		fmt.Fprintf(w, "  Juego    %s\n", st.Game)
	}
	if st.MissingGame != "" {
		fmt.Fprintf(w, "  Falta    %s: está activo en la sala y no lo tienes instalado\n", st.MissingGame)
	}

	if st.Role == "guest" && !st.HostPresent {
		fmt.Fprintf(w, "  Host     ausente desde hace %s\n", milis(st.HostGoneForMS))
	}
	if st.ReconnectingForMS > 0 {
		fmt.Fprintf(w, "  Túnel    reconectando desde hace %s\n", milis(st.ReconnectingForMS))
	}

	fmt.Fprintln(w)
	pintarMiembros(w, st)
	pintarCanario(w, st.Canary)

	if len(st.Alerts) > 0 {
		fmt.Fprintln(w, "\n  AVISOS")
		for _, a := range st.Alerts {
			fmt.Fprintf(w, "    %-18s %s\n", nombreDeAlerta(a.Kind), a.Detail)
		}
	}
}

func pintarMiembros(w io.Writer, st protocol.RoomView) {
	if len(st.Peers) == 0 {
		fmt.Fprintln(w, "  No hay nadie en la sala.")
		return
	}
	fmt.Fprintf(w, "  MIEMBROS (%d)\n", len(st.Peers))
	for _, p := range st.Peers {
		marcas := ""
		if p.Host {
			marcas += " [host]"
		}
		if p.Self {
			marcas += " [tú]"
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
	fmt.Fprintf(w, "\n  PROTECCIÓN   %s", veredictoDelCanario(c.Verdict))
	if c.Port != 0 {
		fmt.Fprintf(w, "  (puerto %d)", c.Port)
	}
	fmt.Fprintln(w)
	if c.Touched {
		fmt.Fprintln(w, "    el host vio entrar tráfico por ahí, con su propio socket")
	}
	for _, a := range c.Answers {
		fmt.Fprintf(w, "    %-14s tcp %s, udp %s\n", a.From, resultado(a.TCP), resultado(a.UDP))
	}
}

func pintarJuegos(w io.Writer, juegos []protocol.GameView) {
	if len(juegos) == 0 {
		fmt.Fprintln(w, "  El catálogo está vacío.")
		return
	}
	// Ordenados por nombre: el daemon los devuelve en el orden del catálogo, que
	// no es el que espera quien busca uno con la vista.
	sort.Slice(juegos, func(i, j int) bool {
		return strings.ToLower(juegos[i].Name) < strings.ToLower(juegos[j].Name)
	})
	fmt.Fprintf(w, "  %-24s %-22s %s\n", "ID", "NOMBRE", "")
	for _, g := range juegos {
		marcas := []string{}
		if g.Installed {
			marcas = append(marcas, "instalado")
		}
		if g.Verified {
			marcas = append(marcas, "verificado")
		}
		fmt.Fprintf(w, "  %-24s %-22s %s\n", g.ID, g.Name, strings.Join(marcas, ", "))
	}
	fmt.Fprintln(w, "\n  `kanpachi game <id>` lo activa.")
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
		fmt.Fprintln(w, "  Kanpachi NO pudo leer lo que tiene puesto en el firewall.")
		fmt.Fprintln(w, "  Esto no dice que no haya nada abierto: dice que no se sabe.")
		return
	}
	fmt.Fprintf(w, "  Compuerta: %s\n", estadoDeCompuerta(v.Gate))
	if len(v.Ports) == 0 {
		fmt.Fprintln(w, "  Kanpachi no tiene ningún puerto abierto.")
	}
	for _, p := range v.Ports {
		rango := fmt.Sprintf("%d", p.From)
		if p.To != p.From {
			rango = fmt.Sprintf("%d-%d", p.From, p.To)
		}
		qué := "juego"
		if p.Control {
			qué = "canal de la sala"
		}
		hacia := strings.Join(append(append([]string{}, p.Members...), p.Nets...), ", ")
		if hacia == "" {
			// Vacío JAMÁS significa "para cualquiera": el dominio no puede
			// expresar eso. Se dice así para que nadie lo lea al revés.
			hacia = "nadie"
		}
		estado := "puesto"
		if !p.Applied {
			estado = "PEDIDO Y NO PUESTO"
		}
		fmt.Fprintf(w, "    %-4s %-12s %-18s hacia %s [%s]\n", p.Proto, rango, qué, hacia, estado)
	}
	for _, u := range v.Unexpected {
		fmt.Fprintf(w, "    REGLA QUE NADIE PIDIÓ: %s\n", u)
	}
}

func pintarRed(w io.Writer, v protocol.NetView) {
	if v.NATKind != "" {
		fmt.Fprintf(w, "  NAT      %s\n", v.NATKind)
	}
	fmt.Fprintf(w, "  UDP      %s\n", map[bool]string{true: "bloqueado", false: "pasa"}[v.UDPBlocked])
	if v.MTU > 0 {
		fmt.Fprintf(w, "  MTU      %d\n", v.MTU)
	}
	if v.Subnet != "" {
		fmt.Fprintf(w, "  Subred   %s", v.Subnet)
		if v.SubnetReason != "" {
			fmt.Fprintf(w, "  (%s)", v.SubnetReason)
		}
		fmt.Fprintln(w)
	}
	for seed, rtt := range v.SeedRTTMS {
		fmt.Fprintf(w, "  Registro %s: %d ms\n", seed, rtt)
	}
}

// pintarSondeo enseña la única medición del producto que atraviesa la red de
// verdad, y por eso la que más cuidado pide al leerla.
func pintarSondeo(w io.Writer, v protocol.ProbeView) {
	if !v.Measured {
		fmt.Fprintln(w, "  No se midió nada.")
		return
	}
	fmt.Fprintf(w, "  Sondeado desde %s (%s): %s\n", v.Name, v.Target, veredictoDelSondeo(v.Verdict))
	for _, r := range v.Results {
		fmt.Fprintf(w, "    %-6d %-11s %-24s %s\n", r.Port, claseDeSondeo(r.Kind), r.Label, resultado(r.Outcome))
	}
}

// ─── Los enums de cable, en castellano ───────────────────────────────────────

func estadoDeConexión(s string) string {
	switch s {
	case "idle":
		return "sin sala"
	case "resolving":
		return "resolviendo"
	case "connecting":
		return "conectando"
	case "connected":
		return "conectada"
	case "degraded":
		return "degradada"
	case "reconnecting":
		return "reconectando"
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
		return "eres el host"
	case "guest":
		return "eres invitado"
	default:
		return s
	}
}

func camino(s string) string {
	switch s {
	case "direct":
		return "directo"
	case "relay":
		return "por relay"
	case "self":
		return "tú"
	default:
		return "?"
	}
}

func resultado(s string) string {
	switch s {
	case "answered":
		return "contestó"
	case "refused":
		return "rechazó"
	case "silent":
		return "silencio"
	case "failed":
		return "falló"
	default:
		return s
	}
}

func estadoDeCompuerta(s string) string {
	switch s {
	case "present":
		return "puesta"
	case "absent":
		return "NO ESTÁ PUESTA"
	default:
		return "sin comprobar"
	}
}

func veredictoDelSondeo(s string) string {
	switch s {
	case "leaky":
		// Prueba POSITIVA de exposición: algo que nadie pidió contestó.
		return "FUGA: algo que nadie abrió contestó desde fuera"
	case "unreachable":
		// No prueba nada, y decirlo importa: se ve igual con la máquina blindada
		// que con la máquina apagada.
		return "no contestó nadie, ni el canal de la sala, así que esto no prueba nada"
	case "sealed":
		return "sellada: el canal contesta y nada de lo prohibido lo hace"
	default:
		return "sin medir"
	}
}

func veredictoDelCanario(s string) string {
	switch s {
	case "leaking":
		return "FUGA"
	case "clean":
		return "limpia"
	case "unconfirmed":
		return "sin confirmar"
	case "mismatch":
		return "respuestas que no cuadran"
	default:
		return "sin comprobar"
	}
}

func claseDeSondeo(s string) string {
	switch s {
	case "reference":
		return "referencia"
	case "forbidden":
		return "prohibido"
	case "game":
		return "del juego"
	default:
		return s
	}
}

func motivoDeSalida(s string) string {
	switch s {
	case "user":
		return "la cerraste tú"
	case "kicked":
		return "te expulsaron"
	case "host_gone":
		return "el host se fue"
	case "room_closed":
		return "el host la cerró"
	case "failed":
		return "falló"
	case "tunnel_lost":
		return "se perdió el túnel"
	default:
		return s
	}
}

func nombreDeAlerta(s string) string {
	switch s {
	case "firewall_off":
		return "firewall apagado"
	case "rules_tampered":
		return "reglas tocadas"
	case "router_mapping":
		return "mapeo en el router"
	case "foreign_rule":
		return "regla ajena"
	case "lobby_conflict":
		return "choque de vestíbulo"
	case "kick_incomplete":
		return "expulsión a medias"
	case "audit_failed":
		return "auditoría fallida"
	case "canary_leaking":
		return "fuga detectada"
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
