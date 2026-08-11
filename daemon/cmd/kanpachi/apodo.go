package main

// El nombre con el que se entra a una sala, y dónde se recuerda.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// apodoFile es dónde se recuerda el nombre.
//
// El mismo fichero y el mismo directorio que usa `roomprobe`, a propósito: son
// dos clientes de la misma máquina y preguntar dos veces lo mismo es lo que hace
// que alguien acabe con dos nombres distintos según con qué haya entrado.
const apodoFile = "nickname.txt"

func rutaApodo(datos string) string { return filepath.Join(datos, apodoFile) }

func leerApodo(datos string) string {
	b, err := os.ReadFile(rutaApodo(datos))
	if err != nil {
		// No se dice nada: que no exista es el caso normal la primera vez, y que
		// no se pueda leer ya se va a notar enseguida al no poder leer el token,
		// que vive en el mismo directorio y con los mismos permisos.
		return ""
	}
	return strings.TrimSpace(string(b))
}

// guardarApodo escribe el nombre, y si no puede lo DICE.
//
// Callarlo haría que el nombre se volviera a preguntar en cada arranque sin que
// nadie supiera por qué. En Linux la causa va a ser casi siempre la misma —el
// directorio es de root y esto corrió sin `sudo`— y entonces el token tampoco se
// pudo leer, así que este aviso llega antes de un fallo que sí corta.
func guardarApodo(datos, nombre string) {
	if err := os.WriteFile(rutaApodo(datos), []byte(nombre), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, "kanpachi: could not remember the name:", err)
	}
}

// nombreDelEquipo es el nombre de la máquina, para ponérselo a la sala.
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

// nombreDeApodoPorDefecto saca un apodo legal del nombre del equipo.
//
// # Por qué hay que limpiarlo y no basta con el hostname
//
// Porque las reglas del apodo son mucho más estrechas que las de un nombre de
// máquina: solo letras y dígitos ASCII, hasta doce. Un servidor llamado
// `mc-01.casa.lan` es un nombre de host perfectamente normal y un apodo
// inválido, y el error que sale del daemon es `bad_nickname`, que no explica
// nada a quien nunca eligió un apodo.
//
// Se descarta el dominio antes de limpiar: `mc-01.casa.lan` quiere ser `mc01`, y
// arrastrar `casalan` gastaría el tope de doce en la parte que no identifica a
// nadie.
func nombreDeApodoPorDefecto() string {
	base, _, _ := strings.Cut(nombreDelEquipo(), ".")

	var limpio strings.Builder
	for _, r := range base {
		esLetra := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		esDígito := r >= '0' && r <= '9'
		if esLetra || esDígito {
			limpio.WriteRune(r)
		}
		if limpio.Len() >= domain.NicknameMaxLen {
			break
		}
	}
	if limpio.Len() == 0 {
		// Un nombre de máquina que no deja ni una letra ni un dígito existe
		// (`_`, o uno entero en otro alfabeto). Antes que fallar, un nombre que
		// vale, y que además dice de dónde salió.
		return "kanpachi"
	}
	return limpio.String()
}
