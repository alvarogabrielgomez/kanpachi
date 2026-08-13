//go:build windows

package uihost

import (
	"fmt"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// enAlgúnJob dice si un proceso ya pertenece a algún job.
//
// Es para el diagnóstico de `AssignProcessToJobObject`, que devuelve el mismo
// `Access is denied` cuando el proceso ya está en un job que no admite anidar y
// cuando el problema es otro. `IsProcessInJob` no está en `x/sys/windows`, así
// que se llama a mano.
func enAlgúnJob(proc windows.Handle) string {
	var dentro int32
	r, _, err := procIsProcessInJob.Call(uintptr(proc), 0, uintptr(unsafe.Pointer(&dentro)))
	if r == 0 {
		return "no se pudo preguntar: " + err.Error()
	}
	if dentro != 0 {
		return "sí"
	}
	return "no"
}

var (
	kernel32           = windows.NewLazySystemDLL("kernel32.dll")
	procIsProcessInJob = kernel32.NewProc("IsProcessInJob")
)

// openJob abre el Job Object que sujeta a la interfaz.
//
// `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE` es lo ÚNICO que garantiza que la
// interfaz muera con el daemon pase lo que pase, incluido un
// `TerminateProcess` desde el Administrador de tareas, que no ejecuta ni una
// línea de código de cierre. Es el mismo mecanismo que ya sujeta al motor.
func (h *Host) openJob() error {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return fmt.Errorf("abriendo el job de la interfaz: %w", err)
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		_ = windows.CloseHandle(job)
		return fmt.Errorf("poniéndole el límite al job de la interfaz: %w", err)
	}

	// Manual, porque lo cierra `Close` una vez y tiene que quedarse levantado
	// hasta que el vigilante lo vea.
	wake, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		_ = windows.CloseHandle(job)
		return fmt.Errorf("creando el evento de parada del vigilante: %w", err)
	}

	h.job = uintptr(job)
	h.wake = uintptr(wake)
	return nil
}

// launch arranca la interfaz en la sesión del usuario y SIN elevar.
//
// # El baile de tokens, y por qué cada paso está
//
// El daemon es SYSTEM en la sesión 0. Un `CreateProcess` normal daría una
// ventana de Flutter corriendo como administrador, que es superficie de ataque
// regalada y además rompe cosas visibles: arrastrar un fichero desde el
// Explorador a una ventana elevada lo bloquea UIPI.
//
//  1. `WTSGetActiveConsoleSessionId` dice en qué sesión está quien usa la
//     máquina. Sin esto el proceso nacería en la 0, donde nadie lo ve.
//  2. `WTSQueryUserToken` da el token de esa persona. **Exige LocalSystem y
//     `SE_TCB_NAME`**, textual en la documentación: es otro motivo por el que
//     el daemon es un servicio como SYSTEM y no un proceso de usuario elevado.
//  3. Si ese token viene ELEVADO, se baja al gemelo. Windows fabrica dos
//     tokens al iniciar sesión un administrador, y el limitado es el que corre
//     el Explorador. La documentación NO dice cuál de los dos entrega
//     `WTSQueryUserToken`, así que no se da por sabido: se mira y se normaliza.
//     Con UAC apagado no hay gemelo, y ahí no hay nada que bajar porque el
//     Explorador también corre en integridad alta.
//  4. `DuplicateTokenEx` a token PRIMARIO, que es lo que
//     `CreateProcessAsUser` acepta.
//  5. `CreateEnvironmentBlock` para que la interfaz tenga las variables de esa
//     persona. Sin esto hereda las de SYSTEM, o sea `APPDATA` apuntando al
//     perfil de SYSTEM, y Flutter guardaría ahí sus preferencias.
//  6. `lpDesktop` en `winsta0\default`, que es lo que la documentación exige
//     para que el proceso sea interactivo. Por omisión nacería en una estación
//     de ventanas no interactiva, o sea invisible y sorda.
func (h *Host) launch(show, persistent bool) error {
	quién, err := tokenDelUsuario(h.deps.Log)
	if err != nil {
		return err
	}
	tok, sesión := quién.tok, quién.sesión
	defer func() { _ = tok.Close() }()

	// El paso 3. `GetLinkedToken` falla cuando no hay gemelo, que es
	// exactamente el caso de UAC apagado, y ahí se sigue con el que hay.
	//
	// Es también el paso que hace todo el trabajo por el camino portable: ahí
	// el token de partida es el de ESTE proceso, que está elevado, y bajar al
	// gemelo limitado es justo lo que hay que hacer.
	if tok.IsElevated() {
		if limitado, err := tok.GetLinkedToken(); err == nil {
			_ = tok.Close()
			tok = limitado
			h.deps.Log.Info("la interfaz se lanza con el token sin elevar del usuario")
		} else {
			h.deps.Log.Warn("el token del usuario viene elevado y no tiene gemelo, "+
				"así que la interfaz va a correr elevada", "error", err)
		}
	}

	var primario windows.Token
	if err := windows.DuplicateTokenEx(
		tok,
		accesoQueHaceFalta,
		nil,
		windows.SecurityImpersonation,
		windows.TokenPrimary,
		&primario,
	); err != nil {
		return fmt.Errorf("duplicando el token a primario: %w", err)
	}
	defer func() { _ = primario.Close() }()

	var entorno *uint16
	if err := windows.CreateEnvironmentBlock(&entorno, primario, false); err != nil {
		return fmt.Errorf("armando el entorno del usuario: %w", err)
	}
	defer func() { _ = windows.DestroyEnvironmentBlock(entorno) }()

	// La línea de comandos. El ejecutable va ENTRECOMILLADO aunque también se
	// pase por separado: la documentación avisa de que un `C:\Program
	// Files\...` sin comillas puede acabar ejecutando `C:\Program.exe`.
	línea := `"` + h.deps.Exe + `"`
	if show {
		línea += " " + h.deps.ShowFlag
	}
	// Solo si HAY reapertura en marcha, preguntado ahora y no recordado. Ver
	// [Deps.Resuming] para por qué es una función.
	if h.deps.ResumeFlag != "" && h.deps.Resuming != nil && h.deps.Resuming() {
		línea += " " + h.deps.ResumeFlag
	}
	// La carpeta del log, entrecomillada por lo mismo que el ejecutable: el
	// camino portable la deja en la carpeta desde donde alguien abrió el
	// bundle, que perfectamente puede ser `C:\Users\...\Mis descargas`. Ver
	// [Deps.LogDir] para por qué se dice en vez de deducirse.
	if h.deps.LogDir != "" {
		línea += ` --log "` + h.deps.LogDir + `"`
	}

	exe, err := windows.UTF16PtrFromString(h.deps.Exe)
	if err != nil {
		return err
	}
	cmd, err := windows.UTF16PtrFromString(línea)
	if err != nil {
		return err
	}
	escritorio, err := windows.UTF16PtrFromString(`winsta0\default`)
	if err != nil {
		return err
	}

	si := windows.StartupInfo{Desktop: escritorio}
	si.Cb = uint32(unsafe.Sizeof(si))
	var pi windows.ProcessInformation

	// **SUSPENDIDO, y eso arregla una carrera medida.**
	//
	// Sin esto el proceso arranca corriendo, y la interfaz hace lo primero de
	// todo su comprobación de instancia única: si ya hay una, le avisa y se
	// mata. Un proceso que ya terminó no se puede meter en un job, y lo que
	// Windows devuelve entonces es `Access is denied`. Pasó al relanzar la
	// interfaz un instante después de matarla: `metiendo la interfaz en su job:
	// Access is denied.`, y con la lógica de entonces eso apagaba Kanpachi
	// entero.
	//
	// Suspendido, además, es lo que hace CIERTO el comentario de abajo: el job
	// queda puesto antes de que el proceso ejecute una sola instrucción, así
	// que no existe ventana en la que una interfaz corra fuera de él.
	//
	// **Y con BREAKAWAY, que arregla lo de arriba en su raíz.** Un proceso hijo
	// nace dentro del job de su padre, y a un proceso que YA está en un job no
	// se le puede meter en el nuestro: `AssignProcessToJobObject` contesta
	// `Access is denied`. Medido, con `IsProcessInJob` diciendo que sí antes de
	// intentarlo. Pasa cuando al daemon lo levanta algo que vive en un job, que
	// es el caso de la carpeta portable: lo arranca una consola.
	//
	// La bandera es una PETICIÓN: si el job del padre no permite salirse,
	// `CreateProcess` falla, y entonces se reintenta sin ella. Ver [crear].
	const banderas = windows.CREATE_UNICODE_ENVIRONMENT |
		windows.CREATE_NEW_CONSOLE |
		windows.CREATE_SUSPENDED

	// Dos APIs para lo mismo, y la elección es de PRIVILEGIOS, no de gusto. Ver
	// [entrega.conToken]: SYSTEM tiene `SE_ASSIGNPRIMARYTOKEN` y un
	// administrador elevado no; lo que este último tiene es `SE_IMPERSONATE`.
	crear := func(f uint32) error {
		if quién.conToken {
			return crearProcesoConToken(primario, exe, cmd, f, entorno, &si, &pi)
		}
		return windows.CreateProcessAsUser(
			primario,
			exe,
			cmd,
			nil, nil,
			false, // sin heredar handles: no se pueden heredar entre sesiones
			f,
			entorno,
			nil,
			&si,
			&pi,
		)
	}

	if err = crear(banderas | windows.CREATE_BREAKAWAY_FROM_JOB); err != nil {
		// El job del padre no deja salirse. Se va sin la bandera: el proceso
		// nace dentro de ese job, y eso NO rompe la invariante —muere con el
		// daemon igual, porque el job del padre se lo lleva—, solo impide
		// meterlo además en el nuestro. Lo de abajo lo tolera.
		h.deps.Log.Info("el job del padre no deja salirse, la interfaz nace dentro de él",
			"error", err)
		if err = crear(banderas); err != nil {
			return fmt.Errorf("lanzando la interfaz en la sesión %d: %w", sesión, err)
		}
	}

	// Al job ANTES de cualquier otra cosa, y con el proceso todavía sin
	// ejecutar nada. Si el daemon muriera entre el arranque y esta línea,
	// quedaría una interfaz suelta.
	heldByJob := true
	if err := windows.AssignProcessToJobObject(windows.Handle(h.job), pi.Process); err != nil {
		// **No se mata la interfaz por esto.** Antes sí, y el precio era
		// absurdo: la ventana no arrancaba, el vigilante lo contaba como caída,
		// y a la tercera Kanpachi se apagaba entero, con la sala dentro. Todo
		// por no poder meter en un job un proceso que YA está en uno.
		//
		// **Lo que NO se puede dar por sentado es que el job del padre supla al
		// nuestro.** Esto decía que sí, con el argumento de que ese job es el
		// de este daemon. Medido el 2026-08-09 con el bundle portable, es
		// falso: `internal/kanpachibundle` no crea ningún job, así que el que
		// hay es ambiente, lo puso Windows al elevar, y nadie controla lo que
		// pasa al cerrarlo. El daemon terminaba, el bundle borraba su carpeta
		// temporal, y `kanpachiui.exe` seguía corriendo desde una carpeta que
		// ya no existía. Por eso se anota, y el cierre la mata a mano.
		//
		// El diagnóstico va dentro del aviso porque hace falta: `Access is
		// denied` a secas tiene tres causas con arreglos distintos —el proceso
		// ya murió, ya está en otro job, o al job le falta el permiso— y sin
		// estos datos no se distinguen. Costó una sesión mirando el sitio
		// equivocado.
		heldByJob = false
		var salida uint32 = 0xFFFFFFFF
		_ = windows.GetExitCodeProcess(pi.Process, &salida)
		h.deps.Log.Warn("la interfaz no entró en el job propio, se la mata a mano al cerrar",
			"error", err, "pid", pi.ProcessId,
			"ya estaba en un job", enAlgúnJob(pi.Process),
			"código de salida", fmt.Sprintf("0x%X", salida))
	}

	// Y ahora sí, a correr. El hilo se suelta y su handle se cierra: lo que se
	// vigila es el PROCESO.
	if _, err := windows.ResumeThread(pi.Thread); err != nil {
		_ = windows.TerminateProcess(pi.Process, 1)
		_ = windows.CloseHandle(pi.Thread)
		_ = windows.CloseHandle(pi.Process)
		return fmt.Errorf("soltando el hilo de la interfaz: %w", err)
	}
	_ = windows.CloseHandle(pi.Thread)

	if !persistent {
		// La transitoria solo tenía que avisarle a la que ya está. Su handle no
		// se guarda, y el job igual se la lleva si el daemon muere.
		_ = windows.CloseHandle(pi.Process)
		return nil
	}

	h.mu.Lock()
	anterior := h.proc
	h.proc = uintptr(pi.Process)
	h.heldByJob = heldByJob
	h.mu.Unlock()
	if anterior != 0 {
		_ = windows.CloseHandle(windows.Handle(anterior))
	}

	h.deps.Log.Info("interfaz lanzada", "pid", pi.ProcessId, "sesión", sesión, "con ventana", show)
	return nil
}

// watch espera a que la interfaz muera y la relanza, con tope.
func (h *Host) watch() {
	defer h.watching.Done()

	var caídas relanzador
	for {
		h.mu.Lock()
		proc := h.proc
		h.mu.Unlock()
		if proc == 0 {
			return
		}

		desde := time.Now()
		handles := []windows.Handle{windows.Handle(proc), windows.Handle(h.wake)}
		cuál, err := windows.WaitForMultipleObjects(handles, false, windows.INFINITE)
		if err != nil || cuál != windows.WAIT_OBJECT_0 {
			// El evento de parada, o algo que no se entiende. En los dos casos
			// se deja de vigilar: `Close` se encarga del resto.
			return
		}

		select {
		case <-h.stop:
			return
		default:
		}

		// CÓMO murió, no solo que murió.
		//
		// Sin esto, "se cerró sin avisar" cubre dos cosas opuestas: alguien que
		// cerró la ventana, que es un `0`, y un crash nativo, que es un
		// `0xC0000005` y familia. Son el mismo texto en el log y arreglos
		// distintos, y la interfaz se cayó ocho veces entre dos máquinas el
		// 2026-08-08 sin que quedara registrado de cuál se trataba.
		//
		// Es el mismo patrón que ya usa el diagnóstico de `AssignProcessToJob`
		// unas líneas más arriba. Se lee ACÁ, con el proceso ya terminado, así
		// que `STILL_ACTIVE` no puede salir.
		vivió := time.Since(desde)
		var salida uint32 = 0xFFFFFFFF
		_ = windows.GetExitCodeProcess(windows.Handle(proc), &salida)

		// Una interfaz que vivió un rato y se cerró es un caso normal, así que
		// el contador vuelve a cero. Lo que se persigue son las caídas rápidas
		// en cadena, que es como se ve algo que no arranca. Ver [relanzador].
		intento, rendirse := caídas.murió(vivió)
		if rendirse {
			h.deps.Log.Error("la interfaz se cayó demasiadas veces seguidas y no se relanza más",
				"intentos", intento, "código de salida", fmt.Sprintf("0x%X", salida),
				"vivió", vivió.Round(time.Millisecond).String())
			// `Stop` y no `Warn`: esto ANUNCIA el apagado que viene en la línea
			// siguiente, así que espera a que alguien lo lea. Con `Warn`, que no
			// espera, el cuadro aparecía en el mismo instante en que Kanpachi
			// desaparecía, que se lee como que reventó y no como una
			// explicación. La espera es además el único rato que le queda a una
			// partida en curso.
			//
			// **Cuatro y no tres**, que es lo que decía este texto: el tope es
			// `maxRelaunches = 3` y la comparación es `seguidas > maxRelaunches`,
			// o sea que se rinde en la CUARTA caída. Lo confirma el nombre del
			// test que fija la regla, `TestCuatroCaídasRápidasApaganElDaemon`.
			Stop("Kanpachi va a cerrarse porque no consigue mantener su ventana abierta.\n\n" +
				"Se abrió y se cerró sola cuatro veces seguidas, así que se deja de " +
				"intentar. Al cerrarse, Kanpachi cierra también la sala y todo lo " +
				"que hubiera abierto en el firewall.\n\n" +
				"Qué hacer: vuelve a abrirlo desde su acceso directo.")
			if h.deps.OnGiveUp != nil {
				h.deps.OnGiveUp()
			}
			return
		}

		h.deps.Log.Warn("la interfaz se cerró sin avisar, se relanza en silencio",
			"intento", intento, "código de salida", fmt.Sprintf("0x%X", salida),
			"vivió", vivió.Round(time.Millisecond).String())

		// Un respiro antes de relanzar. La interfaz que acaba de morir puede
		// tener cosas suyas todavía sin soltar —el evento de instancia única
		// entre ellas—, y volver a arrancar en el mismo instante es pedirle a
		// la nueva que se tope consigo misma.
		select {
		case <-h.stop:
			return
		case <-time.After(relaunchGrace):
		}

		// En silencio: la ventana la abrió el usuario o no, y relanzarla con
		// ventana pondría una encima de lo que estuviera haciendo. Lo que no
		// puede faltar es el icono.
		if err := h.launch(false, true); err != nil {
			// **Se vuelve a intentar, no se tira todo.**
			//
			// Esto rendía en el primer fallo, y lo que rendía era el daemon
			// ENTERO: `OnGiveUp` apaga Kanpachi, o sea cierra la sala, baja la
			// red virtual y suelta el firewall. Un tropiezo al relanzar una
			// ventana no puede costar la partida de cuatro personas. El único
			// motivo legítimo para rendirse es el mismo que para una caída: que
			// pase una y otra vez, y de eso ya se ocupa el contador de arriba.
			h.deps.Log.Error("no se pudo relanzar la interfaz, se reintenta",
				"error", err, "intento", intento)
			continue
		}
	}
}

// signalWake despierta al vigilante sin cerrarle nada debajo.
func (h *Host) signalWake() {
	if h.wake != 0 {
		_ = windows.SetEvent(windows.Handle(h.wake))
	}
}

// closeHandles suelta el job, y con él se va la interfaz.
//
// # El caso en que el job no alcanza
//
// Cerrar el job mata a lo que el job sujeta, y la interfaz puede no estar
// dentro: ver el aviso de `AssignProcessToJobObject` en [Host.launch]. Ahí hay
// que matarla explícitamente, y esto es lo único que la mata.
//
// `TerminateProcess` y no un cierre pedido por las buenas, por lo mismo que el
// job usa `KILL_ON_JOB_CLOSE`: este camino corre en el apagado y no puede
// esperar a que una ventana conteste. Al proceso de la interfaz no le queda
// nada que guardar, sus preferencias se escriben cuando cambian.
func (h *Host) closeHandles() {
	h.mu.Lock()
	proc, job, wake, heldByJob := h.proc, h.job, h.wake, h.heldByJob
	h.proc, h.job, h.wake, h.heldByJob = 0, 0, 0, false
	h.mu.Unlock()

	if proc != 0 {
		if !heldByJob {
			_ = windows.TerminateProcess(windows.Handle(proc), 0)
		}
		_ = windows.CloseHandle(windows.Handle(proc))
	}
	// El job AL FINAL: cerrarlo es lo que mata a la interfaz.
	if job != 0 {
		_ = windows.CloseHandle(windows.Handle(job))
	}
	if wake != 0 {
		_ = windows.CloseHandle(windows.Handle(wake))
	}
}

// procWTSSendMessage shows message box IN the user session.
//
// Daemon lives in session 0: no desktop, no notification area. A plain
// MessageBox from here draws on a window station nobody watches, so it waits
// forever for an OK nobody can click. WTSSendMessageW is the documented way a
// service talks to whoever is at the machine.
//
// Used only when interface CANNOT be launched. Without it, that failure is
// total silence: no tray, no window, no error. Kanpachi running and nothing on
// screen is exactly the shape the invariant forbids.
var procWTSSendMessage = windows.NewLazySystemDLL("wtsapi32.dll").NewProc("WTSSendMessageW")

// Style flags. MB_OK | MB_ICONERROR | MB_SETFOREGROUND.
const mbErrorEnFrente = 0x0 | 0x10 | 0x10000

// avisarEnSesión pops the message on the user desktop. Best effort.
func avisarEnSesión(título, texto string) { mostrarEnSesión(título, texto, 0) }

// esperaDelAviso es how long a waiting message box is given before it gives up.
//
// Bounded rather than infinite because the reason NOT to wait is real: nobody
// may be at the machine. Five minutes is long enough for somebody who is there
// and short enough that a goroutine does not outlive the reason it exists.
// WTSSendMessageW returns IDTIMEOUT on expiry, which reads the same as a
// dismissal to the only caller that waits.
const esperaDelAviso = 300

// avisarYEsperarEnSesión is the same box, and it does NOT return until somebody
// presses OK or [esperaDelAviso] runs out.
//
// Separate from [avisarEnSesión] because the two callers want opposite things
// and the difference is not a detail. The caller that cannot show the interface
// wants to say so and get out of the way. The caller that found this MACHINE
// unable to build a virtual adapter wants the person to read it BEFORE Kanpachi
// goes away, because Kanpachi going away is the next thing that happens.
func avisarYEsperarEnSesión(título, texto string) {
	mostrarEnSesión(título, texto, esperaDelAviso)
}

// mostrarEnSesión is the call itself. `espera` in seconds, zero meaning do not
// wait at all.
func mostrarEnSesión(título, texto string, espera uint32) {
	sesión := windows.WTSGetActiveConsoleSessionId()
	if sesión == 0xFFFFFFFF {
		return
	}
	t, err := windows.UTF16FromString(título)
	if err != nil {
		return
	}
	m, err := windows.UTF16FromString(texto)
	if err != nil {
		return
	}
	var espera32 uintptr
	var esperar uintptr
	if espera > 0 {
		espera32 = uintptr(espera)
		esperar = 1
	}
	var respuesta uint32
	_, _, _ = procWTSSendMessage.Call(
		0, // WTS_CURRENT_SERVER_HANDLE
		uintptr(sesión),
		uintptr(unsafe.Pointer(&t[0])), uintptr(len(t)*2),
		uintptr(unsafe.Pointer(&m[0])), uintptr(len(m)*2),
		uintptr(mbErrorEnFrente),
		espera32,
		uintptr(unsafe.Pointer(&respuesta)),
		esperar,
	)
}
