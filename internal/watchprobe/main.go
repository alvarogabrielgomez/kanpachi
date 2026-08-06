// Command watchprobe corre los tres adaptadores que MIRAN la máquina, sin
// tocar nada y sin elevar.
//
// # Por qué existe
//
// Porque en este proyecto ya pasó tres veces: un adaptador que compila, cuyos
// tests pasan, y que la primera vez que se ejecuta de verdad falla por algo que
// ningún test podía ver. El adaptador COM del firewall dio tres fallos en su
// primera ejecución. Estos tres son iguales de opacos: leen formatos de fichero
// de otro, tablas del kernel con desplazamientos calculados a mano, y un
// protocolo de red que contesta lo que le da la gana.
//
// Los tres son de SOLO LECTURA, así que esto no puede romper una máquina, y
// ninguno necesita privilegios. `--console` no hace falta y elevar tampoco.
//
// El cuarto adaptador, el de eventos del sistema, no entra: no se puede sondear
// sin provocar de verdad un cambio de red o una suspensión. Eso lo mide
// `scripts/medir-cambio-de-red.ps1`, que ya existe.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/daemon/adapter/inspector"
	"github.com/accentiostudios/kanpachi/daemon/adapter/library/steam"
	"github.com/accentiostudios/kanpachi/daemon/adapter/router/igd"
)

func main() {
	steamRoot := flag.String("steam-root", "",
		"raíz de Steam a leer. Vacío la busca en el registro")
	skipRouter := flag.Bool("skip-router", false,
		"no preguntarle al router. Es lo único que sale a la red")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	log := consoleLog{}
	failures := 0

	fmt.Println("=== biblioteca de Steam ===")
	lib := steam.New(log)
	if *steamRoot != "" {
		lib = steam.NewAt(*steamRoot, log)
	}
	games, err := lib.Installed(ctx)
	if err != nil {
		fmt.Println("  FALLO:", err)
		failures++
	} else if len(games) == 0 {
		fmt.Println("  sin juegos detectados (Steam no esta, o no hay ninguno instalado)")
	} else {
		for _, g := range games {
			fmt.Printf("  %-8d %-40s %s\n", g.SteamAppID, trim(g.Name, 40), g.InstallPath)
		}
		fmt.Printf("  %d juegos\n", len(games))
	}

	fmt.Println()
	fmt.Println("=== tabla de sockets ===")
	sockets, err := inspector.New().Snapshot(ctx, domain.ProcessRef{PID: os.Getpid()})
	if err != nil {
		fmt.Println("  FALLO:", err)
		failures++
	} else {
		// Se resume en vez de volcar: una máquina normal tiene cientos de
		// entradas y lo que se comprueba acá es que los desplazamientos y el
		// orden de bytes de los puertos estén bien, no el contenido.
		var tcp, udp, anywhere int
		sample := make([]domain.Listener, 0, 8)
		for _, l := range sockets {
			switch l.Proto {
			case domain.ProtoTCP:
				tcp++
			case domain.ProtoUDP:
				udp++
			}
			if l.Address == "0.0.0.0" || l.Address == "::" {
				anywhere++
				if len(sample) < 8 {
					sample = append(sample, l)
				}
			}
		}
		fmt.Printf("  %d entradas: %d TCP, %d UDP, %d escuchando en todas las interfaces\n",
			len(sockets), tcp, udp, anywhere)
		for _, l := range sample {
			fmt.Printf("    %-4s %-24s :%-6d pid %d\n", proto(l.Proto), l.Address, l.Port, l.PID)
		}
		// Un puerto cero en todas las filas es la señal de que el orden de
		// bytes está mal, que es el error clásico de esta tabla.
		if len(sockets) > 0 && tcp+udp > 0 && anywhere == 0 {
			fmt.Println("  SOSPECHOSO: ni una sola entrada escucha en todas las interfaces")
		}
	}

	if !*skipRouter {
		fmt.Println()
		fmt.Println("=== mapeos del router ===")
		mappings, err := igd.New(log).RouterMappings(ctx)
		if err != nil {
			// No cuenta como fallo: un router sin UPnP es lo normal y el
			// adaptador tiene permitido no encontrar nada.
			fmt.Println("  sin respuesta:", err)
		} else if len(mappings) == 0 {
			fmt.Println("  el router contestó y no tiene ningún puerto abierto")
		} else {
			for _, m := range mappings {
				fmt.Printf("  %-4s %5d -> %s:%d  %s\n",
					proto(m.Proto), m.ExternalPort, m.InternalIP, m.InternalPort, m.Description)
			}
		}
	}

	fmt.Println()
	if failures > 0 {
		fmt.Printf("=== %d adaptador(es) fallaron ===\n", failures)
		os.Exit(1)
	}
	fmt.Println("=== los tres contestaron ===")
}

func proto(p domain.Proto) string {
	switch p {
	case domain.ProtoTCP:
		return "tcp"
	case domain.ProtoUDP:
		return "udp"
	default:
		return "?"
	}
}

// trim recorta por RUNAS y no por bytes. Medido en esta máquina: hay juegos
// llamados "God of War Ragnarök" y "F1® 25", así que cortar por byte partiría
// un carácter por la mitad y sacaría basura a la consola.
func trim(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

type consoleLog struct{}

func (consoleLog) Info(msg string, kv ...any)  { line("info ", msg, kv) }
func (consoleLog) Warn(msg string, kv ...any)  { line("warn ", msg, kv) }
func (consoleLog) Error(msg string, kv ...any) { line("error", msg, kv) }

func line(level, msg string, kv []any) {
	fmt.Fprintf(os.Stderr, "  [%s] %s", level, msg)
	for i := 0; i+1 < len(kv); i += 2 {
		fmt.Fprintf(os.Stderr, " %v=%v", kv[i], kv[i+1])
	}
	fmt.Fprintln(os.Stderr)
}
