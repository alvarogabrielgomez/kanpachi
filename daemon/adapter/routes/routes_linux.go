//go:build linux

package routes

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"syscall"
	"unsafe"

	"github.com/accentiostudios/kanpachi/core/port"
)

// Table reads this machine's addresses and routes.
//
// Same shape and same promise as the Windows one: no state, no cache. A server
// gets a new interface the day somebody installs Docker or brings up a VPN, so
// an answer is only true at the moment it is asked.
type Table struct{}

func New() *Table { return &Table{} }

// The check that this still fits the port, in the same place as in Windows so a
// signature change in core fails here and not where it gets wired.
var _ port.RoutingTable = (*Table)(nil)

// LocalPrefixes answers with both the addresses configured on every interface
// and the destinations in the routing table.
//
// # Reading the routes is NOT optional here, and on this system it matters more
//
// The case that breaks is common on exactly the machine this product is for: a
// server with Tailscale or WireGuard carries a **route** to a whole range while
// its interface only holds a single address inside it. Reading addresses alone,
// that range looks free, and a room placed there takes down the VPN the operator
// administers the machine with.
//
// Medido con una interfaz `dummy` con `100.100.100.1/32` y una ruta a
// `100.64.0.0/10`: sin leer rutas salen dos prefijos y el /10 no está; con
// rutas salen los dos. Es la forma exacta que tiene una VPN.
func (t *Table) LocalPrefixes(ctx context.Context) ([]netip.Prefix, error) {
	// Two reads of kernel state, on the order of milliseconds. There is nothing
	// to interrupt halfway, so the context is honoured once at the door rather
	// than pretending to be cancellable. Same as in Windows.
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	names, addrs, err := interfaceAddresses()
	if err != nil {
		return nil, err
	}
	rows, err := routeTable(names)
	if err != nil {
		return nil, err
	}
	return usable(append(addrs, rows...)), nil
}

// interfaceAddresses returns the index-to-name map and one entry per address.
//
// The map exists for the same reason as its Windows twin: the routing table
// names an interface by index and this package excludes Kanpachi's own
// interfaces by NAME. Without it, an open room's routes would look like somebody
// else's network, and the room that is up would veto the next one.
//
// `net.Interfaces` and not a netlink dump of its own. It is the standard
// library asking the kernel the same thing over the same socket, it already
// deals with the interface that disappears mid-enumeration, and what this
// package is for is the FILTERING, which is in `routes.go`.
func interfaceAddresses() (map[uint32]string, []entry, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, nil, fmt.Errorf("enumerando las interfaces de red: %w", err)
	}

	names := make(map[uint32]string, len(ifaces))
	var out []entry

	for _, i := range ifaces {
		names[uint32(i.Index)] = i.Name

		addrs, err := i.Addrs()
		if err != nil {
			// An interface that went away between the two calls costs its
			// addresses and not the whole answer. It is what happens when a VPN
			// comes down while a room is being opened.
			continue
		}
		for _, a := range addrs {
			red, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			// Through the text form on purpose. `net.IPNet` carries the address
			// in four bytes or in sixteen depending on where it came from, and
			// its mask in as many, so doing the arithmetic by hand means getting
			// the IPv4-mapped case right in a place nobody would come back to
			// read. A mask this cannot express comes out as something
			// [netip.ParsePrefix] rejects, which costs that one address.
			p, err := netip.ParsePrefix(red.String())
			if err != nil {
				continue
			}
			out = append(out, entry{adapter: i.Name, prefix: p})
		}
	}
	return names, out, nil
}

// routeTable returns one entry per route destination, from EVERY table.
//
// # Why netlink and not /proc/net/route
//
// Because `/proc/net/route` only shows the main table and only IPv4, and both
// halves of that are a real hole here. Tailscale and systemd-networkd put
// routes in tables of their own, and a range this machine reaches through a
// policy-routing table is just as taken as one in the main table. The dump
// below returns every family and every table in one read.
//
// Medido, y por eso está escrito así: con `ip route add 172.30.0.0/16 dev X
// table 52` puesto, `/proc/net/route` **no lo menciona** y este dump sí. Un
// rango que se cree libre y no lo está es lo que rompe la red que el operador
// ya tenía, que es peor que no poder abrir la sala.
//
// Nothing new gets pulled in for it: `syscall.NetlinkRIB` and the two parsers are
// the same ones the standard library uses inside `net`.
func routeTable(names map[uint32]string) ([]entry, error) {
	// AF_UNSPEC asks for both families in a single dump.
	raw, err := syscall.NetlinkRIB(syscall.RTM_GETROUTE, syscall.AF_UNSPEC)
	if err != nil {
		return nil, fmt.Errorf("leyendo la tabla de rutas del sistema: %w", err)
	}
	msgs, err := syscall.ParseNetlinkMessage(raw)
	if err != nil {
		return nil, fmt.Errorf("leyendo la respuesta del kernel sobre las rutas: %w", err)
	}

	out := make([]entry, 0, len(msgs))
	for i := range msgs {
		m := &msgs[i]
		if m.Header.Type == syscall.NLMSG_DONE {
			break
		}
		if m.Header.Type != syscall.RTM_NEWROUTE || len(m.Data) < syscall.SizeofRtMsg {
			continue
		}
		rtm := (*syscall.RtMsg)(unsafe.Pointer(&m.Data[0]))

		// **Solo las unicast.** La tabla `local` está llena de RTN_LOCAL, una por
		// cada dirección de la máquina, y de RTN_BROADCAST; las primeras ya
		// vienen por el otro camino y las segundas no son un rango que nadie
		// tenga. Peor son RTN_BLACKHOLE y RTN_UNREACHABLE: son rutas que dicen
		// "acá no se llega", así que contarlas haría que el planificador esquive
		// un rango que está libre.
		if rtm.Type != syscall.RTN_UNICAST {
			continue
		}

		attrs, err := syscall.ParseNetlinkRouteAttr(m)
		if err != nil {
			continue
		}

		var dst []byte
		var oif uint32
		for _, a := range attrs {
			switch a.Attr.Type {
			case syscall.RTA_DST:
				dst = a.Value
			case syscall.RTA_OIF:
				if len(a.Value) == 4 {
					oif = *(*uint32)(unsafe.Pointer(&a.Value[0]))
				}
			}
		}
		// Sin destino es la ruta por defecto, que `worthAvoiding` descarta de
		// todas formas por ser la que solapa con todo. Se salta acá para no
		// inventar una dirección cero que después haya que volver a descartar.
		if dst == nil {
			continue
		}
		addr, ok := netip.AddrFromSlice(dst)
		if !ok {
			continue
		}

		// Una ruta en una interfaz que no está en el mapa se queda sin nombre, y
		// eso solo significa que no se la puede excluir como nuestra.
		out = append(out, entry{
			adapter: names[oif],
			// PrefixFrom devuelve el valor cero cuando la longitud no le cabe a
			// la familia, y la mitad pura descarta los prefijos inválidos, así
			// que una fila rara cuesta una entrada y no la respuesta entera.
			prefix: netip.PrefixFrom(addr.Unmap(), int(rtm.Dst_len)),
		})
	}
	return out, nil
}
