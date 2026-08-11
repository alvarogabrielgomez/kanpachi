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
	fmt.Fprintln(w, "kanpachi — el cliente de Kanpachi.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "  Sin argumentos abre el asistente, con flechas.")
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
	fmt.Fprintln(w, "Opciones, válidas en cualquier posición:")
	fmt.Fprintln(w, "  --nick <nombre>            cómo te ven en la sala. Se recuerda")
	fmt.Fprintln(w, "  --json                     la respuesta cruda del daemon, sin pintar")
	fmt.Fprintln(w, "  --data <dir>               otro directorio de datos")
	fmt.Fprintln(w, "  --pipe <ruta>              otro canal de control")
	fmt.Fprintln(w, "  --timeout <duración>       cuánto esperar una respuesta (por omisión 90s)")
}

// pistaDeConexión dice qué mirar cuando el canal no abre.
//
// # Por qué hace falta decir dos cosas y no una
//
// Porque los dos fallos frecuentes se ven IGUAL desde acá: un socket que no
// abre es lo mismo con el servicio parado que con el servicio corriendo y sin
// `sudo`. Nombrar solo uno manda a la mitad de la gente a mirar donde no está.
//
// # Por qué esto sí mira `runtime.GOOS` y el resto del repositorio no
//
// Porque acá lo que cambia es la PROSA, no lo que hace el programa. La regla de
// partir por fichero existe para que un sistema sin cableado sea un error de
// enlazado en vez de un `default` silencioso que aplica la decisión de otro; un
// texto de ayuda que salga genérico en un sistema nuevo no rompe nada, y
// partirlo costaría tres ficheros para tres frases.
func pistaDeConexión(op opciones) string {
	switch runtime.GOOS {
	case "linux":
		return "  Mira las dos cosas, que desde acá se ven igual:\n" +
			"    systemctl status kanpachid       ¿está corriendo?\n" +
			"    sudo kanpachi ...                el canal y el token son de root"
	case "windows":
		return fmt.Sprintf("  ¿Está corriendo el servicio de Kanpachi?\n"+
			"    sc query kanpachi-daemon\n"+
			"  En modo consola el canal es otro: --pipe %s", pipe.ConsoleName)
	default:
		return "  El canal local está escrito para Windows y para Linux."
	}
}

// tiempo lee una duración de las que acepta Go: `30s`, `2m`, `1h30m`.
func tiempo(s string) (time.Duration, error) {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, uso("--timeout %q no es una duración: se escribe 30s, 2m, 1h30m", s)
	}
	if d <= 0 {
		return 0, uso("--timeout tiene que ser positivo, llegó %s", d)
	}
	return d, nil
}
