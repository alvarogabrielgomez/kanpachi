package main

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// eventosEnPantalla es cuántas líneas del log se pintan debajo de la sala.
//
// Doce: las que caben sin empujar la lista de miembros fuera de una consola de
// las de siempre. El resto está en el fichero, que es donde se lee de verdad.
const eventosEnPantalla = 12

const raya = "=================================================================="

// pintarSala dibuja el estado entero. Es formateo PURO sobre un `io.Writer`:
// no toca la sesión, no toca el sistema y no tiene reloj propio.
//
// Que sea puro no es estética. Este fichero es portable, así que el job de
// Linux del CI lo compila y lo veta, y es la mitad de esta herramienta que se
// puede revisar sin una máquina Windows delante.
func pintarSala(w io.Writer, st domain.RoomState, prog domain.Progress,
	ev []linea, nick string, enlace string, ahora time.Time) {

	fmt.Fprintln(w, raya)
	fmt.Fprintf(w, "  SALA KANPACHI            Yo: %-20s %s\n", nick, ahora.Format("15:04:05"))
	fmt.Fprintln(w, raya)

	rol := "INVITADO"
	if st.Role == domain.RoleHost {
		rol = "HOST"
	}
	fmt.Fprintf(w, "  Rol       %-12s Estado  %s\n", rol, st.Conn.String())
	fmt.Fprintf(w, "  Codigo    %-12s Subred  %s\n", st.Room.InviteID.String(), st.Subnet.String())
	fmt.Fprintf(w, "  Mi IP     %-12s Seed    %s\n", st.LocalIP.String(), st.Room.Seed)
	if enlace != "" {
		fmt.Fprintf(w, "  Enlace    %s\n", enlace)
	}
	if st.CodeLost {
		fmt.Fprintln(w, "  AVISO: el registro ya no conoce este codigo. Nadie nuevo puede entrar")
	}
	fmt.Fprintln(w)

	pintarMiembros(w, st)
	pintarPlazos(w, st, ahora)
	pintarCanario(w, st.Canary, ahora)
	pintarAlertas(w, st)
	pintarProgreso(w, prog)
	pintarEventos(w, ev)
}

func pintarMiembros(w io.Writer, st domain.RoomState) {
	fmt.Fprintf(w, "  --- Miembros (%d) ---\n", len(st.Peers))
	if len(st.Peers) == 0 {
		fmt.Fprintln(w, "    (el motor no ve a nadie mas todavia)")
	}
	for _, p := range st.Peers {
		etiqueta := ""
		if p.Self {
			etiqueta += " (tu)"
		}
		if p.Host {
			etiqueta += " [HOST]"
		}
		extra := p.Path.String()
		if p.RTT > 0 {
			extra += ", " + p.RTT.Round(time.Millisecond).String()
		}
		fmt.Fprintf(w, "    %-16s %-16s %s\n",
			p.Name.String()+etiqueta, p.VirtualIP.String(), extra)
	}
	fmt.Fprintln(w)
}

// pintarPlazos enseña los relojes que están corriendo.
//
// # Por qué esto es la mitad de la herramienta
//
// El escenario que hay que poder probar es apagar el wifi del host y ver cómo
// se entera el invitado. Sin esta sección, esa prueba son veinte minutos
// mirando una pantalla que no cambia y un final que parece una desconexión
// inexplicable. Con ella se ve venir: la presencia cae, el silencio de seis
// minutos empieza a contar, y la ausencia de veinte tiene cuenta atrás.
//
// Solo tiene sentido en un invitado: un host no puede estar ausente de su
// propia sala.
func pintarPlazos(w io.Writer, st domain.RoomState, ahora time.Time) {
	if !st.Conn.InRoom() {
		return
	}
	fmt.Fprintln(w, "  --- Plazos ---")

	if st.Role == domain.RoleGuest {
		if st.HostPresent {
			fmt.Fprintf(w, "    Host          PRESENTE     ultima senal %s\n",
				hace(st.HostLastHeard, ahora))
		} else {
			fmt.Fprintf(w, "    Host          AUSENTE      desde %s\n",
				hace(st.HostGoneSince, ahora))
		}
		fmt.Fprintf(w, "    Silencio 6m   %s\n",
			cuentaAtras(st.HostLastHeard, domain.HostSilenceLimit, ahora))
		fmt.Fprintf(w, "    Ausencia 20m  %s\n",
			cuentaAtras(st.HostGoneSince, domain.HostAbsenceLimit, ahora))
	}
	fmt.Fprintf(w, "    Sin tunel 10m %s\n",
		cuentaAtras(st.ReconnectingSince, domain.ReconnectLimit, ahora))
	fmt.Fprintln(w)
}

func pintarCanario(w io.Writer, c domain.CanaryCheck, ahora time.Time) {
	if c.Blind() {
		return
	}
	fmt.Fprintln(w, "  --- Canario ---")
	fmt.Fprintf(w, "    %s   puerto %d   medido %s   %d de %d contestaron\n",
		c.Verdict().String(), c.Port, hace(c.MeasuredAt, ahora), len(c.Answers), len(c.Asked))
	fmt.Fprintln(w)
}

func pintarAlertas(w io.Writer, st domain.RoomState) {
	if len(st.Alerts) == 0 {
		return
	}
	fmt.Fprintln(w, "  --- Alertas ---")
	for _, a := range st.Alerts {
		fmt.Fprintf(w, "    %s\n", a.Detail)
	}
	fmt.Fprintln(w)
}

// pintarProgreso enseña la operación larga en curso.
//
// Es lo que hace mirable el minuto que `CreateRoom` sostiene el candado de la
// sesión: `Progress()` tiene candado propio justo para poder leerse ahí dentro.
func pintarProgreso(w io.Writer, p domain.Progress) {
	if !p.Running {
		return
	}
	fmt.Fprintf(w, "  --- %s (%s) ---\n", p.Op, p.Elapsed.Round(time.Second))
	if n := len(p.Steps); n > 0 {
		s := p.Steps[n-1]
		fmt.Fprintf(w, "    %s: %s\n", s.Scope, s.Text)
	}
	fmt.Fprintln(w)
}

func pintarEventos(w io.Writer, ev []linea) {
	fmt.Fprintln(w, "  --- Ultimos eventos ---")
	if len(ev) == 0 {
		fmt.Fprintln(w, "    (nada todavia)")
	}
	for _, l := range ev {
		txt := l.Msg
		if len(l.KV) > 0 {
			txt += fmt.Sprintf(" %v", l.KV)
		}
		fmt.Fprintf(w, "    %s %s %s\n", l.At.Format("15:04:05"), l.Nivel, recortar(txt, 92))
	}
	fmt.Fprintln(w, raya)
}

// hace pinta cuánto pasó desde un instante. El cero es "nunca", que es distinto
// de "hace un momento": confundirlos manda a mirar el reloj del sistema.
func hace(t, ahora time.Time) string {
	if t.IsZero() {
		return "nunca"
	}
	return "hace " + ahora.Sub(t).Round(time.Second).String()
}

// cuentaAtras dice lo que falta para que venza un plazo.
//
// Un plazo que no arrancó se pinta con dos guiones y no con el límite entero:
// un "20m0s" en una fila que no está contando se lee como que empezó la cuenta.
func cuentaAtras(desde time.Time, limite time.Duration, ahora time.Time) string {
	if desde.IsZero() {
		return "--"
	}
	queda := limite - ahora.Sub(desde)
	if queda <= 0 {
		return "VENCIDO"
	}
	return "vence en " + queda.Round(time.Second).String()
}

func recortar(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
