package main

import (
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/accentiostudios/kanpachi/daemon/transport/pipe"
	"github.com/accentiostudios/kanpachi/daemon/transport/protocol"
)

// El modo LANZADOR: este mismo binario, arrancado por una persona.
//
// # Por qué un solo ejecutable con tres papeles
//
// Porque el acceso directo apunta al daemon y el daemon es un servicio, y un
// servicio no lo arranca un doble clic: lo arranca el Administrador de
// servicios. Alguien tiene que pedírselo, y ese alguien es este mismo binario
// con un parámetro. Es el patrón de Half-Life y Counter-Strike, el mismo motor
// con banderas distintas, y evita el ejecutable auxiliar que habría que
// mantener sincronizado con este.
//
//	kanpachid.exe             lo arranca el SCM. ES el daemon
//	kanpachid.exe --show      lanzador, y además abre la ventana
//	kanpachid.exe --console   daemon de consola, para desarrollar
//	kanpachid.exe --daemon    el daemon de una carpeta portable, sin SCM
//
// # Nunca un segundo daemon
//
// Lo primero que hace es preguntar si ya hay uno, y lo pregunta por el pipe.
// Podría preguntárselo al SCM, que es más directo, y sería un mecanismo más que
// mantener: el pipe es a la vez la detección y la entrega. Si contesta, ya está
// todo dicho y encima hay por dónde mandarle la orden de mostrarse; si no hay
// nadie, `CreateFile` falla al instante con ERROR_FILE_NOT_FOUND, que es
// justo el caso que tiene que ser rápido.
//
// Y es self-correcting: si la sonda falla porque el daemon está a mitad de
// arrancar, el arranque siguiente devuelve ERROR_SERVICE_ALREADY_RUNNING y se
// vuelve al pipe. Ninguna rama termina en dos daemons.

// probeWait es lo que se espera a la sonda del pipe.
//
// Corto a propósito: alguien acaba de hacer doble clic y está mirando. Si el
// daemon está vivo, contesta en milisegundos; si no está, el fallo es
// inmediato. Lo único que este plazo cubre es una máquina con el disco ocupado.
const probeWait = 750 * time.Millisecond

// callWait es lo que se espera a la respuesta de un método.
const callWait = 5 * time.Second

// readyWait es cuánto se insiste con el pipe después de mandar arrancar el
// servicio.
//
// Un servicio recién arrancado tarda: purga el firewall y levanta el motor
// antes de abrir el pipe, y hasta que no lo abre no hay a quién hablarle. Este
// plazo solo se paga en el camino de "estaba arrancando cuando llegué", que es
// una carrera rara, no el caso normal.
const (
	readyWait  = 20 * time.Second
	readyRetry = 400 * time.Millisecond
)

// abrir es el modo lanzador: deja a Kanpachi corriendo y, si se pide, con la
// ventana a la vista.
//
// `enlace` es el `kanpachi://` que trajo el navegador, o vacío. Viaja hasta el
// daemon por dos vías distintas según quién lo levante, y las dos existen
// porque el daemon puede estar vivo o no: por el pipe cuando ya está, y por
// los argumentos de arranque cuando hay que levantarlo. En los dos casos
// termina en el mismo buzón, ver [procesoHost].
func abrir(datos string, mostrar bool, enlace string) error {
	// 1. ¿Ya hay daemon? Si lo hay, esto es todo lo que hay que hacer.
	if conn, err := marcarPipe(probeWait); err == nil {
		defer func() { _ = conn.Close() }()
		if !mostrar {
			return nil
		}
		return decirShow(conn, datos, enlace)
	}

	// 2. No hay, y esto es una carpeta portable: no hay SCM a quien pedírselo,
	// así que el daemon lo levanta este mismo binario en otro proceso.
	//
	// **Otro proceso y no este, porque hace falta elevar.** Crear el pipe bajo
	// `ProtectedPrefix\Administrators` y escribir en el firewall exigen
	// administrador, y a este proceso lo arrancó un doble clic sin elevar. La
	// única forma de subir es relanzarse, que es lo que hace [ArrancarSuelto].
	//
	// Aquí SÍ hay un UAC, y es la diferencia honesta entre portable e
	// instalado: el instalador pide uno solo, al instalar, y a cambio concede
	// el permiso de arranque; una carpeta que se copió no concedió nada, así
	// que paga un UAC por arranque de Kanpachi.
	if esPortable() {
		// No se le insiste por el pipe después: el daemon que acaba de nacer
		// abre la ventana él mismo si se lo pidieron, igual que en el camino
		// del servicio.
		return ArrancarSuelto(argsDeArranque(mostrar, enlace))
	}

	// 3. No hay. Que lo arranque el Administrador de servicios.
	//
	// El argumento viaja por `StartService` y no por la línea de comandos de
	// este proceso: el daemon lo va a recibir como argumento del SERVICIO, que
	// es la única vía que el SCM tiene de pasarle algo. Ver [ArgShow].
	yaEstaba, err := ArrancarServicio(argsDeArranque(mostrar, enlace))
	if err != nil {
		return err
	}
	if !yaEstaba {
		// Arrancó recién, y el propio daemon abre la ventana si se lo pidieron.
		// Insistirle por el pipe sería pedir dos veces lo mismo.
		return nil
	}

	// 4. Estaba arrancando cuando llegamos. Se espera al pipe y se pide.
	if !mostrar {
		return nil
	}
	conn, err := esperarPipe()
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	return decirShow(conn, datos, enlace)
}

// argsDeArranque son los argumentos con los que se levanta un daemon nuevo.
//
// El enlace va DESPUÉS de la bandera y sin bandera propia, que es la forma en
// que lo recibe este mismo binario desde el manejador de protocolo: Windows
// invoca `kanpachid.exe --show "%1"`. Una bandera propia sería una segunda
// forma de decir lo mismo, y la que se olvidara de actualizarse sería la que
// deja de funcionar.
func argsDeArranque(mostrar bool, enlace string) []string {
	var args []string
	if mostrar {
		args = append(args, ArgShow)
	}
	if enlace != "" {
		args = append(args, enlace)
	}
	return args
}

// esperarPipe insiste hasta que el daemon abra la puerta.
func esperarPipe() (net.Conn, error) {
	hasta := time.Now().Add(readyWait)
	for {
		conn, err := marcarPipe(probeWait)
		if err == nil {
			return conn, nil
		}
		if time.Now().After(hasta) {
			return nil, fmt.Errorf("el servicio de Kanpachi está corriendo y no abrió su canal en %s: %w",
				readyWait, err)
		}
		time.Sleep(readyRetry)
	}
}

// decirShow saluda y pide que la ventana se vea.
//
// **Es lo ÚNICO que este camino le pide al daemon**, y esa es la propiedad que
// importa: un proceso que llega de fuera puede pedir que se muestre una ventana
// y nada más. Lo que acota el resto no es esta función, es la lista cerrada de
// métodos de `protocol`, y el saludo con token de `pipe`.
func decirShow(conn net.Conn, datos, enlace string) error {
	// El token se relee del disco en cada conexión y jamás se recuerda: el
	// daemon lo rota en cada arranque suyo.
	token, err := pipe.ReadToken(datos)
	if err != nil {
		return err
	}

	w := protocol.NewWriter(conn)
	r := protocol.NewReader(conn)

	pedir := func(id uint64, m protocol.Method, params json.RawMessage) error {
		if err := w.Write(protocol.Request{ID: id, Method: m, Params: params}); err != nil {
			return fmt.Errorf("pidiéndole %s al daemon: %w", m, err)
		}
		_ = conn.SetReadDeadline(time.Now().Add(callWait))
		linea, err := r.ReadLine()
		if err != nil {
			return fmt.Errorf("esperando la respuesta a %s: %w", m, err)
		}
		var resp protocol.Response
		if err := json.Unmarshal(linea, &resp); err != nil {
			return fmt.Errorf("la respuesta a %s no se entiende: %w", m, err)
		}
		if resp.Error != nil {
			return fmt.Errorf("el daemon rechazó %s: %s", m, resp.Error.Message)
		}
		return nil
	}

	saludo, err := json.Marshal(struct {
		Token string `json:"token"`
	}{token})
	if err != nil {
		return err
	}
	if err := pedir(1, protocol.MethodHello, saludo); err != nil {
		return err
	}

	// El enlace viaja DENTRO de show_ui y no en un método propio, y esa es la
	// propiedad que se conserva: este camino le sigue pidiendo al daemon una
	// sola cosa, abrir la ventana. Lo que se agrega es con qué abrirla.
	mostrar, err := json.Marshal(struct {
		Link string `json:"link,omitempty"`
	}{enlace})
	if err != nil {
		return err
	}
	return pedir(2, protocol.MethodShowUI, mostrar)
}
