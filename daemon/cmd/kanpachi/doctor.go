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
		return "AVISO"
	case estadoMal:
		return "MAL "
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
			return uso("doctor no entiende %q. Solo acepta --fix", a)
		}
	}

	fmt.Println("EL ENTORNO")
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
		fmt.Println("\nARREGLANDO LO NUESTRO")
		for _, c := range pendientes {
			if err := c.arreglar(ctx, op); err != nil {
				fmt.Printf("  %s %-30s no se pudo: %v\n", estadoMal.marca(), c.nombre, err)
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
		fmt.Printf("\n  %d de esas las puede arreglar `kanpachi doctor --fix`.\n", len(pendientes))
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
		fmt.Println("\nLO QUE MIDE EL DAEMON")
		fmt.Println("  no se pudo preguntar:", err)
	}

	switch peor {
	case estadoBien:
		fmt.Println("\nTodo lo que se pudo comprobar está bien.")
		return nil
	case estadoAviso, estadoNoSeSabe:
		fmt.Println("\nHay cosas que mirar. Nada impide abrir una sala.")
		return nil
	default:
		return negativa("hay algo roto. Está arriba, marcado MAL")
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

	fmt.Println("\nLO QUE MIDE EL DAEMON")

	st, err := client.Ask[protocol.RoomView](c, protocol.MethodStatus, nil)
	if err != nil {
		return err
	}
	if st.Conn == "idle" || st.Conn == "" {
		fmt.Println("  No hay sala abierta, así que no hay nada que medir de ella.")
	} else {
		fmt.Printf("  Sala %s, %s, %d miembros\n", st.Name, estadoDeConexión(st.Conn), len(st.Peers))
		if st.Canary.Measured {
			fmt.Printf("  Protección Kanpachi: %s\n", veredictoDelCanario(st.Canary.Verdict))
		}
		for _, a := range st.Alerts {
			fmt.Printf("  AVISO %s: %s\n", nombreDeAlerta(a.Kind), a.Detail)
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
		fmt.Println("  NO se pudo leer el firewall. Esto no dice que no haya nada abierto:")
		fmt.Println("  dice que no se sabe.")
		return nil
	}
	fmt.Printf("  Compuerta: %s, con %d puertos abiertos\n", estadoDeCompuerta(exp.Gate), len(exp.Ports))
	for _, u := range exp.Unexpected {
		fmt.Printf("  REGLA QUE NADIE PIDIÓ: %s\n", u)
	}
	fmt.Println("\n  `kanpachi exposure` los enseña uno a uno. `kanpachi diag` mide la red.")
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
		nombre: "el canal de control",
		mirar: func(_ context.Context, op opciones) veredicto {
			c, err := client.Open(op.canal, op.datos)
			if err == nil {
				_ = c.Close()
				return ok("%s contesta", op.canal)
			}
			if os.IsPermission(err) {
				return fallar("no hay permiso para %s", op.canal).con(pistaDeElevación())
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
		nombre: "el motor",
		mirar: func(context.Context, opciones) veredicto {
			info, err := os.Stat(ruta)
			if os.IsNotExist(err) {
				return fallar("no está en %s", ruta).
					con("Lo pone el paquete. Reinstalar lo repone.")
			}
			if err != nil {
				return noSeSabe("no se pudo mirar %s: %v", ruta, err)
			}
			if info.Mode()&0o111 == 0 {
				return fallar("%s no es ejecutable (modo %04o)", ruta, info.Mode().Perm())
			}
			return ok("%s, %d KiB", ruta, info.Size()/1024)
		},
	}
}
