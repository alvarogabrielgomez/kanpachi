package wire

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

// TestElTopeSeAplicaAntesDeInterpretar: el lector corta por tamaño sin haber
// mirado el contenido, que es la mitad del punto. Un tope aplicado después de
// parsear ya pagó el coste de parsear.
func TestElTopeSeAplicaAntesDeInterpretar(t *testing.T) {
	gigante := strings.Repeat("x", 128) + "\n"
	r := NewReader(strings.NewReader(gigante), 64)

	if _, err := r.ReadLine(); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("ReadLine con un mensaje del doble del tope = %v", err)
	}
}

// TestCadaTransporteTraeSuTope: es lo único que los distingue, y por eso el
// tope es del llamador y no una constante de este paquete.
func TestCadaTransporteTraeSuTope(t *testing.T) {
	mensaje := strings.Repeat("y", 100) + "\n"

	if _, err := NewReader(strings.NewReader(mensaje), 8<<10).ReadLine(); err != nil {
		t.Fatalf("con el tope del canal de control tendría que caber: %v", err)
	}
	if _, err := NewReader(strings.NewReader(mensaje), 50).ReadLine(); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("con un tope de 50 no cabe, y dio %v", err)
	}
}

// TestLoQueSigueAUnMensajeGiganteNoSeInterpreta.
//
// Es la razón por la que ErrTooLarge es terminal y por la que este lector no es
// un bufio.Scanner: la cola del mensaje que no cupo queda en el flujo, así que
// seguir leyendo sería interpretar basura como mensajes nuevos. El test lo
// AFIRMA en vez de dejarlo en un comentario.
//
// El mensaje gigante mide más que el búfer interno a propósito. Uno más chico
// se consume entero en una sola lectura y el flujo queda alineado por
// casualidad, y esa casualidad es justamente lo que no se puede usar como
// diseño: depende del tamaño del búfer, no del protocolo.
func TestLoQueSigueAUnMensajeGiganteNoSeInterpreta(t *testing.T) {
	flujo := strings.Repeat("x", 8000) + `{"soy":"la cola"}` + "\n" + `{"legítimo":1}` + "\n"
	r := NewReader(strings.NewReader(flujo), 64)

	if _, err := r.ReadLine(); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("primera lectura = %v", err)
	}
	// Quien recibe ErrTooLarge cierra. Si en vez de cerrar siguiera leyendo,
	// lo que le llega es la cola del mensaje que nunca cupo, jamás el mensaje
	// legítimo que venía detrás.
	siguiente, err := r.ReadLine()
	if err == nil && string(siguiente) == `{"legítimo":1}` {
		t.Fatal("la lectura se resincronizó sola, así que el error terminal no haría falta")
	}
}

// TestLasLíneasVacíasSeIgnoran: un salto de más no merece un error.
func TestLasLíneasVacíasSeIgnoran(t *testing.T) {
	r := NewReader(strings.NewReader("\n\r\n  \n{\"ok\":1}\n"), 1024)

	linea, err := r.ReadLine()
	if err != nil {
		t.Fatal(err)
	}
	if string(linea) != `{"ok":1}` {
		t.Fatalf("línea = %q", linea)
	}
}

// TestElFinDelFlujoEsEOFYNoUnFallo: es un cierre limpio.
func TestElFinDelFlujoEsEOFYNoUnFallo(t *testing.T) {
	r := NewReader(strings.NewReader(""), 1024)

	if _, err := r.ReadLine(); !errors.Is(err, io.EOF) {
		t.Fatalf("ReadLine sobre un flujo cerrado = %v", err)
	}
}

// TestUnMensajeQueNoCabeNoSeEscribe: el tope vale para los dos lados. Mandar
// medio mensaje desincroniza al otro exactamente igual.
func TestUnMensajeQueNoCabeNoSeEscribe(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf, 32)

	err := w.Write(struct {
		Texto string `json:"texto"`
	}{strings.Repeat("z", 200)})
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("Write de algo que pasa del tope = %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("se escribieron %d bytes de un mensaje que no cabía", buf.Len())
	}
}

// TestCadaMensajeSeVacíaEnElActo: sin esto, del otro lado hay alguien esperando
// una respuesta que está en un búfer.
func TestCadaMensajeSeVacíaEnElActo(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf, 1024)

	if err := w.Write(map[string]int{"a": 1}); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != "{\"a\":1}\n" {
		t.Fatalf("lo escrito = %q", got)
	}
}

// TestElSaltoDeLíneaDentroDeUnaCadenaViajaEscapado.
//
// El delimitador y el contenido comparten byte, así que esto es lo que hace que
// un nombre de sala con un salto no parta el mensaje en dos.
func TestElSaltoDeLíneaDentroDeUnaCadenaViajaEscapado(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf, 1024)

	if err := w.Write(map[string]string{"sala": "los\npanas"}); err != nil {
		t.Fatal(err)
	}
	if bytes.Count(buf.Bytes(), []byte("\n")) != 1 {
		t.Fatalf("el mensaje trae más de un salto: %q", buf.String())
	}

	linea, err := NewReader(&buf, 1024).ReadLine()
	if err != nil {
		t.Fatal(err)
	}
	vuelta, err := DecodeStrict[map[string]string](linea)
	if err != nil {
		t.Fatal(err)
	}
	if vuelta["sala"] != "los\npanas" {
		t.Fatalf("la vuelta cambió el contenido: %q", vuelta["sala"])
	}
}

// TestUnCampoDeMásRechazaElMensajeEntero: misma disciplina que el catálogo y
// que el estado guardado.
func TestUnCampoDeMásRechazaElMensajeEntero(t *testing.T) {
	type params struct {
		Juego string `json:"game"`
	}

	if _, err := DecodeStrict[params]([]byte(`{"game":"zomboid","ports":[445]}`)); err == nil {
		t.Fatal("un campo que el esquema no define pasó")
	}
	if _, err := DecodeStrict[params]([]byte(`{"game":"zomboid"}`)); err != nil {
		t.Fatalf("el mensaje legítimo falló: %v", err)
	}
}

// TestLoQueViaDespuésDelObjetoTambiénRechaza: un segundo objeto pegado sería
// una orden que nadie leyó.
func TestLoQueVieneDespuésDelObjetoTambiénRechaza(t *testing.T) {
	type params struct {
		Juego string `json:"game"`
	}

	if _, err := DecodeStrict[params]([]byte(`{"game":"a"}{"game":"b"}`)); err == nil {
		t.Fatal("el contenido de después del objeto pasó")
	}
	if _, err := DecodeStrict[params](nil); err == nil {
		t.Fatal("interpretar la nada no dio error")
	}
}
