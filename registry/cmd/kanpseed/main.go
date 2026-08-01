// kanpseed es el nodo de encuentro de Kanpachi.
//
// Un solo binario con subcomandos: `serve` es lo que arranca systemd, y
// `init`, `doctor`, `config` y `nginx` son lo que ejecuta una persona.
//
// Se llama kanpseed y no kanpachi a propósito: `kanpachi` queda reservado para
// el cliente de terminal de Linux, que entrará y creará salas, y que es otra
// cosa. Tenerlos con el mismo nombre en la misma máquina sería una trampa.
package main

import (
	"os"

	"github.com/accentiostudios/kanpachi/registry/cli"
)

func main() {
	os.Exit(cli.Ejecutar(os.Args[1:]))
}
