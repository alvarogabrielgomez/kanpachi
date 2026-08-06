//go:build !windows

package steam

import "fmt"

// steamRoot fuera de Windows no resuelve nada.
//
// Existe para que el paquete compile en el job de Linux, que es lo que mantiene
// honesta la regla de dependencia del proyecto. El lector de VDF y el de
// manifiestos SÍ compilan y corren en cualquier sitio, que es lo que interesa:
// son texto puro y son donde está la lógica. Lo único que Windows aporta es
// dónde empezar a mirar, y para eso está [NewAt].
func steamRoot() (string, error) {
	return "", fmt.Errorf("la detección de Steam solo está escrita para Windows")
}
