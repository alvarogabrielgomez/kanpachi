package protocol

import (
	"encoding/json"
	"io"

	"github.com/accentiostudios/kanpachi/daemon/transport/wire"
)

// MaxMessage es el tope de un mensaje ANTES de deserializar.
//
// Antes y no después, que es la mitad del punto: un tope aplicado después de
// parsear ya pagó el coste de parsear. Este código corre como SYSTEM y lee de
// un pipe al que puede hablarle cualquier proceso del usuario.
//
// Un mega cubre con holgura lo más grande que pasa por acá, que es importar un
// catálogo compartido, y sigue siendo ridículo comparado con la memoria que
// haría falta para molestar.
//
// El canal de control lleva su propio tope, mil veces más chico, porque por ahí
// no pasa ningún catálogo. El enmarcado es el mismo y vive en [wire].
const MaxMessage = 1 << 20

// ErrTooLarge es un mensaje que pasa del tope, y es TERMINAL para la conexión.
// La razón vive en [wire.ErrTooLarge]: con líneas, lo que no cupo deja el flujo
// desincronizado.
var ErrTooLarge = wire.ErrTooLarge

// Reader y Writer son el enmarcado compartido con el tope de este transporte.
type (
	Reader = wire.Reader
	Writer = wire.Writer
)

func NewReader(r io.Reader) *Reader { return wire.NewReader(r, MaxMessage) }
func NewWriter(w io.Writer) *Writer { return wire.NewWriter(w, MaxMessage) }

// decodeStrict interpreta los parámetros de un método y traduce el fallo al
// código cerrado de esta API.
//
// La disciplina es de [wire.DecodeStrict] y es la misma del catálogo y del
// estado guardado: un campo desconocido rechaza el mensaje entero. Lo que agrega
// esta envoltura es el código de error, que es lo propio del pipe.
func decodeStrict[T any](raw json.RawMessage) (T, *Error) {
	out, err := wire.DecodeStrict[T](raw)
	if err != nil {
		if len(raw) == 0 {
			return out, badRequest("faltan los parámetros")
		}
		return out, badRequest("parámetros inválidos: %v", err)
	}
	return out, nil
}

// result serializa el resultado de una operación que salió bien.
func result(v any) (json.RawMessage, *Error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, &Error{Code: CodeInternal, Message: "serializando el resultado: " + err.Error()}
	}
	return raw, nil
}
