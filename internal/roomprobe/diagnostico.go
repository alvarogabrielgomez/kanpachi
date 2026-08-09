package main

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"sort"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/core/port"
	"github.com/accentiostudios/kanpachi/core/usecase"
	kanpachiengine "github.com/accentiostudios/kanpachi/daemon/adapter/engine/kanpachi"
)

// volcarDiagnostico escribe de una vez todo lo que hace falta para contestar
// "por qué no conectan".
//
// # Qué preguntas contesta, y en qué orden
//
// El orden no es estético, es el de la cadena: cada bloque solo tiene sentido
// si el anterior salió bien, así que el primero que sale mal es la respuesta.
//
//  1. ¿Hay sala, y con qué identidad? Sin esto no hay nada que diagnosticar.
//  2. ¿Existen los adaptadores y tienen la dirección que debían? Se lee del
//     SISTEMA, no de lo que la sesión cree. Si el motor no levantó, se ve acá.
//  3. ¿El motor ve al otro lado, y por dónde? Directo, por relay, o nada.
//  4. ¿El firewall de esta máquina deja pasar el canal de control, y HACIA
//     QUIÉN? Este es el bloque que faltaba el día que ningún invitado podía
//     entrar: la regla de la sala no existía y nada lo decía.
//  5. ¿Qué dice el motor de la red de esta máquina? NAT, UDP, seeds.
//  6. ¿Qué plazos están corriendo? Es la mitad del diagnóstico cuando el
//     síntoma es "se desconectó solo".
//
// Sale por el LOG y no por pantalla: se pide justo cuando hay que mandarle el
// fichero a alguien.
func volcarDiagnostico(ctx context.Context, s *usecase.Session, log port.Logger, seed string) {
	st := s.Status()
	ahora := time.Now()

	log.Info("======== DIAGNÓSTICO DE LA SALA ========")

	// 1. Identidad
	log.Info("sala", "rol", st.Role.String(), "estado", st.Conn.String(),
		"código", st.Room.InviteID.String(), "seed", st.Room.Seed,
		"registro-de-esta-máquina", seed,
		"subred", st.Subnet.String(), "mi-ip", st.LocalIP.String())
	if st.CodeLost {
		log.Warn("el registro ya no conoce este código: nadie nuevo puede entrar aunque la sala siga en pie")
	}

	// 2. Adaptadores, leídos del sistema
	volcarAdaptadores(log, st)

	// 3. Miembros
	if len(st.Peers) == 0 {
		log.Warn("miembros: NINGUNO. El motor no ve a nadie más en la red de la sala")
	}
	for _, p := range st.Peers {
		log.Info("  miembro", "nombre", p.Name.String(), "ip", p.VirtualIP.String(),
			"camino", p.Path.String(), "rtt", p.RTT.String(),
			"yo", p.Self, "host", p.Host)
	}

	// 4. Firewall: lo que está PUESTO, no lo que se pidió
	volcarExposicion(ctx, s, log, st)

	// 5. La red de esta máquina, según el motor
	volcarMotor(ctx, s, log, st)

	// 6. El camino hasta el seed, medido desde acá. Va aparte del motor porque
	// el motor no lo cuenta.
	volcarSeed(ctx, log, seed)

	// 7. Plazos y presencia
	volcarPlazos(log, st, ahora)

	// Y las alertas, que son la lista de lo que el propio producto considera
	// roto ahora mismo.
	if len(st.Alerts) == 0 {
		log.Info("alertas: ninguna")
	}
	for _, a := range st.Alerts {
		// El tipo va como número porque `AlertKind` no tiene `String`: lo que
		// se lee es el detalle, que ya viene redactado para una persona.
		log.Warn("  ALERTA", "tipo", uint8(a.Kind), "detalle", a.Detail)
	}

	log.Info("======== FIN DEL DIAGNÓSTICO ========")
}

// volcarAdaptadores lee las interfaces DEL SISTEMA y las compara con lo que la
// sesión cree.
//
// La comparación es el punto entero. La sesión puede tener una `LocalIP`
// perfecta y el adaptador puede no existir: pasa cuando el motor se murió y el
// watchdog todavía no lo notó, y el síntoma para el usuario es una sala que se
// ve bien y por la que no pasa un paquete.
func volcarAdaptadores(log port.Logger, st domain.RoomState) {
	ifaces, err := net.Interfaces()
	if err != nil {
		log.Error("no se pudieron leer las interfaces de esta máquina", "error", err)
		return
	}

	visto := map[string]bool{}
	for _, in := range ifaces {
		if in.Name != kanpachiengine.RoomDevice && in.Name != kanpachiengine.LobbyDevice {
			continue
		}
		visto[in.Name] = true

		var dirs []string
		if addrs, err := in.Addrs(); err == nil {
			for _, a := range addrs {
				dirs = append(dirs, a.String())
			}
		}
		sort.Strings(dirs)
		log.Info("  adaptador", "nombre", in.Name, "mtu", in.MTU,
			"arriba", in.Flags&net.FlagUp != 0, "direcciones", dirs)
	}

	// La ausencia se dice, que es lo contrario de omitirla. Con sala abierta,
	// que falte kanpachi0 es el fallo entero y no un detalle.
	if !visto[kanpachiengine.RoomDevice] {
		if st.Conn.InRoom() {
			log.Error("  adaptador " + kanpachiengine.RoomDevice + " NO EXISTE, y hay una sala abierta: el motor no levantó la red")
		} else {
			log.Info("  adaptador " + kanpachiengine.RoomDevice + " no existe (correcto, no hay sala)")
		}
	}
	if !visto[kanpachiengine.LobbyDevice] {
		log.Info("  adaptador " + kanpachiengine.LobbyDevice + " no existe (el invitado lo suelta al entrar a la sala, y es lo correcto)")
	}
}

// volcarExposicion enseña los huecos que el sistema tiene PUESTOS.
//
// `Applied` es el campo que importa y el que cierra el diagnóstico de un
// invitado que no entra: Kanpachi pidió abrir el canal de control y el firewall
// no lo tiene abierto. Y `Members` vacío en la regla de la sala es el fallo de
// la v0.1.6 en una línea.
func volcarExposicion(ctx context.Context, s *usecase.Session, log port.Logger, st domain.RoomState) {
	rep := s.Exposure(ctx)
	if rep.Blind() {
		log.Error("firewall: NO SE PUDO MEDIR lo que la máquina tiene puesto. " +
			"Todo lo que siga sobre puertos es desconocido, no es un cero")
		return
	}

	log.Info("firewall", "compuerta", rep.Gate.String(), "huecos", len(rep.Ports),
		"medido", rep.MeasuredAt.Format("15:04:05"))

	dejaPasar := map[netip.Addr]bool{}
	for _, p := range rep.Ports {
		nivel := log.Info
		if !p.Applied {
			nivel = log.Error
		}
		nivel("  hueco", "nombre", p.Name, "proto", p.Proto.String(),
			"puertos", rangoPuertos(p.From, p.To), "puesto", p.Applied,
			"canal-de-control", p.Control, "hacia", p.Members, "redes", p.Nets)

		if p.Control && p.Applied {
			for _, ip := range p.Members {
				dejaPasar[ip] = true
			}
		}
	}

	for _, u := range rep.Unexpected {
		log.Warn("  regla del grupo de Kanpachi que nadie pidió", "nombre", u)
	}

	// El contraste que habría ahorrado la investigación entera: lo que este
	// host repartió contra lo que la regla deja pasar.
	//
	// Las dos listas juntas o ninguna. Con la regla sola no se distingue una
	// sala sin nadie dentro —que no tiene regla de sala, y hace bien— del fallo
	// de la v0.1.6, y confundirlas fue exactamente lo que pasó.
	if st.Role != domain.RoleHost || !st.Conn.InRoom() {
		return
	}
	emitidas := s.IssuedAddresses()
	var sinHueco []netip.Addr
	for _, ip := range emitidas {
		if !dejaPasar[ip] {
			sinHueco = append(sinHueco, ip)
		}
	}
	switch {
	case len(emitidas) == 0:
		log.Info("  no hay ninguna credencial repartida todavía, así que tampoco hay " +
			"regla de sala. Es lo correcto: aparece cuando alguien canjea el código")
	case len(sinHueco) > 0:
		log.Error("NADIE con estas direcciones puede abrir el canal de control de la sala",
			"sin-hueco", sinHueco, "repartidas", len(emitidas))
		log.Error("  se quedan esperando en un dial que nadie contesta. " +
			"Es exactamente el fallo de la v0.1.6")
	default:
		log.Info("  todas las direcciones repartidas tienen hueco de control",
			"direcciones", emitidas)
	}
}

// volcarMotor pregunta por la red de ESTA máquina.
//
// Sin sala no hay motor corriendo y la respuesta es "there is no room running".
// Eso no es un fallo, es lo correcto, y anotarlo como error llenaba de rojo el
// volcado de un invitado que justamente no había podido entrar.
func volcarMotor(ctx context.Context, s *usecase.Session, log port.Logger, st domain.RoomState) {
	if !st.Conn.InRoom() {
		log.Info("no hay sala abierta, así que no hay motor al que preguntar por la red")
		return
	}
	chk, err := s.Diagnose(ctx)
	if err != nil {
		log.Error("el motor no contestó al diagnóstico", "error", err)
		return
	}
	log.Info("red de esta máquina", "nat", chk.NATKind, "udp-bloqueado", chk.UDPBlocked,
		"mtu", chk.MTU, "subred", chk.Subnet.String(), "por-qué", chk.SubnetReason)

	if chk.UDPBlocked {
		log.Warn("  el UDP de salida parece bloqueado: el camino directo no se va " +
			"a poder abrir y todo irá por relay, si es que hay relay")
	}
}

// volcarSeed mide el camino hasta el seed, que es lo primero que hay que
// descartar antes de mirar el firewall: sin seed las dos máquinas no se
// encuentran, y todo lo demás da igual.
//
// No sale del motor. Ver [medirSeed].
func volcarSeed(ctx context.Context, log port.Logger, host string) {
	rs, err := medirSeed(ctx, host)
	if err != nil {
		log.Error("el seed no se pudo ni resolver", "seed", host, "error", err)
		return
	}
	for _, r := range rs {
		if r.Err != nil {
			log.Error("  seed inalcanzable", "dirección", r.Dir.String(),
				"puerto", kanpachiengine.SeedPort, "error", r.Err)
			continue
		}
		log.Info("  seed alcanzable", "dirección", r.Dir.String(),
			"puerto", kanpachiengine.SeedPort, "rtt", r.RTT.Round(time.Millisecond).String())
	}
	if !alguienContesto(rs) {
		log.Error("ninguna dirección del seed contestó. Sin seed no hay por dónde " +
			"encontrarse, salvo que las dos máquinas estén en la misma LAN")
	}
}

// volcarPlazos enseña los relojes que están corriendo.
//
// Es la mitad del diagnóstico cuando el síntoma es "se desconectó solo": sin
// esto, los veinte minutos del contador de ausencia del host se viven como una
// desconexión inexplicable.
func volcarPlazos(log port.Logger, st domain.RoomState, ahora time.Time) {
	if st.Role != domain.RoleGuest {
		return
	}
	log.Info("presencia del host", "presente", st.HostPresent,
		"ausente-desde", desde(st.HostGoneSince, ahora),
		"última-señal", desde(st.HostLastHeard, ahora))

	if !st.HostPresent && !st.HostGoneSince.IsZero() {
		queda := domain.HostAbsenceLimit - ahora.Sub(st.HostGoneSince)
		log.Warn("  esta máquina saldrá sola de la sala si el host no vuelve",
			"queda", queda.Round(time.Second).String())
	}
	if !st.ReconnectingSince.IsZero() {
		queda := domain.ReconnectLimit - ahora.Sub(st.ReconnectingSince)
		log.Warn("  sin túnel", "desde", desde(st.ReconnectingSince, ahora),
			"se cierra la sala en", queda.Round(time.Second).String())
	}
}

// desde pinta cuánto hace de un instante. El cero es "nunca", que es distinto
// de "hace un momento" y confundirlos manda a leer la hora del sistema.
func desde(t, ahora time.Time) string {
	if t.IsZero() {
		return "nunca"
	}
	return ahora.Sub(t).Round(time.Second).String()
}

// rangoPuertos es el mismo formato que usa el log del núcleo.
func rangoPuertos(desde, hasta uint16) string {
	if desde == hasta {
		return fmt.Sprint(desde)
	}
	return fmt.Sprintf("%d-%d", desde, hasta)
}
