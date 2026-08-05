// Package firewall junta las DOS capas de contención en una sola.
//
// # Por qué son dos y no una
//
// Las reglas del Firewall de Windows son las que ABREN, y son visibles: el
// usuario las enumera sin elevar, salen en la consola del sistema, y sus
// bloqueos le siguen ganando a los permisos de Kanpachi. Lo que NO pueden
// expresar es "denegar todo en el adaptador salvo esto", porque los bloqueos
// explícitos ganan sobre cualquier permiso en conflicto y taparían también los
// permisos propios.
//
// La compuerta es la segunda capa, un filtro de paquetes acotado al adaptador
// virtual, y ahí un bloqueo SÍ admite excepciones por encima. Eso convierte la
// lista de permitidos de ADITIVA en COMPLETA, que es lo único que cierra el caso
// de una regla ajena de escritorio remoto. Ver decisión 27.
//
// # Por qué este fichero es puro
//
// Porque lo que decide no es cómo se habla con Windows: es el ORDEN de las dos
// capas y qué pasa cuando una falla. Las dos cosas tienen una dirección correcta
// y una que abre un agujero, y ninguna necesita Windows para comprobarse.
package firewall

import (
	"context"
	"fmt"
	"net/netip"
	"sync"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/core/port"
	"github.com/accentiostudios/kanpachi/daemon/adapter/firewall/wfp"
)

// Permits es la capa que ABRE.
type Permits interface {
	Apply(ctx context.Context, desired domain.RuleSet) error
	// ApplyBaseQuarantine es la ÚNICA de esta interfaz que no es de la sala.
	// Escribe lo que falte de la cuarentena de base y no borra nada.
	ApplyBaseQuarantine(ctx context.Context, rules []domain.QuarantineRule) error
	PurgeOwned(ctx context.Context) error
	AuditForeign(ctx context.Context, p domain.GameProfile) ([]domain.ForeignRule, error)
	SuspendForeign(ctx context.Context, rules []domain.ForeignRule) error
	RestoreForeign(ctx context.Context) error
	// SetAdapter anota el nombre del adaptador virtual cuando el motor lo crea.
	SetAdapter(name string)
}

// PermitAudit es lo que la capa de permisos puede MEDIR.
//
// Va aparte de [Permits] porque son dos objetos distintos del mismo adaptador, y
// porque uno escribe y el otro solo lee. Mezclarlos haría que la parte que mide
// tuviera a mano los métodos que modifican, que es justo lo contrario de lo que
// se quiere de una auditoría.
type PermitAudit interface {
	FirewallEnabled(ctx context.Context) ([]domain.FirewallProfileState, error)
	// Enforcement devuelve las reglas vivas del grupo propio, y el estado de la
	// compuerta en AUSENTE porque desde ahí no hay forma de saberlo. Esa
	// respuesta la pisa [Firewall.Enforcement] con la medición de verdad.
	Enforcement(ctx context.Context) (domain.Enforcement, error)
}

// Gate es la capa que CIERRA todo lo demás.
type Gate interface {
	Apply(ctx context.Context, want []wfp.FilterSpec) error
	Purge(ctx context.Context) error
	Measure(ctx context.Context, want []wfp.FilterSpec) (wfp.Measurement, error)
}

// Logger es la rebanada del log del daemon que hace falta acá.
type Logger interface {
	Info(msg string, kv ...any)
	Warn(msg string, kv ...any)
	Error(msg string, kv ...any)
}

// Firewall es las dos capas juntas.
//
// Es INTERSECCIÓN y no reemplazo: un paquete tiene que pasar las dos. Por eso
// la compuerta no abre nada que los permisos visibles no abran ya, y quien
// audite con sus herramientas de siempre sigue viendo la lista completa de lo
// que está abierto.
type Firewall struct {
	mu      sync.Mutex
	permits Permits
	audit   PermitAudit
	gate    Gate
	log     Logger

	// scope es el adaptador y el rango de la sala. Vacío hasta que el motor crea
	// el adaptador, que pasa DESPUÉS de que arranca el daemon.
	scope wfp.Scope

	// last es lo último que se le PIDIÓ a la compuerta, y no lo que tiene
	// puesto. Sirve para saber por qué filtros preguntar al medir: las ranuras
	// son posiciones y no nombres. Quien contesta si el filtro está sigue siendo
	// el sistema.
	last []wfp.FilterSpec

	// swept es si ya se barrió la compuerta alguna vez en la vida de este
	// objeto. Existe para que el primer Apply sin sala barra igual: un daemon
	// que murió sucio dejó filtros puestos, porque la sesión no es dinámica.
	swept bool
}

func New(permits Permits, audit PermitAudit, gate Gate, log Logger) (*Firewall, error) {
	if permits == nil || audit == nil || gate == nil || log == nil {
		return nil, fmt.Errorf("el firewall necesita las dos capas, su auditoría y un log")
	}
	return &Firewall{permits: permits, audit: audit, gate: gate, log: log}, nil
}

// La comprobación de que esto sigue encajando en el puerto. Si cambia una firma
// en core, el error sale acá y no donde se cablee.
var _ port.FirewallPort = (*Firewall)(nil)

// SetScope anota dónde vive la sala, y las dos capas se enteran a la vez.
//
// # Por qué una sola llamada y no dos
//
// Porque si las dos capas discreparan sobre qué adaptador es la sala, los
// permisos se escribirían sobre uno y el bloqueo sobre otro. El resultado es un
// adaptador con los permisos y sin compuerta, o sea el agujero abierto, y una
// pantalla que dice que todo está puesto porque las dos capas contestan que sí.
//
// Se llama cuando el motor crea el adaptador. Antes de eso la compuerta no
// existe, y eso no es un fallo: es el estado normal de un daemon recién
// arrancado.
func (f *Firewall) SetScope(adapter string, luid uint64, room netip.Prefix) error {
	scope := wfp.Scope{LUID: luid, Net: room}
	if err := scope.Valid(); err != nil {
		return fmt.Errorf("el alcance de la sala no sirve para acotar: %w", err)
	}
	if adapter == "" {
		return fmt.Errorf("los permisos necesitan el NOMBRE del adaptador, y llegó vacío")
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	f.permits.SetAdapter(adapter)
	f.scope = scope
	return nil
}

// ClearScope olvida el adaptador. Se llama al salir de la sala.
func (f *Firewall) ClearScope() {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.permits.SetAdapter("")
	f.scope = wfp.Scope{}
}

// Apply lleva las dos capas al conjunto deseado.
//
// # El orden importa, y solo hay uno correcto
//
// Primero la compuerta y después los permisos. Entre las dos llamadas hay un
// instante con una capa nueva y la otra vieja, y las dos direcciones no son
// equivalentes:
//
//   - Compuerta primero: durante ese instante la compuerta nueva convive con los
//     permisos viejos. Como es intersección, lo que sobra queda CERRADO.
//   - Permisos primero: durante ese instante hay permisos nuevos sin compuerta
//     que los acote, o sea la lista aditiva otra vez, o sea el agujero de
//     escritorio remoto abierto.
//
// Que la ventana dure milisegundos no cambia cuál de las dos es la correcta.
//
// # Si la compuerta falla, los permisos NO se tocan
//
// La compuerta se aplica dentro de una transacción, así que un fallo deja la
// anterior intacta. Devolver ahí, sin escribir los permisos nuevos, deja el
// sistema en el estado consistente anterior. Escribirlos igual sería justo
// producir el caso de arriba: permisos sin la compuerta que les corresponde.
func (f *Firewall) Apply(ctx context.Context, desired domain.RuleSet) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	specs, err := f.specsFor(desired)
	if err != nil {
		return err
	}

	if specs == nil {
		// Sin adaptador no hay red virtual, así que no hay nada que contener. Se
		// PURGA en vez de dejar lo anterior: filtros de una sala que ya no existe
		// no protegen nada y no se ven en ninguna herramienta del sistema.
		//
		// Solo cuando hay algo que barrer, o la primera vez. Sin esa condición
		// esto barrería las cuarenta ranuras en cada latido de una máquina en
		// reposo, que es una transacción contra el motor de filtrado por latido a
		// cambio de nada.
		if f.last != nil || !f.swept {
			if err := f.gate.Purge(ctx); err != nil {
				return err
			}
			f.last, f.swept = nil, true
		}
		f.log.Warn("la compuerta no se puso porque todavía no hay adaptador de la sala, " +
			"así que la contención es solo la de las reglas del firewall")
	} else {
		if err := f.gate.Apply(ctx, specs); err != nil {
			return fmt.Errorf("la compuerta no se pudo poner, así que no se abrió nada: %w", err)
		}
		f.last, f.swept = specs, true
	}

	return f.permits.Apply(ctx, desired)
}

// ApplyBaseQuarantine va SOLO a la capa de permisos, y esa asimetría con Apply
// es lo que hace que la cuarentena valga.
//
// La compuerta es un filtro de WFP acotado al adaptador virtual, y se cae con el
// proceso: sus filtros viven en una sesión dinámica del motor de filtrado, que
// es lo que impide que un daemon muerto deje la máquina bloqueada. Justo por eso
// no puede sostener la cuarentena, que tiene que seguir puesta con Kanpachi
// apagado. Las reglas del Firewall de Windows sí sobreviven al reinicio, y esa
// persistencia es la única propiedad que aquí se necesita.
//
// No toma el candado de las dos capas: no lee ni escribe `last` ni `swept`, que
// es lo único que ese candado protege.
func (f *Firewall) ApplyBaseQuarantine(ctx context.Context, rules []domain.QuarantineRule) error {
	return f.permits.ApplyBaseQuarantine(ctx, rules)
}

// PurgeOwned borra lo de las dos capas, y en el orden inverso al de Apply.
//
// Primero los permisos y después la compuerta, por lo mismo de siempre: quitar
// la compuerta primero dejaría, durante ese instante, permisos sin nada encima.
//
// Y si los permisos no se pudieron purgar, la compuerta se queda puesta. Es el
// único caso donde dejar algo a medias es lo correcto: unos permisos huérfanos
// bajo una compuerta siguen acotados, y los mismos permisos sin ella son el
// agujero.
func (f *Firewall) PurgeOwned(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if err := f.permits.PurgeOwned(ctx); err != nil {
		return err
	}
	f.last = nil
	if err := f.gate.Purge(ctx); err != nil {
		return err
	}
	f.swept = true
	return nil
}

func (f *Firewall) AuditForeign(ctx context.Context, p domain.GameProfile) ([]domain.ForeignRule, error) {
	return f.permits.AuditForeign(ctx, p)
}

func (f *Firewall) SuspendForeign(ctx context.Context, rules []domain.ForeignRule) error {
	return f.permits.SuspendForeign(ctx, rules)
}

func (f *Firewall) RestoreForeign(ctx context.Context) error {
	return f.permits.RestoreForeign(ctx)
}

func (f *Firewall) FirewallEnabled(ctx context.Context) ([]domain.FirewallProfileState, error) {
	return f.audit.FirewallEnabled(ctx)
}

// Enforcement mide las DOS capas y no juzga ninguna.
//
// Si eso es lo que se pidió lo decide [domain.Enforcement.Diff], que es dominio
// y corre sin Windows. El adaptador mide y el dominio juzga, por la misma razón
// por la que `Apply` calcula la diferencia contra las reglas vivas y jamás
// contra un recuerdo en memoria.
//
// # Por qué un fallo de una capa tira la medición entera
//
// Porque devolver la mitad medida y la otra en cero sería indistinguible de
// "esa mitad no tiene nada puesto", que es la lectura opuesta. Un error levanta
// [domain.AlertAuditFailed] y la pantalla dice que Kanpachi no pudo leer lo que
// tiene puesto, que es la verdad. Mostrar media pantalla en verde no lo es.
func (f *Firewall) Enforcement(ctx context.Context) (domain.Enforcement, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	e, err := f.audit.Enforcement(ctx)
	if err != nil {
		return domain.Enforcement{}, err
	}

	m, err := f.gate.Measure(ctx, f.last)
	if err != nil {
		return domain.Enforcement{}, fmt.Errorf("midiendo la compuerta: %w", err)
	}

	// El estado de la compuerta lo pisa la medición de verdad. La capa de
	// permisos lo devuelve AUSENTE porque no tiene forma de saberlo, y esa
	// respuesta solo vale mientras nadie componga las dos.
	e.Gate = m.Gate
	e.Rules = append(e.Rules, m.Rules...)
	return e, nil
}

// specsFor calcula los filtros de la compuerta, o nil si todavía no hay dónde.
func (f *Firewall) specsFor(desired domain.RuleSet) ([]wfp.FilterSpec, error) {
	if err := f.scope.Valid(); err != nil {
		return nil, nil
	}
	return wfp.SpecsFor(desired, f.scope)
}
