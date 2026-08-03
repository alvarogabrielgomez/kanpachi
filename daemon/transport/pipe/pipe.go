// Package pipe es la entrada del daemon: un named pipe de Windows por el que
// la UI le habla.
//
// # Qué separa a quién
//
// La frontera de seguridad honesta acá es la sesión del usuario, igual que en
// cualquier aplicación de escritorio: un proceso corriendo como el usuario
// puede hablarle a este pipe, y ninguna autenticación de este paquete lo
// impide. Lo que acota el daño es **lo que la API puede pedir**, y eso vive en
// `protocol`: lista cerrada de métodos, y perfiles solo del catálogo.
//
// Lo que este paquete sí impide, que es distinto y también importa:
//
//   - Que un proceso sin privilegios TOME EL NOMBRE antes que el servicio y se
//     haga pasar por el daemon. Lo impide el prefijo protegido.
//   - Que cualquiera lea la conversación. Lo impide el descriptor de seguridad.
//   - Que se le hable desde OTRA MÁQUINA. Lo impide go-winio, que pone
//     PIPE_REJECT_REMOTE_CLIENTS siempre.
//
// # Por qué go-winio y no CreateNamedPipeW a mano
//
// Trae hechas las dos defensas que uno se olvidaría, la semántica de primera
// instancia y el rechazo de clientes remotos, más la E/S superpuesta, que son
// cientos de líneas de syscall donde un error no se ve hasta que alguien lo
// explota. Es de Microsoft, MIT, y es lo que usa Docker en Windows.
//
// Medido en esta máquina, agosto de 2026: crea el pipe normal con y sin
// descriptor, y bajo el prefijo protegido devuelve "Access is denied" sin
// elevar. Ese rechazo es la prueba de que el prefijo hace lo que promete.
package pipe

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/accentiostudios/kanpachi/core/port"
	"github.com/accentiostudios/kanpachi/daemon/transport/protocol"
)

// Name es el nombre del pipe en producción.
//
// # Por qué bajo ProtectedPrefix
//
// Cualquier proceso del usuario, sin elevar, puede crear `\\.\pipe\kanpachi` y
// quedarse con el nombre antes que el servicio. Ahí la defensa sería ganar una
// carrera contra el atacante, y esa clase de defensa se pierde el día que el
// arranque va lento.
//
// Bajo `ProtectedPrefix\Administrators` no puede, y no porque lo comprobemos
// nosotros: el prefijo lo impone el sistema, que solo deja crear ahí a
// SYSTEM y a los administradores. Un proceso sin privilegios recibe
// ERROR_ACCESS_DENIED al intentarlo. La diferencia es entre defender el
// squatting con una carrera y que sea imposible.
//
// Conectarse es otra cosa y sí funciona sin privilegios: el prefijo restringe
// quién CREA el nombre, y quién puede abrirlo lo dice [SecurityDescriptor].
const Name = `\\.\pipe\ProtectedPrefix\Administrators\kanpachi`

// ConsoleName es el del modo desarrollo, y es OTRO a propósito.
//
// Con el mismo nombre, un proceso sin privilegios ocupa el nombre de producción
// arrancando el binario real con --console. Sería el squatting sin escribir un
// okupa, usando nuestro propio binario firmado como herramienta.
const ConsoleName = `\\.\pipe\ProtectedPrefix\Administrators\kanpachi-console`

// SecurityDescriptor es quién puede abrir el pipe, en SDDL.
//
//	D:P                 DACL protegida: no hereda nada del objeto padre
//	(A;;GA;;;SY)        SYSTEM, todo. Es quien corre el servicio
//	(A;;GA;;;BA)        Administradores, todo. Para diagnosticar
//	(A;;0x12019b;;;IU)  Usuario interactivo: leer, escribir y sincronizar
//
// **El valor del usuario interactivo NO es GENERIC_ALL, y esa es la parte que
// importa.** Con todos los permisos podría crear INSTANCIAS NUEVAS del pipe, y
// una instancia nueva atiende conexiones como si fuera el daemon: sería
// secuestrar a la UI desde una cuenta sin privilegios. `0x12019b` es
// FILE_GENERIC_READ | FILE_GENERIC_WRITE | SYNCHRONIZE, o sea hablar y nada más.
//
// Pasar la cadena vacía NO es "los permisos por defecto y ya": el descriptor
// por defecto de un named pipe da lectura a Everyone y a la cuenta anónima. Por
// eso [Listen] se niega a arrancar sin descriptor en vez de tratarlo como una
// opción.
const SecurityDescriptor = "D:P(A;;GA;;;SY)(A;;GA;;;BA)(A;;0x12019b;;;IU)"

// Los topes. Constantes de compilación, como los cortes automáticos: nada que
// llegue por el pipe puede cambiarlos, porque lo que se configura desde fuera
// se puede apagar desde fuera.
const (
	// MaxConns es cuántas conversaciones a la vez. La UI abre una. Ocho deja
	// sitio para kanpctl y para una UI que se reabre sin haber cerrado limpio,
	// y pone un techo a un proceso que abra conexiones en bucle.
	MaxConns = 8

	// HelloWait es lo que se espera al saludo antes de cortar. Quien se conecta
	// y no saluda está ocupando una plaza de las ocho sin decir quién es.
	HelloWait = 5 * time.Second

	// IdleWait es lo que aguanta una conversación sin que llegue nada. La UI
	// pregunta el estado seguido, así que diez minutos de silencio es una UI
	// que murió sin cerrar el socket.
	IdleWait = 10 * time.Minute

	// WriteWait es por MENSAJE y no por escritura, igual que en el canal de la
	// sala. Un cliente que deja de leer no puede congelar al daemon.
	WriteWait = 5 * time.Second
)

// ErrSinDescriptor es negarse a arrancar sin descriptor de seguridad.
var ErrSinDescriptor = errors.New("pipe: no se puede escuchar sin descriptor de seguridad")

// Deps es lo que el oyente necesita de fuera.
type Deps struct {
	API   protocol.API
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
