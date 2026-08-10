//go:build linux

package inspector

import (
	"context"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/daemon/adapter/procnet"
)

// Snapshot lee las cuatro tablas de `/proc/net` y dice de quién es cada socket.
//
// # El reparto, que es el mismo que en Windows
//
// Devuelve la tabla ENTERA, sin filtrar por proceso. Quién decide qué entrada
// cuenta es [domain.ObservedRanges], que descarta lo que no escucha en todas las
// interfaces, descarta el ruido de Steam y agrupa los puertos contiguos. Ver la
// cabecera del paquete.
//
// El proceso raíz se recibe y no se usa, igual que en Windows: sirve para
// atribuir después, en el dominio, y filtrar acá le quitaría al dominio la
// información con la que decide.
//
// # Lo que cuesta, y por qué se paga solo acá
//
// Saber el PID exige recorrer los descriptores de todos los procesos, porque
// `/proc/net/tcp` trae el inodo del socket y no su dueño. Es lo que en Windows
// contesta `GetExtendedTcpTable` de una sola llamada. Se paga acá y no en el
// lector compartido porque esto corre UNA vez, cuando el usuario pulsa observar
// con el juego ya abierto, mientras que la cuarentena pregunta lo mismo en cada
// arranque y no necesita el dueño.
func (s *Sockets) Snapshot(_ context.Context, _ domain.ProcessRef) ([]domain.Listener, error) {
	filas, err := procnet.Read(procnet.Tables)
	if err != nil {
		return nil, err
	}
	dueños := procnet.OwnersByInode()

	out := make([]domain.Listener, 0, len(filas))
	for _, f := range filas {
		proto := domain.ProtoTCP
		if f.UDP {
			proto = domain.ProtoUDP
		}
		out = append(out, domain.Listener{
			Proto:   proto,
			Port:    f.Port,
			Address: f.Addr.String(),
			// Un socket cuyo dueño no se pudo mirar sale con PID cero, que es lo
			// que ya significa "no se sabe" en el resto del producto. Descartar
			// la fila entera sería peor: el puerto está ocupado igual, y el
			// dominio sabe qué hacer con un puerto sin dueño.
			PID: dueños[f.Inode],
		})
	}
	return out, nil
}
