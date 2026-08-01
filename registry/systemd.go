package registry

import (
	"context"
	"net"
	"os"
	"strconv"
	"time"
)

// El protocolo de notificación de systemd, implementado a mano.
//
// Son treinta líneas y evita una dependencia entera. Lo único que hace es
// escribir cadenas en un socket datagram de Unix cuya ruta llega por entorno.
// Fuera de systemd no hay socket, así que todo esto se vuelve nada y el
// binario corre igual desde una terminal.
//
// Importa porque es lo que reemplaza al vigilante externo. `Restart=always`
// solo actúa cuando el proceso MUERE; un proceso vivo pero colgado se queda
// colgado para siempre. Con el latido, dejar de mandarlo es la señal, y
// systemd lo reinicia sin que haya que instalar nada más que se pueda caer por
// su cuenta.

// notificar manda un mensaje a systemd. Devuelve false si no hay a quién.
func notificar(mensaje string) bool {
	ruta := os.Getenv("NOTIFY_SOCKET")
	if ruta == "" {
		return false
	}
	// Una ruta que empieza con @ es del espacio de nombres abstracto de Linux,
	// que en Go se expresa con un NUL por delante.
	if ruta[0] == '@' {
		ruta = "\x00" + ruta[1:]
	}
	con, err := net.Dial("unixgram", ruta)
	if err != nil {
		return false
	}
	defer con.Close()
	_, err = con.Write([]byte(mensaje))
	return err == nil
}

// Listo le dice a systemd que el servicio ya atiende. Con Type=notify en la
// unit, systemd no considera arrancado el servicio hasta este momento, así que
// lo que dependa de él espera de verdad en vez de adivinar con un sleep.
func Listo(estado string) { notificar("READY=1\nSTATUS=" + estado) }

// Latido manda WATCHDOG=1 al ritmo que systemd pidió, hasta que el contexto se
// cancele. Si WatchdogSec no está configurado, no hace nada.
//
// Late a la MITAD del plazo a propósito: deja margen para una pausa de GC o un
// pico de carga sin que systemd concluya que el proceso murió. Un latido justo
// en el límite convierte cualquier hipo en un reinicio.
func Latido(ctx context.Context, sano func() bool) {
	crudo := os.Getenv("WATCHDOG_USEC")
	if crudo == "" {
		return
	}
	usec, err := strconv.Atoi(crudo)
	if err != nil || usec <= 0 {
		return
	}
	intervalo := time.Duration(usec) * time.Microsecond / 2

	t := time.NewTicker(intervalo)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			// El latido se calla si el servicio dejó de estar sano. Es la
			// diferencia entre "el proceso existe" y "el proceso sirve", y es
			// justo la que un Restart=always no sabe ver.
			if sano == nil || sano() {
				notificar("WATCHDOG=1")
			}
		}
	}
}

// Parando avisa que el cierre empezó, para que systemd no lo tome por una
// muerte súbita mientras se atienden las peticiones en curso.
func Parando() { notificar("STOPPING=1\nSTATUS=cerrando") }
