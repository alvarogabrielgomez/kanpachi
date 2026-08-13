package cli

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/accentiostudios/kanpachi/registry/setup"
)

// Este archivo existe por un fallo silencioso que solo aparece al salir de
// Docker, y que no deja rastro en ningún log.
//
// Docker no publica un puerto abriéndolo en el cortafuegos: inserta sus propias
// reglas de DNAT en la tabla nat y en la cadena DOCKER-USER de FORWARD, que se
// evalúan ANTES que las de ufw. Un contenedor con `ports: 11010:11010` queda
// alcanzable desde internet aunque ufw esté activo y niegue todo lo entrante.
//
// Un proceso nativo no tiene ese privilegio. El motor arranca, escucha en el
// 11010, `doctor` lo ve escuchando, systemd lo da por sano, y ningún cliente
// consigue entrar nunca. El síntoma es indistinguible de un problema de red del
// jugador, así que hay que decirlo aquí.

type reglasUFW struct {
	Instalado bool
	Activo    bool
	Legible   bool
	TCP, UDP  bool
}

// revisarCortafuegos comprueba que el puerto del motor pueda entrar de verdad.
//
// Solo se mira el del motor. El del registro va por el proxy inverso desde el
// propio host, o sea por loopback, que ufw no filtra.
func revisarCortafuegos(cfg setup.Config, malo func(string, ...any)) {
	r := leerUFW(cfg.PuertoMotor)

	switch {
	case !r.Instalado:
		tenue("  ufw is not installed, so it is not filtering the engine port")

	case !r.Legible:
		aviso("ufw is installed and its rules cannot be read without root")
		codigo("sudo kanpseed doctor")

	case !r.Activo:
		ok("ufw is inactive: nothing is blocking port %d", cfg.PuertoMotor)

	case r.TCP && r.UDP:
		ok("ufw lets %d in over TCP and UDP", cfg.PuertoMotor)

	default:
		// Que falte uno de los dos es peor que que falten los dos: el motor
		// negocia por TCP y hace hole punch por UDP, así que a medias funciona
		// para unos jugadores y para otros no, según la NAT de cada casa.
		faltan := []string{}
		if !r.TCP {
			faltan = append(faltan, "TCP")
		}
		if !r.UDP {
			faltan = append(faltan, "UDP")
		}
		malo("ufw is active and is not letting %d in over %s: no client will be able to connect",
			cfg.PuertoMotor, strings.Join(faltan, " ni "))
		tenue("  under Docker this worked because publishing a port steps around ufw. Native does not.")
		codigo(comandosUFW(cfg.PuertoMotor, r)...)
	}
}

// avisarCortafuegos es la versión para el final de init: no cuenta problemas ni
// falla, solo avisa. El instalador promete dejar todo listo, y abrir un puerto
// al mundo es lo único que no hace por su cuenta: es una decisión del dueño de
// la máquina, no del instalador.
func avisarCortafuegos(cfg setup.Config) {
	r := leerUFW(cfg.PuertoMotor)
	if !r.Instalado || !r.Legible || !r.Activo || (r.TCP && r.UDP) {
		return
	}
	fmt.Println()
	aviso("ufw is active and is not letting the engine port %d in", cfg.PuertoMotor)
	tenue("  the seed is up, and no client will be able to connect until you open")
	tenue("  the port. Not done for you: opening to the world is your call.")
	codigo(comandosUFW(cfg.PuertoMotor, r)...)
}

func comandosUFW(puerto int, r reglasUFW) []string {
	c := []string{}
	if !r.TCP {
		c = append(c, fmt.Sprintf("sudo ufw allow %d/tcp", puerto))
	}
	if !r.UDP {
		c = append(c, fmt.Sprintf("sudo ufw allow %d/udp", puerto))
	}
	return c
}

func leerUFW(puerto int) reglasUFW {
	r := reglasUFW{}
	if _, err := exec.LookPath("ufw"); err != nil {
		return r
	}
	r.Instalado = true

	// Sin root, ufw responde "ERROR: You need to be root to run this script" y
	// sale con código distinto de cero. Eso es ilegible, no "no hay reglas":
	// tratarlo como ausencia de reglas daría una alarma falsa a quien corra
	// doctor sin sudo.
	salida, err := exec.Command("ufw", "status").CombinedOutput()
	if err != nil {
		return r
	}
	r.Legible = true
	r.Activo, r.TCP, r.UDP = interpretarUFW(string(salida), puerto)
	return r
}

// interpretarUFW lee la salida de `ufw status`. Se separa de la ejecución para
// poder probarla con salidas reales sin tener ufw delante.
//
// El formato es:
//
//	Status: active
//
//	To                         Action      From
//	--                         ------      ----
//	11010/tcp                  ALLOW       Anywhere
//	11010                      ALLOW       Anywhere
//	11010/udp (v6)             ALLOW       Anywhere (v6)
func interpretarUFW(salida string, puerto int) (activo, tcp, udp bool) {
	if !strings.Contains(salida, "Status: active") {
		return false, false, false
	}
	activo = true
	p := strconv.Itoa(puerto)

	for _, linea := range strings.Split(salida, "\n") {
		campos := strings.Fields(linea)
		if len(campos) < 2 {
			continue
		}
		// La acción es el campo siguiente al destino, salvo cuando el destino
		// lleva "(v6)" pegado como campo aparte. DENY y REJECT no cuentan, y
		// LIMIT sí: deja pasar, solo que con freno.
		accion := campos[1]
		if accion == "(v6)" && len(campos) >= 3 {
			accion = campos[2]
		}
		if !strings.HasPrefix(accion, "ALLOW") && !strings.HasPrefix(accion, "LIMIT") {
			continue
		}

		destino := campos[0]
		switch destino {
		case p:
			// Un puerto sin protocolo abre los dos.
			tcp, udp = true, true
		case p + "/tcp":
			tcp = true
		case p + "/udp":
			udp = true
		}
	}
	return activo, tcp, udp
}
