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
	"syscall"
	"time"

	"golang.org/x/sys/windows"

	"github.com/accentiostudios/kanpachi/internal/layout"
)

// imprescindibles son los que se comprueban POR SU NOMBRE después de extraer.
//
// No es la lista de lo que se empotra —eso es la carpeta entera—, es la lista
// de lo que no puede faltar. Se nombran uno a uno porque la falta de cualquiera
// de ellos produce un fallo que no menciona ningún archivo: sin `Packet.dll` el
// motor muere con 0xC0000135, sin `flutter_windows.dll` la interfaz se cierra
// en el arranque sin decir nada, y sin el marcador el daemon se niega a correr
// con `--daemon` por un motivo que parece otro.
var imprescindibles = []string{
	"kanpachid.exe",
	"kanpachiui.exe",
	"kanpachi-engine.exe",
	"wintun.dll",
	"Packet.dll",
	"kanpachi.portable",
}

// elQueSeCorre es el daemon portable, que abre la interfaz por su cuenta.
const elQueSeCorre = "kanpachid.exe"

func correr() error {
	if !hayCarga {
		return errors.New("este kanpachibundle se compiló SIN carga, así que no lleva nada dentro.\n" +
			"  Se construye con scripts\\build-portable-bundle.ps1, que arma la carpeta\n" +
			"  portable, la copia a internal/kanpachibundle/carga/ y compila con -tags bundle")
	}

	// # Por qué se eleva ANTES de extraer nada
	//
	// El daemon portable necesita administrador: escribe reglas de WFP y crea
	// adaptadores virtuales. Si se lanzara sin elevar, se elevaría él solo, y
	// este proceso vería terminar al hijo en el acto, borraría la carpeta
	// temporal, y la copia elevada se quedaría sin motor, sin DLL y sin
	// interfaz.
	//
	// Elevándonos primero, el daemon hereda el token, ve que ya es
	// administrador y no relanza nada. **Un solo UAC en toda la sesión**: el
	// daemon lanza el motor con un `exec.Command` normal, que hereda el mismo
	// token, así que abrir una sala no vuelve a preguntar nada.
	if !windows.GetCurrentProcessToken().IsElevated() {
		return elevarYSalir()
	}

	dir, err := os.MkdirTemp("", "kanpachi-portable-")
	if err != nil {
		return fmt.Errorf("no se pudo crear la carpeta temporal: %w", err)
	}

	// La limpieza va con `defer` y además avisa si no pudo: 90 MB que se quedan
	// para siempre en el temporal de otra persona es algo que hay que decir.
	defer limpiar(dir)

	if err := extraer(dir); err != nil {
		return err
	}
	return lanzar(dir)
}

// elevarYSalir relanza este mismo ejecutable como administrador y **se va**.
//
// # Por qué casi nunca corre
//
// Porque el manifiesto ya pide `requireAdministrator`, así que Windows eleva
// este proceso ANTES de que empiece y la comprobación de arriba da que sí. Ver
// el doc del paquete.
//
// Se queda como red de seguridad para el único caso en que eso no pasa: un
// binario construido sin el `.syso`. Sin esto, ese ejecutable arrancaría sin
// permisos, extraería todo y moriría al crear el primer adaptador, con un error
// de acceso denegado que no menciona ningún permiso.
//
// # Por qué NO espera, al revés que roombundle
//
// Porque cada proceso trae su consola, y esperar deja DOS VENTANAS abiertas: la
// del padre sin elevar, que ya no hace nada, y la del hijo elevado, que es la
// que trabaja. En roombundle se nota poco porque su hijo dura lo que dura una
// prueba. Acá dura toda la sesión de juego, así que la ventana muerta se queda
// a la vista durante horas, encima diciendo una frase sobre un permiso que ya
// se concedió. Medido abriendo el bundle: se veían las dos.
//
// Nada depende de la espera. El que extrae, el que corre el daemon, el que
// limpia el temporal y el que dice dónde quedó el log es el hijo elevado, de
// punta a punta. Al padre no le queda nada que hacer después de lanzarlo.
//
// Lo que sí se conserva es detectar que alguien canceló el aviso: sin `-Wait`,
// `Start-Process` sigue fallando si el UAC se rechaza, que es el único error
// que este camino tiene que saber contar.
//
// Los argumentos van como LISTA, uno por elemento, con la comilla simple
// doblada. Juntarlos en una sola cadena convierte una ruta con espacios en un
// único argumento con comillas dentro.
func elevarYSalir() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("no se pudo saber qué ejecutable relanzar: %w", err)
	}
	orden := "Start-Process -FilePath '" + comillar(exe) + "' -Verb RunAs"
	if len(os.Args) > 1 {
		var lista []string
		for _, a := range os.Args[1:] {
			lista = append(lista, "'"+comillar(a)+"'")
		}
		orden += " -ArgumentList " + strings.Join(lista, ",")
	}

	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", orden)
	// Sin ventana: este binario es gráfico a propósito, y una consola de
	// PowerShell parpadeando delante sería justo lo que se quitó.
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("no se pudo arrancar como administrador (¿se canceló el aviso?): %w", err)
	}
	return nil
}

func comillar(s string) string { return strings.ReplaceAll(s, "'", "''") }

// dirDelBundle es la carpeta desde donde alguien abrió esto, que es la única
// que esa persona sabe encontrar.
func dirDelBundle() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Dir(exe), nil
}

// rutaDelLog es dónde quedó el registro, o vacío si todavía no hay ninguno.
//
// Se usa para el cuadro de error, y ese es el único sitio donde sirve. Antes
// esto se imprimía al cerrar, siempre; sin consola no hay dónde imprimirlo, y
// un cuadro de diálogo en cada cierre normal para decir algo que nadie pidió
// sería peor que no decirlo. Cuando algo falla, en cambio, la ruta es
// exactamente lo que hace falta y hay que darla escrita.
//
// Devuelve vacío si el archivo no está, para no mandar a nadie a buscar algo
// que no existe: el daemon pudo no haber llegado a arrancar.
func rutaDelLog() string {
	dir, err := dirDelBundle()
	if err != nil {
		return ""
	}
	ruta := filepath.Join(dir, layout.LogFile)
	if _, err := os.Stat(ruta); err != nil {
		return ""
	}
	return ruta
}

// extraer vuelca la carpeta empotrada, con su estructura, y comprueba que no
// falte nada de lo imprescindible.
//
// Recursivo y no una lista plana como en roombundle: la interfaz de Flutter
// trae `data\flutter_assets\` con subdirectorios, y aplanarlos daría una
// interfaz que abre en blanco.
func extraer(dir string) error {
	err := fs.WalkDir(carga, "carga", func(ruta string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// La raíz `carga` no se recrea: su contenido va directo a `dir`.
		rel, err := filepath.Rel("carga", filepath.FromSlash(ruta))
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		destino := filepath.Join(dir, rel)

		// 0o700 en todo: la carpeta es temporal y de un solo uso, y dentro hay
		// ejecutables que corren elevados. Que otro usuario de la máquina pueda
		// reemplazarlos entre que se escriben y se ejecutan sería una escalada
		// de privilegios de manual.
		if d.IsDir() {
			return os.MkdirAll(destino, 0o700)
		}
		datos, err := carga.ReadFile(ruta)
		if err != nil {
			return fmt.Errorf("leyendo %s de dentro del bundle: %w", rel, err)
		}
		if err := os.MkdirAll(filepath.Dir(destino), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(destino, datos, 0o700); err != nil {
			return fmt.Errorf("escribiendo %s: %w", rel, err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("soltando el contenido del bundle: %w", err)
	}

	for _, nombre := range imprescindibles {
		if _, err := os.Stat(filepath.Join(dir, nombre)); err != nil {
			return fmt.Errorf("a este bundle le falta %s por dentro, así que no va a arrancar.\n"+
				"  Se arma con scripts\\build-portable-bundle.ps1", nombre)
		}
	}
	return nil
}

// argumentos son los del daemon portable.
//
//   - `--daemon`: este proceso ES el daemon, sin Administrador de servicios.
//     Solo vale con el marcador presente, y viene dentro de la carga.
//   - `--show`: quien acaba de hacer doble clic está mirando. Sin esto
//     aparecería el icono de la bandeja y ninguna ventana.
//   - `--log`: la razón entera por la que esa bandera existe. Ver el doc del
//     paquete.
//   - `--data`: junto al ejecutable y no en el temporal que se borra. El
//     nombre de la carpeta sale de [layout.PortableDataDir].
//
// # Por qué la carpeta de datos se pasa a mano en vez de deducirse del marcador
//
// Porque el marcador viaja DENTRO de la carga, así que acaba en el directorio
// temporal, y eso es lo que el daemon miraba: escribía su llave, su token y su
// sala ahí, y `limpiar` borraba la carpeta entera al cerrar. Cada arranque
// estrenaba identidad, y las extracciones que no se pudieron borrar quedaron
// tiradas en el temporal con la llave dentro.
//
// La carpeta desde donde alguien abrió esto es lo único que sobrevive al cierre,
// y es la que esa persona sabe encontrar. Es la misma decisión que ya estaba
// tomada para el log, con el mismo motivo.
//
// La interfaz ya no lo deduce por su cuenta: el daemon se lo dice al lanzarla,
// por la misma vía que el log. Deducirlo cada uno por su lado era correcto
// mientras los dos ejecutables vivían en la misma carpeta que el marcador, y
// dejó de serlo el día que el daemon salió de ahí.
func argumentos(dirBundle string) []string {
	return []string{
		"--daemon", "--show",
		"--log", dirBundle,
		"--data", filepath.Join(dirBundle, layout.PortableDataDir),
	}
}

// lanzar corre el daemon portable y espera a que termine.
//
// Hereda el token, que ya es de administrador, así que no vuelve a pedir nada.
// Lo que sostiene la sesión entera es esta espera: mientras el daemon viva,
// esta ventana sigue abierta y la carpeta temporal sigue en pie.
func lanzar(dir string) error {
	dirBundle := dir // el peor caso: el log acompaña a lo que se va a borrar
	if d, err := dirDelBundle(); err == nil {
		dirBundle = d
	}

	exe := filepath.Join(dir, elQueSeCorre)
	cmd := exec.Command(exe, argumentos(dirBundle)...)
	// El directorio de trabajo es el de la carpeta, por lo mismo que lo hace el
	// adaptador del motor: `Packet.dll` es una importación dura y se busca ahí.
	cmd.Dir = dir
	// En nil, o sea a NUL. Sin consola, `os.Stdout` es un handle inválido:
	// pasárselo al hijo sería darle una salida que escribe en la nada, y el
	// daemon la tomaría por válida justo cuando la usa para dejar la traza de un
	// pánico. Su log ya va a archivo, que es donde tiene que estar.
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil

	if err := cmd.Run(); err != nil {
		var salida *exec.ExitError
		if errors.As(err, &salida) {
			// El daemon terminó mal. No es un fallo del bundle: su trabajo era
			// ponerlo a correr, y el motivo quedó en el log, junto a este
			// ejecutable.
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
// Porque `wintun.dll` y `WinDivert64.sys` no son archivos normales mientras
// corren: Windows los tiene cargados como driver de kernel, y quedan tomados
// hasta que el servicio para del todo, que pasa unos instantes DESPUÉS de que
// el daemon termine. El primer intento se encuentra con un "el archivo está en
// uso" que no significa nada malo.
//
// Tres pasos, de más limpio a menos:
//
//  1. Reintentar unos segundos, que es lo que tarda el driver en soltarse.
//  2. Pedirle a Windows que lo borre en el próximo arranque, cuando ya nadie
//     lo tiene abierto.
//  3. Decirlo con la ruta, para poder borrarlo a mano.
func limpiar(dir string) {
	for intento := 0; intento < 10; intento++ {
		if err := os.RemoveAll(dir); err == nil {
			return
		} else if !errors.Is(err, fs.ErrPermission) && !esArchivoEnUso(err) {
			// Un error que no es "está en uso" no se va a arreglar esperando.
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	if err := borrarEnElProximoArranque(dir); err == nil {
		// En silencio a propósito: se borra sola en el arranque siguiente, y un
		// cuadro de diálogo para contar algo que ya está resuelto es ruido.
		return
	}
	// Acá sí se avisa: son 72 MB que se quedan para siempre en el temporal de
	// otra persona, sin nada que lo explique.
	avisar("Kanpachi", "Kanpachi se cerró bien, pero no pudo borrar su carpeta temporal.\n\n"+
		"No afecta a nada, es espacio ocupado: unos 72 MB.\n\n"+
		"Qué hacer: bórrala a mano cuando quieras.\n\n"+dir)
}

func esArchivoEnUso(err error) bool {
	// ERROR_SHARING_VIOLATION (32) y ERROR_ACCESS_DENIED (5) son las dos formas
	// en que se presenta un archivo que otro proceso o el kernel tiene tomado.
	return errors.Is(err, windows.ERROR_SHARING_VIOLATION) ||
		errors.Is(err, windows.ERROR_ACCESS_DENIED)
}

// borrarEnElProximoArranque apunta la carpeta y su contenido en la lista de
// operaciones pendientes de archivo del sistema.
//
// `MoveFileEx` con destino nulo y `MOVEFILE_DELAY_UNTIL_REBOOT` significa
// "bórralo", y el sistema lo hace en el arranque siguiente, antes de cargar
// nada que pueda volver a tomarlo. Exige administrador, que a esta altura ya
// tenemos. Los directorios solo se borran vacíos, así que van los archivos
// primero y las carpetas al final, de la más honda a la más somera.
func borrarEnElProximoArranque(dir string) error {
	var ultimo error
	var carpetas []string
	_ = filepath.WalkDir(dir, func(ruta string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			carpetas = append(carpetas, ruta)
			return nil
		}
		if e := marcarParaBorrar(ruta); e != nil {
			ultimo = e
		}
		return nil
	})
	// Al revés de como se recorrieron: `WalkDir` va de fuera hacia dentro, y
	// una carpeta solo se borra cuando ya está vacía.
	for i := len(carpetas) - 1; i >= 0; i-- {
		if e := marcarParaBorrar(carpetas[i]); e != nil {
			ultimo = e
		}
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
