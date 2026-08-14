// Command roombundle es roomprobe y todo lo que necesita, en UN solo fichero
// que se puede mandar por chat.
//
// # Por qué existe
//
// `roomprobe.exe` no se vale por sí mismo. Necesita al lado el motor
// (`kanpachi-engine.exe`, 31 MB), `wintun.dll` para crear el adaptador virtual
// y `Packet.dll`, que el motor importa de forma dura: sin ella no arranca, y
// por eso el adaptador del motor le fija el directorio de trabajo. Cinco
// ficheros que hay que mantener juntos es exactamente lo que no se puede pedir
// por chat a alguien que solo quiere ayudar a probar diez minutos.
//
// Esto los empotra a todos dentro de su propio ejecutable, los suelta en una
// carpeta temporal, corre roomprobe y borra la carpeta al terminar.
//
// # Dónde queda lo que importa
//
// `roomprobe.log` y la carpeta `roomprobe-data` quedan **junto a este
// ejecutable**, no en el temporal. Dentro de esa carpeta viven `identity.key`,
// que es la llave con la que esta máquina firma lo que hospeda, y
// `known-hosts.json`, la libreta de huellas: borrarla es presentarse con otra
// cara y olvidar a todo el mundo. No es una preferencia: roomprobe los pone
// junto a SU ejecutable, que dentro del bundle vive en la carpeta que este
// programa borra al salir. O sea que sin pasarle `-log` y `-data`, la limpieza
// destruiría el log justo cuando alguien lo iba a mandar por chat, y
// `last-room.json` moriría en cada corrida, dejando "volver a la última sala"
// sin funcionar nunca. Ver [argumentos].
//
// # Cómo se construye
//
// **No se construye con `go build ./...`**, y eso es deliberado. La carga vive
// detrás de la etiqueta `bundle`, así que sin ella este binario compila pero se
// niega a correr diciendo por qué. La alternativa era que un `go build` normal
// produjera un bundle vacío que falla en la máquina de otro, que es el peor
// sitio para descubrirlo.
//
//	scripts\build_test_tools.ps1        # deja roombundle.exe en testTools\
//
// El script copia los cinco ficheros a `internal/roombundle/carga/` y compila
// con `-tags bundle`. Si falta alguno, la compilación falla y lo dice.
//
// # Lo que hay que saber antes de mandarlo
//
// Va SIN FIRMAR, así que Windows lo va a recibir con "Editor desconocido" de
// SmartScreen, y puede que con un aviso de Defender: un ejecutable que suelta
// otros ejecutables y un driver en una carpeta temporal y pide administrador es,
// literalmente, la forma de un dropper. Quien lo reciba tiene que pulsar "Más
// información" y "Ejecutar de todas formas". Lo único que quita ese aviso es un
// certificado de firma de código, que cuesta dinero y no lo tenemos.
//
// Vive en `internal/` para que el producto no lo pueda importar y el instalador
// no lo distribuya, igual que el resto de las sondas.
package main

import (
	"fmt"
	"os"
)

func main() {
	if err := correr(); err != nil {
		fmt.Fprintln(os.Stderr, "\nroombundle:", err)
		pausar()
		os.Exit(1)
	}
}

// pausar existe porque esto se abre con doble clic.
//
// Sin ella, un fallo cierra la ventana antes de que nadie lea el motivo, y lo
// que llega de vuelta por el chat es "no anduvo".
func pausar() {
	fmt.Println("\nPulsa Enter para cerrar...")
	var nada string
	_, _ = fmt.Scanln(&nada)
}
