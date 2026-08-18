package domain

import (
	"encoding/json"
	"fmt"
	"time"
)

// La cuarentena de base: los bloqueos que sobreviven al servicio detenido.
//
// # Qué protege, en una frase
//
// Que un juego, un instalador o un diálogo de Windows deje una regla permisiva
// sobre SMB o Escritorio remoto y la máquina quede alcanzable por ahí. Los
// bloqueos explícitos del Firewall de Windows ganan sobre cualquier permiso en
// conflicto, sin desempate por especificidad, así que un bloqueo puesto acá le
// gana a esa regla ajena esté quien esté escuchando.
//
// # Por qué no es un deny-all
//
// Porque no hace falta y porque no podría. La entrada ya viene bloqueada por
// defecto en los tres perfiles de Windows, o sea que **la ausencia de reglas de
// permiso YA ES el deny-all**, y además es uno que ninguna regla de Kanpachi
// puede tapar. Un bloqueo total escrito acá taparía las reglas del juego activo
// que crea el propio daemon, por la misma mecánica de arriba. Ver decisión 4.
//
// El bloqueo de todo SÍ existe, y vive en la OTRA capa: la compuerta de WFP
// sobre el adaptador virtual, donde un bloqueo sí admite excepciones por encima.
// Confundir las dos capas lleva a "corregir" cualquiera de las dos en la
// dirección equivocada.
//
// # Por qué lo escribe el daemon
//
// Antes lo iba a poner un instalador. No hay instalador, y una cuarentena que
// depende de un programa que no existe es una promesa apagada. El daemon la
// escribe y la REPONE en cada arranque, con un método que solo sabe agregar.
//
// Lo que cambia es quién la pone, y lo que NO cambia es lo que la hace valiosa:
// nadie la borra al apagarse, así que sigue puesta con el servicio detenido,
// deshabilitado o a medio desinstalar. Por eso [FirewallGroupBase] sigue siendo
// un grupo aparte de [FirewallGroup]: la purga del arranque se lleva el de la
// sala y no toca este.

// QuarantineRule es una regla de la cuarentena de base.
//
// # No hay campo de acción, y la ausencia ES la garantía
//
// Cada regla de este tipo es un BLOQUEO por construcción del tipo. No existe
// forma de escribir un permiso con él, ni por descuido ni a propósito.
//
// Por eso es un tipo aparte y no un campo `Action` en [FirewallRule]: aquel es
// el que produce [BuildRuleSet] a partir de un perfil del catálogo, o sea de un
// archivo que el usuario puede importar. Con un campo de acción ahí, **un perfil
// de juego podría emitir bloqueos**. Hoy la invariante "un perfil solo ABRE" la
// sostiene el tipo; con ese campo la sostendría la disciplina de quien escriba
// el código.
//
// # Qué NO lleva, y cada ausencia tiene su motivo
//
//   - **Alcance por adaptador.** Un alcance que deja de casar convierte un
//     permiso en un cierre y **un bloqueo en nada**. La cuarentena vale para
//     toda la máquina o no vale. Lo mismo dice [FirewallRule.Local] al revés.
//   - **Alcance remoto.** Estos bloqueos son contra cualquiera, que es lo único
//     que tiene sentido para "mi PC no ofrece SMB".
//   - **Perfil de red.** Van en los tres, porque el producto no depende de que
//     Windows clasifique bien la red.
type QuarantineRule struct {
	// Name es determinista a partir del resto de campos, igual que en
	// [FirewallRule]: de eso depende que reponer la cuarentena pueda comparar
	// contra lo vivo sin guardar identificadores en ningún sitio.
	Name  string
	Proto Proto
	From  uint16
	To    uint16
	// In es la dirección. Cada puerto se bloquea en las DOS, y son dos reglas
	// porque una regla de Windows tiene una dirección y solo una.
	//
	// **El puerto es siempre el LOCAL, en las dos direcciones**, y de eso
	// depende que la cuarentena no rompa la máquina. Entrante con puerto local
	// 445 es "nadie llega a MI servicio de SMB", que es la protección. Saliente
	// con puerto local 445 es "mi servicio de SMB tampoco habla hacia afuera",
	// que cierra el mismo servicio por el otro lado.
	//
	// Lo que NO hace, y sería un daño de verdad si lo hiciera: **no impide que
	// esta máquina sea CLIENTE**. Montar un disco de red, entrar por Escritorio
	// remoto a otra PC o usar git por SSH salen de un puerto local efímero hacia
	// el 445, el 3389 o el 22 del OTRO, así que ninguna de estas reglas los
	// toca. Bloquear por puerto remoto sí los rompería, y para siempre, porque
	// la cuarentena sigue puesta con Kanpachi apagado.
	In bool
}

// QuarantineSystem es el conjunto CERRADO de sistemas que tienen cuarentena.
//
// Existe porque la lista de puertos no puede ser la misma en los dos, y la razón
// es concreta: en Windows el 22 es un servicio opcional que casi nadie usa, y en
// Linux es el canal por el que el operador administra el servidor. Cerrarlo ahí,
// de forma permanente y sobreviviendo al reinicio, deja a alguien fuera de su
// propia máquina sin forma de volver a entrar.
//
// Medido el 2026-08-10 en el banco: la cuarentena tal como estaba emitía
// `tcp dport 22 drop` entre sus 48 reglas.
type QuarantineSystem uint8

const (
	QuarantineWindows QuarantineSystem = iota + 1
	QuarantineLinux
)

// quarantinePortsFor son los puertos que cierra la cuarentena en cada sistema.
//
// # Por qué NO es [forbiddenPorts], que era lo que usaba antes
//
// Porque esa lista cumple DOS papeles que hasta ahora coincidían y dejaron de
// coincidir. El primero es "ningún perfil de juego puede pedir estos puertos", y
// ese vale igual en los dos sistemas: un perfil que abra el 22 es un perfil que
// abre SSH, y da lo mismo dónde corra. El segundo es "esto lo cierra la
// cuarentena para siempre", y ese sí depende del sistema.
//
// [forbiddenPorts] se queda entera para el primer papel. Esto es el segundo.
func quarantinePortsFor(sys QuarantineSystem) []uint16 {
	switch sys {
	case QuarantineLinux:
		// La misma lista menos el 22. Los otros once son servicios de Windows
		// que en Linux no corren, y se dejan igual: un puerto cerrado que nadie
		// usa no cuesta nada, y el día que alguien levante Samba en el servidor
		// del juego, la cuarentena ya estaba puesta.
		out := make([]uint16, 0, len(forbiddenPorts))
		for _, p := range forbiddenPorts {
			if p == sshPort {
				continue
			}
			out = append(out, p)
		}
		return out
	default:
		return forbiddenPorts[:]
	}
}

// sshPort es el canal de administración de una máquina Linux.
//
// Está nombrado en vez de escrito suelto porque quitarlo de la cuarentena es una
// decisión y tiene que verse como tal al leer el código.
//
// **Esto no cubre a quien movió sshd de sitio.** Un operador que lo puso en otro
// puerto de la lista se quedaría fuera igual, y por eso el adaptador de Linux
// comprueba además qué hay escuchando antes de cerrar nada. Acá no se puede
// saber: el dominio no mira el sistema.
const sshPort = 22

// BaseQuarantine devuelve la cuarentena entera de Windows.
//
// Se conserva con este nombre y esta firma porque es la que el producto de
// Windows viene usando. [BaseQuarantineFor] es la general.
func BaseQuarantine() []QuarantineRule { return BaseQuarantineFor(QuarantineWindows) }

// BaseQuarantineFor devuelve la cuarentena entera del sistema que se nombre.
//
// **El parámetro es el NOMBRE de un sistema, jamás una lista de puertos**, y esa
// diferencia es la que conserva la invariante de antes. Un llamador puede decir
// dónde corre y no puede escribir una cuarentena más floja: las dos listas viven
// acá, cerradas, y no hay ninguna tercera que se pueda pedir. Sigue sin haber
// alcance que ajustar, porque la cuarentena no tiene alcance.
//
// Sale ordenada y es la misma lista en cada llamada para el mismo sistema, así
// que reponerla es comparable contra lo vivo.
//
// # Sin permiso de ICMP echo
//
// Una versión anterior de esto prometía un permiso de ICMP echo "para el
// diagnóstico". No se escribe, y el motivo es que **ninguna función del producto
// depende de él**: el sondeo de MTU manda el ping hacia AFUERA, que la salida no
// bloquea, y la latencia de un miembro la mide el motor por su propio protocolo.
// Sería la única regla de la cuarentena que ABRE en vez de cerrar, y sin acotar,
// o sea contestando el ping en toda red a la que la máquina se conecte, para
// siempre y con Kanpachi apagado. Se paga una superficie permanente por un
// diagnóstico que nadie usa.
func BaseQuarantineFor(sys QuarantineSystem) []QuarantineRule {
	// Los dos protocolos para cada puerto, sin mirar cuál usa cada servicio de
	// verdad. Un puerto bloqueado que nadie usa no cuesta nada; uno abierto
	// porque alguien recordó mal qué protocolo era cuesta justo lo que esta
	// lista existe para impedir.
	protos := [...]Proto{ProtoTCP, ProtoUDP}
	dirs := [...]bool{true, false}

	puertos := quarantinePortsFor(sys)
	out := make([]QuarantineRule, 0, len(puertos)*len(protos)*len(dirs))
	for _, port := range puertos {
		for _, proto := range protos {
			for _, in := range dirs {
				out = append(out, QuarantineRule{
					Name:  quarantineName(proto, port, in),
					Proto: proto,
					From:  port,
					To:    port,
					In:    in,
				})
			}
		}
	}
	return out
}

// QuarantineDecision is the user's answer to "close these ports on this
// machine", and it has three states because two would lie.
//
// Undecided is NOT "no": it is what makes the question get asked exactly
// once. It is also the zero value and the meaning of the file being absent,
// so a machine that never answered cannot be told apart from one that
// answered by accident.
//
// The decision IS the operation, in both directions: recording Accepted
// writes the quarantine and recording Declined removes it. There is no
// preference stored on one side and rules standing on the other — see the
// consent use case.
type QuarantineDecision uint8

const (
	QuarantineUndecided QuarantineDecision = iota
	QuarantineAccepted
	QuarantineDeclined
)

// quarantineDecisionJSON is the on-disk schema. Closed, like every persisted
// shape in this package.
type quarantineDecisionJSON struct {
	Decision  string `json:"decision"`
	DecidedAt string `json:"decided_at"`
}

// EncodeQuarantineDecision serialises a decision somebody actually took.
//
// Undecided is not encodable ON PURPOSE: the absent file already means that,
// and two spellings of the same state contradict each other the day one is
// forgotten. The timestamp is for the faces — "you switched this off" reads
// differently with a date next to it.
func EncodeQuarantineDecision(d QuarantineDecision, when time.Time) ([]byte, error) {
	var s string
	switch d {
	case QuarantineAccepted:
		s = "yes"
	case QuarantineDeclined:
		s = "no"
	default:
		return nil, fmt.Errorf("%w: la decisión de la cuarentena sin tomar no se guarda: "+
			"el fichero ausente ya significa eso", ErrPersistedShape)
	}
	return json.MarshalIndent(quarantineDecisionJSON{
		Decision:  s,
		DecidedAt: when.UTC().Format(time.RFC3339),
	}, "", "  ")
}

// DecodeQuarantineDecision is the only decoder of the decision file.
//
// Strict like the rest: unknown fields reject the file. An unreadable or
// malformed file decodes to an ERROR and never to a decision, and the caller
// treats that as undecided-with-a-log-line: inventing a "no" would silently
// strip the machine of what the user asked for, and inventing a "yes" would
// close ports nobody agreed to close.
func DecodeQuarantineDecision(raw []byte) (QuarantineDecision, time.Time, error) {
	j, err := decodeStrict[quarantineDecisionJSON](raw)
	if err != nil {
		return QuarantineUndecided, time.Time{}, err
	}
	when, err := time.Parse(time.RFC3339, j.DecidedAt)
	if err != nil {
		return QuarantineUndecided, time.Time{}, fmt.Errorf("%w: la fecha de la decisión: %v",
			ErrPersistedShape, err)
	}
	switch j.Decision {
	case "yes":
		return QuarantineAccepted, when, nil
	case "no":
		return QuarantineDeclined, when, nil
	default:
		return QuarantineUndecided, time.Time{}, fmt.Errorf("%w: decisión desconocida %q",
			ErrPersistedShape, j.Decision)
	}
}

// QuarantinePorts is the list of ports the quarantine closes on the named
// system, one entry per port, whether the quarantine is in place or not.
//
// It exists for the faces: an alert or a consent prompt has to say WHAT gets
// closed, and without this each face would carry its own copy of the list,
// drifting. It returns a fresh slice because on Windows the internal list is
// the domain's own backing array, and a caller sorting its copy must not be
// able to reorder what every other caller reads.
func QuarantinePorts(sys QuarantineSystem) []uint16 {
	return append([]uint16(nil), quarantinePortsFor(sys)...)
}

// QuarantineVerdict is the measured answer to "is the base quarantine in
// place", read off the system and never recalled from memory.
//
// The zero value is Unknown ON PURPOSE: an adapter that fails to read returns
// the zero state, and the screen says "could not check" instead of painting
// either green or red over a measurement that never happened. Same doctrine as
// [Enforcement]'s zero.
type QuarantineVerdict uint8

const (
	// QuarantineUnknown is that the system could not be read.
	QuarantineUnknown QuarantineVerdict = iota
	// QuarantineAbsent is that not one rule of the quarantine is in place.
	QuarantineAbsent
	// QuarantinePartial is that some of it is there and some is not: rules
	// missing, present but disabled, or edited until they stopped saying what
	// was written. The last one is the hole that matters most, because the
	// rule still counts as present to anything that counts by name, and it
	// blocks nothing.
	QuarantinePartial
	// QuarantineApplied is that every rule is present, enabled and saying what
	// was written.
	QuarantineApplied
)

// QuarantineState is the base quarantine as it stands on this machine.
//
// It travels in the room state so every face reads the same measurement, and
// it is machine-level like [RoomState.SeedDown]: it describes the machine, not
// the room, so it survives leaving one.
type QuarantineState struct {
	Verdict QuarantineVerdict

	// Ports is what the quarantine closes on this system, so a face can say
	// WHAT without carrying its own copy of the list.
	Ports []uint16

	// Total is how many rules the full quarantine has on this system, and the
	// three counters below say why the verdict is not Applied. Disabled and
	// Drifted can only happen on Windows: an nftables rule cannot be switched
	// off in place, and editing one is deleting and re-adding it, so on Linux
	// every failure shows up as Missing.
	Total    int
	Missing  int
	Disabled int
	Drifted  int
}

// MeasuredQuarantine turns an adapter's tally into the verdict.
//
// The adapter COUNTS and this judges, which is the same split as
// [Enforcement.Diff] and for the same reason: what counts as "applied" is
// policy, and policy has to be testable without a firewall in the room.
func MeasuredQuarantine(sys QuarantineSystem, missing, disabled, drifted int) QuarantineState {
	total := len(BaseQuarantineFor(sys))
	st := QuarantineState{
		Ports:    QuarantinePorts(sys),
		Total:    total,
		Missing:  missing,
		Disabled: disabled,
		Drifted:  drifted,
	}
	switch {
	case missing >= total:
		st.Verdict = QuarantineAbsent
	case missing == 0 && disabled == 0 && drifted == 0:
		st.Verdict = QuarantineApplied
	default:
		st.Verdict = QuarantinePartial
	}
	return st
}

// quarantineName arma el nombre que va a ver el usuario en la consola del
// Firewall de Windows.
//
// En castellano y con el puerto dentro porque ahí es donde se lee: alguien
// mirando por qué su SMB no contesta tiene que poder encontrar la causa sin
// abrir el código. Es la misma razón por la que la lista de arriba no se
// agrupa en rangos, que dejaría "137-139" donde el usuario busca "139".
func quarantineName(p Proto, port uint16, in bool) string {
	sentido := "saliente"
	if in {
		sentido = "entrante"
	}
	proto := "TCP"
	if p == ProtoUDP {
		proto = "UDP"
	}
	return fmt.Sprintf("%s: bloqueo %s %s %d", FirewallGroupBase, sentido, proto, port)
}
