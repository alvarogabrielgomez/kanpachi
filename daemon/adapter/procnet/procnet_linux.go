//go:build linux

package procnet

// Abrir los ficheros de `/proc` y preguntar de quién es cada socket.
//
// Lo único que necesita el sistema debajo. Interpretar el formato está en
// `procnet.go`, sin etiqueta, y por eso se prueba en cualquier sistema.

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// Table es una de las cuatro tablas que publica el kernel.
type Table struct {
	Path string
	UDP  bool
	IPv6 bool
}

// Tables son las cuatro, y hacen falta las cuatro.
//
// Las de IPv6 no son opcionales: un servidor moderno ata sus servicios a `::`,
// y en modo dual eso atiende también a IPv4 apareciendo SOLO en la tabla de
// IPv6. Mirar únicamente IPv4 deja sin ver el caso más común.
var Tables = []Table{
	{Path: "/proc/net/tcp"},
	{Path: "/proc/net/tcp6", IPv6: true},
	{Path: "/proc/net/udp", UDP: true},
	{Path: "/proc/net/udp6", UDP: true, IPv6: true},
}

// TCPTables son las dos que alcanzan para saber qué puertos TCP están ocupados.
var TCPTables = Tables[:2]

// Read lee las tablas que se le pidan y devuelve sus filas.
//
// Que falte un fichero NO es un fallo: `/proc/net/tcp6` no existe en un kernel
// compilado sin IPv6. Que no se pueda leer NINGUNO sí lo es, y devuelve
// [ErrSinTablas]: ver ahí por qué la lista vacía no sirve como respuesta.
func Read(tables []Table) ([]Row, error) {
	var out []Row
	leidoAlguno := false

	for _, t := range tables {
		f, err := os.Open(t.Path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("leyendo %s: %w", t.Path, err)
		}
		filas, err := Parse(f, t.IPv6)
		_ = f.Close()
		if err != nil {
			return nil, fmt.Errorf("interpretando %s: %w", t.Path, err)
		}
		for _, fila := range filas {
			if !fila.Listening(t.UDP) {
				continue
			}
			// El protocolo sale del FICHERO y no de la línea: las cuatro tablas
			// tienen el mismo formato. Ver [Row.UDP].
			fila.UDP = t.UDP
			out = append(out, fila)
		}
		leidoAlguno = true
	}

	if !leidoAlguno {
		return nil, ErrSinTablas
	}
	return out, nil
}

// OwnersByInode dice qué proceso tiene abierto cada socket.
//
// # Por qué hay que recorrer TODOS los procesos para esto
//
// Porque `/proc/net/tcp` no trae el PID: trae el inodo del socket, y quién lo
// tiene abierto solo se sabe mirando los descriptores de cada proceso. Es lo
// mismo que en Windows contesta `GetExtendedTcpTable` de una sola llamada, y
// acá cuesta un recorrido.
//
// Ese coste es la razón de que esto vaya aparte de [Read] y no dentro: la
// cuarentena pregunta CUÁLES puertos están ocupados, sin necesitar de quién son,
// y hacerle pagar el recorrido sería regalarle latencia a cada arranque.
//
// Los fallos se ignoran a propósito. Un proceso que termina mientras se lo mira
// y uno de otro usuario dan el mismo error, y ninguno de los dos es un problema:
// se pierde el dueño de ese socket, no la respuesta.
func OwnersByInode() map[uint64]int {
	out := map[uint64]int{}

	entradas, err := os.ReadDir("/proc")
	if err != nil {
		return out
	}
	for _, e := range entradas {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			// Lo que no es un número son las entradas que `/proc` tiene aparte
			// de los procesos: `self`, `net`, `sys` y compañía.
			continue
		}
		fds := filepath.Join("/proc", e.Name(), "fd")
		abiertos, err := os.ReadDir(fds)
		if err != nil {
			continue
		}
		for _, fd := range abiertos {
			enlace, err := os.Readlink(filepath.Join(fds, fd.Name()))
			if err != nil {
				continue
			}
			if inode := InodeOfLink(enlace); inode != 0 {
				out[inode] = pid
			}
		}
	}
	return out
}
