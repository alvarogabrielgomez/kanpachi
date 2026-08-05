//go:build windows

package netcfg

import (
	"context"
	"fmt"
	"net/netip"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// El sondeo del MTU, y el síntoma que evita.
//
// WireGuard usa 1420 por defecto sobre un camino de 1500. PPPoE da 1492, el
// móvil y el 5G suelen dar menos, e IPv6 exige 1280 como mínimo. El síntoma de
// equivocarse es cruel para un juego y no se parece a un problema de red: el
// túnel levanta, el ping anda, la partida conecta, y el mundo no termina de
// cargar. Los paquetes chicos pasan y los grandes desaparecen en silencio,
// porque el ICMP que avisaría del tamaño va filtrado en algún salto.
const (
	// icmpOverhead son las cabeceras IPv4 (20) más las de ICMP (8), que es lo
	// que hay que sumarle a la carga útil para obtener el MTU del camino.
	icmpOverhead = 28
	// probeTimeout es lo que se espera a cada respuesta, en milisegundos.
	//
	// Corto a propósito, y el número importa: esto corre DENTRO de crear una
	// sala. Se le marca al router de casa, que contesta en milisegundos de un
	// dígito, así que medio segundo es cien veces de margen. Con los pasos que
	// fallan, que son la mitad de una búsqueda binaria de ocho, el peor caso son
	// unos dos segundos, y un router que no conteste al ping cuesta cuatro.
	//
	// Con el valor anterior, 1500, crear una sala se pasaba del plazo de la API
	// local con la red ya levantada. Medido.
	probeTimeout = 500
	// ipFlagDF es IP_FLAG_DF: no fragmentar. Es lo que convierte un ping en una
	// medición, porque sin él el camino trocea el paquete y contesta que sí.
	ipFlagDF = 0x02
)

// ipOptionInformation es IP_OPTION_INFORMATION de ipexport.h.
type ipOptionInformation struct {
	TTL         uint8
	TOS         uint8
	Flags       uint8
	OptionsSize uint8
	OptionsData *byte
}

// ProbeMTU mide el MTU del camino con ping de no fragmentar.
//
// # A quién se le marca, y qué NO cubre
//
// A la puerta de enlace por defecto, que es el router del usuario. Se eligió por
// dos razones: no hace falta contactar con nadie de fuera, lo que encaja con que
// este producto no habla con terceros; y es donde está el caso dominante de
// verdad, el enlace PPPoE que da 1492 en vez de 1500.
//
// **Lo que no cubre, dicho claro:** un estrechamiento a mitad de camino, más
// allá del primer salto. Para eso habría que marcar al otro extremo, y el otro
// extremo todavía no existe cuando esto corre. El valor que se saca es el techo
// del camino local, y con el margen del túnel se queda del lado seguro.
//
// Un fallo NO es fatal para el llamador: devolver cero significa "no se sondeó",
// y `netcfg` deja entonces el MTU que haya en vez de escribir uno inventado.
func (c *Config) ProbeMTU(ctx context.Context) (int, error) {
	gw, err := defaultGateway()
	if err != nil {
		return 0, err
	}

	h, _, _ := procIcmpCreateFile.Call()
	if h == uintptr(windows.InvalidHandle) || h == 0 {
		return 0, fmt.Errorf("IcmpCreateFile falló: %w", windows.GetLastError())
	}
	defer procIcmpCloseHandle.Call(h)

	// Búsqueda binaria sobre la carga útil, entre el mínimo que exige IPv6 y el
	// Ethernet de siempre. Ocho pasadas como mucho, que a un router de casa son
	// milisegundos.
	lo := domain.MinTunnelMTU - icmpOverhead
	hi := 1500 - icmpOverhead
	mejor := 0

	for lo <= hi {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		medio := (lo + hi) / 2
		if pingDF(h, gw, medio) {
			mejor = medio
			lo = medio + 1
		} else {
			hi = medio - 1
		}
	}

	if mejor == 0 {
		// Ni el paquete más chico pasó. Casi siempre es que el router no
		// contesta al ping, que es normal y no dice nada del MTU.
		return 0, fmt.Errorf("la puerta de enlace %v no contestó a ningún ping, "+
			"así que no se pudo medir el camino", gw)
	}
	camino := mejor + icmpOverhead
	c.log.Info("MTU del camino medido", "puerta", gw.String(), "camino", camino, "túnel", domain.TunnelMTU(camino))
	return camino, nil
}

// pingDF manda un eco de `payload` bytes con no fragmentar y dice si volvió.
func pingDF(h uintptr, dst netip.Addr, payload int) bool {
	datos := make([]byte, payload)

	opts := ipOptionInformation{TTL: 128, Flags: ipFlagDF}
	// El búfer de respuesta tiene que caber ICMP_ECHO_REPLY más los datos, y la
	// documentación pide 8 bytes de más para el mensaje de error. Se pide de
	// sobra: quedarse corto devuelve IP_GENERAL_FAILURE, que es indistinguible
	// de "no pasó" y haría medir un MTU más bajo del real.
	respuesta := make([]byte, payload+256)

	// La dirección va en orden de red y como IPv4 de 32 bits, que es lo que
	// espera IcmpSendEcho.
	v4 := dst.As4()
	destino := uint32(v4[0]) | uint32(v4[1])<<8 | uint32(v4[2])<<16 | uint32(v4[3])<<24

	var datosPtr *byte
	if payload > 0 {
		datosPtr = &datos[0]
	}

	n, _, _ := procIcmpSendEcho.Call(
		h,
		uintptr(destino),
		uintptr(unsafe.Pointer(datosPtr)),
		uintptr(uint16(payload)),
		uintptr(unsafe.Pointer(&opts)),
		uintptr(unsafe.Pointer(&respuesta[0])),
		uintptr(uint32(len(respuesta))),
		uintptr(uint32(probeTimeout)),
	)
	// Cero respuestas es que no pasó. Con DF puesto eso es, casi siempre,
	// IP_PACKET_TOO_BIG, que es exactamente lo que se está buscando.
	return n > 0
}

// defaultGateway busca el siguiente salto de la ruta por defecto.
//
// La de MENOR métrica, que es la que el sistema usaría de verdad. Una máquina
// con varias salidas, como cable y WiFi a la vez, tiene más de una.
func defaultGateway() (netip.Addr, error) {
	var table *windows.MibIpForwardTable2
	if err := windows.GetIpForwardTable2(windows.AF_INET, &table); err != nil {
		return netip.Addr{}, fmt.Errorf("leyendo la tabla de rutas: %w", err)
	}
	defer windows.FreeMibTable(unsafe.Pointer(table))

	mejor := netip.Addr{}
	mejorMetrica := ^uint32(0)

	for _, r := range table.Rows() {
		p, ok := prefixOf(&r.DestinationPrefix)
		if !ok || !isDefaultRoute(p) {
			continue
		}
		hop := (*windows.RawSockaddrInet4)(unsafe.Pointer(&r.NextHop))
		if hop.Family != windows.AF_INET {
			continue
		}
		addr := netip.AddrFrom4(hop.Addr)
		// Una ruta por defecto con siguiente salto sin especificar es de enlace
		// directo, y no hay a quién marcar.
		if addr.IsUnspecified() {
			continue
		}
		if r.Metric < mejorMetrica {
			mejor, mejorMetrica = addr, r.Metric
		}
	}

	if !mejor.IsValid() {
		return netip.Addr{}, fmt.Errorf("esta máquina no tiene puerta de enlace por defecto, " +
			"así que no hay a quién sondear el camino")
	}
	return mejor, nil
}
