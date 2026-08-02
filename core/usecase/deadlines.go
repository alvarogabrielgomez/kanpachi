package usecase

import (
	"context"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// Tick es el latido, y lo llama el supervisor.
//
// Es la puerta PERIÓDICA de los vencimientos, no la única. Los mismos plazos se
// evalúan al principio de cada entrada con candado, y esa redundancia es
// deliberada: si la goroutine del latido se muere, los contadores siguen
// venciendo con el siguiente evento del motor.
//
// Devuelve el estado. Sin bool: quien llama compara contra
// [domain.StateIdle], que es la misma información y una cosa menos que
// mantener sincronizada.
//
// [Session.RefreshAlerts] va aparte y con su propia cadencia. Esto no toca
// ningún adaptador salvo que venza algo; aquello hace siempre tres llamadas al
// sistema, una al IGD del router, que en la mayoría de los routers termina en
// timeout. Un latido para el tiempo, un barrido para el mundo.
func (s *Session) Tick(ctx context.Context) domain.RoomState {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.enforceDeadlinesLocked(ctx)
	return s.snapshot()
}

// TickHostAbsence es el contador de la decisión 20, solo.
//
// Existe con nombre propio, además de dentro del latido, porque el corte a los
// veinte minutos es una cosa nombrada del producto y no un efecto colateral de
// una función que hace cinco. Los dos caminos comparten el mismo predicado del
// dominio, así que no hay dos versiones de la regla.
//
// Devuelve true si salió. Es política LOCAL pura: no hay mensaje, no hay
// coordinación y no hay que confiar en nadie. Cada máquina decide sobre sí misma
// a partir de un hecho que no se puede falsificar, que hace veinte minutos que
// no sabe nada del host.
//
// Resuelve el caso real de que el host reinicie la máquina y se olvide de abrir
// Kanpachi, dejando a los demás en una sala sin sentido.
func (s *Session) TickHostAbsence(ctx context.Context) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.leaveForHostAbsenceLocked(ctx)
}

// enforceDeadlinesLocked evalúa TODOS los vencimientos.
//
// Devuelve true si la evaluación terminó fuera de la sala, y quien lo llame
// tiene que cortar ahí: después de salir no queda sala sobre la que operar.
//
// Lo llama cada entrada con candado que OBSERVA algo, y no solo el latido. Esa
// es la respuesta a "¿y si el supervisor se murió?": mientras siga entrando
// cualquier cosa por cualquier puerta, los contadores siguen corriendo.
//
// Las entradas que expresan una INTENCIÓN del usuario no lo llaman a propósito.
// Expulsar, elegir juego, renovar el código y renombrar tienen que fallar con
// un error preciso, y que "expulsar" se convierta en silencio en "saliste de la
// sala" es peor que un contador que vence un latido más tarde.
//
// Asume el candado tomado.
func (s *Session) enforceDeadlinesLocked(ctx context.Context) bool {
	if !s.state.Conn.InRoom() {
		return false
	}
	now := s.deps.Clock.Now()
	s.forgetOldKicks(now)

	// El silencio se comprueba ANTES que la ausencia, porque es lo que la arma
	// en el caso que el flanco del socket no cubre.
	if s.state.HostSilent(now) && s.state.HostPresent {
		// La ausencia se fecha en la última prueba de vida y NO en el ahora, y
		// esa línea es la que impide que veinte minutos se conviertan en
		// veintiséis. El número de la decisión 20 cuenta desde que se dejó de
		// saber del host, no desde que este código se dio cuenta.
		s.state.SetHostPresent(false, s.state.HostLastHeard)
		s.deps.Log.Info("el host lleva demasiado sin dar señales",
			"último", s.state.HostLastHeard, "límite", domain.HostSilenceLimit)
	}

	if s.leaveForHostAbsenceLocked(ctx) {
		return true
	}
	if s.state.ShouldLeaveForReconnectTimeout(now) {
		s.leaveLocked(ctx, "el túnel no volvió a levantar", domain.ExitTunnelLost)
		return true
	}

	// El anuncio periódico del host es lo que le da al otro lado algo que medir.
	// Va al final: si algo de arriba sacó de la sala, no hay a quién anunciarle.
	if s.state.IsHost() && now.Sub(s.lastAnnounce) >= AnnounceInterval {
		s.announceLocked(ctx)
	}
	return false
}

// leaveForHostAbsenceLocked es el contador de la decisión 20, en un solo sitio.
// Asume el candado tomado.
func (s *Session) leaveForHostAbsenceLocked(ctx context.Context) bool {
	if !s.state.ShouldLeaveForHostAbsence(s.deps.Clock.Now()) {
		return false
	}
	s.leaveLocked(ctx, "el host lleva veinte minutos sin aparecer", domain.ExitHostGone)
	return true
}
