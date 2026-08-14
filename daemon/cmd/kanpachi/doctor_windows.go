//go:build windows

package main

// Las comprobaciones de Windows, que son MENOS que las de Linux a propósito.
//
// # Por qué menos, y por qué eso no es un pendiente
//
// Porque en Windows el cliente normal es la ventana, y la ventana ya enseña casi
// todo esto en sus propias pantallas: el estado del servicio, la exposición, la
// Protección Kanpachi. Doctor existe acá para el caso en que alguien esté en una
// terminal, y lo que hace falta ahí es lo mismo que en Linux: saber si el canal
// contesta y si las piezas están donde tienen que estar.
//
// Lo que NO se replica es la configuración del sistema operativo, porque en
// Windows no son ficheros que se puedan mirar ni tienen un arreglo que se
// escriba en una línea. Inventar comprobaciones que contesten "no se sabe"
// llenaría la pantalla de ruido y escondería las que sí contestan.
//
// # Lo que ese razonamiento decía de más, y quedó desmentido
//
// Decía que en Windows NO hay nivel de sistema que mirar, y por eso acá no había
// nada equivalente a `/dev/net/tun`. Lo hay: el driver de red virtual, que se
// instala en el almacén de drivers y puede negarse a hacerlo. Medido el
// 2026-08-11 en la máquina de un invitado, con todos los ficheros en su sitio y
// sin adaptador posible. Ver `chequeoDelAdaptadorVirtual`.

import (
	"context"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"

	kanpachiengine "github.com/accentiostudios/kanpachi/daemon/adapter/engine/kanpachi"
	"github.com/accentiostudios/kanpachi/daemon/preflight"
)

func chequeosDelSistema() []chequeo {
	return []chequeo{
		chequeoDelServicio(),
		chequeoDelDirectorioDeDatos(),
		chequeoDelCanal(),
		chequeoDelMotor(motorAlLadoDelDaemon()),
		chequeoDelAdaptadorVirtual(),
	}
}

// chequeoDelServicio asks the same question roomprobe asks before running, from
// the same code, so the two can never disagree about what "running" means.
func chequeoDelServicio() chequeo {
	return chequeo{
		nombre: "the service",
		mirar: func(context.Context, opciones) veredicto {
			corriendo, err := preflight.DaemonServiceRunning()
			if err != nil {
				return noSeSabe("could not ask the service manager: %v", err)
			}
			if !corriendo {
				return fallar("%s is not running", preflight.DaemonService).
					con("Start-Service " + preflight.DaemonService)
			}
			return ok("%s is running", preflight.DaemonService)
		},
	}
}

// chequeoDelAdaptadorVirtual es el análogo de `/dev/net/tun` en Windows.
//
// Corre el MISMO sondeo que el daemon corre antes de levantar el motor, y eso es
// lo que lo hace valer: una comprobación escrita aparte contestaría sobre otra
// cosa parecida, y la que importa es la que el producto va a hacer de verdad.
//
// # Por qué se rinde sin elevación en vez de fallar
//
// Porque crear un adaptador es una operación privilegiada, y sin permisos el
// sondeo devuelve acceso denegado. Eso NO significa que la máquina esté rota,
// significa que esta corrida no puede saberlo. Doctor se lee cuando algo va mal,
// y un rojo que solo dice que lo corriste como tú mismo mandaría a la gente a
// buscar un problema que no existe. Es la única comprobación de este binario que
// pide elevación, y por eso lo dice en vez de suponerlo.
//
// **No tiene arreglo**, y es la misma regla de siempre: el almacén de drivers de
// Windows es del sistema y no nuestro. Se nombra, se dice qué mirar, y se para.
func chequeoDelAdaptadorVirtual() chequeo {
	return chequeo{
		nombre: "the virtual adapter",
		mirar: func(context.Context, opciones) veredicto {
			if !windows.GetCurrentProcessToken().IsElevated() {
				return noSeSabe("creating an adapter needs elevation, so this one " +
					"cannot be answered from here").
					con("Run this in an elevated terminal to have it checked.")
			}
			dir := filepath.Dir(motorAlLadoDelDaemon())
			if err := kanpachiengine.Preflight(dir); err != nil {
				return fallar("this machine cannot build one").con(err.Error())
			}
			return ok("built one and took it down again")
		},
	}
}

func pistaDeElevación() string {
	return "On Windows any user of the machine can read the token,\n" +
		"so this should not need elevation. If it does, the ACL on\n" +
		"ProgramData\\Kanpachi is not the one the installer put there."
}

// motorAlLadoDelDaemon es donde vive el motor en una instalación de Windows.
//
// Se busca junto a ESTE binario porque el paquete los pone juntos, que es lo
// mismo que hace el daemon para lanzarlo. En el portable eso sigue siendo cierto
// y en el instalado también, así que no hace falta leer el registro.
func motorAlLadoDelDaemon() string {
	exe, err := os.Executable()
	if err != nil {
		return "kanpachi-engine.exe"
	}
	return filepath.Join(filepath.Dir(exe), "kanpachi-engine.exe")
}

// chequeoDelDirectorioDeDatos mira que esté, y NO lo crea.
//
// Crearlo sería el arreglo equivocado: lo crea el instalador con una ACL propia,
// y esa ACL es la mitad de la protección de todo lo que hay dentro. Un
// directorio hecho acá saldría con la ACL heredada y la perdería en silencio, que
// es peor que no tenerlo. Por eso este chequeo no lleva `arreglar`.
func chequeoDelDirectorioDeDatos() chequeo {
	return chequeo{
		nombre: "the data directory",
		mirar: func(_ context.Context, op opciones) veredicto {
			info, err := os.Stat(op.datos)
			if os.IsNotExist(err) {
				return fallar("%s is not there", op.datos).
					con("The installer creates it, with an ACL of its own. Reinstalling puts it back.")
			}
			if err != nil {
				return noSeSabe("could not look at %s: %v", op.datos, err)
			}
			if !info.IsDir() {
				return fallar("%s exists and is not a directory", op.datos)
			}
			return ok("%s", op.datos)
		},
	}
}
