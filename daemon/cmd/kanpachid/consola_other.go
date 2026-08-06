//go:build !windows

package main

// reengancharConsola no hace nada fuera de Windows.
//
// El problema que resuelve es de Windows entero: los subsistemas de enlace, el
// que un binario gráfico nazca sin descriptores válidos, y que el Explorador
// abra una ventana negra al lanzar un programa de consola. Nada de eso existe
// acá, donde un proceso hereda los descriptores de su padre y punto.
func reengancharConsola() {}
