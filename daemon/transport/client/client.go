// Package client habla el protocolo local desde el lado del cliente.
//
// Es la otra mitad de [protocol.Server], y existe por lo mismo que aquel está
// separado del pipe: lo que hay acá no cambia porque el canal sea un named pipe
// de Windows o un socket Unix de Linux. Lo único que se bifurca es [Dial], que
// son tres líneas.
//
// # Lo que impone, y no es opcional para quien lo use
//
// El saludo va PRIMERO y lo manda [Open]. El daemon rechaza cualquier método
// antes de él, así que un cliente que se lo salte no consigue nada y lo que ve
// es `unauthorized`, que se lee como un problema de permisos y no como lo que
// es. Que el saludo esté dentro de la conexión y no en un método aparte es lo
// que impide construir un cliente al que se le olvide.
//
// # Una orden por conexión, y por eso `progress` y `cancel` no están acá
//
// El servidor atiende una orden por vez en cada conexión: lee, despacha,
// contesta. Los dos métodos que sirven para mirar o cortar una operación larga
// se piden por una conexión APARTE, porque por la misma se encolarían detrás de
// justo lo que quieren observar. Quien los necesite abre un segundo [Client],
// que es barato y es lo que hace la interfaz.
package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync/atomic"
	"time"

	"github.com/accentiostudios/kanpachi/core/timing"
	"github.com/accentiostudios/kanpachi/daemon/transport/pipe"
	"github.com/accentiostudios/kanpachi/daemon/transport/protocol"
)

// Client es una conexión ya saludada.
//
// No es seguro usarlo desde varias gorrutinas: el protocolo es una petición y
// una respuesta por vez sobre el mismo flujo, así que dos llamadas a la vez
// mezclarían las líneas. Quien necesite concurrencia abre otro.
type Client struct {
	conn  net.Conn
	w     *protocol.Writer
	r     *protocol.Reader
	id    atomic.Uint64
	Plazo time.Duration
	// Addr es a qué canal se conectó, para poder nombrarlo en un error.
	Addr string
}

// Open abre el canal, lee el token del directorio de datos y saluda.
//
// El token se relee en CADA conexión, y eso es obligatorio, no cuidado de más:
// el daemon lo rota al arrancar, así que uno recordado de la sesión anterior es
// siempre el equivocado.
func Open(addr, dataDir string) (*Client, error) {
	token, err := pipe.ReadToken(dataDir)
	if err != nil {
		return nil, fmt.Errorf("reading the token in %s: %w\n"+
			"  The daemon writes that file on start, and only someone with permission on "+
			"the data directory can read it", dataDir, err)
	}
	return OpenWithToken(addr, token)
}

// OpenWithToken es [Open] con el token ya en la mano.
//
// Existe para las herramientas que necesitan mandar uno que NO vale, que es la
// única forma de comprobar que el daemon rechaza lo que tiene que rechazar. Ver
// `internal/pipeprobe`.
func OpenWithToken(addr, token string) (*Client, error) {
	conn, err := Dial(addr)
	if err != nil {
		return nil, fmt.Errorf("could not open %s: %w", addr, err)
	}
	c := &Client{
		conn:  conn,
		w:     protocol.NewWriter(conn),
		r:     protocol.NewReader(conn),
		Plazo: timing.PipeDefaultTimeout,
		Addr:  addr,
	}
	if _, err := c.Call(protocol.MethodHello, struct {
		Token string `json:"token"`
	}{token}); err != nil {
		_ = c.Close()
		return nil, err
	}
	return c, nil
}

// Close cierra la conexión. Llamarlo dos veces es seguro.
func (c *Client) Close() error { return c.conn.Close() }

// Call manda un método y devuelve lo que contestó el daemon.
//
// # Puede devolver resultado Y error a la vez
//
// Y quien lo use tiene que contar con eso, porque no es un descuido del
// protocolo: la expulsión a medias contesta con la sala YA sin el expulsado más
// el aviso de lo que no se pudo cerrar. Un cliente que mire el error primero y
// descarte el resultado tira el estado que acaba de pedir. Ver [protocol.Response].
//
// El error, cuando viene del daemon, es un *[protocol.Error] con su código
// cerrado, así que se puede ramificar con `errors.As` sin leer castellano.
func (c *Client) Call(m protocol.Method, params any) (json.RawMessage, error) {
	var raw json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("serializing the parameters of %s: %w", m, err)
		}
		raw = b
	}
	return c.CallRaw(m, raw)
}

// CallRaw es [Client.Call] con los parámetros ya en JSON, sin tocar.
//
// Existe para poder mandar lo que el daemon tiene que RECHAZAR. Pasar por
// `json.Marshal` normalizaría el mensaje, así que un JSON malformado a propósito
// llegaría al otro lado ya arreglado y la comprobación no probaría nada. Lo usa
// `internal/pipeprobe`.
func (c *Client) CallRaw(m protocol.Method, params json.RawMessage) (json.RawMessage, error) {
	req := protocol.Request{ID: c.id.Add(1), Method: m, Params: params}
	if err := c.w.Write(req); err != nil {
		return nil, fmt.Errorf("sending %s: %w", m, err)
	}

	// El plazo se pone por llamada y no una vez al abrir: es un plazo de
	// RESPUESTA, y un plazo absoluto puesto al principio se agotaría a mitad de
	// una sesión larga aunque cada orden hubiera contestado a tiempo.
	if err := c.conn.SetReadDeadline(time.Now().Add(c.Plazo)); err != nil {
		return nil, fmt.Errorf("setting the deadline for %s: %w", m, err)
	}
	linea, err := c.r.ReadLine()
	if err != nil {
		return nil, fmt.Errorf("waiting for the answer to %s: %w", m, err)
	}

	var resp protocol.Response
	if err := json.Unmarshal(linea, &resp); err != nil {
		return nil, fmt.Errorf("the answer to %s is not JSON: %w", m, err)
	}
	if resp.ID != req.ID {
		// No se ignora ni se reintenta: el flujo quedó desincronizado, y seguir
		// leyendo daría la respuesta de una orden anterior como si fuera de esta.
		return nil, fmt.Errorf("%s answered with id %d and was asked with id %d",
			m, resp.ID, req.ID)
	}
	if resp.Error != nil {
		return resp.Result, resp.Error
	}
	return resp.Result, nil
}

// Ask es [Client.Call] con el resultado ya deserializado.
//
// Devuelve el valor AUNQUE haya error, por lo mismo que [Client.Call]: la
// expulsión a medias trae las dos cosas y las dos hacen falta.
func Ask[T any](c *Client, m protocol.Method, params any) (T, error) {
	var out T
	raw, err := c.Call(m, params)
	if len(raw) > 0 {
		if e := json.Unmarshal(raw, &out); e != nil && err == nil {
			return out, fmt.Errorf("parsing the answer to %s: %w", m, e)
		}
	}
	return out, err
}

// Code saca el código cerrado de un error del daemon.
//
// Vacío significa que el error NO viene del daemon: es del canal, del plazo o de
// la serialización. La distinción importa porque un `no_room` es una respuesta
// del producto que se le enseña al usuario, y un canal caído es otra cosa muy
// distinta que además se arregla de otra manera.
func Code(err error) protocol.Code {
	var e *protocol.Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}
