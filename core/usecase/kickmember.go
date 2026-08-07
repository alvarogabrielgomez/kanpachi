package usecase

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// KickMember saca a quien está dentro.
//
// SOLO el host. Hace DOS cosas en el mismo acto, y ninguna es cooperativa: no
// hay mensaje que el expulsado pueda ignorar.
//
//  1. Revoca su credencial por el motor. Sale de la red en alrededor de un
//     segundo, medido contra los binarios de la v2.6.4.
//  2. Saca su IP de la lista de miembros, con lo que el RuleSet completo se
//     regenera sin ella. Aunque siguiera en la red, no alcanza ningún puerto.
//
// Las dos capas fallan por motivos distintos, y esa es la gracia: un bug del
// motor lo deja sin autorización del firewall, y un cálculo malo de reglas lo
// deja sin sesión.
//
// Expulsar NO cierra la puerta. Con el mismo código, y si el host no lo
// renovó, entra de nuevo como si nada, y eso es deliberado: expulsar y
// bloquear son cosas distintas. Para que no vuelva, el host renueva el código,
// que es la otra operación y es independiente de esta.
func (s *Session) KickMember(ctx context.Context, ip netip.Addr) (domain.RoomState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.requireHost(); err != nil {
		return domain.RoomState{}, err
	}

	peer, ok := s.virtualIPOf(ip)
	if !ok {
		return domain.RoomState{}, fmt.Errorf("%w: %s", ErrNotAMember, ip)
	}
	if peer.Self {
		return domain.RoomState{}, ErrSelfKick
	}

	cred, err := s.credentialFor(ctx, ip)
	if err != nil {
		return domain.RoomState{}, err
	}

	// El aviso va PRIMERO, antes de tocar nada. Es el único orden en que
	// sirve: revocar le cierra la sesión en alrededor de un segundo, y a partir
	// de ahí no hay por dónde mandarle un mensaje. Del otro lado eso es la
	// diferencia entre "el host te sacó de la sala" y una partida que se cae
	// sola sin explicación.
	//
	// Mandarlo primero no le regala una ventana para escapar, porque el aviso
	// no es lo que expulsa. Lo que expulsa son las dos capas de abajo, y
	// ninguna es cooperativa: una es el motor cerrándole la sesión, la otra es
	// el Firewall de Windows del host descartando sus paquetes. Un cliente
	// modificado que ignore el aviso sale igual y en el mismo segundo.
	//
	// Que falle no detiene nada. Lo que se pierde es que se entere.
	if err := s.deps.Control.Notify(ctx, ip, domain.RoomNotice{
		Kind:   domain.NoticeKicked,
		Reason: "el host te sacó de la sala",
	}); err != nil {
		s.deps.Log.Warn("no se le pudo avisar al expulsado", "ip", ip.String(), "error", err)
	}

	// A partir de acá NADA se salta, y ese es el arreglo. Las dos capas fallan
	// por motivos distintos y esa es toda la gracia de la decisión 22, así que
	// abortar en la primera convertía dos capas en una: un bug del motor le
	// dejaba la sesión abierta Y el puerto autorizado.
	var fallos []error
	if err := s.deps.Engine.RevokeCredential(ctx, cred.ID); err != nil {
		fallos = append(fallos, fmt.Errorf("%w: %s: %v", ErrRevokeFailed, peer.Name, err))
		s.deps.Log.Error("el motor no revocó la credencial, sigue la segunda capa",
			"ip", ip.String(), "error", err)
	}

	// La lista se recorta acá y no se espera al siguiente sondeo del motor. Un
	// sondeo tarda segundos, y durante esos segundos la regla de firewall
	// seguiría autorizando a alguien a quien el host acaba de echar. La
	// segunda capa tiene que cerrar en el mismo acto que la primera.
	//
	// Y se anota en `kicked` para que el sondeo NO lo devuelva a la lista. El
	// motor tarda alrededor de un segundo en cerrarle la sesión, así que
	// cualquier evento de cambio de miembros en esa ventana lo reconstruiría
	// desde una realidad que todavía no se enteró, deshaciendo el recorte y
	// reabriéndole el puerto.
	if s.kicked == nil {
		s.kicked = make(map[netip.Addr]time.Time)
	}
	s.kicked[ip] = s.deps.Clock.Now()
	s.dropPeerLocked(ip)

	// El canal de control se recorta ANTES que el firewall, y el orden importa:
	// es la superficie que corre como SYSTEM y parsea entrada de la sala. Si
	// aplicar las reglas fallara, quedarse sin recortarlo dejaría al expulsado
	// hablándole al código que más revisión merece del proyecto.
	s.restrictControlChannel(ctx)

	if err := s.applyPolicy(ctx); err != nil {
		fallos = append(fallos, fmt.Errorf("%w: %v", ErrRulesFailed, err))
	}

	if len(fallos) > 0 {
		// Se AVISA en vez de deshacer, y la asimetría es deliberada. Deshacer el
		// recorte volvería a autorizar a quien el host acaba de echar, así que
		// lo que queda es la mitad que sí funcionó más una alerta que dice que
		// la otra no. Es un estado visible, no un estado escondido.
		s.raiseKickAlertLocked(peer, fallos)
		s.deps.Log.Error("expulsión a medias", "nombre", peer.Name.String(), "ip", ip.String())
		// Devuelve el estado VÁLIDO junto al error, que es lo contrario de lo
		// que hace el resto del paquete y va dicho acá para que nadie lo
		// "arregle": el miembro ya está fuera del conjunto de reglas, y
		// descartar el estado haría que la UI redibuje a alguien que el
		// firewall ya no autoriza.
		return s.snapshot(), fmt.Errorf("%w: %w", ErrKickPartial, errors.Join(fallos...))
	}

	s.deps.Log.Info("miembro expulsado", "nombre", peer.Name.String(), "ip", ip.String())
	return s.snapshot(), nil
}

// raiseKickAlertLocked deja constancia de que una expulsión no cerró sus dos
// capas. Asume el candado tomado.
func (s *Session) raiseKickAlertLocked(peer domain.Peer, fallos []error) {
	s.state.DropAlerts(domain.AlertKickIncomplete)
	s.state.Alerts = append(s.state.Alerts, domain.Alert{
		Kind: domain.AlertKickIncomplete,
		Detail: fmt.Sprintf("la expulsión de %s no se aplicó entera: %v. Renueva el código para que no vuelva",
			peer.Name, errors.Join(fallos...)),
	})
}

// dropPeerLocked saca a alguien de la lista de miembros. Asume el candado.
func (s *Session) dropPeerLocked(ip netip.Addr) {
	kept := make([]domain.Peer, 0, len(s.state.Peers))
	for _, p := range s.state.Peers {
		if p.VirtualIP != ip {
			kept = append(kept, p)
		}
	}
	s.state.Peers = kept
}

// KickGrace es cuánto se ignora a un expulsado que el motor todavía reporta.
//
// La revocación tarda alrededor de un segundo, medido. Treinta segundos son
// treinta veces eso, que es margen de sobra para una máquina cargada, y siguen
// siendo poco para el otro caso que cubre esta ventana: que el expulsado vuelva
// a entrar con un código que el host no renovó, cosa que es legítima y tiene
// que funcionar. Pasada la ventana, si está, está.
const KickGrace = 30 * time.Second

// forgetOldKicks limpia la lista de expulsados recientes. Asume el candado.
func (s *Session) forgetOldKicks(now time.Time) {
	for ip, t := range s.kicked {
		if now.Sub(t) >= KickGrace {
			delete(s.kicked, ip)
		}
	}
}

// credentialFor busca la credencial emitida a una IP virtual.
//
// Se le pregunta al motor en vez de llevar un registro propio: el motor es
// quien las emite y quien las revoca, y una segunda lista acá se
// desincronizaría justo cuando importa, que es al reabrir una sala tras un
// reinicio del host.
func (s *Session) credentialFor(ctx context.Context, ip netip.Addr) (domain.Credential, error) {
	creds, err := s.deps.Engine.ListCredentials(ctx)
	if err != nil {
		return domain.Credential{}, fmt.Errorf("consultando las credenciales emitidas: %w", err)
	}
	for _, c := range creds {
		if c.VirtualIP == ip {
			return c, nil
		}
	}
	return domain.Credential{}, fmt.Errorf("%w: no hay credencial emitida para %s", ErrNotAMember, ip)
}

// authorizedControlIPsLocked junta las IPs de los miembros presentes con las
// de quienes acaban de recibir una credencial y todavía no entraron a la red.
//
// Es lo que evita la condición de carrera: si la autorización esperara a que el
// motor reporte a la persona como miembro activo de la malla P2P, el primer
// intento de conexión del invitado rebotaría contra el firewall. Pre-autorizar
// el puerto de control en cuanto se emite la credencial cierra la ventana.
//
// Asume el candado tomado.
func (s *Session) authorizedControlIPsLocked(ctx context.Context) ([]netip.Addr, error) {
	out := domain.MemberIPs(s.state.Peers)
	if !s.state.IsHost() {
		return out, nil
	}

	creds, err := s.deps.Engine.ListCredentials(ctx)
	if err != nil {
		return nil, fmt.Errorf("consultando credenciales para pre-autorizar: %w", err)
	}

	seen := make(map[netip.Addr]bool, len(out))
	for _, ip := range out {
		seen[ip] = true
	}

	now := s.deps.Clock.Now()
	s.forgetOldKicks(now)

	for _, c := range creds {
		if c.Expired(now) || seen[c.VirtualIP] {
			continue
		}
		if _, kicked := s.kicked[c.VirtualIP]; kicked {
			continue
		}
		out = append(out, c.VirtualIP)
		seen[c.VirtualIP] = true
	}

	return out, nil
}

// controlScope arma el alcance del oyente del host. Asume el candado tomado.
func (s *Session) controlScope(ctx context.Context) (domain.ControlScope, error) {
	members, err := s.authorizedControlIPsLocked(ctx)
	if err != nil {
		return domain.ControlScope{}, err
	}
	return domain.ControlScope{
		Lobby:   domain.RendezvousHostAddress,
		Room:    s.state.LocalIP,
		Members: members,
	}, nil
}

// restrictControlChannel reduce el alcance del oyente a los miembros que
// quedan.
//
// El canal de la sala solo acepta IPs de miembros presentes, así que expulsar
// a alguien tiene que quitarlo también de esa lista. Sin esto, el expulsado
// seguiría pudiendo abrir conexiones al código que corre como SYSTEM en la
// máquina del host, que es la superficie más seria del producto.
//
// La puerta del vestíbulo NO se recorta, y es a propósito: sigue abierta a
// cualquiera con el código, porque expulsar no es bloquear. Para que el
// expulsado no vuelva, el host renueva el código, que es la otra operación.
func (s *Session) restrictControlChannel(ctx context.Context) {
	scope, err := s.controlScope(ctx)
	if err != nil {
		s.deps.Log.Error("no se pudo calcular el alcance del canal de control", "error", err)
		return
	}
	if err := s.deps.Control.Serve(ctx, scope); err != nil {
		s.deps.Log.Error("no se pudo recortar el alcance del canal de la sala", "error", err)
	}
}

// OnPeersChanged lo llama el supervisor cuando el motor avisa que alguien
// entró o salió.
//
// Recalcula el conjunto entero de reglas. Es la misma operación que ejecuta un
// cambio de juego, y por eso no hay dos caminos: cada cambio regenera el
// RuleSet completo desde el perfil, el rol y los miembros presentes.
func (s *Session) OnPeersChanged(ctx context.Context) (domain.RoomState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Un cambio de miembros es también una oportunidad de comprobar los plazos,
	// y esa es la red de abajo de todo: si el latido del supervisor se murió,
	// esto es lo que sigue sacando de una sala que ya no tiene host.
	if s.enforceDeadlinesLocked(ctx) {
		return s.snapshot(), nil
	}
	return s.onPeersChangedLocked(ctx)
}

// onPeersChangedLocked es el cuerpo, compartido con el evento del motor. Asume
// el candado tomado.
func (s *Session) onPeersChangedLocked(ctx context.Context) (domain.RoomState, error) {
	if !s.state.Conn.InRoom() {
		return s.snapshot(), nil
	}
	before := len(s.state.Peers)
	if err := s.refreshPeersLocked(ctx); err != nil {
		return domain.RoomState{}, err
	}
	// El sondeo trae la realidad del motor, que todavía puede incluir a alguien
	// a quien el host acaba de echar. Sin este filtro, el evento de cambio de
	// miembros deshace el recorte inmediato de KickMember y le vuelve a abrir
	// el puerto durante el segundo que tarda la revocación.
	now := s.deps.Clock.Now()
	s.forgetOldKicks(now)
	for ip := range s.kicked {
		s.dropPeerLocked(ip)
	}
	if err := s.applyPolicy(ctx); err != nil {
		return domain.RoomState{}, err
	}
	if s.state.IsHost() {
		s.restrictControlChannel(ctx)
		// Quien acaba de entrar no estaba cuando se anunció lo anterior, así
		// que su pantalla en sala arrancaría sin juego y sin nombre.
		s.announceLocked(ctx)
	}
	s.deps.Log.Info("cambió la lista de miembros", "antes", before, "ahora", len(s.state.Peers))
	return s.snapshot(), nil
}
