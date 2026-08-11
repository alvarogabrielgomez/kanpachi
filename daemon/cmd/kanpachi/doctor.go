package main

// `kanpachi doctor`: qué hace falta para que esto funcione, y qué está mal.
//
// # Corre en dos niveles, y la separación es el punto
//
// Doctor tiene que servir JUSTO cuando el daemon no arranca. Así que lo del
// entorno lo mira él mismo, en local, sin daemon: el nodo de TUN, el kernel, las
// unidades, el socket, el motor. Lo que necesita medición de red se lo pide al
// daemon por los métodos que ya existen, y solo si contesta.
//
// Lo segundo NO se reimplementa, y decirlo importa porque la tentación es
// grande: `diag_report` ya trae el NAT y el MTU desde el motor, `exposure` ya
// dice si la compuerta está puesta. Doctor los presenta. Un segundo medidor
// daría un segundo número distinto y nadie sabría cuál creer.
//
// # Por omisión NO escribe nada
//
// Arreglar es `doctor --fix`. Un diagnóstico que modifica la máquina no se puede
// correr para entender qué pasa, que es exactamente para lo que se corre.
//
// # La regla del arreglo: solo se toca lo NUESTRO
//
// Nuestras unidades, nuestras tablas, nuestro socket, nuestro nodo de
// dispositivo. Lo del operador —ufw, firewalld, Docker, el kernel— se reporta
// con el comando exacto y no se ejecuta jamás, ni con `--fix`. Es la misma regla
// que hace que `SuspendForeign` niegue en Linux y la misma que llevó a bifurcar
// EasyTier: las dos llamadas que se le quitaron escribían reglas permanentes en
// el firewall de quien lo corría.

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/accentiostudios/kanpachi/daemon/transport/client"
	"github.com/accentiostudios/kanpachi/daemon/transport/protocol"
)

type estado int

const (
	estadoBien estado = iota
	estadoAviso
	estadoMal
	// estadoNoSeSabe es que la comprobación no se pudo hacer, que NO es lo
	// mismo que salir bien. Se enseña distinto a propósito: leerla como buena
	// es exactamente el error que este producto no puede cometer.
	estadoNoSeSabe
)

func (e estado) marca() string {
	switch e {
	case estadoBien:
		return "OK  "
	case estadoAviso:
		return "WARN"
	case estadoMal:
		return "BAD "
	default:
		return "?   "
	}
}

// veredicto es cómo está una cosa.
type veredicto struct {
	estado  estado
	detalle string
	// comando es lo que tiene que ejecutar la PERSONA, y es lo que sustituye al
	// arreglo automático en todo lo que no es nuestro. Un diagnóstico que dice
	// "tienes ufw bloqueando" sin decir qué escribir deja el trabajo a medias.
	comando string
}

func ok(f string, a ...any) veredicto {
	return veredicto{estado: estadoBien, detalle: fmt.Sprintf(f, a...)}
}
func avisar(f string, a ...any) veredicto {
	return veredicto{estado: estadoAviso, detalle: fmt.Sprintf(f, a...)}
}
func fallar(f string, a ...any) veredicto {
	return veredicto{estado: estadoMal, detalle: fmt.Sprintf(f, a...)}
}
func noSeSabe(f string, a ...any) veredicto {
	return veredicto{estado: estadoNoSeSabe, detalle: fmt.Sprintf(f, a...)}
}

// con le pega a un veredicto el comando que lo arregla a mano.
func (v veredicto) con(comando string) veredicto { v.comando = comando; return v }

// chequeo es una comprobación con su nombre y, si es nuestra, su arreglo.
type chequeo struct {
	nombre string
	// mirar contesta cómo está. **Nunca escribe**, ni siquiera con `--fix`.
	mirar func(ctx context.Context, op opciones) veredicto
	// arreglar es nil en todo lo que no es nuestro, y esa ausencia es la regla
	// convertida en algo que no se puede saltar sin querer.
	arreglar func(ctx context.Context, op opciones) error
}

func cmdDoctor(ctx context.Context, op opciones, args []string) error {
	arreglar := false
	for _, a := range args {
		switch a {
		case "--fix", "-fix":
			arreglar = true
		default:
			return uso("doctor does not understand %q. It only takes --fix", a)
		}
	}

	fmt.Println("THE ENVIRONMENT")
	medidos := map[string]veredicto{}
	pendientes := []chequeo{}
	for _, c := range chequeosDelSistema() {
		v := c.mirar(ctx, op)
		imprimirVeredicto(c.nombre, v)
		medidos[c.nombre] = v
		if v.estado == estadoMal && c.arreglar != nil {
			pendientes = append(pendientes, c)
		}
	}

	if arreglar && len(pendientes) > 0 {
		fmt.Println("\nFIXING WHAT IS OURS")
		for _, c := range pendientes {
			if err := c.arreglar(ctx, op); err != nil {
				fmt.Printf("  %s %-30s could not: %v\n", estadoMal.marca(), c.nombre, err)
				continue
			}
			// Se vuelve a MIRAR en vez de dar por bueno el arreglo: un comando que
			// devuelve cero y no cambia nada es un caso real, y darlo por arreglado
			// sería el peor resultado posible de un diagnóstico. El veredicto que
			// cuenta pasa a ser este.
			v := c.mirar(ctx, op)
			imprimirVeredicto(c.nombre, v)
			medidos[c.nombre] = v
		}
	} else if len(pendientes) > 0 {
		fmt.Printf("\n  `kanpachi doctor --fix` can fix %d of those.\n", len(pendientes))
	}

	// El resumen sale de lo medido, con los arreglados ya actualizados. No se
	// vuelve a mirar nada: en esta lista, repasar es media docena de procesos
	// hijo y una lectura del ruleset, y ya está todo contestado.
	peor := estadoBien
	for _, v := range medidos {
		if v.estado > peor {
			peor = v.estado
		}
	}

	if err := loQueMideElDaemon(ctx, op); err != nil {
		fmt.Println("\nWHAT THE DAEMON MEASURES")
		fmt.Println("  could not ask:", err)
	}

	switch peor {
	case estadoBien:
		fmt.Println("\nEverything that could be checked is fine.")
		return nil
	case estadoAviso, estadoNoSeSabe:
		fmt.Println("\nThere are things to look at. Nothing stops a room from opening.")
		return nil
	default:
		return negativa("something is broken. It is above, marked BAD")
	}
}

func imprimirVeredicto(nombre string, v veredicto) {
	fmt.Printf("  %s %-30s %s\n", v.estado.marca(), nombre, v.detalle)
	if v.comando != "" {
		for _, línea := range strings.Split(v.comando, "\n") {
			fmt.Printf("       %s\n", línea)
		}
	}
}

// loQueMideElDaemon es el nivel 2: lo que ya está medido y no se recalcula.
//
// Se salta entero si el daemon no contesta, y sin ruido: que no conteste ya lo
// dijo el nivel 1, que mira la unidad y el socket. Repetirlo acá sería el mismo
// fallo contado dos veces con dos palabras distintas.
func loQueMideElDaemon(ctx context.Context, op opciones) error {
	c, err := client.Open(op.canal, op.datos)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	fmt.Println("\nWHAT THE DAEMON MEASURES")

	st, err := client.Ask[protocol.RoomView](c, protocol.MethodStatus, nil)
	if err != nil {
		return err
	}
	if st.Conn == "idle" || st.Conn == "" {
		fmt.Println("  No room is open, so there is nothing of it to measure.")
	} else {
		fmt.Printf("  Room %s, %s, %d members\n", st.Name, estadoDeConexión(st.Conn), len(st.Peers))
		if st.Canary.Measured {
			fmt.Printf("  Kanpachi Protection: %s\n", veredictoDelCanario(st.Canary.Verdict))
		}
		for _, a := range st.Alerts {
			fmt.Printf("  ALERT %s: %s\n", nombreDeAlerta(a.Kind), a.Detail)
		}
	}

	// La exposición se pide siempre, con sala y sin ella: lo que contesta es qué
	// tiene puesto Kanpachi en el firewall AHORA, y una compuerta que no está es
	// noticia aunque no haya nadie jugando.
	exp, err := client.Ask[protocol.ExposureView](c, protocol.MethodExposure, nil)
	if err != nil {
		return err
	}
	if !exp.Measured {
		fmt.Println("  The firewall could NOT be read. This does not say nothing is open:")
		fmt.Println("  it says nobody knows.")
		return nil
	}
	fmt.Printf("  Gate: %s, with %d open ports\n", estadoDeCompuerta(exp.Gate), len(exp.Ports))
	for _, u := range exp.Unexpected {
		fmt.Printf("  RULE NOBODY ASKED FOR: %s\n", u)
	}
	fmt.Println("\n  `kanpachi exposure` shows them one by one. `kanpachi diag` measures the network.")
	return nil
}

// ─── Trozos que sirven en los dos sistemas ───────────────────────────────────

// chequeoDelCanal mira si el canal de control se puede abrir.
//
// Es la comprobación que MÁS sirve, porque su fallo es el que se ve peor desde
// fuera: el socket no abre igual con el servicio parado que con el servicio
// corriendo y sin permiso, y quien lo sufre no tiene forma de distinguirlos.
func chequeoDelCanal() chequeo {
	return chequeo{
		nombre: "the control channel",
		mirar: func(_ context.Context, op opciones) veredicto {
			c, err := client.Open(op.canal, op.datos)
			if err == nil {
				_ = c.Close()
				return ok("%s answers", op.canal)
			}
			if os.IsPermission(err) {
				return fallar("no permission for %s", op.canal).con(pistaDeElevación())
			}
			return fallar("%v", err).con(pistaDeConexión(op))
		},
	}
}

// chequeoDelMotor mira que el motor esté donde el daemon lo va a buscar.
//
// No se arregla: el motor lo pone el paquete, y un doctor que se pusiera a
// descargarlo estaría instalando software por su cuenta, que no es lo que
// alguien pide cuando escribe `doctor --fix`.
func chequeoDelMotor(ruta string) chequeo {
	return chequeo{
		nombre: "the engine",
		mirar: func(context.Context, opciones) veredicto {
			info, err := os.Stat(ruta)
			if os.IsNotExist(err) {
				return fallar("it is not at %s", ruta).
					con("The package puts it there. Reinstalling puts it back.")
			}
			if err != nil {
				return noSeSabe("could not look at %s: %v", ruta, err)
			}
			if info.Mode()&0o111 == 0 {
				return fallar("%s is not executable (mode %04o)", ruta, info.Mode().Perm())
			}
			return ok("%s, %d KiB", ruta, info.Size()/1024)
		},
	}
}
