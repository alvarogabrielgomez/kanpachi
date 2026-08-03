package domain

import "sort"

// La contención de Kanpachi vive en DOS capas que fallan por motivos distintos,
// y este archivo es donde se juntan para poder decir la verdad sobre las dos.
//
// # Por qué dos capas
//
// Las reglas del Firewall de Windows son las que ABREN, y son visibles: se
// enumeran sin ser administrador, salen en la consola del sistema, y el usuario
// conserva el poder de veto porque sus bloqueos ganan sobre los nuestros.
//
// Lo que NO pueden expresar es "denegar todo en el adaptador salvo esto".
// Microsoft lo dice textual: los bloqueos explícitos ganan sobre cualquier
// permiso en conflicto, y no hay orden asignable por el administrador. O sea que
// un bloqueo total taparía también los permisos del propio Kanpachi, y por eso
// la cuarentena de base tuvo que degradarse a una lista de puertos prohibidos.
//
// La segunda capa es un filtro de paquetes propio, acotado al adaptador virtual,
// y ahí un bloqueo SÍ admite excepciones por encima. Eso convierte la lista de
// permitidos de ADITIVA en COMPLETA, que es lo único que cierra el caso de una
// regla ajena de escritorio remoto. Ver [ClassRemoteControl].
//
// # Por qué esto vive en el dominio
//
// El adaptador MIDE y el dominio JUZGA. Un adaptador que devolviera "está todo
// bien" convertiría la comprobación en una opinión suya, y la promesa del
// producto se apoya en que la pantalla diga lo que el sistema tiene puesto y no
// lo que Kanpachi cree haber puesto. La misma razón por la que `Apply` calcula
// la diferencia contra las reglas vivas y jamás contra un recuerdo en memoria.

// EnforcementLayer es dónde vive una restricción medida.
type EnforcementLayer uint8

const (
	// LayerFirewallRules son las reglas del Firewall de Windows del grupo
	// propio. Son las que ABREN y las que el usuario puede ver sin elevar.
	LayerFirewallRules EnforcementLayer = iota + 1
	// LayerPacketFilter es el filtro de paquetes propio, acotado al adaptador
	// virtual. Es el que CIERRA todo lo demás.
	LayerPacketFilter
)

func (l EnforcementLayer) String() string {
	switch l {
	case LayerFirewallRules:
		return "reglas del firewall"
	case LayerPacketFilter:
		return "filtro de paquetes"
	default:
		return "capa-inválida"
	}
}

// AppliedRule es una regla tal como se MIDIÓ en el sistema.
//
// No es lo que se pidió: es lo que hay. La diferencia entre las dos cosas es
// justo lo que esta capa existe para calcular.
type AppliedRule struct {
	// Name empareja con [FirewallRule.Name], que es determinista a partir del
	// perfil y el rango. De eso depende que el diff funcione sin guardar ids.
	Name  string
	Layer EnforcementLayer
	// Enabled distingue "no está" de "está y alguien la apagó". Las dos dejan
	// el puerto cerrado, y solo una significa que hubo alguien.
	Enabled bool
}

// GateState es si la compuerta del adaptador virtual está puesta.
//
// La compuerta es el bloqueo de todo lo que no esté permitido, acotado al
// adaptador. Sin ella, la lista de permitidos es aditiva y cualquier regla
// ajena permisiva alcanza al usuario por la red virtual.
type GateState uint8

const (
	// GateUnknown es que NO SE PUDO MEDIR, y jamás se muestra como si estuviera
	// bien. La pantalla dice que Kanpachi no pudo leer lo que tiene puesto, por
	// la misma razón que existe [AlertAuditFailed].
	GateUnknown GateState = iota
	GateAbsent
	GatePresent
)

func (g GateState) String() string {
	switch g {
	case GatePresent:
		return "puesta"
	case GateAbsent:
		return "ausente"
	default:
		return "sin comprobar"
	}
}

// Enforcement es lo que el sistema tiene puesto AHORA, medido en las dos capas.
//
// Lo devuelve el adaptador de auditoría y lo juzga [Enforcement.Diff]. Reemplaza
// a un booleano que decía "las reglas están intactas": un booleano no puede
// decir QUÉ falta, y esa es justo la frase que la pantalla necesita.
type Enforcement struct {
	Rules []AppliedRule
	Gate  GateState
}

// EnforcementDiff es el veredicto: qué falta, qué sobra, y si la compuerta está.
type EnforcementDiff struct {
	// Missing son reglas deseadas que el sistema no tiene, o que tiene
	// apagadas. Es lo que la autorreparación repone.
	Missing []string
	// Extra son reglas del grupo propio que nadie pidió. Kanpachi es el único
	// que escribe en ese grupo, así que una de más significa que alguien lo
	// tocó, o que quedó de una salida sucia.
	Extra []string
	// GateMissing es que la compuerta se midió y no está. Con la compuerta
	// ausente NO se abre nada nuevo: se degrada exactamente a la contención de
	// antes de que existiera, que es la del Firewall de Windows. Sigue siendo
	// una alerta, jamás una emergencia.
	GateMissing bool
	// GateUnchecked es que no se pudo medir. Distinto de ausente a propósito:
	// una es un hecho y la otra es ceguera.
	GateUnchecked bool
}

// Intact dice si el sistema tiene exactamente lo que se pidió.
//
// La compuerta sin comprobar NO cuenta como alterado. Lo que no se pudo medir
// se denuncia aparte, y mezclarlo acá haría que una auditoría caída se leyera
// como manipulación.
func (d EnforcementDiff) Intact() bool {
	return len(d.Missing) == 0 && len(d.Extra) == 0 && !d.GateMissing
}

// Diff compara lo medido contra el estado deseado.
//
// Es pura, así que se testea sin Windows y sin adaptadores, que es la mitad de
// la razón por la que vive acá.
//
// La compuerta se juzga contra `wantGate` en vez de asumirla siempre: sin sala
// abierta no hay adaptador virtual, y exigir una compuerta sobre algo que no
// existe produciría una alerta encendida en reposo, que es la forma más rápida
// de que el usuario aprenda a ignorar la pantalla.
func (e Enforcement) Diff(desired RuleSet, wantGate bool) EnforcementDiff {
	var d EnforcementDiff

	puestas := make(map[string]bool, len(e.Rules))
	for _, r := range e.Rules {
		if r.Layer != LayerFirewallRules {
			continue
		}
		// Una regla apagada cuenta como que no está: el efecto es el mismo y
		// reponerla es lo mismo.
		if r.Enabled {
			puestas[r.Name] = true
		} else if _, ok := puestas[r.Name]; !ok {
			puestas[r.Name] = false
		}
	}

	quiere := make(map[string]bool, len(desired.Rules))
	for _, r := range desired.Rules {
		quiere[r.Name] = true
		if !puestas[r.Name] {
			d.Missing = append(d.Missing, r.Name)
		}
	}

	for nombre := range puestas {
		if !quiere[nombre] {
			d.Extra = append(d.Extra, nombre)
		}
	}

	// Orden estable: esto alimenta el texto de una alerta, y dos latidos con la
	// misma entrada tienen que producir el mismo texto. Sin esto la pantalla
	// parpadearía sola por el recorrido aleatorio de un mapa.
	sort.Strings(d.Missing)
	sort.Strings(d.Extra)

	switch {
	case !wantGate:
	case e.Gate == GateUnknown:
		d.GateUnchecked = true
	case e.Gate == GateAbsent:
		d.GateMissing = true
	}
	return d
}
