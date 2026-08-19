package main

import (
	"context"
	"fmt"
	"runtime"
	"runtime/debug"

	"github.com/accentiostudios/kanpachi/internal/engineid"
)

// Version la sella el que compila, con
// `-ldflags "-X main.Version=$VERSION"`.
//
// Es el mismo mecanismo que ya usa el seed (`registry/cli.Version`), y lo que
// deja en el binario compilado a mano es `dev`, que es la verdad: un binario sin
// sellar no salió de una release y decir lo contrario haría que un informe de
// fallo apuntara a una versión publicada que no es la que falló.
var Version = "dev"

// cmdVersion dice qué versión es esta, y de qué tanda salió.
//
// # Por qué también el commit
//
// Porque el número de versión solo cambia en una release, y buena parte de lo
// que se prueba está entre dos. `go build` sella el estado del repositorio en el
// binario, así que sale gratis y contesta la pregunta que de verdad se hace
// cuando algo va mal: exactamente qué código es este.
func cmdVersion(_ context.Context, op opciones, _ []string) error {
	revisión, sucio := revisión()

	// El motor se lee del FICHERO, sin ejecutarlo, por los centinelas que su
	// compilación le sella dentro. Vacío significa que existe y es anterior al
	// centinela; ausente o ilegible se calla, porque esta orden describe este
	// binario y el motor es un dato extra, no una condición.
	motor := ""
	if ruta := rutaDelMotor(); ruta != "" {
		if id, err := engineid.Scan(ruta); err == nil {
			motor = id.String()
		}
	}

	if op.json {
		fmt.Printf("{\"version\":%q,\"revision\":%q,\"dirty\":%v,\"engine\":%q,\"go\":%q,\"os\":%q}\n",
			Version, revisión, sucio, motor, runtime.Version(), runtime.GOOS)
		return nil
	}

	// Una versión para las tres caras, y decirlo es parte de la respuesta: el
	// daemon, la terminal y la ventana salen del mismo corte y viajan juntos.
	fmt.Printf("kanpachi %s (daemon, cli, ui)\n", Version)
	if revisión != "" {
		marca := ""
		if sucio {
			marca = " (with uncommitted changes)"
		}
		fmt.Printf("  commit  %s%s\n", revisión, marca)
	}
	if motor != "" {
		fmt.Printf("  engine  %s\n", motor)
	}
	fmt.Printf("  go      %s on %s/%s\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)
	return nil
}

// revisión lee el sellado de git que `go build` deja en el binario.
//
// Devuelve vacío cuando se compiló con `-buildvcs=false`, que es lo que hace
// `scripts/build-deb.sh` cuando el checkout es de otro usuario. Ahí no hay nada
// que decir y se calla, en vez de inventar un valor.
func revisión() (string, bool) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "", false
	}
	var rev string
	var sucio bool
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			sucio = s.Value == "true"
		}
	}
	return rev, sucio
}
