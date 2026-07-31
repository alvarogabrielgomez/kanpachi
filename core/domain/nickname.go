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

// String devuelve el nombre. Es seguro para mostrar y para pasar como
// argumento a un proceso hijo, porque ParseNickname ya descartó todo lo que no
// sea alfanumérico ASCII.
func (n Nickname) String() string { return n.value }

// IsZero informa si el Nickname no fue construido.
func (n Nickname) IsZero() bool { return n.value == "" }
