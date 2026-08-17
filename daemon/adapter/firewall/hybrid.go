// Package firewall junta las DOS capas de contención en una sola.
//
// # Por qué son dos y no una
//
// La capa que ABRE es visible y es del sistema: en Windows, las reglas del
// Firewall de Windows, que el usuario enumera sin elevar y cuyos bloqueos le
// siguen ganando a los permisos de Kanpachi. Lo que NO pueden expresar es
// "denegar todo en el adaptador salvo esto", porque los bloqueos explícitos
// ganan sobre cualquier permiso en conflicto y taparían también los permisos
// propios.
//
// La compuerta es la segunda capa, un filtro de paquetes acotado al adaptador
// virtual, y ahí un bloqueo SÍ admite excepciones por encima. Eso convierte la
// lista de permitidos de ADITIVA en COMPLETA, que es lo único que cierra el caso
// de una regla ajena de escritorio remoto. Ver decisión 27.
//
// # En Linux la segunda capa hace las dos cosas, y este fichero no cambia
//
// nftables sí expresa "denegar salvo esto" en una tabla propia, así que la
// compuerta se basta sola. La capa que abre queda como AUDITORÍA: en Linux
// nadie puede abrir por encima del `drop` de un ufw o un firewalld, así que se
// reporta y no se toca. La cuarentena de base sigue siendo suya, porque es lo
// único que tiene que sobrevivir a Kanpachi apagado.
//
// La composición no se entera de nada de eso, y ese es el punto: sigue siendo
// intersección, sigue habiendo un orden correcto, y [Permits] sigue siendo el
// mismo contrato en los dos sistemas.
//
// # Por qué este fichero es puro
//
// Porque lo que decide no es cómo se habla con el sistema: es el ORDEN de las
// dos capas y qué pasa cuando una falla. Las dos cosas tienen una dirección
// correcta y una que abre un agujero, y ninguna necesita un sistema concreto
// para comprobarse.
package firewall

import (
	"context"
	"fmt"
	"net/netip"
	"sync"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/core/port"
	"github.com/accentiostudios/kanpachi/daemon/adapter/firewall/gate"
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
	// The three for the FOREIGN firewall that blocks inbound. On Linux they
	// read and touch ufw/firewalld with consent and a ledger (decision 36); on
	// Windows they answer empty until the case is measured. See
	// [port.FirewallPort].
	InboundBlocked(ctx context.Context) ([]domain.FirewallBlock, error)
	AllowAdapters(ctx context.Context, blocks []domain.FirewallBlock) error
	WithdrawAdapters(ctx context.Context) error
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
	Apply(ctx context.Context, want []gate.Spec) error
	Purge(ctx context.Context) error
	Measure(ctx context.Context, want []gate.Spec) (gate.Measurement, error)
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
	scope gate.Scope

	// last es lo último que se le PIDIÓ a la compuerta, y no lo que tiene
	// puesto. Sirve para saber por qué filtros preguntar al medir: las ranuras
	// son posiciones y no nombres. Quien contesta si el filtro está sigue siendo
	// el sistema.
	last []gate.Spec

	// swept es si ya se barrió la compuerta alguna vez en la vida de este
	// objeto. Existe para que el primer Apply sin sala barra igual: un daemon
	// que murió sucio dejó filtros puestos, porque la sesión no es dinámica.
	swept bool

	// ifaceOf traduce el nombre de un adaptador al número que lo identifica.
	//
	// Se INYECTA para que este fichero siga siendo puro: resolverlo es una
	// llamada al sistema, y lo que se decide acá es el orden de las dos capas y
	// qué pasa cuando una falla, que se prueba sin sistema. El cableado de
	// Windows le pasa [wfp.LUIDOf] y el de Linux, el `ifindex`.
	ifaceOf IfaceResolver
}

// IfaceResolver traduce el nombre de un adaptador al número que lo identifica:
// el LUID en Windows, el `ifindex` en Linux.
//
// Devuelve error y jamás cero: el cero no es "sin adaptador", es TODAS las
// interfaces, y con un bloqueo duro eso deja al usuario sin su red de casa. Vale
// igual para los dos sistemas, y por eso la firma es una sola.
type IfaceResolver func(name string) (uint64, error)

// El parámetro de la compuerta se llama `g` y no `gate` a propósito: `gate` es
// el paquete del modelo compartido, y un parámetro con ese nombre lo taparía
// dentro de esta función.
func New(permits Permits, audit PermitAudit, g Gate, log Logger, ifaceOf IfaceResolver) (*Firewall, error) {
	if permits == nil || audit == nil || g == nil || log == nil {
		return nil, fmt.Errorf("el firewall necesita las dos capas, su auditoría y un log")
	}
	if ifaceOf == nil {
		// Sin resolver, `BindRoom` no puede acotar nada, o sea que la compuerta
		// no se pone nunca y el fallo aparecería recién al abrir una sala, con
		// la red virtual ya arriba.
		return nil, fmt.Errorf("el firewall necesita resolver el nombre del adaptador a un " +
			"número de interfaz, o la compuerta no se puede acotar")
	}
	return &Firewall{permits: permits, audit: audit, gate: g, log: log, ifaceOf: ifaceOf}, nil
}

// La comprobación de que esto sigue encajando en el puerto. Si cambia una firma
// en core, el error sale acá y no donde se cablee.
var _ port.FirewallPort = (*Firewall)(nil)

// SetScopeForMeasurement acota a un adaptador elegido A MANO.
//
// # Por qué el nombre lo dice, en vez de llamarse SetScope a secas
//
// Porque el producto NO entra por acá: entra por [Firewall.BindRoom], que
// resuelve las constantes del dominio y además cubre el vestíbulo. Esto existe
// solo para `internal/fwprobe`, que mide la compuerta sobre un adaptador FÍSICO
// de verdad, que es como se comprobó con dos máquinas que el bloqueo de WFP le
// gana a un permiso del Firewall de Windows.
//
// El nombre largo es lo que permite que el guardián del cableado no tenga
// excepciones silenciosas: exige que todo método de unión se llame desde
// producción, y este se salta la regla diciendo en su propio nombre por qué.
// Dos veces ya pasó que un método de unión estuviera escrito, probado y sin
// llamar desde el daemon, y las dos veces se descubrió corriendo el producto.
//
// Acotar sigue siendo una sola llamada para las dos capas: si discreparan sobre
// qué adaptador es la sala, los permisos irían sobre uno y el bloqueo sobre
// otro, con las dos contestando que sí.
func (f *Firewall) SetScopeForMeasurement(adapter string, iface uint64, room netip.Prefix) error {
	if adapter == "" {
		return fmt.Errorf("los permisos necesitan el NOMBRE del adaptador, y llegó vacío")
	}
	return f.setScope(gate.Scope{Iface: iface, Net: room}, adapter)
}

// setScope es la única puerta por la que se acota, y valida SIEMPRE.
//
// Estar sola importa: un alcance que entrara sin pasar por `Valid` podría
// llevar `0.0.0.0/0`, y un bloqueo duro con ese alcance deja al usuario sin la
// entrada de su red de casa sin que nada se vea raro leyendo el código.
func (f *Firewall) setScope(scope gate.Scope, adapter string) error {
	if err := scope.Valid(); err != nil {
		return fmt.Errorf("el alcance de la sala no sirve para acotar: %w", err)
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
	f.scope = gate.Scope{}
}

// BindRoom es [port.FirewallPort.BindRoom]: acota las dos capas a los
// adaptadores que el motor acaba de crear.
//
// # Por qué acá no viaja ningún nombre
//
// Los adaptadores son [domain.AdapterName] y [domain.LobbyAdapterName],
// constantes del dominio, y este método las resuelve a número de interfaz con la función que
// le inyectó el cableado de Windows. Aceptar el nombre por parámetro dejaría al
// caso de uso eligiendo a qué adaptador se acota un BLOQUEO DURO, que es la
// decisión que separa contener la sala de dejar al usuario sin su red de casa.
//
// Que el vestíbulo falte no es un fallo: el invitado lo suelta al entrar. Que
// falte cuando se pidió, sí.
//
// `lobbyNet` es el /24 del vestíbulo de ESTA sala, que cada una deriva de su
// código. Se recibe en vez de leerse de una constante desde que el vestíbulo
// dejó de vivir en un rango fijo. Ver [domain.Rendezvous.LobbySubnet].
func (f *Firewall) BindRoom(
	ctx context.Context, room, lobbyNet netip.Prefix, with domain.RoomBinding,
) error {
	_ = ctx // resolver un nombre a un número de interfaz no espera por nada

	if f.ifaceOf == nil {
		return fmt.Errorf("el firewall se construyó sin resolver de adaptadores, así que la " +
			"compuerta no se puede acotar y no hay forma de contener la sala")
	}

	iface, err := f.ifaceOf(domain.AdapterName)
	if err != nil {
		return fmt.Errorf("no se encontró el adaptador %s de la sala: %w", domain.AdapterName, err)
	}
	scope := gate.Scope{Iface: iface, Net: room}

	if with == domain.BindRoomAndLobby {
		// Sin el rango no se puede emitir el bloqueo por red del vestíbulo, y
		// emitir el resto igual dejaría el adaptador del vestíbulo acotado solo
		// por interfaz. Se corta acá en vez de degradar en silencio.
		if !lobbyNet.IsValid() {
			return fmt.Errorf("se pidió acotar también el vestíbulo y no vino su rango, " +
				"así que la compuerta no lo podría bloquear por red")
		}
		lobby, err := f.ifaceOf(domain.LobbyAdapterName)
		if err != nil {
			return fmt.Errorf("no se encontró el adaptador %s del vestíbulo, que es donde "+
				"llega gente que todavía no es miembro: %w", domain.LobbyAdapterName, err)
		}
		scope.Lobby = lobby
		scope.LobbyNet = lobbyNet
	}

	if err := f.setScope(scope, domain.AdapterName); err != nil {
		return err
	}
	f.log.Info("la compuerta quedó acotada", "sala", room.String(), "alcance", with.String())
	return nil
}

// UnbindRoom es [port.FirewallPort.UnbindRoom].
func (f *Firewall) UnbindRoom() { f.ClearScope() }

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
		// # Sin compuerta no se abre NADA. Falla en la cara
		//
		// Esto antes dejaba un aviso en el log y escribía los permisos igual, o
		// sea que la lista de permitidos volvía a ser ADITIVA justo cuando había
		// puertos que abrir: una regla ajena de escritorio remoto alcanzando al
		// usuario por la red virtual, con la pantalla diciendo que la sala está
		// configurada.
		//
		// El conjunto VACÍO sigue pasando, y no es una excepción: sin nada que
		// abrir no hay nada que acotar. Ese es el caso normal del daemon en
		// reposo, y es también lo que garantiza que la interfaz virtual nazca
		// sin nada abierto.
		if len(desired.Rules) > 0 {
			return fmt.Errorf("hay %d regla(s) que abrir y la compuerta no está acotada a "+
				"ningún adaptador, así que no habría nada que las contenga. No se abre nada",
				len(desired.Rules))
		}

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

func (f *Firewall) InboundBlocked(ctx context.Context) ([]domain.FirewallBlock, error) {
	return f.permits.InboundBlocked(ctx)
}

func (f *Firewall) AllowAdapters(ctx context.Context, blocks []domain.FirewallBlock) error {
	return f.permits.AllowAdapters(ctx, blocks)
}

func (f *Firewall) WithdrawAdapters(ctx context.Context) error {
	return f.permits.WithdrawAdapters(ctx)
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
func (f *Firewall) specsFor(desired domain.RuleSet) ([]gate.Spec, error) {
	if err := f.scope.Valid(); err != nil {
		return nil, nil
	}
	return gate.SpecsFor(desired, f.scope)
}
