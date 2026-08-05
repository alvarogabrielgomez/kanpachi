package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// Reset deja la máquina como una instalación recién arrancada, sin daemon.
//
// # Para qué existe, si la limpieza del arranque ya es incondicional
//
// Porque la limpieza del arranque necesita un arranque. `NewSession` repone la
// cuarentena, purga el grupo propio y restaura las reglas ajenas en CADA
// arranque, sin depender de ninguna señal, y eso cubre la muerte sucia. Lo que
// no cubre es el daemon que **no vuelve a arrancar**: una configuración
// corrupta, un adaptador que ya no monta, un motor huérfano peleándose por la
// red virtual. Ahí hace falta poder limpiar sin pasar por el camino que está
// roto.
//
// # Lo que repone, y por qué esa asimetría es deliberada
//
// Repone la cuarentena de base y NO la quita. Lo que hace que la cuarentena
// valga es seguir puesta con el daemon detenido, así que un reset por corrupción
// que se la llevara destruiría justo lo que protege del caso corrupción.
// Quitarla es del desinstalador, y el desinstalador no es esto.
//
// Conserva `last-room.json`. Resetear la configuración no tiene por qué borrar a
// qué sala volver: son dos cosas distintas y juntarlas convierte una limpieza en
// una pérdida.
//
// # Por qué no corta en el primer error
//
// Porque no hay un segundo intento. Cada paso deshace algo distinto, y abortar
// en el primero dejaría el resto puesto justo cuando alguien pide limpiar
// porque nada más funciona. Se registran todos los fallos y se devuelve el
// conjunto al final.
func Reset(ctx context.Context, d ResetDeps) error {
	if err := d.validate(); err != nil {
		return err
	}

	var fallos []error
	falló := func(qué string, err error) {
		if err == nil {
			return
		}
		d.Log.Error("el reset no pudo con un paso", "paso", qué, "error", err)
		fallos = append(fallos, fmt.Errorf("%s: %w", qué, err))
	}

	// 1. Los motores huérfanos PRIMERO. Mientras uno siga vivo, la red virtual
	// sigue arriba, y quitarle las reglas dejaría exactamente el estado que hay
	// que impedir: un adaptador virtual con tráfico y sin nada conteniéndolo.
	if n := d.Engine.KillOrphans(); n > 0 {
		d.Log.Info("motores huérfanos terminados", "cantidad", n)
	}

	// 2. La cuarentena de base, ANTES de purgar. La purga es el instante de
	// menos protección: se lleva las reglas de la sala y todavía no hay nada
	// nuevo. Es el mismo orden que el arranque, por la misma razón.
	falló("reponiendo la cuarentena de base", d.Firewall.ApplyBaseQuarantine(ctx, domain.BaseQuarantine()))

	// 3. Las dos capas de la sala. `PurgeOwned` se lleva el grupo propio y las
	// ranuras de la compuerta, en el orden que corresponde, que lo decide el
	// adaptador compuesto.
	falló("purgando las reglas de la sala", d.Firewall.PurgeOwned(ctx))
	d.Firewall.UnbindRoom()

	// 4. Las reglas de otros que se hubieran suspendido. Son de otra persona, y
	// dejarlas apagadas por un daemon que ya no corre es lo peor que este
	// producto puede hacerle a una máquina.
	falló("restaurando las reglas ajenas", d.Firewall.RestoreForeign(ctx))

	// 5. El libro de ajustes: la política de prefijo IPv6 y DirectPlay, que son
	// lo único persistente que `netcfg` toca. Se revierten al valor que había
	// ANTES, que es lo que el libro guarda.
	falló("revirtiendo los ajustes de la máquina", d.NetCfg.RevertTweaks(ctx))

	// 6. La sala pendiente. Su mera presencia es lo que hace que el arranque
	// siguiente pregunte si reabrir, y después de un reset no hay nada que
	// reabrir: la sala que dejó ese archivo ya no existe en ningún sitio.
	falló("borrando la sala pendiente", d.State.ClearRoom())

	if len(fallos) > 0 {
		return fmt.Errorf("el reset terminó con %d paso(s) fallidos: %w",
			len(fallos), errors.Join(fallos...))
	}
	d.Log.Info("reset completo: cuarentena repuesta, sala purgada, ajustes revertidos")
	return nil
}

// ResetDeps son las piezas que el reset necesita, como interfaces MÍNIMAS.
//
// Mínimas a propósito: este paquete es puro y corre en el job de Linux, así que
// lo que entra es la forma de cada cosa y no el tipo real. De paso, `Firewall`
// acá no puede escribir reglas de sala aunque el tipo real sepa hacerlo.
type ResetDeps struct {
	Firewall ResetFirewall
	NetCfg   ResetNetCfg
	Engine   ResetEngine
	State    ResetState
	Log      Logger
}

// ResetFirewall es el firewall visto desde el reset. NO tiene `Apply`: un reset
// no abre nada.
type ResetFirewall interface {
	ApplyBaseQuarantine(ctx context.Context, rules []domain.QuarantineRule) error
	PurgeOwned(ctx context.Context) error
	RestoreForeign(ctx context.Context) error
	UnbindRoom()
}

// ResetNetCfg es la parte de netcfg que deshace.
type ResetNetCfg interface {
	RevertTweaks(ctx context.Context) error
}

// ResetEngine mata lo que quedó vivo.
//
// Devuelve CUÁNTOS mató y no un error. No poder mirar un proceso ajeno es lo
// normal y no un problema, así que un error acá no diría nada útil; cuántos
// murió sí, porque un motor huérfano es la explicación de una sala que conecta
// a ratos.
type ResetEngine interface {
	KillOrphans() int
}

// ResetState es el estado en disco que el reset toca. `last-room.json` no
// aparece a propósito: no se borra.
type ResetState interface {
	ClearRoom() error
}

// Logger es la rebanada del log que hace falta acá.
type Logger interface {
	Info(msg string, kv ...any)
	Warn(msg string, kv ...any)
	Error(msg string, kv ...any)
}

func (d ResetDeps) validate() error {
	if d.Firewall == nil || d.NetCfg == nil || d.Engine == nil || d.State == nil || d.Log == nil {
		return errors.New("el reset necesita el firewall, netcfg, el motor, el estado y un log")
	}
	return nil
}
