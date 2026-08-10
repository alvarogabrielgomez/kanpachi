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
// # El manifiesto dice `asInvoker`, y eso NO es decorativo
//
// La primera versión de este recurso salió sin manifiesto ninguno, con el
// argumento de que este binario ya lo lanza alguien elevado y meterle uno le
// cambiaría el arranque a cambio de nada. **Ese argumento estaba mal**, y lo
// desmintió una medición del 2026-08-10: `kanpachi-engine.exe`, que tampoco
// tenía manifiesto, hacía aparecer el diálogo de Control de cuentas de usuario
// a su nombre, con el daemon lanzándolo por `CreateProcess`, que hereda el token
// y no eleva nada.
//
// La causa no está en este repositorio. KB5089549 y KB5087051 cambiaron la
// lógica de Windows para INFERIR si un ejecutable **sin manifiesto embebido**
// necesita elevación, y esa inferencia ya alcanza a los binarios de 64 bits.
// Declarar `asInvoker` es el arreglo que documenta Microsoft.
//
// No estorba al camino que SÍ eleva: `ArrancarSuelto` pide elevación con el
// verbo `runas` de `ShellExecute`, explícitamente, y eso sigue funcionando.
// `asInvoker` no dice "nunca elevado", dice "no me eleves por tu cuenta".
//
// # Cómo se regenera
//
// La fuente es `winres/winres.json`, y es json y no banderas porque el modo
// `simply` de go-winres no tiene campo para `Comments`, que es donde va la
// descripción larga. `FileDescription` es un NOMBRE: lo que se ve es una
// columna estrecha del Administrador de tareas.
//
// # El icono es PROPIO, y no el del producto
//
// El daemon lleva el suyo: fondo gris con el engranaje naranja, contra el naranja
// liso del producto y el engranaje blanco del motor. Los tres se distinguen en
// la lista del Administrador de tareas, que es de donde salió todo esto.
//
// La fuente es `logos/kanpachi_daemon_icon.svg`. Los cuatro PNG de al lado se
// rasterizan con Inkscape y se versionan, porque `go-winres make` no sabe leer
// un SVG y tampoco el `.ico` del producto (contesta `image: unknown format`).
// Se dan los cuatro tamaños en vez de dejar que go-winres reduzca el de 256:
// el engranaje a 16 píxeles se convierte en una mancha si lo escala una máquina
// en vez de dibujarlo el rasterizador.
//
//	& 'C:\Program Files\Inkscape\bin\inkscape.exe' logos\kanpachi_daemon_icon.svg `
//	    --export-type=png --export-filename=daemon\cmd\kanpachid\winres\icon.png `
//	    --export-width=256 --export-height=256
//
// El `.syso` se versiona, porque es una ENTRADA de la compilación y no un
// resultado: sin él, un clon recién hecho produciría un ejecutable sin nombre y
// sin icono, y nadie se enteraría hasta verlo en el Administrador de tareas.
// Fuera de Windows amd64 el enlazador lo ignora solo, así que el job de Linux ni
// se entera.
//
//go:generate go run github.com/tc-hib/go-winres@latest make --arch amd64 --out rsrc --product-version git-tag --file-version git-tag
package main
