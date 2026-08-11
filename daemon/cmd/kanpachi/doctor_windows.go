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
	return "En Windows el token lo puede leer cualquier usuario de la máquina,\n" +
		"así que esto no debería pedir elevación. Si la pide, la ACL de\n" +
		"ProgramData\\Kanpachi no es la que puso el instalador."
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
		nombre: "el directorio de datos",
		mirar: func(_ context.Context, op opciones) veredicto {
			info, err := os.Stat(op.datos)
			if os.IsNotExist(err) {
				return fallar("no está %s", op.datos).
					con("Lo crea el instalador, con una ACL propia. Reinstalar lo repone.")
			}
			if err != nil {
				return noSeSabe("no se pudo mirar %s: %v", op.datos, err)
			}
			if !info.IsDir() {
				return fallar("%s existe y no es un directorio", op.datos)
			}
			return ok("%s", op.datos)
		},
	}
}
