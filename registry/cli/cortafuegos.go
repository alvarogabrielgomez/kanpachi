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
		tenue("  ufw no está instalado, así que no filtra el puerto del motor")

	case !r.Legible:
		aviso("ufw está instalado pero no puedo leer sus reglas sin root")
		codigo("sudo kanpseed doctor")

	case !r.Activo:
		ok("ufw está inactivo: nada bloquea el puerto %d", cfg.PuertoMotor)

	case r.TCP && r.UDP:
		ok("ufw deja entrar el %d por TCP y UDP", cfg.PuertoMotor)

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
		malo("ufw está activo y no deja entrar el %d por %s: los clientes no van a poder conectarse",
			cfg.PuertoMotor, strings.Join(faltan, " ni "))
		tenue("  con Docker esto funcionaba porque publicar un puerto se salta ufw. Nativo no.")
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
	aviso("ufw está activo y no deja entrar el puerto %d del motor", cfg.PuertoMotor)
	tenue("  el seed está arriba, pero ningún cliente va a poder conectarse hasta que")
	tenue("  abras el puerto. No lo hago yo: abrir al mundo lo decides tú.")
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
