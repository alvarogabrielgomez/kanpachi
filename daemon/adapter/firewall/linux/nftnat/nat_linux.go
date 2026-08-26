//go:build linux

package nftnat

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"

	"github.com/google/nftables"
	"github.com/google/nftables/binaryutil"
	"github.com/google/nftables/expr"
	"github.com/mdlayher/netlink"
	"golang.org/x/sys/unix"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// Los desplazamientos dentro de las cabeceras, en bytes. Los mismos que usa la
// compuerta, y por el mismo motivo: acá se habla nftables, no dominio.
const (
	offsetDstAddrV4 = 16
	lenAddrV4       = 4
	offsetDstPort   = 2
	lenPort         = 2
	reg1            = 1
)

// Lo que hace falta para tocar el devconf de IPv4 de una interfaz por
// netlink. Los tres salen de los headers del kernel y no de una librería,
// porque la que hay en el árbol no los expone. Ver [permitirLoopback].
const (
	iflaAFSpec           = 26 // IFLA_AF_SPEC
	iflaInetConf         = 1  // IFLA_INET_CONF
	devconfRouteLocalnet = 26 // IPV4_DEVCONF_ROUTE_LOCALNET, la lista empieza en 1
	tamañoIfinfomsg      = 16
)

// Apply deja puesto EXACTAMENTE el desvío que se le pide.
//
// Borra la tabla y la vuelve a escribir en lugar de calcular una diferencia:
// son dos o tres reglas, se reescriben en microsegundos, y una tabla que se
// reconstruye entera no puede acumular restos de un juego anterior. Es la misma
// elección que hace la compuerta con su conjunto.
//
// Un spec incompleto no se escribe: media regla de nat manda tráfico a
// cualquier parte. Ver [domain.RedirectSpec.Understood].
func (r *Redirect) Apply(ctx context.Context, spec domain.RedirectSpec) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !spec.Understood() {
		return fmt.Errorf("desvío incompleto: adaptador %q, sala %v, destino %v, %d rangos",
			spec.Adapter, spec.RoomIP, spec.To, len(spec.Ports))
	}
	if !spec.RoomIP.Is4() || !spec.To.Is4() {
		// La sala direcciona en IPv4 dentro de 100.64.0.0/10, así que un desvío
		// IPv6 sería una traducción de algo que la sala no encamina.
		return fmt.Errorf("el desvío es de IPv4: sala %v, destino %v", spec.RoomIP, spec.To)
	}

	if spec.To.IsLoopback() {
		if err := permitirLoopback(spec.Adapter); err != nil {
			return err
		}
	}

	idx, err := ifaceIndex(spec.Adapter)
	if err != nil {
		return err
	}

	c, err := nftables.New()
	if err != nil {
		return fmt.Errorf("abriendo netlink para el desvío: %w", err)
	}
	defer func() { _ = c.CloseLasting() }()

	if err := borrarTabla(c); err != nil {
		return err
	}

	política := nftables.ChainPolicyAccept
	tabla := c.AddTable(&nftables.Table{Family: nftables.TableFamilyIPv4, Name: TableName})
	cadena := c.AddChain(&nftables.Chain{
		Name:     ChainName,
		Table:    tabla,
		Type:     nftables.ChainTypeNAT,
		Hooknum:  nftables.ChainHookPrerouting,
		Priority: nftables.ChainPriorityNATDest,
		// ACCEPT, y es lo único correcto: una cadena de nat con política de
		// descarte tiraría todo lo que entra a la máquina y no solo lo de la
		// sala. Lo que decide acá es a dónde va lo que casa, jamás si pasa.
		Policy: &política,
	})

	// `both` se expande a una regla por protocolo, igual que hace la compuerta
	// con las suyas: una regla de nat lleva UN protocolo, así que el azúcar del
	// perfil se deshace acá y no en el dominio. Sin esto, un perfil con un rango
	// `both` no conseguía desvío ninguno, porque [l4proto] lo rechazaba entero.
	for _, p := range expandProto(spec.Ports) {
		regla, err := rule(tabla, cadena, idx, spec, p)
		if err != nil {
			return err
		}
		c.AddRule(regla)
	}

	if err := c.Flush(); err != nil {
		return fmt.Errorf("escribiendo el desvío hacia %v: %w", spec.To, err)
	}
	return nil
}

// Clear borra la tabla entera, y no falla si no estaba.
//
// Se llama al salir de la sala, al quitar el juego y cada vez que la medición
// dice que ya no hace falta, así que "no había nada" es el caso normal y no un
// error. Es lo que hace que una salida sucia no deje una traducción puesta.
func (r *Redirect) Clear(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c, err := nftables.New()
	if err != nil {
		return fmt.Errorf("abriendo netlink para quitar el desvío: %w", err)
	}
	defer func() { _ = c.CloseLasting() }()

	if err := borrarTabla(c); err != nil {
		return err
	}
	if err := c.Flush(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("quitando el desvío: %w", err)
	}
	return nil
}

// borrarTabla encola el borrado de la tabla propia SI EXISTE.
//
// La comprobación no es cortesía: el borrado va en el mismo lote que la
// escritura, y el kernel contesta ENOENT a borrar lo que no está, lo que tumba
// el lote ENTERO. Medido el 2026-08-20 en un contenedor privilegiado: la
// primera aplicación fallaba siempre con «netlink receive: no such file or
// directory», y era esto, no el desvío.
func borrarTabla(c *nftables.Conn) error {
	tablas, err := c.ListTablesOfFamily(nftables.TableFamilyIPv4)
	if err != nil {
		return fmt.Errorf("listando tablas de nat: %w", err)
	}
	for _, t := range tablas {
		if t.Name == TableName {
			c.DelTable(t)
			return nil
		}
	}
	return nil
}

// rule traduce un rango del perfil a la regla que lo desvía.
//
// Las cuatro condiciones van juntas y ninguna sobra: la interfaz de entrada
// (solo lo que llega POR la sala), la dirección de destino (solo lo que iba a
// la dirección de la sala, no lo que la máquina se manda a sí misma), el
// protocolo y el puerto. Sin la primera, el desvío alcanzaría al tráfico de la
// red de casa; sin la segunda, al de cualquier otra dirección de la máquina.
func rule(
	t *nftables.Table, c *nftables.Chain, iface int,
	spec domain.RedirectSpec, p domain.PortRange,
) (*nftables.Rule, error) {
	l4, err := l4proto(p.Proto)
	if err != nil {
		return nil, err
	}
	sala := spec.RoomIP.As4()
	hacia := spec.To.As4()

	exprs := []expr.Any{
		&expr.Meta{Key: expr.MetaKeyIIF, Register: reg1},
		&expr.Cmp{
			Op: expr.CmpOpEq, Register: reg1,
			// Índice de interfaz: un u32 del kernel, o sea orden de la máquina.
			Data: binaryutil.NativeEndian.PutUint32(uint32(iface)),
		},
		&expr.Payload{
			DestRegister: reg1, Base: expr.PayloadBaseNetworkHeader,
			Offset: offsetDstAddrV4, Len: lenAddrV4,
		},
		&expr.Cmp{Op: expr.CmpOpEq, Register: reg1, Data: sala[:]},
		&expr.Meta{Key: expr.MetaKeyL4PROTO, Register: reg1},
		&expr.Cmp{Op: expr.CmpOpEq, Register: reg1, Data: []byte{l4}},
		&expr.Payload{
			DestRegister: reg1, Base: expr.PayloadBaseTransportHeader,
			Offset: offsetDstPort, Len: lenPort,
		},
	}

	if p.From == p.To {
		exprs = append(exprs, &expr.Cmp{
			Op: expr.CmpOpEq, Register: reg1,
			Data: binaryutil.BigEndian.PutUint16(p.From),
		})
	} else {
		// Un rango es UNA expresión de rango, igual que en la compuerta: el
		// catálogo no acota la amplitud, y expandir 27000-27100 daría cien
		// reglas de nat para un solo perfil.
		exprs = append(exprs, &expr.Range{
			Op: expr.CmpOpEq, Register: reg1,
			FromData: binaryutil.BigEndian.PutUint16(p.From),
			ToData:   binaryutil.BigEndian.PutUint16(p.To),
		})
	}

	// El puerto NO se traduce, solo la dirección. Lo que el miembro escribió es
	// el puerto del juego, y el juego escucha en ese mismo puerto en la otra
	// dirección: lo único que estaba mal era a dónde iba.
	exprs = append(exprs,
		&expr.Immediate{Register: reg1, Data: hacia[:]},
		&expr.NAT{
			Type:       expr.NATTypeDestNAT,
			Family:     unix.NFPROTO_IPV4,
			RegAddrMin: reg1,
		},
	)

	return &nftables.Rule{Table: t, Chain: c, Exprs: exprs}, nil
}

// expandProto deshace [domain.ProtoBoth] en los dos protocolos que describe.
//
// Vive acá y no en el dominio porque es una limitación de ESTA capa: una regla
// de nat, como una de firewall, lleva un protocolo y solo uno. El perfil
// declara el rango una vez a propósito.
func expandProto(ports []domain.PortRange) []domain.PortRange {
	out := make([]domain.PortRange, 0, len(ports)+1)
	for _, p := range ports {
		if p.Proto != domain.ProtoBoth {
			out = append(out, p)
			continue
		}
		tcp, udp := p, p
		tcp.Proto = domain.ProtoTCP
		udp.Proto = domain.ProtoUDP
		out = append(out, tcp, udp)
	}
	return out
}

func l4proto(p domain.Proto) (byte, error) {
	switch p {
	case domain.ProtoTCP:
		return unix.IPPROTO_TCP, nil
	case domain.ProtoUDP:
		return unix.IPPROTO_UDP, nil
	default:
		return 0, fmt.Errorf("protocolo que no se puede desviar: %v", p)
	}
}

// ifaceIndex resuelve el adaptador de la sala a su índice.
//
// Por índice y no por nombre, igual que la compuerta: el nombre lo elige el
// motor al crear el adaptador y el índice es lo que el kernel usa. Que no
// exista es un error de verdad y no un caso normal: sin adaptador no hay sala
// que desviar.
func ifaceIndex(name string) (int, error) {
	dev, err := net.InterfaceByName(name)
	if err != nil {
		return 0, fmt.Errorf("buscando el adaptador %q para el desvío: %w", name, err)
	}
	return dev.Index, nil
}

// permitirLoopback deja que un paquete que entra por el adaptador de la sala
// llegue a un destino en `127.0.0.0/8`.
//
// # Por qué hace falta, medido
//
// Sin esto el núcleo lo tira como marciano ANTES de entregarlo, y la regla de
// nat ni se entera: casa, traduce, y el datagrama muere. Reproducido en un
// netns limpio, con un oyente UDP en `127.0.1.1` y esta misma forma de regla:
//
//	route_localnet=0  bytes entregados: 0
//	route_localnet=1  bytes entregados: 4
//
// # Por qué por netlink y no escribiendo en /proc/sys
//
// Porque en un contenedor `/proc/sys` está montado de solo lectura, y subir a
// `CAP_SYS_ADMIN` para remontarlo sería pedir el permiso que da la máquina
// entera por tocar un bit. Medido el 2026-08-26 contra el pod real:
//
//	no se pudo desviar hacia donde escucha el juego [hacia 127.0.1.1
//	error ... open /proc/sys/net/ipv4/conf/kanpachi0/route_localnet:
//	read-only file system]
//
// El kernel expone el mismo ajuste por `RTM_NEWLINK`, dentro de
// `IFLA_AF_SPEC`, y eso solo pide `CAP_NET_ADMIN`, que es lo que el daemon ya
// tiene para crear el adaptador. Comprobado en WSL sobre una interfaz dummy:
// `/proc` pasa de 0 a 1 sin que nadie escriba en `/proc`.
//
// **`IFLA_INET_CONF` es NESTED y cada hijo lleva el id del ajuste por TIPO.**
// Mandado como array plano de u32, que es la forma que tiene en memoria, el
// kernel contesta ACK y no aplica nada: el primer intento salió "bien" y dejó
// el bit en cero.
//
// # Qué abre exactamente, y qué NO
//
// Es por interfaz, y la interfaz es la de Kanpachi. No toca `all`, no toca
// `default`, y no toca ninguna otra tarjeta de la máquina: lo que entre por
// eth0 con destino a loopback se sigue tirando igual que siempre.
//
// Lo que llega por el adaptador de la sala ya pasa por la compuerta, que
// descarta por omisión y solo deja los puertos del juego hacia los miembros.
// Y el conjunto deseado se reescribe con la dirección traducida dentro, por
// [domain.RuleSet.MirrorLocal], así que el permiso nombra ese `127.x` y esos
// puertos y nada más. Sin esa parte, esto abriría el enrutado y la compuerta
// seguiría tirando el paquete.
//
// # Por qué no se devuelve a cero al quitar el desvío
//
// Porque el adaptador es de Kanpachi y muere con la sala: cuando se va, se
// lleva su devconf con él. Restaurarlo a mano solo importaría si la interfaz
// sobreviviera al desvío, y no lo hace.
func permitirLoopback(adaptador string) error {
	ifi, err := net.InterfaceByName(adaptador)
	if err != nil {
		return fmt.Errorf("buscando %s para habilitar el enrutado a loopback: %w", adaptador, err)
	}

	ae := netlink.NewAttributeEncoder()
	ae.Nested(iflaAFSpec, func(af *netlink.AttributeEncoder) error {
		af.Nested(unix.AF_INET, func(inet *netlink.AttributeEncoder) error {
			inet.Nested(iflaInetConf, func(conf *netlink.AttributeEncoder) error {
				conf.Uint32(devconfRouteLocalnet, 1)
				return nil
			})
			return nil
		})
		return nil
	})
	atributos, err := ae.Encode()
	if err != nil {
		return fmt.Errorf("armando el mensaje de netlink para %s: %w", adaptador, err)
	}

	// struct ifinfomsg: family, pad, type, index, flags, change. El índice va
	// en el offset 4, y ponerlo en el 8 es lo que el kernel contesta como
	// "no such device" aunque la interfaz exista.
	cabecera := make([]byte, tamañoIfinfomsg)
	cabecera[0] = unix.AF_UNSPEC
	binary.NativeEndian.PutUint32(cabecera[4:8], uint32(ifi.Index))

	c, err := netlink.Dial(unix.NETLINK_ROUTE, nil)
	if err != nil {
		return fmt.Errorf("abriendo netlink para el enrutado a loopback: %w", err)
	}
	defer func() { _ = c.Close() }()

	if _, err := c.Execute(netlink.Message{
		Header: netlink.Header{
			Type:  unix.RTM_NEWLINK,
			Flags: netlink.Request | netlink.Acknowledge,
		},
		Data: append(cabecera, atributos...),
	}); err != nil {
		return fmt.Errorf("habilitando el enrutado a loopback en %s: %w", adaptador, err)
	}
	return nil
}
