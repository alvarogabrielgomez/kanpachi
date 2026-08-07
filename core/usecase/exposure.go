package usecase

import (
	"context"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// Exposure es lo que Kanpachi tiene abierto AHORA, para la pantalla que lo
// enseña.
//
// # Por qué no devuelve error
//
// Porque la pantalla tiene que decir algo siempre, y lo que tiene que decir
// cuando la medición falla ya está en el tipo: un informe CIEGO, con la
// compuerta sin comprobar y sin lista de puertos. Devolver un error obligaría a
// quien llama a inventar qué pintar, y la respuesta más cómoda sería enseñar la
// última lista buena. Misma doctrina que [domain.AlertAuditFailed].
//
// # Por qué el candado se suelta antes de medir
//
// Porque medir es una llamada a Windows que enumera cientos de reglas del
// sistema, y sostener el candado durante eso haría que un `Status` de la UI que
// llegue en ese momento se quede esperando. Es el único camino por el que la
// promesa de que nada retrasa una respuesta se rompía, y ya costó una vez: ver
// la primera publicación de [NewSession].
func (s *Session) Exposure(ctx context.Context) domain.ExposureReport {
	s.mu.Lock()
	desired, err := s.desiredRuleSetLocked(ctx)
	// La compuerta solo se exige con sala abierta: sin adaptador virtual no hay
	// dónde ponerla, y pedirla en reposo dejaría una alerta encendida sin sala,
	// que es la forma más rápida de que el usuario aprenda a ignorar la
	// pantalla.
	wantGate := s.state.Conn.InRoom()
	s.mu.Unlock()

	if err != nil {
		// No se puede decir qué falta sin saber qué se pidió. Ciego y no vacío:
		// una lista vacía se lee como "no hay nada abierto", que es lo contrario
		// de lo que se sabe.
		s.deps.Log.Warn("no se pudo calcular lo que debería estar abierto", "error", err)
		return domain.BlindExposure()
	}

	e, err := s.deps.Audit.Enforcement(ctx)
	if err != nil {
		s.deps.Log.Warn("no se pudo medir lo que está abierto", "error", err)
		return domain.BlindExposure()
	}
	return domain.NewExposureReport(desired, e, wantGate, s.deps.Clock.Now())
}
