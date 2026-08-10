package nft

// Los conjuntos anónimos que acotan por origen cuando hay varios miembros.
//
// nftables une varios valores del mismo campo con un conjunto, que es el
// equivalente exacto del O que WFP aplica a las condiciones del mismo campo. La
// diferencia es que acá el conjunto es un objeto que hay que declarar antes de
// la regla que lo mira.

import (
	"fmt"
	"strconv"

	"github.com/google/nftables"

	"net/netip"
)

// setBuilder acumula los conjuntos que hacen falta para un lote de reglas.
//
// Existe porque el conjunto y la regla que lo usa viajan en el MISMO lote de
// netlink, y el conjunto tiene que ir antes. Declararlos sobre la marcha y
// vaciarlos justo antes de cada regla es lo que garantiza ese orden sin que
// quien traduce tenga que pensar en el lote.
type setBuilder struct {
	next     int
	pendings []pendingSet
}

type pendingSet struct {
	set   *nftables.Set
	elems []nftables.SetElement
}

// exact declara un conjunto de coincidencia EXACTA con estas direcciones.
//
// Sin intervalos, a propósito. Ver [remoteExprs] para el porqué: un intervalo
// mal cerrado casa con direcciones que nadie pidió, y en un permiso eso es un
// puerto abierto a quien no es de la sala.
//
// `Constant` porque el contenido no cambia mientras la regla viva: cambiar los
// miembros de la sala reescribe la cadena entera, que es como se aplica todo
// acá.
func (b *setBuilder) exact(t *nftables.Table, label string, addrs []netip.Addr) (*nftables.Set, error) {
	if len(addrs) == 0 {
		return nil, fmt.Errorf("filtro %q: conjunto de origen vacío, y vacío en nftables no "+
			"casa con nada, así que el permiso quedaría puesto sin abrir", label)
	}

	elems := make([]nftables.SetElement, 0, len(addrs))
	vistas := make(map[netip.Addr]struct{}, len(addrs))
	for _, a := range addrs {
		if _, dup := vistas[a]; dup {
			// El kernel rechaza el lote entero por un elemento repetido, así que
			// un miembro duplicado tiraría la sala. Quitarlo acá es lo correcto:
			// un conjunto con la dirección una vez significa lo mismo.
			continue
		}
		vistas[a] = struct{}{}
		v4, err := as4(a)
		if err != nil {
			return nil, fmt.Errorf("filtro %q, dirección remota: %w", label, err)
		}
		elems = append(elems, nftables.SetElement{Key: v4})
	}

	b.next++
	s := &nftables.Set{
		Table:     t,
		Anonymous: true,
		Constant:  true,
		KeyType:   nftables.TypeIPAddr,
		// Un conjunto anónimo igual necesita un nombre único dentro del lote: es
		// por donde la regla lo referencia antes de que el kernel le asigne el
		// suyo.
		Name: "__kanpachi" + strconv.Itoa(b.next),
		ID:   uint32(b.next),
	}
	b.pendings = append(b.pendings, pendingSet{set: s, elems: elems})
	return s, nil
}

// flushInto mete en el lote los conjuntos declarados desde la última vez.
//
// Se llama ANTES de añadir la regla que los mira, que es el orden que el kernel
// exige dentro de un lote.
func (b *setBuilder) flushInto(c *nftables.Conn) error {
	for _, p := range b.pendings {
		if err := c.AddSet(p.set, p.elems); err != nil {
			return fmt.Errorf("declarando el conjunto %s: %w", p.set.Name, err)
		}
	}
	b.pendings = nil
	return nil
}
