package usecase

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"strings"
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

	cred, err := s.credentialFor(ip)
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
// # Por qué la lleva la sesión y no se le pregunta al motor
//
// Porque **el motor no sabe a qué dirección fue una credencial.** Emite el
// token y nada más: la dirección la elige el host acá al lado, en
// [Session.IssueCredentialFor], y nunca baja. Su `ListCredentials` devuelve el
// id y el vencimiento con la IP en CERO, así que buscar ahí por IP no
// encontraba nunca, y eso rompía las dos cosas que dependen de este lazo:
// expulsar contestaba que esa dirección no es de ningún miembro, y la
// pre-autorización del canal de control no abría el puerto a nadie. Ver
// [Session.authorizedControlIPsLocked].
//
// El precio es que el mapa vive en memoria: un host que reinicia el daemon y
// retoma la sala pierde el lazo IP↔credencial de quien ya estaba dentro, y no
// puede expulsarlo hasta que vuelva a entrar. Se paga a sabiendas, porque la
// alternativa era un lazo que no existía.
//
// Asume el candado tomado.
func (s *Session) credentialFor(ip netip.Addr) (domain.Credential, error) {
	if c, ok := s.issued[ip]; ok {
		return c, nil
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
func (s *Session) authorizedControlIPsLocked() []netip.Addr {
	out := domain.MemberIPs(s.state.Peers)
	if !s.state.IsHost() {
		return out
	}

	now := s.deps.Clock.Now()
	s.forgetOldKicks(now)

	seen := make(map[netip.Addr]bool, len(out))
	for _, ip := range out {
		seen[ip] = true
	}

	for ip, c := range s.issued {
		if c.Expired(now) || seen[ip] {
			continue
		}
		if _, kicked := s.kicked[ip]; kicked {
			continue
		}
		out = append(out, ip)
		seen[ip] = true
	}

	return out
}

// controlScope arma el alcance del oyente del host. Asume el candado tomado.
func (s *Session) controlScope() domain.ControlScope {
	return domain.ControlScope{
		Lobby:   s.lobbyHostAddrLocked(),
		Room:    s.state.LocalIP,
		Members: s.authorizedControlIPsLocked(),
		// THIS room's rendezvous network, which is what the host signs when it
		// issues a credential. Empty on a guest, which issues nothing.
		RendezvousName: s.hostSpec.Rendezvous.NetworkName(),
	}
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
	if err := s.deps.Control.Serve(ctx, s.controlScope()); err != nil {
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
	antes := s.state.Peers
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
	s.logMemberDiffLocked(antes)
	s.tellStaleMembersLocked(ctx)
	return s.snapshot(), nil
}

// OnMemberChannelUp lo llama el supervisor cuando un miembro abre su canal de
// control con este host.
//
// Existe por una sola cosa: es el primer instante en que se le puede hablar. Lo
// que hay que decirle, si su credencial no está, es justo lo que no se pudo
// decir antes. Ver [Session.tellStaleMembersLocked] y [domain.NoticeStale].
//
// Se le borra el plazo para que el aviso salga AHORA y no cuando venza el
// reintento: ese plazo se puso porque no había canal, y acaba de haberlo.
func (s *Session) OnMemberChannelUp(ctx context.Context, ip netip.Addr) domain.RoomState {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.state.IsHost() || !s.state.Conn.InRoom() {
		return s.snapshot()
	}
	delete(s.staleProxAviso, ip)
	s.tellStaleMembersLocked(ctx)
	return s.snapshot()
}

// StaleNoticeCooldown es cada cuánto se le repite a un miembro que el host no
// tiene su credencial, cuando el aviso SÍ salió.
//
// Hace falta porque el disparador es un cambio de miembros, y el motor los emite
// en ráfagas: un invitado sin credencial produce además un parpadeo de un
// segundo, entrando y saliendo de la tabla. Sin freno, eso serían decenas de
// avisos por minuto a la misma máquina.
//
// Treinta segundos son de sobra: reingresar tarda unos diez, así que un solo
// aviso suele bastar y el siguiente solo llega si el primero no sirvió de nada.
const StaleNoticeCooldown = 30 * time.Second

// StaleNoticeRetry es cada cuánto se reintenta cuando el aviso NO salió.
//
// # Por qué no vale el enfriamiento largo, medido
//
// El 2026-08-11 el host volvió a las 20:21:23. El motor le puso a los dos
// invitados en la tabla a las 20:21:22.365, y ahí mismo se les intentó avisar y
// los dos fallaron con "no hay canal abierto". Es lo ESPERABLE y no un
// accidente: el host acaba de arrancar, su servidor de control lleva
// milisegundos escuchando y los invitados todavía no lo redialaron. Uno de ellos
// reconectó trece segundos después.
//
// O sea que el primer intento del caso que esto existe para arreglar falla
// SIEMPRE, y sin un reintento corto el aviso no llega nunca. Aquella medición
// dio recuperaciones de 1m51s y 1m56s, que fue exactamente lo que tardó el
// invitado en darse cuenta solo: el aviso no aportó nada.
//
// Dos segundos alcanzan porque lo que se está esperando es que el otro lado
// redial, y el coste de errar es una línea de log.
const StaleNoticeRetry = 2 * time.Second

// StaleNoticeTries es cuántas veces seguidas se reintenta rápido antes de pasar
// al plazo largo.
//
// Diez por dos segundos son veinte de insistencia, y los trece que tardó el
// invitado medido en redialar caben con margen. El tope existe por un caso que
// no es el del reinicio: un miembro puede estar en la tabla del motor sin canal
// de control durante mucho rato, y sin tope eso sería una línea de aviso cada
// dos segundos para siempre.
const StaleNoticeTries = 10

// avisoStale es cuándo se le puede volver a hablar a un miembro sin credencial,
// y cuántos intentos rápidos seguidos lleva fallados.
//
// Los intentos se cuentan porque el plazo solo no distingue los dos casos que
// hay que tratar distinto: el otro lado todavía no redialó, que se resuelve en
// segundos, y no hay por dónde hablarle, que no se resuelve.
type avisoStale struct {
	prox     time.Time
	intentos int
}

// tellStaleMembersLocked le avisa a quien está en la sala y no tiene credencial
// de este host.
//
// # El caso que arregla, medido
//
// El host reinicia. Sus credenciales viven en la memoria del motor y mueren con
// el proceso, así que al volver no reconoce a nadie aunque reabra la MISMA sala
// con el MISMO código. Los invitados quedan en un estado que **no pueden
// diagnosticar solos**: el 2026-08-11 uno tuvo canal de control con el host
// noventa segundos seguidos, con la pantalla normal, y su credencial ya no
// existía del otro lado. Todo lo que él podía mirar decía que estaba bien.
//
// El host sí lo sabe, y es el único: tiene esa dirección entre sus miembros y
// ninguna credencial emitida para ella. Decirlo convierte una espera de minutos,
// que dependía de que el invitado acabara notando la ausencia por su cuenta, en
// un reingreso inmediato.
//
// Solo el HOST, y solo hacia miembros presentes. A quien no está no hay por
// dónde avisarle, y su credencial vencida se limpia sola.
//
// Que falle no rompe nada: el invitado sigue teniendo su propio camino, que es
// notar que el host no está y volver a pedir. Esto lo acelera, no lo sustituye.
//
// Lo llaman los dos sitios que pueden: el cambio de miembros, que es lo que da
// la inmediatez, y el latido, que es lo que hace que un intento fallido se
// reintente. Con solo el primero, el aviso del caso principal no llega nunca.
//
// Asume el candado tomado.
func (s *Session) tellStaleMembersLocked(ctx context.Context) {
	if !s.state.IsHost() {
		return
	}
	ahora := s.deps.Clock.Now()
	for _, p := range s.state.Peers {
		if p.Self || !p.VirtualIP.IsValid() {
			continue
		}
		if c, hay := s.issued[p.VirtualIP]; hay && !c.Expired(ahora) {
			continue
		}
		estado := s.staleProxAviso[p.VirtualIP]
		if !estado.prox.IsZero() && ahora.Before(estado.prox) {
			continue
		}
		if s.staleProxAviso == nil {
			s.staleProxAviso = make(map[netip.Addr]avisoStale)
		}

		err := s.deps.Control.Notify(ctx, p.VirtualIP, domain.RoomNotice{
			Kind:   domain.NoticeStale,
			Reason: "el host reinició y no tiene tu credencial, vuelve a pedirla",
		})
		switch {
		case err == nil:
			s.deps.Log.Info("se le avisó a un miembro de que este host no tiene su credencial",
				"nombre", p.Name.String(), "ip", p.VirtualIP.String())
			s.staleProxAviso[p.VirtualIP] = avisoStale{prox: ahora.Add(StaleNoticeCooldown)}
		case estado.intentos+1 < StaleNoticeTries:
			s.deps.Log.Warn("no se le pudo avisar a un miembro sin credencial, se reintenta",
				"ip", p.VirtualIP.String(), "error", err, "en", StaleNoticeRetry)
			s.staleProxAviso[p.VirtualIP] = avisoStale{
				prox: ahora.Add(StaleNoticeRetry), intentos: estado.intentos + 1,
			}
		default:
			// Se agotaron los intentos rápidos. Esto ya no es "todavía no
			// redialó", es un miembro al que no hay por dónde hablarle, y para
			// ése el aviso no es el camino: su propio reingreso lo es. Se pasa al
			// plazo largo para no llenar el log con lo mismo.
			s.deps.Log.Warn("no se le pudo avisar a un miembro sin credencial, se deja de insistir",
				"ip", p.VirtualIP.String(), "error", err, "intentos", estado.intentos+1)
			s.staleProxAviso[p.VirtualIP] = avisoStale{prox: ahora.Add(StaleNoticeCooldown)}
		}
	}
	// Y se olvida lo que ya no frena nada, para que el mapa no crezca con la
	// sala. Borrar un plazo cumplido es además lo que devuelve los intentos
	// rápidos: quien siga sin credencial dentro de un rato vuelve a tener su
	// ráfaga corta en vez de quedar condenado al plazo largo para siempre.
	for ip, e := range s.staleProxAviso {
		if ahora.After(e.prox) {
			delete(s.staleProxAviso, ip)
		}
	}
}

// logMemberDiffLocked anota QUIÉN entró y quién salió, y no anota nada cuando
// no cambió nadie.
//
// El sondeo corre por cada evento del motor, y el motor los manda en ráfagas:
// abrir una sala producía cinco líneas en un segundo diciendo "antes 1 ahora 1",
// que es una lista de miembros que no cambió contada cinco veces. La pregunta
// que alguien le hace de verdad al log es quién entró y cuándo, y para eso hace
// falta el nombre, no el total.
//
// Asume el candado tomado.
func (s *Session) logMemberDiffLocked(antes []domain.Peer) {
	previos := make(map[netip.Addr]bool, len(antes))
	for _, p := range antes {
		previos[p.VirtualIP] = true
	}
	ahora := make(map[netip.Addr]bool, len(s.state.Peers))
	for _, p := range s.state.Peers {
		ahora[p.VirtualIP] = true
		if !previos[p.VirtualIP] {
			s.deps.Log.Info("entró a la sala",
				"nombre", p.Name.String(), "ip", p.VirtualIP.String(), "camino", p.Path.String())
		}
	}
	for _, p := range antes {
		if !ahora[p.VirtualIP] {
			s.deps.Log.Info("salió de la sala",
				"nombre", p.Name.String(), "ip", p.VirtualIP.String())
		}
	}
	s.logInvitadosQueNoLleganLocked(ahora)
}

// logInvitadosQueNoLleganLocked anota a quién le dimos credencial y no aparece.
//
// # El fallo que esto hace legible
//
// El host tiene las dos listas —a quién le emitió credencial y a quién ve el
// motor— y hasta ahora nadie las comparaba. El 2026-08-08 un host tuvo un
// invitado dentro veinte minutos: el invitado veía dos miembros, el host veía
// uno, y **ninguno de los dos logs lo decía**. Se dedujo por omisión, mirando
// que nunca apareciera un `entró a la sala` y que las reglas del firewall se
// quedaran en dos.
//
// No es cosmético. La lista de miembros es de donde salen las reglas de juego:
// sin miembros no se abre ni un puerto hacia el invitado, así que este silencio
// tapaba que la sala no servía para jugar.
//
// # Por qué solo cuando hay discrepancia
//
// Porque esto corre en cada sondeo de miembros, y el motor los dispara en
// ráfagas. Una línea por sondeo diciendo que todo está bien sería el ruido que
// tapa lo que sí importa. Sin discrepancia, silencio.
//
// Asume el candado tomado.
func (s *Session) logInvitadosQueNoLleganLocked(presentes map[netip.Addr]bool) {
	if !s.state.IsHost() || len(s.issued) == 0 {
		return
	}
	ahora := s.deps.Clock.Now()

	faltan := make([]string, 0, len(s.issued))
	for ip, c := range s.issued {
		// Una credencial vencida ya no autoriza a nadie, así que su ausencia no
		// es noticia: quien la tenía se fue hace rato y esto no es un fallo.
		if c.Expired(ahora) || presentes[ip] {
			continue
		}
		faltan = append(faltan, c.Name.String()+" "+ip.String())
	}
	if len(faltan) == 0 {
		return
	}
	sort.Strings(faltan)
	s.deps.Log.Warn("hay credenciales emitidas que el motor no ve en la sala",
		"cuántas", len(faltan), "quiénes", strings.Join(faltan, ", "),
		"miembros que sí ve", len(presentes))
}
