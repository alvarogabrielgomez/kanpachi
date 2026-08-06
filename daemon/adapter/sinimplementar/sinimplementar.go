// Package sinimplementar son los puertos que todavía no tienen adaptador.
//
// # Por qué fallan en TODO, y por qué eso no es pereza
//
// Un provisional que devuelve éxito hace la cuarentena **inverificable desde
// dentro del programa**. Con un firewall que dice "purgado" sin purgar, la
// pantalla pinta verde, el módulo de exposición no encuentra nada raro, y el
// producto entero afirma una promesa que nadie cumplió. El daño no es que
// falten funciones: es que la mentira sea indistinguible de la verdad.
//
// Fallando, en cambio, todo lo que dependa de ellos se rompe en la cara de
// quien lo está escribiendo, que es exactamente cuando conviene enterarse.
//
// # La excepción, y es una sola
//
// [SystemEvents] no puede fallar porque su firma no tiene por dónde: sus tres
// métodos devuelven canales, no errores. Su provisional devuelve canales que
// **nunca emiten**, y eso no es fingir que funciona: es decir la verdad, que no
// hay eventos. El supervisor ya está diseñado para eso, porque reaplica los
// ajustes cada tantos latidos precisamente porque estos eventos no son fiables
// ni con el adaptador de verdad.
//
// # El riesgo real, que no es que fallen
//
// Es que un binario con provisionales termine instalado en la máquina de
// alguien. Por eso [Presente] existe y por eso `cmd/kanpachid` se niega a
// correr como servicio cuando es true: fallar delante de quien programa es
// barato, fallar callado en la máquina de un amigo no.
package sinimplementar

import (
	"context"
	"errors"
	"fmt"
	"net/netip"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/core/port"
)

// ErrSinImplementar es lo que devuelve todo esto.
//
// Un centinela y no un error suelto por cada sitio, para que quien reciba uno
// pueda distinguir "esto todavía no existe" de "esto falló", que son dos cosas
// muy distintas cuando estás leyendo un log a las dos de la mañana.
var ErrSinImplementar = errors.New("sinimplementar: este adaptador todavía no existe")

func falla(qué string) error { return fmt.Errorf("%w: %s", ErrSinImplementar, qué) }

// Engine no arranca ningún motor.
type Engine struct{}

func (Engine) HostNetwork(context.Context, domain.HostSpec) error { return falla("arrancar la red") }
func (Engine) JoinRendezvous(context.Context, domain.RendezvousSpec) error {
	return falla("entrar al vestíbulo")
}
func (Engine) LeaveRendezvous(context.Context) error { return falla("salir del vestíbulo") }
func (Engine) JoinWithCredential(context.Context, domain.GuestSpec) error {
	return falla("entrar con credencial")
}

// Leave es IDEMPOTENTE en el de verdad y acá también devuelve nil, porque lo
// llama el camino de error de crear y de entrar. Un error acá convertiría un
// fallo en dos, y no afirma nada falso: no había red que dejar.
func (Engine) Leave(context.Context) error { return nil }

func (Engine) IssueCredential(context.Context, domain.CredentialRequest) (domain.Credential, error) {
	return domain.Credential{}, falla("emitir una credencial")
}
func (Engine) RevokeCredential(context.Context, domain.CredentialID) error {
	return falla("revocar una credencial")
}
func (Engine) ListCredentials(context.Context) ([]domain.Credential, error) {
	return nil, falla("listar credenciales")
}
func (Engine) Restart(context.Context) error { return falla("reiniciar el motor") }
func (Engine) Peers(context.Context) ([]domain.Peer, error) {
	return nil, falla("preguntar por los miembros")
}

// Events devuelve un canal cerrado y no uno mudo, a diferencia de
// [SystemEvents]: el supervisor compara este canal por identidad en cada latido
// y vuelve a pedirlo, así que uno cerrado se lee como "el motor no está", que
// es la verdad.
func (Engine) Events() <-chan domain.EngineEvent {
	ch := make(chan domain.EngineEvent)
	close(ch)
	return ch
}

func (Engine) Diagnostics(context.Context) (domain.NetCheck, error) {
	return domain.NetCheck{}, falla("diagnosticar la red")
}

// Firewall no toca ni una regla.
//
// **RestoreForeign falla como los demás, y es deliberado.** Es el que más
// invita a la excepción, porque el arranque lo llama por si una salida sucia
// dejó algo desactivado, y devolver nil haría que el arranque siguiera. Pero eso
// es exactamente lo peligroso: el daemon anotaría "reglas ajenas restauradas"
// sin haber restaurado nada, y el usuario se quedaría con reglas de su juego
// desactivadas creyendo que volvieron.
type Firewall struct{}

func (Firewall) Apply(context.Context, domain.RuleSet) error { return falla("aplicar reglas") }
func (Firewall) ApplyBaseQuarantine(context.Context, []domain.QuarantineRule) error {
	return falla("aplicar la cuarentena de base")
}
func (Firewall) PurgeOwned(context.Context) error { return falla("purgar el grupo Kanpachi") }
func (Firewall) AuditForeign(context.Context, domain.GameProfile) ([]domain.ForeignRule, error) {
	return nil, falla("buscar reglas ajenas")
}
func (Firewall) SuspendForeign(context.Context, []domain.ForeignRule) error {
	return falla("desactivar reglas ajenas")
}
func (Firewall) RestoreForeign(context.Context) error { return falla("restaurar reglas ajenas") }
func (Firewall) BindRoom(context.Context, netip.Prefix, domain.RoomBinding) error {
	return falla("acotar la compuerta a los adaptadores")
}

// UnbindRoom no falla, y es el único de este tipo que no lo hace.
//
// Olvidar un alcance que nunca se puso no puede fracasar, y devolver error acá
// obligaría a quien sale de la sala a tratar un fallo imposible en el camino de
// limpieza, que es justo donde no se quiere ruido.
func (Firewall) UnbindRoom() {}

// NetConfig no toca el adaptador.
type NetConfig struct{}

func (NetConfig) ApplyAdapter(context.Context, domain.AdapterState) error {
	return falla("configurar el adaptador")
}
func (NetConfig) RevertTweaks(context.Context) error { return falla("revertir los ajustes") }
func (NetConfig) ProbeMTU(context.Context) (int, error) {
	return 0, falla("sondear el MTU")
}
func (NetConfig) SetDirectPlay(context.Context, bool) error { return falla("tocar DirectPlay") }

// Routing no mira la tabla de rutas.
//
// Falla en vez de devolver la lista vacía, que sería peor de lo que parece: sin
// prefijos locales, el plan de direcciones concluiría que NINGÚN rango choca
// con la red de casa y elegiría uno que sí choca.
type Routing struct{}

func (Routing) LocalPrefixes(context.Context) ([]netip.Prefix, error) {
	return nil, falla("leer la tabla de rutas")
}

// Library no detecta juegos.
//
// Este es el único que podría devolver la lista vacía sin mentir, porque la
// detección es falible por diseño y core ya trata el error como lista vacía.
// Falla igual, para que el log diga que no hay adaptador en vez de dejar creer
// que esta máquina no tiene juegos.
type Library struct{}

func (Library) Installed(context.Context) ([]domain.GameRef, error) {
	return nil, falla("detectar los juegos instalados")
}

// Inspector no mira ningún proceso.
type Inspector struct{}

func (Inspector) Snapshot(context.Context, domain.ProcessRef) ([]domain.Listener, error) {
	return nil, falla("mirar los puertos de un proceso")
}

// Audit no comprueba nada.
//
// Falla en los tres, y el resultado se ve: levanta [domain.AlertAuditFailed], o
// sea que la pantalla dice "Kanpachi no pudo comprobar cómo está tu PC" en vez
// de quedarse en verde. Es la prueba de que esa alerta hacía falta.
type Audit struct{}

func (Audit) FirewallEnabled(context.Context) ([]domain.FirewallProfileState, error) {
	return nil, falla("comprobar el estado del firewall")
}
func (Audit) Enforcement(context.Context) (domain.Enforcement, error) {
	return domain.Enforcement{}, falla("medir lo que el firewall tiene puesto")
}
func (Audit) RouterMappings(context.Context) ([]domain.PortMapping, error) {
	return nil, falla("consultar los mapeos del router")
}

// Events es la excepción: canales que NUNCA emiten.
//
// No puede fallar porque su firma no tiene por dónde. Y no hace falta que
// pueda: el supervisor reaplica los ajustes cada tantos latidos aunque no
// llegue ningún evento, porque estas tres suscripciones no son fiables ni con
// el adaptador de verdad. Sin ese respaldo, una suscripción muerta se traduce
// en "ayer funcionaba".
type Events struct {
	identificada chan struct{}
	despertó     chan struct{}
	cambió       chan struct{}
}

func NewEvents() *Events {
	return &Events{
		identificada: make(chan struct{}),
		despertó:     make(chan struct{}),
		cambió:       make(chan struct{}),
	}
}

func (e *Events) NetworkIdentified() <-chan struct{} { return e.identificada }
func (e *Events) Resumed() <-chan struct{}           { return e.despertó }
func (e *Events) NetworkChanged() <-chan struct{}    { return e.cambió }

// Close cierra los tres canales, igual que el de verdad, y NUNCA espera a que
// alguien lea: un Close que bloqueara colgaría el apagado del servicio.
func (e *Events) Close() error {
	close(e.identificada)
	close(e.despertó)
	close(e.cambió)
	return nil
}

// Las comprobaciones de que esto sigue encajando en los puertos. Si una firma
// de core cambia, el error sale acá y no en el sitio donde se cablea.
var (
	_ port.EnginePort      = Engine{}
	_ port.FirewallPort    = Firewall{}
	_ port.NetConfigPort   = NetConfig{}
	_ port.RoutingTable    = Routing{}
	_ port.GameLibrary     = Library{}
	_ port.SocketInspector = Inspector{}
	_ port.ExposureAudit   = Audit{}
	_ port.SystemEvents    = (*Events)(nil)
)
