package wfp

import (
	"testing"

	"github.com/accentiostudios/kanpachi/daemon/adapter/firewall/gate"
)

// Este test vivía en el paquete del modelo y se mudó con las claves. Lo que
// comprueba no cambió: la limpieza al arrancar tiene que encontrar lo que dejó
// la ejecución anterior sin recordar nada entre arranques.

func TestKeysAreStableAndDistinct(t *testing.T) {
	seen := map[[16]byte]int{}
	for slot := 0; slot < gate.MaxFilters; slot++ {
		k := keyForSlot(slot)
		if k != keyForSlot(slot) {
			t.Errorf("la clave de la posición %d cambia entre llamadas", slot)
		}
		if otra, dup := seen[k]; dup {
			t.Errorf("las posiciones %d y %d comparten clave", otra, slot)
		}
		seen[k] = slot

		// Y bien formada como UUID v4, que es lo que esperan las herramientas que
		// la muestren.
		if k[6]&0xf0 != 0x40 {
			t.Errorf("la clave de la posición %d no lleva la versión de UUID: %#x", slot, k[6])
		}
		if k[8]&0xc0 != 0x80 {
			t.Errorf("la clave de la posición %d no lleva la variante de UUID: %#x", slot, k[8])
		}
	}
}

func TestTheSweepCoversEveryPositionTheModelCanEmit(t *testing.T) {
	// La otra mitad de lo que comprobaba `gate.TestTheSweepCoversEverything...`:
	// allá se verifica que ninguna posición emitida se sale del rango, y acá que
	// el barrido cubre ese rango entero. Un hueco entre las dos deja un filtro
	// puesto para siempre, y un filtro de WFP no se ve en `wf.msc`.
	barrido := map[[16]byte]bool{}
	for _, k := range AllKeys() {
		barrido[k] = true
	}
	if len(barrido) != gate.MaxFilters {
		t.Fatalf("el barrido tiene %d claves distintas y las posiciones son %d",
			len(barrido), gate.MaxFilters)
	}
	for slot := 0; slot < gate.MaxFilters; slot++ {
		if !barrido[keyForSlot(slot)] {
			t.Errorf("la posición %d no está en el barrido", slot)
		}
	}
}
