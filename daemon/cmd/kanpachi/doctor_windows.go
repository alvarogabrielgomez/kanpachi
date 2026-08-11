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
// Lo que NO se replica es el nivel del sistema —el nodo de TUN, la
// configuración del kernel, las unidades— porque en Windows esas cosas no son
// ficheros que se puedan mirar sin privilegios ni tienen un arreglo que se
// escriba en una línea. Inventar comprobaciones que contesten "no se sabe"
// llenaría la pantalla de ruido y escondería las tres que sí contestan.

import (
	"context"
	"os"
	"path/filepath"
)

func chequeosDelSistema() []chequeo {
	return []chequeo{
		chequeoDelDirectorioDeDatos(),
		chequeoDelCanal(),
		chequeoDelMotor(motorAlLadoDelDaemon()),
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
