package domain

import (
	"net/netip"
	"strings"
)

// GameHealth dice si hay algo escuchando en los puertos del juego activo.
//
// # Por qué lo mide el HOST y no lo sondea el invitado
//
// Porque desde fuera no se puede. El sondeo de puertos toca TCP: abre y mira
// si contesta. En UDP no hay handshake, así que un puerto sin nadie detrás y
// uno con el servidor del juego contestan lo mismo, que es nada — y el rebote
// que lo distinguiría, el ICMP de puerto inalcanzable, lo tapa la compuerta de
// la máquina sondeada. Un semáforo UDP sondeado desde fuera pintaría rojo con
// el servidor corriendo, que es peor que no tener semáforo.
//
// El host, en cambio, no adivina: lee su propia tabla de sockets y ve si algo
// está atado a esos puertos. Eso viaja en el anuncio de la sala, y por eso es
// un dato de PRESENTACIÓN y nada más: un host que lo mienta consigue que a los
// demás les aparezca un punto de otro color, jamás que se abra o se cierre un
// puerto en la máquina de nadie. Ver [RoomAnnounce], que lleva la misma regla
// para el id del juego.
type GameHealth uint8

const (
	// GameHealthUnknown es no haber podido mirar, y también no haber juego que
	// mirar. Es el cero a propósito: lo que no se midió no se pinta.
	GameHealthUnknown GameHealth = iota
	// GameHealthListening es que algo está atado a los puertos del juego.
	GameHealthListening
	// GameHealthSilent es que se miró y no hay nadie: el juego está elegido y
	// su servidor no está levantado.
	GameHealthSilent
	// GameHealthElsewhere es el caso que costó una tarde encontrar: el servidor
	// del juego ESTÁ levantado, y atado a otra dirección de esta misma máquina,
	// así que lo que llega por la sala golpea un puerto sin nadie detrás.
	//
	// Es un estado aparte y no un silencio porque el arreglo es otro: acá no
	// hay que arrancar nada, hay que atarlo a `0.0.0.0`. Medido el 2026-08-19
	// contra un despliegue de Kubernetes que le pasaba al servidor la IP del
	// pod: túnel perfecto, puertos abiertos, y el host contestando "udp port
	// 16261 unreachable" a toda la sala.
	GameHealthElsewhere
)

func (h GameHealth) String() string {
	switch h {
	case GameHealthListening:
		return "listening"
	case GameHealthSilent:
		return "silent"
	case GameHealthElsewhere:
		return "elsewhere"
	default:
		return "unknown"
	}
}

// GameReach es dónde se puede alcanzar el juego activo, y no solo si se puede.
//
// La dirección importa por dos motivos distintos: es lo que hace que la pantalla
// pueda NOMBRAR el arreglo en vez de pintar un ámbar mudo, y es hacia dónde
// redirige el modo contenedor. Ver [RedirectSpec].
type GameReach struct {
	Health GameHealth

	// Where es la dirección local donde el juego SÍ escucha. Solo se llena con
	// [GameHealthElsewhere], y solo cuando hay UNA.
	//
	// Con varias queda inválida a propósito: elegir una sería adivinar hacia
	// dónde mandar el tráfico de la sala, y de esa adivinanza cuelga un redirect
	// que puede acabar en un servicio que no es el juego. Sin dirección, la
	// pantalla dice lo que sabe y no se redirige nada.
	Where netip.Addr
}

// GameReachOf compara los puertos que el perfil abre con lo que la máquina
// tiene atado, y dice DÓNDE.
//
// **Basta UNO.** Un perfil abre lo que el juego PUEDE usar, y un servidor sano
// no tiene por qué usarlo todo: Zomboid declara 16261-16262 y las versiones
// desde la 41.65 se apañan con el primero. Exigir los dos pintaría rojo un
// servidor que funciona, que es la forma de que nadie vuelva a mirar el punto.
//
// Alcanzable es escuchar en TODAS las interfaces o en la dirección de la sala.
// Lo primero con el mismo criterio que [ObservedRanges]; lo segundo porque atar
// el juego a la IP de Kanpachi es lo que este producto recomienda, y sería
// absurdo llamar enfermo a quien hizo caso.
//
// Lo atado a CUALQUIER OTRA dirección de la máquina es [GameHealthElsewhere]:
// el servidor corre y no lo alcanza nadie de la sala. `127.0.0.1` cuenta como
// otra dirección, que es lo que es.
//
// Sin puertos que mirar la respuesta es [GameHealthUnknown] y no "silencio":
// una sala sin juego no tiene nada roto.
func GameReachOf(ports []PortRange, listeners []Listener, roomIP netip.Addr) GameReach {
	if len(ports) == 0 {
		return GameReach{}
	}

	otras := make([]netip.Addr, 0, 2)
	for _, l := range listeners {
		if l.Port == 0 || !matchesAny(l, ports) {
			continue
		}
		if listensEverywhere(l.Address) {
			return GameReach{Health: GameHealthListening}
		}
		dir, err := netip.ParseAddr(strings.TrimSpace(l.Address))
		if err != nil {
			continue
		}
		if roomIP.IsValid() && dir == roomIP {
			return GameReach{Health: GameHealthListening}
		}
		if !contiene(otras, dir) {
			otras = append(otras, dir)
		}
	}

	switch len(otras) {
	case 0:
		return GameReach{Health: GameHealthSilent}
	case 1:
		return GameReach{Health: GameHealthElsewhere, Where: otras[0]}
	default:
		// Varias direcciones y ninguna es la de la sala. Se dice que escucha en
		// otro sitio y no CUÁL: ver [GameReach.Where].
		return GameReach{Health: GameHealthElsewhere}
	}
}

// RedirectSpec es el desvío de lo que llega por la sala hacia donde el juego
// escucha de verdad.
//
// # Por qué existe, y por qué NO es para cualquier máquina
//
// En el sidecar de un contenedor la intención no es ambigua: ese contenedor
// declaró un juego, comparte el espacio de red de Kanpachi y existe para servir
// esa sala. No hay red de casa que proteger. Ahí, un servidor atado a la IP del
// contenedor deja la sala muerta sin que nadie pueda notarlo, y desviar es
// hacer lo que el operador pidió.
//
// En un escritorio la misma automatización sería lo contrario: atar un servicio
// a una dirección concreta es lo que hace alguien para que NO lo alcancen desde
// otro sitio, y desviarlo por su cuenta rompería esa decisión. Fuera de
// contenedor esto no se emite, y lo que queda es la pantalla diciendo dónde
// escucha. Ver [GameHealthElsewhere].
//
// La bandera que lo enciende la pone el entrypoint de la IMAGEN, así que la
// reciben también las plantillas que corren con `network_mode: host`, donde
// Kanpachi comparte la pila de la máquina de verdad. Se acepta a propósito y
// no se condiciona: el porqué está en la decisión 40 de los docs.
//
// # Lo que acota
//
// Solo los puertos que DECLARA el perfil activo, los mismos que ya abre la capa
// de permisos: desviar no puede alcanzar un puerto que la sala no alcanzaba.
type RedirectSpec struct {
	// Adapter es el adaptador de la sala. Lo que entra por otro no se toca.
	Adapter string
	// RoomIP es la dirección de esta máquina EN la sala, o sea el destino que
	// los miembros escriben y que hoy no tiene a nadie escuchando.
	RoomIP netip.Addr
	// To es dónde escucha el juego, tal como se midió en la tabla de sockets.
	To netip.Addr
	// Ports son los del perfil activo.
	Ports []PortRange
}

// Understood dice si el desvío está completo. Un campo a medias no se emite:
// media regla de nat es una regla que manda tráfico a cualquier parte.
//
// **El loopback SÍ vale de destino, y hasta el 2026-08-26 no.** La negativa
// venía de un hecho cierto a medias: el núcleo tira como marciano un paquete
// que llega por un adaptador y va dirigido a `127.0.0.0/8`, **salvo que
// `route_localnet` esté puesto en esa interfaz**. Esa segunda mitad estaba
// escrita acá y sin usar, así que la regla no se armaba y el ámbar se quedaba
// pidiéndole a una persona que reatara su servidor.
//
// Medido en un netns limpio, con esta misma forma de regla, un oyente UDP en
// `127.0.1.1` y el tráfico entrando por otro adaptador:
//
//	route_localnet=0  bytes entregados: 0
//	route_localnet=1  bytes entregados: 4
//
// La regla casó las dos veces; lo único que cambió fue el sysctl. Ponerlo es
// del adaptador y sobre la interfaz de Kanpachi, que es suya: el dominio no
// sabe de sysctls y no tiene por qué.
//
// El caso llega solo, y no es de laboratorio: un contenedor con `hostNetwork`
// hace que `hostname -i` conteste el loopback del nodo, así que el servidor
// del juego ata ahí. La alternativa era escribir a mano la dirección de la
// sala en el manifiesto, y esa se SORTEA al crearla.
func (r RedirectSpec) Understood() bool {
	return r.Adapter != "" && r.RoomIP.IsValid() && r.To.IsValid() &&
		r.To != r.RoomIP && len(r.Ports) > 0
}

// matchesAny dice si un oyente cae dentro de alguno de los rangos del perfil.
//
// **[ProtoBoth] casa con los dos, y esa rama es el arreglo.** Un oyente es tcp
// o udp y NUNCA both, así que comparar por igualdad dejaba un rango `both` sin
// casar con nada: la salud salía [GameHealthSilent] con el juego escuchando de
// verdad, y en contenedor el desvío tampoco se ponía, porque cuelga de esta
// misma medición. Medido el 2026-08-20 con Project Zomboid escuchando en
// 16261 y 16262 mientras la sala decía "nothing listening". Afectaba también a
// los tres perfiles del catálogo que ya declaraban `both`: Age of Empires II,
// Stardew Valley y 7 Days to Die.
//
// Es coherente con lo que `both` significa en el resto del dominio: azúcar de
// escritura que [BuildRuleSet] expande a una regla por protocolo. Si el rango
// describe las dos reglas, describe también a los oyentes de las dos.
func matchesAny(l Listener, ports []PortRange) bool {
	for _, r := range ports {
		if l.Port < r.From || l.Port > r.To {
			continue
		}
		if r.Proto == ProtoBoth || l.Proto == r.Proto {
			return true
		}
	}
	return false
}

func contiene(list []netip.Addr, a netip.Addr) bool {
	for _, x := range list {
		if x == a {
			return true
		}
	}
	return false
}
