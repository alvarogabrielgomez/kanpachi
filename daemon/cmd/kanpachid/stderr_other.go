//go:build !windows

package main

import "os"

// redirigirStderr no hace nada fuera de Windows.
//
// Donde este daemon no corre como servicio de Windows, la salida de errores ya
// existe y la recoge quien lo haya lanzado. La captura del pánico es una
// respuesta a que un servicio con `-H windowsgui` no tenga ninguna.
func redirigirStderr(*os.File) error { return nil }
