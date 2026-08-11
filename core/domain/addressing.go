package domain

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/netip"
)

// Los dos espacios donde puede vivir una sala.
//
// SharedSpace es 100.64.0.0/10, el espacio compartido de RFC 6598, el mismo
// que usa Tailscale y por la misma razón: no choca con 10.0.0.0/8 ni con
// 192.168.0.0/16, que es lo que hay en las casas.
//
// FallbackSpace es la salida de emergencia. Existe porque el espacio
// compartido es justo el que los ISP usan para CGNAT, y CGNAT es dominante en
// América Latina, que es donde vive el grupo. La solución de Tailscale para
// este conflicto es apagar IPv4 en la tailnet, y acá es inviable: el
// descubrimiento LAN y el netcode viejo de los juegos son IPv4.
var (
	SharedSpace   = netip.MustParsePrefix("100.64.0.0/10")
	FallbackSpace = netip.MustParsePrefix("10.99.0.0/16")
)

// RoomPrefixBits es el tamaño de la subred de una sala. Un /24 da 254
// direcciones para un grupo de amigos, que sobra por dos órdenes de magnitud,
// y mantiene el alcance de las reglas de firewall en algo que se lee de un
// vistazo.
const RoomPrefixBits = 24

// LobbySpace es donde viven los vestíbulos, y deliberadamente NO es CGNAT.
//
// # Por qué se mudó de 100.127.255.0/24, que era donde estaba
//
// Porque aquel era un /24 fijo dentro de 100.64.0.0/10, y ese espacio tiene dos
// ocupantes enormes que lo hacen mal sitio para algo que no se puede mover:
//
//   - **Los ISP.** CGNAT es dominante en América Latina, que es donde vive el
//     grupo. Medido el 2026-08-11: un invitado en Venezuela se quedó colgado
//     esperando a que su adaptador tomara 100.127.255.102 mientras otro en
//     Brasil entraba sin nada.
//   - **Tailscale.** Reparte las IP de sus nodos por TODO el /10 y solo reserva
//     100.100.0.0/24, 100.100.100.0/24 y 100.115.92.0/23 para sí misma, o sea
//     que nada le impide asignarle a un nodo una dirección dentro del /24 que
//     el vestíbulo tenía fijo.
//
// Y el remedio de Tailscale para su propio conflicto no sirve acá. Su
// documentación ofrece uno solo, apagar IPv4 y quedarse en IPv6, y este producto
// no puede: el descubrimiento LAN y el netcode viejo de los juegos son IPv4.
//
// # Por qué esta mitad y no la otra
//
// 198.18.0.0/15 es de RFC 2544, reservado para bancos de pruebas: no se enruta
// en internet y las empresas no lo usan. La mitad baja, 198.18.0.0/16, es el
// rango por defecto del modo fake-ip de Clash y sing-box, que es software de
// proxy muy usado justamente donde más falta hace esto. Así que se usa la alta.
//
// **Elegir bien el rango no es el arreglo.** No hay forma de saber qué tiene
// cada máquina, y dar por buena una suposición es exactamente el error que se
// está corrigiendo. Lo que arregla es que el vestíbulo sea MOVIBLE: ver
// [Rendezvous.LobbySubnet].
var LobbySpace = netip.MustParsePrefix("198.19.0.0/16")

// ControlPort es donde escucha el canal de la sala de la decisión 23, en la
// interfaz virtual y en ninguna otra.
//
// Fijo y no negociado por lo mismo que el /24 del vestíbulo: quien entra tiene
// que llegar sin haber hablado antes con nadie, y el canal por el que se
// negociaría un puerto es justamente el que se está montando.
//
// **Solo el host escucha acá.** Los invitados marcan hacia afuera y no abren
// nada, así que su deny-all queda literalmente intacto.
//
// Del rango privado, y sobre un adaptador dedicado donde no compite con nada de
// la máquina. No es un puerto del router y no se mapea en ningún lado: vive
// dentro del overlay cifrado.
const ControlPort = 57623

// ErrNoSubnet es que ninguno de los dos espacios tiene un /24 libre.
//
// Es prácticamente imposible: exigiría que la máquina ya tuviera rutas
// cubriendo 100.64.0.0/10 entero y 10.99.0.0/16 entero. Se devuelve como error
// y no se fuerza un rango igual, porque instalar una subred que pisa una ruta
// existente rompe la conectividad que el usuario ya tenía, y eso es peor que
// no poder crear la sala.
var ErrNoSubnet = errors.New("no hay ningún /24 libre en los dos espacios de direcciones")

// AddressPlan es el /24 elegido con el motivo por el que se eligió.
//
// El motivo viaja hasta Diagnostics para que un conflicto de rango sea
// diagnosticable en un renglón, en vez de que alguien tenga que deducir por
// qué su sala está en 10.99 mirando la tabla de rutas.
//
// Ya NO lleva nada del vestíbulo, y esa ausencia es el resultado de haberlo
// sacado de CGNAT: mientras vivía dentro del espacio de las salas, elegir el /24
// de una sala y comprobar el del vestíbulo eran la misma consulta a la misma
// tabla. Ahora son sitios distintos, la comprobación del vestíbulo depende del
// código de la sala a la que se entra, y quien la necesita es el invitado. Vive
// en [LobbyOverlap], que es a quien se le puede pasar el /24 correcto.
type AddressPlan struct {
	Subnet netip.Prefix
	Reason string
}

// LobbyOverlap devuelve el prefijo local que le puede GANAR al vestíbulo de
// ESTA sala, o el cero si no hay ninguno.
//
// Recibe el /24 porque ya no hay uno solo: lo deriva cada sala de su código. Ver
// [Rendezvous.LobbySubnet].
//
// Es el invitado quien lo necesita, y esa asimetría no es casual: el host que
// tiene un conflicto abre su sala igual, porque su vestíbulo solo hace falta
// cuando alguien está entrando. Quien no puede entrar es el otro.
//
// # Por qué no vale con que se solapen, que fue el primer intento
//
// Porque solaparse no es competir. El reenvío elige por prefijo más largo, y el
// del vestíbulo es un /24: cualquier ruta MÁS CORTA que lo contenga pierde
// contra él y no rompe nada.
//
// Eso no es teórico y tiene un caso enorme: **Tailscale instala una ruta a
// 100.64.0.0/10 entera en cada nodo**. Cuando el vestíbulo vivía ahí dentro,
// contar el solape a secas marcaba conflicto en toda máquina con Tailscale
// puesto, que es justo la máquina donde está medido que entrar funciona.
//
// Así que solo cuentan los prefijos de /24 o más largos: los que empatan con el
// del vestíbulo o le ganan. Ahí entran los tres casos que sí rompen, un rango
// ajeno que reparta exactamente este /24, una dirección de esta máquina dentro
// de él, y una ruta más específica que lo parta.
func LobbyOverlap(local []netip.Prefix, lobby netip.Prefix) netip.Prefix {
	for _, p := range local {
		if !p.IsValid() || !p.Addr().Is4() {
			continue
		}
		if p.Bits() >= RoomPrefixBits && p.Overlaps(lobby) {
			return p
		}
	}
	return netip.Prefix{}
}

// PlanAddresses elige la subred de la sala esquivando lo que ya existe en la
// máquina.
//
// `local` son las rutas y las direcciones de TODOS los adaptadores, tal como
// las devuelve [port.RoutingTable]. Se consultan al crear o al unirse a una
// sala, no al instalar: la LAN de una laptop cambia entre la casa y la
// oficina, y un rango elegido en la instalación sería correcto solo el primer
// día.
//
// La regla que gobierna el resultado, y que no aparece en el tipo porque es
// una ausencia: NUNCA se instala una regla que descarte 100.64.0.0/10 entero.
// Ese es exactamente el error que rompe la conectividad de quien está detrás
// de CGNAT. El alcance es siempre el /24 de la sala.
func PlanAddresses(local []netip.Prefix, r io.Reader) (AddressPlan, error) {
	// El caso que fuerza la salida de emergencia no es un choque con el /24
	// que se vaya a elegir, es que la máquina YA viva en el espacio
	// compartido: si la LAN de casa reparte 100.64.x.x, cualquier /24 de ese
	// espacio es una ruta que compite con la del router del usuario, aunque
	// hoy no se solapen. El caso común, con el router en 100.64.x.x del lado
	// WAN y la LAN en 192.168.x.x, no aparece en la tabla del PC y no dispara
	// nada de esto.
	if p, occupied := firstOverlap(local, SharedSpace); occupied {
		sub, err := pickSubnet(FallbackSpace, local, r)
		if err != nil {
			return AddressPlan{}, err
		}
		return AddressPlan{
			Subnet: sub,
			Reason: fmt.Sprintf("esta máquina ya usa %s dentro del espacio compartido, la sala va en %s", p, FallbackSpace),
		}, nil
	}

	sub, err := pickSubnet(SharedSpace, local, r)
	if err != nil {
		// Que el espacio compartido esté lleno sin que ninguna ruta lo solape
		// es contradictorio, así que llegar acá significa un `local` raro. Se
		// intenta la reserva igual antes de rendirse.
		sub, err = pickSubnet(FallbackSpace, local, r)
		if err != nil {
			return AddressPlan{}, err
		}
		return AddressPlan{
			Subnet: sub,
			Reason: fmt.Sprintf("no quedaba un /24 libre en %s, la sala va en %s", SharedSpace, FallbackSpace),
		}, nil
	}
	return AddressPlan{
		Subnet: sub,
		Reason: "sin conflictos, la sala va en el espacio compartido",
	}, nil
}

// pickSubnet elige un /24 dentro de space que no se solape con nada local.
//
// Arranca en un punto aleatorio y avanza en círculo. Aleatorio para que dos
// salas de la misma persona no caigan siempre en el mismo /24, que haría que
// una sala vieja mal cerrada en otra máquina colisionara con la nueva. En
// círculo, y no reintentando al azar, para que el resultado sea "no hay
// ninguno libre" en vez de "no encontré ninguno en veinte intentos".
func pickSubnet(space netip.Prefix, local []netip.Prefix, r io.Reader) (netip.Prefix, error) {
	// Un espacio más chico que un /24 no contiene ninguno, y el cálculo de
	// abajo desplazaría un número negativo, que en Go es un panic. Los dos
	// espacios del producto son un /10 y un /16, así que llegar acá significa
	// que alguien agregó un tercero mal medido, y vale más un error que un
	// servicio que se muere al crear una sala.
	if !space.Addr().Is4() || space.Bits() > RoomPrefixBits {
		return netip.Prefix{}, fmt.Errorf("%w: %s no contiene ningún /%d", ErrNoSubnet, space, RoomPrefixBits)
	}
	spaceBase := space.Addr().As4()
	base := binary.BigEndian.Uint32(spaceBase[:])
	count := uint32(1) << (RoomPrefixBits - space.Bits()) // cuántos /24 caben

	start, err := randomIndex(count, r)
	if err != nil {
		return netip.Prefix{}, err
	}

	for i := uint32(0); i < count; i++ {
		idx := (start + i) % count
		var b [4]byte
		binary.BigEndian.PutUint32(b[:], base+idx*256)
		candidate := netip.PrefixFrom(netip.AddrFrom4(b), RoomPrefixBits)

		// Antes acá se saltaba el /24 del vestíbulo, porque vivía dentro de este
		// mismo espacio y entregárselo a una sala habría roto la conexión por la
		// que se estaba pidiendo la credencial. Ya no hace falta: [LobbySpace]
		// está fuera de los dos espacios de salas, así que no hay coincidencia
		// posible. Es la simplificación que compró mudarlo.
		if _, hit := firstOverlap(local, candidate); !hit {
			return candidate, nil
		}
	}
	return netip.Prefix{}, fmt.Errorf("%w: %s está entero ocupado", ErrNoSubnet, space)
}

// randomIndex devuelve un índice uniforme en [0, count).
//
// Con rechazo de muestras, no con un módulo pelado: count es potencia de dos
// en los dos espacios que usamos, así que hoy el módulo sería uniforme igual,
// y dejarlo así ataría la corrección de esta función a que nadie cambie nunca
// SharedSpace por un prefijo que no lo sea.
func randomIndex(count uint32, r io.Reader) (uint32, error) {
	if count == 0 {
		return 0, ErrNoSubnet
	}
	limit := (1 << 32) - ((1 << 32) % uint64(count))
	var buf [4]byte
	for tries := 0; tries < 64; tries++ {
		if _, err := io.ReadFull(r, buf[:]); err != nil {
			return 0, fmt.Errorf("domain: leyendo aleatoriedad para la subred: %w", err)
		}
		v := binary.BigEndian.Uint32(buf[:])
		if uint64(v) < limit {
			return v % count, nil
		}
	}
	// Sesenta y cuatro rechazos seguidos con un rechazo de probabilidad menor
	// a la mitad significa que el lector no es aleatorio. Vale más fallar que
	// producir una subred predecible en silencio.
	return 0, errors.New("domain: la fuente de aleatoriedad no rinde valores utilizables")
}

// firstOverlap devuelve el primer prefijo local que se solapa con want.
//
// Solaparse no es contenerse: 192.168.0.0/16 y 192.168.1.0/24 no se contienen
// en la dirección que uno espera, y las dos se pisan. Se comprueban las dos
// direcciones por eso.
func firstOverlap(local []netip.Prefix, want netip.Prefix) (netip.Prefix, bool) {
	for _, p := range local {
		if !p.IsValid() || !p.Addr().Is4() {
			continue
		}
		if p.Overlaps(want) {
			return p, true
		}
	}
	return netip.Prefix{}, false
}

// HostAddress devuelve la dirección .1 de la subred, que es la que toma quien
// hospeda. Es convención y no requisito del motor: sirve para que la IP del
// host se reconozca de un vistazo en la lista de miembros y al escribirla en
// el juego.
func HostAddress(subnet netip.Prefix) netip.Addr {
	return subnet.Addr().Next()
}
