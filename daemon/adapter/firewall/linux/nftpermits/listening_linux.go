package nftpermits

// La red de seguridad que impide que la cuarentena deje al operador fuera de su
// propia máquina.
//
// # Por qué hace falta si el dominio ya saca el 22
//
// Porque sacar el 22 cubre al que dejó sshd donde estaba, y no al que lo movió.
// Mover sshd a otro puerto es una práctica común en un VPS, y si ese puerto cae
// en la lista de la cuarentena, el resultado es el mismo: una regla permanente,
// que sobrevive al reinicio y a apagar Kanpachi, cerrando el único camino de
// vuelta.
//
// El dominio no puede saberlo, porque para saberlo hay que mirar el sistema. Así
// que lo mira el adaptador, que es de quien es ese trabajo.

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// procTCP son los ficheros donde el kernel publica los sockets TCP.
//
// Los dos, y no solo el primero: un sshd que escucha en `::` aparece únicamente
// en el de IPv6, y mirar solo IPv4 dejaría exactamente el caso más común de un
// servidor moderno sin cubrir.
var procTCP = []string{"/proc/net/tcp", "/proc/net/tcp6"}

// tcpListen es el estado LISTEN en la columna `st` de /proc/net/tcp.
//
// Hexadecimal y sin ceros a la izquierda, tal como lo escribe el kernel.
const tcpListen = "0A"

// listeningPorts devuelve los puertos TCP donde hay algo escuchando.
//
// # Por qué se lee /proc y no se abre un socket para probar
//
// Porque probar a conectarse contesta sobre el camino de red y esto pregunta por
// la máquina. Un puerto con algo escuchando detrás de un firewall ajeno no
// contestaría, y cerrarlo sería igual de destructivo.
func listeningPorts() (map[uint16]bool, error) {
	out := map[uint16]bool{}
	leidoAlguno := false

	for _, ruta := range procTCP {
		f, err := os.Open(ruta)
		if err != nil {
			if os.IsNotExist(err) {
				// /proc/net/tcp6 falta en un kernel sin IPv6. No es un fallo.
				continue
			}
			return nil, fmt.Errorf("leyendo %s: %w", ruta, err)
		}
		err = parseListening(f, out)
		_ = f.Close()
		if err != nil {
			return nil, fmt.Errorf("interpretando %s: %w", ruta, err)
		}
		leidoAlguno = true
	}

	if !leidoAlguno {
		// Sin ningún fichero leído, devolver un mapa vacío se leería como "no hay
		// nada escuchando", que es justo la respuesta con la que se cierra el
		// puerto de administración. Es la misma doctrina que `routes_other.go`:
		// fallar en vez de devolver la lista vacía.
		return nil, fmt.Errorf("no se pudo leer ninguno de %v, así que no se sabe qué hay "+
			"escuchando y no se cierra nada", procTCP)
	}
	return out, nil
}

// parseListening interpreta el formato de /proc/net/tcp.
//
// Va aparte y recibe un io.Reader para poder comprobarse sin /proc, igual que el
// parser de VDF de la biblioteca de Steam.
//
// El formato es de ancho fijo por columnas separadas por espacios:
//
//	sl  local_address rem_address st tx_queue:rx_queue tr:tmwhen retrnsmt uid ...
//	0: 00000000:0016 00000000:0000 0A 00000000:00000000 00:00000000 00000000 0 ...
//
// La dirección local es `HEX:HEX` y el puerto es la parte de después de los dos
// puntos, en hexadecimal.
func parseListening(r interface{ Read([]byte) (int, error) }, out map[uint16]bool) error {
	sc := bufio.NewScanner(r)
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
		if !strings.EqualFold(campos[3], tcpListen) {
			continue
		}
		local := campos[1]
		i := strings.LastIndexByte(local, ':')
		if i < 0 {
			continue
		}
		puerto, err := strconv.ParseUint(local[i+1:], 16, 16)
		if err != nil {
			// Una línea que no se entiende se salta y no tira la lectura: lo
			// caro es NO ver un puerto que está escuchando, y una línea rota no
			// dice nada de las demás.
			continue
		}
		out[uint16(puerto)] = true
	}
	return sc.Err()
}
