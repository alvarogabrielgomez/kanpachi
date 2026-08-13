// Command roomprobe ejercita el ciclo de vida de una sala con los adaptadores
// REALES, sin daemon, sin interfaz y sin instalador.
//
// # Por qué existe
//
// Probar un cambio en el ingreso a una sala costaba recompilar Kanpachi entero,
// esperar al CI, armar el instalador e instalarlo en dos máquinas. El fallo del
// canal de control del 2026-08-07 —el host no abría el puerto 57623 hacia la
// dirección que él mismo acababa de repartir, así que ningún invitado entraba—
// tardó horas en aislarse por eso, y la evidencia que lo cerró fueron capturas
// de pantalla de un móvil.
//
// # Qué hace
//
// Levanta la sesión de verdad con los dieciséis puertos cableados igual que
// `daemon/cmd/kanpachid`, corre el supervisor de verdad, y ofrece un menú para
// crear sala, entrar, expulsar, cerrar, salir y volver a la última. Todo lo que
// pasa queda en `roomprobe.log`, junto al ejecutable.
//
// **El log es el producto.** Lleva los pasos del diario de cada operación, las
// reglas de firewall con su destinatario, la tabla de miembros con el camino de
// cada uno, y un volcado a demanda que contesta de una vez por qué dos máquinas
// no se ven. Ver `diagnostico.go`.
//
// # Qué NO hace
//
//   - No abre puertos de juego. No hay catálogo y no hace falta: lo que se mide
//     acá es la SALA, o sea la red cifrada, el canal de control, la compuerta y
//     los adaptadores.
//   - No sirve el named pipe, así que `kanpctl` y la interfaz no lo ven.
//   - No le pone ACL a `identity.key`. Ver [sinACL].
//
// # Cómo se corre
//
// Necesita consola elevada, y se eleva sola si hace falta: escribe en el
// firewall y crea adaptadores virtuales. **Con el servicio `kanpachi-daemon`
// corriendo se niega a arrancar** salvo con `-force`, porque construir la
// sesión purga las reglas del firewall y se llevaría por delante la sala que
// ese daemon tenga abierta.
//
//	go run ./internal/roomprobe -data C:\ruta\a\datos
//
// Vive en `internal/` para que el producto no lo pueda importar y el instalador
// no lo distribuya. Mismo motivo y mismo patrón que `internal/fwprobe`.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// opciones son los flags ya resueltos, para no pasarlos de a cinco.
type opciones struct {
	datos  string
	seed   string
	nick   string
	force  bool
	dirExe string
	// dirLog es DÓNDE se escribe roomprobe.log, y existe separado de dirExe
	// por el bundle.
	//
	// Corriendo suelto son el mismo sitio. Corriendo dentro de `roombundle`,
	// este ejecutable vive en una carpeta temporal que se borra al terminar:
	// dejar ahí el log significa destruirlo justo cuando alguien lo iba a
	// mandar por chat, que es la única razón por la que existe esta
	// herramienta. El bundle pasa `-log` apuntando a su propio directorio.
	dirLog string
}

func main() {
	// La elevación va PRIMERO, antes de leer flags: el proceso elevado los
	// vuelve a leer enteros, y hacer trabajo antes es hacerlo dos veces.
	if relanzado, err := elevar(); err != nil {
		fmt.Fprintln(os.Stderr, "roomprobe:", err)
		pausar()
		os.Exit(1)
	} else if relanzado {
		return
	}

	datos := flag.String("data", "data", "directorio de datos de ESTA herramienta")
	dirLog := flag.String("log", "", "dónde dejar "+LogFile+" (por omisión, junto al ejecutable)")
	// El seed gobierna dónde ABRE salas esta máquina, y nada más.
	//
	// Ya no hay valor por defecto que poner, porque el producto dejó de tener
	// uno: sin esta bandera, esta sonda no puede crear una sala, igual que el
	// producto sin registro configurado. Para ENTRAR no hace falta, porque el
	// registro sale del código pegado, que ahora siempre trae el suyo.
	seed := flag.String("seed", "",
		"host del registro donde esta máquina abre salas. Sin él solo se puede entrar")
	force := flag.Bool("force", false,
		"arrancar aunque el servicio kanpachi-daemon esté corriendo (le purga las reglas)")
	flag.Parse()

	// El directorio del EJECUTABLE, no `os.Args[0]`: tras la elevación por UAC
	// el argumento cero no es una ruta fiable, y de él cuelgan el motor, el log
	// y el directorio de datos por defecto.
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "roomprobe: no se pudo saber dónde está este ejecutable:", err)
		pausar()
		os.Exit(1)
	}
	dirExe := filepath.Dir(exe)

	op := opciones{datos: *datos, seed: *seed, force: *force, dirExe: dirExe, dirLog: *dirLog}
	if !filepath.IsAbs(op.datos) {
		op.datos = filepath.Join(dirExe, op.datos)
	}
	if op.dirLog == "" {
		op.dirLog = dirExe
	}
	if err := os.MkdirAll(op.datos, 0o700); err != nil {
		fmt.Fprintln(os.Stderr, "roomprobe: no se pudo crear el directorio de datos:", err)
		pausar()
		os.Exit(1)
	}

	op.nick = leerApodo(op.datos)
	if op.nick == "" {
		op.nick = pedirApodo("")
		if _, err := domain.ParseNickname(op.nick); err != nil {
			fmt.Fprintln(os.Stderr, "roomprobe: ese nombre no vale:", err)
			pausar()
			os.Exit(1)
		}
		guardarApodo(op.datos, op.nick)
	}

	if err := correr(op); err != nil {
		fmt.Fprintln(os.Stderr, "roomprobe:", err)
		pausar()
		os.Exit(1)
	}
	pausar()
}

// ─── El apodo, en disco ──────────────────────────────────────────────────────

func rutaApodo(datos string) string { return filepath.Join(datos, "nickname.txt") }

func leerApodo(datos string) string {
	b, err := os.ReadFile(rutaApodo(datos))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func guardarApodo(datos, nombre string) {
	if err := os.WriteFile(rutaApodo(datos), []byte(nombre), 0o600); err != nil {
		// No se calla: sin esto, el nombre se pregunta otra vez en cada arranque
		// y nadie sabría por qué.
		fmt.Fprintln(os.Stderr, "roomprobe: no se pudo guardar el nombre:", err)
	}
}

func pausar() {
	fmt.Println("\nPulsa Enter para salir...")
	var nada string
	_, _ = fmt.Scanln(&nada)
}
