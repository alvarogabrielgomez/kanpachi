//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/accentiostudios/kanpachi/internal/layout"
)

// hijoEnv es la variable que convierte esta prueba en el proceso que se muere.
const hijoEnv = "KANPACHI_PRUEBA_PÁNICO"

// TestUnPánicoDelDaemonQuedaEnElLog comprueba lo que hoy NO pasaba: que un
// pánico deje traza en disco.
//
// # Por qué esto merece una prueba
//
// El 2026-08-08 el servicio se murió dos veces. Lo único que quedó fue el
// registro de eventos de Windows diciendo *"The Kanpachi service terminated
// unexpectedly"*, y el log del daemon sin nada entre la última línea normal y
// el arranque siguiente. El daemon se enlaza con `-H windowsgui`, así que no
// tiene salida de errores, y ahí es donde el runtime de Go escribe la traza.
//
// # Por qué hace falta un proceso hijo
//
// Porque lo que se comprueba es que el RUNTIME escriba en el archivo, y el
// runtime solo escribe una traza cuando el proceso se muere de verdad.
// Recuperar el pánico probaría otra cosa.
//
// # Lo que esta prueba NO cubre
//
// El binario de pruebas es de consola, o sea que ya tiene salida de errores. Lo
// que se comprueba acá es el cableado: que la traza acabe en el archivo del
// log. Que `SetStdHandle` funcione en un binario `-H windowsgui`, que es el
// caso del servicio, se midió aparte con un binario enlazado así, y la traza
// apareció igual.
func TestUnPánicoDelDaemonQuedaEnElLog(t *testing.T) {
	if dir := os.Getenv(hijoEnv); dir != "" {
		// Este es el hijo. Monta el log igual que el daemon y se muere.
		archivo := nuevoLogArchivo(carpetaDelLog(dir, ""))
		if err := archivo.CapturarPánicos(); err != nil {
			// Sin captura no hay nada que comprobar, y el padre lo va a ver como
			// un log sin la traza.
			archivo.Error("no se pudo capturar la salida de errores", "error", err)
			return
		}
		archivo.Info("el hijo está a punto de reventar")
		panic("pánico de prueba, a propósito")
	}

	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=TestUnPánicoDelDaemonQuedaEnElLog")
	cmd.Env = append(os.Environ(), hijoEnv+"="+dir)
	// Se ESPERA que falle: un pánico sale con código 2. Lo que importa no es el
	// error, es lo que dejó escrito.
	_ = cmd.Run()

	crudo, err := os.ReadFile(filepath.Join(dir, layout.LogDir, layout.LogFile))
	if err != nil {
		t.Fatalf("el hijo no dejó ningún log: %v", err)
	}
	texto := string(crudo)

	// Las dos mitades, y las dos hacen falta. La línea normal dice que el log
	// funcionaba; la traza dice que ADEMÁS se capturó lo que antes se perdía.
	if !strings.Contains(texto, "el hijo está a punto de reventar") {
		t.Error("falta la línea normal del log, así que no se está mirando el archivo correcto")
	}
	if !strings.Contains(texto, "panic: pánico de prueba, a propósito") {
		t.Errorf("la traza del pánico no llegó al log. Lo que hay:\n%s", texto)
	}
	// La pila es lo que convierte "se murió" en "se murió acá", que es la
	// diferencia entre poder diagnosticarlo y no.
	if !strings.Contains(texto, "goroutine") {
		t.Errorf("la traza llegó sin pila, y sin pila no se puede ubicar el fallo:\n%s", texto)
	}
}

// TestLaCarpetaPedidaSeUsaTalCual fija lo que el bundle portable necesita: que
// `--log` mande el archivo a ESA carpeta, sin colgarle `logs\` debajo.
//
// Si se le colgara, el bundle dejaría el log en un subdirectorio que quien lo
// ejecutó no eligió ni espera, y el archivo que existe para mandarse por chat
// sería justo el que no se encuentra.
func TestLaCarpetaPedidaSeUsaTalCual(t *testing.T) {
	pedida := filepath.Join("C:\\", "Descargas")
	if got := carpetaDelLog("C:\\datos", pedida); got != pedida {
		t.Errorf("con --log se esperaba %q y salió %q", pedida, got)
	}
}

// TestSinCarpetaPedidaVaAlSubdirectorioDeSiempre es la otra mitad: el producto
// instalado no cambia de sitio por existir la bandera.
func TestSinCarpetaPedidaVaAlSubdirectorioDeSiempre(t *testing.T) {
	quiero := filepath.Join("C:\\datos", layout.LogDir)
	if got := carpetaDelLog("C:\\datos", ""); got != quiero {
		t.Errorf("sin --log se esperaba %q y salió %q", quiero, got)
	}
}

// TestLaRotaciónNoSeLlevaLaCapturaDelPánico es el fallo silencioso que este
// diseño podía tener: rotar cierra el archivo y abre otro, así que una captura
// hecha una sola vez al arrancar apuntaría a un handle cerrado, y el pánico que
// importa —el de un daemon que lleva días arriba— se perdería igual.
func TestLaRotaciónNoSeLlevaLaCapturaDelPánico(t *testing.T) {
	dir := t.TempDir()
	archivo := nuevoLogArchivo(carpetaDelLog(dir, ""))
	t.Cleanup(func() { _ = archivo.Close() })

	if err := archivo.CapturarPánicos(); err != nil {
		t.Fatalf("no se pudo capturar la salida de errores: %v", err)
	}

	anterior := archivo.f
	archivo.mu.Lock()
	archivo.rotar()
	archivo.mu.Unlock()

	if archivo.f == nil {
		t.Fatal("tras rotar no quedó archivo abierto")
	}
	if archivo.f == anterior {
		t.Fatal("rotar no reabrió nada, así que la prueba no está midiendo la rotación")
	}
	if !archivo.pánicos {
		t.Fatal("la rotación se llevó por delante la marca de captura")
	}

	// La comprobación de verdad: la salida de errores del proceso apunta al
	// archivo NUEVO, no al que se acaba de cerrar.
	if os.Stderr != archivo.f {
		t.Error("tras rotar, la salida de errores sigue apuntando al archivo viejo")
	}
}
