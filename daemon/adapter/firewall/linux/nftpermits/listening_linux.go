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
//
// # Quién lee /proc, que ya no es este fichero
//
// Lo lee `adapter/procnet`, que es el mismo lector que usa el inspector de
// sockets. Estuvo escrito dos veces, una acá y otra allá, y dos lectores del
// mismo formato son dos sitios donde arreglar el mismo fallo. Lo que queda acá
// es la única parte que es de la cuarentena: qué se hace con la respuesta.

import (
	"fmt"

	"github.com/accentiostudios/kanpachi/daemon/adapter/procnet"
)

// listeningPorts devuelve los puertos TCP donde hay algo escuchando.
//
// # Por qué se lee /proc y no se abre un socket para probar
//
// Porque probar a conectarse contesta sobre el camino de red y esto pregunta por
// la máquina. Un puerto con algo escuchando detrás de un firewall ajeno no
// contestaría, y cerrarlo sería igual de destructivo.
//
// # Solo TCP, y solo los puertos
//
// TCP porque es lo que la cuarentena cierra y porque el camino de vuelta del
// operador, que es lo que esto protege, va por ahí. Solo los puertos porque
// saber DE QUIÉN es cada socket cuesta recorrer los descriptores de todos los
// procesos, y para no cerrar un puerto ocupado alcanza con saber que lo está.
func listeningPorts() (map[uint16]bool, error) {
	filas, err := procnet.Read(procnet.TCPTables)
	if err != nil {
		// Sin ninguna tabla leída, devolver un mapa vacío se leería como "no hay
		// nada escuchando", que es justo la respuesta con la que se cierra el
		// puerto de administración. Es la misma doctrina que `routes_other.go`:
		// fallar en vez de devolver la lista vacía.
		return nil, fmt.Errorf("no se sabe qué hay escuchando, así que no se cierra nada: %w", err)
	}

	out := make(map[uint16]bool, len(filas))
	for _, f := range filas {
		out[f.Port] = true
	}
	return out, nil
}
