//go:build !windows && !linux

package steam

import "fmt"

// steamRoot fuera de Windows y de Linux no resuelve nada.
//
// Existe para que el paquete compile en cualquier sistema, que es lo que
// mantiene honesta la regla de dependencia del proyecto. El lector de VDF y el de
// manifiestos SÍ compilan y corren en cualquier sitio, que es lo que interesa:
// son texto puro y son donde está la lógica. Lo único que Windows aporta es
// dónde empezar a mirar, y para eso está [NewAt].
func steamRoot() (string, error) {
	return "", fmt.Errorf("la detección de Steam está escrita para Windows y para Linux")
}
