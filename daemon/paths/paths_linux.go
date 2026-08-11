//go:build linux

package paths

// Data es dónde viven el token, la llave y la sala.
//
// `/var/lib/kanpachi` es lo que dice el estándar de jerarquía de ficheros para
// el estado que un servicio tiene que conservar entre reinicios, y es lo que el
// `.deb` crea. No es `/etc/kanpachi`, que es solo para configuración, ni
// `/run/kanpachi`, que se borra al arrancar: la llave de identidad no puede
// perderse en un reinicio o el equipo deja de ser el mismo para quienes ya
// jugaron con él.
//
// El directorio es de root y va en 0700, así que el CLI necesita `sudo` para
// leer el token. Es la misma decisión que el modo del socket de control, y por
// el mismo motivo: quien puede abrir salas y escribir en el firewall ya puede
// ser root en esa máquina, y una vía que no pasa por `sudo` solo quitaría el
// registro de quién hizo qué.
func Data() string { return "/var/lib/kanpachi" }
