// Package pipe es la entrada del daemon: el canal local por el que le hablan la
// interfaz en Windows y el CLI en Linux.
//
// El nombre del paquete es de cuando solo había named pipes. El canal es un
// named pipe en Windows y un socket Unix en Linux, y **la única diferencia que
// llega a este fichero es el `net.Listener` que devuelve `abrirPipe`**. Todo lo
// que decide qué pasa después (las plazas, los tres plazos, el cierre que espera
// a las conversaciones en curso) es lógica que se prueba entera en el job de
// Linux.
//
// # Qué separa a quién
//
// La frontera de seguridad honesta acá es **quién puede abrir el canal**, y en
// los dos sistemas es la misma persona que ya puede hacer el daño por otras
// vías: el usuario de la sesión en Windows, root en Linux. Ninguna
// autenticación de este paquete lo cambia. Lo que acota el daño es **lo que la
// API puede pedir**, y eso vive en `protocol`: lista cerrada de métodos, y
// perfiles solo del catálogo.
//
// Lo que este paquete sí impide, que es distinto y también importa:
//
//   - Que un proceso sin privilegios TOME LA DIRECCIÓN antes que el daemon y se
//     haga pasar por él. En Windows lo impide el prefijo protegido; en Linux, el
//     directorio del socket, que es de root y no deja entrar a nadie más.
//   - Que cualquiera lea la conversación. En Windows es el descriptor de
//     seguridad; en Linux, el modo del socket más la comprobación del dueño de
//     quien se conecta.
//   - Que se le hable desde OTRA MÁQUINA. En Windows lo pone go-winio con
//     PIPE_REJECT_REMOTE_CLIENTS; un socket Unix no tiene forma de salir de la
//     máquina.
//
// # Por qué el descriptor no se puede dejar vacío
//
// Los dos sistemas comparten la misma trampa: **el valor por omisión es el
// permisivo**. Un named pipe sin descriptor da lectura a Everyone y a la cuenta
// anónima; un socket Unix recién creado toma `0777 &^ umask`, o sea 0755 con el
// umask de siempre, y eso es conectable por cualquiera. Por eso la cadena vacía
// es [ErrSinDescriptor] y no una opción, y por eso la comprobación va en cada
// `abrirPipe` y no acá: es la única forma de que el job de Linux la pruebe.
package pipe

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"

	"github.com/accentiostudios/kanpachi/core/port"
	"github.com/accentiostudios/kanpachi/daemon/transport/protocol"
)

// Las direcciones ([Name], [PortableName], [ConsoleName]) y el descriptor
// ([SecurityDescriptor]) viven en el fichero de cada sistema, no acá.
//
// Es lo mismo que hace [abrirPipe] y por la misma razón: un nombre de named pipe
// no significa nada en Linux y una ruta de `/run` no significa nada en Windows,
// así que un fichero compartido que declarara los dos juegos obligaría a leerlo
// entero para saber cuál manda. Los identificadores sí son los mismos en los dos
// sistemas, que es lo que permite que `main.go` los use sin preguntar dónde
// corre.

// Los topes. Constantes de compilación, como los cortes automáticos: nada que
// llegue por el pipe puede cambiarlos, porque lo que se configura desde fuera
// se puede apagar desde fuera.
const (
	// MaxConns es cuántas conversaciones a la vez. La UI abre una. Ocho deja
	// sitio para pipeprobe y para una UI que se reabre sin haber cerrado limpio,
	// y pone un techo a un proceso que abra conexiones en bucle.
	MaxConns = 8
)

// ErrSinDescriptor es negarse a arrancar sin descriptor de seguridad.
var ErrSinDescriptor = errors.New("pipe: no se puede escuchar sin descriptor de seguridad")

// Deps es lo que el oyente necesita de fuera.
type Deps struct {
	API protocol.API
	// Host es el PROCESO del daemon: su interfaz, su apagado y su tipo de
	// arranque. Nil en modo consola, y eso no es un hueco: ver [protocol.Host].
	Host  protocol.Host
	Token string
	Clock port.Clock
	Log   port.Logger

	// Name es el nombre del pipe. Vacío significa [Name].
	Name string
	// SecurityDescriptor vacío significa [SecurityDescriptor]. La cadena
	// explícita "" no existe como opción: ver [ErrSinDescriptor].
	SecurityDescriptor string

	// listen existe para los tests, que no pueden crear un named pipe en el
	// CI de Linux. Nil significa el de verdad.
	listen func(nombre, sddl string) (net.Listener, error)
}

// Listener atiende conexiones de la UI.
type Listener struct {
	deps     Deps
	ln       net.Listener
	plazas   chan struct{}
	cerrando chan struct{}
	una      sync.Once
	wg       sync.WaitGroup
}

// Listen abre el pipe.
//
// **Va ÚLTIMO en el arranque del daemon**, y no es orden de conveniencia: el
// pipe es la única superficie alcanzable desde fuera del proceso, así que
// abrirlo al final significa que ninguna orden puede llegar antes de que la
// máquina esté en cuarentena y el firewall purgado.
func Listen(deps Deps) (*Listener, error) {
	if deps.API == nil {
		return nil, errors.New("pipe: sin API no hay nada que atender")
	}
	if deps.Token == "" {
		// Un token vacío haría que el saludo lo pase cualquiera, o sea que la
		// puerta quedaría abierta con la cerradura puesta.
		return nil, errors.New("pipe: sin token no se puede saludar a nadie")
	}
	if deps.Clock == nil || deps.Log == nil {
		return nil, errors.New("pipe: faltan el reloj o el log")
	}

	nombre := deps.Name
	if nombre == "" {
		nombre = Name
	}
	sddl := deps.SecurityDescriptor
	if sddl == "" {
		sddl = SecurityDescriptor
	}

	abrir := deps.listen
	if abrir == nil {
		abrir = abrirPipe
	}
	ln, err := abrir(nombre, sddl)
	if err != nil {
		return nil, fmt.Errorf("pipe: abriendo %s: %w", nombre, err)
	}

	l := &Listener{
		deps:     deps,
		ln:       ln,
		plazas:   make(chan struct{}, MaxConns),
		cerrando: make(chan struct{}),
	}
	l.deps.Log.Info("la API local escucha", "pipe", nombre)
	return l, nil
}

// Serve atiende hasta que se cierre el oyente o se cancele el contexto.
func (l *Listener) Serve(ctx context.Context) error {
	for {
		conn, err := l.ln.Accept()
		if err != nil {
			select {
			case <-l.cerrando:
				// Cerrar el oyente hace fallar el Accept que está esperando.
				// Es la salida normal, no un error.
				return nil
			default:
			}
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("pipe: aceptando: %w", err)
		}

		select {
		case l.plazas <- struct{}{}:
		default:
			// Sin plaza se corta ENSEGUIDA y sin leer nada. Encolarlas dejaría
			// que un proceso en bucle llene la memoria de conexiones que nadie
			// va a atender.
			l.deps.Log.Warn("se rechazó una conexión por el tope", "tope", MaxConns)
			_ = conn.Close()
			continue
		}

		l.wg.Add(1)
		go func() {
			defer func() {
				<-l.plazas
				l.wg.Done()
			}()
			l.atender(ctx, conn)
		}()
	}
}

// Close corta el oyente y espera a las conversaciones en curso.
//
// IDEMPOTENTE: lo llama el camino de error del arranque, que puede correr antes
// de que se haya abierto nada.
func (l *Listener) Close() error {
	l.una.Do(func() {
		close(l.cerrando)
		_ = l.ln.Close()
	})
	l.wg.Wait()
	return nil
}
