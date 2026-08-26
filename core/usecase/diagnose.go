package usecase

import (
	"context"
	"fmt"
	"net/netip"
	"strings"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// TamperRepairLimit es cuántas veces se reponen las reglas propias antes de
// dejar de intentarlo.
//
// Tres distingue los dos casos que producen el mismo síntoma: el toque puntual
// de alguien mirando la consola del firewall, que se arregla con una
// reaplicación, y algo que las está quitando en bucle, normalmente un antivirus.
// Contra lo segundo, insistir es pelearse a golpe de COM y eso no lo gana nadie:
// lo que corresponde es decirlo.
const TamperRepairLimit = 3

// RefreshAlerts corre el módulo de exposición de la decisión 19 y publica el
// resultado dentro del estado.
//
// Lo llama el supervisor cada tanto. No hay canal aparte ni evento especial:
// el módulo publica su último resultado y [Session.Status] lo arrastra, así que
// una alerta nunca puede bloquear ni retrasar una respuesta.
//
// **Ninguna comprobación devuelve error fatal.** Cada una responde una pregunta
// que Kanpachi no controla y que anula su promesa si nadie la mira, y que la
// consulta falle no puede impedir entrar a una sala ni jugar.
//
// **Que falle sí se dice.** Va al log y además levanta [domain.AlertAuditFailed],
// porque una comprobación que no contesta y un resultado limpio se ven idénticos
// desde la pantalla: las dos pintan verde. La excepción es la consulta al router,
// que falla en condiciones normales.
func (s *Session) RefreshAlerts(ctx context.Context) domain.RoomState {
	var found []domain.Alert

	// Lo que no se pudo mirar. Se junta en UNA alerta y no en una por
	// comprobación, porque es un solo hecho: el módulo dejó de vigilar. Dos
	// filas en la pantalla por el mismo hecho invitan a leer una y descartar la
	// otra.
	var sinRespuesta []string

	if estados, err := s.deps.Audit.FirewallEnabled(ctx); err != nil {
		s.deps.Log.Warn("no se pudo comprobar si el firewall está encendido", "error", err)
		sinRespuesta = append(sinRespuesta, "si el Firewall de Windows está encendido")
	} else {
		for _, e := range estados {
			if !e.Enabled {
				// Con el firewall apagado, las reglas de Kanpachi no
				// restringen nada: la red sigue cifrada y la cuarentena
				// desaparece, que es media promesa del producto.
				found = append(found, domain.Alert{
					Kind:   domain.AlertFirewallOff,
					Detail: fmt.Sprintf("el Firewall de Windows está apagado en el perfil %s", e.Profile),
				})
			}
		}
	}

	// El registro se sondea en el mismo barrido y no en el latido, por lo que
	// dice la cabecera de este fichero: un latido para el tiempo, un barrido
	// para el mundo. Esto sale a la red.
	//
	// Su fallo NO entra en `sinRespuesta`: aquello es la auditoría de exposición
	// dejando de vigilar, o sea seguridad, y esto es disponibilidad. Juntarlos
	// diría que la máquina quedó sin vigilar cuando lo que pasa es que no se
	// pueden abrir salas nuevas. Va a su propio campo del estado, que es
	// transitorio y se apaga solo. Ver [domain.RoomState.SeedDown].
	// Se sondea el registro de ESTA máquina, que es el que decide si se puede
	// abrir una sala. No tener ninguno configurado todavía **no es que esté
	// caído**: son dos cosas distintas y solo una es un problema de red. Sin
	// registro no se sondea nada y el aviso no se enciende; quien intente crear
	// se topa con [port.ErrNoOwnSeed] y con la pantalla que lo configura.
	caído := false
	if dir, err := s.deps.Directories.Own(); err == nil {
		if err := dir.Reachable(ctx); err != nil {
			s.deps.Log.Warn("el registro no contesta, así que no se pueden abrir ni entrar salas",
				"seed", dir.Seed(), "error", err)
			caído = true
		}
	}

	// Las reglas propias se MIDEN acá, suelto, igual que las otras dos
	// consultas. El veredicto se calcula más abajo con el candado tomado,
	// porque comparar contra el estado deseado exige leer el estado de la
	// sesión, y reponerlas es aplicar política.
	medido, sePudoRevisar := domain.Enforcement{}, true
	if e, err := s.deps.Audit.Enforcement(ctx); err != nil {
		s.deps.Log.Warn("no se pudo medir lo que el firewall tiene puesto", "error", err)
		sePudoRevisar = false
		sinRespuesta = append(sinRespuesta, "si las reglas de Kanpachi siguen puestas")
	} else {
		medido = e
	}

	// The base quarantine is measured in the same sweep and STORED, not judged
	// into an alert here: whether a machine without it should say so, and how
	// loudly, is each face's decision, and the faces read the state. Measuring
	// from the system, never from a memory of what was applied, is what lets a
	// rule somebody deleted or edited show up at all.
	cuarentena := domain.QuarantineState{}
	if q, err := s.deps.Audit.QuarantineState(ctx); err != nil {
		s.deps.Log.Warn("no se pudo medir la cuarentena de base", "error", err)
		sinRespuesta = append(sinRespuesta, "si la cuarentena de base sigue puesta")
	} else {
		cuarentena = q
	}

	// The foreign firewall that blocks inbound is asked about in the same
	// sweep: a ufw enabled MID-room turns a working room into one nobody can
	// enter, with every other check still green. The alert below is the only
	// thing on screen that says why. A read failure joins `sinRespuesta`, same
	// as the other audit queries: it is the module going blind, not a finding.
	var bloqueos []domain.FirewallBlock
	if b, err := s.deps.Firewall.InboundBlocked(ctx); err != nil {
		s.deps.Log.Warn("no se pudo mirar si un firewall ajeno bloquea la entrada", "error", err)
		sinRespuesta = append(sinRespuesta, "si un firewall ajeno bloquea la entrada de la sala")
	} else {
		bloqueos = b
	}

	// La consulta al router es la única de las tres cuyo fallo se traga entero:
	// ni alerta, ni línea de log.
	//
	// Muchísimos routers no contestan al IGD, así que falla en condiciones
	// normales. Una alerta encendida en la mayoría de las máquinas del mundo no
	// informa de nada, y una línea por minuto durante toda la vida del daemon
	// diciendo que el mundo sigue igual es peor: entierra las que sí importan.
	// Las otras dos comprobaciones son locales y solo fallan si algo está roto
	// de verdad, por eso ellas sí cuentan como auditoría caída.
	//
	// Lo que se anota es el HALLAZGO, que es la respuesta rara y la que alguien
	// quiere leer.
	if mapeos, err := s.deps.Audit.RouterMappings(ctx); err == nil && len(mapeos) > 0 {
		s.deps.Log.Info("el router publica puertos que no puso Kanpachi", "cantidad", len(mapeos))
		for _, m := range mapeos {
			// Kanpachi no lo puso y no lo va a quitar: el router del usuario no
			// se toca nunca. Lo único que hace es decirlo.
			found = append(found, domain.Alert{
				Kind: domain.AlertRouterMapping,
				Detail: fmt.Sprintf("el router publica el puerto %d hacia %s, y no lo puso Kanpachi",
					m.ExternalPort, m.InternalIP),
			})
		}
	}

	// El módulo que existe para avisar que la promesa se rompió tiene que poder
	// avisar que él mismo dejó de mirar. Sin esto, con las consultas fallando la
	// pantalla queda en verde, que es la peor de las dos mentiras posibles: la
	// otra sería inventar un hallazgo falso, y esa al menos se investiga.
	if len(sinRespuesta) > 0 {
		found = append(found, domain.Alert{
			Kind:   domain.AlertAuditFailed,
			Detail: "no se pudo comprobar " + strings.Join(sinRespuesta, " ni "),
		})
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Antes del corte por vencimientos, y a propósito. Lo demás que se calcula
	// acá describe una SALA, así que sin sala sobra; esto describe la MÁQUINA y
	// vale igual sin sala, que es justamente cuando más sirve: es el estado que
	// dice si se va a poder abrir una.
	s.state.SeedDown = caído

	// Machine-level like SeedDown, and stored for the same reason: it is worth
	// the most precisely when there is no room. On a failed read it stores the
	// zero, whose verdict is Unknown — leaving the previous measurement up
	// would have the screen vouching for something nobody just measured.
	s.state.Quarantine = cuarentena

	// El barrido es una entrada que OBSERVA, así que también hace vencer los
	// plazos. Si salió de la sala, los hallazgos recién calculados describen
	// una sala que ya no existe y clearRoom ya dejó las alertas en nil.
	if s.enforceDeadlinesLocked(ctx) {
		return s.snapshot()
	}

	// The foreign-firewall block becomes an alert only WITH a room open, same
	// argument as the gate below: at rest it would train people to ignore the
	// screen, and the moment it matters is when members should be arriving and
	// are not. The automatic reopen paths rely on this alert being here: they
	// cannot ask for consent, so this is what says why the room stays empty.
	if s.state.Conn.InRoom() {
		for _, b := range bloqueos {
			found = append(found, domain.Alert{
				Kind: domain.AlertForeignRule,
				Detail: fmt.Sprintf("%s deniega la entrada en %s, así que nadie puede llegar a la sala. Para abrirlo: %s",
					b.Manager, strings.Join(b.Adapters, " y "), strings.Join(b.Fix, " && ")),
			})
		}

		// La cuarentena sin poner entra por la misma puerta y con el mismo
		// argumento: solo con sala abierta, porque en reposo enseñaría a
		// ignorar la pantalla y el interruptor ya cuenta el estado. El aviso
		// es el ESTADO y no un regaño: sale igual si el usuario dijo que no,
		// porque decir que no apaga los bloqueos y no el termómetro.
		switch s.state.Quarantine.Verdict {
		case domain.QuarantineAbsent:
			found = append(found, domain.Alert{
				Kind: domain.AlertQuarantineOff,
				Detail: "esta PC contesta a compartir archivos y a Escritorio remoto en todas " +
					"sus redes: la cuarentena de base no está puesta",
			})
		case domain.QuarantinePartial:
			q := s.state.Quarantine
			found = append(found, domain.Alert{
				Kind: domain.AlertQuarantineOff,
				Detail: fmt.Sprintf("a la cuarentena de base le falta parte: de %d reglas hay "+
					"%d ausentes, %d deshabilitadas y %d editadas",
					q.Total, q.Missing, q.Disabled, q.Drifted),
			})
		}
	}

	// El veredicto se calcula ACÁ y no en el adaptador: el adaptador mide y el
	// dominio juzga. Y se compara contra el MISMO cálculo que se aplica, que es
	// para lo que existe desiredRuleSetLocked.
	//
	// La compuerta solo se exige con sala abierta, porque sin sala no hay
	// adaptador virtual y una alerta encendida en reposo enseña a ignorar la
	// pantalla.
	alteradas := false
	if sePudoRevisar {
		deseado, err := s.desiredRuleSetLocked()
		if err != nil {
			// Sin poder calcular lo deseado no hay veredicto posible, y
			// inventar uno sería peor que callarse. Es la misma doctrina que
			// no poder medir.
			s.deps.Log.Warn("no se pudo calcular el conjunto deseado para comparar", "error", err)
			sePudoRevisar = false
		} else {
			d := medido.Diff(deseado, s.state.Conn.InRoom())
			alteradas = !d.Intact()
			if d.GateUnchecked {
				found = append(found, domain.Alert{
					Kind:   domain.AlertAuditFailed,
					Detail: "no se pudo comprobar si la compuerta del adaptador está puesta",
				})
			}
		}
	}

	if alteradas || !sePudoRevisar {
		found = append(found, s.repairOwnRulesLocked(ctx, alteradas, sePudoRevisar)...)
	}

	found = append(found, s.roomHealthAlertsLocked()...)

	// Las alertas PEGAJOSAS sobreviven al refresco. Describen algo que pasó y
	// que nadie va a volver a medir, a diferencia del resto, que es el
	// resultado de una comprobación puntual y se recalcula entero.
	for _, a := range s.state.Alerts {
		if a.Kind.Sticky() {
			found = append(found, a)
		}
	}
	s.state.Alerts = found
	return s.snapshot()
}

// repairOwnRulesLocked repone las reglas propias y devuelve lo que haya que
// avisar.
//
// **Reponer no es arreglarle la máquina al usuario.** Kanpachi no toca su
// firewall, su router ni sus reglas: lo único que hace acá es volver a hacer
// cierta SU PROPIA declaración, que es la que sostiene la cuarentena. Un
// hallazgo que se denuncia y no se repara deja la promesa rota mientras el
// usuario lee el aviso.
//
// Funciona porque [port.FirewallPort.Apply] calcula la diferencia contra las
// reglas vivas del grupo Kanpachi, no contra un recuerdo en memoria. Con un
// recuerdo, reaplicar el mismo conjunto sería un no-op y esto no serviría de
// nada.
//
// Asume el candado tomado.
func (s *Session) repairOwnRulesLocked(ctx context.Context, alteradas, sePudoRevisar bool) []domain.Alert {
	const detalle = "las reglas de Kanpachi no están como se aplicaron"

	switch {
	case !sePudoRevisar:
		// Sin poder mirarlas no hay nada que reponer, y reaplicar a ciegas en
		// cada barrido sería tocar el firewall cada minuto por una consulta que
		// falla. Se calla: la comprobación fallida ya quedó en el log.
		return nil

	case !s.state.Conn.InRoom():
		// Sin sala el conjunto deseado es el vacío, así que reaplicar sería
		// pedirle al firewall que borre lo que la purga del arranque ya borró.
		return []domain.Alert{{Kind: domain.AlertRulesTampered, Detail: detalle}}

	case s.tamperRepairs >= TamperRepairLimit:
		return []domain.Alert{{
			Kind:   domain.AlertRulesTampered,
			Detail: fmt.Sprintf("%s, y se volvieron a alterar tras reponerlas %d veces", detalle, TamperRepairLimit),
		}}
	}

	s.tamperRepairs++
	if err := s.applyPolicy(ctx); err != nil {
		return []domain.Alert{{
			Kind:   domain.AlertRulesTampered,
			Detail: fmt.Sprintf("%s y no se pudieron reponer: %v", detalle, err),
		}}
	}
	// No se vuelve a comprobar acá. El barrido siguiente lo verifica, y una
	// segunda enumeración del firewall por barrido cuesta más que el minuto de
	// latencia que ahorra.
	s.deps.Log.Warn("las reglas propias estaban alteradas y se repusieron", "intento", s.tamperRepairs)
	return nil
}

// Diagnose consulta al motor y refresca el diagnóstico de red.
//
// Es lo que convierte "no conecta" en "tu router hace NAT simétrico, vas por
// relay". Los campos que NO vienen del motor se conservan: el MTU lo sondea
// netcfg y la subred la eligió el plan de direcciones, y pisarlos con los ceros
// de una respuesta del motor dejaría el diagnóstico peor que antes de pedirlo.
func (s *Session) Diagnose(ctx context.Context) (domain.NetCheck, error) {
	check, err := s.deps.Engine.Diagnostics(ctx)
	if err != nil {
		return domain.NetCheck{}, fmt.Errorf("consultando el diagnóstico al motor: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	check.MTU = s.state.Net.MTU
	check.Subnet = s.state.Net.Subnet
	check.SubnetReason = s.state.Net.SubnetReason
	s.state.Net = check
	s.snapshot()
	return check, nil
}

// ObserveGame saca la foto de sockets del creador de perfiles.
//
// Es una consulta PUNTUAL, disparada por un botón del usuario que ya abrió el
// juego. No hay espera de fondo, no queda nada corriendo al cerrar el
// asistente, y fuera de este asistente Kanpachi jamás consulta procesos. Es la
// única función del programa que mira un proceso, y por eso está sola en su
// propio método en vez de escondida dentro de otro flujo.
//
// `tree` son los PIDs del árbol que arranca en el ejecutable elegido, porque
// muchos juegos arrancan desde un launcher y el servidor es un proceso hijo.
// Quién arma ese árbol es el adaptador, que es el único que puede recorrerlo.
func (s *Session) ObserveGame(ctx context.Context, root domain.ProcessRef, tree map[int]bool, keepSteam bool) ([]domain.PortRange, error) {
	listeners, err := s.deps.Inspector.Snapshot(ctx, root)
	if err != nil {
		return nil, fmt.Errorf("mirando los puertos de %s: %w", root.Executable, err)
	}
	if len(tree) == 0 {
		// Sin árbol la foto trae todos los sockets de la máquina. Se toma la
		// raíz como árbol de un solo nodo en vez de devolver todo: es lo que
		// el usuario pidió mirar.
		tree = map[int]bool{root.PID: true}
	}
	rangos := domain.ObservedRanges(listeners, tree, keepSteam)
	s.deps.Log.Info("foto de puertos", "ejecutable", root.Executable, "rangos", len(rangos))
	return rangos, nil
}

// roomHealthAlertsLocked mira la sala en vez de la máquina.
//
// Las demás comprobaciones de este fichero preguntan si esta PC está expuesta
// de más. Estas dos preguntan si la SALA sirve, que es lo que nadie preguntaba
// el 2026-08-25, cuando un host pasó treinta y tres horas sin poder recibir a
// nadie mientras cada señal hacia fuera seguía en verde.
//
// Solo del host, que es quien tiene libro y quien tiene oyente.
//
// Asume el candado tomado.
func (s *Session) roomHealthAlertsLocked() []domain.Alert {
	if !s.state.IsHost() || !s.state.Conn.InRoom() {
		return nil
	}
	ahora := s.deps.Clock.Now()
	var found []domain.Alert

	// # Miembros presentes y ningún canal
	//
	// Se cuentan los que ESTÁN y ya pasaron su ventana de ingreso. Sin lo
	// primero, una sala con todo el mundo AFK gritaría cada noche; sin lo
	// segundo, gritaría en cada ingreso legítimo, que es justo el momento en
	// que todavía no hay canal.
	presentes := 0
	for _, p := range s.state.Peers {
		if p.Self || p.Away || !p.VirtualIP.IsValid() {
			continue
		}
		m, hay := s.members[p.VirtualIP]
		if !hay || m.Cred == nil || arrivalGraceOpen(*m.Cred, ahora) {
			continue
		}
		presentes++
	}
	if presentes > 0 && len(s.deps.Control.ConnectedMembers()) == 0 {
		found = append(found, domain.Alert{
			Kind: domain.AlertNoMemberChannels,
			Detail: fmt.Sprintf("la sala tiene %d miembro(s) conectado(s) y ninguno abrió su canal: "+
				"revisa la compuerta de esta máquina", presentes),
		})
	}

	// # El /24 casi lleno
	//
	// Antes de agotarse, porque al agotarse el aviso le llegaría a quien no
	// puede arreglarlo: el error lo ve el invitado que se quedó fuera.
	if s.state.Subnet.IsValid() {
		libres := freeAddressCount(s.state.Subnet, s.takenAddressesLocked())
		if libres <= roomAlmostFullFree {
			found = append(found, domain.Alert{
				Kind: domain.AlertRoomAlmostFull,
				Detail: fmt.Sprintf("a la sala le quedan %d direcciones libres de %s: "+
					"cuando se acaben, quien entre se queda fuera sin que esta pantalla lo diga",
					libres, s.state.Subnet),
			})
		}
	}
	return found
}

// roomAlmostFullFree es cuántas direcciones libres quedan cuando se avisa.
//
// Veinte sobre las 253 de un /24 es poco más del siete por ciento, y son
// suficientes para que el aviso llegue con margen: al ritmo medido el
// 2026-08-16, setenta y tres direcciones en cuatro horas, veinte dan poco más
// de una hora para mirar qué las está gastando. Una sala real son cuatro o
// cinco personas, así que en uso normal esto no se enciende nunca.
const roomAlmostFullFree = 20

// freeAddressCount cuenta los huecos que quedan en la subred de la sala.
func freeAddressCount(subnet netip.Prefix, taken map[netip.Addr]bool) int {
	n := 0
	for a := subnet.Addr(); subnet.Contains(a); a = a.Next() {
		if !taken[a] {
			n++
		}
	}
	return n
}
