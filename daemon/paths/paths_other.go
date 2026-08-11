//go:build !windows && !linux

package paths

// Data contesta la ruta de Unix aunque acá no arranque nada.
//
// El directorio de datos se resuelve ANTES que el firewall, así que devolver
// vacío haría que el fallo del daemon saliera como "no está el directorio" en
// vez del "este binario no es de tu sistema" que corresponde, y eso manda a
// quien lo lee a crear un directorio que no le va a servir de nada.
//
// Ver `wiring_other.go`, que es quien de verdad se niega.
func Data() string { return "/var/lib/kanpachi" }
