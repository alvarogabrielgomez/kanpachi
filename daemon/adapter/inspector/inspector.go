// Package inspector saca la foto de las tablas de sockets de Windows.
//
// # Cuándo corre, que es casi nunca
//
// SOLO cuando el usuario pulsa el botón de observar dentro del creador de
// perfiles, con el juego ya abierto. No hay espera de fondo, no queda nada
// corriendo al cerrar el asistente, y durante una partida normal Kanpachi no
// consulta procesos ni una vez. Ver [port.SocketInspector], que lo dice con las
// mismas palabras.
//
// # Lo que devuelve, y por qué es la tabla ENTERA
//
// La misma información que `netstat -ano`: qué está escuchando, en qué
// dirección, y de qué PID. Sin filtrar por proceso.
//
// Filtrar acá parecería más limpio y sería un error de reparto: quién decide
// qué entrada cuenta es [domain.ObservedRanges], que descarta lo que no escucha
// en todas las interfaces, descarta el ruido de Steam y agrupa los puertos
// contiguos. Eso es criterio, se prueba sin Windows, y ya está escrito. Este
// paquete solo sabe leer una tabla del sistema.
package inspector

import (
	"github.com/accentiostudios/kanpachi/core/port"
)

// Sockets lee las tablas de TCP y UDP.
//
// No guarda estado y no cachea nada. Una foto solo es cierta en el instante en
// que se toma, y el asistente la pide justo cuando el usuario acaba de abrir el
// juego.
type Sockets struct{}

func New() *Sockets { return &Sockets{} }

var _ port.SocketInspector = (*Sockets)(nil)
