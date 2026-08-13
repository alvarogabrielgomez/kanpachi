// Command netcfgprobe ejercita los caminos de netcfg que una sala normal no toca.
//
// # Para qué existe
//
// Al contrastar lo escrito contra lo que de verdad se había ejecutado apareció
// una brecha incómoda: crear una sala prueba la métrica, el MTU y el barrido de
// rutas, y **no toca nada más**. Las rutas de broadcast y multicast solo las
// pide un perfil de juego, la política de prefijo también, y el borrado de una
// ruta por defecto solo corre cuando hay una que borrar, que en una máquina sana
// no pasa nunca.
//
// Código que nunca se ejecutó no está probado, por muy revisado que esté. Este
// arnés lo ejecuta, y ya encontró un fallo: la fila de una ruta se construía sin
// pasar por `InitializeIpForwardEntry`, así que sus tiempos de vida quedaban en
// cero y la ruta nacía caducada. La llamada devolvía éxito.
//
// # Qué hace, y por qué es seguro
//
// Trabaja SOLO sobre el adaptador virtual, que existe únicamente mientras hay
// una sala abierta. La ruta por defecto de mentira se crea con métrica altísima
// a propósito, para que no le gane a la de verdad ni un instante, y lo que se
// comprueba es que netcfg la quita.
//
// Necesita consola elevada y una sala abierta.
package main

import (
	"context"
	"flag"
	"fmt"
	"net/netip"
	"os"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/daemon/adapter/netcfg"
)

func main() {
	dataDirectory := flag.String("data", `C:\ProgramData\Kanpachi`, "directorio de datos, donde vive el libro de ajustes")
	flag.Parse()

	if err := run(*dataDirectory); err != nil {
		fmt.Fprintln(os.Stderr, "netcfgprobe:", err)
		os.Exit(1)
	}
}

func run(datos string) error {
	cfg := netcfg.New(datos, loggerConsole{})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	ip, subred, err := roomAdapter()
	if err != nil {
		return err
	}
	fmt.Printf("sala encontrada en %s: %v dentro de %v\n\n", domain.AdapterName, ip, subred)

	base := domain.AdapterState{
		Address:    ip,
		Subnet:     subred,
		MetricIPv4: domain.MetricIPv4,
		MetricIPv6: domain.MetricIPv6,
		MTU:        1500,
	}

	failureCount := 0
	mal := func(f string, a ...any) { failureCount++; fmt.Printf("  MAL %s\n", fmt.Sprintf(f, a...)) }
	logSuccess := func(f string, a ...any) { fmt.Printf("  OK  %s\n", fmt.Sprintf(f, a...)) }

	// 1. Las rutas que pide un perfil. Se ponen y se quitan en pasos separados:
	// un estado suelto no prueba nada, lo que se mide es la transición.
	fmt.Println("=== rutas de broadcast y multicast, poniéndolas ===")
	withRoutes := base
	withRoutes.BroadcastRoute, withRoutes.MulticastRoute = true, true
	if err := cfg.ApplyAdapter(ctx, withRoutes); err != nil {
		mal("ApplyAdapter con rutas: %v", err)
	}
	for _, p := range []netip.Prefix{netcfg.BroadcastRoute, netcfg.MulticastRoute} {
		if routeExists(p) {
			logSuccess("%v puesta", p)
		} else {
			mal("%v NO se puso, y la llamada dijo que sí", p)
		}
	}

	fmt.Println("\n=== las mismas rutas, quitándolas ===")
	if err := cfg.ApplyAdapter(ctx, base); err != nil {
		mal("ApplyAdapter sin rutas: %v", err)
	}
	for _, p := range []netip.Prefix{netcfg.BroadcastRoute, netcfg.MulticastRoute} {
		if routeExists(p) {
			mal("%v sigue puesta después de dejar de pedirla", p)
		} else {
			logSuccess("%v quitada", p)
		}
	}

	// 2. El borrado de la ruta por defecto. Es la invariante más dura de este
	// adaptador y la que menos se ejecuta sola.
	fmt.Println("\n=== una ruta por defecto sobre el adaptador virtual ===")
	if err := setDefaultRoute(); err != nil {
		mal("no se pudo fabricar la ruta por defecto: %v", err)
	} else {
		logSuccess("fabricada 0.0.0.0/0 sobre %s, con métrica altísima para que no gane", domain.AdapterName)
		if err := cfg.ApplyAdapter(ctx, base); err != nil {
			mal("ApplyAdapter: %v", err)
		}
		if routeExists(netip.MustParsePrefix("0.0.0.0/0")) {
			mal("la ruta por defecto SIGUE puesta. Kanpachi jamás enruta internet")
			_ = removeDefaultRoute()
		} else {
			logSuccess("netcfg la quitó")
		}
	}

	// 3. El MTU medido de verdad contra la puerta de enlace.
	fmt.Println("\n=== sondeo del MTU ===")
	if mtuValue, err := cfg.ProbeMTU(ctx); err != nil {
		fmt.Printf("  --  no se pudo sondear, y no es fatal: %v\n", err)
	} else {
		logSuccess("camino %d, túnel %d", mtuValue, domain.TunnelMTU(mtuValue))
	}

	fmt.Println()
	if failureCount > 0 {
		return fmt.Errorf("%d comprobación(es) fallaron", failureCount)
	}
	fmt.Println("Los caminos que una sala no ejercita hacen lo que dicen.")
	return nil
}

type loggerConsole struct{}

func (loggerConsole) Info(msg string, kv ...any)  { fmt.Println("  info ", msg, kv) }
func (loggerConsole) Warn(msg string, kv ...any)  { fmt.Println("  aviso", msg, kv) }
func (loggerConsole) Error(msg string, kv ...any) { fmt.Println("  error", msg, kv) }
