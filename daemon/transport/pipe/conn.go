package pipe

import (
	"context"
	"errors"
	"io"
	"net"
	"time"

	"github.com/accentiostudios/kanpachi/core/port"
	"github.com/accentiostudios/kanpachi/core/timing"
	"github.com/accentiostudios/kanpachi/daemon/transport/protocol"
)

// atender es una conversación entera, de la conexión al cierre.
//
// El bucle de leer, despachar y contestar NO se reimplementa acá: es
// [protocol.Server.Serve], que ya trata la desincronización como terminal y ya
// tiene sus tests. Lo que este paquete agrega es lo que sí es del transporte,
// que son los plazos y la plaza.
//
// # Los tres plazos, y por qué son tres
//
// Cada uno tapa una forma distinta de ocupar una plaza sin usarla, y ninguno
// sustituye a los otros:
//
//	el saludo     conectarse y callarse. Sin este plazo, ocho procesos mudos
//	              dejan al daemon sin plazas y a la UI sin poder entrar
//	el ocio       saludar y desaparecer sin cerrar. Es lo que deja una UI que
//	              murió de golpe, y sin plazo la plaza no vuelve nunca
//	la escritura  saludar, pedir, y dejar de leer la respuesta. Sin plazo, un
//	              cliente que no lee congela la goroutine que le escribe
//
// El de escritura es el que más cuesta ver y el que ya mordió una vez en este
// proyecto, en el canal de la sala: un miembro que dejaba de leer congelaba al
// host entero, porque quien le escribía tenía tomado el candado de la sesión.
func (l *Listener) atender(ctx context.Context, conn net.Conn) {
	defer func() { _ = conn.Close() }()

	// Un pánico atendiendo una conexión NO se puede llevar el daemon.
	//
	// Sin esto, cualquier ruta de la API que reviente mata el proceso entero, y
	// con él la sala: los puertos se cierran, el motor se va y a todos los que
	// estaban jugando se les cae la partida. Todo eso porque una pantalla pidió
	// algo raro.
	//
	// Lo descubrió un test con una API a medio implementar, que es exactamente
	// la forma que tiene el daemon durante esta rebanada: nueve adaptadores
	// todavía son provisionales. No se traga el fallo, se anota como Error y se
	// pierde SOLO esa conexión. La UI reconecta.
	defer func() {
		if r := recover(); r != nil {
			l.deps.Log.Error("pánico atendiendo la API local, se cortó esa conexión",
				"pánico", r)
		}
	}()

	// Quién está del otro lado, ANTES de leer un solo byte suyo.
	//
	// Lo contesta cada sistema como puede: Windows ya filtró con la DACL antes de
	// que hubiera conexión y acá no repite nada, Linux le pregunta al kernel por
	// el uid del proceso. La comprobación vive en el transporte y no en
	// `protocol` porque no es sobre lo que el cliente DICE (para eso está el
	// token del saludo), es sobre quién es, y eso solo lo sabe el socket.
	if err := checkPeer(conn); err != nil {
		l.deps.Log.Warn("se rechazó una conexión por quién la abrió", "error", err)
		return
	}

	// Abrir una conexión NO se anota, y ese silencio es deliberado.
	//
	// La pantalla abre una por cada pedido, y mientras narra los pasos de una
	// operación pregunta cada 400 ms: dos líneas y media por segundo durante
	// todo lo que dure crear o entrar a una sala, diciendo cada vez que el
	// programa que está en la pantalla sigue ahí. Ninguna de esas líneas
	// contesta ninguna pregunta, y juntas tapan las que sí. Lo que se anota es
	// lo que se sale de lo normal: el que no saluda, el que manda de más, el
	// que revienta.
	srv := protocol.NewServer(l.deps.API, l.deps.Token, l.deps.Clock, l.deps.Log)
	// El host es OPCIONAL: en modo consola no hay interfaz que lanzar ni
	// servicio que reconfigurar, y el servidor ya sabe contestar que no está.
	srv.AttachHost(l.deps.Host)

	// El vigilante del saludo corre aparte porque el hilo de abajo se queda
	// bloqueado en Read, así que no puede vigilarse a sí mismo. Cerrar la
	// conexión es lo que desbloquea ese Read, y es la única forma que hay: no
	// existe "cancelar una lectura en curso" sin cerrar el descriptor.
	//
	// Por eso `Greeted` es atómico del otro lado: se escribe en la goroutine de
	// abajo y se lee en esta.
	terminado := make(chan struct{})
	defer close(terminado)

	// # El interbloqueo que el BUCLE arregla, medido con el bundle portable
	//
	// Esto era un `select` suelto, sin bucle, y por eso vigilaba solo cinco
	// segundos. Pasado `timing.PipeHelloWait`, la rama del plazo se elegía, veía que la
	// conexión SÍ había saludado, no hacía nada, y la goroutine TERMINABA. A
	// partir de ahí nadie miraba `cerrando` ni `ctx` por esa conexión.
	//
	// Como cerrar la conexión es la única forma de desbloquear el `Read` de
	// abajo, el apagado se quedaba esperando: `Listener.Close` hace `wg.Wait()`,
	// y una conversación sin vigilante no se entera de nada hasta `timing.PipeIdleWait`,
	// que son diez minutos.
	//
	// Y quien pide el apagado es la propia interfaz, por una de estas
	// conexiones, así que la que colgaba era siempre la que llevaba más de cinco
	// segundos abierta, o sea todas las de una sesión de verdad. Medido el
	// 2026-08-08: tres apagados dejaron `apagando el daemon` en el log y ninguno
	// llegó a `el daemon se apagó`. El daemon quedaba vivo, y con él el
	// ejecutable del bundle que lo espera, así que "salir de Kanpachi" no
	// cerraba nada.
	//
	// **Por qué los tests no lo vieron:** `TestCerrarNoSeCuelgaConConexiones\
	// Abiertas` cierra a los pocos milisegundos de saludar, o sea DENTRO de los
	// cinco segundos en que el vigilante todavía existía. Medía el caso que
	// funcionaba.
	go func() {
		// Un temporizador y no `time.After` en el bucle: ahí dentro, cada vuelta
		// armaría uno nuevo y el plazo del saludo no vencería jamás.
		saludo := time.NewTimer(timing.PipeHelloWait)
		defer saludo.Stop()
		for {
			select {
			case <-terminado:
				return
			case <-saludo.C:
				if !srv.Greeted() {
					l.deps.Log.Warn("se cortó una conexión que no saludó a tiempo", "plazo", timing.PipeHelloWait)
					_ = conn.Close()
					return
				}
				// Saludó a tiempo: el plazo del saludo ya cumplió y no vuelve a
				// armarse. Lo que sigue vigilando es el cierre, que es lo que
				// antes se perdía acá.
			case <-l.cerrando:
				_ = conn.Close()
				return
			case <-ctx.Done():
				_ = conn.Close()
				return
			}
		}
	}()

	err := srv.Serve(ctx, &conPlazos{Conn: conn, clock: l.deps.Clock})
	l.registraCorte(err)
}

// conPlazos pone los plazos donde ocurre la E/S.
//
// # Por qué el plazo de escritura es por MENSAJE y no por escritura
//
// [wire.Writer] escribe sobre un bufio, así que una respuesta grande, un
// catálogo exportado por ejemplo, sale en varias escrituras. Renovar el plazo
// en cada una lo convierte en un plazo por trozo: un cliente que lee un byte
// por segundo mantiene la conexión viva para siempre, que es exactamente lo que
// el plazo existe para impedir.
//
// El truco es que en un protocolo de petición y respuesta cada mensaje de
// salida empieza justo después de una lectura. Así que el plazo se pone en la
// PRIMERA escritura tras leer, y no se toca hasta la lectura siguiente.
//
// No lleva candado y no lo necesita: Read y Write los llama la misma goroutine,
// la del bucle de Serve. Lo único que pasa desde fuera es el Close del
// vigilante, y eso net.Conn ya lo permite.
type conPlazos struct {
	net.Conn
	clock port.Clock

	// respuestaEnCurso dice si la escritura de ahora continúa un mensaje ya
	// empezado o abre uno nuevo.
	respuestaEnCurso bool
}

func (c *conPlazos) Read(p []byte) (int, error) {
	// Se renueva en CADA lectura y no una sola vez al principio: si no, sería
	// un tope a la duración de la conversación, y una UI abierta toda la tarde
	// es lo normal.
	_ = c.Conn.SetReadDeadline(c.clock.Now().Add(timing.PipeIdleWait))
	c.respuestaEnCurso = false
	return c.Conn.Read(p)
}

func (c *conPlazos) Write(p []byte) (int, error) {
	if !c.respuestaEnCurso {
		_ = c.Conn.SetWriteDeadline(c.clock.Now().Add(timing.PipeWriteWait))
		c.respuestaEnCurso = true
	}
	return c.Conn.Write(p)
}

// registraCorte distingue las salidas normales de las que hay que mirar.
//
// Que la UI cierre su ventana es lo más común que pasa acá, y anotarlo como
// error enseña a ignorar el log. Lo que sí merece un aviso es el mensaje por
// encima del tope, porque es lo único que no puede venir de un cliente sano.
func (l *Listener) registraCorte(err error) {
	switch {
	case err == nil, errors.Is(err, io.EOF), errors.Is(err, net.ErrClosed):
		// El cierre limpio es el final esperado de cada pedido de la pantalla.
		// No se anota, por lo mismo que no se anota la apertura.

	case errors.Is(err, protocol.ErrTooLarge):
		l.deps.Log.Warn("se cortó una conexión por mandar un mensaje por encima del tope",
			"tope", protocol.MaxMessage)

	default:
		var to net.Error
		if errors.As(err, &to) && to.Timeout() {
			l.deps.Log.Info("se cortó una conexión por silencio", "plazo", timing.PipeIdleWait)
			return
		}
		l.deps.Log.Warn("se cortó una conexión de la API local", "error", err)
	}
}
