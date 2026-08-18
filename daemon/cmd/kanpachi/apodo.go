package main

// El nombre del EQUIPO, que es con lo que se nombra una sala sin nombre.
//
// # Lo que este fichero ya no hace
//
// Guardar el apodo. Tuvo un `nickname.txt` propio en el directorio de datos, con
// un comentario que decía que compartirlo con `roomprobe` evitaba que alguien
// acabara «con dos nombres distintos según con qué haya entrado». Unificó dos de
// las tres caras y se olvidó de la ventana, que guardaba el suyo al lado, así
// que produjo exactamente lo que existía para evitar: medido el 2026-08-18, una
// ventana que decía «Alvaro» y una sala que enseñaba «AlvaroGDeskt».
//
// Hoy el apodo lo guarda el daemon y esta cara se lo pregunta. Ver `apodo` en
// `comandos.go` y [protocol.MethodNickname]. La derivación desde el nombre del
// equipo también se fue, a [domain.NicknameFromHost], por lo mismo: era una
// copia, y encima la de acá escribía su resultado.

import (
	"os"
	"strings"
)

// nombreDelEquipo es el nombre de la máquina, para ponérselo a la SALA cuando
// quien la abre no le puso ninguno. No es el apodo de nadie.
//
// Viaja DENTRO de la tarjeta cifrada y el registro no lo conoce, así que esto no
// filtra el nombre del servidor a ningún tercero: lo ve quien recibe el enlace,
// que es exactamente a quien se le quiso dar.
func nombreDelEquipo() string {
	h, err := os.Hostname()
	if err != nil || strings.TrimSpace(h) == "" {
		return "Kanpachi"
	}
	return h
}
