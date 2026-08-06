//go:build windows

package uihost

import (
	"fmt"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
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

	const banderas = windows.CREATE_UNICODE_ENVIRONMENT | windows.CREATE_NEW_CONSOLE

	// Dos APIs para lo mismo, y la elección es de PRIVILEGIOS, no de gusto. Ver
	// [entrega.conToken]: SYSTEM tiene `SE_ASSIGNPRIMARYTOKEN` y un
	// administrador elevado no; lo que este último tiene es `SE_IMPERSONATE`.
	if quién.conToken {
		err = crearProcesoConToken(primario, exe, cmd, banderas, entorno, &si, &pi)
	} else {
		err = windows.CreateProcessAsUser(
			primario,
			exe,
			cmd,
			nil, nil,
			false, // sin heredar handles: no se pueden heredar entre sesiones
			banderas,
			entorno,
			nil,
			&si,
			&pi,
		)
	}
	if err != nil {
		return fmt.Errorf("lanzando la interfaz en la sesión %d: %w", sesión, err)
	}
	_ = windows.CloseHandle(pi.Thread)

	// Al job ANTES de cualquier otra cosa. Si el daemon muriera entre el
	// arranque y esta línea, quedaría una interfaz suelta.
	if err := windows.AssignProcessToJobObject(windows.Handle(h.job), pi.Process); err != nil {
		_ = windows.TerminateProcess(pi.Process, 1)
		_ = windows.CloseHandle(pi.Process)
		return fmt.Errorf("metiendo la interfaz en su job: %w", err)
	}

	if !persistent {
		// La transitoria solo tenía que avisarle a la que ya está. Su handle no
		// se guarda, y el job igual se la lleva si el daemon muere.
		_ = windows.CloseHandle(pi.Process)
		return nil
	}

	h.mu.Lock()
	anterior := h.proc
	h.proc = uintptr(pi.Process)
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

	caídas := 0
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

		// Una interfaz que vivió un rato y se cerró es un caso normal, así que
		// el contador vuelve a cero. Lo que se persigue son las caídas rápidas
		// en cadena, que es como se ve algo que no arranca.
		if time.Since(desde) > quickDeath {
			caídas = 0
		}
		caídas++
		if caídas > maxRelaunches {
			h.deps.Log.Error("la interfaz se cayó demasiadas veces seguidas y no se relanza más",
				"intentos", caídas)
			Warn("Kanpachi no consigue mantener su ventana abierta y va a cerrarse.\n\n" +
				"Se intentó abrirla varias veces seguidas y se cerró sola cada vez.")
			if h.deps.OnGiveUp != nil {
				h.deps.OnGiveUp()
			}
			return
		}

		h.deps.Log.Warn("la interfaz se cerró sin avisar, se relanza en silencio", "intento", caídas)
		// En silencio: la ventana la abrió el usuario o no, y relanzarla con
		// ventana pondría una encima de lo que estuviera haciendo. Lo que no
		// puede faltar es el icono.
		if err := h.launch(false, true); err != nil {
			h.deps.Log.Error("no se pudo relanzar la interfaz", "error", err)
			if h.deps.OnGiveUp != nil {
				h.deps.OnGiveUp()
			}
			return
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
func (h *Host) closeHandles() {
	h.mu.Lock()
	proc, job, wake := h.proc, h.job, h.wake
	h.proc, h.job, h.wake = 0, 0, 0
	h.mu.Unlock()

	if proc != 0 {
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
func avisarEnSesión(título, texto string) {
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
	var respuesta uint32
	// Timeout 0 with bWait FALSE = show and return. Waiting would park a
	// daemon goroutine on a dialog nobody may be there to close.
	_, _, _ = procWTSSendMessage.Call(
		0, // WTS_CURRENT_SERVER_HANDLE
		uintptr(sesión),
		uintptr(unsafe.Pointer(&t[0])), uintptr(len(t)*2),
		uintptr(unsafe.Pointer(&m[0])), uintptr(len(m)*2),
		uintptr(mbErrorEnFrente),
		0, // timeout, ignored when not waiting
		uintptr(unsafe.Pointer(&respuesta)),
		0, // bWait FALSE
	)
}
