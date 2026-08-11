package main

// La VARIANTE del producto, que es un eje distinto del sistema operativo.
//
// # El error que esto corrige
//
// La primera versión decidía "¿hospeda una ventana?" en `wiring_linux.go` y
// `wiring_windows.go`, o sea ataba **headless** a **Linux**. Son dos preguntas
// separadas y confundirlas cierra la puerta a lo que ya se ve venir: una
// interfaz de Linux, para el que quiera Kanpachi en su escritorio y no en un
// servidor. El día que exista, con la decisión atada al sistema habría que
// desatarla a mano en varios sitios a la vez.
//
// Así que el sistema decide lo que es del sistema —rutas, llamadas, dónde vive
// un fichero— y la variante decide qué FORMA tiene el producto.
//
//	              headless                    desktop
//	Windows       -tags headless              por omisión
//	Linux         por omisión                 -tags desktop
//
// Las etiquetas por omisión son las de hoy, y eso es a propósito: `go build ./...`
// sigue funcionando en los dos CI sin que nadie pase nada, y quien quiere la otra
// forma la PIDE. El `.deb` pasa `-tags headless` igual, aunque hoy sea lo mismo
// que el valor por omisión: es lo que hace que el día que Linux tenga interfaz,
// el paquete del servidor siga siendo el del servidor.
//
// # Qué gobierna, y qué va a gobernar
//
// Hoy son dos cosas, las dos sobre la ventana: si el daemon la lanza él, y cómo
// se llama su ejecutable.
//
// **Lo que falta decidir, y conviene decirlo ahora que se ve:** en Linux el
// canal de control es un socket 0600 de root, y esa decisión se tomó para el
// caso headless, donde el cliente es un CLI que se corre con `sudo`. Una interfaz
// de escritorio corre como el USUARIO, así que con ese modo no podría abrirlo.
// La variante de escritorio va a necesitar otro modelo de acceso —un grupo con
// 0660, o polkit— y esa elección es de seguridad, no de empaquetado: hay que
// tomarla mirándola, no de paso. Ver `pipe_linux.go`.

// hostsUI dice si el daemon lanza la ventana él mismo.
//
// Lo define exactamente uno de los ficheros de variante. Que sean excluyentes lo
// comprueba el compilador: si dos coincidieran, el símbolo estaría declarado dos
// veces y no compila.
var _ = hostsUI

// uiExeName es cómo se llama el ejecutable de la interfaz, al lado del daemon.
var _ = uiExeName
