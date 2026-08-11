//go:build windows

package paths

import (
	"os"
	"path/filepath"
)

// Data es dónde viven el token, la llave y la sala del producto instalado.
//
// Lo crea el INSTALADOR con una ACL propia, y por eso el arranque del daemon se
// niega si no está en vez de crearlo: esa ACL es la mitad de la protección de
// todo lo que hay dentro, y crearlo por accidente la perdería en silencio. Ver
// el rechazo en `daemon/cmd/kanpachid/main.go` y el porqué en `protegerFichero`.
//
// La ACL da lectura a los usuarios de la máquina a propósito, porque ahí viven
// ficheros que la interfaz necesita leer sin elevar, `api.token` entre ellos.
// Eso es lo que hace que el CLI de Windows no necesite elevación para hablar por
// el canal, al revés que el de Linux. La excepción es `identity.key`, que lleva
// ACL propia.
//
// **El producto portable NO usa esta ruta**, y por eso el CLI acepta `--data`:
// el portable guarda su estado junto al ejecutable, en la carpeta que eligió
// quien lo descomprimió, y nada de acá puede adivinarla.
func Data() string { return filepath.Join(os.Getenv("ProgramData"), "Kanpachi") }
