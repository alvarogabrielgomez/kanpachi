//go:build !bundle

package main

import "embed"

// Sin la etiqueta `bundle` no hay nada empotrado.
//
// Existe para que `go build ./...` y el `go vet` del CI compilen este paquete
// sin necesitar los 90 MB de carga en el árbol. El binario que sale de aquí es
// válido y no sirve para nada: lo dice al arrancar y se va.
var carga embed.FS

const hayCarga = false
