// Package procnet lee las tablas de sockets que el kernel de Linux publica.
//
// # Por qué es un paquete propio y no un fichero dentro de quien lo usa
//
// Porque lo necesitan DOS adaptadores que no tienen por qué conocerse:
// `inspector` saca la foto de qué escucha para el asistente de perfiles, y la
// capa de permisos del firewall pregunta qué puertos están ocupados antes de
// cerrarlos con la cuarentena. La primera vez estuvo escrito dos veces, y dos
// lectores del mismo formato son dos sitios donde arreglar el mismo fallo.
//
// El reparto es el de siempre en este repositorio: **este fichero no tiene
// etiqueta y se prueba en cualquier sistema**, porque interpretar un formato es
// lógica. El que lleva `_linux` abre los ficheros y pregunta de quién es cada
// socket, que es lo único que necesita el sistema debajo.
//
// # El formato, que es de ancho fijo y en hexadecimal
//
//	sl  local_address rem_address st tx_queue:rx_queue tr:tmwhen retrnsmt uid timeout inode
//	0: 3500007F:0035 00000000:0000 0A 00000000:00000000 00:00000000 00000000 101 0 26538
//
// La dirección local es `HEX:HEX`. El puerto va en hexadecimal de red, o sea
// como se lee. La dirección **no**: va en el orden de bytes de la MÁQUINA, así
// que `3500007F` es 127.0.0.53 y no 53.0.0.127. Es el error que hace que una
// tabla parezca llena de direcciones de otro planeta.
package procnet

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"io"
	"net/netip"
	"strconv"
	"strings"
)

// StateListen es el estado LISTEN en la columna `st`, en hexadecimal.
//
// Solo TCP lo usa. Un socket UDP atado y sin conectar sale como `07`, que es
// CLOSE, así que filtrar UDP por este valor devolvería la lista vacía y el
// asistente de perfiles no vería ni un puerto de un juego que usa UDP, que son
// casi todos.
const StateListen = "0A"

// Row es una línea de la tabla, ya interpretada.
type Row struct {
	Addr netip.Addr
	Port uint16
	// State es la columna `st` tal cual, en hexadecimal y en mayúsculas.
	State string
	// Inode es el número con el que se le pregunta a `/proc/<pid>/fd` de quién
	// es este socket. Cero significa que la línea no lo traía.
	Inode uint64
	// UDP dice de qué tabla salió la fila.
	//
	// Lo pone quien lee los ficheros y no [Parse], porque el formato de las
	// cuatro tablas es el MISMO: el protocolo no está escrito dentro, está en
	// cuál de los cuatro ficheros se abrió. Va en la fila porque quien la
	// consume necesita el par completo, puerto y protocolo, y deducirlo después
	// por el orden de la lista sería atarse a cómo se leyeron.
	UDP bool
}

// Listening reports whether this row is a socket waiting for somebody.
//
// El protocolo entra como parámetro porque la respuesta depende de él, y esa
// diferencia es justo la que se olvida: en TCP hay un estado que lo dice y en
// UDP no hay tal cosa, así que un socket UDP atado ya está escuchando.
func (r Row) Listening(udp bool) bool { return udp || r.State == StateListen }

// Parse interpreta una tabla entera.
//
// Recibe un [io.Reader] y no una ruta para poder comprobarse sin `/proc`, igual
// que el parser de VDF de la biblioteca de Steam.
//
// # Una línea rota se salta y no tira la lectura
//
// Lo caro acá es NO ver un puerto que está ocupado: del lado del firewall eso
// significa cerrar el puerto por el que alguien administra su servidor, y del
// lado del asistente, un perfil al que le falta la mitad. Una línea que no se
// entiende no dice nada de las demás, así que cuesta esa línea.
func Parse(r io.Reader, ipv6 bool) ([]Row, error) {
	sc := bufio.NewScanner(r)
	var out []Row
	primera := true

	for sc.Scan() {
		if primera {
			// La cabecera con los nombres de columna.
			primera = false
			continue
		}
		campos := strings.Fields(sc.Text())
		if len(campos) < 4 {
			continue
		}

		local := campos[1]
		i := strings.LastIndexByte(local, ':')
		if i < 0 {
			continue
		}
		addr, ok := parseAddr(local[:i], ipv6)
		if !ok {
			continue
		}
		port, err := strconv.ParseUint(local[i+1:], 16, 16)
		if err != nil {
			continue
		}

		fila := Row{Addr: addr, Port: uint16(port), State: strings.ToUpper(campos[3])}
		// El inodo es la décima columna, y falta en kernels viejos y en algunas
		// líneas raras. No tenerlo cuesta saber de quién es el socket, no la
		// línea entera: para la cuarentena el puerto ya alcanza.
		if len(campos) > 9 {
			if inode, err := strconv.ParseUint(campos[9], 10, 64); err == nil {
				fila.Inode = inode
			}
		}
		out = append(out, fila)
	}
	return out, sc.Err()
}

// parseAddr convierte la dirección hexadecimal del kernel.
//
// # El orden de los bytes, que es la trampa de este formato
//
// El kernel escribe la dirección con `%08X` sobre enteros de 32 bits en el
// orden de la MÁQUINA, que en todo lo que corre esto es little-endian. Así que
// cada grupo de cuatro bytes va al revés: `0100007F` es 127.0.0.1.
//
// En IPv6 son CUATRO grupos de 32 bits, cada uno dado vuelta por su cuenta, y
// no los dieciséis bytes en bloque. Darlos vuelta todos juntos produce una
// dirección que parece válida y no es ninguna, que es la peor forma de
// equivocarse acá.
func parseAddr(s string, ipv6 bool) (netip.Addr, bool) {
	raw, err := hex.DecodeString(s)
	if err != nil {
		return netip.Addr{}, false
	}
	esperado := 4
	if ipv6 {
		esperado = 16
	}
	if len(raw) != esperado {
		return netip.Addr{}, false
	}

	for i := 0; i < len(raw); i += 4 {
		raw[i], raw[i+1], raw[i+2], raw[i+3] = raw[i+3], raw[i+2], raw[i+1], raw[i]
	}
	addr, ok := netip.AddrFromSlice(raw)
	if !ok {
		return netip.Addr{}, false
	}
	// Unmap deja `::ffff:0.0.0.0` como `0.0.0.0`, que es lo que un socket atado
	// a todas las interfaces en modo dual escribe en la tabla de IPv6.
	return addr.Unmap(), true
}

// SocketLink es la forma que tiene un enlace de `/proc/<pid>/fd` que apunta a un
// socket: `socket:[26538]`.
const socketPrefix = "socket:["

// InodeOfLink saca el inodo de un enlace de `/proc/<pid>/fd`.
//
// Devuelve cero para todo lo que no sea un socket, que es la mayoría: un proceso
// tiene abiertos ficheros, tuberías y dispositivos, y todos pasan por acá.
func InodeOfLink(link string) uint64 {
	if !strings.HasPrefix(link, socketPrefix) || !strings.HasSuffix(link, "]") {
		return 0
	}
	n, err := strconv.ParseUint(link[len(socketPrefix):len(link)-1], 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// ErrSinTablas es no haber podido leer NINGUNA de las tablas.
//
// Existe como error y no como lista vacía a propósito, y del lado del firewall
// la diferencia es cara: una lista vacía se lee como "no hay nada escuchando", y
// con eso la cuarentena cierra el puerto por el que el operador entra a su
// servidor. Es la misma doctrina que `routes_other.go`.
var ErrSinTablas = fmt.Errorf("procnet: no se pudo leer ninguna tabla de sockets")
