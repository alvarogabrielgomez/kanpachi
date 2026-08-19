//go:build windows

package pipe

import (
	"net"

	"github.com/Microsoft/go-winio"
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
const Name = `\\.\pipe\ProtectedPrefix\Administrators\kanpachi-installed`

// PortableName es el canal del producto portable.
//
// No puede compartir [Name]. Instalado y portable son dos productos completos,
// cada uno con su daemon, sus datos y su interfaz. Compartir el pipe hacía que
// el primero que arrancara secuestrara al lanzador del otro y que una UI leyera
// el token de su carpeta contra el daemon ajeno.
//
// **Lo que separa es el canal y los datos, y hasta ahí llega.** Los dos usan el
// mismo `kanpachi0`, el mismo grupo de firewall y la misma compuerta, que son
// de la máquina y no de la instalación. Con los dos daemons vivos, el segundo
// muere al abrir sala: Wintun admite una sola sesión por adaptador y contesta
// «WintunStartSession failed», sin nombrar la causa. Medido el 2026-08-19 con
// el instalado y el portable abiertos a la vez. Que reviente ahí es lo mejor
// que puede pasar: si los adaptadores tuvieran nombres distintos, los dos
// seguirían adelante pisándose el firewall en silencio.
const PortableName = `\\.\pipe\ProtectedPrefix\Administrators\kanpachi-portable`

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
//
// # Comprobado a mano, y el resultado es mejor de lo esperado
//
// Con el daemon corriendo como usuario NORMAL, el pipe se crea y el token se
// escribe, y la primera conexión falla al aceptar con "Access is denied". El
// motivo es que aceptar exige crear la instancia SIGUIENTE del pipe, y el
// usuario interactivo no puede: solo tiene leer, escribir y sincronizar.
//
// O sea que el descriptor cumple su promesa tan literalmente que impide
// probarlo sin elevar. En producción el daemon corre como SYSTEM, que sí tiene
// GENERIC_ALL, y por eso ahí sí atiende. Para probar a mano hace falta una
// consola elevada.
const SecurityDescriptor = "D:P(A;;GA;;;SY)(A;;GA;;;BA)(A;;0x12019b;;;IU)"

// checkPeer en Windows no comprueba nada, y no es un hueco.
//
// Quién puede abrir el pipe lo decidió el sistema ANTES de que hubiera conexión,
// leyendo [SecurityDescriptor]: una conexión que llega acá ya pasó por la DACL.
// En Linux no hay ese filtro previo con la misma finura, y por eso allá esta
// función sí hace trabajo. Ver el `checkPeer` de `pipe_linux.go`.
func checkPeer(net.Conn) error { return nil }

// abrirPipe crea el named pipe de verdad.
//
// Vive aparte del resto del paquete para que TODO lo demás compile y se pruebe
// en Linux: las plazas, los plazos y el cierre son lógica, y la lógica que solo
// corre en la máquina donde se programa es lógica sin CI.
//
// Lo que go-winio pone y no se ve acá, que es por lo que se usa:
//
//   - FILE_FLAG_FIRST_PIPE_INSTANCE en la primera instancia. Es lo que hace que
//     el nombre sea nuestro: si ya existe, falla en vez de sumarse.
//   - PIPE_REJECT_REMOTE_CLIENTS, siempre. Nadie le habla a este pipe desde
//     otra máquina.
//   - E/S superpuesta, que es lo que permite que un Close corte un Read
//     bloqueado en vez de dejar la goroutine colgada para siempre.
func abrirPipe(nombre, sddl string) (net.Listener, error) {
	if sddl == "" {
		return nil, ErrSinDescriptor
	}
	// MessageMode en false a propósito: el enmarcado es por líneas y lo hace
	// `wire`, así que el modo mensaje de Windows sería un segundo enmarcado por
	// debajo del nuestro. Dos capas que dicen dónde termina un mensaje es una
	// de más, y la que se puede probar en Linux es la nuestra.
	return winio.ListenPipe(nombre, &winio.PipeConfig{
		SecurityDescriptor: sddl,
		MessageMode:        false,
		InputBufferSize:    64 * 1024,
		OutputBufferSize:   64 * 1024,
	})
}
