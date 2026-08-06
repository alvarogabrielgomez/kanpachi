//go:build windows

package inspector

import (
	"context"
	"encoding/binary"
	"fmt"
	"net/netip"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// El idioma de `NewLazySystemDLL` ya está establecido en este repositorio, y se
// sigue: `windows.NewLazySystemDLL` resuelve desde `System32` y solo desde ahí,
// que es lo que impide que un DLL plantado al lado del binario se cuele en un
// proceso que corre como SYSTEM.
var (
	iphlpapi             = windows.NewLazySystemDLL("iphlpapi.dll")
	procGetExtendedTCP   = iphlpapi.NewProc("GetExtendedTcpTable")
	procGetExtendedUDP   = iphlpapi.NewProc("GetExtendedUdpTable")
	errInsufficientSpace = windows.ERROR_INSUFFICIENT_BUFFER
)

// Las clases de tabla que se piden.
//
// Para TCP se pide la de ESCUCHAS y no la de todo: la de todo trae cada
// conexión establecida de la máquina, que son miles en un navegador abierto y
// ninguna de ellas es un puerto que haya que abrir. UDP no tiene esa
// distinción, porque un socket UDP ligado ya es lo más parecido a una escucha
// que ese protocolo tiene.
const (
	tcpTableOwnerPIDListener = 3
	udpTableOwnerPID         = 1
)

type mibTCPRowOwnerPID struct {
	state      uint32
	localAddr  uint32
	localPort  uint32
	remoteAddr uint32
	remotePort uint32
	owningPID  uint32
}

type mibTCPTableOwnerPID struct {
	numEntries uint32
	table      [1]mibTCPRowOwnerPID
}

type mibTCP6RowOwnerPID struct {
	localAddr    [16]byte
	localScopeID uint32
	localPort    uint32
	remoteAddr   [16]byte
	remoteScope  uint32
	remotePort   uint32
	state        uint32
	owningPID    uint32
}

type mibTCP6TableOwnerPID struct {
	numEntries uint32
	table      [1]mibTCP6RowOwnerPID
}

type mibUDPRowOwnerPID struct {
	localAddr uint32
	localPort uint32
	owningPID uint32
}

type mibUDPTableOwnerPID struct {
	numEntries uint32
	table      [1]mibUDPRowOwnerPID
}

type mibUDP6RowOwnerPID struct {
	localAddr    [16]byte
	localScopeID uint32
	localPort    uint32
	owningPID    uint32
}

type mibUDP6TableOwnerPID struct {
	numEntries uint32
	table      [1]mibUDP6RowOwnerPID
}

// Snapshot lee las cuatro tablas: TCP y UDP, en IPv4 y en IPv6.
//
// Las cuatro y no dos. Un juego que liga en `::` con sockets de doble pila no
// aparece en la tabla de IPv4, y perderlo produciría un perfil sin el puerto
// que el juego usa de verdad. Cuesta dos llamadas más y no puede fallar de
// forma silenciosa.
//
// El `root` NO se usa para filtrar. Ver la cabecera del paquete: quién decide
// qué entrada cuenta es el dominio.
func (s *Sockets) Snapshot(ctx context.Context, _ domain.ProcessRef) ([]domain.Listener, error) {
	// Cuatro llamadas síncronas de milisegundos. No hay nada que interrumpir a
	// la mitad, así que el contexto se honra en la puerta y no se finge
	// cancelable.
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var fuera []domain.Listener

	tcp4, err := tableBytes(procGetExtendedTCP, windows.AF_INET, tcpTableOwnerPIDListener)
	if err != nil {
		return nil, fmt.Errorf("leyendo la tabla TCP de IPv4: %w", err)
	}
	fuera = append(fuera, readTCP4(tcp4)...)

	tcp6, err := tableBytes(procGetExtendedTCP, windows.AF_INET6, tcpTableOwnerPIDListener)
	if err != nil {
		return nil, fmt.Errorf("leyendo la tabla TCP de IPv6: %w", err)
	}
	fuera = append(fuera, readTCP6(tcp6)...)

	udp4, err := tableBytes(procGetExtendedUDP, windows.AF_INET, udpTableOwnerPID)
	if err != nil {
		return nil, fmt.Errorf("leyendo la tabla UDP de IPv4: %w", err)
	}
	fuera = append(fuera, readUDP4(udp4)...)

	udp6, err := tableBytes(procGetExtendedUDP, windows.AF_INET6, udpTableOwnerPID)
	if err != nil {
		return nil, fmt.Errorf("leyendo la tabla UDP de IPv6: %w", err)
	}
	fuera = append(fuera, readUDP6(udp6)...)

	return fuera, nil
}

// tableBytes pide una tabla, midiéndola primero.
//
// La forma documentada de dimensionar estas llamadas es dejarlas fallar una vez
// y repetir con el tamaño que piden. El bucle está ACOTADO porque la tabla
// crece entre las dos llamadas cada vez que alguien abre un socket, o sea todo
// el rato: sin tope, una máquina ocupada colgaría esto para siempre.
func tableBytes(proc *windows.LazyProc, familia uint32, clase uint32) ([]byte, error) {
	tamaño := uint32(8192)
	for intento := 0; ; intento++ {
		buf := make([]byte, tamaño)
		r, _, _ := proc.Call(
			uintptr(unsafe.Pointer(&buf[0])),
			uintptr(unsafe.Pointer(&tamaño)),
			0, // sin ordenar: ordenar cuesta y el orden no se usa
			uintptr(familia),
			uintptr(clase),
			0,
		)
		switch {
		case r == 0:
			return buf, nil
		case windows.Errno(r) == errInsufficientSpace && intento < 4:
			// `tamaño` ya trae lo que hace falta. Un margen encima evita
			// repetir por un socket que nació entre las dos llamadas.
			tamaño += tamaño / 4
		default:
			return nil, windows.Errno(r)
		}
	}
}

// portOf saca el puerto de un DWORD donde viene en orden de red.
//
// Los dos bytes de arriba del DWORD son basura documentada, así que se
// descartan, y los dos de abajo van al revés de como los lee esta máquina.
func portOf(dw uint32) uint16 {
	return uint16(dw&0xff)<<8 | uint16((dw>>8)&0xff)
}

// addr4 convierte el DWORD de una dirección IPv4.
//
// Viene en orden de red, o sea que sus bytes EN MEMORIA ya están en el orden
// correcto; lo que hay que evitar es interpretarlo como número.
func addr4(dw uint32) netip.Addr {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], dw)
	return netip.AddrFrom4(b)
}

// rows recorre una tabla de las cuatro. El primer campo es siempre la cantidad,
// y el array viene detrás; el desplazamiento sale de `unsafe.Offsetof` para que
// lo calcule el compilador con su alineación y no yo a ojo.
func rows[T any, Tabla any](buf []byte, off uintptr, n func(*Tabla) uint32) []T {
	if len(buf) < int(off) {
		return nil
	}
	tabla := (*Tabla)(unsafe.Pointer(&buf[0]))
	cuántas := int(n(tabla))
	if cuántas <= 0 {
		return nil
	}
	// El tope contra lo que el búfer aguanta de verdad. La cantidad la escribe
	// el sistema y no hay motivo para desconfiar; leer fuera del búfer por un
	// número torcido es un fallo de memoria en un proceso SYSTEM, y esa
	// comprobación cuesta una resta.
	caben := (len(buf) - int(off)) / int(unsafe.Sizeof(*new(T)))
	if cuántas > caben {
		cuántas = caben
	}
	primero := (*T)(unsafe.Pointer(uintptr(unsafe.Pointer(&buf[0])) + off))
	return unsafe.Slice(primero, cuántas)
}

func readTCP4(buf []byte) []domain.Listener {
	var t mibTCPTableOwnerPID
	filas := rows[mibTCPRowOwnerPID, mibTCPTableOwnerPID](buf, unsafe.Offsetof(t.table),
		func(x *mibTCPTableOwnerPID) uint32 { return x.numEntries })
	fuera := make([]domain.Listener, 0, len(filas))
	for i := range filas {
		fuera = append(fuera, domain.Listener{
			Proto:   domain.ProtoTCP,
			Port:    portOf(filas[i].localPort),
			Address: addr4(filas[i].localAddr).String(),
			PID:     int(filas[i].owningPID),
		})
	}
	return fuera
}

func readTCP6(buf []byte) []domain.Listener {
	var t mibTCP6TableOwnerPID
	filas := rows[mibTCP6RowOwnerPID, mibTCP6TableOwnerPID](buf, unsafe.Offsetof(t.table),
		func(x *mibTCP6TableOwnerPID) uint32 { return x.numEntries })
	fuera := make([]domain.Listener, 0, len(filas))
	for i := range filas {
		fuera = append(fuera, domain.Listener{
			Proto:   domain.ProtoTCP,
			Port:    portOf(filas[i].localPort),
			Address: netip.AddrFrom16(filas[i].localAddr).Unmap().String(),
			PID:     int(filas[i].owningPID),
		})
	}
	return fuera
}

func readUDP4(buf []byte) []domain.Listener {
	var t mibUDPTableOwnerPID
	filas := rows[mibUDPRowOwnerPID, mibUDPTableOwnerPID](buf, unsafe.Offsetof(t.table),
		func(x *mibUDPTableOwnerPID) uint32 { return x.numEntries })
	fuera := make([]domain.Listener, 0, len(filas))
	for i := range filas {
		fuera = append(fuera, domain.Listener{
			Proto:   domain.ProtoUDP,
			Port:    portOf(filas[i].localPort),
			Address: addr4(filas[i].localAddr).String(),
			PID:     int(filas[i].owningPID),
		})
	}
	return fuera
}

func readUDP6(buf []byte) []domain.Listener {
	var t mibUDP6TableOwnerPID
	filas := rows[mibUDP6RowOwnerPID, mibUDP6TableOwnerPID](buf, unsafe.Offsetof(t.table),
		func(x *mibUDP6TableOwnerPID) uint32 { return x.numEntries })
	fuera := make([]domain.Listener, 0, len(filas))
	for i := range filas {
		fuera = append(fuera, domain.Listener{
			Proto:   domain.ProtoUDP,
			Port:    portOf(filas[i].localPort),
			Address: netip.AddrFrom16(filas[i].localAddr).Unmap().String(),
			PID:     int(filas[i].owningPID),
		})
	}
	return fuera
}
