package usecase

import (
	"context"
	"fmt"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// RefreshAlerts corre el módulo de exposición de la decisión 19 y publica el
// resultado dentro del estado.
//
// Lo llama el supervisor cada tanto. No hay canal aparte ni evento especial:
// el módulo publica su último resultado y [Session.Status] lo arrastra, así que
// una alerta nunca puede bloquear ni retrasar una respuesta.
//
// **Ninguna comprobación devuelve error fatal.** Cada una responde una pregunta
// que Kanpachi no controla y que anula su promesa si nadie la mira, y que la
// consulta falle no puede impedir entrar a una sala ni jugar. Lo que se pierde
// es el aviso, y eso queda en el log.
func (s *Session) RefreshAlerts(ctx context.Context) domain.RoomState {
	var found []domain.Alert

	if estados, err := s.deps.Audit.FirewallEnabled(ctx); err != nil {
		s.deps.Log.Warn("no se pudo comprobar si el firewall está encendido", "error", err)
	} else {
		for _, e := range estados {
			if !e.Enabled {
				// Con el firewall apagado, las reglas de Kanpachi no
				// restringen nada: la red sigue cifrada y la cuarentena
				// desaparece, que es media promesa del producto.
				found = append(found, domain.Alert{
					Kind:   domain.AlertFirewallOff,
					Detail: fmt.Sprintf("el Firewall de Windows está apagado en el perfil %s", e.Profile),
				})
			}
		}
	}

	if intactas, err := s.deps.Audit.OwnRulesIntact(ctx); err != nil {
		s.deps.Log.Warn("no se pudieron revisar las reglas propias", "error", err)
	} else if !intactas {
		found = append(found, domain.Alert{
			Kind:   domain.AlertRulesTampered,
			Detail: "las reglas de Kanpachi no están como se aplicaron",
		})
	}

	if mapeos, err := s.deps.Audit.RouterMappings(ctx); err != nil {
		// Muchísimos routers no contestan al IGD, así que esto falla en
		// condiciones normales y no vale ni un aviso al usuario.
		s.deps.Log.Info("el router no respondió a la consulta de mapeos", "detalle", err)
	} else {
		for _, m := range mapeos {
			// Kanpachi no lo puso y no lo va a quitar: el router del usuario no
			// se toca nunca. Lo único que hace es decirlo.
			found = append(found, domain.Alert{
				Kind: domain.AlertRouterMapping,
				Detail: fmt.Sprintf("el router publica el puerto %d hacia %s, y no lo puso Kanpachi",
					m.ExternalPort, m.InternalIP),
			})
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// El conflicto de vestíbulo lo detecta el plan de direcciones y sobrevive a
	// este refresco, porque describe la máquina y no una comprobación puntual.
	for _, a := range s.state.Alerts {
		if a.Kind == domain.AlertLobbyConflict {
			found = append(found, a)
		}
	}
	s.state.Alerts = found
	return s.snapshot()
}

// Diagnose consulta al motor y refresca el diagnóstico de red.
//
// Es lo que convierte "no conecta" en "tu router hace NAT simétrico, vas por
// relay". Los campos que NO vienen del motor se conservan: el MTU lo sondea
// netcfg y la subred la eligió el plan de direcciones, y pisarlos con los ceros
// de una respuesta del motor dejaría el diagnóstico peor que antes de pedirlo.
func (s *Session) Diagnose(ctx context.Context) (domain.NetCheck, error) {
	check, err := s.deps.Engine.Diagnostics(ctx)
	if err != nil {
		return domain.NetCheck{}, fmt.Errorf("consultando el diagnóstico al motor: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	check.MTU = s.state.Net.MTU
	check.Subnet = s.state.Net.Subnet
	check.SubnetReason = s.state.Net.SubnetReason
	s.state.Net = check
	s.snapshot()
	return check, nil
}

// ObserveGame saca la foto de sockets del creador de perfiles.
//
// Es una consulta PUNTUAL, disparada por un botón del usuario que ya abrió el
// juego. No hay espera de fondo, no queda nada corriendo al cerrar el
// asistente, y fuera de este asistente Kanpachi jamás consulta procesos. Es la
// única función del programa que mira un proceso, y por eso está sola en su
// propio método en vez de escondida dentro de otro flujo.
//
// `tree` son los PIDs del árbol que arranca en el ejecutable elegido, porque
// muchos juegos arrancan desde un launcher y el servidor es un proceso hijo.
// Quién arma ese árbol es el adaptador, que es el único que puede recorrerlo.
func (s *Session) ObserveGame(ctx context.Context, root domain.ProcessRef, tree map[int]bool, keepSteam bool) ([]domain.PortRange, error) {
	listeners, err := s.deps.Inspector.Snapshot(ctx, root)
	if err != nil {
		return nil, fmt.Errorf("mirando los puertos de %s: %w", root.Executable, err)
	}
	if len(tree) == 0 {
		// Sin árbol la foto trae todos los sockets de la máquina. Se toma la
		// raíz como árbol de un solo nodo en vez de devolver todo: es lo que
		// el usuario pidió mirar.
		tree = map[int]bool{root.PID: true}
	}
	rangos := domain.ObservedRanges(listeners, tree, keepSteam)
	s.deps.Log.Info("foto de puertos", "ejecutable", root.Executable, "rangos", len(rangos))
	return rangos, nil
}
