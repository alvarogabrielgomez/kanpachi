package domain

import (
	"errors"
	"fmt"
	"unicode"
)

// NicknameMaxLen es el tope de la decisión 21.
const NicknameMaxLen = 12

var (
	ErrNicknameEmpty   = errors.New("el nombre no puede estar vacío")
	ErrNicknameTooLong = fmt.Errorf("el nombre no puede pasar de %d caracteres", NicknameMaxLen)
	ErrNicknameSymbol  = errors.New("el nombre solo admite letras y números")
)

// Nickname es el nombre que ven los demás miembros de la sala.
//
// No es único, no se verifica y no es una cuenta: es una etiqueta para que un
// humano reconozca a otro en una lista corta. Existe por dos razones, y la
// segunda importa tanto como la primera:
//
//  1. Sin nombres, expulsar a alguien es adivinar.
//  2. Reemplaza el nombre del equipo, que el motor publica a todos los peers y
//     que en Windows suele contener el nombre real de la persona.
type Nickname struct {
	value string
}

// ParseNickname valida contra las reglas de la decisión 21.
//
// La validación es estricta porque este valor viaja a las pantallas de otras
// personas y termina como argumento del motor. Solo letras y números ASCII:
// nada de espacios, de caracteres de control, ni de alfabetos que permitan
// dibujar un nombre visualmente idéntico al de otro miembro para suplantarlo
// en la lista.
func ParseNickname(s string) (Nickname, error) {
	if s == "" {
		return Nickname{}, ErrNicknameEmpty
	}
	// Se cuenta en runas y no en bytes: un nombre de 12 caracteres no latinos
	// mediría más de 12 bytes, y el tope que le prometemos al usuario está en
	// caracteres. El rechazo de no-ASCII ocurre igual en el bucle de abajo,
	// esto solo hace que el mensaje de error sea el correcto.
	runes := []rune(s)
	if len(runes) > NicknameMaxLen {
		return Nickname{}, fmt.Errorf("%w, llegaron %d", ErrNicknameTooLong, len(runes))
	}
	for _, r := range runes {
		isASCIILetter := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		isASCIIDigit := r >= '0' && r <= '9'
		if !isASCIILetter && !isASCIIDigit {
			if unicode.IsSpace(r) {
				return Nickname{}, fmt.Errorf("%w: no se admiten espacios", ErrNicknameSymbol)
			}
			return Nickname{}, fmt.Errorf("%w: %q no vale", ErrNicknameSymbol, r)
		}
	}
	return Nickname{value: string(runes)}, nil
}

// NicknameFromHost turns this machine's host name into a legal nickname, to
// SUGGEST when nobody chose one. It never fails.
//
// # Why the cleaning, and why the host name is not enough on its own
//
// Because the nickname rule is far narrower than a host name's: ASCII letters
// and digits, twelve of them. A server called `mc-01.casa.lan` is a perfectly
// ordinary host name and an illegal nickname, and what comes back from the
// daemon then is `bad_nickname`, which explains nothing to somebody who never
// chose a name at all.
//
// The domain goes first: `mc-01.casa.lan` wants to be `mc01`, and dragging
// `casalan` along would spend the twelve on the part that identifies nobody.
//
// # Why the host arrives as a parameter
//
// So this package keeps not importing `os`. That is what lets the domain run
// under the Linux CI whatever machine it is compiled on, and it is the same
// reason [QuarantineSystem] is injected instead of detected.
//
// # It is a suggestion and it is never persisted
//
// Whoever calls this ANSWERS with it and stores nothing. A derived name that
// gets written becomes indistinguishable from a chosen one, and then it beats
// the real one: measured on 2026-08-18, a machine whose window said "Alvaro"
// entered a room as "AlvaroGDeskt" because the terminal had written its guess
// to disk once.
func NicknameFromHost(host string) Nickname {
	base := host
	if i := indexByte(base, '.'); i >= 0 {
		base = base[:i]
	}

	var clean []rune
	for _, r := range base {
		isASCIILetter := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		isASCIIDigit := r >= '0' && r <= '9'
		if isASCIILetter || isASCIIDigit {
			clean = append(clean, r)
		}
		if len(clean) >= NicknameMaxLen {
			break
		}
	}
	if len(clean) == 0 {
		// A host name that leaves neither a letter nor a digit exists (`_`, or
		// one written entirely in another alphabet). Rather than fail, a name
		// that works and that says where it came from.
		return Nickname{value: "kanpachi"}
	}
	return Nickname{value: string(clean)}
}

// indexByte avoids importing `strings` for one lookup, which would be the only
// import this file has beyond the errors it declares.
func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

// String devuelve el nombre. Es seguro para mostrar y para pasar como
// argumento a un proceso hijo, porque ParseNickname ya descartó todo lo que no
// sea alfanumérico ASCII.
func (n Nickname) String() string { return n.value }

// IsZero informa si el Nickname no fue construido.
func (n Nickname) IsZero() bool { return n.value == "" }
