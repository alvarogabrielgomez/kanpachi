// Command kanpctl le habla al daemon por el named pipe.
//
// Vive en `internal/` para que el producto no lo pueda importar y el instalador
// no lo distribuya: es la herramienta con la que se prueba el pipe a mano, y de
// paso la implementación de referencia del cliente.
//
// Uso:
//
//	go run ./internal/kanpctl -data C:\ruta\a\datos status
//	go run ./internal/kanpctl -data C:\ruta\a\datos -no-token status   (debe fallar)
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/accentiostudios/kanpachi/daemon/transport/pipe"
	"github.com/accentiostudios/kanpachi/daemon/transport/protocol"
)

func main() {
	datos := flag.String("data", "", "directorio de datos donde está api.token")
	nombre := flag.String("pipe", pipe.ConsoleName, "nombre del pipe")
	sinToken := flag.Bool("no-token", false, "saludar con un token inválido, para comprobar que se rechaza")
	// Sin esto la herramienta solo puede llamar a los métodos que no llevan
	// parámetros, o sea que no puede crear una sala, que es justo lo que hace
	// falta para probar el motor de punta a punta.
	//
	// El JSON va crudo y sin validar acá a propósito: quien valida es el daemon,
	// con esquema estricto y tope de tamaño, y esta herramienta existe para
	// poder mandarle también lo que va a rechazar.
	params := flag.String("params", "", "JSON crudo para el campo params del método")
	// El plazo era de 10 s fijos, y `create_room` lo pasaba de largo con el
	// daemon trabajando bien: cada adaptador tarda unos 5 s en tomar dirección,
	// una sala levanta DOS (la sala y el vestíbulo), y encima va el sondeo del
	// MTU. El cliente cortaba, el daemon terminaba, y el síntoma era un
	// `i/o timeout` con la sala creada.
	//
	// El techo real lo pone el motor: `engine.AddressDeadline` son 30 s por
	// adaptador. Este plazo cubre los dos con margen para el sondeo, y es
	// ajustable porque una máquina lenta es un caso, no un fallo.
	plazo := flag.Duration("timeout", 90*time.Second, "cuánto esperar la respuesta del daemon")
	flag.Parse()

	metodo := "status"
	if flag.NArg() > 0 {
		metodo = flag.Arg(0)
	}

	if err := hablar(*datos, *nombre, metodo, *params, *sinToken, *plazo); err != nil {
		fmt.Fprintln(os.Stderr, "kanpctl:", err)
		os.Exit(1)
	}
}

func hablar(datos, nombre, metodo, params string, sinToken bool, plazo time.Duration) error {
	// El token se relee en CADA conexión: el daemon lo rota al arrancar, así que
	// uno recordado de la sesión anterior es siempre el equivocado.
	token := "token-invalido-a-proposito"
	if !sinToken {
		var err error
		if token, err = pipe.ReadToken(datos); err != nil {
			return err
		}
	}

	conn, err := marcar(nombre)
	if err != nil {
		return fmt.Errorf("no se pudo abrir %s: %w\n  ¿está corriendo kanpachid --console?", nombre, err)
	}
	defer func() { _ = conn.Close() }()

	w := protocol.NewWriter(conn)
	r := protocol.NewReader(conn)
	pedir := func(id uint64, m protocol.Method, params string) error {
		req := protocol.Request{ID: id, Method: m}
		if params != "" {
			req.Params = json.RawMessage(params)
		}
		if err := w.Write(req); err != nil {
			return err
		}
		_ = conn.SetReadDeadline(time.Now().Add(plazo))
		linea, err := r.ReadLine()
		if err != nil {
			return err
		}
		fmt.Printf("%s → %s\n", m, linea)
		return nil
	}

	// El saludo va PRIMERO: el daemon rechaza cualquier método antes de él.
	if err := pedir(1, protocol.MethodHello, fmt.Sprintf(`{"token":%q}`, token)); err != nil {
		return err
	}
	return pedir(2, protocol.Method(metodo), params)
}

func marcar(nombre string) (net.Conn, error) { return dial(nombre) }
