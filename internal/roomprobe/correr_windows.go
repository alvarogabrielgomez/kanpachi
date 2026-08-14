//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/accentiostudios/kanpachi/core/usecase"
	"github.com/accentiostudios/kanpachi/daemon/adapter/directory"
	"github.com/accentiostudios/kanpachi/daemon/preflight"
	"github.com/accentiostudios/kanpachi/daemon/wiring"
)

// plazoDeApagado es cuánto se le da al cierre ordenado.
//
// Veinte segundos, que es `service.ApagadoPorDefecto` y por el mismo motivo:
// cerrar una sala es avisar a los miembros, escribir en el firewall por COM y
// bajar dos redes virtuales, y ninguna de las tres es instantánea.
const plazoDeApagado = 20 * time.Second

// sinACL deja `identity.key` con los permisos que herede del directorio.
//
// El daemon le pone una ACL propia con `protegerFichero`, y acá no: esta
// herramienta escribe en un `data` de pruebas al lado del ejecutable y no en
// `ProgramData`, así que no hay ACL de la que heredar. Es aceptable en una
// herramienta de medición y **no lo es en el producto**; está dicho acá y en el
// doc del paquete para que nadie copie este cableado creyendo que está entero.
func sinACL(string) error { return nil }

func correr(op opciones) error {
	// El log es lo PRIMERO que se abre, antes incluso de mirar el servicio.
	// Todo lo que falle a partir de aquí queda en disco, incluido el portazo
	// del paso siguiente.
	log := nuevoLog(op.dirLog)
	defer func() { _ = log.Close() }()
	log.Info("roomprobe arranca", "datos", op.datos, "registro", op.seed,
		"nombre", op.nick, "log", filepath.Join(op.dirLog, LogFile))
	fmt.Println("El log va a", filepath.Join(op.dirLog, LogFile))

	// El desfase de reloj se anota ANTES que nada más, porque es lo que hace
	// legible todo lo que viene después cuando este fichero se lee junto al de
	// la otra máquina. Ver [desfaseDeReloj]. No poder medirlo no detiene nada.
	if d, err := preflight.ClockSkew(context.Background(), op.seed); err != nil {
		log.Warn("no se pudo comparar el reloj de esta máquina con el del registro",
			"error", err, "consecuencia", "los tiempos de este log no se pueden alinear con los de otra máquina")
	} else {
		log.Info("reloj de esta máquina", "adelantado-respecto-al-registro", d.Round(time.Second).String(),
			"cómo-se-usa", "restarle esto a cada hora de este log da la hora del registro, que es la que comparten las dos máquinas")
	}

	if err := comprobarServicio(log, op.force); err != nil {
		return err
	}

	c := abrirConsola()
	defer c.cocido()

	// El motor tiene que estar al lado. No se busca en el PATH, por lo mismo
	// que el daemon: un PATH que alguien pueda escribir es una forma de que
	// este proceso, que corre elevado, ejecute otro ejecutable con ese nombre.
	motorExe := filepath.Join(op.dirExe, "kanpachi-engine.exe")
	if err := preflight.EngineAt(motorExe); err != nil {
		return fmt.Errorf("%w. Lo copia scripts/build-test-tools.ps1", err)
	}

	ctxRaiz, pararSeñales := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer pararSeñales()

	// Los cierres se APUNTAN y no se ponen con defer: el apagado ordenado los
	// corre al revés y en un momento concreto, después de cerrar la sala.
	var cierres []func()
	apuntar := func(f func()) { cierres = append(cierres, f) }

	// El MISMO cableado que el daemon, construido una vez en
	// [wiring.BuildSession]. Las desviaciones de esta sonda van explícitas en
	// los parámetros, y cada una tiene su porqué al lado; todo lo que no se
	// nombra es lo que hace el producto, estado sellado y tokens del seed
	// incluidos.
	watch := wiring.NewWatchers(log)
	apuntar(func() { _ = watch.Events.Close() })

	built, err := wiring.BuildSession(ctxRaiz, wiring.SessionParams{
		DataDir:   op.datos,
		LogDir:    op.dirLog,
		EngineExe: motorExe,
		// El catálogo de fábrica viaja junto al ejecutable, igual que en
		// Windows lo hace el del producto.
		BuiltinCatalogDir: op.dirExe,
		// Sin ACL, y es la única desviación de seguridad: el `data` de esta
		// herramienta no vive en ProgramData y no hay ACL de la que heredar.
		// Aceptable en una sonda y NO en el producto; ver [sinACL].
		Protect: sinACL,
		// La bandera pisa el disco; vacía, manda lo guardado.
		SeedOverride: op.seed,
		// Esta herramienta muere sucio con frecuencia, y un motor huérfano
		// tiene kanpachi0 tomado: el síntoma sería un CreateRoom que falla por
		// un adaptador que ya existe.
		KillOrphans: true,
		Watchers:    watch,
		Log:         log,
	})
	for _, c := range built.Closers {
		apuntar(c)
	}
	if err != nil {
		return err
	}
	sesion, canal, bucle := built.Session, built.Control, built.Supervisor
	op.seed = built.OwnSeed

	log.Info("llave de identidad de esta máquina", "huella", built.Fingerprint,
		"para-qué", "es lo que las otras máquinas recuerdan de esta; se compara con lo que ellas enseñan al entrar")
	fmt.Println("Tu huella:", built.Fingerprint)

	apuntar(func() { _ = canal.Close() })

	// Los pasos de cada operación NO se copian acá: los escribe el propio
	// `usecase.Journal`, así que salen igual en el log del daemon instalado y
	// del portable. Antes vivían en esta herramienta, y eso obligaba a correr
	// roomprobe para conseguir lo que el binario de producción ya debería estar
	// anotando.

	// El menú no se abre hasta que el supervisor está drenando.
	//
	// Es la regla de `daemon/service.Start`, con el menú en el sitio del named
	// pipe. No se reutiliza `service.Start` porque exige una `Entrada`, que es
	// el pipe, y acá no hay ninguno; pasarle una falsa metería un provisional
	// en el camino de arranque para satisfacer una firma. Lo que se copia es la
	// regla, que es lo que importa: sin esperar, el primer "crear sala" puede
	// correr antes de que nadie drene el canal de eventos del motor, y el
	// arranque de la sala se pierde los primeros.
	listo := make(chan struct{})
	go func() {
		if err := bucle.Run(ctxRaiz, listo); err != nil && !errors.Is(err, context.Canceled) {
			log.Error("el supervisor se detuvo", "error", err)
		}
	}()
	select {
	case <-listo:
	case <-ctxRaiz.Done():
		return ctxRaiz.Err()
	}
	log.Info("el supervisor está drenando; el menú queda abierto")

	// ─── Apagado, una sola vez ───────────────────────────────────────────────
	var unaVez sync.Once
	fallos := 0
	apagar := func() {
		unaVez.Do(func() {
			c.cocido()
			fmt.Println("\nCerrando...")
			pararSeñales()

			// `WithoutCancel` NO es opcional: `ctxRaiz` ya viene cancelado si se
			// llegó por Ctrl+C, y con un contexto cancelado cada cierre de
			// puerto es un no-op, o sea que el apagado limpio no limpiaría nada.
			ctxCierre, fin := context.WithTimeout(
				context.WithoutCancel(ctxRaiz), plazoDeApagado)
			defer fin()

			// Esto es el cierre gracioso: siendo host avisa a todos los
			// miembros ANTES de desmontar nada, y después vuelve a MEDIR y
			// devuelve error si quedó algún puerto abierto.
			if err := sesion.LeaveRoomOnShutdown(ctxCierre); err != nil {
				log.Error("el cierre de la sala no quedó limpio", "error", err)
				fmt.Println("  MAL el cierre no quedó limpio:", err)
				fallos++
			}

			// Al revés de como se montó: primero se va el motor y con él la red
			// virtual, y SOLO ENTONCES se sueltan las reglas que la contenían.
			for i := len(cierres) - 1; i >= 0; i-- {
				cierres[i]()
			}
			log.Info("roomprobe termina", "fallos", fallos)
		})
	}
	defer apagar()

	e := entorno{s: sesion, log: log, c: c, op: &op, registros: built.Directories,
		apagar: apagar, fallos: &fallos}

	// La bandera `-seed` SIEMBRA el estado, no lo sustituye: si lo que vino por
	// bandera no es lo que hay guardado, se guarda por el mismo camino que la
	// ventana, que valida, sondea y solo entonces escribe.
	if op.seed != "" && sesion.OwnSeed() != op.seed {
		if _, err := guardarRegistro(ctxRaiz, e, op.seed); err != nil {
			return err
		}
	}
	autenticarSiHaceFalta(ctxRaiz, e)

	if err := menuPrincipal(ctxRaiz, e); err != nil && !errors.Is(err, errInterrumpido) && !errors.Is(err, context.Canceled) {
		return err
	}
	apagar()
	if fallos > 0 {
		return fmt.Errorf("%d comprobación(es) terminaron mal, mira %s", fallos, LogFile)
	}
	return nil
}

// entorno es lo que los menús necesitan, junto en vez de en siete parámetros.
type entorno struct {
	s   *usecase.Session
	log *logRoomprobe
	c   *consola
	op  *opciones
	// registros es la MISMA fábrica que usa la sesión, para poder preguntarle a
	// un registro por su cuenta sin construir un segundo cliente que hablaría
	// con otras opciones y mediría otra cosa. Ver [verFirmaDelRegistro].
	registros *directory.Factory
	apagar    func()
	fallos    *int
}
