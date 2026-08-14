package usecase

import (
	"context"
	"errors"
	"strings"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/core/port"
)

// InvitePreview es todo lo que se puede saber de una sala SIN entrar.
//
// Existe para la pantalla de confirmación, que es una invariante del proyecto
// hecha interfaz: nada que llegue de fuera surte efecto sin que alguien lo
// confirme dentro. Un enlace `kanpachi://` abre la app y enseña ESTO; entrar
// se decide después, siempre.
//
// Todos los campos salvo [InvitePreview.Room] pueden venir vacíos, y ninguno
// impide entrar. La tarjeta es presentación, y esta pantalla no decide nada:
// quien decide es `JoinRoom`, cuando alguien pulsa.
//
// **Que el registro no conteste acá tampoco quita el botón**, y ahora que
// entrar exige registro conviene decir por qué. Esto es UNA medición de un
// instante, y el registro puede estar de vuelta cuando la persona pulse. Quitar
// el botón por un fallo de hace un momento sería tratar la ausencia de
// información como una respuesta, que es justo lo que [InvitePreview.Unknown]
// existe para distinguir. Si sigue caído, `JoinRoom` lo dice en el primer
// segundo con el mensaje correcto, y el aviso de la portada ya lo venía
// anunciando por su lado.
type InvitePreview struct {
	// Room es el invite ID y su seed, ya validados. Es lo único garantizado.
	Room domain.Room

	// Card es la tarjeta de presentación, si la clave venía en el enlace y el
	// registro la sirvió. Su cero es una tarjeta que no se pudo leer.
	Card domain.RoomCard

	// Unknown es que el registro dijo que esa sala NO existe. Es un hecho
	// afirmado, no la ausencia de información: si el registro no contestó,
	// esto queda en false.
	Unknown bool

	// Trust is what is known about who wrote the card. See [domain.CardTrust].
	//
	// Its zero is [domain.CardUnverified], which is the right answer for every
	// path that returns before asking the registry: not having been able to
	// check is exactly what happened.
	Trust domain.CardTrust
}

// HasCard dice si la tarjeta trae algo que enseñar.
func (p InvitePreview) HasCard() bool { return p.Card.Room != "" || !p.Card.Host.IsZero() }

// PeekInvite mira qué hay detrás de un enlace, sin tocar la sesión.
//
// # Por qué NO toma el candado
//
// Porque no lee ni escribe nada de la sesión: solo el registro y el enlace. Y
// eso hace falta de verdad — el enlace puede llegar mientras se está creando
// una sala, que tiene el candado tomado durante decenas de segundos, y la
// pantalla de confirmación no puede esperar a que termine algo que el usuario
// quizá quiera cancelar justamente por haber pulsado el enlace.
//
// # Qué se hace con el fragmento
//
// [domain.ParseRoom] lo DESCARTA, y con razón: es la frontera de entrada
// hostil y la clave no forma parte de a qué sala se entra. Acá se recorta
// antes, aparte, y solo se usa para descifrar la tarjeta. Si la clave está
// mal, la tarjeta se descarta entera y queda la vista genérica; nunca cambia a
// qué sala apunta el enlace.
func (s *Session) PeekInvite(ctx context.Context, link string) (InvitePreview, error) {
	room, err := domain.ParseRoom(link)
	if err != nil {
		return InvitePreview{}, err
	}
	out := InvitePreview{Room: room}

	// Se le pregunta AL REGISTRO DEL ENLACE, sea cual sea. Antes había una rama
	// que enseñaba sin tarjeta cuando el seed no era el nuestro, porque el
	// cliente hablaba con uno fijo y preguntarle por un ID que emitió otro habría
	// dado "no existe" sobre una sala que existe.
	//
	// Que este paso ocurra ANTES de la pantalla de confirmación es deliberado: es
	// lo que la llena. La contrapartida está aceptada en la decisión 16, y es que
	// ese servidor ve la IP pública de esta máquina y el invite ID consultado.
	dir, err := s.deps.Directories.For(room.Seed)
	if err != nil {
		// No se pudo ni abrir el cliente, o sea que el nombre no sirve. Se enseña
		// la sala sin tarjeta: quien decide si se entra es `JoinRoom`, y va a
		// tropezar con lo mismo y con su propio mensaje.
		s.deps.Log.Warn("el enlace trae un registro que no se pudo usar",
			"seed", room.Seed, "error", err)
		return out, nil
	}

	vista, err := dir.Lookup(ctx, room.InviteID)
	switch {
	case err == nil:
		// sigue

	case errors.Is(err, port.ErrUnknownRoom):
		out.Unknown = true
		return out, nil

	default:
		// Ausencia de información, no una respuesta. Se enseña la sala sin
		// tarjeta y sin acusar a nadie de no existir.
		s.deps.Log.Warn("no se pudo resolver el enlace contra el registro",
			"código", room.InviteID.String(), "error", err)
		return out, nil
	}

	// The verdict travels ALWAYS, card or no card: "could not be checked" is as
	// informative as "the signature validates", and both are decided here,
	// before the fragment is looked at.
	out.Trust = vista.Trust()
	if out.Trust == domain.CardForged {
		// It is not opened. Showing the name of a room that the key that
		// registry pinned does not back means painting on screen whatever a
		// compromised registry wanted read, and this screen exists precisely so
		// that somebody can decide from what they read.
		s.deps.Log.Error("la tarjeta no valida contra la llave que el registro fijó",
			"código", room.InviteID.String(), "seed", room.Seed)
		return out, nil
	}

	key, hay := cardKeyOf(link)
	if !hay {
		return out, nil
	}
	card, err := domain.OpenRoomCard(vista.Sealed, key)
	if err != nil {
		// Clave equivocada o tarjeta manipulada. AES-GCM autentica, así que un
		// fallo acá significa que el contenido no es de fiar: se descarta
		// entero, igual que hace la página.
		s.deps.Log.Warn("la tarjeta del enlace no se pudo abrir",
			"código", room.InviteID.String(), "error", err)
		return out, nil
	}
	out.Card = card
	return out, nil
}

// cardKeyOf saca la clave del fragmento del enlace.
//
// Devuelve false ante cualquier duda, y ninguna es un error: un código dictado
// por teléfono no tiene fragmento, y la página genera enlaces sin él cuando no
// hay tarjeta. Lo que se pierde entonces es el nombre de la sala, no el acceso.
func cardKeyOf(link string) ([domain.CardKeyLen]byte, bool) {
	i := strings.IndexByte(link, '#')
	if i < 0 {
		return [domain.CardKeyLen]byte{}, false
	}
	key, err := domain.ParseCardKeyFragment(strings.TrimSpace(link[i+1:]))
	if err != nil {
		return [domain.CardKeyLen]byte{}, false
	}
	return key, true
}
