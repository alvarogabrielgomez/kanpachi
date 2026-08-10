// Command kanpachibundle es Kanpachi portable entero, en UN solo ejecutable
// que se le puede pasar a un amigo por chat.
//
// # Qué problema resuelve
//
// Una carpeta portable funciona, pero son quince archivos que hay que mantener
// juntos: el daemon, la interfaz con TODO el bundle de Flutter —su DLL, sus
// plugins y su `data\`—, el motor de 32 MB, `wintun.dll`, `Packet.dll` y el
// marcador. Un ZIP que alguien descomprime a medias, o del que arrastra solo el
// `.exe` que reconoce, es una carpeta que no arranca y un "no me anda" sin
// ninguna pista.
//
// Esto los empotra a todos dentro de sí mismo, los suelta en una carpeta
// temporal, corre el daemon portable y borra la carpeta al salir.
//
// # Lo que NO hace, y es a propósito
//
// No instala nada. No registra un servicio, no toca el arranque de Windows, no
// deja accesos directos y no escribe en ProgramData. Al cerrarse no queda
// rastro salvo el log. Todo eso es cosa del instalador, y el sentido de esto es
// justamente probar Kanpachi sin nada de eso.
//
// Por lo mismo, la interfaz no enseña el interruptor de arranque automático: en
// una carpeta portable no hay nada que arrancar automáticamente. Lo deduce del
// marcador, sin banderas de compilación. Ver `pipe_names.dart`.
//
// # Dónde queda el log, y por qué no en el temporal
//
// `kanpachi.log` queda **junto a este ejecutable**, o sea en la carpeta desde
// donde alguien lo abrió. El daemon lo pondría dentro de su directorio de
// datos, que acá vive en el temporal que este programa borra al salir: el
// archivo que existe para mandarse por chat sería justo el que la limpieza
// destruye. Por eso se le pasa `--log`. Es lo mismo que hace `roombundle` con
// `roomprobe`, y por el mismo motivo.
//
// Los DATOS sí se quedan en el temporal, y eso tiene una consecuencia que
// conviene conocer: la llave de identidad y `last-room.json` mueren con cada
// corrida, así que "volver a la última sala" no cruza de una a otra. Es el
// precio de no dejar nada, y no un descuido. La alternativa —datos junto al
// ejecutable— rompería el contrato del marcador: la interfaz deduce el
// directorio de datos de DÓNDE ESTÁ ELLA, que es el temporal, y pasarle otro
// por bandera es exactamente el fallo silencioso que `portable.go` documenta
// haber tenido cuatro veces.
//
// # Cómo se construye
//
// **No sale de un `go build ./...`**, igual que roombundle. La carga vive
// detrás de la etiqueta `bundle`, así que sin ella este binario compila y se
// niega a correr diciendo por qué, en vez de producir un bundle vacío que falla
// en la máquina de otra persona.
//
//	scripts\build_portable_bundle.ps1
//
// El script arma la carpeta portable con `kanpachi-portable.ps1 -NoArrancar`,
// la copia a `internal/kanpachibundle/carga/` y compila con `-tags bundle`.
//
// # Lo que hay que saber antes de mandarlo
//
// Va SIN FIRMAR. Windows lo va a recibir con "Editor desconocido" de
// SmartScreen y puede que con un aviso de Defender: un ejecutable que suelta
// otros ejecutables y un driver en una carpeta temporal y pide administrador
// tiene la forma exacta de un dropper. Quien lo reciba tiene que pulsar "Más
// información" y "Ejecutar de todas formas". Lo único que quita ese aviso es un
// certificado de firma de código.
//
// # El icono y el permiso de administrador
//
// Los dos salen de `rsrc_windows_amd64.syso`, que el enlazador de Go mete en el
// ejecutable por estar en este directorio con ese nombre. Fuera de Windows amd64
// se ignora solo, así que no molesta al job de Linux.
//
// Lleva dos cosas dentro y las dos importan:
//
//   - **El icono.** Sin él, lo que le llega a la otra persona es un ejecutable
//     con el icono genérico de consola, indistinguible de cualquier cosa
//     descargada. Es el mismo `app_icon.ico` que usa la interfaz.
//   - **`requireAdministrator` en el manifiesto.** Esto es lo que hace que
//     ELEVE WINDOWS al arrancar, en vez de arrancar sin permisos y relanzarse.
//     La diferencia se ve: relanzándose quedan DOS ventanas de consola, la del
//     padre que ya no hace nada y la del hijo que trabaja, y la muerta se queda
//     ahí toda la sesión de juego. Además pone el escudo en el icono, así que
//     el aviso de Windows deja de ser una sorpresa.
//
// Se regenera con esto, y el `.syso` se versiona porque es una entrada de la
// compilación y no un resultado: sin él, un clon recién hecho produciría un
// ejecutable sin icono y sin manifiesto, y nadie se enteraría hasta mandarlo.
//
//go:generate go run github.com/tc-hib/go-winres@latest simply --arch amd64 --out rsrc --icon ../../ui/windows/runner/resources/app_icon.ico --manifest cli --admin --product-name Kanpachi --product-version git-tag --file-version git-tag --file-description "Kanpachi portable wrapper" --original-filename kanpachi-portable.exe --copyright "Accentio Studios"
//
// Vive en `internal/` para que el producto no lo importe y el instalador no lo
// distribuya, igual que el resto de las sondas.
package main

import "os"

func main() {
	if err := correr(); err != nil {
		// Un cuadro de diálogo y no una línea: sin consola, imprimir es escribir
		// en la nada, y lo que se vería es un icono que parpadea y desaparece.
		// Ver [avisar].
		//
		// El orden de las frases es el punto: qué pasó, si rompió algo, qué
		// hacer, y recién al final la línea que se copia en un reporte. Es la
		// misma forma que usa el daemon cuando no puede abrir su ventana.
		texto := "Kanpachi no pudo arrancar.\n\n" +
			"No se instaló ni se cambió nada en tu PC.\n\n" +
			"Qué hacer: vuelve a abrir el archivo. Si apareció el aviso de Windows " +
			"sobre el editor desconocido, hay que pulsar «Más información» y " +
			"«Ejecutar de todas formas».\n\n"
		// La ruta del log solo si el archivo existe: si el daemon no llegó a
		// arrancar no hay ninguno, y mandar a buscarlo sería mandar a nadie.
		if ruta := rutaDelLog(); ruta != "" {
			texto += "El registro de lo que pasó quedó acá, y es lo que sirve para " +
				"reportarlo:\n" + ruta + "\n\n"
		}
		avisar("Kanpachi", texto+"Detalle técnico:\n"+err.Error())
		os.Exit(1)
	}
}
