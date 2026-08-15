package main

import (
	"os"
	"path/filepath"

	"github.com/accentiostudios/kanpachi/internal/layout"
)

// El modo PORTABLE: una carpeta que se copia y funciona.
//
// # Qué problema resuelve
//
// El producto instalado apoya en tres cosas que un instalador deja puestas: un
// servicio registrado, un directorio de datos en ProgramData con su ACL, y un
// permiso concedido al usuario interactivo para arrancar ese servicio. Nada de
// eso viaja dentro de un ZIP.
//
// Portable es la respuesta a "mándame la carpeta y la abro": el daemon corre en
// SU PROPIO proceso en vez de como servicio, y sus datos viven junto al
// ejecutable en vez de en ProgramData. No hay nada que registrar, así que no hay
// nada que desinstalar: se borra la carpeta y no queda rastro, salvo las reglas
// de firewall, que el daemon purga al apagarse igual que siempre.
//
// # Por qué un fichero marcador y no una bandera
//
// Porque la pregunta tiene que contestarse IGUAL en tres sitios que no comparten
// línea de comandos: el lanzador que arranca de un doble clic sin argumentos, el
// daemon que arranca después, y la interfaz de Flutter, que es otro ejecutable
// entero. Una bandera habría que acordarse de pasarla en los tres, y el olvido
// es silencioso: el daemon escribiría su token en ProgramData y la interfaz lo
// buscaría al lado del binario, o al revés, y el síntoma sería "no hay
// servicio" con el servicio corriendo. Ya pasó cuatro veces en este repositorio
// con el nombre del pipe.
//
// Con un fichero, la respuesta es una propiedad de la CARPETA. Quien la copia se
// lleva el marcador, y los tres procesos deducen lo mismo sin hablar entre
// ellos.
//
// # Lo que NO cambia
//
// Todo lo demás. Pipe PROPIO bajo el mismo prefijo protegido, mismo saludo con
// token, misma cuarentena, mismo motor. Portable no es un modo degradado: es el
// mismo producto con dueño, datos y ciclo de vida independientes del instalado.
const (
	// PortableMarker es el fichero cuya presencia junto al binario convierte la
	// carpeta en un Kanpachi portable. Su contenido no se lee.
	PortableMarker = "kanpachi.portable"

	// ArgDaemon es con lo que el lanzador se relanza a sí mismo para SER el
	// daemon de la carpeta portable.
	//
	// Escrito acá y no donde se declara la bandera porque hay dos que lo
	// escriben —quien la define y quien la manda— y son el mismo binario en dos
	// momentos distintos. Con la cadena en un solo sitio, no se pueden
	// desincronizar.
	ArgDaemon = "--daemon"
)

// esPortable dice si este binario vive en una carpeta portable.
func esPortable() bool {
	_, err := os.Stat(filepath.Join(dirDelBinario(), PortableMarker))
	return err == nil
}

// dirDeDatos resuelve el directorio de datos, vacío incluido.
//
// El orden importa y es el único que tiene sentido: lo que se pidió a mano gana
// siempre, después la carpeta portable, y el del sistema es el default del
// producto instalado.
//
// La rama portable se evalúa en los dos sistemas y solo puede dar verdadero en
// Windows, porque el marcador viaja dentro de un ZIP que solo se publica ahí.
// Dejarla puesta cuesta un `Stat` por arranque y evita que este fichero tenga
// que saber en qué sistema corre para contestar una pregunta sobre una carpeta.
func dirDeDatos(datos string) string {
	if datos != "" {
		return datos
	}
	if esPortable() {
		return filepath.Join(dirDelBinario(), layout.PortableDataDir)
	}
	return defaultDataDir()
}
