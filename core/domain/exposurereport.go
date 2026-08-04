package domain

import (
	"net/netip"
	"sort"
	"time"
)

// ExposureReport es lo que Kanpachi tiene abierto, MEDIDO.
//
// # Por qué existe si ya está [Enforcement]
//
// Porque aquel dice qué reglas están puestas, por nombre, y la pantalla tiene
// que decir QUÉ ABRE cada una y PARA QUIÉN. El nombre de una regla es
// determinista y opaco: sirve para comparar, no para leer.
//
// Junta las dos mitades en un solo sitio: lo que se pidió, que tiene los
// puertos, y lo que se midió, que tiene la verdad sobre si está.
//
// # Las dos filas de la pantalla
//
// Los puertos abiertos son una. La otra es que **todo lo demás del adaptador
// virtual está cerrado**, y esa segunda es la que convierte la lista de arriba
// de "lo que Kanpachi abrió" en "lo que hay abierto". Sin ella la lista es
// cierta y engañosa a la vez: enumera lo propio sin decir que la puerta de al
// lado la puede tener abierta cualquiera.
type ExposureReport struct {
	// MeasuredAt es cuándo se midió. El CERO significa que no se pudo medir, y
	// es la diferencia entre una pantalla que informa y una que miente. Con el
	// cero, la pantalla dice que Kanpachi no pudo leer lo que tiene puesto, y
	// jamás la última lista buena. Misma doctrina que [AlertAuditFailed].
	MeasuredAt time.Time

	Ports []ExposedPort
	Gate  GateState

	// Unexpected son reglas del grupo propio que nadie pidió. Kanpachi es el
	// único que escribe ahí, así que una de más significa que alguien lo tocó, o
	// que quedó de una salida sucia.
	Unexpected []string
}

// Blind dice si esto es una medición o una ceguera.
func (r ExposureReport) Blind() bool { return r.MeasuredAt.IsZero() }

// ExposedPort es un hueco, con lo que hace falta para poder leerlo.
type ExposedPort struct {
	// Name es el de la regla, determinista y opaco. Va para poder emparejar con
	// lo medido, y no para enseñárselo a nadie.
	Name  string
	Proto Proto
	From  uint16
	To    uint16

	// Members son las direcciones que pueden alcanzarlo, y Nets los rangos.
	// Que estén vacíos JAMÁS significa cualquiera: el dominio no tiene forma de
	// expresar eso, y [RuleSet] rechaza una regla sin alcance remoto.
	Members []netip.Addr
	Nets    []netip.Prefix

	// Applied es si el sistema lo tiene puesto AHORA. Falso con una medición
	// buena significa que Kanpachi pidió abrirlo y no está abierto, o sea que
	// alguien no va a poder entrar.
	Applied bool

	// Control distingue el hueco del canal de la sala de un puerto de juego.
	// Son cosas distintas para quien lee: uno lo pidió el juego que eligió y el
	// otro lo abre Kanpachi por su cuenta, y esa diferencia hay que decirla.
	Control bool
}

// NewExposureReport junta lo pedido con lo medido.
//
// Es pura y por eso se prueba sin Windows, que es la mitad de la razón por la
// que vive acá. La otra mitad es que decidir qué se le enseña al usuario sobre
// su propia exposición no es trabajo de un adaptador.
//
// `at` es cuándo se midió, y lo pone quien llama porque quien serializa no
// decide qué hora es. Pasar el cero produce el informe ciego a propósito, que es
// lo que hay que devolver cuando la medición falló.
func NewExposureReport(desired RuleSet, e Enforcement, wantGate bool, at time.Time) ExposureReport {
	if at.IsZero() {
		return BlindExposure()
	}

	d := e.Diff(desired, wantGate)
	falta := make(map[string]bool, len(d.Missing))
	for _, n := range d.Missing {
		falta[n] = true
	}

	r := ExposureReport{
		MeasuredAt: at,
		Gate:       e.Gate,
		Ports:      make([]ExposedPort, 0, len(desired.Rules)),
		Unexpected: d.Extra,
	}
	for _, rule := range desired.Rules {
		r.Ports = append(r.Ports, ExposedPort{
			Name:    rule.Name,
			Proto:   rule.Proto,
			From:    rule.From,
			To:      rule.To,
			Members: append([]netip.Addr(nil), rule.Remote...),
			Nets:    append([]netip.Prefix(nil), rule.Nets...),
			// Lo medido manda. Pedirlo y que esté puesto son dos cosas, y esta
			// pantalla existe justamente para no confundirlas.
			Applied: !falta[rule.Name],
			Control: rule.IsControl(),
		})
	}

	// Orden estable: dos lecturas seguidas con el mismo estado tienen que
	// producir la misma pantalla. Sin esto la lista parpadearía sola.
	sort.Slice(r.Ports, func(i, j int) bool { return r.Ports[i].Name < r.Ports[j].Name })
	return r
}

// BlindExposure es lo que se devuelve cuando la medición falló.
//
// Con la compuerta SIN COMPROBAR y sin puertos, para que ninguna lectura de esto
// pueda pintarse como "no hay nada abierto": eso sería exactamente la mentira
// que [ExposureReport.MeasuredAt] existe para impedir.
func BlindExposure() ExposureReport {
	return ExposureReport{Gate: GateUnknown}
}
