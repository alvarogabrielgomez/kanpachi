package usecase

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/netip"
	"sort"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// La Protección Kanpachi: comprobar desde la red que la compuerta contiene.
//
// # Qué comprueba, y por qué nada más lo comprueba
//
// La auditoría local pregunta si el filtro EXISTE, por su clave. No pregunta si
// CASA. Un filtro vivo por su GUID con la condición de interfaz vacía lee como
// presente y no contiene nada, y desde dentro de la máquina eso se ve verde.
//
// La ronda del canario es lo único que ve el paquete cruzar.
//
// # La forma de una ronda
//
//	1. abrir un oyente en un puerto que la compuerta TIENE que bloquear
//	2. pedirle a TODOS los presentes que lo marquen
//	3. cerrar con lo primero de tres: lo tocan, contestan todos, o el plazo
//	4. concluir con lo que vio el SOCKET PROPIO, jamás con lo que dijo nadie
//
// El paso 2 es plural a propósito. Preguntarle a uno sorteado lo elige un
// adversario que sostenga varias membresías, y sostener varias es gratis: el
// código de invitación no es secreto, no hay baneo, y volver a entrar tampoco
// cuesta nada.

const (
	// CanaryRoundDeadline es lo que dura una ronda como mucho.
	//
	// Diez segundos porque un invitado tarda hasta seis en contestar, que son los
	// dos sondeos de tres, y hace falta margen para el viaje de ida y vuelta por
	// el canal de la sala. Una ronda real medida contra el droplet tardó 3376 ms
	// con el ida y vuelta de ssh incluido.
	CanaryRoundDeadline = 10 * time.Second

	// CanaryTTL es lo que el socket puede quedar abierto, y va POR ENCIMA del
	// plazo de ronda a propósito.
	//
	// El tope del adaptador es la red de seguridad para el caso de que este
	// proceso se muera a mitad de una ronda, jamás la espera normal. Si fuera
	// menor que el plazo, cerraría canarios vivos en el camino bueno.
	CanaryTTL = 12 * time.Second

	// CanaryRepairLimit son las reposiciones silenciosas antes de avisar.
	//
	// UNO, y no tres como [TamperRepairLimit], y la diferencia tiene motivo: las
	// dos evidencias no valen lo mismo. La auditoría local dice "falta una
	// clave", que sale en falso por carreras normales. Un toque del canario dice
	// que un paquete CRUZÓ, medido desde otra máquina. Y cada ronda cuesta diez
	// segundos con la compuerta rota, así que tres serían medio minuto de
	// exposición real esperando.
	CanaryRepairLimit = 1
)

// canaryPlan es todo lo que la ronda necesita, leído de una sola vez bajo el
// candado para poder soltarlo antes de salir a la red.
type canaryPlan struct {
	// gen es la sala a la que pertenece esta ronda. Ver [domain.RoomState.Gen].
	gen     uint64
	at      netip.Addr
	nonce   domain.CanaryNonce
	asked   []domain.CanaryAsked
	avoid   func(uint16) bool
	alarmed bool
}

// CanaryDue avisa de que se acaba de aplicar la protección y toca comprobarla.
//
// Lo drena el supervisor, que es quien tiene el bucle. La sesión no lanza
// goroutines propias: una ronda dura hasta diez segundos y quién la corre es
// política de ciclo de vida, no del caso de uso.
func (s *Session) CanaryDue() <-chan struct{} { return s.canaryDue }

// RunCanaryRound corre una ronda entera y devuelve lo que concluyó.
//
// `afterApply` en true es la ronda que sigue a haber aplicado las reglas, y es
// la ÚNICA que corre con la alarma puesta. Con la alarma puesta y sin venir de
// aplicar no se abre nada, y no es ahorro: mientras la protección está caída el
// canario **sí es alcanzable de verdad**, así que seguir abriendo sockets
// alcanzables en una máquina que ya se sabe expuesta sería trabajar en contra.
//
// # El candado
//
// Se toma para leer el plan, se SUELTA para salir a la red, y se vuelve a tomar
// para guardar. Es la disciplina de [Session.ProbeHost], y acá pesa más: una
// ronda tarda segundos, y sostener el candado dejaría a cualquier Status de la
// UI esperando todo ese rato.
func (s *Session) RunCanaryRound(ctx context.Context, afterApply bool) domain.CanaryCheck {
	s.mu.Lock()
	plan, ok := s.canaryPlanLocked(ctx, afterApply)
	s.mu.Unlock()
	if !ok {
		return domain.CanaryCheck{}
	}

	c, err := s.deps.Canary.Listen(plan.at, plan.nonce, CanaryTTL, plan.avoid)
	if err != nil {
		// No es fatal y no levanta alerta. No haber podido comprobar no es un
		// hallazgo, y encender un aviso acá enseñaría a ignorar los avisos.
		s.deps.Log.Warn("no se pudo abrir el canario", "error", err)
		return domain.CanaryCheck{}
	}

	// Se vacía la cola ANTES de preguntar. Informes tardíos de un canario ya
	// cerrado ocupan el buffer del adaptador y se comerían las vueltas del select
	// de esta ronda. Se filtran por puerto igual, así que esto es caudal y no
	// corrección.
	s.drainStaleCanaryReports()

	req := domain.CanaryRequest{Port: c.Port(), Nonce: plan.nonce}
	for _, m := range plan.asked {
		if err := s.deps.Control.RequestCanary(ctx, m.At, req); err != nil {
			s.deps.Log.Warn("no se le pudo pedir el canario a un miembro",
				"a", m.At, "error", err)
		}
	}

	check := s.collectCanary(ctx, c, plan)
	_ = c.Close()

	// DESPUÉS de cerrar, y el orden importa: un toque puede caer entre la última
	// vuelta del select y el cierre, y el hecho propio del host tiene que
	// incluirlo. Leerlo del canal lo perdería.
	check.Touched = c.WasTouched()

	s.mu.Lock()
	defer s.mu.Unlock()
	s.foldCanaryLocked(ctx, plan, check)
	return check
}

// canaryPlanLocked arma el plan, o se niega. Cada negativa es un caso con
// nombre y no una condición compuesta, porque cada una se niega por su motivo.
//
// Asume el candado tomado.
func (s *Session) canaryPlanLocked(ctx context.Context, afterApply bool) (canaryPlan, bool) {
	switch {
	case !s.state.Conn.InRoom():
		// Sin sala no hay adaptador virtual que comprobar.
		return canaryPlan{}, false
	case !s.state.IsHost():
		// El oyente solo existe en el host, y un invitado no tiene a quién
		// pedirle que lo marque.
		return canaryPlan{}, false
	case !s.state.LocalIP.IsValid():
		return canaryPlan{}, false
	}

	asked := make([]domain.CanaryAsked, 0, len(s.state.Peers))
	for _, p := range s.state.Peers {
		if p.Self || !p.VirtualIP.IsValid() {
			continue
		}
		asked = append(asked, domain.CanaryAsked{At: p.VirtualIP, Name: p.Name})
	}
	if len(asked) == 0 {
		// Estar solo en la sala. "Sin confirmar" es la respuesta honesta y no
		// necesita un socket: solo hay de quién protegerse cuando hay alguien.
		return canaryPlan{}, false
	}

	alarmed := s.state.HasAlert(domain.AlertCanaryLeaking)
	if alarmed && !afterApply {
		return canaryPlan{}, false
	}

	desired, err := s.desiredRuleSetLocked(ctx)
	if err != nil {
		s.deps.Log.Warn("no se pudo calcular qué puertos esquivar para el canario", "error", err)
		return canaryPlan{}, false
	}

	nonce, err := s.canaryNonce()
	if err != nil {
		s.deps.Log.Warn("no se pudo generar el número del canario", "error", err)
		return canaryPlan{}, false
	}

	return canaryPlan{
		gen:   s.state.Gen,
		at:    s.state.LocalIP,
		nonce: nonce,
		asked: asked,
		// El conjunto que se está aplicando, y no una segunda versión del
		// cálculo: sale del mismo desiredRuleSetLocked que alimenta a Apply. Se
		// usa dentro de Listen y no se guarda, así que no hay aliasing con la
		// ronda siguiente.
		avoid:   desired.Allows,
		alarmed: alarmed,
	}, true
}

// canaryNonce saca el número de s.deps.Rand y no de crypto/rand directamente,
// por lo mismo que el resto del proyecto: un test puede fijarlo y seguir
// recorriendo este mismo camino.
func (s *Session) canaryNonce() (domain.CanaryNonce, error) {
	var n domain.CanaryNonce
	src := s.deps.Rand
	if src == nil {
		src = rand.Reader
	}
	if _, err := src.Read(n[:]); err != nil {
		return domain.CanaryNonce{}, err
	}
	if n == (domain.CanaryNonce{}) {
		// Un número en ceros lo adivina cualquiera, y por UDP el número es lo
		// único que distingue el eco del canario de un paquete perdido.
		return domain.CanaryNonce{}, fmt.Errorf("el número del canario salió en ceros")
	}
	return n, nil
}

// drainStaleCanaryReports vacía la cola sin bloquear.
func (s *Session) drainStaleCanaryReports() {
	for {
		select {
		case <-s.deps.Control.CanaryReports():
		default:
			return
		}
	}
}

// collectCanary espera, y cierra con lo PRIMERO de cuatro.
//
// Los tres del diseño, más la cancelación del contexto, que es el apagado del
// servicio y no puede quedar esperando diez segundos a una ronda.
func (s *Session) collectCanary(ctx context.Context, c interface {
	Port() uint16
	Touched() <-chan struct{}
}, plan canaryPlan) domain.CanaryCheck {

	check := domain.CanaryCheck{
		MeasuredAt: s.deps.Clock.Now(),
		Port:       c.Port(),
		Asked:      plan.asked,
	}

	plazo := time.After(CanaryRoundDeadline)
	for {
		select {
		case <-c.Touched():
			// El hecho propio. No hace falta esperar a nadie más: el paquete
			// cruzó, y eso ya es la conclusión.
			return check

		case r := <-s.deps.Control.CanaryReports():
			// Record es la puerta: descarta el puerto ajeno, al que no se le
			// preguntó, y al que ya contestó. Ese último es lo que impide que un
			// miembro cierre la ronda solo mandando tantos informes como
			// miembros haya, y con ello esconda una fuga real.
			if !check.Record(r) {
				s.deps.Log.Info("informe del canario descartado", "de", r.From, "puerto", r.Port)
				continue
			}
			if check.AllAnswered() {
				return check
			}

		case <-plazo:
			return check

		case <-ctx.Done():
			return check
		}
	}
}

// foldCanaryLocked guarda la conclusión y decide qué hacer con la alarma.
//
// Tiene TRES ramas y ningún "si no", y esa forma es de seguridad. Ver abajo.
//
// Asume el candado tomado.
func (s *Session) foldCanaryLocked(ctx context.Context, plan canaryPlan, check domain.CanaryCheck) {
	if plan.gen != s.state.Gen {
		// La sala que se estaba midiendo ya no existe. Escribir esto acá sería
		// presentar una medición contra otros miembros y otras reglas como el
		// estado de la sala actual, con la hora de ahora.
		s.deps.Log.Info("se descarta una ronda del canario de una sala que ya se cerró",
			"medida-en", plan.gen, "sala-actual", s.state.Gen)
		return
	}

	// Los informes se apilaron según fueron llegando, y ese orden lo decide la
	// red. Dos rondas con la misma entrada tienen que pintar la misma pantalla.
	ordenarPorDirección(check.Answers)
	s.state.Canary = check

	switch {
	case check.Verdict() == domain.CanaryLeaking:
		// Se quita siempre antes de decidir, así cinco rondas con fuga no apilan
		// cinco filas idénticas en la pantalla.
		s.state.DropAlerts(domain.AlertCanaryLeaking)

		if s.canaryRepairs < CanaryRepairLimit {
			s.canaryRepairs++
			s.deps.Log.Warn("el canario fue tocado: se repone la protección sin avisar",
				"puerto", check.Port, "intento", s.canaryRepairs)
			if err := s.applyPolicy(ctx); err != nil {
				s.deps.Log.Error("no se pudo reponer la protección", "error", err)
			}
			break
		}

		s.deps.Log.Error("la protección no aguantó la reposición",
			"puerto", check.Port, "preguntados", len(check.Asked))
		s.state.Alerts = append(s.state.Alerts, domain.Alert{
			Kind: domain.AlertCanaryLeaking,
			Detail: fmt.Sprintf(
				"alguien de la sala alcanzó el puerto %d de esta máquina, y se repuso la protección sin que aguantara",
				check.Port),
		})

	case check.ClearsAlarm():
		s.canaryRepairs = 0
		s.state.DropAlerts(domain.AlertCanaryLeaking)
	}
	// [domain.CanaryUnconfirmed], [domain.CanaryMismatch] y el cero NO TOCAN
	// NADA, y es deliberado. Si "sin confirmar" apagara la alarma, un miembro
	// borraría una fuga YA DEMOSTRADA con solo quedarse callado, y callarse
	// pasaría de esconder información a borrar una medición. Ver
	// [domain.CanaryCheck.ClearsAlarm].

	s.snapshot()
}

// ReapplyProtection es la acción del usuario, y es IDEMPOTENTE.
//
// Es un applyPolicy y nada más. El firewall calcula la diferencia contra las
// reglas VIVAS, así que reaplicar con nada roto no lo toca, y pulsarlo dos veces
// cuesta lo mismo que pulsarlo una.
//
// Vale para los dos roles. Un invitado también tiene protección, su propio
// deny-all más la compuerta, y reponerla es el mismo acto.
//
// NO apaga la alarma. Apagarla acá la escondería sin haber comprobado nada: lo
// único que la apaga es una ronda que MIDIÓ y volvió limpia, y aplicar programa
// justo esa ronda.
func (s *Session) ReapplyProtection(ctx context.Context) (domain.RoomState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.state.Conn.InRoom() {
		return domain.RoomState{}, ErrNoRoom
	}
	if err := s.applyPolicy(ctx); err != nil {
		return domain.RoomState{}, err
	}
	s.deps.Log.Info("el usuario repuso la protección a mano")
	return s.snapshot(), nil
}

// OnCanaryRequest es el invitado atendiendo el pedido del host: marca y
// contesta.
//
// # El candado se suelta antes de marcar
//
// Son hasta seis segundos contra la red, los dos sondeos de tres, y sostener el
// candado dejaría a cualquier Status de la UI esperándolos.
//
// # Lo que se comprueba, y por qué también acá
//
// Que `req.Host` sea el host de ESTA sala. En el cable ya es imposible pedir por
// una tercera máquina, porque el mensaje no tiene campo de dirección y hay un
// test que falla si alguien se lo agrega. Esto es lo mismo dicho en la capa que
// seguiría en pie si el transporte cambiara, y se comprueba sin sockets.
//
// Sin esa regla, el canal de la sala sería un escáner de puertos por encargo:
// cualquiera pediría que los demás marcaran a una máquina de fuera, y el tráfico
// saldría de las casas de otros.
//
// Que no se pueda atender NO es un error del usuario: se anota y se calla.
func (s *Session) OnCanaryRequest(ctx context.Context, req domain.CanaryRequest) error {
	if err := req.Valid(); err != nil {
		s.deps.Log.Warn("pedido de canario inválido", "error", err)
		return nil
	}

	s.mu.Lock()
	host, ok := s.canaryAskerLocked(req)
	s.mu.Unlock()
	if !ok {
		return nil
	}

	tcp, udp := s.deps.Prober.ProbeCanary(ctx, netip.AddrPortFrom(host, req.Port), req.Nonce)
	s.deps.Log.Info("canario del host sondeado", "a", host, "puerto", req.Port, "tcp", tcp, "udp", udp)

	// El informe no lleva remitente: lo rellena el adaptador desde la conexión,
	// que es lo que impide informar en nombre de otro.
	return s.deps.Control.SendCanaryReport(ctx, domain.CanaryReport{
		Port: req.Port,
		TCP:  tcp,
		UDP:  udp,
	})
}

// canaryAskerLocked devuelve la dirección a marcar, y solo si es el host.
//
// Asume el candado tomado.
func (s *Session) canaryAskerLocked(req domain.CanaryRequest) (netip.Addr, bool) {
	if !s.state.Conn.InRoom() {
		return netip.Addr{}, false
	}
	if s.state.IsHost() {
		// Marcarse a uno mismo no atraviesa ningún firewall: el tráfico a la
		// propia dirección no sale por el adaptador. Contestaría que todo está
		// abierto en una máquina blindada.
		s.deps.Log.Warn("llegó un pedido de canario siendo el host, y se ignora")
		return netip.Addr{}, false
	}

	host, ok := s.state.HostPeer()
	if !ok || !host.VirtualIP.IsValid() {
		return netip.Addr{}, false
	}
	if req.Host != host.VirtualIP {
		s.deps.Log.Warn("se pidió marcar a una máquina que no es el host de esta sala, y se ignora",
			"pedida", req.Host, "host", host.VirtualIP)
		return netip.Addr{}, false
	}
	return host.VirtualIP, true
}

// ordenarPorDirección deja los informes en orden estable.
//
// El recorrido de un mapa en Go no lo es, y dos rondas con la misma entrada
// tienen que pintar la misma pantalla.
func ordenarPorDirección(as []domain.CanaryAnswer) {
	sort.Slice(as, func(i, j int) bool { return as[i].From.Less(as[j].From) })
}
