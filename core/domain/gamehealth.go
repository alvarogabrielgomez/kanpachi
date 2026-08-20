package domain

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
)

func (h GameHealth) String() string {
	switch h {
	case GameHealthListening:
		return "listening"
	case GameHealthSilent:
		return "silent"
	default:
		return "unknown"
	}
}

// GameHealthOf compara los puertos que el perfil abre con lo que la máquina
// tiene atado.
//
// **Basta UNO.** Un perfil abre lo que el juego PUEDE usar, y un servidor sano
// no tiene por qué usarlo todo: Zomboid declara 16261-16262 y las versiones
// desde la 41.65 se apañan con el primero. Exigir los dos pintaría rojo un
// servidor que funciona, que es la forma de que nadie vuelva a mirar el punto.
//
// Solo cuenta lo que escucha en TODAS las interfaces, con el mismo criterio que
// [ObservedRanges]: un socket atado a 127.0.0.1 no lo alcanza nadie de la sala,
// así que para esta pregunta es lo mismo que no existir.
//
// Sin puertos que mirar la respuesta es [GameHealthUnknown] y no "silencio":
// una sala sin juego no tiene nada roto.
func GameHealthOf(ports []PortRange, listeners []Listener) GameHealth {
	if len(ports) == 0 {
		return GameHealthUnknown
	}
	for _, l := range listeners {
		if l.Port == 0 || !listensEverywhere(l.Address) {
			continue
		}
		for _, r := range ports {
			if l.Proto == r.Proto && l.Port >= r.From && l.Port <= r.To {
				return GameHealthListening
			}
		}
	}
	return GameHealthSilent
}
