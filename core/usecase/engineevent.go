package usecase

import (
	"context"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// OnEngineEvent traduce lo que dice el motor a una transición.
//
// Lo llama el supervisor drenando [port.EnginePort.Events]. Vive acá y no en el
// supervisor porque el mapeo es política del producto: qué significa
// "degradado", si arranca un contador, y qué se recalcula al volver.
//
// Un evento repetido no es un error. El motor los emite de a ráfagas y la tabla
// de transiciones admite un estado a sí mismo, así que llegar tres veces
// "desconectado" produce lo mismo que llegar una.
func (s *Session) OnEngineEvent(ctx context.Context, ev domain.EngineEvent) (domain.RoomState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Antes del switch. Sin esto, la transición de abajo chocaría contra la
	// tabla desde StateIdle y ensuciaría el log con un ErrBadTransition que no
	// es un error: es que el vencimiento ya sacó de la sala.
	if s.enforceDeadlinesLocked(ctx) {
		return s.snapshot(), nil
	}
	if !s.state.Conn.InRoom() {
		return s.snapshot(), nil
	}

	switch ev.Kind {
	case domain.EngineConnected:
		return s.tunnelUpLocked(ctx, ev.Reason)

	case domain.EnginePeersChanged:
		return s.onPeersChangedLocked(ctx)

	case domain.EngineDegraded:
		// No arranca ningún contador, y esa ausencia es el punto: degradado es
		// que el túnel sigue en pie y va peor, normalmente por relay. Contarlo
		// como una caída sacaría de la sala a quien está jugando por relay, que
		// es un caso soportado y no un fallo.
		if err := s.state.Transition(domain.StateDegraded, razón(ev, "el motor reporta la conexión degradada")); err != nil {
			s.deps.Log.Warn("transición rechazada", "error", err)
		}
		return s.snapshot(), nil

	case domain.EngineDisconnected, domain.EngineDied:
		return s.tunnelDownLocked(ctx, ev)

	default:
		// El conjunto es cerrado a propósito, así que llegar acá significa que
		// alguien agregó un evento y no lo manejó. Se registra en vez de
		// ignorarse: un cambio de red que se pierde en silencio es lo que este
		// switch existe para evitar.
		s.deps.Log.Warn("evento del motor sin manejar", "tipo", int(ev.Kind), "motivo", ev.Reason)
		return s.snapshot(), nil
	}
}

// OnEngineGaveUp es lo que hace el supervisor cuando el watchdog se rinde.
//
// Separado de [Session.OnEngineEvent] porque no es algo que diga el motor: es
// una decisión de POLÍTICA que toma el supervisor tras agotar sus reintentos, y
// el motor a esa altura ni siquiera está vivo para opinar.
//
// Sale de la sala y purga. La purga va además del teardown, que ya aplica el
// conjunto vacío, y es a propósito: se llega hasta acá precisamente porque las
// cosas están rotas, y el martillo es lo apropiado cuando el camino ordenado ya
// falló. Es la "purga de reglas si se rinde" de 03-arquitectura.md.
func (s *Session) OnEngineGaveUp(ctx context.Context, reason string) domain.RoomState {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.state.Conn.InRoom() {
		s.leaveLocked(ctx, razónTexto(reason, "el motor no volvió a levantar"), domain.ExitTunnelLost)
	}
	if err := s.deps.Firewall.PurgeOwned(ctx); err != nil {
		s.deps.Log.Error("no se pudieron purgar las reglas tras rendirse el motor", "error", err)
	}
	// Se llega acá porque algo ya falló, así que es justo donde menos hay que
	// creerle a la purga.
	s.verifyClosedLocked(ctx)
	return s.snapshot()
}

// tunnelUpLocked es volver a tener red. Asume el candado tomado.
//
// Recalcula TODO en vez de suponer que nada cambió mientras no había túnel. Los
// miembros son otros, las reglas cuelgan de los miembros, y los ajustes del
// adaptador se pierden en cada evento de identificación de red, que es
// exactamente lo que pasa cuando una interfaz vuelve.
func (s *Session) tunnelUpLocked(ctx context.Context, reason string) (domain.RoomState, error) {
	s.state.SetTunnelUp()
	if err := s.state.Transition(domain.StateConnected, razónTexto(reason, "el motor conectó")); err != nil {
		s.deps.Log.Warn("transición rechazada", "error", err)
	}
	if err := s.refreshPeersLocked(ctx); err != nil {
		return domain.RoomState{}, err
	}
	if err := s.applyPolicy(ctx); err != nil {
		return domain.RoomState{}, err
	}
	s.configureAdapter(ctx)

	if s.state.IsHost() {
		// El alcance del oyente se recorta a los miembros de AHORA, que pueden
		// no ser los de antes de la caída, y se vuelve a anunciar porque quien
		// reconectó no tiene por qué haber conservado el último anuncio.
		s.restrictControlChannel(ctx)
		s.announceLocked(ctx)
	}
	s.deps.Log.Info("el túnel volvió", "miembros", len(s.state.Peers))
	return s.snapshot(), nil
}

// tunnelDownLocked es quedarse sin red. Asume el candado tomado.
//
// La sala NO se abandona: los reintentos son del túnel y no del ingreso, así
// que nadie tiene que volver a pegar un código. Lo que arranca es el plazo de
// [domain.ReconnectLimit], que es el respaldo del watchdog del supervisor para
// el caso de que el watchdog tampoco esté.
func (s *Session) tunnelDownLocked(ctx context.Context, ev domain.EngineEvent) (domain.RoomState, error) {
	s.state.SetTunnelDown(s.deps.Clock.Now())

	// Sin túnel no hay canal de control, así que el host no está aunque su
	// socket todavía no se haya enterado. Es una prueba independiente de la del
	// canal, y por eso se aplica acá aunque el drenaje de presencia exista.
	if !s.state.IsHost() {
		s.state.SetHostPresent(false, s.deps.Clock.Now())
	}

	texto := razón(ev, "el motor perdió la conexión")
	if err := s.state.Transition(domain.StateReconnecting, texto); err != nil {
		s.deps.Log.Warn("transición rechazada", "error", err)
	}
	s.deps.Log.Warn("sin túnel, reintentando", "motivo", texto, "plazo", domain.ReconnectLimit)
	return s.snapshot(), nil
}

// razón usa el motivo que trajo el evento, y cae a uno propio si vino vacío.
// Cada transición queda en el log con su causa, y "desconectado" a secas es una
// línea que no sirve para nada seis meses después.
func razón(ev domain.EngineEvent, fallback string) string {
	return razónTexto(ev.Reason, fallback)
}

func razónTexto(reason, fallback string) string {
	if reason == "" {
		return fallback
	}
	return reason
}
