//go:build windows

package netfw

import (
	"context"
	"fmt"

	"github.com/go-ole/go-ole"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// RemoveBaseQuarantineForUninstall borra la cuarentena de base al desinstalar.
//
// # Es una de las DOS funciones del repositorio que pueden hacer esto
//
// El nombre es largo a propósito: aparece entero en cualquier búsqueda y en
// cualquier diff, y el guardián de `internal/arch` lo permite POR NOMBRE, en
// este paquete y en ninguna otra parte. La otra es la retirada a pedido de
// abajo, y cualquier llamada destructiva apuntada al grupo base fuera de las
// dos falla el guardián, aquí dentro también.
//
// # Quién puede llamarla, y quién no
//
// Solo `cmd/kanpachid`, y solo detrás de la bandera `--uninstall-cleanup`.
// **Jamás un puerto de core**: la retirada que sí viaja por el puerto es la de
// abajo, con su propia lista de llamadores. Lo vigila un tercer guardián.
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

// RemoveBaseQuarantineAtUserRequest takes the quarantine down because the
// PERSON said so: it is the other half of the user's decision being the
// operation, and the second of the two closed names the guardian allows.
//
// # The body repeats the uninstaller's, and that is the guardian's price
//
// A shared helper that names the group and deletes would be a third function
// doing exactly what only two may do, and the guardian would redden it — as
// it should, because sharing the body couples the two doors and their two
// caller lists. Each door stands alone: this one is reached only through
// [port.FirewallPort] from the consent use case, that one only from
// `--uninstall-cleanup`, and each says in the log who asked.
//
// Idempotent: nothing left to remove is success, the intention is already
// true. A name somebody else's rule shares is disabled instead of removed by
// `retire`, logged, and left for the audit to report — same trade as
// PurgeOwned, for the same reason.
func (f *Firewall) RemoveBaseQuarantineAtUserRequest(ctx context.Context) error {
	all, err := f.liveRules(ctx)
	if err != nil {
		return err
	}

	// Igualdad EXACTA del grupo, jamás prefijo, por lo mismo que arriba.
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
		return nil
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
		return err
	}
	f.log.Info("cuarentena de base retirada a pedido del usuario",
		"quitadas", borradas, "encontradas", len(nuestras))
	return nil
}
