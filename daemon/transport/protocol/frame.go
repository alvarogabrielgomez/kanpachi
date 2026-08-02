package protocol

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
const MaxMessage = 1 << 20

// ErrTooLarge es un mensaje que pasa del tope.
//
// **Es terminal para esa conexión.** Con un protocolo delimitado por líneas, un
// mensaje que no cupo deja el flujo desincronizado: lo que sigue es la cola de
// algo que nunca se leyó entero, y seguir leyendo sería interpretar basura como
// mensajes. Quien lo reciba cierra.
var ErrTooLarge = errors.New("el mensaje pasa del tope")

// Reader lee mensajes delimitados por líneas, con tope.
//
// No usa bufio.Scanner a propósito. Scanner con buffer acotado devuelve
// bufio.ErrTooLong y deja el resto de la línea en el flujo, así que quien lo
// use sin cerrar la conexión sigue leyendo la cola de un mensaje gigante como
// si fueran mensajes nuevos. Acá el tope y la desincronización son la misma
// cosa, y por eso el error dice cerrá.
type Reader struct {
	br *bufio.Reader
}

func NewReader(r io.Reader) *Reader {
	return &Reader{br: bufio.NewReaderSize(r, 4096)}
}

// ReadLine devuelve la siguiente línea sin el salto.
//
// Las líneas vacías se saltan: un cliente que mande "\r\n" de más no merece un
// error, merece que se lo ignore.
func (r *Reader) ReadLine() ([]byte, error) {
	var acumulado []byte
	for {
		trozo, esPrefijo, err := r.br.ReadLine()
		if err != nil {
			return nil, err
		}
		if len(acumulado)+len(trozo) > MaxMessage {
			return nil, fmt.Errorf("%w: más de %d bytes", ErrTooLarge, MaxMessage)
		}
		acumulado = append(acumulado, trozo...)
		if esPrefijo {
			continue
		}
		if linea := bytes.TrimSpace(acumulado); len(linea) > 0 {
			return linea, nil
		}
		acumulado = acumulado[:0]
	}
}

// Writer escribe mensajes delimitados por líneas.
type Writer struct {
	w  io.Writer
	bw *bufio.Writer
}

func NewWriter(w io.Writer) *Writer {
	return &Writer{w: w, bw: bufio.NewWriter(w)}
}

// Write serializa y manda, con el salto de línea y el vaciado.
//
// Vacía en cada mensaje y no acumula: del otro lado hay una UI esperando una
// respuesta, y un búfer a medias es una pantalla congelada.
func (w *Writer) Write(v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("serializando la respuesta: %w", err)
	}
	// El JSON serializado no lleva saltos, así que el delimitador no se puede
	// confundir con contenido. Se comprueba igual: si algún día un campo trae
	// un salto sin escapar, el síntoma sería un cliente leyendo media respuesta.
	if bytes.ContainsRune(raw, '\n') {
		return errors.New("la respuesta serializada trae un salto de línea")
	}
	if len(raw) > MaxMessage {
		return fmt.Errorf("%w: la respuesta mide %d bytes", ErrTooLarge, len(raw))
	}
	if _, err := w.bw.Write(raw); err != nil {
		return err
	}
	if err := w.bw.WriteByte('\n'); err != nil {
		return err
	}
	return w.bw.Flush()
}

// decodeStrict interpreta los parámetros de un método.
//
// Estricto por las dos vías, igual que el catálogo y el estado guardado: un
// campo desconocido rechaza el mensaje entero, y contenido después del objeto
// también. Un campo de más no es un cliente amable con extensiones, es un
// cliente que cree estar pidiendo algo que este daemon no hace.
func decodeStrict[T any](raw json.RawMessage) (T, *Error) {
	var out T
	if len(raw) == 0 {
		return out, badRequest("faltan los parámetros")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return out, badRequest("parámetros inválidos: %v", err)
	}
	if dec.More() {
		return out, badRequest("hay contenido después de los parámetros")
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
