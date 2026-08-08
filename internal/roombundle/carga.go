//go:build bundle

package main

import "embed"

// carga son los cinco ficheros empotrados dentro de este ejecutable.
//
// `go:embed` los mete en el binario en tiempo de COMPILACIÓN, así que el
// `roombundle.exe` que sale pesa lo que pesan ellos: unos 48 MB. No hay
// descarga, no hay red y no hay nada que buscar en la máquina de destino.
//
// El patrón nombra un directorio, así que si falta alguno de los cinco la
// compilación no falla: sale un bundle incompleto. Por eso los comprueba
// [ficheros] al arrancar, y por eso el script los verifica antes de compilar.
//
//go:embed carga
var carga embed.FS

// hayCarga distingue este fichero de su gemelo `carga_stub.go`. Ver el doc del
// paquete: sin la etiqueta `bundle` el binario compila y se niega a correr, en
// vez de producir un bundle vacío que falla en la máquina de otra persona.
const hayCarga = true
