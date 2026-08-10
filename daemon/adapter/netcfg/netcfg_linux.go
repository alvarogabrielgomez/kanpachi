//go:build linux

package netcfg

// Los ajustes del adaptador virtual en Linux.
//
// # Este fichero es MUCHO más corto que el de Windows, y no está a medias
//
// La cabecera del paquete dice que Windows "revierte la métrica, la categoría de
// red y las rutas en cada evento de identificación de red". Eso es una conducta
// de Windows y acá no pasa: lo que se escribe en un enlace se queda hasta que
// alguien lo cambie. Así que la mitad del trabajo del adaptador de Windows es
// pelear con el sistema, y esa mitad acá no existe.
//
// Lo que queda es lo que sí es del producto:
//
//   - **Que en el adaptador de la sala NO haya ruta por defecto.** Kanpachi
//     jamás enruta internet, y esto importa MÁS acá que en Windows: en Linux
//     EasyTier programa rutas por netlink él mismo, incluidas las que aprende de
//     la red, así que la ruta que hay que borrar la puede poner otro.
//   - Las rutas de descubrimiento que pide el perfil del juego.
//   - El MTU, cuando alguien lo haya sondeado.
//
// Y lo que NO tiene análogo se dice, no se finge: la política de prefijo IPv6 y
// DirectPlay son componentes de Windows. Ver [Config.SetDirectPlay].

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"net/netip"
	"sync"
	"syscall"
	"unsafe"

	"github.com/mdlayher/netlink"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/core/port"
)

// Config implements [port.NetConfigPort].
type Config struct {
	log port.Logger

	// mu serialises the whole adapter pass, same as in Windows: the supervisor
	// reapplies on a timer, so two passes overlapping is the normal case.
	mu sync.Mutex
}

// New toma el directorio de datos y no lo usa, a propósito.
//
// En Windows ahí vive el libro de ajustes, que anota el valor ANTERIOR de las
// dos cosas que sobreviven a un reinicio para poder deshacerlas. Acá ese libro
// estaría siempre vacío: no hay ningún ajuste de esta capa que sobreviva a la
// sala, porque el adaptador lo crea el motor por sala y se lleva sus rutas y su
// MTU al morir. Un libro vacío que se escribe igual es un fichero que confunde.
//
// El parámetro se conserva para que el cableado sea el mismo en los dos
// sistemas, que es lo que permite que `main.go` no pregunte dónde corre.
func New(_ string, log port.Logger) *Config { return &Config{log: log} }

// The check that this still fits the port. If a signature in core changes, the
// error comes out here and not where it gets wired.
var _ port.NetConfigPort = (*Config)(nil)

// ApplyAdapter brings the virtual adapter to the requested state.
//
// Nada de acá es fatal por sí solo, igual que en Windows y por lo mismo: un MTU
// sin escribir degrada la partida y una ruta que falta rompe el descubrimiento,
// mientras que abortar la sala deja al usuario sin nada en vez de con algo
// imperfecto. Se intentan todos los pasos y se juntan los fallos.
func (c *Config) ApplyAdapter(_ context.Context, want domain.AdapterState) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !want.Address.IsValid() {
		return nil // sin sala no hay adaptador que ajustar
	}
	iface, err := net.InterfaceByName(domain.AdapterName)
	if err != nil {
		return fmt.Errorf("buscando %s: %w", domain.AdapterName, err)
	}

	var fallos []error
	añadir := func(qué string, err error) {
		if err != nil {
			fallos = append(fallos, fmt.Errorf("%s: %w", qué, err))
		}
	}

	añadir("MTU", setMTU(iface.Name, mtuFor(want)))
	añadir("rutas", c.syncRoutes(iface.Index, wantedRoutes(want)))

	// La política de prefijo IPv6 NO se toca, y es una decisión.
	//
	// El análogo de `netsh interface ipv6 set prefixpolicy` acá es
	// `/etc/gai.conf`, que es un fichero de configuración de la MÁQUINA y no del
	// adaptador. Editarlo cambiaría cómo resuelve direcciones todo lo que corre
	// en ese servidor, para siempre y para todos, por una preferencia de una
	// sala. Es exactamente lo que este producto se niega a hacerle a la máquina
	// de otro, la misma regla por la que no se toca ufw ni firewalld.
	_ = want.PreferIPv4

	if len(fallos) > 0 {
		return fmt.Errorf("ajustando %s: %v", domain.AdapterName, fallos)
	}
	return nil
}

// RevertTweaks no tiene nada que deshacer, y eso es verdad, no un provisional.
//
// En Windows deshace las dos cosas que sobreviven a la sala, la política de
// prefijo IPv6 y DirectPlay. Acá no se toca ninguna de las dos: la primera
// porque sería reconfigurar la máquina entera, la segunda porque no existe. Lo
// que sí se escribe, las rutas y el MTU, vive en un adaptador que el motor
// destruye al cerrar la sala.
func (c *Config) RevertTweaks(context.Context) error { return nil }

// ProbeMTU no está sondeado en Linux, y devolver cero es la forma de decirlo.
//
// Cero significa "todavía no se sondeó", y el llamador entonces deja el MTU que
// haya en vez de escribir uno inventado. Ver [domain.AdapterState].
//
// # Por qué no está, y qué haría falta
//
// En Windows lo mide `IcmpSendEcho`, que es una API que arma el ping, pone el
// bit de no fragmentar y espera la respuesta. Linux no tiene ese atajo: hay que
// abrir un socket ICMP, armar el paquete a mano y llevar la búsqueda binaria.
// Es hacible y no es gratis, y mientras no esté, EasyTier usa su valor por
// defecto, que es el mismo camino que ya se recorre en Windows cuando el router
// no contesta al ping.
func (c *Config) ProbeMTU(context.Context) (int, error) { return 0, nil }

// SetDirectPlay no hace nada porque DirectPlay es un componente de Windows.
//
// Devuelve nil y no un error, y la diferencia importa: un perfil que lo pide es
// un juego anterior a 2005, y fallar acá le negaría la sala a alguien por pedir
// algo que en este sistema no significa nada. Lo que no se hace es callarlo: se
// anota una vez, con el motivo.
func (c *Config) SetDirectPlay(_ context.Context, want bool) error {
	if want {
		c.log.Info("el perfil pide DirectPlay, que es un componente de Windows",
			"detalle", "en Linux no hay nada que encender, y el juego que lo pide corre en la "+
				"máquina de quien juega, no en esta")
	}
	return nil
}

// setMTU escribe el MTU del enlace con el ioctl de siempre.
//
// `SIOCSIFMTU` y no netlink, que es lo que se usa para las rutas unas líneas más
// abajo. El motivo es el tamaño: acá el mensaje es un `ifreq` de dieciséis bytes
// con un número, y armar un RTM_NEWLINK para lo mismo sería más código para el
// mismo efecto. Cero significa "no se sondeó" y no se escribe: un MTU de cero
// apaga la interfaz.
func setMTU(nombre string, mtu int) error {
	if mtu <= 0 {
		return nil
	}
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_DGRAM, 0)
	if err != nil {
		return fmt.Errorf("abriendo el socket de control: %w", err)
	}
	defer func() { _ = syscall.Close(fd) }()

	// ifreq: nombre de la interfaz en los primeros 16 bytes, y la unión detrás.
	var req struct {
		Name [16]byte
		MTU  int32
		_    [20]byte
	}
	copy(req.Name[:], nombre)
	req.MTU = int32(mtu)

	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd),
		uintptr(syscall.SIOCSIFMTU), uintptr(unsafe.Pointer(&req))); errno != 0 {
		return fmt.Errorf("poniendo el MTU %d en %s: %w", mtu, nombre, errno)
	}
	return nil
}

// syncRoutes deja en NUESTRO adaptador exactamente las rutas que se pidieron.
//
// # Los tres pasos, y por qué el primero es el que importa
//
//  1. **Se borra toda ruta por defecto que haya en nuestro adaptador.** No la
//     ponemos nosotros: EasyTier puede instalar rutas que aprendió de la red, y
//     una `0.0.0.0/0` en el adaptador de la sala mandaría el tráfico de internet
//     de esta máquina por el túnel. Es la invariante de [isDefaultRoute] y es la
//     razón principal de que esta función exista en Linux.
//  2. Se agregan las que pidió el perfil y no están.
//  3. Se borran las nuestras que ya no se piden, y SOLO esas dos. Lo que
//     EasyTier haya puesto para que la sala funcione no es asunto de esta capa.
func (c *Config) syncRoutes(ifindex int, want []netip.Prefix) error {
	conn, err := netlink.Dial(syscall.NETLINK_ROUTE, nil)
	if err != nil {
		return fmt.Errorf("abriendo netlink: %w", err)
	}
	defer func() { _ = conn.Close() }()

	puestas, err := routesOn(ifindex)
	if err != nil {
		return err
	}

	var fallos []error
	tiene := func(p netip.Prefix) bool {
		for _, q := range puestas {
			if q == p {
				return true
			}
		}
		return false
	}
	quiere := func(p netip.Prefix) bool {
		for _, q := range want {
			if q == p {
				return true
			}
		}
		return false
	}

	// 1. La ruta por defecto, la ponga quien la ponga.
	for _, p := range puestas {
		if !isDefaultRoute(p) {
			continue
		}
		if err := writeRoute(conn, syscall.RTM_DELROUTE, ifindex, p); err != nil {
			fallos = append(fallos, fmt.Errorf("borrando la ruta por defecto %s: %w", p, err))
			continue
		}
		c.log.Warn("se quitó una ruta por defecto del adaptador de la sala",
			"adaptador", domain.AdapterName, "ruta", p.String(),
			"detalle", "Kanpachi no enruta internet, y esa ruta no la puso Kanpachi")
	}

	// 2. Las que faltan.
	for _, p := range want {
		if tiene(p) {
			continue
		}
		if err := writeRoute(conn, syscall.RTM_NEWROUTE, ifindex, p); err != nil {
			fallos = append(fallos, fmt.Errorf("agregando %s: %w", p, err))
		}
	}

	// 3. Las nuestras que sobran. La lista de candidatas son LAS DOS que esta
	// capa sabe poner, y ninguna más: borrar cualquier otra sería llevarse por
	// delante lo que el motor necesita para que la sala funcione.
	for _, p := range []netip.Prefix{BroadcastRoute, MulticastRoute} {
		if quiere(p) || !tiene(p) {
			continue
		}
		if err := writeRoute(conn, syscall.RTM_DELROUTE, ifindex, p); err != nil {
			fallos = append(fallos, fmt.Errorf("borrando %s: %w", p, err))
		}
	}

	if len(fallos) > 0 {
		return fmt.Errorf("%v", fallos)
	}
	return nil
}

// routesOn lista los destinos que hay ahora mismo en una interfaz.
func routesOn(ifindex int) ([]netip.Prefix, error) {
	raw, err := syscall.NetlinkRIB(syscall.RTM_GETROUTE, syscall.AF_UNSPEC)
	if err != nil {
		return nil, fmt.Errorf("leyendo las rutas: %w", err)
	}
	msgs, err := syscall.ParseNetlinkMessage(raw)
	if err != nil {
		return nil, fmt.Errorf("leyendo la respuesta del kernel: %w", err)
	}

	var out []netip.Prefix
	for i := range msgs {
		m := &msgs[i]
		if m.Header.Type != syscall.RTM_NEWROUTE || len(m.Data) < syscall.SizeofRtMsg {
			continue
		}
		rtm := (*syscall.RtMsg)(unsafe.Pointer(&m.Data[0]))
		attrs, err := syscall.ParseNetlinkRouteAttr(m)
		if err != nil {
			continue
		}

		var dst []byte
		mía := false
		for _, a := range attrs {
			switch a.Attr.Type {
			case syscall.RTA_DST:
				dst = a.Value
			case syscall.RTA_OIF:
				if len(a.Value) == 4 {
					mía = int(binary.NativeEndian.Uint32(a.Value)) == ifindex
				}
			}
		}
		if !mía {
			continue
		}
		// Sin destino es la ruta por defecto, que acá SÍ hay que ver: es
		// justamente la que hay que borrar. Se arma con la dirección cero de su
		// familia, que es lo que el kernel omitió por ser toda ceros.
		if dst == nil {
			if rtm.Family == syscall.AF_INET6 {
				out = append(out, netip.PrefixFrom(netip.IPv6Unspecified(), int(rtm.Dst_len)))
			} else {
				out = append(out, netip.PrefixFrom(netip.IPv4Unspecified(), int(rtm.Dst_len)))
			}
			continue
		}
		if addr, ok := netip.AddrFromSlice(dst); ok {
			out = append(out, netip.PrefixFrom(addr.Unmap(), int(rtm.Dst_len)))
		}
	}
	return out, nil
}

// writeRoute agrega o borra una ruta en una interfaz.
//
// `netlink.Conn.Execute` es lo que hace que esto quepa en treinta líneas: manda
// el mensaje, espera el ACK y convierte el error del kernel en un error de Go.
// Escrito a mano serían tres veces más líneas, y el trozo que se ahorra es
// justo donde un fallo se lee como éxito.
func writeRoute(conn *netlink.Conn, tipo int, ifindex int, p netip.Prefix) error {
	familia := byte(syscall.AF_INET)
	if p.Addr().Is6() {
		familia = syscall.AF_INET6
	}

	// rtmsg, doce bytes: familia, longitud del destino, longitud del origen,
	// tos, tabla, protocolo, alcance, tipo, y las banderas.
	msg := []byte{
		familia,
		byte(p.Bits()),
		0, 0,
		syscall.RT_TABLE_MAIN,
		syscall.RTPROT_STATIC,
		// SCOPE_LINK: el destino está en el enlace, sin salto intermedio. Es lo
		// que corresponde a las dos rutas que esta capa pone, que son la de
		// difusión y la de multidifusión.
		syscall.RT_SCOPE_LINK,
		syscall.RTN_UNICAST,
		0, 0, 0, 0,
	}

	ae := netlink.NewAttributeEncoder()
	ae.Bytes(syscall.RTA_DST, p.Addr().AsSlice())
	ae.Uint32(syscall.RTA_OIF, uint32(ifindex))
	attrs, err := ae.Encode()
	if err != nil {
		return fmt.Errorf("armando los atributos: %w", err)
	}

	banderas := netlink.Request | netlink.Acknowledge
	if tipo == syscall.RTM_NEWROUTE {
		// Create y Replace juntos: si ya está, se pisa en vez de fallar con
		// EEXIST. Esta función la llama un supervisor que reaplica en bucle, así
		// que "ya estaba" es el caso normal y no un error.
		banderas |= netlink.Create | netlink.Replace
	}

	_, err = conn.Execute(netlink.Message{
		Header: netlink.Header{Type: netlink.HeaderType(tipo), Flags: banderas},
		Data:   append(msg, attrs...),
	})
	return err
}
