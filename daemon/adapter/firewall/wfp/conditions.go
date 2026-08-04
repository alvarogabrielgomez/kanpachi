package wfp

// Las condiciones de un filtro, decididas sin tocar Windows.
//
// # Por qué esto no vive en el archivo de Windows
//
// Porque acá se deciden cuatro cosas que se equivocan calladas, y ninguna de las
// cuatro necesita a Windows para comprobarse:
//
//   - El orden de bytes de una dirección. WFP compara enteros en orden de host,
//     así que meter los bytes al revés produce un filtro que casa con otra red.
//   - La máscara de un prefijo. Un corrimiento de un bit multiplica por dos el
//     alcance de un bloqueo.
//   - Rango contra igualdad en el puerto. Un rango pedido como igualdad abre
//     solo el primer puerto, y el juego no funciona a medias: no funciona.
//   - Cuántas condiciones salen y de qué campo, que es lo que decide si WFP las
//     une con Y o con O.
//
// La parte de Windows traduce estos valores a las estructuras de la API y nada
// más. Es la misma partición que ya usan los dos adaptadores del firewall.

import (
	"fmt"
	"net/netip"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// Field es el campo del paquete que compara una condición.
//
// # La regla que hay que tener presente al leer todo esto
//
// WFP une con Y las condiciones de campos DISTINTOS, y con O las del MISMO
// campo. Por eso varias direcciones remotas significan "cualquiera de estas",
// que es lo que se quiere, y por eso [FilterSpec.Validate] prohíbe llevar a la
// vez red local y dirección local: ahí el O ensancharía el filtro.
type Field uint8

const (
	FieldLocalInterface Field = iota + 1
	FieldLocalAddress
	FieldLocalPort
	FieldProtocol
	FieldRemoteAddress
)

func (f Field) String() string {
	switch f {
	case FieldLocalInterface:
		return "adaptador"
	case FieldLocalAddress:
		return "dirección local"
	case FieldLocalPort:
		return "puerto local"
	case FieldProtocol:
		return "protocolo"
	case FieldRemoteAddress:
		return "dirección remota"
	default:
		return "campo-inválido"
	}
}

// Match es cómo se compara.
type Match uint8

const (
	MatchEqual Match = iota + 1
	MatchRange
)

// ValueKind es la forma del valor, y decide qué campo de [Condition] se lee.
type ValueKind uint8

const (
	// ValueNum es un entero, con el ancho que pida el campo.
	ValueNum ValueKind = iota + 1
	// ValueAddrMask es el par dirección y máscara, los dos en orden de host.
	ValueAddrMask
	// ValuePortRange son los dos extremos de un rango, ambos incluidos.
	ValuePortRange
)

// Width es el ancho del entero de un [ValueNum], en bits.
//
// No es cosmética: la API lleva el tipo del dato dentro del valor, y decirle 32
// donde esperaba 64 no falla, compara otra cosa.
type Width uint8

const (
	Width8  Width = 8
	Width16 Width = 16
	Width32 Width = 32
	Width64 Width = 64
)

// Condition es una comparación decidida, lista para que la capa de Windows la
// copie a una estructura de la API.
type Condition struct {
	Field Field
	Match Match
	Kind  ValueKind

	// Num y Width son el valor de un [ValueNum].
	Num   uint64
	Width Width

	// Addr y Mask son el par de un [ValueAddrMask], en orden de host.
	Addr, Mask uint32

	// From y To son los extremos de un [ValuePortRange].
	From, To uint16
}

// Expand traduce las condiciones a la lista que la API espera.
//
// El orden es fijo, y no por estética: dos cálculos con la misma entrada tienen
// que producir el mismo filtro, o el día que haya que comparar lo puesto contra
// lo deseado la comparación vería cambios donde no los hay.
func (c Conditions) Expand() ([]Condition, error) {
	var out []Condition

	if c.LUID != 0 {
		out = append(out, Condition{
			Field: FieldLocalInterface, Match: MatchEqual,
			Kind: ValueNum, Num: c.LUID, Width: Width64,
		})
	}

	if c.LocalNet.IsValid() {
		addr, mask, err := v4Mask(c.LocalNet)
		if err != nil {
			return nil, fmt.Errorf("red local: %w", err)
		}
		out = append(out, Condition{
			Field: FieldLocalAddress, Match: MatchEqual,
			Kind: ValueAddrMask, Addr: addr, Mask: mask,
		})
	}
	if c.LocalAddr.IsValid() {
		addr, err := v4(c.LocalAddr)
		if err != nil {
			return nil, fmt.Errorf("dirección local: %w", err)
		}
		out = append(out, Condition{
			Field: FieldLocalAddress, Match: MatchEqual,
			Kind: ValueNum, Num: uint64(addr), Width: Width32,
		})
	}

	if c.LocalPortFrom != 0 || c.LocalPortTo != 0 {
		if c.LocalPortFrom == c.LocalPortTo {
			out = append(out, Condition{
				Field: FieldLocalPort, Match: MatchEqual,
				Kind: ValueNum, Num: uint64(c.LocalPortFrom), Width: Width16,
			})
		} else {
			// Un rango sale como UNA condición de rango y no como una condición
			// por puerto: el catálogo no pone tope a la amplitud, así que
			// expandir `27000-27100` daría cien filtros para una sola regla.
			out = append(out, Condition{
				Field: FieldLocalPort, Match: MatchRange,
				Kind: ValuePortRange, From: c.LocalPortFrom, To: c.LocalPortTo,
			})
		}
	}

	if c.Proto != 0 {
		proto, err := ipProto(c.Proto)
		if err != nil {
			return nil, err
		}
		out = append(out, Condition{
			Field: FieldProtocol, Match: MatchEqual,
			Kind: ValueNum, Num: uint64(proto), Width: Width8,
		})
	}

	// Cada miembro es una condición suya del MISMO campo, y eso es lo que hace
	// que WFP las una con O. Una sola condición con varios valores no existe.
	for _, r := range c.Remote {
		addr, err := v4(r)
		if err != nil {
			return nil, fmt.Errorf("miembro %v: %w", r, err)
		}
		out = append(out, Condition{
			Field: FieldRemoteAddress, Match: MatchEqual,
			Kind: ValueNum, Num: uint64(addr), Width: Width32,
		})
	}
	for _, n := range c.RemoteNets {
		addr, mask, err := v4Mask(n)
		if err != nil {
			return nil, fmt.Errorf("rango remoto %v: %w", n, err)
		}
		out = append(out, Condition{
			Field: FieldRemoteAddress, Match: MatchEqual,
			Kind: ValueAddrMask, Addr: addr, Mask: mask,
		})
	}

	if len(out) == 0 {
		// [FilterSpec.Validate] lo caza antes y se recomprueba igual: un filtro
		// sin condiciones aplica a TODOS los adaptadores de la máquina, y siendo
		// un bloqueo duro deja al usuario sin la entrada de su red de casa.
		return nil, fmt.Errorf("no salió ninguna condición, y un filtro sin condiciones " +
			"aplica a todos los adaptadores de la máquina")
	}
	return out, nil
}

// v4 pasa una dirección al entero en orden de host que compara WFP.
//
// A mano y no con `binary.BigEndian`, para que se lea que el número ES la
// dirección: el primer octeto pesa más. Meterlo al revés produce un filtro
// perfectamente válido que casa con otra red, sin fallar en ningún sitio.
func v4(a netip.Addr) (uint32, error) {
	a = a.Unmap()
	if !a.Is4() {
		return 0, fmt.Errorf("%v no es IPv4, y la compuerta direcciona en IPv4", a)
	}
	b := a.As4()
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3]), nil
}

// v4Mask pasa un prefijo al par dirección y máscara que compara WFP.
func v4Mask(p netip.Prefix) (uint32, uint32, error) {
	if !p.IsValid() {
		return 0, 0, fmt.Errorf("prefijo vacío")
	}
	bits := p.Bits()
	if bits <= 0 || bits > 32 {
		// El cero se rechaza acá también: `0.0.0.0/0` casa con toda dirección
		// local de la máquina, que con un bloqueo duro es dejar al usuario sin
		// su red de casa.
		return 0, 0, fmt.Errorf("prefijo de %d bits, que no acota nada", bits)
	}

	addr, err := v4(p.Masked().Addr())
	if err != nil {
		return 0, 0, err
	}
	return addr, ^uint32(0) << (32 - bits), nil
}

func ipProto(p domain.Proto) (uint8, error) {
	switch p {
	case domain.ProtoTCP:
		return 6, nil
	case domain.ProtoUDP:
		return 17, nil
	}
	return 0, fmt.Errorf("protocolo %v sin traducción: [domain.BuildRuleSet] expande "+
		"`both` en dos reglas antes de llegar acá", p)
}
