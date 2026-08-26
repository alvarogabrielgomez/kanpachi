package nft

import (
	"testing"

	"github.com/google/nftables/binaryutil"
	"github.com/google/nftables/expr"
)

// TestLaMedidaMiraLoQueLaReglaDICEYNoSoloQueEsté.
//
// La medida daba por puesta una ranura con solo encontrar una regla que
// llevara su comentario, sin mirar ni una de sus expresiones. Con eso, una
// compuerta atada a un índice de interfaz que ya no existe —el motor recrea el
// adaptador y el índice cambia— informa todo `[applied]` con todos los
// bloqueos inertes: la regla está, no casa con nada, y no bloquea nada.
//
// Un bloqueo que no casa con nada no falla ruidosamente. Deja la sala
// descubierta en silencio, que es la forma exacta del fallo de treinta y tres
// horas del 2026-08-25.
func TestLaMedidaMiraLoQueLaReglaDICEYNoSoloQueEsté(t *testing.T) {
	conIface := func(idx uint32) []expr.Any {
		return []expr.Any{
			&expr.Meta{Key: expr.MetaKeyIIF, Register: reg1},
			&expr.Cmp{
				Op: expr.CmpOpEq, Register: reg1,
				Data: binaryutil.NativeEndian.PutUint32(idx),
			},
			&expr.Verdict{},
		}
	}

	if got, ok := ifaceOf(conIface(42)); !ok || got != 42 {
		t.Fatalf("ifaceOf = %d, %v; se esperaba 42, true", got, ok)
	}
	if _, ok := ifaceOf([]expr.Any{&expr.Verdict{}}); ok {
		t.Fatal("una regla sin acotar por interfaz se leyó como acotada")
	}
	// El índice es un u32 del kernel, o sea orden de la máquina. Leerlo en orden
	// de red daría otro número, y ese es justo el fallo que la regla documenta
	// al escribirlo.
	if got, _ := ifaceOf(conIface(1)); got != 1 {
		t.Fatalf("ifaceOf leyó el índice con el orden de bytes equivocado: %d", got)
	}
}
