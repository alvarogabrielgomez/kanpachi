//go:build linux

package main

// El daemon como unidad de systemd.
//
// # El paralelo con Windows, que es exacto donde importa
//
// El Administrador de servicios exige que el arranque DEVUELVA cuando el daemon
// está listo, y que esperar y apagar sean dos cosas separadas. systemd con
// `Type=notify` exige lo mismo dicho de otra forma: el proceso no devuelve, y
// avisa con un datagrama cuando está listo. En los dos casos, **lo que hay que
// respetar es que el aviso llegue DESPUÉS de que el canal local escuche**, y no
// antes, porque hasta ese momento cualquier cliente que arranque contra este
// servicio se encuentra la puerta cerrada.
//
// Ese orden no vive acá: vive en `daemon/service/arranque.go`, que es puro y se
// prueba en los dos sistemas. Lo único de este fichero es cómo se avisa.
//
// # Por qué el aviso se escribe a mano y no con una librería
//
// Porque son veinte líneas: abrir un socket de datagramas Unix y mandar una
// cadena. `go-systemd` trae eso mismo, más un árbol de dependencias para hablar
// D-Bus, activación por socket y journal, que este daemon no usa. El protocolo
// está congelado hace más de una década y su formato es texto plano.

import (
	"context"
	"fmt"
	"github.com/accentiostudios/kanpachi/daemon/preflight"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
)

// ServiceName es el nombre de la unidad, sin el `.service`.
//
// El mismo que en Windows tiene el servicio registrado, y por lo mismo: es el
// nombre que el usuario escribe cuando algo va mal. Tiene que coincidir con el
// del fichero que instala el paquete.
// ServiceName sale de [preflight.DaemonService], por lo mismo que en Windows.
const ServiceName = preflight.DaemonService

// ArgShow existe para que el nombre sea único en el programa.
//
// En Windows es cómo el lanzador le dice al servicio "abrila con ventana". Acá
// no hay ventana que abrir: en Linux la interfaz es el CLI, que es un proceso
// que arranca el usuario y no algo que el daemon lance en la sesión de nadie.
const ArgShow = "--show"

// notifySocket es la variable con la que systemd dice dónde escuchar.
//
// Su presencia es también CÓMO se sabe que systemd nos arrancó con
// `Type=notify`, que es la pregunta que contesta [EnServicio]. La alternativa
// sería `INVOCATION_ID`, que systemd pone en toda unidad: sirve para saber que
// nos arrancó él, y no para saber si está ESPERANDO un aviso. Y lo que decide
// qué camino tomar es justamente eso: si nadie espera el aviso, el camino
// normal funciona; si alguien lo espera y no llega, systemd nos mata por no
// arrancar.
const notifySocket = "NOTIFY_SOCKET"

// EnServicio le pregunta al SISTEMA, no a la bandera.
//
// Igual que en Windows: arrancar como servicio y arrancar a mano se distinguen
// por CÓMO entró el proceso, no por lo que alguien escribió en la línea de
// comandos. Acá lo dice el entorno que preparó systemd antes de ejecutarnos.
func EnServicio() (bool, error) { return os.Getenv(notifySocket) != "", nil }

// CorrerComoServicio arranca, avisa que está listo, y espera a que lo paren.
//
// Recibe la misma función que el camino de Windows y por la misma razón: el
// cableado se escribe UNA vez. Lo que cambia es quién manda la señal de parada,
// que acá es systemd con un SIGTERM.
//
// La lista de argumentos va vacía a propósito. En Windows los argumentos del
// servicio son la única vía que el SCM tiene para pasar algo en el momento de
// arrancar, y el lanzador los usa. systemd no tiene ese canal: lo que haya que
// pasar está en el `ExecStart` de la unidad y ya lo leyó `flag.Parse`.
func CorrerComoServicio(arrancar func(context.Context, []string) (func() error, func(), error)) error {
	ctx, parar := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer parar()

	esperar, apagar, err := arrancar(ctx, nil)
	if err != nil {
		// Se le dice a systemd POR QUÉ, y no solo que se falló. Es lo que sale
		// en `systemctl status` sin tener que ir al journal, y en un servidor
		// ajeno esa diferencia es la mitad del diagnóstico.
		_ = notify("STATUS=" + primeraLínea(err.Error()))
		return err
	}

	// READY=1 va DESPUÉS de que arrancar haya vuelto, o sea con el canal local
	// ya escuchando. Ver la cabecera.
	if err := notify("READY=1", "STATUS=la sala está lista para recibir órdenes"); err != nil {
		// No es fatal. Un aviso que no llega hace que systemd nos dé por no
		// arrancados y termine matándonos, y hasta entonces el daemon funciona:
		// abortar acá sería adelantar el daño y perder la línea que lo explica.
		fmt.Fprintln(os.Stderr, "kanpachid: no se pudo avisar a systemd:", err)
	}

	go func() {
		<-ctx.Done()
		// STOPPING=1 antes de empezar, no después: es lo que hace que
		// `systemctl stop` muestre "deactivating" en vez de parecer colgado
		// mientras se cierra la sala y se purga el firewall.
		_ = notify("STOPPING=1", "STATUS=cerrando la sala y quitando las reglas")
		apagar()
	}()
	return esperar()
}

// notify manda una o más asignaciones a systemd.
//
// Que no haya socket NO es un error: es el caso de correr a mano, y ahí no hay
// nadie escuchando. Devolver error entonces obligaría a cada llamador a
// distinguir "no pude" de "no hacía falta".
func notify(estados ...string) error {
	ruta := os.Getenv(notifySocket)
	if ruta == "" {
		return nil
	}
	// Un socket abstracto se escribe con `@` al principio y se abre con un byte
	// cero. Es la forma que systemd usa por omisión, así que este caso es el
	// normal y no el raro.
	if strings.HasPrefix(ruta, "@") {
		ruta = "\x00" + ruta[1:]
	}

	conn, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: ruta, Net: "unixgram"})
	if err != nil {
		return fmt.Errorf("abriendo %s: %w", notifySocket, err)
	}
	defer func() { _ = conn.Close() }()

	if _, err := conn.Write([]byte(strings.Join(estados, "\n"))); err != nil {
		return fmt.Errorf("avisando a systemd: %w", err)
	}
	return nil
}

// primeraLínea recorta un error a lo que cabe en una línea de estado.
//
// Los errores de este daemon son largos a propósito y llevan la explicación
// debajo. `systemctl status` muestra una línea, así que mandarlo entero deja el
// resto cortado por el medio, y lo que se pierde es la primera frase, que es la
// que dice qué pasó.
func primeraLínea(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// tiene dice si el argumento está en la lista.
func tiene(args []string, quéBusco string) bool {
	for _, a := range args {
		if a == quéBusco {
			return true
		}
	}
	return false
}

// Autostart dice si la unidad arranca sola con la máquina.
//
// # Por qué se le pregunta a systemctl y no se mira el disco
//
// Porque "está habilitada" no es un fichero: es el resultado de mirar los
// enlaces en varios directorios, más las unidades que la puedan estar
// arrastrando, más si la unidad está enmascarada. `systemctl is-enabled` es la
// única respuesta que coincide con lo que va a pasar en el próximo arranque.
//
// Shelling out acá es aceptable por lo mismo que en `netcfg` de Windows se
// llama a `dism`: la alternativa es hablar D-Bus con systemd, que es mucho más
// código para la misma respuesta, y esto es NUESTRA unidad, no la del operador.
func (h *procesoHost) Autostart() (bool, error) {
	salida, err := exec.Command("systemctl", "is-enabled", ServiceName).Output()
	estado := strings.TrimSpace(string(salida))
	if err != nil {
		// `is-enabled` sale con código distinto de cero cuando la respuesta es
		// "disabled", que no es un fallo: es la respuesta. Lo que sí es fallo es
		// no haber contestado nada.
		if estado == "" {
			return false, fmt.Errorf("preguntando si %s arranca sola: %w", ServiceName, err)
		}
	}
	// `enabled-runtime` es habilitada solo hasta el próximo reinicio, o sea que
	// NO arranca con la máquina. Contestar que sí ahí sería la respuesta que el
	// usuario descubre equivocada justo cuando reinicia el servidor.
	return estado == "enabled" || estado == "alias" || estado == "static", nil
}

// SetAutostart la habilita o la deshabilita.
//
// Sin `--now`: encender el arranque automático y arrancar el servicio son dos
// cosas distintas, y quien llama a esto ya tiene el daemon corriendo, porque
// esta llamada le llegó por el canal local.
func (h *procesoHost) SetAutostart(quiero bool) error {
	verbo := "disable"
	if quiero {
		verbo = "enable"
	}
	salida, err := exec.Command("systemctl", verbo, ServiceName).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w: %s", verbo, ServiceName, err, strings.TrimSpace(string(salida)))
	}
	return nil
}
