// Package nft es la compuerta de Kanpachi sobre nftables.
//
// # Por qué acá hay UNA capa y en Windows hay dos
//
// El modelo compartido, [gate], emite "denegar todo lo que entre por el
// adaptador virtual, salvo esto". En Windows eso no se puede expresar con las
// reglas del Firewall de Windows, porque sus bloqueos explícitos ganan sobre
// cualquier permiso y taparían también los propios, así que hizo falta una
// segunda capa donde el bloqueo sí admite excepciones por encima.
//
// nftables lo expresa directo. Medido en el banco: un `drop` corta la
// evaluación de todas las cadenas del hook, y un `accept` no es final, o sea que
// deja al paquete seguir a las cadenas de prioridad posterior. Primera
// coincidencia gana dentro de la cadena, así que basta con poner los permisos
// antes que los bloqueos y toda la compuerta cabe en una tabla propia.
//
// # Lo que esta capa NO puede hacer, y hay que decirlo
//
// Que un `accept` no sea final es lo que conserva el veto del operador, y
// también significa que **Kanpachi no puede abrir por encima del `drop` de un
// ufw, un firewalld o una cadena de Docker**. No es una limitación de esta
// implementación: netfilter no lo permite, y es la misma propiedad que hace
// fuerte a nuestro bloqueo. Lo ajeno que bloquea se abre por OTRA vía, con
// consentimiento explícito y anotado: se le pide al gestor del operador con su
// propio CLI, jamás desde esta tabla. Ver la decisión 36 y el paquete
// nftpermits; el resto de lo ajeno se sigue auditando sin tocarse.
//
// # La política de la cadena es ACCEPT, y es deliberado
//
// Una cadena base con política `drop` tira todo lo que entre a la máquina por
// cualquier interfaz, y este proceso corre con CAP_NET_ADMIN en un servidor al
// que se llega por SSH. Los bloqueos son reglas EXPLÍCITAS acotadas al
// adaptador; la política es la red de seguridad de que un fallo a medias deje la
// máquina alcanzable.
package nft

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
	"sync"

	"github.com/google/nftables"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/daemon/adapter/firewall/gate"
)

// Los nombres de lo que instala esta capa.
//
// Fijos y literales, para que la limpieza de un arranque encuentre lo que dejó
// el arranque anterior sin recordar nada. Es el mismo criterio que la GUID del
// sublayer en Windows.
const (
	TableName = "kanpachi"
	ChainName = "gate"
)

// Logger es la rebanada del log del daemon que hace falta acá.
type Logger interface {
	Info(msg string, kv ...any)
	Warn(msg string, kv ...any)
	Error(msg string, kv ...any)
}

// Gate es la compuerta.
type Gate struct {
	mu  sync.Mutex
	log Logger
}

// Open abre la compuerta.
//
// No toca el sistema: `github.com/google/nftables` abre su socket de netlink en
// cada operación, y sostener uno entre llamadas (`AsLasting`) obligaría a
// reabrirlo cuando el kernel lo cierra. La compuerta se pone y se quita pocas
// veces por sesión, así que el socket por operación no cuesta nada y quita un
// estado que se puede quedar viejo.
func Open(log Logger) (*Gate, error) {
	if log == nil {
		return nil, errors.New("la compuerta necesita un log")
	}
	return &Gate{log: log}, nil
}

// Close no tiene nada que cerrar, y existe para que el cableado no tenga que
// saberlo.
func (g *Gate) Close() error { return nil }

// tableOf devuelve la descripción de nuestra tabla y nuestra cadena.
//
// La cadena va en el hook `input` con prioridad `filter`. No antes: adelantarla
// la pondría por delante de `conntrack`, y sin conntrack un permiso de entrada
// no alcanza para que vuelva la respuesta.
func tableOf() (*nftables.Table, *nftables.Chain) {
	t := &nftables.Table{Family: nftables.TableFamilyINet, Name: TableName}
	politica := nftables.ChainPolicyAccept
	c := &nftables.Chain{
		Name:     ChainName,
		Table:    t,
		Type:     nftables.ChainTypeFilter,
		Hooknum:  nftables.ChainHookInput,
		Priority: nftables.ChainPriorityFilter,
		Policy:   &politica,
	}
	return t, c
}

// Apply deja la compuerta con exactamente estos filtros.
//
// # Todo va en UNA transacción
//
// Aplicar es vaciar la cadena y volver a escribirla entera, no parchear regla a
// regla. Entre el vaciado y la escritura hay una ventana donde el bloqueo no
// está, y esa ventana está en el cable. Con una sola llamada a `Flush` no
// existe: netlink publica el lote entero o no publica nada. Es la misma razón
// por la que la compuerta de Windows va dentro de una transacción de WFP.
//
// # El orden de las reglas ES el arbitraje
//
// nftables resuelve por PRIMERA COINCIDENCIA dentro de la cadena, así que los
// filtros se ordenan por peso descendente: los permisos, que pesan más, quedan
// por delante de los bloqueos. Poner los bloqueos primero es una compuerta que
// no abre el puerto del juego, y la sala no serviría para nada.
func (g *Gate) Apply(ctx context.Context, want []gate.Spec) error {
	_ = ctx // netlink no espera por nada que se pueda cancelar acá

	g.mu.Lock()
	defer g.mu.Unlock()

	if len(want) > gate.MaxFilters {
		return fmt.Errorf("la compuerta necesita %d reglas y el tope son %d",
			len(want), gate.MaxFilters)
	}

	c, err := nftables.New()
	if err != nil {
		return fmt.Errorf("abriendo netlink: %w", err)
	}
	defer c.CloseLasting() //nolint:errcheck // sin sesión persistente no hay nada que cerrar

	t, ch := tableOf()
	// Los dos son idempotentes: si ya están, el kernel no se queja.
	c.AddTable(t)
	c.AddChain(ch)
	c.FlushChain(ch)

	// El conntrack va PRIMERO, y sin él la sala no funciona.
	//
	// # Por qué esto existe acá y no en el modelo
	//
	// Windows no lo necesita, y no por suerte: la compuerta vive en
	// `ALE_AUTH_RECV_ACCEPT`, que es la capa donde se autoriza una conexión
	// entrante NUEVA. El tráfico de vuelta de una conexión que abrimos nosotros
	// ya quedó autorizado en `ALE_AUTH_CONNECT` y no vuelve a pasar por ahí.
	//
	// El hook `input` de nftables no distingue: ve TODOS los paquetes que entran,
	// incluidas las respuestas. Sin esta regla, el bloqueo por adaptador se come
	// la respuesta del host al invitado que acaba de marcar a su puerto de juego,
	// que es el caso central del producto.
	//
	// Descubierto corriendo la compuerta de verdad en el banco el 2026-08-10. La
	// diferencia no se ve leyendo el modelo, porque el modelo dice «denegar lo que
	// entre salvo esto» y las dos implementaciones lo cumplen; lo que cambia es
	// qué cuenta como «lo que entra».
	if err := addConntrack(c, t, ch); err != nil {
		return err
	}

	sets := &setBuilder{}
	orden := ordered(want)
	for _, s := range orden {
		r, err := ruleFor(t, ch, s, sets)
		if err != nil {
			// No se aplica NADA. El lote todavía no se publicó, así que salir acá
			// deja el sistema con la compuerta anterior intacta, que es el estado
			// consistente. Es la misma doctrina que la transacción de WFP.
			return fmt.Errorf("traduciendo el filtro %q: %w", s.Label, err)
		}
		if err := sets.flushInto(c); err != nil {
			return err
		}
		c.AddRule(r)
	}

	if err := c.Flush(); err != nil {
		return fmt.Errorf("publicando la compuerta: %w", err)
	}
	g.log.Info("compuerta puesta", "reglas", len(orden), "tabla", TableName)
	return nil
}

// ordered devuelve los filtros en el orden en que van a la cadena.
//
// Peso descendente, y a igual peso, posición ascendente. Lo segundo es lo que
// hace que dos cálculos con la misma entrada produzcan la misma cadena, que es
// lo que permite comparar lo puesto contra lo deseado sin ver cambios donde no
// los hay.
func ordered(want []gate.Spec) []gate.Spec {
	out := append([]gate.Spec(nil), want...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Weight != out[j].Weight {
			return out[i].Weight > out[j].Weight
		}
		return out[i].Slot < out[j].Slot
	})
	return out
}

// Purge se lleva la tabla entera.
//
// Borrar la tabla se lleva la cadena, las reglas y los conjuntos de una vez, y
// es lo que hace que la limpieza no tenga que enumerar nada ni recordar qué
// puso. Que la tabla no exista NO es un error: es el caso normal de un daemon
// que arranca por primera vez.
func (g *Gate) Purge(ctx context.Context) error {
	_ = ctx

	g.mu.Lock()
	defer g.mu.Unlock()

	c, err := nftables.New()
	if err != nil {
		return fmt.Errorf("abriendo netlink: %w", err)
	}

	t, err := ourTable(c)
	if err != nil {
		return err
	}
	if t == nil {
		return nil
	}

	c.DelTable(t)
	if err := c.Flush(); err != nil {
		return fmt.Errorf("barriendo la compuerta: %w", err)
	}
	g.log.Info("compuerta barrida", "tabla", TableName)
	return nil
}

// ourTable devuelve nuestra tabla si está puesta, y nil si no.
//
// Se pregunta al sistema en vez de recordar: un daemon que murió sucio dejó la
// tabla puesta y este proceso no sabe nada de ella.
func ourTable(c *nftables.Conn) (*nftables.Table, error) {
	tablas, err := c.ListTablesOfFamily(nftables.TableFamilyINet)
	if err != nil {
		return nil, fmt.Errorf("enumerando tablas: %w", err)
	}
	for _, t := range tablas {
		if t.Name == TableName {
			return t, nil
		}
	}
	return nil, nil
}

// Measure dice qué tiene puesto la compuerta AHORA.
//
// Mide y jamás juzga. Si eso es lo que se pidió lo decide
// [domain.Enforcement.Diff], que es dominio y corre sin sistema.
//
// # Se lee del kernel, no de un recuerdo
//
// Igual que en Windows: quien contesta si una regla está puesta es el sistema.
// Un recuerdo en memoria diría que sí después de que otro proceso vaciara la
// tabla, y ese verde falso es justo lo que esta medición existe para impedir.
func (g *Gate) Measure(ctx context.Context, want []gate.Spec) (gate.Measurement, error) {
	_ = ctx

	g.mu.Lock()
	defer g.mu.Unlock()

	out := gate.Measurement{Gate: domain.GateUnknown}

	c, err := nftables.New()
	if err != nil {
		return out, fmt.Errorf("abriendo netlink: %w", err)
	}

	t, err := ourTable(c)
	if err != nil {
		return out, err
	}
	if t == nil {
		// Sin tabla no hay compuerta, y eso es una respuesta, no un fallo.
		out.Gate = domain.GateAbsent
		return out, nil
	}

	_, ch := tableOf()
	ch.Table = t
	reglas, err := c.GetRules(t, ch)
	if err != nil {
		return out, fmt.Errorf("leyendo la cadena: %w", err)
	}

	puestas := map[int]bool{}
	for _, r := range reglas {
		if slot, ok := slotOf(r.UserData); ok {
			puestas[slot] = true
		}
	}

	// La compuerta está puesta si están sus BLOQUEOS. Preguntar por los permisos
	// no sirve para esto: con los permisos puestos y el bloqueo ausente la lista
	// vuelve a ser aditiva, que es exactamente lo que hay que detectar.
	//
	// El del vestíbulo solo se exige si se pidió: que falte cuando nadie lo pidió
	// no es un fallo, el invitado lo suelta al entrar. Y al revés, pedido y
	// ausente, contestar que está puesta sería pintar de verde media compuerta
	// justo en el adaptador donde llega gente que todavía no es miembro.
	out.Gate = domain.GatePresent
	for _, s := range want {
		if s.Action != gate.Block {
			continue
		}
		if !puestas[s.Slot] {
			out.Gate = domain.GateAbsent
			break
		}
	}

	// # Cada permiso se reporta en las DOS capas, y no es un duplicado
	//
	// En Windows son dos capas de verdad: las reglas del Firewall de Windows,
	// que son las visibles y las que el usuario audita con sus herramientas, y
	// los filtros espejo de WFP. Cada adaptador reporta la suya.
	//
	// En Linux la misma tabla cumple los dos papeles: es el filtro de paquetes
	// Y es lo que el operador ve con `nft list ruleset`. Reportar solo
	// [domain.LayerPacketFilter] no sería "más honesto", sería FALSO en un
	// sentido concreto y caro: [domain.Enforcement.Diff] cuenta únicamente las de
	// [domain.LayerFirewallRules] para decidir qué falta, así que toda regla
	// saldría como ausente, la pantalla diría que alguien tocó las reglas, y la
	// autorreparación las repondría en cada latido sobre un sistema que ya las
	// tenía puestas.
	//
	// Así que se emiten las dos, leídas del mismo sitio y del kernel. Es lo mismo
	// que pasa en Windows, donde un permiso también aparece dos veces porque de
	// verdad está en dos capas.
	for _, s := range want {
		if s.Rule == "" {
			continue
		}
		for _, capa := range [...]domain.EnforcementLayer{
			domain.LayerFirewallRules, domain.LayerPacketFilter,
		} {
			out.Rules = append(out.Rules, domain.AppliedRule{
				Name:    s.Rule,
				Layer:   capa,
				Enabled: puestas[s.Slot],
			})
		}
	}
	return out, nil
}

// IfaceOf traduce el nombre de un adaptador a su índice.
//
// Es lo que el cableado le inyecta a `firewall.New` como [firewall.IfaceResolver],
// y el gemelo de `wfp.LUIDOf`.
//
// Devuelve error y jamás cero: el índice cero no es "sin adaptador", es que
// `meta iifindex` no se emite, y una regla de bloqueo sin interfaz tira todo el
// tráfico de entrada de la máquina.
func IfaceOf(name string) (uint64, error) {
	if name == "" {
		return 0, errors.New("la compuerta necesita el NOMBRE del adaptador, y llegó vacío")
	}
	i, err := net.InterfaceByName(name)
	if err != nil {
		return 0, fmt.Errorf("no se encontró el adaptador %s: %w", name, err)
	}
	if i.Index <= 0 {
		return 0, fmt.Errorf("el adaptador %s dio índice %d, y el cero no acota nada",
			name, i.Index)
	}
	return uint64(i.Index), nil
}
