//go:build windows

package sysevents

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	wevtapi          = windows.NewLazySystemDLL("wevtapi.dll")
	procEvtSubscribe = wevtapi.NewProc("EvtSubscribe")
	procEvtNext      = wevtapi.NewProc("EvtNext")
	procEvtClose     = wevtapi.NewProc("EvtClose")

	iphlpapi             = windows.NewLazySystemDLL("iphlpapi.dll")
	procNotifyAddrChange = iphlpapi.NewProc("NotifyAddrChange")

	powrprof                 = windows.NewLazySystemDLL("powrprof.dll")
	procPowerRegisterSuspend = powrprof.NewProc("PowerRegisterSuspendResumeNotification")
	procPowerUnregister      = powrprof.NewProc("PowerUnregisterSuspendResumeNotification")
)

// El canal y la consulta del visor de eventos.
//
// El **10000** es el que importa: es el que Windows escribe cuando IDENTIFICA
// una red, y es justo el instante en que revierte la métrica del adaptador, su
// categoría y las rutas. `docs/04` lo nombra por número por este motivo.
const (
	networkProfileChannel = `Microsoft-Windows-NetworkProfile/Operational`
	queryID10000          = `*[System[EventID=10000]]`

	evtSubscribeToFutureEvents = 1
)

// subscribe abre las tres. Cada una es independiente: que una falle no impide
// las otras dos, porque son tres avisos distintos y perder uno no es perderlos
// todos.
func (e *Events) subscribe() {
	if err := e.watchNetworkProfile(); err != nil {
		e.log.Warn("sin notice de identificación de red, se depende del latido", "error", err)
	}
	if err := e.watchAddrChange(); err != nil {
		e.log.Warn("sin notice de cambio de direcciones, se depende del latido", "error", err)
	}
	if err := e.watchPower(); err != nil {
		e.log.Warn("sin notice de reanudación, se depende del latido", "error", err)
	}
}

// watchNetworkProfile se suscribe al Event ID 10000.
//
// Con handle de evento y NO con callback. `EvtSubscribe` admite las dos formas,
// y la de evento deja el bucle del lado de Go: se espera, se drena, se vuelve a
// esperar. Con callback habría que meter una función Go en un hilo del sistema
// para no hacer nada más que avisar por un canal, que es más maquinaria para el
// mismo resultado.
func (e *Events) watchNetworkProfile() error {
	// Automático y NO manual. El servicio lo levanta cada vez que hay eventos
	// nuevos, y uno manual se quedaría levantado para siempre convirtiendo la
	// espera en un bucle que gira sin parar.
	ready, err := windows.CreateEvent(nil, 0 /*auto-reset*/, 0 /*sin señalar*/, nil)
	if err != nil {
		return fmt.Errorf("creando el evento de la suscripción: %w", err)
	}

	channel, err := windows.UTF16PtrFromString(networkProfileChannel)
	if err != nil {
		windows.CloseHandle(ready)
		return err
	}
	query, err := windows.UTF16PtrFromString(queryID10000)
	if err != nil {
		windows.CloseHandle(ready)
		return err
	}

	sus, _, errno := procEvtSubscribe.Call(
		0, // sin sesión: el visor de esta máquina
		uintptr(ready),
		uintptr(unsafe.Pointer(channel)),
		uintptr(unsafe.Pointer(query)),
		0, // sin marcador: no interesa lo de antes de arrancar
		0, // sin contexto
		0, // sin callback, que es lo que hace válido el handle de arriba
		evtSubscribeToFutureEvents,
	)
	if sus == 0 {
		windows.CloseHandle(ready)
		return fmt.Errorf("suscribiendo a %s: %w", networkProfileChannel, errno)
	}

	quit, err := windows.CreateEvent(nil, 1 /*manual*/, 0, nil)
	if err != nil {
		procEvtClose.Call(sus)
		windows.CloseHandle(ready)
		return err
	}

	e.undo = append(e.undo, func() {
		procEvtClose.Call(sus)
		windows.CloseHandle(ready)
		windows.CloseHandle(quit)
	})

	e.waiting.Add(1)
	go func() {
		defer e.waiting.Done()
		// El puente entre cerrar `stop`, que es de Go, y despertar una espera
		// del kernel, que no sabe nada de canales.
		go func() {
			<-e.stop
			windows.SetEvent(quit)
		}()

		handles := []windows.Handle{ready, quit}
		for {
			which, err := windows.WaitForMultipleObjects(handles, false, windows.INFINITE)
			if err != nil || which != windows.WAIT_OBJECT_0 {
				return
			}
			// Drenar es OBLIGATORIO aunque no se mire ni un evento: mientras
			// queden sin leer, el servicio no vuelve a levantar la señal, y la
			// suscripción se queda muda sin cerrarse.
			drain(sus)
			signal(e.identified)
		}
	}()
	return nil
}

// drain vacía la cola de la suscripción y cierra cada evento leído.
//
// El contenido no se mira: la consulta ya filtró por ID, así que la sola
// llegada de uno ES el aviso. Leer el XML costaría dos llamadas más por evento
// para saber lo que ya se sabe.
func drain(sus uintptr) {
	var handles [16]uintptr
	for rounds := 0; rounds < 64; rounds++ {
		var returned uint32
		ok, _, _ := procEvtNext.Call(
			sus,
			uintptr(len(handles)),
			uintptr(unsafe.Pointer(&handles[0])),
			0, // sin espera: si no hay más, que lo diga y ya
			0,
			uintptr(unsafe.Pointer(&returned)),
		)
		if ok == 0 || returned == 0 {
			return
		}
		for i := uint32(0); i < returned; i++ {
			procEvtClose.Call(handles[i])
		}
		if int(returned) < len(handles) {
			return
		}
	}
}

// watchAddrChange avisa cuando cambia una dirección IP de la máquina.
//
// Es lo que cubre pasar de WiFi a cable, que el cable se caiga, o que el DHCP
// entregue otra dirección. `NotifyAddrChange` con `OVERLAPPED` señala un evento
// y hay que volver a armarlo cada vez: es de un solo disparo por llamada.
func (e *Events) watchAddrChange() error {
	notice, err := windows.CreateEvent(nil, 0 /*auto-reset*/, 0, nil)
	if err != nil {
		return fmt.Errorf("creando el evento de cambio de direcciones: %w", err)
	}
	quit, err := windows.CreateEvent(nil, 1 /*manual*/, 0, nil)
	if err != nil {
		windows.CloseHandle(notice)
		return err
	}

	e.undo = append(e.undo, func() {
		windows.CloseHandle(notice)
		windows.CloseHandle(quit)
	})

	e.waiting.Add(1)
	go func() {
		defer e.waiting.Done()
		go func() {
			<-e.stop
			windows.SetEvent(quit)
		}()

		// El OVERLAPPED vive lo que vive la goroutine y NO se reutiliza entre
		// vueltas por comodidad: el sistema escribe dentro de él mientras la
		// operación está viva, así que soltarlo o moverlo antes de cosecharla
		// es corrupción de memoria en un proceso SYSTEM.
		var ov windows.Overlapped
		ov.HEvent = notice

		handles := []windows.Handle{notice, quit}
		for {
			var h windows.Handle
			r, _, _ := procNotifyAddrChange.Call(
				uintptr(unsafe.Pointer(&h)),
				uintptr(unsafe.Pointer(&ov)),
			)
			// Lo esperado es ERROR_IO_PENDING: la operación quedó armada. Un
			// cero significa que ya pasó algo, y cualquier otra cosa es que no
			// se pudo armar y no tiene sentido insistir.
			if errno := windows.Errno(r); errno != windows.ERROR_IO_PENDING && r != 0 {
				return
			}

			which, err := windows.WaitForMultipleObjects(handles, false, windows.INFINITE)
			if err != nil || which != windows.WAIT_OBJECT_0 {
				return
			}
			signal(e.changed)
		}
	}()
	return nil
}

// deviceNotifySubscribeParameters es lo que pide
// `PowerRegisterSuspendResumeNotification` con la variante de callback.
type deviceNotifySubscribeParameters struct {
	callback uintptr
	context  uintptr
}

const (
	deviceNotifyCallback = 2

	// Los tres tipos que dicen "acaba de volver". Se aceptan los tres porque
	// cuál llega depende de si la máquina se suspendió, hibernó, o volvió sin
	// que nadie tocara nada: `docs/03` cuenta que Fast Startup entra por acá.
	pbtAPMResumeSuspend   = 0x07
	pbtAPMResumeAutomatic = 0x12
	pbtAPMResumeCritical  = 0x06
)

// watchPower avisa al despertar de suspensión o hibernación.
//
// Es la ÚNICA de las tres con callback, porque la API de energía no ofrece
// variante con evento. La regla que lo hace seguro: el callback no hace nada
// más que un envío que no bloquea. Corre en un hilo del sistema, así que
// cualquier cosa que espere ahí dentro cuelga ese hilo, y con él la secuencia
// de reanudación de Windows entera.
func (e *Events) watchPower() error {
	cb := windows.NewCallback(func(_ uintptr, tipo uint32, _ uintptr) uintptr {
		switch tipo {
		case pbtAPMResumeSuspend, pbtAPMResumeAutomatic, pbtAPMResumeCritical:
			signal(e.resumed)
		}
		// ERROR_SUCCESS. Otra cosa haría que Windows lo tratara como un fallo
		// de la notificación.
		return 0
	})

	params := deviceNotifySubscribeParameters{callback: cb}
	var registration uintptr
	r, _, errno := procPowerRegisterSuspend.Call(
		deviceNotifyCallback,
		uintptr(unsafe.Pointer(&params)),
		uintptr(unsafe.Pointer(&registration)),
	)
	if r != 0 {
		return fmt.Errorf("registrando el notice de energía: %w", errno)
	}

	// Sin goroutine: acá no hay nada que esperar, el sistema llama solo. Lo
	// único que hace falta es darse de baja, y hacerlo ANTES de que el proceso
	// muera: un registro vivo apuntando a un callback de un Go que ya no está
	// es un salto a memoria liberada.
	e.undo = append(e.undo, func() {
		procPowerUnregister.Call(registration)
	})
	return nil
}
