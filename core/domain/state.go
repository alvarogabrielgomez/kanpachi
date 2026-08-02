package domain

import (
	"fmt"
	"net/netip"
	"time"
)

// HostAbsenceLimit es la salida automática de la decisión 20.
//
// Veinte minutos y no dos: el margen tiene que cubrir un reinicio del host con
// holgura. Una salida inmediata echaría a todo el mundo cada vez que al host
// se le cierra el juego, que es el caso más común de todos.
//
// Es política LOCAL pura. No hay mensaje, no hay coordinación y no hay que
// confiar en nadie: cada máquina decide sobre sí misma a partir de un hecho
// que no se puede falsificar, que su socket al host no está.
const HostAbsenceLimit = 20 * time.Minute

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

	// Game es el juego activo, y el cero es un estado válido y el que trae una
	// sala recién creada: red cifrada, cero puertos abiertos.
	Game GameProfile

	// LocalIP es la IP de kanpachi0 en esta máquina.
	LocalIP netip.Addr
	Subnet  netip.Prefix

	Net    NetCheck
	Alerts []Alert

	// LastExit es por qué se volvió a Idle, y sobrevive a limpiar la sala.
	// Sin esto, que te expulsen, que el host desaparezca y salir por tu cuenta
	// se ven exactamente igual desde la pantalla de inicio.
	LastExit ExitReason
}

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
	r.Game = GameProfile{}
	r.LocalIP = netip.Addr{}
	r.Alerts = nil
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
	return now.Sub(r.HostGoneSince) >= HostAbsenceLimit
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
