package main

import (
	"fmt"
	"io"
	"runtime"
	"time"

	"github.com/accentiostudios/kanpachi/daemon/transport/pipe"
)

// ayuda escribe la lista de comandos.
//
// A mano y en orden, no recorriendo el mapa: un mapa de Go no tiene orden, así
// que la ayuda saldría barajada distinta en cada ejecución. Y el orden no es
// alfabético a propósito, va por lo que se hace primero.
func ayuda(w io.Writer) {
	fmt.Fprintln(w, "kanpachi — the Kanpachi client.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "  With no arguments it opens the wizard, driven with arrow keys.")
	fmt.Fprintln(w)
	for _, g := range grupos {
		fmt.Fprintf(w, "%s\n", g.titulo)
		for _, n := range g.nombres {
			c := comandos[n]
			izquierda := n
			if c.args != "" {
				izquierda += " " + c.args
			}
			fmt.Fprintf(w, "  %-26s %s\n", izquierda, c.breve)
		}
		fmt.Fprintln(w)
	}
	fmt.Fprintln(w, "Options, valid in any position:")
	fmt.Fprintln(w, "  --nick <name>              how the room sees you. Remembered")
	fmt.Fprintln(w, "  --json                     the daemon's raw answer, unrendered")
	fmt.Fprintln(w, "  --data <dir>               a different data directory")
	fmt.Fprintln(w, "  --pipe <path>              a different control channel")
	fmt.Fprintln(w, "  --timeout <duration>       how long to wait for an answer (90s default)")
}

// pistaDeConexión dice qué mirar cuando el canal no abre.
//
// # Por qué hace falta decir dos cosas y no una
//
// Porque los dos fallos frecuentes se ven IGUAL desde acá: un socket que no abre
// es lo mismo con el servicio parado que con el servicio corriendo y sin `sudo`.
// Nombrar solo uno manda a la mitad de la gente a mirar donde no está.
//
// # Por qué esto sí mira `runtime.GOOS` y el resto del repositorio no
//
// Porque acá lo que cambia es la PROSA, no lo que hace el programa. La regla de
// partir por fichero existe para que un sistema sin cableado sea un error de
// enlazado en vez de un `default` silencioso que aplica la decisión de otro; un
// texto de ayuda que salga genérico en un sistema nuevo no rompe nada, y
// partirlo costaría tres ficheros para tres frases.
func pistaDeConexión(opciones) string {
	switch runtime.GOOS {
	case "linux":
		return "  Check both, they look the same from here:\n" +
			"    systemctl status kanpachid       is it running?\n" +
			"    sudo kanpachi ...                the channel and the token belong to root"
	case "windows":
		return fmt.Sprintf("  Is the Kanpachi service running?\n"+
			"    sc query kanpachi-daemon\n"+
			"  In console mode the channel is another one: --pipe %s", pipe.ConsoleName)
	default:
		return "  The local channel is written for Windows and for Linux."
	}
}

// tiempo lee una duración de las que acepta Go: `30s`, `2m`, `1h30m`.
func tiempo(s string) (time.Duration, error) {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, uso("--timeout %q is not a duration: write it 30s, 2m, 1h30m", s)
	}
	if d <= 0 {
		return 0, uso("--timeout has to be positive, got %s", d)
	}
	return d, nil
}
