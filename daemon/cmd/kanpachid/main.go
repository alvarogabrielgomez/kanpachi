// Command kanpachid es el daemon.
//
// Acá vive la ELECCIÓN de adaptadores concretos, que es lo único que este
// binario decide. El ORDEN vive en `daemon/service`, que es Go puro y corre en
// el job de Linux. El `main` está acá y no allá porque el guardián de pureza no
// mira etiquetas de compilación: un main con `//go:build windows` dentro de
// service rompería ese job. Mismo reparto que `registry/cmd/kanpseed`.
package main

import (
	"context"
	"crypto/rand"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/core/port"
	"github.com/accentiostudios/kanpachi/core/usecase"
	"github.com/accentiostudios/kanpachi/daemon/adapter/canary/opener"
	catalogstore "github.com/accentiostudios/kanpachi/daemon/adapter/catalog/jsonfile"
	"github.com/accentiostudios/kanpachi/daemon/adapter/directory"
	kanpachiengine "github.com/accentiostudios/kanpachi/daemon/adapter/engine/kanpachi"
	"github.com/accentiostudios/kanpachi/daemon/adapter/identity"
	"github.com/accentiostudios/kanpachi/daemon/adapter/inspector"
	"github.com/accentiostudios/kanpachi/daemon/adapter/library/steam"
	"github.com/accentiostudios/kanpachi/daemon/adapter/netcfg"
	"github.com/accentiostudios/kanpachi/daemon/adapter/probe"
	"github.com/accentiostudios/kanpachi/daemon/adapter/router/igd"
	"github.com/accentiostudios/kanpachi/daemon/adapter/routes"
	"github.com/accentiostudios/kanpachi/daemon/adapter/sinimplementar"
	statestore "github.com/accentiostudios/kanpachi/daemon/adapter/state/jsonfile"
	"github.com/accentiostudios/kanpachi/daemon/adapter/sysevents"
	"github.com/accentiostudios/kanpachi/daemon/adapter/uihost"
	"github.com/accentiostudios/kanpachi/daemon/service"
	"github.com/accentiostudios/kanpachi/daemon/service/supervisor"
	"github.com/accentiostudios/kanpachi/daemon/transport/control"
	"github.com/accentiostudios/kanpachi/daemon/transport/pipe"
)

func main() {
	// LO PRIMERO, antes incluso de leer las banderas: sin esto, un binario
	// enlazado con `-H windowsgui` no puede imprimir ni el error de una bandera
	// mal escrita. Ver [reengancharConsola].
	reengancharConsola()

	consola := flag.Bool("console", false, "correr como aplicación de consola en vez de servicio")
	// **La bandera del acceso directo.** Ver [abrir] para los papeles de este
	// binario y por qué son uno y no varios ejecutables.
	mostrar := flag.Bool("show", false, "arrancar Kanpachi y enseñar la ventana")
	// El daemon de una carpeta portable: este proceso ES el daemon, sin
	// Administrador de servicios detrás. Solo vale en una carpeta portable, y
	// eso se comprueba. Ver [portable.go].
	suelto := flag.Bool("daemon", false,
		"correr el daemon en este proceso, sin servicio. Solo en una carpeta portable")
	datos := flag.String("data", "", "directorio de datos. Vacío usa ProgramData\\Kanpachi")
	// El nombre del pipe se puede cambiar SOLO en modo consola, y existe por una
	// razón concreta: el de producción vive bajo ProtectedPrefix\Administrators,
	// que Windows no deja crear sin elevar. Sin esta bandera, probar el saludo,
	// el token y los topes exigiría un UAC cada vez.
	//
	// No es un agujero: en modo servicio no se lee, y un binario con
	// provisionales no arranca como servicio.
	nombre := flag.String("pipe", "", "nombre del pipe. Solo con --console. Vacío usa el protegido")
	// El hard reset y el camino del desinstalador. Son dos banderas y no una, y
	// la diferencia es deliberada: ver [service.Reset].
	reset := flag.Bool("reset", false,
		"deshacer todo lo de la sala y salir. REPONE la cuarentena de base y conserva la última sala")
	desinstalar := flag.Bool("uninstall-cleanup", false,
		"lo mismo que --reset y ADEMÁS quitar la cuarentena de base. Solo para el desinstalador")
	flag.Parse()

	if *reset || *desinstalar {
		if err := limpiar(*datos, *desinstalar); err != nil {
			fmt.Fprintln(os.Stderr, "kanpachid:", err)
			os.Exit(1)
		}
		return
	}

	if err := correr(*consola, *suelto, *mostrar, *datos, *nombre); err != nil {
		fmt.Fprintln(os.Stderr, "kanpachid:", err)
		// Y en una ventana si no hay consola, que es el caso del doble clic.
		// Sin esto, un acceso directo que falla no hace nada visible. Ver
		// [avisar].
		//
		// El motivo va con su etiqueta y no suelto: los errores de este camino
		// son de dos clases muy distintas —los que están escritos para leerse,
		// como el permiso del servicio, y los que son la cadena cruda de una
		// API de Windows— y quien lee no tiene por qué adivinar cuál le tocó.
		avisar("Kanpachi no pudo arrancar.\n\nMotivo:\n" + err.Error())
		os.Exit(1)
	}
}

// limpiar corre el hard reset, y con `desinstalar` además quita la cuarentena.
//
// # Por qué no pasa por `correr`
//
// Porque el reset existe para la máquina donde el arranque ESTÁ ROTO. Montar la
// sesión entera antes de limpiar sería exigir que funcione justo lo que el
// usuario dice que no funciona. Acá se abren las piezas mínimas, se limpia, y se
// sale.
func limpiar(datos string, desinstalar bool) error {
	datos = dirDeDatos(datos)
	if _, err := os.Stat(datos); err != nil {
		return fmt.Errorf("el directorio de datos %s no está, así que no hay nada que limpiar", datos)
	}

	ctx, parar := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer parar()

	log := logConsola{}

	// El auditor se descarta (`_`): limpiar no pregunta nada. Va el de verdad
	// igual, para que en el cableado no quede ni un provisional.
	fw, _, cerrarFirewall, err := realFirewall(datos, log, igd.New(log))
	if err != nil {
		return err
	}
	defer func() { _ = cerrarFirewall() }()

	motor, err := kanpachiengine.New(kanpachiengine.Deps{
		Exe: filepath.Join(dirDelBinario(), "kanpachi-engine.exe"),
		Log: log,
	})
	if err != nil {
		return err
	}
	defer func() { _ = motor.Close() }()

	errReset := service.Reset(ctx, service.ResetDeps{
		Firewall: fw,
		NetCfg:   netcfg.New(datos, log),
		Engine:   motor,
		State:    statestore.New(datos),
		Log:      log,
	})

	if !desinstalar {
		return errReset
	}

	// La parte que el reset JAMÁS hace. Va después y no antes: si algo del
	// reset falla, la cuarentena se quita igual, porque desinstalar a medias
	// dejaría puertos bloqueados en una máquina donde ya no hay nada que los
	// explique.
	if errReset != nil {
		log.Warn("el reset falló y la desinstalación sigue igual", "error", errReset)
	}

	// La llave larga de esta instalación, que el reset CONSERVA a propósito.
	//
	// Se borra acá y explícitamente, y no se da por hecho que "ya la borrará el
	// instalador al llevarse el directorio": hoy nada de este repositorio borra
	// ese directorio, así que darlo por hecho dejaría la llave viva en una
	// máquina donde Kanpachi ya no está. Es best effort porque desinstalar tiene
	// que terminar: una llave que no se pudo borrar es una molestia, y una
	// desinstalación a medias es una máquina con puertos bloqueados que nadie
	// puede explicar.
	llave := filepath.Join(datos, identity.IdentityFile)
	if err := os.Remove(llave); err != nil && !os.IsNotExist(err) {
		log.Warn("no se pudo borrar la llave de esta instalación", "ruta", llave, "error", err)
	}

	if err := quitarCuarentenaDeBase(ctx, datos, log); err != nil {
		return err
	}
	return nil
}

// La interfaz, nombrada en un solo sitio.
//
// El nombre del ejecutable y el de la bandera son un CONTRATO entre este
// binario y el de Flutter, y de esos que no dan error al romperse: cambiar uno
// sin el otro produce un daemon que no encuentra su interfaz, o una interfaz
// que abre ventana cuando le pidieron que no. Escritos acá para que se vean
// juntos.
// La bandera pide MOSTRAR, no callar, y esa vuelta es deliberada: el silencio
// es el default de los dos ejecutables. Una bandera que se pierda por el camino
// deja la interfaz callada en vez de abriendo una ventana sola.
const (
	uiExeName  = "kanpachiui.exe"
	uiShowFlag = "--show"
)

// watchers son los cuatro adaptadores que MIRAN la máquina sin cambiarla: los
// avisos del sistema, la biblioteca de Steam, la tabla de sockets y los mapeos
// del router.
//
// Están juntos porque comparten la propiedad que importa: ninguno escribe nada,
// así que ninguno puede romper una máquina si falla. Los que sí escriben
// —firewall, netcfg, motor— se construyen aparte y con más ceremonia.
//
// Se eligen en un solo sitio porque de acá salen dos cosas: el cableado de la
// sesión, y la comprobación de si alguno sigue siendo provisional.
type watchers struct {
	Events    port.SystemEvents
	Library   port.GameLibrary
	Inspector port.SocketInspector
	Router    port.ExposureAudit
}

func chooseWatchers(log port.Logger) watchers {
	return watchers{
		Events:    sysevents.New(log),
		Library:   steam.New(log),
		Inspector: inspector.New(),
		Router:    igd.New(log),
	}
}

// provisionales pregunta cuáles de los elegidos todavía no existen de verdad.
func (m watchers) stubbed() []string {
	return sinimplementar.Names(m.Events, m.Library, m.Inspector, m.Router)
}

// booted es lo que un arranque deja vivo: cómo esperar a que se caiga, y cómo
// apagarlo.
//
// Existe porque los DOS modos necesitan exactamente el mismo cableado y lo
// necesitan de formas distintas. En consola, el proceso espera y Ctrl+C apaga.
// Como servicio, el Administrador de servicios exige que el arranque DEVUELVA
// cuando el daemon está listo, y que esperar y apagar sean dos cosas
// separadas: es lo que hace que SERVICE_RUNNING se reporte después de que el
// pipe esté abierto y no antes.
//
// Devolver esto en vez de bloquear es lo que permite que el cableado se escriba
// UNA vez. Duplicarlo por modo es exactamente el fallo que este repositorio ya
// tuvo tres veces: `control.Attach`, `firewall.SetScope` y este mismo modo
// servicio, todos escritos, probados, y llamados por nadie.
type booted struct {
	wait     func() error
	shutdown func()
}

func correr(consola, suelto, mostrar bool, datos, nombre string) error {
	portable := esPortable()
	datos = dirDeDatos(datos)

	if _, err := os.Stat(datos); err != nil {
		// **En una carpeta portable se crea, y en el producto instalado no.**
		//
		// No es una excepción por comodidad: son dos dueños distintos. El
		// directorio de ProgramData lo crea el INSTALADOR con una ACL propia, y
		// esa ACL es la mitad de la protección del token; crearlo acá la
		// perdería en silencio. En portable no hay instalador que lo cree, así
		// que la alternativa a crearlo es que la carpeta no arranque nunca.
		//
		// Lo que se pierde con eso queda dicho en `docs/03`: los datos de un
		// Kanpachi portable heredan los permisos de donde alguien dejó la
		// carpeta.
		if !portable {
			return fmt.Errorf("el directorio de datos %s no está.\n"+
				"  Lo crea el instalador con su ACL. Para probar, créalo a mano o pasa --data", datos)
		}
		if err := os.MkdirAll(datos, 0o700); err != nil {
			return fmt.Errorf("creando el directorio de datos %s de la carpeta portable: %w", datos, err)
		}
	}

	// **La pregunta se la hace a Windows, no a la bandera.** Arrancar como
	// servicio y arrancar a mano se distinguen por CÓMO entró el proceso, no
	// por lo que alguien escribió en la línea de comandos. Ver [EnServicio].
	//
	// `--console` no fuerza el modo consola: lo pide, y si resulta que a este
	// proceso lo arrancó el Administrador de servicios, gana lo que dice el
	// sistema. Al revés, un servicio arrancado con la bandera puesta se
	// quedaría sin contestarle nunca al SCM, que lo mataría por no arrancar.
	enServicio, err := EnServicio()
	if err != nil {
		return fmt.Errorf("preguntando si este proceso es un servicio: %w", err)
	}

	if enServicio {
		// El nombre de producción, siempre. La bandera `--pipe` no se lee acá y
		// eso es la mitad de por qué existe: el nombre alternativo sirve para
		// no pedir un UAC en cada prueba, y un servicio que lo aceptara sería
		// una forma de que el daemon de verdad atienda en un nombre que
		// cualquiera puede ocupar.
		return CorrerComoServicio(func(ctx context.Context, args []string) (func() error, func(), error) {
			// Los argumentos con los que ALGUIEN arrancó el servicio, que no son
			// los de la línea de comandos de este proceso. Windows los pasa por
			// `StartService`, y es como el lanzador dice "ábrela con ventana":
			// el arranque automático de Windows no manda ninguno, así que la
			// interfaz sale en silencio.
			b, err := arrancar(ctx, datos, pipe.Name, false, tiene(args, ArgShow))
			if err != nil {
				return nil, nil, err
			}
			return b.wait, b.shutdown, nil
		})
	}

	// **El daemon de una carpeta portable.** Mismo cableado que el servicio y
	// mismo pipe de producción; lo único que cambia es quién sostiene el
	// proceso, que acá es nadie: vive hasta que lo apaguen.
	//
	// Se exige el marcador y no basta con la bandera. Sin esa comprobación,
	// `kanpachid.exe --daemon` en una máquina con Kanpachi instalado sería un
	// segundo daemon peleándose por el mismo nombre de pipe con el servicio.
	if suelto {
		if !portable {
			return fmt.Errorf("--daemon solo vale en una carpeta portable, " +
				"y acá no está el fichero " + PortableMarker + ".\n" +
				"  Instalado, al daemon lo arranca el Administrador de servicios")
		}
		ctx, parar := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer parar()

		b, err := arrancar(ctx, datos, pipe.Name, false, mostrar)
		if err != nil {
			return err
		}
		go func() {
			<-ctx.Done()
			b.shutdown()
		}()
		return b.wait()
	}

	// **Ni servicio ni consola: entonces esto es el LANZADOR.**
	//
	// Es el camino del doble clic, y el default a propósito: quien encuentre
	// este binario en Program Files y lo ejecute obtiene un Kanpachi corriendo,
	// nunca un segundo daemon compitiendo con el que ya hay. Correr el daemon a
	// mano hay que PEDIRLO con `--console`, y ese pide un nombre de pipe
	// distinto justamente para no poder ocupar el de producción. Ver [abrir].
	if !consola {
		return abrir(datos, mostrar)
	}

	if nombre == "" {
		nombre = pipe.ConsoleName
	}

	ctx, parar := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer parar()

	b, err := arrancar(ctx, datos, nombre, consola, false)
	if err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		// Says WHICH way out this was. Without it, an interrupt and an order
		// over the pipe both showed up as one line saying the daemon was
		// shutting down, with no way to tell a Ctrl+C from a console nobody
		// was looking at apart from the interface asking to quit. Measured:
		// the console daemon died twice with no explanation, and the absence
		// of the interface's own line was the only clue.
		fmt.Fprintln(os.Stderr, "kanpachid: llegó una interrupción, se apaga")
		b.shutdown()
	}()
	return b.wait()
}

// arrancar monta el daemon entero y devuelve cuando está listo.
//
// # Por qué no usa `defer` para limpiar
//
// Porque en modo servicio esta función RETORNA con el daemon vivo, así que un
// `defer` correría el cierre justo cuando acaba de arrancar. Los cierres se
// apuntan en orden y se corren al revés a mano, en dos sitios: si el arranque
// falla a mitad, y dentro de `shutdown`.
func arrancar(ctx context.Context, datos, nombre string, consola, mostrarUI bool) (*booted, error) {
	// **Un servicio no tiene salida estándar.** En consola quien mira es una
	// persona con una terminal delante; como servicio, todo lo que se imprima se
	// pierde y un arranque fallido queda sin una sola línea que lo explique. Ver
	// [logArchivo].
	var log port.Logger = logConsola{}
	var cierres []func()
	if !consola {
		archivo := nuevoLogArchivo(datos)
		log = archivo
		// El último en cerrarse, o sea el primero de la lista: los cierres
		// corren al revés, y lo que se apaga por el camino tiene que poder
		// dejarlo escrito.
		cierres = append(cierres, func() { _ = archivo.Close() })
	}

	// Los cierres, en orden de creación. Se corren al revés, que es lo que
	// hacía el `defer` y por los mismos motivos: el motor se va antes que el
	// firewall, para que la red virtual desaparezca antes que las reglas que la
	// contenían.
	cerrarTodo := func() {
		for i := len(cierres) - 1; i >= 0; i-- {
			cierres[i]()
		}
	}
	abortar := func(err error) (*booted, error) {
		// Se deja escrito ANTES de cerrar, que es cuando el log todavía existe.
		// Como servicio esta línea es lo único que va a quedar: quien devuelve
		// este error es el manejador del SCM, y lo que Windows enseña entonces
		// es "el servicio se detuvo", sin motivo.
		log.Error("el arranque falló", "error", err)
		cerrarTodo()
		return nil, err
	}

	// Los watchers ANTES del guardián, porque el guardián les pregunta a ellos.
	// Ninguno escribe nada, así que construirlos y descartarlos no deja rastro.
	watch := chooseWatchers(log)
	cierres = append(cierres, func() { _ = watch.Events.Close() })

	// **Un binario con provisionales NO se instala como servicio.** El riesgo
	// real nunca fue que fallen: es que uno con un firewall que dice que purgó
	// termine corriendo en la máquina de alguien.
	//
	// La lista se le pregunta al CABLEADO y no a una constante: ver
	// [sinimplementar.Provisional]. Un provisional que vuelva se delata solo, y
	// el error lo nombra en vez de decir que hay algo. Va ANTES del firewall,
	// que es lo primero con efectos de verdad.
	if missing := watch.stubbed(); len(missing) > 0 && !consola {
		return abortar(fmt.Errorf("este binario lleva adaptadores provisionales dentro (%s), "+
			"así que solo arranca con --console.\n"+
			"  Un provisional que devuelve éxito hace la cuarentena inverificable, y eso "+
			"instalado es peor que no tener daemon", strings.Join(missing, ", ")))
	}

	// El firewall ANTES que el resto, y no por orden de lectura: construir la
	// sesión purga las reglas de la ejecución anterior, así que si el firewall
	// no se puede abrir hay que enterarse acá y no a mitad del arranque.
	fw, audit, cerrarFirewall, err := realFirewall(datos, log, watch.Router)
	if err != nil {
		return abortar(err)
	}
	cierres = append(cierres, func() { _ = cerrarFirewall() })

	// El host del proceso: lanzar la interfaz, apagar todo, y el arranque con
	// Windows. Se construye acá porque el listener del pipe lo necesita, y su
	// `apagar` se une al final, cuando existe.
	host := &procesoHost{log: log}

	// La interfaz NO se hospeda en modo consola, y esa es toda la diferencia.
	// El modo consola es para desarrollar: quien lo usa ya tiene una terminal
	// delante y arranca la interfaz cuando quiere. Levantarle una ventana en
	// cada `--console` sería una molestia, y peor, taparía el caso que el
	// producto de verdad tiene que resolver, que es el daemon lanzándola él.
	var ui *uihost.Host
	if !consola {
		ui, err = uihost.New(uihost.Deps{
			// Junto a este binario, y de `os.Executable()`. **Nunca del estado,
			// de la configuración ni del pipe**: esto corre como SYSTEM, y una
			// ruta que alguien pueda influir es escalada de privilegios.
			Exe:      filepath.Join(dirDelBinario(), uiExeName),
			ShowFlag: uiShowFlag,
			// Si la interfaz no arranca ni a la tercera, se apaga todo. Un
			// daemon vivo sin forma de mostrarse es justo lo que la invariante
			// de `docs/03` prohíbe.
			OnGiveUp: func() { host.Shutdown() },
			Log:      log,
		})
		if err != nil {
			return abortar(err)
		}
		cierres = append(cierres, func() { _ = ui.Close() })
		host.ui = ui
	}

	eventos := watch.Events

	canal := control.New(control.Deps{Clock: relojReal{}, Log: log})

	// El registro del seed, que es SOLO presentación: si no contesta, la sala se
	// crea igual y lo que se pierde es la tarjeta de la página de invitación.
	//
	// El seed sale del dominio y no de una bandera: hoy es uno solo. El campo
	// existe para el día que "Avanzado" deje elegir otro, y ese día es esto lo
	// que cambia, no el caso de uso.
	directorio, err := directory.New(directory.Deps{
		DataDir: datos,
		Seed:    domain.DefaultSeedHost,
		Log:     log,
		Protect: protegerFichero,
	})
	if err != nil {
		return abortar(err)
	}

	// El motor REAL. Vive al lado de este binario y no se busca en el PATH: un
	// PATH que alguien pueda escribir es una forma de que este proceso, que
	// corre como SYSTEM, ejecute otro ejecutable con ese nombre.
	//
	// No arranca acá. El proceso hijo se lanza con la primera orden que lo
	// necesite, así que un daemon que nunca abre una sala nunca levanta un
	// motor.
	// Diary of long op. Made HERE and not inside the session because the
	// ADAPTERS write to it too: only engine adapter knows engine process just
	// started, or that virtual adapter took twelve seconds to get its address.
	diary := usecase.NewJournal(relojReal{})

	motor, err := kanpachiengine.New(kanpachiengine.Deps{
		Exe:      filepath.Join(dirDelBinario(), "kanpachi-engine.exe"),
		Log:      log,
		Progress: diary,
	})
	if err != nil {
		return abortar(err)
	}
	// El cierre se apunta acá arriba y no al final por el orden en que se
	// corren: al revés, así que este corre DESPUÉS del cierre del firewall.
	// Es el orden que hace falta: primero se va el motor y con él la red
	// virtual, y solo entonces se sueltan las reglas que la contenían.
	cierres = append(cierres, func() { _ = motor.Close() })

	// NewSession PURGA el firewall antes de devolver, así que a partir de acá la
	// máquina está en el estado que este arranque decidió y no en el que dejó el
	// anterior. Que la purga esté dentro del constructor y no en una llamada
	// aparte es lo que hace que no se pueda saltar.
	sesion, err := usecase.NewSession(ctx, usecase.Deps{
		Engine:   motor,
		Firewall: fw,
		// Los ajustes del adaptador. MANTIENE en vez de aplicar: Windows revierte
		// la métrica, la categoría y las rutas en cada evento de identificación
		// de red, así que el supervisor lo reaplica entero, y además cada tantos
		// latidos por si esa suscripción está muerta.
		NetCfg: netcfg.New(datos, log),
		// La tabla de rutas REAL. Se consulta al crear o al entrar a una sala,
		// nunca al instalar: la LAN de una laptop cambia entre la casa y la
		// oficina, y un rango elegido en la instalación sería correcto solo el
		// primer día.
		Routes:    routes.New(),
		Store:     catalogstore.New(dirDelBinario(), datos, log),
		State:     statestore.New(datos),
		Library:   watch.Library,
		Directory: directorio,
		Control:   canal,
		Audit:     audit,
		Inspector: watch.Inspector,
		Prober:    probe.New(),
		// El canario es real desde el primer día, y puede serlo porque es `net`
		// puro: no toca Windows ni necesita privilegios para ligar en el
		// adaptador virtual. Ver daemon/adapter/canary.
		Canary:   opener.New(log),
		Clock:    relojReal{},
		Log:      log,
		Rand:     rand.Reader,
		Progress: diary,
	})
	if err != nil {
		return abortar(err)
	}

	// El canal y la sesión se unen ACÁ, y hace falta: la dependencia es circular
	// por naturaleza, porque la sesión recibe el canal en su `Deps` y el canal
	// necesita a la sesión para contestar la puerta del vestíbulo. Se construye
	// uno, después el otro, y se unen.
	//
	// Sin esto `Serve` devuelve [control.ErrNotAttached] y crear una sala falla
	// entera al abrir el canal, con el motor ya levantado. Pasó con el daemon de
	// verdad: `Attach` existía, estaba probado, y solo lo llamaban los tests.
	canal.Attach(sesion)

	bucle, err := supervisor.New(supervisor.Deps{
		Room:    sesion,
		Engine:  motor,
		Control: canal,
		System:  eventos,
		Log:     log,
	})
	if err != nil {
		return abortar(err)
	}

	// El token rota una vez por vida del proceso y se borra en TODO camino de
	// salida: uno que sobreviva al proceso no abre nada y solo es un secreto
	// muerto en disco.
	token, err := pipe.NewToken()
	if err != nil {
		return abortar(err)
	}
	if err := pipe.WriteToken(datos, token); err != nil {
		return abortar(err)
	}
	cierres = append(cierres, func() { _ = pipe.RemoveToken(datos) })

	ln, err := pipe.Listen(pipe.Deps{
		API: sesion,
		// El proceso, aparte de la sala. En consola va con `ui` nil y los tres
		// métodos del proceso contestan que no están, que es la verdad.
		Host:  host,
		Token: token,
		Clock: relojReal{},
		Log:   log,
		Name:  nombre,
	})
	if err != nil {
		return abortar(err)
	}

	rt, err := service.Start(ctx, service.Deps{
		Bucle:   bucle,
		Entrada: ln,
		Sala:    sesion,
		Log:     log,
	})
	if err != nil {
		_ = ln.Close()
		return abortar(err)
	}

	if consola {
		fmt.Printf("kanpachid en modo consola\n  pipe:  %s\n  token: %s\n  datos: %s\n\n"+
			"Ctrl+C para salir. Prueba con:  go run ./internal/kanpctl -data %q status\n\n",
			nombre, token, datos, datos)
	} else {
		log.Info("kanpachid listo", "pipe", nombre, "datos", datos)
	}

	// Una sola vez. Como servicio, `shutdown` lo puede llamar el SCM, además
	// pedirlo la interfaz por el pipe, y además dispararlo el bucle al caerse
	// solo. Tres apagados a la vez sueltan los mismos handles tres veces.
	var unaVez sync.Once
	apagar := func() {
		unaVez.Do(func() {
			// El apagado tiene su PROPIO contexto dentro de service, porque el
			// de acá puede venir ya cancelado y con él cada cierre de puerto
			// sería un no-op.
			if err := rt.Shutdown(ctx); err != nil {
				log.Error("el apagado no terminó bien", "error", err)
			}
			cerrarTodo()
		})
	}

	// El host y el apagado se unen ACÁ, y hace falta: la dependencia es
	// circular por naturaleza. El host lo necesita el listener del pipe, el
	// listener lo necesita el arranque del servicio, y el apagado sale de ese
	// arranque. Mismo patrón que `canal.Attach(sesion)` unas líneas arriba, y
	// por el mismo motivo.
	host.apagar = apagar

	// La interfaz, LO ÚLTIMO. Antes de esta línea el daemon no estaba listo
	// para atenderla, y una interfaz que arranque contra un pipe que todavía no
	// escucha enseñaría el cartel de "no hay servicio" justo al encender.
	if ui != nil {
		if err := ui.Start(mostrarUI); err != nil {
			// No aborta el arranque. Un daemon sin interfaz es un producto a
			// medias y sigue siendo mejor que ninguno: la sala que estuviera
			// abierta sigue en pie, y el usuario tiene un error que leer.
			log.Error("no se pudo lanzar la interfaz", "error", err)
		}
	}

	return &booted{wait: rt.Wait, shutdown: apagar}, nil
}

// dirDelBinario es donde vive el catálogo que trajo el instalador.
//
// Junto al ejecutable y no en el directorio de datos: ese archivo es del
// instalador, se actualiza con la app, y el daemon no lo escribe jamás. Ver
// [catalogstore.Store] para por qué son dos rutas y no una.
func dirDelBinario() string {
	exe, err := os.Executable()
	if err != nil {
		// Sin ruta del ejecutable el catálogo que vino con la app no se lee, y
		// el producto sigue funcionando con el del usuario. Se devuelve el
		// directorio actual, que es lo que hace que `go run` encuentre uno si
		// lo hay al lado.
		return "."
	}
	return filepath.Dir(exe)
}

type relojReal struct{}

func (relojReal) Now() time.Time { return time.Now() }
