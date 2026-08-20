package usecase

import (
	"context"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/core/timing"
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
	s.forgetExpiredCredentialsLocked(now)

	// El silencio se comprueba ANTES que la ausencia, porque es lo que la arma
	// en el caso que el flanco del socket no cubre.
	if s.state.HostSilent(now) && s.state.HostPresent {
		// La ausencia se fecha en la última prueba de vida y NO en el ahora, y
		// esa línea es la que impide que veinte minutos se conviertan en
		// veintiséis. El número de la decisión 20 cuenta desde que se dejó de
		// saber del host, no desde que este código se dio cuenta.
		s.state.SetHostPresent(false, s.state.HostLastHeard)
		s.deps.Log.Info("el host lleva demasiado sin dar señales",
			"último", s.state.HostLastHeard, "límite", timing.HostSilenceLimit)
	}

	if s.leaveForHostAbsenceLocked(ctx) {
		return true
	}
	if s.state.ShouldLeaveForReconnectTimeout(now) {
		s.leaveLocked(ctx, "el túnel no volvió a levantar", domain.ExitTunnelLost, cerrarDeVerdad)
		return true
	}

	// La salud del juego se remide en CADA latido, y un cambio se anuncia en el
	// acto. Los dos plazos son distintos a propósito: mirar la tabla de sockets
	// de esta máquina cuesta dos llamadas al sistema, así que se puede hacer
	// cada quince segundos, y avisarle a la sala cuesta un mensaje por miembro,
	// así que solo se paga cuando la respuesta CAMBIÓ. Sin el aviso por flanco,
	// levantar el servidor del juego tardaría hasta [timing.AnnounceInterval] en
	// verse del otro lado, que son dos minutos mirando un punto que miente.
	if s.state.IsHost() && s.state.Conn.InRoom() {
		previa := s.gameReach
		s.measureGameHealthLocked(ctx)
		if s.gameReach != previa {
			s.announceLocked(ctx)
		}
	}

	// El anuncio periódico del host es lo que le da al otro lado algo que medir.
	// Va al final: si algo de arriba sacó de la sala, no hay a quién anunciarle.
	if s.state.IsHost() && now.Sub(s.lastAnnounce) >= timing.AnnounceInterval {
		s.announceLocked(ctx)
	}

	// Y la republicación de la tarjeta, por lo mismo y con el mismo criterio.
	// La diferencia con el anuncio es a quién le habla: el anuncio va a los que
	// ya están dentro, y esto va al registro, o sea a los que todavía no
	// entraron. Without it, a room that stays open past `timing.RoomTTL` gets
	// swept and its invite ID goes back in the pool, so the code somebody handed
	// out weeks ago stops being theirs.
	if s.state.IsHost() && now.Sub(s.lastPublish) >= timing.RepublishInterval {
		s.republishCardLocked(ctx)
	}

	// Y el tercero de la familia, que le habla al MOTOR. El anuncio refresca lo
	// que ven los que están dentro, la republicación lo que ve el registro, y
	// esto la vida de las credenciales con las que los de dentro siguen dentro.
	// Sin él, una sala más larga que [timing.CredentialTTL] echa a sus miembros uno por
	// uno al cumplirse las 24 h de cada ingreso.
	if s.state.IsHost() && now.Sub(s.lastRenew) >= timing.RenewInterval {
		s.renewCredentialsLocked(ctx)
	}

	// Y el aviso a quien está en la sala sin credencial de este host. No tiene
	// reloj propio porque el suyo es por miembro y vive en el propio mapa; esto
	// solo le da la oportunidad de correr.
	//
	// Va acá ADEMÁS de en el cambio de miembros, y sin esto el aviso no llega en
	// el caso que existe para arreglar: al reabrir la sala el motor pone a todos
	// en la tabla antes de que ninguno haya redialado el canal de control, así
	// que el primer intento falla siempre. Medido el 2026-08-11.
	if s.state.IsHost() {
		s.tellStaleMembersLocked(ctx)
	}
	return false
}

// leaveForHostAbsenceLocked es el contador de la decisión 20, en un solo sitio.
// Asume el candado tomado.
func (s *Session) leaveForHostAbsenceLocked(ctx context.Context) bool {
	if !s.state.ShouldLeaveForHostAbsence(s.deps.Clock.Now()) {
		return false
	}
	s.leaveLocked(ctx, "el host lleva veinte minutos sin aparecer", domain.ExitHostGone, cerrarDeVerdad)
	return true
}
