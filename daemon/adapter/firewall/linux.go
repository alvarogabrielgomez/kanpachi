//go:build linux

package firewall

// Dónde se eligen los dos adaptadores concretos de esta familia en Linux.
//
// Es el gemelo de `windows.go` y existe por lo mismo: el binario elige si USA
// este firewall, y lo que no puede elegir es con qué componerlo. Juntar la capa
// de permisos de un sitio con la compuerta de otro es justo el error que
// [Firewall.BindRoom] existe para impedir.
//
// # Acá las dos capas no son simétricas, y en Windows sí
//
// En Windows las dos escriben: una abre con reglas del Firewall de Windows y la
// otra cierra con filtros de WFP. En Linux la compuerta hace las dos cosas, y
// la capa de permisos se queda con lo que la compuerta no puede: la cuarentena
// de base, que tiene que sobrevivir a Kanpachi apagado, y la auditoría de lo que
// hay en el sistema y no puso Kanpachi.
//
// [Firewall] no se entera de esa asimetría, y ese es el punto de que su fichero
// sea puro: sigue siendo intersección, sigue habiendo un orden correcto, y
// [Permits] sigue teniendo los mismos siete métodos.

import (
	"fmt"

	"github.com/accentiostudios/kanpachi/daemon/adapter/firewall/linux/nft"
	"github.com/accentiostudios/kanpachi/daemon/adapter/firewall/linux/nftpermits"
)

// Las comprobaciones de que los tipos reales encajan.
//
// Están acá y no en cada adaptador porque son lo único que puede fallar al
// componer, y acá el error sale en el sitio donde se arreglaría.
var (
	_ Permits     = (*nftpermits.Permits)(nil)
	_ PermitAudit = (*nftpermits.Permits)(nil)
	_ Gate        = (*nft.Gate)(nil)
)

// NewLinux abre las dos capas y las compone.
//
// # Por qué el mismo objeto cumple Permits y PermitAudit, y en Windows son dos
//
// En Windows son dos objetos del mismo adaptador y van separados para que la
// parte que MIDE no tenga a mano los métodos que modifican. Acá esa separación
// no compra nada: la capa de permisos de Linux no modifica nada de la sala, así
// que no hay métodos peligrosos de los que apartar a la auditoría. Se declara
// dos veces en las comprobaciones de arriba para que, el día que esta capa
// empiece a escribir, el error salga acá.
//
// Devuelve un cierre que hay que llamar al apagar. Hoy ninguna de las dos capas
// tiene nada que cerrar, y el cierre existe igual para que el cableado no tenga
// que saberlo y para que el día que lo tengan no haya que cambiar la firma.
func NewLinux(quarantineFile string, log Logger) (*Firewall, func() error, error) {
	permits, err := nftpermits.New(quarantineFile, log)
	if err != nil {
		return nil, nil, fmt.Errorf("abriendo la capa de permisos: %w", err)
	}

	gate, err := nft.Open(log)
	if err != nil {
		_ = permits.Close()
		return nil, nil, fmt.Errorf("abriendo la compuerta: %w", err)
	}

	// El resolver de interfaces entra ACÁ y no dentro de `hybrid.go`: resolver un
	// nombre a índice es una llamada al sistema, y lo que decide `hybrid.go` es
	// el orden de las dos capas y qué pasa cuando una falla, que se prueba sin
	// sistema.
	fw, err := New(permits, permits, gate, log, nft.IfaceOf)
	if err != nil {
		_ = gate.Close()
		_ = permits.Close()
		return nil, nil, err
	}

	// Las dos se cierran SIEMPRE, aunque la primera falle. Un cierre que corta en
	// el primer error deja abierto lo que venía después.
	cerrar := func() error {
		errGate := gate.Close()
		errPermits := permits.Close()
		if errGate != nil {
			return errGate
		}
		return errPermits
	}
	return fw, cerrar, nil
}
