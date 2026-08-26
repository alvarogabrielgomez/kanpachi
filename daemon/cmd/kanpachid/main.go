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
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/accentiostudios/kanpachi/core/port"
	kanpachiengine "github.com/accentiostudios/kanpachi/daemon/adapter/engine/kanpachi"
	"github.com/accentiostudios/kanpachi/daemon/adapter/identity"
	"github.com/accentiostudios/kanpachi/daemon/adapter/netcfg"
	"github.com/accentiostudios/kanpachi/daemon/adapter/router/igd"
	statestore "github.com/accentiostudios/kanpachi/daemon/adapter/state/jsonfile"
	"github.com/accentiostudios/kanpachi/daemon/adapter/uihost"
	"github.com/accentiostudios/kanpachi/daemon/service"
	"github.com/accentiostudios/kanpachi/daemon/transport/pipe"
	"github.com/accentiostudios/kanpachi/daemon/wiring"
	"github.com/accentiostudios/kanpachi/internal/layout"
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
	// Aparte de `--data` a propósito: el bundle portable manda los datos a una
	// carpeta temporal que borra al salir, y el log NO puede morir con ella. Ver
	// [carpetaDelLog].
	dirLog := flag.String("log", "", "carpeta del log. Vacío usa <datos>\\"+layout.LogDir)
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

	// El enlace que trajo el navegador. Ver [enlaceDe] para por qué se filtra
	// en vez de tomar el primer argumento suelto.
	enlace := enlaceDe(flag.Args())
	// Un enlace implica ventana aunque nadie haya escrito `--show`. Quien
	// pulsó un botón en su navegador está mirando: abrir Kanpachi en silencio
	// sería, desde donde él está, no haber hecho nada.
	if enlace != "" {
		*mostrar = true
	}

	if err := correr(*consola, *suelto, *mostrar, *datos, *dirLog, *nombre, enlace); err != nil {
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
	fw, _, cerrarFirewall, err := wiring.NewFirewall(datos, log, igd.New(log))
	if err != nil {
		return err
	}
	defer func() { _ = cerrarFirewall() }()

	motor, err := kanpachiengine.New(kanpachiengine.Deps{
		Exe: filepath.Join(dirDelBinario(), engineExe),
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
		// Qué lista de puertos cierra la cuarentena. Lo dice el cableado del
		// sistema, no un runtime.GOOS acá. Ver `wiring_linux.go`.
		Quarantine: wiring.Quarantine,
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
	// # Quién la borra depende de si el empaquetador se lleva el directorio
	//
	// En Windows no se lo lleva nadie, así que se borra acá y explícitamente:
	// darlo por hecho dejaría la llave viva en una máquina donde Kanpachi ya no
	// está.
	//
	// En Linux sí, y eso cambia quién manda. `dpkg` distingue dos cosas que este
	// código no distinguía: `apt remove` quita el programa y `apt purge` quita
	// además sus datos. Borrar la llave en el `remove` rompe esa promesa, y lo
	// que se pierde no es un fichero cualquiera: es lo que hace que esta máquina
	// siga siendo LA MISMA para todos los que ya jugaron con ella. Quitar y
	// reinstalar la convertiría en otra, sin que nadie lo hubiera pedido.
	//
	// Medido el 2026-08-10 con el paquete puesto: un `apt remove` se llevaba la
	// llave, contra lo que promete el propio README y contra lo que espera
	// cualquiera que use Debian.
	if !packageRemovesData {
		// Best effort porque desinstalar tiene que terminar: una llave que no se
		// pudo borrar es una molestia, y una desinstalación a medias es una
		// máquina con puertos bloqueados que nadie puede explicar.
		llave := filepath.Join(datos, identity.IdentityFile)
		if err := os.Remove(llave); err != nil && !os.IsNotExist(err) {
			log.Warn("no se pudo borrar la llave de esta instalación", "ruta", llave, "error", err)
		}
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
// `uiExeName`, la pareja de esto, la decide la VARIANTE y no el sistema: el
// nombre del ejecutable de la interfaz solo tiene sentido donde hay interfaz.
// Ver `variant.go`.
const uiShowFlag = "--show"

// uiResumeFlag le dice a la ventana que hay una sala reabriéndose ahora mismo,
// para que su primer fotograma sea la espera y no la portada.
//
// Es un CONTRATO con `ui/lib/main.dart`, que la escribe en su propia constante,
// igual que [uiShowFlag]. De los que no dan error al romperse: cambiar una sin
// la otra deja el hueco que esto vino a cerrar, sin que nada falle.
//
// **Falla hacia el lado callado**, por lo mismo que la de mostrar: sin ella la
// ventana arranca como arrancaba. Al revés sería una ventana esperando por una
// sala que nadie está abriendo.
const uiResumeFlag = "--resume-hosted-room"

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

// enlaceDe saca el `kanpachi://` de una lista de argumentos.
//
// **Filtra por el esquema en vez de tomar el primer suelto**, y esa es toda su
// razón de existir: este binario lo invoca Windows con `"%1"` desde el
// manejador de protocolo, o sea con lo que un sitio web cualquiera puso en un
// enlace. Aceptar cualquier argumento suelto convertiría una ruta de fichero o
// una bandera en algo que el daemon se pasa a sí mismo.
//
// Lo que pase este filtro sigue siendo entrada hostil: quien lo valida de
// verdad es [domain.ParseRoom], que rechaza rutas, argumentos y todo lo que no
// sea una de las seis formas.
func enlaceDe(args []string) string {
	const esquema = "kanpachi://"
	for _, a := range args {
		if len(a) > len(esquema) && strings.EqualFold(a[:len(esquema)], esquema) {
			return a
		}
	}
	return ""
}

func correr(consola, suelto, mostrar bool, datos, dirLog, nombre, enlace string) error {
	portable := esPortable()
	datos = dirDeDatos(datos)
	carpetaLog := carpetaDelLog(datos, dirLog)

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
			// El enlace viaja por los MISMOS argumentos del servicio, que es la
			// única vía que el SCM tiene de pasar algo. El lanzador lo pone ahí
			// cuando no hay daemon todavía: ver [abrir].
			b, err := arrancar(ctx, datos, carpetaLog, pipe.Name, false, tiene(args, ArgShow), enlaceDe(args))
			if err != nil {
				return nil, nil, err
			}
			return b.wait, b.shutdown, nil
		})
	}

	// **El daemon de una carpeta portable.** Mismo cableado que el servicio y
	// canal PROPIO; quién sostiene el proceso es nadie: vive hasta que lo
	// apaguen. El nombre distinto es lo que permite que portable e instalado
	// funcionen a la vez sin robarse lanzadores, tokens ni ventanas.
	//
	// Se exige el marcador y no basta con la bandera. Sin esa comprobación,
	// `kanpachid.exe --daemon` fuera de una carpeta portable tomaría datos y
	// ciclo de vida que no le pertenecen. El marcador decide juntos el pipe, el
	// token, los datos y la instancia de interfaz.
	if suelto {
		if !portable {
			return fmt.Errorf("--daemon solo vale en una carpeta portable, " +
				"y acá no está el fichero " + PortableMarker + ".\n" +
				"  Instalado, al daemon lo arranca el Administrador de servicios")
		}
		ctx, parar := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer parar()

		b, err := arrancar(ctx, datos, carpetaLog, pipe.PortableName, false, mostrar, enlace)
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
		return abrir(datos, mostrar, enlace)
	}

	if nombre == "" {
		nombre = pipe.ConsoleName
	}

	ctx, parar := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer parar()

	b, err := arrancar(ctx, datos, carpetaLog, nombre, consola, false, enlace)
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
func arrancar(ctx context.Context, datos, carpetaLog, nombre string, consola, mostrarUI bool, invitación string) (*booted, error) {
	// **Un servicio no tiene salida estándar.** En consola quien mira es una
	// persona con una terminal delante; como servicio, todo lo que se imprima se
	// pierde y un arranque fallido queda sin una sola línea que lo explique. Ver
	// [logArchivo].
	var log port.Logger = logConsola{}
	var cierres []func()
	if !consola {
		archivo := nuevoLogArchivo(carpetaLog)
		log = archivo
		// En un contenedor van los DOS. El fichero por lo de siempre, y stdout
		// porque `docker logs` y `kubectl logs` no leen otra cosa: sin él, un
		// host que lleva horas sin poder recibir a nadie se ve desde fuera como
		// un contenedor sano y mudo. Ver [logDoble].
		if enContenedor() {
			log = logDoble{a: logConsola{}, b: archivo}
		}
		// La salida de errores del proceso va al mismo archivo, y con ella la
		// traza de un pánico. Sin esto el daemon puede morirse sin dejar una
		// línea: ya pasó, y lo único que quedó fue el "terminated unexpectedly"
		// del registro de eventos de Windows. Ver [redirigirStderr].
		//
		// No se aborta si falla: se pierde la traza del pánico, no el daemon.
		if err := archivo.CapturarPánicos(); err != nil {
			log.Warn("no se pudo capturar la salida de errores, un pánico no dejaría traza", "error", err)
		}
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
	watch := wiring.NewWatchers(log)
	cierres = append(cierres, func() { _ = watch.Events.Close() })

	// **Un binario con provisionales NO se instala como servicio.** El riesgo
	// real nunca fue que fallen: es que uno con un firewall que dice que purgó
	// termine corriendo en la máquina de alguien.
	//
	// La lista se le pregunta al CABLEADO y no a una constante: ver
	// [sinimplementar.Provisional]. Un provisional que vuelva se delata solo, y
	// el error lo nombra en vez de decir que hay algo. Va ANTES del firewall,
	// que es lo primero con efectos de verdad.
	if missing := watch.Stubbed(); len(missing) > 0 && !consola {
		return abortar(fmt.Errorf("este binario lleva adaptadores provisionales dentro (%s), "+
			"así que solo arranca con --console.\n"+
			"  Un provisional que devuelve éxito hace la cuarentena inverificable, y eso "+
			"instalado es peor que no tener daemon", strings.Join(missing, ", ")))
	}

	// El host del proceso: lanzar la interfaz, apagar todo, y el arranque con
	// Windows. Se construye acá porque el listener del pipe lo necesita, y su
	// `apagar` se une al final, cuando existe.
	host := &procesoHost{log: log, motorRuta: filepath.Join(dirDelBinario(), engineExe)}
	// El enlace que levantó a este daemon queda puesto ANTES de que la interfaz
	// exista. Es el arranque en frío del `kanpachi://`: no había daemon, así que
	// el enlace vino por los argumentos en vez de por el pipe, y la interfaz que
	// se lanza unas líneas más abajo lo va a encontrar ya en el buzón.
	host.setInvitación(invitación)

	// La interfaz NO se hospeda en modo consola, y esa es toda la diferencia.
	// El modo consola es para desarrollar: quien lo usa ya tiene una terminal
	// delante y arranca la interfaz cuando quiere. Levantarle una ventana en
	// cada `--console` sería una molestia, y peor, taparía el caso que el
	// producto de verdad tiene que resolver, que es el daemon lanzándola él.
	//
	// # Y tampoco donde no hay ventana que hospedar
	//
	// [hostsUI] es falso en la variante headless, donde el cliente es un CLI que
	// arranca el usuario y no algo que el daemon lance en la sesión de nadie.
	// Sin esa condición, el arranque como servicio moría con `uihost: lanzar la
	// interfaz en la sesión del usuario es de Windows`, y solo como servicio:
	// en `--console` esta rama no se pisa, así que el daemon parecía sano.
	// Medido con el paquete instalado, el 2026-08-10.
	//
	// Lo decide la VARIANTE y no el sistema, que son dos ejes distintos: ver
	// `variant.go`.
	// La sesión nace bastante más abajo y el lanzador de la ventana se arma acá,
	// así que la pregunta viaja en un hueco que se rellena después. Es el mismo
	// patrón que `host.apagar` unas líneas más abajo, y por el mismo motivo: la
	// dependencia es circular por naturaleza.
	//
	// Nulo mientras no haya sesión, y eso ya es la respuesta correcta: sin
	// sesión no hay sala que reabrir.
	var salaReabriendo func() bool

	var ui *uihost.Host
	if !consola && hostsUI {
		var err error
		// La hoja donde la interfaz deja su log, preparada ANTES de lanzarla.
		//
		// Un fallo acá no tumba el arranque: lo que se pierde es el registro de la
		// ventana, y lo que costaría negarse es la ventana entera, con el icono de
		// la bandeja y la única forma de cerrar una sala. Se dice y se sigue.
		carpetaLogUI := carpetaDelLogDeLaVentana(carpetaLog, esPortable())
		if err := prepararCarpetaDeLogDeLaUI(carpetaLogUI); err != nil {
			log.Warn("la interfaz se va a quedar sin log", "error", err)
		}

		ui, err = uihost.New(uihost.Deps{
			// Junto a este binario, y de `os.Executable()`. **Nunca del estado,
			// de la configuración ni del pipe**: esto corre como SYSTEM, y una
			// ruta que alguien pueda influir es escalada de privilegios.
			Exe:      filepath.Join(dirDelBinario(), uiExeName),
			ShowFlag: uiShowFlag,
			// Para que el primer fotograma de la ventana sea la espera y no la
			// portada cuando hay sala reabriéndose. Ver [uiResumeFlag].
			ResumeFlag: uiResumeFlag,
			Resuming: func() bool {
				return salaReabriendo != nil && salaReabriendo()
			},
			// Su propia hoja dentro de la carpeta de logs, o la carpeta a
			// secas en una copia portable. La interfaz se cae sola y hasta
			// hace poco no dejaba nada; ver [carpetaDelLogDeLaVentana] para
			// por qué son dos carpetas instalado y una en portable, y
			// [uihost.Deps.LogDir] para por qué se le dice en vez de dejar
			// que la deduzca.
			LogDir: carpetaLogUI,
			// Y dónde están los datos de verdad, que es de donde sale el
			// token. Ver [uihost.Deps.DataDir]: en el bundle portable la
			// interfaz lo deducía del temporal donde vive su ejecutable, y
			// el daemon ya no escribe ahí.
			DataDir: datos,
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

	// Todo lo que un binario de adaptadores reales construye igual, construido
	// UNA vez. El orden de dentro es el que importa: el firewall primero porque
	// `NewSession` purga en su constructor, y `Attach` cierra la dependencia
	// circular que ya estuvo escrita, probada y llamada por nadie. Ver
	// [wiring.BuildSession]; lo que difiere entre binarios va en los parámetros,
	// y el cero de cada opcional es lo que hace el producto.
	built, err := wiring.BuildSession(ctx, wiring.SessionParams{
		DataDir:           datos,
		LogDir:            carpetaLog,
		EngineExe:         filepath.Join(dirDelBinario(), engineExe),
		BuiltinCatalogDir: builtinCatalogDir(),
		Protect:           protegerFichero,
		OnEngineFatal:     fatalDeMáquina(ui, host, log),
		CheckMachine:      true,
		// Explícito por variable de entorno, y no adivinado por `/.dockerenv`
		// ni por cgroups: de esta bandera cuelga que Kanpachi reescriba
		// destinos, así que tiene que poder leerse en un diff. La pone el
		// entrypoint de la imagen, que es quien sabe para qué existe ese
		// contenedor. Ver [domain.RedirectSpec].
		Container: enContenedor(),
		Watchers:  watch,
		Log:       log,
	})
	cierres = append(cierres, built.Closers...)
	if err != nil {
		return abortar(err)
	}
	sesion, bucle := built.Session, built.Supervisor
	log.Info("llave de identidad de esta máquina", "huella", built.Fingerprint)

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
		Clock: wiring.RealClock{},
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
		// Se nombran los DOS, y cuál primero importa: para pedir algo que el
		// producto ya sabe hacer, el cliente, que valida y pinta. `pipeprobe` es
		// la sonda del canal, y lo que la justifica es lo que el cliente no puede
		// expresar: un método por su nombre de cable, un JSON crudo, un token que
		// no vale. Cuando esta pista nombraba solo a la sonda, se leía como si el
		// canal se probara con ella y punto.
		fmt.Printf("kanpachid en modo consola\n  pipe:  %s\n  token: %s\n  datos: %s\n\n"+
			"Ctrl+C para salir. Prueba con:\n"+
			"  go run ./daemon/cmd/kanpachi -data %q -pipe %q status\n"+
			"  go run ./internal/pipeprobe  -data %q -pipe %q -no-token status   (debe fallar)\n\n",
			nombre, token, datos, datos, nombre, datos, nombre)
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

	// **Esperar es esperar al apagado ENTERO, y no solo a que la puerta cierre.**
	//
	// `rt.Wait` termina cuando termina `Serve`, y lo primero que hace el apagado
	// es cerrar la entrada, así que `Wait` devuelve con el apagado a medias. El
	// proceso salía por ahí mientras el otro hilo todavía estaba saliendo de la
	// sala, cerrando el motor y borrando el token, y quien ganara la carrera
	// decidía qué quedaba puesto.
	//
	// Medido en Linux el 2026-08-10 con el mismo binario y el mismo SIGTERM: dos
	// ejecuciones, una dejó `api.token` en disco y la otra no, y la que lo dejó
	// cortó el log en `apagando el daemon` sin llegar a `el daemon se apagó`. El
	// fichero era lo visible; lo que estaba en juego eran las reglas de la sala,
	// los ajustes de red y el proceso del motor.
	//
	// El arreglo es llamar al apagado ACÁ también. Es idempotente por
	// [sync.Once], y esa es la parte que lo hace funcionar: `Do` no devuelve
	// hasta que la función termina, así que si el apagado ya venía corriendo
	// desde la señal, esta llamada se queda esperándolo en vez de duplicarlo.
	//
	// De paso arregla el otro camino, que nadie cubría: si la entrada se cae
	// sola, antes se salía sin cerrar nada.
	esperar := func() error {
		err := rt.Wait()
		apagar()
		return err
	}
	return &booted{wait: esperar, shutdown: apagar}, nil
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
