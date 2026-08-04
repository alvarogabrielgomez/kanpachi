//go:build windows

package firewall

// Dónde se eligen los dos adaptadores concretos de esta familia.
//
// El binario sigue eligiendo si USA este firewall o no, que es lo que dice la
// doctrina de `cmd/kanpachid`. Lo que no puede elegir es con qué componerlo:
// juntar la capa de permisos de un sitio con la compuerta de otro es justo el
// error que [Firewall.SetScope] existe para impedir, y dejar la composición
// suelta lo volvería posible otra vez.

import (
	"fmt"

	"github.com/accentiostudios/kanpachi/daemon/adapter/firewall/wfp"
	"github.com/accentiostudios/kanpachi/daemon/adapter/firewall/windowscom"
)

// Las comprobaciones de que los tipos reales encajan.
//
// Están acá y no en cada adaptador porque son lo único que puede fallar al
// componer, y acá el error sale en el sitio donde se arreglaría.
var (
	_ Permits     = (*windowscom.Firewall)(nil)
	_ PermitAudit = (*windowscom.Audit)(nil)
	_ Gate        = (*wfp.Gate)(nil)
)

// NewWindows abre las dos capas y las compone.
//
// Devuelve un cierre que hay que llamar al apagar: la capa de permisos tiene un
// hilo propio para su apartamento de COM y la compuerta tiene una sesión abierta
// contra el motor de filtrado.
//
// Cerrar la sesión NO se lleva los filtros, y eso es deliberado: la sesión no es
// dinámica, así que una muerte sucia del daemon deja la sala contenida en vez de
// abierta. Quien limpia es [Firewall.PurgeOwned] en el arranque siguiente, y un
// reinicio de la máquina se los lleva de todas formas.
//
// Abrir NO exige administrador, y casi todo lo demás sí. Un proceso sin elevar
// monta las dos capas, enumera las reglas del firewall, y falla al leer un
// filtro de la compuerta que exista o al cambiar cualquier cosa. Está medido y
// anotado en [wfp.Open], con la trampa que tiene.
func NewWindows(dataDir string, log Logger) (*Firewall, func() error, error) {
	permits, err := windowscom.New(dataDir, "", log)
	if err != nil {
		return nil, nil, fmt.Errorf("abriendo la capa de permisos: %w", err)
	}

	gate, err := wfp.Open(log)
	if err != nil {
		_ = permits.Close()
		return nil, nil, fmt.Errorf("abriendo la compuerta: %w", err)
	}

	fw, err := New(permits, windowscom.NewAudit(permits), gate, log)
	if err != nil {
		_ = gate.Close()
		_ = permits.Close()
		return nil, nil, err
	}

	// Las dos se cierran SIEMPRE, aunque la primera falle. Un cierre que corta en
	// el primer error deja abierto lo que venía después, y acá lo que viene
	// después es un hilo del sistema fijado y una sesión contra el motor de
	// filtrado.
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
