//go:build windows

package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows"
)

// ficheros son los cinco que tienen que viajar juntos, y en qué orden se
// escriben.
//
// Se nombran uno por uno en vez de recorrer el directorio empotrado, para que
// falte lo que falte se diga POR SU NOMBRE. Un bundle al que le falte
// `Packet.dll` arranca igual y muere después, dentro del motor, con un error
// que no menciona ningún fichero.
var ficheros = []string{
	"roomprobe.exe",
	"kanpachi-engine.exe",
	"wintun.dll",
	"Packet.dll",
	"WinDivert64.sys",
}

// elQueSeCorre es el que se lanza una vez extraído todo.
const elQueSeCorre = "roomprobe.exe"

func correr() error {
	if !hayCarga {
		return errors.New("este roombundle se compiló SIN carga, así que no lleva nada dentro.\n" +
			"  Se construye con scripts\\build_test_tools.ps1, que copia los cinco\n" +
			"  ficheros a internal/roombundle/carga/ y compila con -tags bundle")
	}

	// La ayuda no eleva nada, y es la única cosa que corre sin permisos.
	//
	// Extraer a un temporal propio y preguntarle a roomprobe es lo que hace que
	// esta ayuda sea LA SUYA: este programa no declara banderas y no tiene qué
	// listar por su cuenta, así que cualquier texto escrito acá se quedaría
	// corto en la primera bandera nueva. Medido: sin esto, `roombundle -h`
	// pedía administrador para contestar una pregunta que no toca nada.
	if pideAyuda() {
		return ayuda()
	}

	// # Por qué se eleva ANTES de extraer nada
	//
	// roomprobe necesita administrador y se eleva solo si no lo tiene: la copia
	// sin privilegios lanza una elevada y **se muere en el acto**. Si este
	// bundle corriera roomprobe sin estar elevado, vería terminar al proceso en
	// un segundo, borraría la carpeta temporal, y la copia elevada se quedaría
	// a mitad sin motor y sin DLL.
	//
	// Elevándonos primero, roomprobe hereda el token, ve que ya es
	// administrador, no relanza nada, y este proceso puede esperarlo de verdad.
	// Un solo UAC y un solo proceso de punta a punta.
	if !windows.GetCurrentProcessToken().IsElevated() {
		return elevarYEsperar()
	}

	// Lo ÚLTIMO que se ve al cerrar es dónde quedó el log, y por eso se registra
	// antes que la limpieza: los `defer` corren al revés de como se apuntan.
	// Es el único motivo por el que alguien corrió esto, así que no puede
	// quedar sepultado entre mensajes de borrado.
	defer decirDondeQuedoElLog()

	dir, err := os.MkdirTemp("", "kanpachi-roomprobe-")
	if err != nil {
		return fmt.Errorf("no se pudo crear la carpeta temporal: %w", err)
	}
	fmt.Println("Preparando en", dir)

	// La limpieza va con `defer` y además avisa si no pudo: una carpeta de 48 MB
	// que se queda para siempre en el temporal de otra persona es algo que hay
	// que decir, no callar.
	defer limpiar(dir)

	if err := extraer(dir); err != nil {
		return err
	}
	return lanzar(dir)
}

// elevarYEsperar relanza este mismo ejecutable como administrador y **espera a
// que termine**.
//
// El `-Wait` es lo que hace que esto funcione. Sin él, `Start-Process` vuelve
// en cuanto lanza, este proceso terminaría enseguida, y la ventana del padre se
// cerraría dejando a la copia elevada trabajando sola. Con él, PowerShell no
// vuelve hasta que el hijo termina.
//
// Los argumentos se pasan como LISTA, un elemento por argumento, con la comilla
// simple doblada. Juntarlos en una sola cadena convierte `-data "C:\ruta con
// espacios"` en un único argumento con comillas dentro.
func elevarYEsperar() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("no se pudo saber qué ejecutable relanzar: %w", err)
	}
	orden := "Start-Process -FilePath '" + comillar(exe) + "' -Verb RunAs -Wait"
	if len(os.Args) > 1 {
		var lista []string
		for _, a := range os.Args[1:] {
			lista = append(lista, "'"+comillar(a)+"'")
		}
		orden += " -ArgumentList " + strings.Join(lista, ",")
	}

	fmt.Println("Esto necesita permisos de administrador: Windows va a preguntar.")
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", orden)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("no se pudo arrancar como administrador (¿se canceló el aviso?): %w", err)
	}
	return nil
}

func comillar(s string) string { return strings.ReplaceAll(s, "'", "''") }

// pideAyuda mira las tres formas de pedirla, sin parsear: acá no hay banderas
// propias que parsear.
func pideAyuda() bool {
	for _, a := range os.Args[1:] {
		switch a {
		case "-h", "--h", "-help", "--help":
			return true
		}
	}
	return false
}

// ayuda extrae lo justo y deja que conteste roomprobe.
func ayuda() error {
	dir, err := os.MkdirTemp("", "kanpachi-roomprobe-ayuda-")
	if err != nil {
		return fmt.Errorf("no se pudo crear la carpeta temporal: %w", err)
	}
	defer limpiar(dir)
	if err := extraer(dir); err != nil {
		return err
	}
	fmt.Println("roombundle lleva roomprobe dentro y le pasa TODAS las banderas tal cual.")
	fmt.Println("Agrega -log y -data junto a este ejecutable, solo si no vinieron.")
	fmt.Println()
	cmd := exec.Command(filepath.Join(dir, elQueSeCorre), "-h")
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

// decirDondeQuedoElLog es el mensaje de cierre.
//
// La carpeta temporal ya no está y roomprobe.log sí, junto a este ejecutable.
// Quien corrió esto lo hizo para mandar ese fichero, así que la última línea de
// la ventana tiene que ser su ruta.
func decirDondeQuedoElLog() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	ruta := filepath.Join(filepath.Dir(exe), "roomprobe.log")
	if _, err := os.Stat(ruta); err != nil {
		return
	}
	fmt.Println("\n================================================================")
	fmt.Println("  El registro de todo lo que pasó quedó acá. Mándalo por chat:")
	fmt.Println("  " + ruta)
	fmt.Println("================================================================")
}

// argumentos son los que recibió el bundle, más los dos que roomprobe no puede
// deducir por su cuenta.
//
// # Por qué hacen falta
//
// roomprobe pone su log y su directorio de datos JUNTO A SU EJECUTABLE, que es
// lo correcto cuando se corre suelto. Dentro del bundle su ejecutable vive en
// la carpeta temporal, o sea que sin esto pasarían dos cosas, las dos malas:
//
//   - `roomprobe.log` se escribiría en el temporal y **lo borraría la limpieza
//     de este mismo programa**, justo el fichero por el que existe la
//     herramienta.
//   - El directorio de datos moriría con él, y con él `last-room.json`. La
//     opción "volver a la última sala" no funcionaría nunca desde un bundle,
//     porque cada corrida empezaría sin memoria.
//   - Y desde la decisión 25 se llevaría además `identity.key` y
//     `known-hosts.json`, o sea la identidad con la que esta máquina firma y la
//     libreta de con quién jugó. Cada corrida se presentaría con una huella
//     nueva y vería a todos como desconocidos, que es exactamente el aviso que
//     hay que poder reproducir para medirlo.
//
// Los dos apuntan al directorio del BUNDLE, que es donde la persona lo dejó al
// descargarlo y donde va a saber buscarlos.
//
// **Todo lo demás pasa TAL CUAL.** Este programa no declara banderas propias ni
// las interpreta: lo que recibe se lo entrega a roomprobe entero y en orden, así
// que `-seed`, `-seed-password`, `-force` y las que se agreguen mañana funcionan
// desde el bundle el día que existan en roomprobe, sin tocar este fichero. Un
// espejo que hubiera que mantener a mano es un espejo que se queda corto en la
// primera bandera nueva, y lo descubriría quien está probando en la máquina de
// otro. `-h` también viaja, así que la ayuda que sale es la de roomprobe.
//
// Lo que venga por la línea de órdenes gana: quien pasa `-log` a mano sabe lo
// que quiere, y pasarlo dos veces haría que Go se quedara con el último.
func argumentos(dirTemporal string) []string {
	args := append([]string{}, os.Args[1:]...)

	dirBundle := dirTemporal // el peor caso: al menos corre
	if exe, err := os.Executable(); err == nil {
		dirBundle = filepath.Dir(exe)
	}

	if !yaTrae(args, "log") {
		args = append(args, "-log", dirBundle)
	}
	if !yaTrae(args, "data") {
		// Un nombre propio y no `data` a secas: esto aterriza en la carpeta de
		// descargas de otra persona, y una carpeta llamada `data` ahí no dice
		// de quién es ni se puede borrar con confianza.
		args = append(args, "-data", filepath.Join(dirBundle, "roomprobe-data"))
	}
	return args
}

// yaTrae mira si un flag vino de fuera, en cualquiera de las formas que `flag`
// acepta: `-x v`, `--x v`, `-x=v` y `--x=v`.
func yaTrae(args []string, nombre string) bool {
	for _, a := range args {
		for _, guiones := range []string{"-", "--"} {
			if a == guiones+nombre || strings.HasPrefix(a, guiones+nombre+"=") {
				return true
			}
		}
	}
	return false
}

// extraer escribe los cinco ficheros y comprueba que estén los cinco.
func extraer(dir string) error {
	for _, nombre := range ficheros {
		datos, err := carga.ReadFile("carga/" + nombre)
		if err != nil {
			return fmt.Errorf("a este bundle le falta %s por dentro: %w", nombre, err)
		}
		// 0o700: la carpeta es temporal y de un solo uso, y dentro hay
		// ejecutables que corren elevados. No hay motivo para que otro usuario
		// de la máquina pueda ni leerlos ni, peor, reemplazarlos entre que se
		// escriben y se ejecutan.
		if err := os.WriteFile(filepath.Join(dir, nombre), datos, 0o700); err != nil {
			return fmt.Errorf("escribiendo %s: %w", nombre, err)
		}
	}
	return nil
}

// lanzar corre roomprobe y espera a que termine.
//
// Hereda la consola de este proceso, así que el menú de roomprobe se dibuja en
// esta misma ventana en vez de abrir otra. Y hereda el token, que ya es de
// administrador, así que roomprobe no vuelve a pedir nada.
func lanzar(dir string) error {
	exe := filepath.Join(dir, elQueSeCorre)
	cmd := exec.Command(exe, argumentos(dir)...)
	// El directorio de trabajo es el de la carpeta, por lo mismo que lo hace el
	// adaptador del motor: `Packet.dll` es una importación dura y se busca ahí.
	cmd.Dir = dir
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr

	if err := cmd.Run(); err != nil {
		var salida *exec.ExitError
		if errors.As(err, &salida) {
			// roomprobe termina con 1 cuando alguna comprobación salió mal. Eso
			// no es un fallo del bundle: su trabajo era ponerlo a correr.
			fmt.Printf("\nroomprobe terminó con código %d\n", salida.ExitCode())
			return nil
		}
		return fmt.Errorf("no se pudo ejecutar %s: %w", elQueSeCorre, err)
	}
	return nil
}

// limpiar borra la carpeta temporal, con paciencia.
//
// # Por qué no basta un `os.RemoveAll`
//
// Porque `wintun.dll` y `WinDivert64.sys` no son ficheros normales mientras
// corren: Windows los tiene cargados como driver de kernel, y el fichero queda
// tomado hasta que el servicio para del todo. Eso pasa unos instantes DESPUÉS
// de que roomprobe termine, así que el primer intento de borrado se encuentra
// con un "el archivo está en uso" que no significa nada malo.
//
// Tres pasos, de más limpio a menos:
//
//  1. Intentar y reintentar durante unos segundos, que es lo que tarda el
//     driver en soltarse.
//  2. Si sigue tomado, pedirle a Windows que lo borre en el próximo arranque.
//     Es una lista del registro que el sistema procesa antes de cargar nada, o
//     sea cuando ya nadie tiene el fichero abierto.
//  3. Si ni eso, decirlo con la ruta, para que se pueda borrar a mano.
func limpiar(dir string) {
	fmt.Println("\nLimpiando", dir)
	for intento := 0; intento < 10; intento++ {
		if err := os.RemoveAll(dir); err == nil {
			fmt.Println("Listo, no queda nada.")
			return
		} else if !errors.Is(err, fs.ErrPermission) && !esArchivoEnUso(err) {
			// Un error que no es "está en uso" no se va a arreglar esperando.
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	if err := borrarEnElProximoArranque(dir); err == nil {
		fmt.Println("Algo quedó tomado por el driver. Windows lo borrará al reiniciar.")
		return
	}
	fmt.Println("No se pudo borrar la carpeta temporal. Bórrala a mano:")
	fmt.Println("  ", dir)
}

func esArchivoEnUso(err error) bool {
	// ERROR_SHARING_VIOLATION (32) y ERROR_ACCESS_DENIED (5) son las dos formas
	// en que se presenta un fichero que otro proceso o el kernel tiene tomado.
	return errors.Is(err, windows.ERROR_SHARING_VIOLATION) ||
		errors.Is(err, windows.ERROR_ACCESS_DENIED)
}

// borrarEnElProximoArranque apunta la carpeta y su contenido en la lista de
// operaciones pendientes de fichero del sistema.
//
// `MoveFileEx` con destino nulo y `MOVEFILE_DELAY_UNTIL_REBOOT` significa
// "bórralo", y el sistema lo hace en el arranque siguiente, antes de cargar
// nada que pueda volver a tomarlo. Exige administrador, que a esta altura ya
// tenemos. Los directorios solo se borran vacíos, así que van los ficheros
// primero y la carpeta al final.
func borrarEnElProximoArranque(dir string) error {
	var ultimo error
	_ = filepath.WalkDir(dir, func(ruta string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if e := marcarParaBorrar(ruta); e != nil {
			ultimo = e
		}
		return nil
	})
	if e := marcarParaBorrar(dir); e != nil {
		ultimo = e
	}
	return ultimo
}

func marcarParaBorrar(ruta string) error {
	p, err := windows.UTF16PtrFromString(ruta)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(p, nil, windows.MOVEFILE_DELAY_UNTIL_REBOOT)
}
