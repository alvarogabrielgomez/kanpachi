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

// El vestíbulo vive en un /24 FIJO, el último del espacio compartido.
//
// Fijo y no negociado porque los dos lados tienen que llegar al mismo sin
// hablarse: el invitado necesita una dirección conocida a la que marcar ANTES
// de tener nada del host, y la subred de la sala llega dentro de la
// credencial, o sea después. Elegirlo al azar exigiría un canal para
// comunicarlo, y ese canal es justamente el que se está montando.
//
// Que sea el mismo para todas las salas no filtra nada: el vestíbulo ya es
// público por definición, su red la deriva cualquiera que tenga el invite ID,
// y una dirección IP dentro de un overlay cifrado no dice de qué sala es.
//
// Es momentáneo. Se entra, se pide la credencial y se sale.
var (
	RendezvousSubnet      = netip.MustParsePrefix("100.127.255.0/24")
	RendezvousHostAddress = netip.MustParseAddr("100.127.255.1")
)

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
type AddressPlan struct {
	Subnet netip.Prefix
	Reason string

	// LobbyConflict es un prefijo local que pisa el /24 del vestíbulo. El cero
	// significa que no hay ninguno.
	//
	// Es el hueco que la subred fija del vestíbulo deja abierto y no se puede
	// cerrar: el /24 tiene que ser el mismo en las dos máquinas para que el que
	// entra sepa a dónde marcar, así que no hay forma de esquivar el de esta.
	// Requiere que el router del usuario reparta exactamente 100.127.255.0/24,
	// que es raro y no imposible.
	//
	// Lo que sí se puede hacer es DECIRLO. Un "entrar a salas ajenas no
	// funciona" sin explicación es indiagnosticable; con esto, el diagnóstico
	// lo dice en un renglón. Crear una sala propia sigue funcionando, porque el
	// vestíbulo del host solo hace falta cuando alguien está entrando.
	LobbyConflict netip.Prefix
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
	lobby, _ := firstOverlap(local, RendezvousSubnet)

	if p, occupied := firstOverlap(local, SharedSpace); occupied {
		sub, err := pickSubnet(FallbackSpace, local, r)
		if err != nil {
			return AddressPlan{}, err
		}
		return AddressPlan{
			Subnet:        sub,
			Reason:        fmt.Sprintf("esta máquina ya usa %s dentro del espacio compartido, la sala va en %s", p, FallbackSpace),
			LobbyConflict: lobby,
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
			Subnet:        sub,
			Reason:        fmt.Sprintf("no quedaba un /24 libre en %s, la sala va en %s", SharedSpace, FallbackSpace),
			LobbyConflict: lobby,
		}, nil
	}
	return AddressPlan{
		Subnet:        sub,
		Reason:        "sin conflictos, la sala va en el espacio compartido",
		LobbyConflict: lobby,
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

		// El /24 del vestíbulo nunca se entrega a una sala. Si coincidieran,
		// entrar a la sala rompería la conexión que se está usando para pedir
		// la credencial, y el fallo aparecería una vez de cada dieciséis mil.
		if candidate == RendezvousSubnet {
			continue
		}
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
