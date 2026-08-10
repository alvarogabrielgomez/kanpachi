// Los recursos de Windows del daemon: su nombre y su icono.
//
// # Por qué existe
//
// Sin esto, el Administrador de tareas de Windows lista este proceso como
// `kanpachid.exe`, con el icono genérico de consola, en medio de una lista donde
// todo lo demás dice su nombre en palabras. Quien mire ahí para entender por qué
// su PC está haciendo algo se encuentra un ejecutable sin identificar, que es
// exactamente la forma que tiene lo que uno quiere cerrar.
//
// La columna «Nombre» del Administrador de tareas muestra el `FileDescription`
// del recurso VERSIONINFO, y no el nombre del archivo. Así que se lo damos:
// **Kanpachi service**.
//
// # Lo que este recurso NO trae, a propósito
//
// **No trae manifiesto** (`--manifest none`), y esa es la diferencia con el del
// bundle. El del bundle lleva `requireAdministrator`, que es lo que hace que
// Windows lo eleve al arrancar. Acá no toca: este binario ya lo lanza alguien
// elevado —el bundle, o el gestor de servicios— y meterle un manifiesto le
// cambiaría el arranque a cambio de nada. Un recurso que existe para poner un
// nombre no tiene por qué mover cómo se eleva un proceso.
//
// # Cómo se regenera
//
// El `.syso` se versiona, porque es una ENTRADA de la compilación y no un
// resultado: sin él, un clon recién hecho produciría un ejecutable sin nombre y
// sin icono, y nadie se enteraría hasta verlo en el Administrador de tareas.
// Fuera de Windows amd64 el enlazador lo ignora solo, así que el job de Linux ni
// se entera.
//
//go:generate go run github.com/tc-hib/go-winres@latest simply --arch amd64 --out rsrc --icon ../../../ui/windows/runner/resources/app_icon.ico --manifest none --product-name Kanpachi --product-version git-tag --file-version git-tag --file-description "Kanpachi service" --original-filename kanpachid.exe --copyright "Accentio Studios"
package main
