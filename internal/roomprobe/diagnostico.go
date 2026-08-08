package main

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"sort"
	"strings"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/core/port"
	"github.com/accentiostudios/kanpachi/core/usecase"
	kanpachiengine "github.com/accentiostudios/kanpachi/daemon/adapter/engine/kanpachi"
)

// espejoDiario copia al log el diario de pasos de la operación en curso.
//
// # Por qué existe
//
// Cuando alguien no puede entrar a una sala, la respuesta ya está escrita: el
// diario narra los doce pasos del ingreso, y el último que aparece es donde se
// atascó. Eso es lo que la pantalla enseña en "ver detalles", y hasta ahora era
// **lo único** que lo sabía: el log no veía ni uno solo de esos pasos.
//
// El caso real, del 2026-08-07: el diario del invitado decía `kanpachi1 listo
// con 100.127.255.73`, `pidiéndole al host la credencial de la sala`, y ahí se
// paraba con `dial tcp 10.99.184.1:57623: connectex: ... did not properly
// respond`. Con esas tres líneas en un fichero, el fallo era de media hora. Sin
// ellas hubo que pedir capturas de pantalla.
//
// # Por qué se sondea en vez de envolver
//
// `usecase.Deps.Progress` es un `*usecase.Journal` concreto, no una interfaz, y
// no se puede envolver. Sondearlo captura además los pasos que escriben los
// ADAPTADORES en el mismo diario, que son justo los que dicen cuánto tardó el
// motor en tomar dirección.
type espejoDiario struct {
	s   *usecase.Session
	log port.Logger

	op      string
	vistos  int
	abierta bool
}

// espejoCadencia es cada cuánto se mira el diario.
//
// Doscientos milisegundos contra pasos que duran segundos: no se pierde
// ninguno, y una operación entera cuesta un puñado de lecturas de un candado
// que no es el de la sesión. `Progress()` no toma el candado de la sesión
// justamente para poder mirarse mientras `CreateRoom` lo tiene tomado un
// minuto entero.
const espejoCadencia = 200 * time.Millisecond

func (e *espejoDiario) correr(ctx context.Context) {
	t := time.NewTicker(espejoCadencia)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			e.mirar() // un último vistazo: lo que falló al apagar también cuenta
			return
		case <-t.C:
			e.mirar()
		}
	}
}

func (e *espejoDiario) mirar() {
	p := e.s.Progress()
	if p.Op == "" {
		return
	}

	// Operación nueva. El diario pisa la anterior en vez de acumular, así que
	// dos ingresos seguidos traen el mismo nombre con la lista reiniciada: por
	// eso no alcanza con comparar el nombre, hay que ver si la lista encogió.
	if p.Op != e.op || len(p.Steps) < e.vistos {
		e.op = p.Op
		e.vistos = 0
		e.abierta = true
		e.log.Info("=== " + p.Op + " ===")
	}

	for i := e.vistos; i < len(p.Steps); i++ {
		s := p.Steps[i]
		e.log.Info("  paso", "en", s.Since.Round(time.Millisecond).String(),
			"alcance", string(s.Scope), "texto", s.Text)
	}
	e.vistos = len(p.Steps)

	if p.Dropped > 0 {
		e.log.Warn("  el diario tiró pasos del medio por el tope", "cantidad", p.Dropped)
	}

	if !p.Running && e.abierta {
		e.abierta = false
		switch {
		case p.Failure != "":
			e.log.Error("=== "+p.Op+": FALLÓ ===",
				"duró", p.Elapsed.Round(time.Millisecond).String(), "error", p.Failure)
		default:
			e.log.Info("=== "+p.Op+": listo ===",
				"duró", p.Elapsed.Round(time.Millisecond).String())
		}
	}
}

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

// latidoMalla es cada cuánto se le pregunta al motor quién hay en la malla.
//
// Un segundo: lo que se persigue son los veintiún segundos que un invitado pasa
// marcando al host, y con el latido de quince del supervisor eso son una o dos
// muestras. Cuesta un mensaje por la tubería del motor, que es barato.
const latidoMalla = 1 * time.Second

// malla es lo poco que el vigía necesita del motor.
type malla interface {
	Peers(context.Context) ([]domain.Peer, error)
}

// espejoMalla vigila la malla del motor y anota CADA CAMBIO.
//
// # Por qué le pregunta al motor y no a la sesión
//
// Porque durante un ingreso la sesión tiene el candado tomado de punta a punta:
// su lista de miembros no se refresca hasta que la operación termina, y el
// supervisor se queda esperando detrás. El resultado se midió el 2026-08-08. Un
// invitado marcó al host durante veintiún segundos, se rindió, y NINGUNO de los
// dos logs guardó lo único que hacía falta: si los dos motores llegaron a verse
// en la red de la sala. Los dos volcados de miembros se tomaron después del
// desmontaje, o sea de una sala que ya no existía.
//
// Sin ese dato, "el firewall no deja pasar" y "todavía no hay camino" se ven
// idénticos desde fuera, y son arreglos opuestos.
//
// El adaptador del motor habla por su propia tubería y no toca el candado de la
// sesión, así que esto sigue vivo justo cuando todo lo demás está bloqueado.
//
// # Qué NO es
//
// No es un latido: se anota el cambio, no el tick. Sin cambios no escribe una
// sola línea, que es lo que lo hace legible durante una espera larga.
type espejoMalla struct {
	motor malla
	log   port.Logger
}

func (m *espejoMalla) correr(ctx context.Context) {
	t := time.NewTicker(latidoMalla)
	defer t.Stop()

	// El arranque no cuenta como cambio: sin sala no hay malla, y anunciarlo
	// sería una línea de ruido en cada arranque.
	firma := "sin sala"
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}

		peers, err := m.motor.Peers(ctx)
		if err != nil {
			// "there is no room running" es la respuesta NORMAL fuera de una
			// sala, y tratarla como fallo llenaría el log de errores durante
			// todo el tiempo que alguien pasa en el menú.
			firma = "sin sala"
			continue
		}

		nueva := firmaDeMalla(peers)
		if nueva == firma {
			continue
		}
		anterior := firma
		firma = nueva

		if len(peers) == 0 {
			// Que se vacíe con la sala en pie es un hecho, y de los gordos.
			if anterior != "sin sala" {
				m.log.Warn("la malla se quedó vacía: el motor ya no ve a nadie más")
			}
			continue
		}
		for _, p := range peers {
			if p.Self {
				continue
			}
			m.log.Info("MALLA: el motor ve a alguien", "ip", p.VirtualIP.String(),
				"nombre", p.Name.String(), "camino", p.Path.String(),
				"rtt", p.RTT.String(), "host", p.Host)
		}
	}
}

// firmaDeMalla resume la malla para poder comparar dos muestras.
//
// Lleva el CAMINO además de la dirección: que un miembro pase de relay a
// directo es un cambio que interesa, y con la dirección sola sería invisible.
// No lleva el RTT, que se mueve solo y convertiría esto en un latido.
func firmaDeMalla(peers []domain.Peer) string {
	partes := make([]string, 0, len(peers))
	for _, p := range peers {
		partes = append(partes, p.VirtualIP.String()+"/"+p.Path.String())
	}
	sort.Strings(partes)
	return strings.Join(partes, ",")
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
