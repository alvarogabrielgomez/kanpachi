package domain

import (
	"errors"
	"fmt"
	"strings"
)

// DefaultSeedHost es la semilla que usa un invite ID pelado.
//
// Un ID sin host SIEMPRE usa esta, jamás la última que se usó. Recordar la
// última tiene una trampa real: tras entrar una vez a un seed ajeno, un ID
// pelado de otro amigo fallaría sin explicación posible.
const DefaultSeedHost = "kanpachi.accentio.dev"

// MaxInputLen es el tope de longitud que se aplica ANTES de parsear.
//
// El manejador kanpachi:// queda expuesto a toda la web, así que lo que entra
// por ahí es hostil por definición. Un tope temprano evita que una entrada de
// megabytes llegue siquiera a los bucles de normalización. 300 cubre con
// holgura la forma más larga: esquema, host de hasta 253, separador, invite ID
// y la clave de tarjeta en el fragmento.
const MaxInputLen = 300

const maxHostLen = 253

var (
	ErrInputTooLong = fmt.Errorf("la entrada pasa de %d caracteres", MaxInputLen)
	ErrInputShape   = errors.New("eso no tiene forma de código de Kanpachi")
	ErrSeedHost     = errors.New("el servidor del código no es un nombre válido")
)

// Room es un invite ID con el seed por el que se lo alcanza.
//
// **Un invite ID solo significa algo en el seed que lo emitió.** No es global,
// es local a un registro: el mismo ID en dos seeds son dos salas distintas que
// no se conocen. Por eso el seed viaja pegado y no es decoración.
type Room struct {
	InviteID InviteID
	Seed     string // host del seed, ya normalizado. Nunca vacío tras ParseRoom
}

// ParseRoom acepta las seis formas documentadas y devuelve siempre lo mismo.
//
//	A7K2M9QX                                  → seed por defecto
//	a7k2-m9qx                                 → seed por defecto
//	kanpachi://A7K2M9QX                       → seed por defecto
//	A7K2M9QX@seed.midominio.com               → ese seed
//	kanpachi.accentio.dev/A7K2M9QX            → ese seed
//	https://kanpachi.accentio.dev/A7K2M9QX    → ese seed
//
// El usuario pega lo que le llegó por Telegram y funciona, sin tener que saber
// cuál de las seis es la correcta.
//
// Es la frontera de entrada hostil del producto. No interpreta rutas, no
// acepta argumentos, no sigue redirecciones y no adivina: cualquier cosa que
// no calce exactamente con una de las seis formas se rechaza entera.
func ParseRoom(input string) (Room, error) {
	// El tope va primero, antes de tocar el contenido.
	if len(input) > MaxInputLen {
		return Room{}, fmt.Errorf("%w, llegaron %d", ErrInputTooLong, len(input))
	}
	s := strings.TrimSpace(input)
	if s == "" {
		return Room{}, ErrInputShape
	}

	// El fragmento se descarta ANTES de mirar la forma, y no dentro de una de
	// las ramas. Lleva la clave con que la PÁGINA descifra la tarjeta de
	// presentación: a la app no le sirve para nada, el nombre de la sala lo
	// recibe del host por el canal de control. Recortarlo solo en la forma con
	// barra hacía que `kanpachi://A7K2M9QX#clave`, que es exactamente lo que
	// produce el botón de la página, se rechazara por tener un carácter que no
	// existe en el alfabeto.
	if i := strings.IndexByte(s, '#'); i >= 0 {
		s = s[:i]
	}

	// Los esquemas se quitan por delante. Se comparan en minúsculas porque un
	// esquema es insensible a mayúsculas.
	for _, scheme := range []string{"kanpachi://", "https://", "http://"} {
		if len(s) >= len(scheme) && strings.EqualFold(s[:len(scheme)], scheme) {
			s = s[len(scheme):]
			break
		}
	}

	hasAt := strings.Contains(s, "@")
	hasSlash := strings.Contains(s, "/")

	// Las dos formas con host son distintas y excluyentes. Traer las dos
	// marcas a la vez no es ninguna de las seis, así que se descarta en vez de
	// intentar entenderlo.
	if hasAt && hasSlash {
		return Room{}, ErrInputShape
	}

	var idPart, hostPart string
	switch {
	case hasAt:
		// inviteID@host
		parts := strings.Split(s, "@")
		if len(parts) != 2 {
			return Room{}, ErrInputShape
		}
		idPart, hostPart = parts[0], parts[1]
		// Un @ escrito a propósito con nada detrás es una entrada rota, no una
		// petición del seed por defecto. Caer al default acá haría que un ID
		// truncado a mitad de copiar conecte en silencio a otra sala.
		if hostPart == "" {
			return Room{}, ErrSeedHost
		}

	case hasSlash:
		// host/inviteID, con el fragmento de la clave de tarjeta opcional
		parts := strings.SplitN(s, "/", 2)
		hostPart, idPart = parts[0], parts[1]
		// Una barra de más significa una ruta, y por este canal no entran
		// rutas. Ver el modelo de amenaza del manejador de protocolo.
		if strings.ContainsAny(idPart, "/?&") {
			return Room{}, ErrInputShape
		}

	default:
		idPart = s
	}

	id, err := ParseInviteID(idPart)
	if err != nil {
		return Room{}, err
	}

	seed := DefaultSeedHost
	if hostPart != "" {
		seed, err = parseSeedHost(hostPart)
		if err != nil {
			return Room{}, err
		}
	}
	return Room{InviteID: id, Seed: seed}, nil
}

// InviteURL es la forma que la app GENERA, con el invite ID en la ruta.
//
// En la ruta y no en el fragmento, y eso cambió respecto del diseño anterior.
// El motivo de esconderlo era que el servidor no lo recibiera, y ese argumento
// se apoyaba en que el código era el secreto de la red. Con la decisión 2 dejó
// de serlo, y el registro del seed tiene que conocerlo igual para resolverlo.
// En la ruta la página se renderiza en el servidor, el chat muestra vista
// previa, y la URL se dicta por teléfono sin explicar qué es un `#`.
//
// No incluye la clave de la tarjeta. Esa la agrega quien arma el enlace de
// invitación, que es el único que la tiene. Ver decisión 17.
func (r Room) InviteURL() string {
	return r.Seed + "/" + r.InviteID.String()
}

// UsesDefaultSeed dice si la sala va por la semilla de Accentio. La UI resalta
// el caso contrario, porque conectarse al servidor de un desconocido merece
// verse.
func (r Room) UsesDefaultSeed() bool { return r.Seed == DefaultSeedHost }

// parseSeedHost valida un nombre de host de forma conservadora.
//
// Sin puerto a propósito: ninguna de las seis formas documentadas lo lleva, el
// seed siempre escucha en 11010, y aceptarlo sería superficie de parseo a
// cambio de nada.
func parseSeedHost(h string) (string, error) {
	h = strings.ToLower(strings.TrimSuffix(h, "."))
	if h == "" || len(h) > maxHostLen {
		return "", ErrSeedHost
	}
	// Se exige un punto para que un ID mal pegado no termine interpretándose
	// como un host de una sola etiqueta.
	if !strings.Contains(h, ".") {
		return "", ErrSeedHost
	}
	for _, label := range strings.Split(h, ".") {
		if label == "" || len(label) > 63 {
			return "", ErrSeedHost
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
			return "", ErrSeedHost
		}
		for i := 0; i < len(label); i++ {
			c := label[i]
			isDigit := c >= '0' && c <= '9'
			isLower := c >= 'a' && c <= 'z'
			if !isDigit && !isLower && c != '-' {
				return "", ErrSeedHost
			}
		}
	}
	return h, nil
}
