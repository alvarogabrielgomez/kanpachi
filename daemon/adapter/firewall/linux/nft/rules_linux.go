package nft

// La traducción de un [gate.Spec] a una regla de nftables.
//
// # Por qué esto es de Linux y el modelo no
//
// Lo que se decide acá es de nftables entero: qué expresiones se emiten, en qué
// registro, y en qué orden de bytes va cada valor. Lo que NO es de nftables es
// qué hay que acotar, y eso vive en [gate.Spec], que Windows traduce a lo suyo.
//
// # Por qué lleva etiqueta de compilación, a diferencia de su gemelo de Windows
//
// `windows/wfp/conditions.go` no la lleva, porque construye estructuras propias
// y el CI de Linux puede comprobarlas. Acá no se puede: `github.com/google/
// nftables` importa `golang.org/x/sys/unix`, así que el fichero solo compila en
// Linux. Se comprueba corriéndolo en Linux, que es donde va a vivir.

import (
	"encoding/binary"
	"fmt"
	"net/netip"
	"strconv"
	"strings"

	"github.com/google/nftables"
	"github.com/google/nftables/binaryutil"
	"github.com/google/nftables/expr"
	"github.com/google/nftables/userdata"
	"golang.org/x/sys/unix"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/daemon/adapter/firewall/gate"
)

// Los desplazamientos dentro de la cabecera IPv4, en bytes.
const (
	offsetSrcAddrV4 = 12
	offsetDstAddrV4 = 16
	lenAddrV4       = 4

	// El puerto de destino dentro de la cabecera de transporte. Vale igual para
	// TCP y para UDP, que es lo que permite una sola expresión para los dos.
	offsetDstPort = 2
	lenPort       = 2
)

// El registro donde se cargan los valores antes de compararlos. Uno solo alcanza
// porque cada comparación consume el suyo antes de la siguiente.
const reg1 = 1

// ruleFor traduce un filtro decidido a la regla que se va a instalar.
//
// # El orden de las expresiones no es estético
//
// nftables las evalúa en secuencia y corta en la primera que no casa, así que
// van de más barata y más descartadora a más cara: familia, interfaz, dirección,
// protocolo, puerto, y el alcance remoto al final porque es el único que puede
// necesitar un conjunto.
func ruleFor(t *nftables.Table, c *nftables.Chain, s gate.Spec, sets *setBuilder) (*nftables.Rule, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}

	var exprs []expr.Any

	// La familia. La tabla es `inet`, o sea que ve IPv4 e IPv6 por la misma
	// cadena, así que sin esto un bloqueo IPv6 casaría también con IPv4.
	proto := byte(unix.NFPROTO_IPV4)
	if s.Layer == gate.InboundV6 {
		proto = unix.NFPROTO_IPV6
	}
	exprs = append(exprs,
		&expr.Meta{Key: expr.MetaKeyNFPROTO, Register: reg1},
		&expr.Cmp{Op: expr.CmpOpEq, Register: reg1, Data: []byte{proto}},
	)

	if s.Conditions.Iface != 0 {
		// `MetaKeyIIF` es el ÍNDICE de la interfaz de entrada, y no `IIFNAME`, que
		// es su nombre. Acotar por nombre parece más legible y es más frágil: el
		// nombre lo elige el motor al crear el adaptador y el índice es lo que el
		// kernel usa para encaminar.
		//
		// El índice es un u32 del kernel, o sea orden de la máquina. En orden de
		// red casaría con otra interfaz, o con ninguna, y un bloqueo que no casa
		// con nada no falla: deja la sala descubierta en silencio.
		exprs = append(exprs,
			&expr.Meta{Key: expr.MetaKeyIIF, Register: reg1},
			&expr.Cmp{
				Op: expr.CmpOpEq, Register: reg1,
				Data: binaryutil.NativeEndian.PutUint32(uint32(s.Conditions.Iface)),
			},
		)
	}

	// La dirección local es la de DESTINO del paquete que entra.
	switch {
	case s.Conditions.LocalAddr.IsValid():
		v4, err := as4(s.Conditions.LocalAddr)
		if err != nil {
			return nil, fmt.Errorf("dirección local: %w", err)
		}
		exprs = append(exprs,
			payload(offsetDstAddrV4, lenAddrV4),
			&expr.Cmp{Op: expr.CmpOpEq, Register: reg1, Data: v4},
		)
	case s.Conditions.LocalNet.IsValid():
		red, mascara, err := maskOf(s.Conditions.LocalNet)
		if err != nil {
			return nil, fmt.Errorf("red local: %w", err)
		}
		exprs = append(exprs,
			payload(offsetDstAddrV4, lenAddrV4),
			&expr.Bitwise{
				SourceRegister: reg1, DestRegister: reg1, Len: lenAddrV4,
				Mask: mascara, Xor: []byte{0, 0, 0, 0},
			},
			&expr.Cmp{Op: expr.CmpOpEq, Register: reg1, Data: red},
		)
	}

	if s.Conditions.Proto != 0 {
		p, err := l4proto(s.Conditions.Proto)
		if err != nil {
			return nil, err
		}
		exprs = append(exprs,
			&expr.Meta{Key: expr.MetaKeyL4PROTO, Register: reg1},
			&expr.Cmp{Op: expr.CmpOpEq, Register: reg1, Data: []byte{p}},
		)
	}

	if s.Conditions.LocalPortFrom != 0 || s.Conditions.LocalPortTo != 0 {
		exprs = append(exprs, payloadTransport(offsetDstPort, lenPort))
		if s.Conditions.LocalPortFrom == s.Conditions.LocalPortTo {
			exprs = append(exprs, &expr.Cmp{
				Op: expr.CmpOpEq, Register: reg1,
				Data: binaryutil.BigEndian.PutUint16(s.Conditions.LocalPortFrom),
			})
		} else {
			// Un rango sale como UNA expresión de rango y no como una regla por
			// puerto: el catálogo no pone tope a la amplitud, así que expandir
			// `27000-27100` daría cien reglas para un solo filtro.
			exprs = append(exprs, &expr.Range{
				Op: expr.CmpOpEq, Register: reg1,
				FromData: binaryutil.BigEndian.PutUint16(s.Conditions.LocalPortFrom),
				ToData:   binaryutil.BigEndian.PutUint16(s.Conditions.LocalPortTo),
			})
		}
	}

	remoto, err := remoteExprs(t, s, sets)
	if err != nil {
		return nil, err
	}
	exprs = append(exprs, remoto...)

	verdicto := expr.VerdictDrop
	if s.Action == gate.Permit {
		verdicto = expr.VerdictAccept
	}
	exprs = append(exprs, &expr.Verdict{Kind: verdicto})

	return &nftables.Rule{
		Table:    t,
		Chain:    c,
		Exprs:    exprs,
		UserData: userDataFor(s),
	}, nil
}

// remoteExprs acota por quién manda el paquete.
//
// # Por qué NO hay conjuntos de intervalos acá
//
// nftables une varios valores del mismo campo con un conjunto, y para prefijos
// eso pide un conjunto de INTERVALOS, que se representa con elementos de apertura
// y de cierre ordenados. Un intervalo mal cerrado no falla: casa con direcciones
// que no son las que se pidieron, y en un permiso eso es un puerto abierto a
// alguien que no es de la sala. Es exactamente la clase de fallo que esta capa
// existe para impedir.
//
// Lo que el dominio produce hoy no lo necesita, y está comprobado: los prefijos
// salen de un solo sitio, [domain.BuildRuleSet] para la puerta del vestíbulo, con
// UN prefijo y sin direcciones sueltas. Las reglas de juego llevan direcciones
// sueltas y ningún prefijo. Así que se cubren esos dos casos exactos y **se falla
// en cualquier otro**, con el mensaje diciendo qué hay que implementar.
//
// Fallar es lo correcto y no una pereza: una traducción aproximada de un alcance
// remoto abre de más, y abrir de más acá no se ve en ninguna pantalla.
func remoteExprs(t *nftables.Table, s gate.Spec, sets *setBuilder) ([]expr.Any, error) {
	addrs, nets := s.Conditions.Remote, s.Conditions.RemoteNets
	if len(addrs) == 0 && len(nets) == 0 {
		// Los bloqueos no acotan por origen a propósito: bloquean todo lo que
		// entre por el adaptador, venga de donde venga.
		return nil, nil
	}

	switch {
	case len(nets) == 1 && len(addrs) == 0:
		red, mascara, err := maskOf(nets[0])
		if err != nil {
			return nil, fmt.Errorf("filtro %q, red remota: %w", s.Label, err)
		}
		return []expr.Any{
			payload(offsetSrcAddrV4, lenAddrV4),
			&expr.Bitwise{
				SourceRegister: reg1, DestRegister: reg1, Len: lenAddrV4,
				Mask: mascara, Xor: []byte{0, 0, 0, 0},
			},
			&expr.Cmp{Op: expr.CmpOpEq, Register: reg1, Data: red},
		}, nil

	case len(nets) == 0 && len(addrs) == 1:
		v4, err := as4(addrs[0])
		if err != nil {
			return nil, fmt.Errorf("filtro %q, dirección remota: %w", s.Label, err)
		}
		return []expr.Any{
			payload(offsetSrcAddrV4, lenAddrV4),
			&expr.Cmp{Op: expr.CmpOpEq, Register: reg1, Data: v4},
		}, nil

	case len(nets) == 0:
		// Varias direcciones sueltas: conjunto de coincidencia EXACTA, sin
		// intervalos. Cada elemento es una dirección entera, así que no hay
		// forma de que el conjunto cubra una que no se pidió.
		set, err := sets.exact(t, s.Label, addrs)
		if err != nil {
			return nil, err
		}
		return []expr.Any{
			payload(offsetSrcAddrV4, lenAddrV4),
			&expr.Lookup{SourceRegister: reg1, SetName: set.Name, SetID: set.ID},
		}, nil
	}

	return nil, fmt.Errorf("filtro %q: alcance remoto con %d dirección(es) y %d red(es). "+
		"Hoy el dominio emite o una red sola, que es la puerta del vestíbulo, o solo "+
		"direcciones, que son los miembros. Traducir la mezcla pide un conjunto de "+
		"intervalos, y uno mal cerrado abre el puerto a quien no es de la sala sin que "+
		"nada lo diga. Se implementa acá el día que el dominio lo emita",
		s.Label, len(addrs), len(nets))
}

// addConntrack mete la regla que deja pasar lo que ya está establecido.
//
// Va la PRIMERA de la cadena, o sea antes que cualquier bloqueo. Ver el porqué
// en [Gate.Apply], que es donde se llama.
//
// No lleva alcance de adaptador a propósito. Acotarla a la interfaz de la sala
// dejaría fuera el vestíbulo, y acotarla a las dos obligaría a saber acá cuál es
// cuál. Lo que se deja pasar es tráfico de conexiones que esta máquina ya
// autorizó, así que no amplía nada: la decisión de dejar entrar la primera vez
// sigue siendo entera de los filtros de abajo.
func addConntrack(c *nftables.Conn, t *nftables.Table, ch *nftables.Chain) error {
	c.AddRule(&nftables.Rule{
		Table: t, Chain: ch,
		Exprs: []expr.Any{
			&expr.Ct{Register: reg1, Key: expr.CtKeySTATE},
			&expr.Bitwise{
				SourceRegister: reg1, DestRegister: reg1, Len: 4,
				Mask: binaryutil.NativeEndian.PutUint32(
					expr.CtStateBitESTABLISHED | expr.CtStateBitRELATED),
				Xor: binaryutil.NativeEndian.PutUint32(0),
			},
			// Distinto de cero, o sea que casó alguno de los dos bits.
			&expr.Cmp{
				Op: expr.CmpOpNeq, Register: reg1,
				Data: binaryutil.NativeEndian.PutUint32(0),
			},
			&expr.Verdict{Kind: expr.VerdictAccept},
		},
		UserData: userdata.AppendString(nil, userdata.TypeComment,
			commentPrefix+"ct] lo ya establecido, que en Windows lo resuelve la capa"),
	})
	return nil
}

func payload(offset, length uint32) *expr.Payload {
	return &expr.Payload{
		DestRegister: reg1, Base: expr.PayloadBaseNetworkHeader,
		Offset: offset, Len: length,
	}
}

func payloadTransport(offset, length uint32) *expr.Payload {
	return &expr.Payload{
		DestRegister: reg1, Base: expr.PayloadBaseTransportHeader,
		Offset: offset, Len: length,
	}
}

// as4 devuelve los cuatro bytes de una dirección IPv4, en orden de red.
//
// Rechaza lo que no sea IPv4 en vez de recortarlo: Kanpachi direcciona dentro de
// `100.64.0.0/10`, así que una dirección que no es v4 acá es un fallo de quien
// llama, y recortarla produciría un filtro que casa con otra cosa.
func as4(a netip.Addr) ([]byte, error) {
	if !a.Is4() {
		if !a.Is4In6() {
			return nil, fmt.Errorf("%v no es IPv4", a)
		}
		a = a.Unmap()
	}
	b := a.As4()
	return b[:], nil
}

// maskOf devuelve la red y la máscara de un prefijo, las dos en orden de red.
func maskOf(p netip.Prefix) ([]byte, []byte, error) {
	red, err := as4(p.Masked().Addr())
	if err != nil {
		return nil, nil, err
	}
	m := make([]byte, 4)
	binary.BigEndian.PutUint32(m, ^uint32(0)<<(32-p.Bits()))
	return red, m, nil
}

func l4proto(p domain.Proto) (byte, error) {
	switch p {
	case domain.ProtoTCP:
		return unix.IPPROTO_TCP, nil
	case domain.ProtoUDP:
		return unix.IPPROTO_UDP, nil
	default:
		// `both` no llega hasta acá: [domain.BuildRuleSet] lo expande en dos
		// reglas, y traducirlo como uno solo cerraría la mitad del juego.
		return 0, fmt.Errorf("protocolo %v: no se traduce a un número de protocolo", p)
	}
}

// commentPrefix es cómo empieza el comentario de toda regla nuestra.
//
// Lleva la POSICIÓN dentro del comentario, y no en un campo aparte, porque los
// tipos de userdata de una regla son un conjunto cerrado de dos
// (`TypeComment` y `TypeEbtablesPolicy`) y meter un tipo inventado deja una
// regla que `nft` no sabe leer. El comentario cumple los dos papeles a la vez:
// la posición es lo que permite releer la cadena y saber qué es cada regla sin
// recordar nada entre arranques, que es el mismo papel que cumple la GUID
// derivada en Windows, y la etiqueta es lo que se lee en `nft list ruleset`.
const commentPrefix = "kanpachi["

func userDataFor(s gate.Spec) []byte {
	c := commentPrefix + strconv.Itoa(s.Slot) + "] " + s.Label
	return userdata.AppendString(nil, userdata.TypeComment, c)
}

// slotOf lee la posición que dejó [userDataFor].
//
// Un `false` significa "esta regla no es nuestra", y quien lo lea tiene que
// tratarlo así: la cadena es nuestra, y aun así una regla sin nuestra marca es
// algo que puso otro, no algo que se pueda dar por propio.
func slotOf(ud []byte) (int, bool) {
	c, ok := userdata.GetString(ud, userdata.TypeComment)
	if !ok || !strings.HasPrefix(c, commentPrefix) {
		return 0, false
	}
	fin := strings.IndexByte(c, ']')
	if fin < 0 {
		return 0, false
	}
	slot, err := strconv.Atoi(c[len(commentPrefix):fin])
	if err != nil || slot < 0 || slot >= gate.MaxFilters {
		return 0, false
	}
	return slot, true
}
