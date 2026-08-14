// Command pipeprobe le habla al daemon por el canal local, a pelo.
//
// Vive en `internal/` para que el producto no lo pueda importar y el instalador
// no lo distribuya: es la sonda con la que se prueba el canal a mano, y de paso
// la que puede mandar lo que el daemon tiene que RECHAZAR.
//
// # En qué se diferencia del CLI, y por qué el nombre importa
//
// `kanpachi` es el cliente: subcomandos con nombre, parámetros validados y
// salida para leer. Esto pide el método por su nombre de cable con el JSON crudo
// en `-params`, y sabe saludar con un token que no vale. O sea que sirve para
// probar la frontera, no para dárselo a nadie.
//
// Se llamó `kanpctl` hasta el 2026-08-14, y el nombre hacía el daño solo: se leía
// como hermano de `kanpachi`, o sea como un binario del producto. `prepare-stage.ps1`
// llegó a describirlo como "el cliente de linea de comandos". Ahora se llama como
// el resto del instrumental, `<lo que mide>probe`, y lo que mide es el pipe.
//
// Uso:
//
//	go run ./internal/pipeprobe -data C:\ruta\a\datos status
//	go run ./internal/pipeprobe -data C:\ruta\a\datos -no-token status   (debe fallar)
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/accentiostudios/kanpachi/daemon/transport/client"
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
	plazo := flag.Duration("timeout", client.DefaultTimeout, "cuánto esperar la respuesta del daemon")
	flag.Parse()

	metodo := "status"
	if flag.NArg() > 0 {
		metodo = flag.Arg(0)
	}

	if err := hablar(*datos, *nombre, metodo, *params, *sinToken, *plazo); err != nil {
		fmt.Fprintln(os.Stderr, "pipeprobe:", err)
		os.Exit(1)
	}
}

func hablar(datos, nombre, metodo, params string, sinToken bool, plazo time.Duration) error {
	abrir := func() (*client.Client, error) {
		if sinToken {
			return client.OpenWithToken(nombre, "token-invalido-a-proposito")
		}
		return client.Open(nombre, datos)
	}
	c, err := abrir()
	if err != nil {
		return fmt.Errorf("%w\n  ¿está corriendo kanpachid --console?", err)
	}
	defer func() { _ = c.Close() }()
	c.Plazo = plazo

	// El JSON crudo va tal cual, sin pasar por `json.Marshal`: envolverlo en un
	// `any` lo re-serializaría normalizado, y entonces esta herramienta no podría
	// mandar el JSON malformado que existe para poder mandar.
	raw, err := c.CallRaw(protocol.Method(metodo), []byte(params))
	if len(raw) > 0 {
		fmt.Printf("%s → %s\n", metodo, raw)
	}
	return err
}
