//go:build windows

package netfw

import (
	"context"
	"fmt"

	"github.com/go-ole/go-ole"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// RemoveBaseQuarantineForUninstall borra la cuarentena de base.
//
// # Es la única función del repositorio que puede hacer esto, y por eso se llama así
//
// El nombre es largo a propósito: aparece entero en cualquier búsqueda y en
// cualquier diff, y el guardián de `internal/arch` lo permite POR NOMBRE, en
// este paquete y en ninguna otra parte. Cualquier otra llamada destructiva
// apuntada al grupo base falla el guardián, aquí dentro también.
//
// # Quién puede llamarla, y quién no
//
// Solo `cmd/kanpachid`, y solo detrás de la bandera `--uninstall-cleanup`.
// **Jamás un puerto de core**, y eso está cerrado por otra vía: `FirewallPort`
// no declara nada que pueda quitar la cuarentena, así que ningún caso de uso
// puede pedirlo aunque quiera. Lo vigila un tercer guardián.
//
// # Por qué el reset NO la llama
//
// Porque lo que hace valiosa a la cuarentena es seguir puesta con el daemon
// detenido, deshabilitado o a medio desinstalar. Un `--reset` se pide justo
// cuando la configuración está corrupta y nada arranca: quitarla ahí destruiría
// exactamente lo que protege del caso que motivó el reset. Resetear y
// desinstalar son cosas distintas y la asimetría es el diseño.
//
// Devuelve cuántas reglas se llevó. Una regla que no se pudo borrar porque
// alguien más usa ese nombre se deja en paz, igual que en `PurgeOwned`: el
// desinstalador es el momento de menos derecho a romperle la configuración a
// nadie.
func RemoveBaseQuarantineForUninstall(ctx context.Context, f *Firewall) (int, error) {
	all, err := f.liveRules(ctx)
	if err != nil {
		return 0, err
	}

	// Igualdad EXACTA del grupo, jamás prefijo, igual que en todas partes: acá
	// la trampa está invertida y sigue doliendo. "Kanpachi" es prefijo de
	// "Kanpachi-base", así que un prefijo mal puesto convertiría el
	// desinstalador en algo que también se lleva por delante lo que no le toca.
	nuestras := map[string]bool{}
	otros := map[string]int{}
	for _, c := range all {
		if c.Group == domain.FirewallGroupBase {
			nuestras[c.Name] = true
			continue
		}
		otros[c.Name]++
	}
	if len(nuestras) == 0 {
		return 0, nil
	}

	borradas := 0
	err = f.ap.do(ctx, func(policy *ole.IDispatch) error {
		rules, err := rulesOf(policy)
		if err != nil {
			return err
		}
		defer rules.Release()

		for name := range nuestras {
			gone, err := f.retire(rules, name, otros[name])
			if err != nil {
				return fmt.Errorf("quitando la regla %q de la cuarentena: %w", name, err)
			}
			if gone {
				borradas++
			}
		}
		return nil
	})
	if err != nil {
		return borradas, err
	}
	f.log.Info("cuarentena de base retirada por el desinstalador", "cantidad", borradas)
	return borradas, nil
}
