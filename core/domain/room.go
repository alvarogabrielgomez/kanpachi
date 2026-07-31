package domain

import (
	"errors"
	"fmt"
	"strings"
)

// DefaultSeedHost es la semilla que usa un código pelado.
//
// Un código sin host SIEMPRE usa esta, jamás la última que se usó. Recordar la
// última tiene una trampa real: tras entrar una vez a un seed ajeno, un código
// pelado de otro amigo fallaría sin explicación posible.
const DefaultSeedHost = "kanpachi.accentio.dev"

// MaxInputLen es el tope de longitud que se aplica ANTES de parsear.
//
// El manejador kanpachi:// queda expuesto a toda la web, así que lo que entra
// por ahí es hostil por definición. Un tope temprano evita que una entrada de
// megabytes llegue siquiera a los bucles de normalización. 300 cubre con
// holgura la forma más larga: esquema, host de hasta 253, separador y código.
const MaxInputLen = 300

const maxHostLen = 253

var (
	ErrInputTooLong = fmt.Errorf("la entrada pasa de %d caracteres", MaxInputLen)
	ErrInputShape   = errors.New("eso no tiene forma de código de Kanpachi")
	ErrSeedHost     = errors.New("el servidor del código no es un nombre válido")
)

// Room es un código con el seed por el que se lo alcanza.
//
// El host del seed es SOLO transporte y no entra en la derivación: la misma
// sala es alcanzable por cualquier ruta si alguien conoce el código. Ver
// docs/03-arquitectura.md.
type Room struct {
	Code Code
	Seed string // host del seed, ya normalizado. Nunca vacío tras ParseRoom
}

// ParseRoom acepta las seis formas documentadas y devuelve siempre lo mismo.
//
//	KANP7X4MB2QF                                  → seed por defecto
//	kanp-7x4m-b2qf                                → seed por defecto
//	kanpachi://KANP-7X4M-B2QF                     → seed por defecto
//	KANP-7X4M-B2QF@seed.midominio.com             → ese seed
//	kanpachi.accentio.dev/#KANP-7X4M-B2QF         → ese seed
//	https://kanpachi.accentio.dev/#KANP-7X4M-B2QF → ese seed
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

	var codePart, hostPart string
	switch {
	case hasAt:
		// código@host
		parts := strings.Split(s, "@")
		if len(parts) != 2 {
			return Room{}, ErrInputShape
		}
		codePart, hostPart = parts[0], parts[1]
		// Un @ escrito a propósito con nada detrás es una entrada rota, no una
		// petición del seed por defecto. Caer al default acá haría que un
		// código truncado a mitad de copiar conecte en silencio a otra sala.
		if hostPart == "" {
			return Room{}, ErrSeedHost
		}

	case hasSlash:
		// host/#código, con el fragmento opcional
		parts := strings.SplitN(s, "/", 2)
		hostPart, codePart = parts[0], parts[1]
		codePart = strings.TrimPrefix(codePart, "#")
		// Una barra de más significa una ruta, y por este canal no entran
		// rutas. Ver el modelo de amenaza del manejador de protocolo.
		if strings.ContainsAny(codePart, "/?&#") {
			return Room{}, ErrInputShape
		}

	default:
		codePart = s
	}

	code, err := ParseCode(codePart)
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
	return Room{Code: code, Seed: seed}, nil
}

// InviteURL es la forma que la app GENERA, con el código en el fragmento.
//
// El fragmento no se manda en la petición HTTP, así que el servidor jamás
// recibe el código y sus logs no pueden contenerlo. La garantía no depende de
// una promesa de no registrar, depende de no recibir.
func (r Room) InviteURL() string {
	return r.Seed + "/#" + r.Code.String()
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
	// Se exige un punto para que un código mal pegado no termine
	// interpretándose como un host de una sola etiqueta.
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
