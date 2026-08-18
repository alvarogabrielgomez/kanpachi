package domain

import (
	"fmt"
	"net/netip"
	"time"

	"github.com/accentiostudios/kanpachi/core/timing"
)

// ConnState es el estado del túnel, y es un valor único y explícito, sin flags
// booleanos regados. La UI lo renderiza, no lo infiere.
type ConnState uint8

const (
	// StateIdle es sin sala. El cero, a propósito: una sesión recién
	// construida ya está en el estado correcto.
	StateIdle ConnState = iota
	// StateResolving es derivando el vestíbulo y resolviendo las semillas.
	StateResolving
	// StateConnecting es el canje de credencial y la perforación de NAT.
	StateConnecting
	// StateConnected es dentro. El camino, directo o relay, va aparte: es una
	// propiedad de cada peer, no del estado.
	StateConnected
	// StateDegraded es dentro y peor. La conexión sigue en pie.
	StateDegraded
	// StateReconnecting es que se cayó y el watchdog está reintentando. La
	// sala no se abandona: los reintentos son del túnel, no del ingreso.
	StateReconnecting
)

func (s ConnState) String() string {
	switch s {
	case StateIdle:
		return "sin sala"
	case StateResolving:
		return "resolviendo"
	case StateConnecting:
		return "conectando"
	case StateConnected:
		return "conectado"
	case StateDegraded:
		return "degradado"
	case StateReconnecting:
		return "reconectando"
	default:
		return "estado-inválido"
	}
}

// transitions es la máquina de estados de 03-arquitectura.md, escrita como
// dato y no como una madeja de ifs repartidos por el supervisor.
//
//	Idle → Resolving → Connecting → Connected
//	                                    │
//	                             Degraded ↔ Reconnecting
//	                                    │
//	                                  Idle
//
// Todo estado puede volver a Idle, porque salir de la sala es una acción del
// usuario que tiene que funcionar siempre, incluso a mitad de un intento de
// conexión que no responde.
var transitions = map[ConnState][]ConnState{
	StateIdle:         {StateResolving},
	StateResolving:    {StateConnecting, StateIdle},
	StateConnecting:   {StateConnected, StateReconnecting, StateIdle},
	StateConnected:    {StateDegraded, StateReconnecting, StateIdle},
	StateDegraded:     {StateConnected, StateReconnecting, StateIdle},
	StateReconnecting: {StateConnected, StateDegraded, StateIdle},
}

// CanGoTo dice si la transición es legal.
//
// Existe para que una transición imposible falle acá, con nombre y causa, en
// vez de dejar la sesión en un estado que ninguna pantalla sabe pintar. Un
// estado a sí mismo es siempre legal y no hace nada: los eventos del motor
// llegan repetidos y tratarlos como error sería ruido.
func (s ConnState) CanGoTo(next ConnState) bool {
	if s == next {
		return true
	}
	for _, ok := range transitions[s] {
		if ok == next {
			return true
		}
	}
	return false
}

// InRoom dice si hay sala. No es lo mismo que estar conectado: reconectando y
// degradado siguen siendo estar en la sala, y el usuario no tiene que volver a
// pegar ningún código.
func (s ConnState) InRoom() bool { return s != StateIdle }

// Established es dentro y CON túnel en pie, vaya directo o por relay.
//
// Existe porque degradado y conectado son el mismo hecho para casi todo el
// producto: hay red y hay miembros que ver. Escribir `Conn == StateConnected`
// donde se quería decir esto deja fuera al degradado en silencio, y eso ya
// pasó: la deducción de la presencia del host desde la tabla de miembros se
// apagaba entera en cuanto la sala se marcaba degradada, que es justo la capa
// que sigue funcionando cuando el canal de control está roto.
func (s ConnState) Established() bool {
	return s == StateConnected || s == StateDegraded
}

// ErrBadTransition lo devuelve [RoomState.Transition]. Lleva los dos estados
// porque el log de cada transición registra su causa, y "transición inválida"
// a secas no dice cuál.
type ErrBadTransition struct {
	From, To ConnState
	Reason   string
}

func (e ErrBadTransition) Error() string {
	return fmt.Sprintf("no se puede pasar de %s a %s (%s)", e.From, e.To, e.Reason)
}

// RoomState es todo lo que el daemon sabe de la sala actual.
//
// Es la única fuente de verdad del producto: cerrar la ventana no cierra la
// sala, así que este estado sobrevive a la UI. La UI lo lee por Status() y
// persiste únicamente cosas de presentación.
type RoomState struct {
	Conn ConnState
	Role Role

	// Room es el invite ID vigente con su seed. Cambia al renovar el código,
	// y renovar no toca a los presentes.
	Room Room
	// Name es el nombre visible de la sala. Es presentación, viaja en la
	// tarjeta cifrada y no lo conoce el seed.
	Name string

	Peers []Peer

	// HostPresent NO es un estado de conexión, y separarlo es la decisión 20.
	// Que el host se haya ido es información sobre la SALA: la conexión sigue
	// perfecta, lo que falta es la persona que corre el juego. Meterlo en la
	// máquina de estados mezclaría dos cosas que fallan por motivos distintos.
	HostPresent bool
	// HostGoneSince marca desde cuándo. El cero significa que está presente, o
	// que soy yo el host.
	HostGoneSince time.Time
	// HostLastHeard es cuándo llegó la última PRUEBA DE VIDA del host: la
	// conexión de control levantada, un anuncio, un aviso, o su dirección en la
	// tabla de peers del motor.
	//
	// Existe porque la caída del socket es una señal que puede no llegar, y
	// esta llega sola. Solo se llena en invitados.
	HostLastHeard time.Time

	// ReconnectingSince marca desde cuándo NO hay túnel. El cero es que lo hay.
	//
	// Es de la máquina de estados y no de la sala, a diferencia de
	// HostGoneSince: aquello es que falta una persona, esto es que falta la red.
	ReconnectingSince time.Time

	// Rejoining dice que este invitado está volviendo a pedir credencial AHORA.
	//
	// Existe para la pantalla y nada más. Sin esto, un reingreso son diez
	// segundos en los que lo único que se ve es el cartel de que el host no está,
	// que además ya es falso cuando el reingreso lo dispara el aviso del host.
	//
	// **No es un estado de conexión y por eso no está en la máquina de estados.**
	// Pasar a Reconectando arrancaría el corte de diez minutos, y acá el túnel del
	// invitado nunca se cayó. Ver [usecase.Rejoin].
	//
	// Se publica ANTES de empezar, que es el único momento en que sirve: el
	// reingreso tiene el candado de la sesión tomado mientras corre.
	Rejoining bool

	// Game es el juego activo, y el cero es un estado válido y el que trae una
	// sala recién creada: red cifrada, cero puertos abiertos.
	Game GameProfile

	// LocalIP es la IP de kanpachi0 en esta máquina.
	LocalIP netip.Addr
	Subnet  netip.Prefix

	Net    NetCheck
	Alerts []Alert

	// Canary es la última ronda del canario. El cero es que no se comprobó.
	//
	// Va en el estado y no detrás de un método como el sondeo, porque esto lo
	// produce el daemon SOLO, igual que las alertas. El usuario no va a pulsar
	// un botón para descubrir que su protección se cayó.
	Canary CanaryCheck

	// CodeLost dice que el registro ya no conoce este invite ID.
	//
	// No es "el registro no contesta", que es transitorio y se arregla solo en
	// el siguiente latido. Es la respuesta explícita de que esa sala no existe
	// para él, y la produce un reinicio del registro o un fijado vencido: en
	// los dos casos la entrada se fue, y publicar NO crea, así que el código
	// repartido queda muerto para siempre por más que la sala siga en pie.
	//
	// Va en el estado porque es lo único que el usuario puede accionar: sin
	// esto, su sala funciona para los que ya están dentro y le dice "ese código
	// no existe" a todo el que intente entrar, sin una sola pista de por qué.
	// Lo cierra renovar el código, que saca una entrada nueva.
	CodeLost bool

	// SeedDown dice que el registro NO CONTESTA, que es el hermano transitorio
	// de [RoomState.CodeLost].
	//
	// La distinción es toda la razón de que sean dos campos y no uno. Que el
	// registro afirme no conocer un código no se arregla nunca, porque publicar
	// no crea; que no conteste se arregla solo en cuanto vuelva. Un solo campo
	// obligaría a la pantalla a decir lo mismo en los dos casos, y solo uno de
	// los dos pide hacer algo.
	//
	// # Por qué está en el estado y no es solo un error de la operación
	//
	// Porque desde que crear y entrar fallan rápido sin registro, no tenerlo
	// impide usar Kanpachi entero. Enterarse al pulsar el botón es enterarse
	// después de elegir un juego y escribir ocho caracteres, y esto se puede
	// saber con la pantalla todavía vacía. Es la misma razón por la que el
	// adaptador virtual se sondea al arrancar el daemon.
	//
	// Lo recalcula el barrido, así que se apaga solo. No es accionable por el
	// usuario y no lleva botón: no hay nada que pulsar salvo esperar.
	SeedDown bool

	// Quarantine is the base quarantine as MEASURED on this machine, refreshed
	// by the same sweep as the alerts. Machine-level like SeedDown: it
	// describes the machine and not the room, so clearRoom leaves it alone.
	// Its zero verdict is "could not check", never "absent".
	Quarantine QuarantineState

	// Gen sube en CADA vaciado de la sala, e identifica a la sala viva.
	//
	// Existe por la ronda del canario, que suelta el candado hasta diez segundos
	// para salir a la red. En ese hueco el host puede salir y crear otra sala, y
	// sin esto la ronda vieja despertaría y escribiría su conclusión en la sala
	// nueva: un verde medido contra otros miembros y otras reglas, con la hora de
	// ahora. La simétrica es peor, una alarma colgada de una sala que ya no
	// existe.
	//
	// Sube en [RoomState.clearRoom] y no en cada llamador a propósito: así lo
	// heredan por construcción los cinco caminos que llegan a Idle, y ninguno
	// futuro se puede olvidar. Es interno y no viaja por el cable.
	Gen uint64

	// LastExit es por qué se volvió a Idle, y sobrevive a limpiar la sala.
	// Sin esto, que te expulsen, que el host desaparezca y salir por tu cuenta
	// se ven exactamente igual desde la pantalla de inicio.
	LastExit ExitReason

	// Displaces is what entering a room would cost right now, and its zero is
	// "nothing", which is the ordinary case.
	//
	// It rides in the state because every face already asks for the state, and
	// because working it out is the daemon's job: three faces each deciding when
	// to ask for confirmation is three copies of one rule, drifting. See
	// [Displacement].
	//
	// **A projection, like [RoomState.Returning].** Computed on the way out of a
	// snapshot, never stored.
	Displaces Displacement

	// Returning is the room this machine is on its way back into. Its zero means
	// it is not going back anywhere. See [ReturnAttempt].
	//
	// **It is a projection and never a stored value.** Nothing writes it into the
	// live state: it is computed when a snapshot goes out, from the saved room on
	// disk and the clocks of the attempt. Setting it here would be a latch, and a
	// latch is how a machine ends up going back to a room nobody wants any more.
	Returning ReturnAttempt
}

// ReturnAttempt is a machine on its way back into a room it was in.
//
// # What it is NOT
//
// It is not [StateReconnecting], which is a tunnel that dropped while still
// INSIDE a live room and is bounded by [timing.ReconnectLimit]. It is not `Rejoining`
// either, which is a guest asking its host for a credential again, also from
// inside. This is being in no room at all and still trying, with no cap: what
// ends it is the room ceasing to exist, or a person saying so.
//
// # Where it comes from
//
// Not from an event. It is DERIVED, on every snapshot, from three facts that are
// all already true somewhere else: not being in a room, having a saved last
// room, and that room's `AutoReturn` being on. Same discipline as `Degraded`,
// which is recomputed from the member table and never remembered.
//
// The intent behind it is written when somebody ENTERS a room successfully, and
// only then. Retrying is for getting back somewhere you were, never for getting
// into somewhere that never let you in.
type ReturnAttempt struct {
	// Room and Name come from the saved room, and are what a screen needs to say
	// where it is going.
	Room Room
	Name string

	// NextAt is when the next attempt is due. Zero while one is running.
	NextAt time.Time

	// Attempts counts what has been tried, for the screen. It does not bound
	// anything: there is no number of failures that means the room is gone.
	Attempts int

	// Reason is why the last attempt failed, in the daemon's own words. Empty
	// before the first one.
	Reason string
}

// Returning says whether this machine is on its way back into a room.
func (r ReturnAttempt) Returning() bool { return !r.Room.InviteID.IsZero() }

// Clone devuelve una copia que no comparte nada mutable con el original.
//
// Copiar el struct a secas NO alcanza: los slices quedarían apuntando al mismo
// array, y quien reordenara su lista de miembros se la movería a todos los
// demás. Es el tipo que sale del daemon hacia el named pipe, así que la copia
// tiene que ser profunda donde hay slices y no en ningún otro sitio.
func (r RoomState) Clone() RoomState {
	out := r
	out.Peers = append([]Peer(nil), r.Peers...)
	out.Alerts = append([]Alert(nil), r.Alerts...)
	if r.Net.SeedRTT != nil {
		out.Net.SeedRTT = make(map[string]time.Duration, len(r.Net.SeedRTT))
		for k, v := range r.Net.SeedRTT {
			out.Net.SeedRTT[k] = v
		}
	}
	// El juego lleva slices propios. Un perfil no se muta después de
	// construido, y copiarlos cuesta dos asignaciones en una operación que ya
	// serializa la sala entera.
	out.Game.HostPorts = append([]PortRange(nil), r.Game.HostPorts...)
	out.Game.ClientPorts = append([]PortRange(nil), r.Game.ClientPorts...)
	out.Game.Detect.Executables = append([]string(nil), r.Game.Detect.Executables...)
	// La comprobación del canario lleva dos slices, y la ronda los ESCRIBE
	// mientras Status() entrega esta copia al serializador del pipe. Es el mismo
	// aliasing del que avisa el párrafo de arriba, con un escritor más real
	// todavía: acá el que muta corre en otra goroutine por diseño.
	out.Canary.Asked = append([]CanaryAsked(nil), r.Canary.Asked...)
	out.Canary.Answers = append([]CanaryAnswer(nil), r.Canary.Answers...)
	// The measured quarantine carries the port list. Nobody mutates it after
	// the sweep stores it, and it gets copied anyway by the same argument as
	// the game profile above: one assignment in an operation that already
	// serialises the whole room.
	out.Quarantine.Ports = append([]uint16(nil), r.Quarantine.Ports...)
	return out
}

// Transition mueve el estado y deja constancia del motivo.
func (r *RoomState) Transition(next ConnState, reason string) error {
	return r.TransitionWithExit(next, reason, ExitNone)
}

// TransitionWithExit es lo mismo, diciendo por qué se sale.
//
// El motivo solo se guarda al llegar a Idle, y solo si se da uno: una
// transición entre dos estados de sala no explica ninguna salida, y pisar el
// motivo anterior con un cero borraría justo lo que la pantalla de inicio
// necesita.
func (r *RoomState) TransitionWithExit(next ConnState, reason string, exit ExitReason) error {
	if !r.Conn.CanGoTo(next) {
		return ErrBadTransition{From: r.Conn, To: next, Reason: reason}
	}
	r.Conn = next
	if next == StateIdle {
		r.clearRoom()
		if exit != ExitNone {
			r.LastExit = exit
		}
	}
	return nil
}

// clearRoom devuelve la sesión al cero.
//
// Borra la sala entera y no solo el estado porque volver a Idle es haber
// salido, y dejar el invite ID, los peers o el juego colgando haría que la
// pantalla de inicio mostrara restos de la sala anterior. La subred y el
// diagnóstico se conservan: describen la máquina, no la sala.
func (r *RoomState) clearRoom() {
	// La subred se conserva solo si la eligió ESTA máquina. En un host
	// describe el plan de direcciones y sigue valiendo para el diagnóstico; en
	// un invitado vino dentro de la credencial del host, así que al salir pasa
	// a describir una sala que ya no existe, y dejarla ahí hace que
	// Diagnostics reporte un rango que nadie está usando.
	if r.Role == RoleGuest {
		r.Subnet = netip.Prefix{}
		r.Net.Subnet = netip.Prefix{}
		r.Net.SubnetReason = ""
	}
	r.Role = 0
	r.Room = Room{}
	r.Name = ""
	r.Peers = nil
	r.HostPresent = false
	r.HostGoneSince = time.Time{}
	r.HostLastHeard = time.Time{}
	r.ReconnectingSince = time.Time{}
	r.Game = GameProfile{}
	r.LocalIP = netip.Addr{}
	r.Alerts = nil
	r.Canary = CanaryCheck{}
	r.CodeLost = false

	// Y se sube la generación, que es lo que le deja a una ronda en vuelo saber
	// que la sala que estaba midiendo ya no existe. Va acá, en el único sitio que
	// vacía, para que los cinco caminos que llegan a Idle lo hereden sin que
	// nadie tenga que acordarse. Ver [RoomState.Gen].
	r.Gen++
}

// IsHost es la propiedad que decide qué operaciones se admiten. Solo el host
// elige juego, expulsa y renueva el código.
func (r RoomState) IsHost() bool { return r.Role == RoleHost }

// ShouldLeaveForHostAbsence implementa el contador de la decisión 20.
//
// Recibe el ahora por parámetro y no lee el reloj: el dominio no conoce el
// tiempo del sistema, entra por [port.Clock]. Sin ese corte, probar veinte
// minutos costaría veinte minutos.
//
// Solo aplica a invitados. Un host no puede estar ausente de su propia sala, y
// si el campo se rellenara por error en un host, el resultado sería que el
// host se echa a sí mismo de la sala que hospeda.
func (r RoomState) ShouldLeaveForHostAbsence(now time.Time) bool {
	if r.Role != RoleGuest || r.HostPresent || r.HostGoneSince.IsZero() {
		return false
	}
	return now.Sub(r.HostGoneSince) >= timing.HostAbsenceLimit
}

// SetHostPresent registra la presencia y arranca o para el contador.
//
// El hecho que alimenta esto es la conexión de control al host: si el socket
// está, el host está. Es información confiable sin necesidad de confiar en
// nadie, porque no es un mensaje que alguien pueda falsificar, es la ausencia
// de un socket. Ver decisión 23.
func (r *RoomState) SetHostPresent(present bool, now time.Time) {
	if present {
		r.HostPresent = true
		r.HostGoneSince = time.Time{}
		// Que el socket esté levantado es prueba de vida, así que sella el
		// reloj del silencio por el mismo acto. Sin esto habría dos verdades
		// sobre lo mismo, la del flanco y la del plazo, y se desincronizarían.
		r.HostLastHeard = now
		return
	}
	// No se pisa la marca si ya estaba ausente: el contador cuenta desde la
	// primera caída, y reiniciarlo en cada latido fallido lo dejaría en cero
	// para siempre.
	if r.HostPresent || r.HostGoneSince.IsZero() {
		r.HostGoneSince = now
	}
	r.HostPresent = false
}

// NoteHostAlive registra una prueba de vida del host.
//
// Existe con nombre propio, en vez de llamar a [RoomState.SetHostPresent], para
// que los sitios que la llaman se lean como lo que son: EVIDENCIA de que el
// host está, no el flanco de un socket. Un anuncio que llega, un aviso que
// llega, su dirección en la tabla de peers.
//
// Solo en invitados. Un host no se prueba vida a sí mismo.
func (r *RoomState) NoteHostAlive(now time.Time) {
	if r.Role != RoleGuest {
		return
	}
	r.SetHostPresent(true, now)
}

// HostSilent dice si pasó [timing.HostSilenceLimit] sin oír nada del host.
//
// Es la señal que llega sola cuando la caída del socket no llega nunca. Solo en
// invitados, y solo si alguna vez se oyó algo: sin marca previa no hay silencio
// que medir, hay un ingreso a medio hacer.
func (r RoomState) HostSilent(now time.Time) bool {
	if r.Role != RoleGuest || r.HostLastHeard.IsZero() {
		return false
	}
	return now.Sub(r.HostLastHeard) >= timing.HostSilenceLimit
}

// SetTunnelDown marca que no hay túnel, sin pisar la marca previa.
//
// No pisarla es la misma razón que en [RoomState.SetHostPresent]: el plazo
// cuenta desde la primera caída, y un motor que reporta la desconexión tres
// veces seguidas dejaría el contador en cero para siempre.
func (r *RoomState) SetTunnelDown(now time.Time) {
	if r.ReconnectingSince.IsZero() {
		r.ReconnectingSince = now
	}
}

// SetTunnelUp borra la marca. El túnel volvió.
func (r *RoomState) SetTunnelUp() { r.ReconnectingSince = time.Time{} }

// TunnelDown dice si se está sin túnel ahora mismo.
func (r RoomState) TunnelDown() bool { return !r.ReconnectingSince.IsZero() }

// ShouldLeaveForReconnectTimeout es el hermano de
// [RoomState.ShouldLeaveForHostAbsence], del lado de la red.
//
// Aplica a los dos roles, y ahí está la diferencia con el otro: un host sin
// túnel tampoco tiene sala, y sostenerla abierta dejaría sus puertos aplicados
// hacia miembros que no puede alcanzar.
func (r RoomState) ShouldLeaveForReconnectTimeout(now time.Time) bool {
	if !r.Conn.InRoom() || r.ReconnectingSince.IsZero() {
		return false
	}
	return now.Sub(r.ReconnectingSince) >= timing.ReconnectLimit
}

// AnyRelay dice si algún OTRO miembro llega por relay ahora mismo.
//
// Uno mismo no cuenta: [PathSelf] no es un camino, y un peer sin camino
// conocido tampoco es una degradación, es una tabla a medio llenar.
func (r RoomState) AnyRelay() bool {
	for _, p := range r.Peers {
		if !p.Self && p.Path == PathRelay {
			return true
		}
	}
	return false
}

// ConnFromPeers es el estado que los HECHOS implican, entre conectado y
// degradado. No decide nada más.
//
// # El fallo que esto cierra, medido con el producto entero
//
// Degradado era un PESTILLO que nadie soltaba. Lo ponía el evento `degraded`
// del motor, y volver a conectado exigía un evento `connected`, que el motor
// emite en UN solo sitio: cuando SUBE el adaptador virtual. Un corte de red no
// tira el adaptador, así que no había vuelta.
//
// Medido el 2026-08-05: doce segundos con la WiFi apagada dejaron la sala en
// degradado, y ciento cincuenta segundos después seguía en degradado con la red
// entera recuperada, los dos adaptadores arriba y un solo miembro, que era uno
// mismo. Una sala de uno no puede estar degradada: no hay nadie con quien ir
// por relay.
//
// Por eso el estado se DERIVA de la tabla de miembros en vez de recordarse. Es
// la misma doctrina que [Session.tunnelUpLocked], que recalcula todo en vez de
// suponer que nada cambió.
func (r RoomState) ConnFromPeers() ConnState {
	if r.AnyRelay() {
		return StateDegraded
	}
	return StateConnected
}

// Self devuelve el peer que es esta máquina.
func (r RoomState) Self() (Peer, bool) {
	for _, p := range r.Peers {
		if p.Self {
			return p, true
		}
	}
	return Peer{}, false
}

// HostPeer devuelve el peer que hospeda, si está.
func (r RoomState) HostPeer() (Peer, bool) {
	for _, p := range r.Peers {
		if p.Host {
			return p, true
		}
	}
	return Peer{}, false
}
