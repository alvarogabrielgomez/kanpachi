package usecase

import (
	"context"
	"fmt"

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
		// El evento es una PISTA con causa, no el estado.
		//
		// Antes esto fijaba StateDegraded, y nada lo soltaba: el motor emite
		// `connected` en UN solo sitio, cuando sube el adaptador virtual, y un
		// corte de red no tira el adaptador. Medido el 2026-08-05, doce
		// segundos con la WiFi apagada dejaron la sala en degradado para
		// siempre, con la red entera recuperada y un solo miembro, que era uno
		// mismo. Ver [domain.RoomState.ConnFromPeers].
		//
		// No arranca ningún contador, y esa ausencia sigue siendo el punto:
		// degradado es que el túnel sigue en pie y va peor, normalmente por
		// relay. Contarlo como una caída sacaría de la sala a quien está
		// jugando por relay, que es un caso soportado y no un fallo.
		s.deps.Log.Info("el motor reporta la conexión degradada",
			"motivo", razón(ev, "sin motivo"))
		// Releer los miembros es lo que decide: si de verdad hay alguien por
		// relay, la derivación lo marca degradado; si el error fue un intento
		// suelto contra una dirección que no contestó, no cambia nada.
		if err := s.refreshPeersLocked(ctx); err != nil {
			return domain.RoomState{}, err
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
		s.leaveLocked(ctx, razónTexto(reason, "el motor no volvió a levantar"), domain.ExitTunnelLost, cerrarDeVerdad)
	}
	if err := s.deps.Firewall.PurgeOwned(ctx); err != nil {
		s.deps.Log.Error("no se pudieron purgar las reglas tras rendirse el motor", "error", err)
	}
	// Se llega acá porque algo ya falló, así que es justo donde menos hay que
	// creerle a la purga.
	s.verifyClosedLocked(ctx)
	return s.snapshot()
}

// OnEngineRestarted es lo que hace el supervisor cuando el motor volvió ENTERO.
//
// # Por qué existe además del evento de conexión
//
// Porque el evento llega en cuanto conecta la primera de las dos redes, y un
// host tiene dos. Durante un reinicio del watchdog eso significa que el evento
// puede llegar con el vestíbulo todavía sin levantar, y ahí no hay adaptador que
// acotar. Esto corre después, cuando `Restart` ya esperó a que las dos redes
// tengan dirección, así que es el único momento en que reacotar puede exigirse.
//
// **Acá sí es fatal.** Los adaptadores son nuevos, o sea LUID nuevo, y una
// compuerta que se quede apuntando al viejo no falla en ningún sitio: emite sus
// filtros, la llamada devuelve éxito, y la pantalla dice que la sala está
// contenida. Con gente ya dentro, eso es afirmar lo único que este producto
// promete sobre una red que no lo cumple.
func (s *Session) OnEngineRestarted(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.state.Conn.InRoom() {
		return nil
	}
	if err := s.deps.Firewall.BindRoom(ctx, s.state.Subnet, s.lobbyNetLocked(), s.bindingLocked()); err != nil {
		return fmt.Errorf("reacotando la contención tras el reinicio del motor: %w", err)
	}
	// Y se reaplica, porque los filtros que se hayan escrito durante la carrera
	// fueron contra el adaptador viejo.
	if err := s.applyPolicy(ctx); err != nil {
		return fmt.Errorf("reaplicando las reglas tras el reinicio del motor: %w", err)
	}
	s.deps.Log.Info("contención reacotada tras el reinicio del motor", "alcance", s.bindingLocked().String())
	return nil
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
	// La compuerta se vuelve a acotar ANTES de aplicar nada, y hace falta de
	// verdad: si el motor murió y volvió, los adaptadores virtuales son NUEVOS.
	//
	// Un adaptador nuevo tiene un LUID nuevo, así que la compuerta se quedaría
	// acotada a uno que ya no existe. Nada falla: `Apply` emite sus filtros
	// contra el LUID viejo, la llamada devuelve éxito, y la pantalla dice que la
	// sala está contenida mientras debajo hay una red virtual con los permisos
	// puestos y sin bloqueo.
	//
	// **Acá NO es fatal, y sí lo es en [Session.OnEngineRestarted].** Este evento
	// llega en cuanto conecta la PRIMERA de las dos redes, así que durante un
	// reinicio del watchdog llega legítimamente con el vestíbulo todavía sin
	// levantar. Tratarlo como fatal ahí convertía una carrera de un segundo en
	// una sala que no volvía nunca: medido, la sala se quedaba en reconectando
	// con las dos redes ya arriba. Quien cierra el caso de verdad es
	// `OnEngineRestarted`, que corre cuando el motor terminó de levantar las dos.
	if err := s.deps.Firewall.BindRoom(ctx, s.state.Subnet, s.lobbyNetLocked(), s.bindingLocked()); err != nil {
		s.deps.Log.Warn("todavía no se pudo reacotar la contención, se reintenta al terminar el reinicio",
			"error", err)
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

// rederiveConnLocked recalcula degradado ↔ conectado desde la tabla de miembros.
//
// La calidad de la conexión es una PROPIEDAD de quién está y por dónde llega,
// no un recuerdo de lo último que dijo el motor. Derivarla es lo que la hace
// curarse sola: en cuanto el miembro que iba por relay pasa a directo, o en
// cuanto se va, la sala deja de estar degradada sin que nadie tenga que mandar
// un evento de recuperación.
//
// Se llama desde [Session.refreshPeersLocked], o sea desde TODOS los caminos
// que releen miembros, y por eso no hay ninguno que se pueda olvidar.
//
// Asume el candado tomado.
func (s *Session) rederiveConnLocked() {
	// Solo entre esos dos estados. Reconectando manda sobre esto: sin túnel, la
	// tabla de miembros describe una sala que ahora mismo no se alcanza, y
	// pisarlo con "conectado" sería la pantalla mintiendo. Durante un ingreso
	// todavía no hay a quién ver.
	if !s.state.Conn.Established() || s.state.TunnelDown() {
		return
	}
	quiero := s.state.ConnFromPeers()
	if quiero == s.state.Conn {
		return
	}
	motivo := "todos los miembros llegan directo"
	if quiero == domain.StateDegraded {
		motivo = "hay algún miembro llegando por relay"
	}
	if err := s.state.Transition(quiero, motivo); err != nil {
		s.deps.Log.Warn("transición rechazada", "error", err)
		return
	}
	s.deps.Log.Info("cambió la calidad de la conexión", "estado", quiero.String(), "motivo", motivo)
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
