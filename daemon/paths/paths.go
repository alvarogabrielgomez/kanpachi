// Package paths dice dónde vive lo que Kanpachi guarda entre arranques.
//
// # Por qué es un paquete y no una constante en cada binario
//
// Porque son DOS binarios que tienen que coincidir. El daemon escribe
// `api.token` en el directorio de datos, y el CLI lo lee de ahí para poder
// saludar por el canal de control. Escrita dos veces, el día que una de las dos
// cambie el síntoma es un CLI que dice que el daemon no está corriendo con el
// daemon corriendo perfectamente, que es de los fallos que se buscan durante
// horas en el sitio equivocado.
//
// Esto vivía en `wiring_linux.go`, exportado, con un comentario que decía que el
// `.deb` y el CLI tenían que nombrar exactamente esa ruta. El CLI no podía:
// aquello es `package main`, así que no lo importa nadie y la promesa del
// comentario no la sostenía nada.
//
// # Qué NO está acá
//
// El catálogo que viene con la instalación y el nombre del ejecutable del motor,
// que se quedan en el cableado del daemon. La diferencia es quién los mira: esos
// dos los lee UN binario, y una constante que usa uno solo no se puede
// desincronizar con nadie.
package paths
