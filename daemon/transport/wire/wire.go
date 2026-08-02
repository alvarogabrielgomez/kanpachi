// Package wire es el enmarcado de mensajes delimitados por líneas, con tope.
//
// Existe una sola vez y lo usan los dos transportes del daemon, la API local del
// named pipe y el canal de control de la sala. Los dos leen bytes de gente que
// no es este programa, los dos corren como SYSTEM, y los dos necesitan el mismo
// tratamiento: **tope antes de deserializar y desincronización tratada como
// terminal**.
//
// Es una copia y no dos porque el día que una se arregle, la otra no. El tope va
// por parámetro justamente porque es lo único que cambia entre ellos: un mega
// por el pipe, donde pasa un catálogo importado entero, y ocho kilobytes por el
// canal, donde el mensaje más grande son unos cientos de bytes.
//
// No conoce Windows: es io.Reader e io.Writer.
package wire

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// ErrTooLarge es un mensaje que pasa del tope.
//
// **Es terminal para esa conexión**, y eso no es severidad de más. Con un
// protocolo delimitado por líneas, un mensaje que no cupo deja el flujo
// desincronizado: lo que sigue es la cola de algo que nunca se leyó entero, y
// seguir leyendo sería interpretar basura como mensajes. Quien lo reciba cierra.
var ErrTooLarge = errors.New("el mensaje pasa del tope")

// ErrNewline es una serialización que trae un salto de línea.
//
// No debería poder pasar: encoding/json escapa los saltos dentro de las cadenas.
// Se comprueba igual porque el delimitador y el contenido comparten byte, y el
// síntoma de que un día dejara de ser cierto sería el peor posible, alguien del
// otro lado leyendo medio mensaje como si fuera uno entero.
var ErrNewline = errors.New("la serialización trae un salto de línea")

// Reader lee mensajes delimitados por líneas, con tope.
//
// No usa bufio.Scanner a propósito. Scanner con buffer acotado devuelve
// bufio.ErrTooLong y **deja el resto de la línea en el flujo**, así que quien lo
// use sin cerrar la conexión sigue leyendo la cola de un mensaje gigante como si
// fueran mensajes nuevos. Acá el tope y la desincronización son la misma cosa, y
// por eso el error dice cerrá.
type Reader struct {
	br  *bufio.Reader
	max int
}

// NewReader arma el lector. El tope es del llamador: es lo único que distingue
// a un transporte de otro.
func NewReader(r io.Reader, max int) *Reader {
	return &Reader{br: bufio.NewReaderSize(r, 4096), max: max}
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
		if len(acumulado)+len(trozo) > r.max {
			return nil, fmt.Errorf("%w: más de %d bytes", ErrTooLarge, r.max)
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
	bw  *bufio.Writer
	max int
}

func NewWriter(w io.Writer, max int) *Writer {
	return &Writer{bw: bufio.NewWriter(w), max: max}
}

// Write serializa y manda, con el salto de línea y el vaciado.
//
// Vacía en cada mensaje y no acumula: del otro lado hay alguien esperando, y un
// búfer a medias es una pantalla congelada.
func (w *Writer) Write(v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("serializando el mensaje: %w", err)
	}
	if bytes.ContainsRune(raw, '\n') {
		return ErrNewline
	}
	if len(raw) > w.max {
		return fmt.Errorf("%w: el mensaje mide %d bytes", ErrTooLarge, len(raw))
	}
	if _, err := w.bw.Write(raw); err != nil {
		return err
	}
	if err := w.bw.WriteByte('\n'); err != nil {
		return err
	}
	return w.bw.Flush()
}

// DecodeStrict interpreta un mensaje, estricto por las dos vías.
//
// Misma disciplina que el catálogo y que el estado guardado: un campo
// desconocido rechaza el mensaje ENTERO, y contenido después del objeto también.
// Un campo de más no es alguien amable con extensiones, es alguien que cree
// estar pidiendo algo que este daemon no hace.
func DecodeStrict[T any](raw []byte) (T, error) {
	var out T
	if len(raw) == 0 {
		return out, errors.New("no hay nada que interpretar")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return out, err
	}
	if dec.More() {
		return out, errors.New("hay contenido después del objeto")
	}
	return out, nil
}
