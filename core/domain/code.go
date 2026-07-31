// Package domain contiene los tipos y las reglas puras de Kanpachi.
//
// No hace I/O, no conoce el sistema operativo y no abre sockets. La
// aleatoriedad entra por un io.Reader que provee quien llama. Sus tests corren
// en cualquier sistema, sin privilegios y sin red, y esa es la métrica que dice
// si la arquitectura sigue sana (ver docs/03-arquitectura.md).
package domain

import (
	"errors"
	"fmt"
	"io"
	"strings"
)

// Alphabet son los 36 alfanuméricos menos 0, O, 1 e I: 32 símbolos exactos.
//
// El 32 no es cosmético. 12 caracteres × 5 bits = 60 bits, y de esos 60 bits
// depende el modelo de seguridad entero: como no hay backend que valide
// códigos, un atacante puede enumerar networkIDs contra un seed público
// buscando salas vivas. Con 60 bits eso es inviable por diseño, sin depender de
// que el seed limite la tasa. Cambiar este alfabeto cambia la entropía, así que
// hay un test que cuenta los símbolos.
//
// Se eliminan los dos miembros de cada par confuso, no solo uno: fuera 0 y O,
// fuera 1 e I. La L se conserva porque en mayúsculas, sin el 1 presente, no se
// confunde con nada.
const Alphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"

// CodeLen es la longitud del código ya normalizado, sin separadores.
const CodeLen = 12

// groupLen es el tamaño de cada grupo en la forma canónica (KANP-7X4M-B2QF).
const groupLen = 4

var (
	// ErrCodeLength y ErrCodeSymbol se distinguen porque la UI muestra textos
	// distintos: "no parece completo" contra "tiene un carácter que no existe".
	ErrCodeLength = errors.New("el código debe tener 12 caracteres")
	ErrCodeSymbol = errors.New("el código tiene un carácter que no existe en el alfabeto")
)

// Code es un código de sala válido y normalizado.
//
// El cero de Code no es utilizable. Se construye solo con [NewCode] o
// [ParseCode], así que todo Code que circule por el programa ya pasó
// validación y nadie tiene que revalidarlo aguas abajo.
type Code struct {
	raw string // 12 símbolos del Alphabet, sin separadores
}

// NewCode genera un código leyendo 12 símbolos de r.
//
// El enmascarado con 0x1f es uniforme porque 256 es múltiplo exacto de 32, así
// que no hace falta rechazo de muestras. Si alguien cambia el alfabeto a un
// tamaño que no divida a 256, esta función pasa a estar sesgada, y por eso el
// test del alfabeto cuida esa propiedad.
func NewCode(r io.Reader) (Code, error) {
	buf := make([]byte, CodeLen)
	if _, err := io.ReadFull(r, buf); err != nil {
		return Code{}, fmt.Errorf("domain: leyendo aleatoriedad para el código: %w", err)
	}
	var sb strings.Builder
	sb.Grow(CodeLen)
	for _, b := range buf {
		sb.WriteByte(Alphabet[b&0x1f])
	}
	return Code{raw: sb.String()}, nil
}

// ParseCode normaliza y valida un código pelado, sin host.
//
// Para las seis formas que acepta el producto, incluidas las que traen seed,
// ver [ParseRoom].
func ParseCode(s string) (Code, error) {
	n := normalizeCode(s)
	if len(n) != CodeLen {
		return Code{}, fmt.Errorf("%w, llegaron %d", ErrCodeLength, len(n))
	}
	for i := 0; i < len(n); i++ {
		if !strings.ContainsRune(Alphabet, rune(n[i])) {
			return Code{}, fmt.Errorf("%w: %q", ErrCodeSymbol, n[i])
		}
	}
	return Code{raw: n}, nil
}

// String devuelve la forma canónica con guiones: KANP-7X4M-B2QF.
func (c Code) String() string {
	if c.raw == "" {
		return ""
	}
	var sb strings.Builder
	sb.Grow(CodeLen + CodeLen/groupLen - 1)
	for i := 0; i < len(c.raw); i += groupLen {
		if i > 0 {
			sb.WriteByte('-')
		}
		sb.WriteString(c.raw[i : i+groupLen])
	}
	return sb.String()
}

// Raw devuelve los 12 símbolos sin separadores. Es lo que entra a la
// derivación, así que la forma en que el usuario escribió el código no puede
// cambiar la red a la que se conecta.
func (c Code) Raw() string { return c.raw }

// IsZero informa si el Code no fue construido, para distinguir "no hay sala"
// de "hay sala" sin punteros.
func (c Code) IsZero() bool { return c.raw == "" }

// normalizeCode quita separadores decorativos y pasa a mayúsculas.
//
// Es deliberadamente permisiva de ENTRADA: el usuario pega lo que le llegó por
// Telegram y funciona, sin que nadie le enseñe un formato. La salida es siempre
// canónica, así que dos personas que escriben el mismo código de forma distinta
// derivan la misma red.
//
// No traduce O a 0 ni I a 1. Con los dos miembros de cada par fuera del
// alfabeto no existe una traducción correcta, y un código generado nunca los
// contiene, así que verlos significa un error real de transcripción que vale
// más reportar que adivinar.
func normalizeCode(s string) string {
	var sb strings.Builder
	sb.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '-' || r == ' ' || r == '_' || r == '\t':
			// Separadores decorativos, se descartan.
		case r >= 'a' && r <= 'z':
			sb.WriteRune(r - 32)
		default:
			sb.WriteRune(r)
		}
	}
	return sb.String()
}
