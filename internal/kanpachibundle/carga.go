//go:build bundle

package main

import "embed"

// carga es la carpeta portable ENTERA, empotrada en este ejecutable.
//
// `all:` y no `carga` a secas, y la diferencia importa: el patrón normal se
// salta los archivos y directorios que empiezan por `.` o `_`, y el bundle de
// Flutter trae varios dentro de `data\flutter_assets\`. Sin el prefijo saldría
// un bundle al que le faltan assets, y el síntoma sería una interfaz que abre
// en blanco.
//
// Lo que pesa esto es lo que pesa el binario: unos 90 MB, sin descargas ni red
// en la máquina de destino.
//
//go:embed all:carga
var carga embed.FS

// hayCarga distingue este archivo de su gemelo `carga_stub.go`. Ver el doc del
// paquete: sin la etiqueta `bundle` el binario compila y se niega a correr, en
// vez de producir un bundle vacío que falla en la máquina de otra persona.
const hayCarga = true
