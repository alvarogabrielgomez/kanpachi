//go:build windows

package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/core/usecase"
	"github.com/accentiostudios/kanpachi/daemon/adapter/canary/opener"
	catalogstore "github.com/accentiostudios/kanpachi/daemon/adapter/catalog/jsonfile"
	"github.com/accentiostudios/kanpachi/daemon/adapter/directory"
	kanpachiengine "github.com/accentiostudios/kanpachi/daemon/adapter/engine/kanpachi"
	"github.com/accentiostudios/kanpachi/daemon/adapter/firewall"
	"github.com/accentiostudios/kanpachi/daemon/adapter/identity"
	"github.com/accentiostudios/kanpachi/daemon/adapter/inspector"
	"github.com/accentiostudios/kanpachi/daemon/adapter/library/steam"
	"github.com/accentiostudios/kanpachi/daemon/adapter/netcfg"
	"github.com/accentiostudios/kanpachi/daemon/adapter/probe"
	"github.com/accentiostudios/kanpachi/daemon/adapter/router/igd"
	"github.com/accentiostudios/kanpachi/daemon/adapter/routes"
	statestore "github.com/accentiostudios/kanpachi/daemon/adapter/state/jsonfile"
	"github.com/accentiostudios/kanpachi/daemon/adapter/sysevents"
	"github.com/accentiostudios/kanpachi/daemon/service/supervisor"
	"github.com/accentiostudios/kanpachi/daemon/transport/control"
	"github.com/accentiostudios/kanpachi/daemon/wiring"
)

// plazoDeApagado es cuánto se le da al cierre ordenado.
//
// Veinte segundos, que es `service.ApagadoPorDefecto` y por el mismo motivo:
// cerrar una sala es avisar a los miembros, escribir en el firewall por COM y
// bajar dos redes virtuales, y ninguna de las tres es instantánea.
const plazoDeApagado = 20 * time.Second

// relojReal es el único [port.Clock] de esta herramienta.
type relojReal struct{}

func (relojReal) Now() time.Time { return time.Now() }

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
	if d, err := desfaseDeReloj(context.Background(), op.seed); err != nil {
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
	if _, err := os.Stat(motorExe); err != nil {
		return fmt.Errorf("no está kanpachi-engine.exe junto a roomprobe.exe (%s). "+
			"Lo copia scripts/build_test_tools.ps1", op.dirExe)
	}

	ctxRaiz, pararSeñales := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer pararSeñales()

	// Los cierres se APUNTAN y no se ponen con defer: el apagado ordenado los
	// corre al revés y en un momento concreto, después de cerrar la sala.
	var cierres []func()
	apuntar := func(f func()) { cierres = append(cierres, f) }

	eventos := sysevents.New(log)
	apuntar(func() { _ = eventos.Close() })

	// El firewall antes que nada más: construir la sesión purga, y para purgar
	// hay que tener con qué.
	fw, cerrarFw, err := firewall.NewWindows(op.datos, log)
	if err != nil {
		return fmt.Errorf("abriendo el firewall (¿consola elevada?): %w", err)
	}
	apuntar(func() { _ = cerrarFw() })

	diario := usecase.NewJournal(relojReal{}, log)

	motor, err := kanpachiengine.New(kanpachiengine.Deps{
		Exe: motorExe, Log: log, LogDir: op.dirLog, Progress: diario,
	})
	if err != nil {
		return fmt.Errorf("preparando el motor: %w", err)
	}
	apuntar(func() { _ = motor.Close() })

	// Un motor huérfano de una corrida anterior tiene kanpachi0 tomado, y el
	// síntoma sería un `CreateRoom` que falla por un adaptador que ya existe.
	if n := motor.KillOrphans(); n > 0 {
		log.Warn("se mataron motores huérfanos de una corrida anterior", "cantidad", n)
	}

	// La llave larga de ESTA instalación de la sonda, y el canal construido
	// DESPUÉS de tenerla.
	//
	// Sin esto la sonda hospeda sin firmar, o sea que ningún cliente con
	// decisión 25 puede entrar a su sala: una respuesta sin firma, habiendo
	// llave fijada en el registro, se rechaza. Una herramienta de medición que
	// no puede reproducir el camino que mide no mide nada. El firmador lo arma
	// `wiring.ControlIdentity`, el mismo que usa el daemon.
	llave, err := identity.LoadOrCreate(op.datos, sinACL)
	if err != nil {
		return fmt.Errorf("cargando la llave de identidad: %w", err)
	}
	propia := llave.Public().(ed25519.PublicKey)
	log.Info("llave de identidad de esta máquina", "huella", domain.Fingerprint(propia),
		"para-qué", "es lo que las otras máquinas recuerdan de esta; se compara con lo que ellas enseñan al entrar")
	fmt.Println("Tu huella:", domain.Fingerprint(propia))

	canal := control.New(control.Deps{
		Clock: relojReal{}, Log: log, Identity: wiring.ControlIdentity(llave),
	})

	// El almacén se construye ACÁ y no dentro de las dependencias, porque de él
	// sale el registro con el que nace la fábrica.
	almacén := statestore.New(op.datos)

	// **El registro propio sale del DISCO, y la bandera solo lo pisa.**
	//
	// Sale del MISMO `wiring.SeedFromDisk` que usa el daemon, y no estaba:
	// la fábrica nacía solo con lo que trajera `-seed`, así que una corrida sin
	// bandera arrancaba con `seed.txt` escrito, la cabecera enseñando ese
	// registro —que lo lee del estado— y `CreateRoom` contestando "esta máquina
	// no tiene registro configurado", que es lo que la fábrica sabía. Dos
	// fuentes para el mismo dato, discrepando en pantalla.
	propioAlArrancar := op.seed
	if propioAlArrancar == "" {
		propioAlArrancar = wiring.SeedFromDisk(almacén, log)
	}
	op.seed = propioAlArrancar

	// La fábrica, igual que el producto: al entrar manda el registro del código
	// pegado, y el propio solo dice dónde ABRE salas esta sonda.
	registros := directory.NewFactory(directory.Deps{
		DataDir: op.datos, Log: log, Protect: sinACL,
	}, propioAlArrancar)

	sesion, err := usecase.NewSession(ctxRaiz, usecase.Deps{
		// Esta sonda es de Windows, así que la cuarentena es la de Windows. El
		// fichero lleva etiqueta `_windows`, o sea que la constante no puede
		// desalinearse con el sistema en el que corre.
		Quarantine:  domain.QuarantineWindows,
		Engine:      motor,
		Firewall:    fw,
		NetCfg:      netcfg.New(op.datos, log),
		Routes:      routes.New(),
		Store:       catalogstore.New(op.dirExe, op.datos, log),
		State:       almacén,
		Library:     steam.New(log),
		Directories: registros,
		Control:     canal,
		Audit:       wiring.Exposure{FW: fw, Router: igd.New(log)},
		Inspector:   inspector.New(),
		Prober:      probe.New(),
		Canary:      opener.New(log),
		Clock:       relojReal{},
		Rand:        rand.Reader,
		Log:         log,
		Progress:    diario,
	})
	if err != nil {
		return fmt.Errorf("construyendo la sesión: %w", err)
	}
	// Sin esto, `Serve` da ErrNotAttached y crear sala falla con el motor ya
	// levantado. La dependencia es circular y se resuelve a mano, igual que en
	// el daemon.
	canal.Attach(sesion)
	apuntar(func() { _ = canal.Close() })

	// Los pasos de cada operación NO se copian acá: los escribe el propio
	// `usecase.Journal`, así que salen igual en el log del daemon instalado y
	// del portable. Antes vivían en esta herramienta, y eso obligaba a correr
	// roomprobe para conseguir lo que el binario de producción ya debería estar
	// anotando.

	// El vigía de la malla vive en `daemon/service/supervisor`, no acá: es el
	// dato que contesta "¿los dos motores llegaron a verse?", y pedirle a
	// alguien que corra otra herramienta para conseguirlo era el problema.
	vigia := &supervisor.VigiaDeMalla{Motor: motor, Log: log}
	go vigia.Correr(ctxRaiz)

	bucle, err := supervisor.New(supervisor.Deps{
		Room: sesion, Engine: motor, Control: canal, System: eventos, Log: log,
	})
	if err != nil {
		return fmt.Errorf("armando el supervisor: %w", err)
	}

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

	e := entorno{s: sesion, log: log, c: c, op: &op, registros: registros,
		apagar: apagar, fallos: &fallos}

	// La bandera `-seed` SIEMBRA el estado, no lo sustituye.
	//
	// Antes solo llenaba `own` de la fábrica, así que la sesión seguía sin
	// registro guardado y crear sala moría en "esta máquina no tiene registro
	// configurado" con el nombre delante, en la cabecera. Guardarlo por el
	// mismo camino que la ventana deja las dos cosas de acuerdo, y de paso
	// comprueba que ese registro conteste antes de que nadie intente nada.
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
