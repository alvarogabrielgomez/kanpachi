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
// Se eliminan los dos miembros de cada par confuso, no solo uno: fuera 0 y O,
// fuera 1 e I. La L se conserva porque en mayúsculas, sin el 1 presente, no se
// confunde con nada. El objetivo es que alguien dicte un ID por teléfono y del
// otro lado lo escriban bien a la primera.
//
// Que sean 32 exactos tampoco es cosmético: [NewInviteID] enmascara con 0x1f, y
// eso solo es uniforme porque 256 es múltiplo de 32. Hay un test que cuenta los
// símbolos por ese motivo.
const Alphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"

// InviteIDLen es la longitud del invite ID ya normalizado, sin separadores.
//
// Ocho, o sea 40 bits, y eso es una decisión con historia. El diseño anterior
// exigía 12 caracteres con este argumento: sin backend que valide, un atacante
// enumera networkIDs contra un seed público hasta hallar salas vivas, y la
// única defensa posible es la entropía.
//
// Las dos premisas cambiaron. Ahora hay un registro que responde las consultas,
// así que la enumeración se corta con límite de tasa como cualquier otra. Y
// acertar un invite ID dejó de dar entrada: da la tarjeta de presentación y el
// derecho a tocar la puerta, porque entrar exige una credencial que emite el
// host. Ver decisiones 2, 17 y 24 en docs/02-decisiones-de-diseno.md.
const InviteIDLen = 8

// groupLen es el tamaño de cada grupo en la forma canónica (A7K2-M9QX).
const groupLen = 4

var (
	// ErrInviteIDLength y ErrInviteIDSymbol se distinguen porque la UI muestra
	// textos distintos: "no parece completo" contra "tiene un carácter que no
	// existe".
	ErrInviteIDLength = fmt.Errorf("el código debe tener %d caracteres", InviteIDLen)
	ErrInviteIDSymbol = errors.New("el código tiene un carácter que no existe en el alfabeto")
)

// InviteID es un código de invitación válido y normalizado.
//
// NO es material criptográfico y no debe tratarse como un secreto. Es una llave
// de búsqueda que el seed emite, guarda y registra en sus logs. Lo que abre una
// sala es la credencial que emite el host, jamás esto.
//
// El cero de InviteID no es utilizable. Se construye solo con [NewInviteID] o
// [ParseInviteID], así que todo InviteID que circule por el programa ya pasó
// validación y nadie tiene que revalidarlo aguas abajo.
type InviteID struct {
	raw string // 8 símbolos del Alphabet, sin separadores
}

// NewInviteID genera un invite ID leyendo 8 símbolos de r.
//
// En producción lo emite el seed, que es quien puede garantizar unicidad dentro
// de su registro. Esta función existe para el host que se autohospeda, para los
// tests, y para proponer un ID cuando el registro no está disponible.
//
// El enmascarado con 0x1f es uniforme porque 256 es múltiplo exacto de 32, así
// que no hace falta rechazo de muestras. Si alguien cambia el alfabeto a un
// tamaño que no divida a 256, esta función pasa a estar sesgada, y por eso el
// test del alfabeto cuida esa propiedad.
func NewInviteID(r io.Reader) (InviteID, error) {
	buf := make([]byte, InviteIDLen)
	if _, err := io.ReadFull(r, buf); err != nil {
		return InviteID{}, fmt.Errorf("domain: leyendo aleatoriedad para el invite ID: %w", err)
	}
	var sb strings.Builder
	sb.Grow(InviteIDLen)
	for _, b := range buf {
		sb.WriteByte(Alphabet[b&0x1f])
	}
	return InviteID{raw: sb.String()}, nil
}

// ParseInviteID normaliza y valida un invite ID pelado, sin host.
//
// Para las seis formas que acepta el producto, incluidas las que traen seed,
// ver [ParseRoom].
func ParseInviteID(s string) (InviteID, error) {
	n := normalizeInviteID(s)
	if len(n) != InviteIDLen {
		return InviteID{}, fmt.Errorf("%w, llegaron %d", ErrInviteIDLength, len(n))
	}
	for i := 0; i < len(n); i++ {
		if !strings.ContainsRune(Alphabet, rune(n[i])) {
			return InviteID{}, fmt.Errorf("%w: %q", ErrInviteIDSymbol, n[i])
		}
	}
	return InviteID{raw: n}, nil
}

// String devuelve la forma canónica con guion: A7K2-M9QX.
func (i InviteID) String() string {
	if i.raw == "" {
		return ""
	}
	var sb strings.Builder
	sb.Grow(InviteIDLen + InviteIDLen/groupLen - 1)
	for p := 0; p < len(i.raw); p += groupLen {
		if p > 0 {
			sb.WriteByte('-')
		}
		sb.WriteString(i.raw[p : p+groupLen])
	}
	return sb.String()
}

// Raw devuelve los 8 símbolos sin separadores. Es lo que entra a la derivación
// de encuentro y lo que viaja en la URL, así que la forma en que el usuario
// escribió el ID no puede cambiar la sala a la que llega.
func (i InviteID) Raw() string { return i.raw }

// IsZero informa si el InviteID no fue construido, para distinguir "no hay
// sala" de "hay sala" sin punteros.
func (i InviteID) IsZero() bool { return i.raw == "" }

// normalizeInviteID quita separadores decorativos y pasa a mayúsculas.
//
// Es deliberadamente permisiva de ENTRADA: el usuario pega lo que le llegó por
// Telegram y funciona, sin que nadie le enseñe un formato. La salida es siempre
// canónica, así que dos personas que escriben el mismo ID de forma distinta
// llegan a la misma sala.
//
// No traduce O a 0 ni I a 1. Con los dos miembros de cada par fuera del
// alfabeto no existe una traducción correcta, y un ID generado nunca los
// contiene, así que verlos significa un error real de transcripción que vale
// más reportar que adivinar.
func normalizeInviteID(s string) string {
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
