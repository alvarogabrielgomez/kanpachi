# Arquitectura

## Vista general

```
┌──────────────────────────── PC Windows ────────────────────────────┐
│                                                                    │
│  kanpachi-ui  (Flutter, usuario, sin privilegios)                  │
│      │        lo lanza el daemon, muere con el daemon, y es quien  │
│      │        pone el icono en la bandeja. Ver "modelo de procesos"│
│      │  named pipe \\.\pipe\kanpachi  +  token                     │
│      ▼                                                             │
│  kanpachi-daemon  (servicio Windows, SYSTEM)                       │
│  ├── transport/   named pipe + protocolo JSON-RPC, y el canal de   │
│  │                control de la sala                               │
│  ├── service/     el orden de arranque y el supervisor:            │
│  │                latido, watchdog del motor, eventos de energía,  │
│  │                de red y de identificación de red                │
│  ├── cmd/         kanpachid: main, host del servicio y la          │
│  │                elección de los adaptadores concretos            │
│  ├── adapter/     lo único que conoce Windows y EasyTier:          │
│  │                firewall por COM, netcfg, motor, Steam,          │
│  │                iphlpapi, catálogo y estado en JSON              │
│  └── core/        sin I/O, sin syscalls, sin API de Windows        │
│      ├── domain/    tipos y reglas puras, invariantes             │
│      ├── port/      las interfaces que el dominio necesita         │
│      ├── timing/    TODOS los relojes del producto, en un archivo  │
│      └── usecase/   una intención por archivo                      │
│                                                                    │
│  adaptador Wintun "kanpachi0"  ← creado por el instalador          │
└────────────────────────────────│───────────────────────────────────┘
                                 │
                                 │  internet
                                 ▼
        kanpachi-seed (droplet, systemd)        otros peers
        rendezvous + coordinación de      ◄──── P2P directo WireGuard
        hole punch + relay de último            (o vía relay si el
        recurso                                  NAT no cede)
```

`core/` no vive dentro de `daemon/`, vive al lado. Se dibuja acá para que se lea la dirección de las dependencias: todo apunta hacia adentro, y `core` no conoce a ninguno de los de arriba.

## El modelo de procesos

Tres procesos, y uno solo manda.

```
sesión 0  (aislada, sin escritorio, sin área de notificación)
│
│  kanpachid.exe                       servicio Windows, SYSTEM
│    │
│    ├──[job]──> kanpachi-engine.exe   sesión 0, SYSTEM
│    │
└────┼───────────────────────────────────────────────────────────────
     │
sesión del usuario
     │
     └──[job]──> kanpachiui.exe        Flutter, SIN elevar
                   el icono de la bandeja vive acá
                   habla con el daemon por el named pipe
```

En el Administrador de tareas de Windows los tres se ven por su nombre y no por el del archivo, porque esa columna muestra el `FileDescription` del recurso VERSIONINFO:

| Ejecutable | Lo que dice el Administrador de tareas | De dónde sale |
|---|---|---|
| `kanpachid.exe` | Kanpachi service | `daemon/cmd/kanpachid/rsrc_windows_amd64.syso` |
| `kanpachi-engine.exe` | Kanpachi tunnel engine | `build.rs` del motor, que genera el recurso y lo compila con `rc.exe` |
| `kanpachiui.exe` | Kanpachi UI | `ui/windows/runner/Runner.rc` |
| `kanpachi-portable.exe` | Kanpachi portable wrapper | `internal/kanpachibundle/rsrc_windows_amd64.syso` |

Procesos sin identificar en esa lista son cosas que alguien puede decidir cerrar sin saber qué son, y este producto pide administrador.

El cuarto solo existe corriendo el portable de un archivo, y su nombre dice lo que es: un envoltorio. Extrae, lanza el daemon, espera, y borra la carpeta temporal al salir. Esperar es su único motivo para seguir vivo.

La descripción larga de cada uno va en `Comments`, que es el campo que Windows enseña en Propiedades del archivo. `FileDescription` se queda con el nombre corto porque lo que lo muestra es una columna estrecha. El servicio además lleva la suya propia, escrita con `sc description` por el instalador, que es la que sale en `services.msc`.

**Y cada uno lleva su propio icono**, porque un nombre en una lista donde los cuatro iconos son iguales sigue obligando a leer para distinguirlos:

| Ejecutable | Icono | Fuente |
|---|---|---|
| `kanpachid.exe` | gris, engranaje naranja | `logos/kanpachi_daemon_icon.svg` |
| `kanpachi-engine.exe` | naranja, engranaje blanco | `logos/kanpachi_engine_icon.svg`, rasterizado y versionado como `.ico` en el repositorio del motor |
| `kanpachiui.exe` | naranja, sin engranaje | `ui/windows/runner/resources/app_icon.ico` |
| `kanpachi-portable.exe` | naranja, sin engranaje | el mismo, que es el del producto |

Los PNG que consume `go-winres` se versionan al lado de su `winres.json` y salen de los SVG con Inkscape, en 16, 32, 48 y 256. **Se dibuja cada tamaño en vez de escalar el de 256**: un engranaje reducido por una máquina a 16 píxeles es una mancha.

#### Los cuatro declaran `asInvoker`, y tres de ellos no lo hacían

**Un ejecutable sin `requestedExecutionLevel` en su manifiesto se lo deja adivinar a Windows, y Windows cambió de opinión.** Los parches KB5089549 y KB5087051 extendieron la inferencia de elevación a los binarios de 64 bits, que antes quedaban fuera. Medido el 2026-08-10: `kanpachi-engine.exe` hacía aparecer el diálogo de Control de cuentas de usuario **a su nombre**, con el daemon lanzándolo por `CreateProcess`, que hereda el token y no eleva nada. Ningún manifiesto, ninguna capa de compatibilidad, ningún script con `runas`: lo elevaba la heurística.

Los tres que faltaban ahora lo declaran, y cada uno por un motivo distinto:

| | Antes | Ahora | Por qué importa |
|---|---|---|---|
| `kanpachi-engine.exe` | sin manifiesto | `asInvoker` | Es el que se midió pidiendo UAC sin que nadie lo elevara |
| `kanpachid.exe` | sin manifiesto | `asInvoker` | Elevar es cosa de `ArrancarSuelto`, que lo pide con el verbo `runas` a propósito. Que Windows adivine se lo saltea |
| `kanpachiui.exe` | manifiesto **sin** `trustInfo` | `asInvoker` | Es el peor caso de los tres: esta ventana tiene que correr SIN privilegios por diseño, y una elevación adivinada rompería lo que `spawn_windows.go` se toma el trabajo de garantizar con el token del Explorador |

`kanpachi-portable.exe` ya declaraba `requireAdministrator` y se queda igual: es el único que sí necesita elevarse, y es donde vive el UAC único del portable de un archivo.

**`asInvoker` no dice «nunca elevado», dice «no me eleves por tu cuenta».** El camino que sí eleva sigue siendo explícito y sigue funcionando.

`[job]` es un Job Object con `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`, el mismo mecanismo que ya usaba el motor. De ahí sale la invariante que gobierna todo lo demás:

> **Hay bandeja si y solo si hay daemon.**

Mientras el daemon vive, hay un icono que lo dice. Cuando el daemon muere, por la vía que sea (apagado ordenado, `TerminateProcess` desde el Administrador de tareas, un fallo), **el kernel se lleva el motor y la UI con él**, y el icono desaparece. No es código que corre en el cierre: es una propiedad del objeto, así que no hay ninguna muerte del daemon lo bastante sucia como para dejar un túnel vivo sin nada visible que lo explique.

Un túnel abierto sin interfaz que lo enseñe tiene la forma exacta de un troyano. Esa es la razón de la invariante, y es de producto antes que técnica.

### Por qué la bandeja está en la UI y no en el daemon

Porque no hay alternativa. **Un proceso de la sesión 0 no puede poner un icono en la bandeja**: esa sesión no tiene escritorio interactivo ni área de notificación. Los servicios interactivos (`SERVICE_INTERACTIVE_PROCESS`) los retiró Microsoft en Windows 10 1803. Y un servicio que corriera como usuario en vez de SYSTEM seguiría en la sesión 0, porque la sesión no depende de la cuenta.

Así que el icono lo pone el único proceso que puede: uno sin elevar, en la sesión del usuario. Ese proceso ya existía y ya tenía el código de la bandeja escrito. Lo que cambia es **quién lo arranca**.

### Por qué el daemon lanza la UI, y no al revés

Si la UI se lanzara sola, la bandeja podría sobrevivir al daemon o faltar con el daemon vivo, y la invariante se cae. Con el daemon de padre no hay nada que vigilar ni que sincronizar: el `job` lo resuelve el kernel.

Sale gratis, además, la detección del cierre abrupto de la UI. El daemon es el padre, así que tiene el handle del proceso desde que nace y lo único que hace es esperar sobre él. Cuando la UI se cierra de golpe la relanza en modo silencioso, con tope: si vuelve a caer enseguida, apaga todo en vez de insistir.

### Cómo lanza el daemon un proceso con menos privilegios que él

El daemon es SYSTEM en la sesión 0. Un `CreateProcess` normal daría una ventana de Flutter corriendo como administrador, que es superficie de ataque regalada y además rompe cosas visibles, como arrastrar un fichero desde el Explorador, que UIPI bloquea contra una ventana elevada.

```
WTSGetActiveConsoleSessionId()
WTSQueryUserToken(sid, &tok)
GetTokenInformation(tok, TokenElevationType)
    si es TokenElevationTypeFull -> GetTokenInformation(tok, TokenLinkedToken)
DuplicateTokenEx(..., TokenPrimary, &prim)
CreateProcessAsUser(prim, os.Executable() del directorio propio, ...,
                    STARTUPINFO{ lpDesktop: "winsta0\\default" })
AssignProcessToJobObject(job, hProcess)
```

Tres cosas que hay que respetar, y las tres vienen de la documentación de Microsoft:

- **`WTSQueryUserToken` exige LocalSystem y `SE_TCB_NAME`**, textual. Es otro motivo por el que el daemon es un servicio como SYSTEM: como proceso de usuario, ni elevado, no podría lanzar su propia UI en condiciones.
- **La documentación no dice cuál de los dos tokens devuelve** para un administrador con UAC, el completo o el filtrado. Por eso el paso de `TokenElevationType` más `TokenLinkedToken` no es adorno: es lo único que garantiza que la UI no salga elevada por accidente. Windows fabrica los dos tokens en el inicio de sesión y el filtrado es el que corre el Explorador, así que ese es el que queremos.
- **Los handles de token se cierran los tres.** La documentación advierte explícitamente de no filtrarlos.

**La ruta que se lanza sale de `os.Executable()` y del directorio propio, jamás del estado ni de la configuración ni del pipe.** Un SYSTEM que lanza una ruta que alguien puede influir es escalada de privilegios directa.

#### El proceso nace suspendido, y pide salirse del job del padre

Dos banderas que no estaban, y las dos vienen de un fallo medido en la carpeta portable: `AssignProcessToJobObject` contestaba `Access is denied`, la interfaz no arrancaba, el vigilante lo contaba como caída, y a la tercera **Kanpachi se apagaba entero, con la sala dentro**.

- **`CREATE_SUSPENDED`.** Sin ella el proceso arranca corriendo y hace lo primero de todo su comprobación de instancia única: si ya hay una, avisa y se mata. A un proceso que ya terminó no se le puede meter en un job. De paso, suspendido es lo que hace cierta la promesa de que el job queda puesto antes de que la interfaz ejecute una sola instrucción.
- **`CREATE_BREAKAWAY_FROM_JOB`.** Un hijo nace dentro del job de su padre, y a un proceso que YA está en un job no se le puede meter en otro. Medido con `IsProcessInJob`, que contestaba que sí antes de intentarlo. Pasa cuando al daemon lo levanta algo que vive en un job, o sea la consola de una carpeta portable. La bandera es una PETICIÓN: si el job del padre no deja salirse, `CreateProcess` falla y se reintenta sin ella.

**Y si aun así no entra en el job propio, se sigue igual, con un aviso.** Antes se mataba la interfaz recién creada, que es cómo un tropiezo al abrir una ventana acababa costando la partida de cuatro personas.

**Lo que NO se puede dar por sentado es que el job del padre supla al propio.** Esto decía que sí, con el argumento de que el proceso nació dentro del job del daemon y muere con él lo mismo. Es falso en el bundle portable, y está medido el 2026-08-09: `internal/kanpachibundle` no crea ningún job, solo lanza el daemon y espera, así que el job que hay lo puso Windows al elevar y nadie controla qué pasa al cerrarlo. El síntoma era exacto: al salir por "Salir de Kanpachi", el daemon terminaba, el bundle borraba su carpeta temporal, y `kanpachiui.exe` seguía en la lista de procesos corriendo desde una carpeta que ya no existía.

Así que el daemon **anota si el job la sujeta**, y al cerrar mata a mano a la que quedó fuera. El job sigue siendo el camino primario, porque el kernel lo cumple pase lo que pase; matarla explícitamente es código de cierre, o sea justo lo que un `TerminateProcess` desde el Administrador de tareas se salta. Con las dos vías, la única forma de dejar una interfaz suelta es matar el daemon a lo bruto en portable, que es peor que lo que había y sigue siendo mejor que lo que se creía.

El aviso lleva el PID, si el proceso ya estaba en un job, y su código de salida. Hace falta: `Access is denied` a secas tiene tres causas con arreglos distintos, y sin esos datos no se distinguen.

### Un solo ejecutable, cuatro papeles, y el papel lo dice el parámetro

`kanpachid.exe` es el daemon. También es lo que arranca el daemon, y también es el modo desarrollo. No hay un binario auxiliar, y ese es el punto: un segundo ejecutable habría que mantenerlo sincronizado con este, y la sincronización entre dos binarios que se acompañan es exactamente la clase de contrato que se rompe sin dar error. Es el patrón de un motor de juego que corre dos juegos según con qué lo llamen.

| Línea de comandos | Quién lo invoca | Qué hace |
|---|---|---|
| `kanpachid.exe` | el Administrador de servicios | **es** el daemon |
| `kanpachid.exe --show` | el acceso directo, el enlace `kanpachi://` | **lanzador**: deja el daemon corriendo y la ventana a la vista |
| `kanpachid.exe --console` | quien programa | daemon de consola, con otro nombre de pipe |
| `kanpachid.exe --daemon` | el propio lanzador, en una carpeta portable, o el bundle portable | **es** el daemon, sin Administrador de servicios detrás |

El lanzador es el **default** cuando el proceso no lo arrancó el Administrador de servicios: quien encuentre este binario en Program Files y lo ejecute obtiene un Kanpachi corriendo, jamás un segundo daemon compitiendo con el que ya hay. Correr el daemon a mano hay que pedirlo con `--console` o con `--daemon`.

`--daemon` solo vale dentro de una carpeta portable y eso se comprueba antes de montar nada. Además, cada producto tiene su canal: `kanpachi-installed`, `kanpachi-portable` y `kanpachi-console`, todos bajo el prefijo protegido. Portable e instalado pueden correr a la vez sin robarse el lanzador, el token ni la ventana.

**Lo primero que hace el lanzador es preguntar si ya hay daemon, y lo pregunta por el pipe.** Podría preguntárselo al Administrador de servicios, que es más directo, y sería un mecanismo más que mantener: el pipe es a la vez la detección y la entrega. Si contesta, ya hay por dónde mandarle la orden de mostrarse; si no hay nadie, `CreateFile` falla al instante, que es justo el caso que tiene que ser rápido. Y se corrige solo: si la sonda falla porque el daemon está a mitad de arrancar, el arranque siguiente devuelve `ERROR_SERVICE_ALREADY_RUNNING` y se vuelve al pipe. Ninguna rama termina en dos daemons.

**Lo único que ese camino le pide al daemon es `show_ui`.** Un proceso que llega de fuera puede pedir que se enseñe una ventana y nada más, y lo que lo acota no es el lanzador, es la lista cerrada de métodos del protocolo más el saludo con token.

### Silencioso es el default de los dos ejecutables

| Cómo entra la UI | Ventana | Bandeja |
|---|---|---|
| El daemon la lanza sin bandera | no | sí |
| El daemon la lanza con `--show` | sí | sí |
| El usuario la abre a mano, sin daemon | **sí**, con el aviso de servicio ausente | **no** |

**La bandera pide MOSTRAR, no callar, y esa vuelta es deliberada.** Una bandera que se pierda por el camino —un argumento mal pasado, un lanzamiento nuevo que se olvida de ponerla— tiene que fallar hacia el lado callado. Al revés, el fallo es una ventana abriéndose sola encima de lo que estuvieras haciendo al encender la PC.

**Y el silencio tiene una condición: que haya bandeja.** Callarse significa "mi cara es el icono", y ese icono solo existe si hay daemon. Por eso el arranque silencioso pregunta primero, con una llamada al pipe que sin daemon falla en el acto. Sin daemon se enseña la ventana igual: lo contrario sería un proceso sin ventana, sin icono y sin forma de cerrarlo que no fuera el Administrador de tareas.

La tercera fila es la que protege la invariante por el otro lado. Una UI suelta son mandos sin nada que mandar, y está bien que se pueda abrir así. Lo que no puede hacer es poner un icono que promete algo que no está.

### Qué pasa al hacer doble clic

El acceso directo apunta al **daemon**, con `--show`.

| Estado | Qué ocurre |
|---|---|
| Windows arranca | el SCM levanta el servicio sin argumentos, y lanza la UI callada: bandeja, sin ventana |
| Doble clic, daemon parado | el lanzador arranca el servicio con `--show`, y el daemon lanza la UI con ventana |
| Doble clic, daemon vivo | el lanzador lo detecta por el pipe, le manda `show_ui`, y se muere |

Ninguna de las tres pide UAC. Arrancar el servicio no lo pide porque el instalador le concede al usuario interactivo `SERVICE_START`, `SERVICE_STOP` y `SERVICE_QUERY_STATUS` **sobre este servicio y ninguno más**, con `sc sdset`. Es una concesión mínima, hecha una vez, con el único UAC de la vida del producto. El gestor de servicios se abre con `SC_MANAGER_CONNECT` y el servicio con los dos permisos que hacen falta: pedir `SC_MANAGER_ALL_ACCESS`, que es lo que hace el ayudante habitual de la librería estándar de Go, falla sin elevar.

**El daemon se compila con `-H windowsgui`.** Con el subsistema de consola, que es lo que Go hace por defecto, el doble clic haría parpadear una ventana negra. Para no perder la salida de `--console` y `--reset`, se reengancha a la consola del padre con `AttachConsole(ATTACH_PARENT_PROCESS)` cuando la hay: lanzado desde una terminal imprime, lanzado desde el acceso directo no muestra nada. Cuando el lanzador falla y no hay consola a la que escribir, el error sale en una ventana de mensaje: sin eso, un acceso directo roto no haría nada visible.

### La carpeta portable

Hay una segunda forma de repartir Kanpachi, y no reemplaza al instalador: una carpeta que se copia y funciona. Es lo que se le manda a alguien en un ZIP, y lo que cabe en una llave USB.

Lo que la define es un fichero vacío junto al binario, `kanpachi.portable`. Con él presente cambian dos cosas y ninguna más:

- El daemon guarda sus datos en `kanpachi-data\`, ahí mismo, en vez de en `ProgramData\Kanpachi`.
- El daemon corre en su propio proceso con `--daemon`, en vez de como servicio.

**Se llama `kanpachi-data` y no `data` por una colisión medida.** El bundle de Windows de Flutter trae su propio `data\` con `icudtl.dat`, `app.so` y `flutter_assets\`, y en una carpeta portable los dos ejecutables comparten directorio: el daemon habría escrito su token y su `identity.key` entre los recursos de la interfaz, y limpiar los datos se habría llevado los assets por delante. Salió corriendo el script la primera vez, no leyendo el código.

**Es un fichero y no una bandera porque la pregunta la tienen que contestar igual tres procesos que no comparten línea de comandos**: el lanzador, que arranca de un doble clic sin argumentos; el daemon, que nace después; y la interfaz de Flutter, que es otro ejecutable entero. Una bandera habría que acordarse de pasarla en los tres, y el olvido es silencioso: el daemon escribiría su token al lado del binario y la interfaz lo buscaría en ProgramData, con el síntoma de "no hay servicio" delante de un servicio corriendo. Ese fallo exacto ya ocurrió cuatro veces en este repositorio con el nombre del pipe. Con un fichero, ser portable es una propiedad de la CARPETA, y quien la copia se la lleva.

Todo lo demás es idéntico: la misma disciplina de pipe bajo el prefijo protegido, el mismo saludo con token, la misma cuarentena de base, el mismo motor, el mismo job que se lleva la interfaz por delante. El NOMBRE del pipe y el evento de instancia única son distintos a propósito: portable no es una ventana alternativa del instalado, es otro producto completo que puede convivir con él. Portable no es un modo degradado.

Lo que cuesta, y se dice entero:

| | Instalado | Portable |
|---|---|---|
| UAC | uno solo, al instalar | uno por arranque de Kanpachi |
| Datos | `ProgramData\Kanpachi` con ACL propia del instalador | `kanpachi-data\` junto al binario, con los permisos de donde esté la carpeta |
| Arranque con Windows | sí, servicio con arranque retrasado | no, no hay servicio que Windows pueda levantar |
| Desinstalar | el desinstalador, con sus 11 pasos | borrar la carpeta |

El UAC por arranque es la consecuencia directa de no haber instalado nada: el permiso de arrancar el servicio se lo concede el instalador al usuario interactivo con `sc sdset`, y una carpeta que se copió no concedió nada. Así que el lanzador se relanza a sí mismo elevado con `ShellExecute` y el verbo `runas`, que es la única forma documentada de pedir elevación desde un proceso que no la tiene. Que el usuario diga que no es una respuesta y no un fallo: Windows devuelve `ERROR_CANCELLED` y el mensaje lo nombra como lo que es.

**El directorio de datos lo crea el daemon, y solo en portable.** En el producto instalado lo crea el instalador con su ACL, y crearlo desde el daemon perdería esa ACL en silencio. En portable no hay instalador, así que la alternativa a crearlo es que la carpeta no arranque nunca. Lo que se pierde queda escrito en la tabla de arriba.

La carpeta la arma `scripts\build-portable.ps1`, que compila las dos mitades, copia el catálogo y las DLL, escribe el marcador y arranca. Ver `04-flujos-y-configuracion.md`.

#### El bundle: esa misma carpeta dentro de un solo ejecutable

Una carpeta portable funciona y **no se puede mandar por chat**. Son quince archivos que hay que mantener juntos: el daemon, la interfaz con todo su bundle de Flutter —su DLL, sus plugins y su `data\`—, el motor, las DLL y el marcador. Un ZIP descomprimido a medias, o alguien que arrastra solo el `.exe` que reconoce, es una carpeta que no arranca y un "no me anda" sin ninguna pista.

`kanpachi-portable.exe` empotra esa carpeta entera con `go:embed`, la suelta en un directorio temporal, corre `kanpachid --daemon --show` y borra el temporal al salir. Unos 78 MB. Lo arma `scripts\build-portable-bundle.ps1`, y lo que empotra es la salida de `build-portable.ps1`, o sea la MISMA receta que se usa a mano: no hay dos listas de archivos que se puedan desincronizar, por lo mismo que el instalador copia `{#Carga}\*` en vez de enumerar.

Lo que lo hace funcionar, y qué pasa si falta:

- **Eleva el manifiesto, no el código.** El `.syso` del paquete lleva `requireAdministrator`, así que Windows eleva el proceso ANTES de que arranque. La alternativa —arrancar sin permisos y relanzarse elevado— deja DOS procesos, y con ellos dos ventanas y dos iconos en la barra de tareas. El relanzado sigue ahí como red de seguridad para un binario construido sin el `.syso`.
- **Un solo UAC en toda la sesión.** El daemon hereda el token elevado, y lanza el motor con un `exec.Command` normal, que lo hereda también. Abrir una sala no vuelve a preguntar nada.
- **Sin consola.** Se enlaza con `-H windowsgui`. Con consola quedaba una ventana negra abierta durante toda la sesión de juego y un segundo icono en la barra, que hace parecer que Kanpachi se abrió dos veces. Lo que se pierde son los mensajes de progreso; un fallo sale por un cuadro de diálogo, con la ruta del log dentro.
- **El log y los datos, los dos al lado del ejecutable.** El log viaja con `--log` y los datos con `--data`, y el temporal solo guarda los binarios que se sueltan. Hubo una versión que dejaba los datos en el temporal, con el argumento de que la interfaz los deduce del marcador y pasárselos sería el fallo silencioso que el marcador existe para impedir. Corrida, esa versión estrenaba `identity.key` en cada arranque, dejaba extracciones sin borrar con la llave dentro, y no conservaba ni la sala ni el servidor propio: quien abría el `.exe` dos veces era dos equipos distintos para quien ya había jugado con él. El marcador sigue contestando lo que sabe, que es qué producto es esto, y de ahí salen el nombre del pipe y los defaults. Dónde escribe el daemon lo contesta el daemon, y **se lo dice a la interfaz al lanzarla**, por la misma vía que el log.
- **El motor se comprueba antes de empaquetar.** El script elige el más reciente de los sitios donde queda compilado y **se planta** si el binario es más viejo que el código del motor, con una verificación cruzada por SHA256 entre el que eligió y el que acabó dentro. No es celo de más: la primera versión apuntaba a una ruta fija con un motor de tres días antes, sin el commit que hace que el host acepte a los invitados a los que él mismo dio credencial. Un motor viejo ahí dentro no se descubre acá, se descubre en la máquina de la otra persona.

### Dónde queda escrito lo que el daemon dice

**Un servicio no tiene salida estándar**, y este binario es gráfico, así que tampoco tiene consola a la que reengancharse. Corriendo como servicio, todo lo que el daemon imprimiera se perdería, y un arranque fallido quedaría como un servicio que se detuvo solo, sin una línea que diga por qué, ni en pantalla ni en disco.

Por eso todo modo que no sea consola manda el log a `logs\kanpachi.log` dentro del directorio de datos, con rotación por tamaño a los 2 MB y una sola copia anterior. Eso cubre al servicio y también al daemon de una carpeta portable, que tampoco tiene consola. En modo consola sigue yendo a la salida estándar, que es donde está mirando quien programa.

La carpeta se puede mover con `--log`, y existe por un caso concreto: el bundle portable corre desde un directorio temporal que borra al salir, y el log no puede morir con él. Con la bandera, la carpeta que se pasa se usa **tal cual**, sin colgarle `logs\` debajo: quien la nombra ya eligió. `--data` es su pareja y hace lo mismo con el directorio de datos, por el mismo motivo.

No va al Visor de eventos, que sería lo idiomático: exigiría registrar una fuente en el instalador, y si esa fuente falta cada línea se convierte en "no se encuentra la descripción del ID de evento". Un archivo de texto al lado de los otros datos lo abre cualquiera, se pega en un reporte de fallo, y ya está protegido por la ACL que el instalador le puso al directorio.

#### La traza de un pánico va al mismo archivo, y antes se perdía entera

El log de arriba solo recoge lo que el daemon decide escribir. **Un pánico de Go no pasa por ahí**: el runtime lo escribe por la salida de errores del proceso, y un binario `-H windowsgui` corriendo como servicio no tiene ninguna. `GetStdHandle(STD_ERROR_HANDLE)` devuelve un handle inválido y la traza se escribe en la nada.

Lo que eso produce es un daemon que **se muere sin dejar una sola línea**. Medido el 2026-08-08: el registro de eventos de Windows anotó dos veces `Event 7031, "The Kanpachi service terminated unexpectedly"`, con el servicio reiniciándose solo a los 5 y a los 10 segundos por sus `FAILURE_ACTIONS`, y `kanpachi.log` no tiene nada entre la última línea normal y el arranque siguiente. Desde fuera se ve como que la interfaz "perdió el servicio" y como que "la ventana se cerró sola" — las dos cosas son el job del daemon llevándose a sus hijos, no causas.

El arranque en modo servicio apunta ahora la salida de errores del proceso a ese mismo archivo, con `SetStdHandle` más `os.Stderr`. Las dos mitades hacen falta: la primera es la que lee el runtime para un pánico, la segunda es a donde escribe el código normal, incluido el `cmd.Stderr` del motor, que hasta ahora también se tiraba.

**Que `SetStdHandle` alcance hubo que medirlo, no suponerlo.** Si el runtime cacheara el handle al arrancar, cambiarlo después no serviría de nada y haría falta otro mecanismo entero. Se comprobó con un binario `-H windowsgui` que hace `SetStdHandle` a un archivo y entra en pánico: el archivo queda con `panic:` y su goroutine, y el proceso sale con código 2. La captura se rehace en cada rotación, porque rotar cierra el archivo y abre otro; hacerla una sola vez al arrancar dejaría el handle apuntando a uno cerrado, y el pánico que importa —el de un daemon que lleva días arriba— se perdería igual.

#### El motor escribe el suyo, y su consola va apagada a propósito

`kanpachi-engine.log`, en la misma carpeta, escrito por el motor. Lo enciende `log_dir` en la parte común de cada orden de arranque, que es el mismo directorio que el daemon usa para el suyo, por el mismo motivo que existe `--log`: en el bundle el motor vive en un temporal que se borra al salir.

**No se escribió un subscriber nuevo.** EasyTier ya trae el suyo, en `easytier/src/common/log.rs`, con filtros por nivel y capa de archivo, y el `TomlConfigLoader` ya tiene `console_logger` y `file_logger`. Se configura, no se reimplementa.

Tres cosas que hay que saber, y ninguna es obvia:

- **El nivel de consola va en `off`, escrito explícito.** El logger de consola de EasyTier manda `WARN` y peor a la salida de errores, y todo lo demás —`INFO`, `DEBUG`, `TRACE`— a la **salida estándar**. Y la salida estándar del motor es el protocolo, una línea JSON por mensaje. Encenderlo con el default corrompe el canal de órdenes. Va escrito en falso aunque el default ya lo esté, que es la regla que el propio motor declara para toda capacidad prohibida.
- **Se inicializa una vez por PROCESO, no por instancia.** `init` usa `try_init`, y el host levanta dos redes, la sala y el vestíbulo. Llamarlo por instancia falla en la segunda.
- **Son dos archivos y no uno.** El motor es otro proceso, con su propia rotación. Juntarlos exigiría que le mandara sus líneas al daemon por el mismo canal por el que recibe órdenes, y ese canal es lo primero que se cae cuando el motor está en problemas.

**Rota a los 8 MB y guarda dos copias, más que el del daemon**, porque se llena mucho más rápido: EasyTier anota una línea INFO por cada paquete de multidifusión que no sabe encaminar, y una máquina con Windows emite SSDP y mDNS todo el tiempo. Medido el 2026-08-09: 266 KB en quince minutos, cerca de un megabyte por hora. A 2 MB daba la vuelta a mitad de la sesión que alguien está tratando de explicar.

**Ese ruido NO se filtra**, y hay dos motivos. EasyTier parsea el nivel con `unwrap()` sobre un `LevelFilter`, así que solo admite un nivel pelado y una directiva de `EnvFilter` como `easytier::peers::peer_manager=warn` haría entrar en pánico al motor en vez de acotar nada. Y callar ese módulo entero se llevaría por delante las líneas de encaminamiento de peers, que son justo con lo que se diagnostica un host que no ve a su invitado.

#### La interfaz también, y ahí ninguna vía del sistema servía

`kanpachi-ui.log`, otra vez en la misma carpeta, escrito desde Dart. La interfaz se moría sola y no dejaba nada: medido el 2026-08-09 con el bundle corriendo doce horas, **dieciocho muertes**, una cada veinte a noventa minutos, y de cada una solo quedaba la línea del daemon diciendo que la relanzaba.

Un binario de Flutter para Windows no tiene dónde contarlo, y las dos vías obvias están cerradas, las dos comprobadas:

- **Heredar la salida de errores desde el daemon no se puede.** El daemon vive en la sesión 0 y la interfaz en la del usuario, y los handles no se heredan entre sesiones. Es el `false` de `CreateProcessAsUser`.
- **Redirigir los streams del runner de C++ tampoco.** `flutter_windows.dll` enlaza su propio CRT, así que un `freopen_s` en el runner no toca los del motor de Flutter. Para eso existe `FlutterDesktopResyncOutputStreams`, y esa API está cableada a `CONOUT$`: llamarla después de redirigir a un archivo deshace la redirección.

Así que se escribe desde Dart, con `runZonedGuarded` para lo asíncrono, `FlutterError.onError` para los errores del framework y `PlatformDispatcher.instance.onError` para lo que se le escapa a la zona. Se anotan **también el arranque y el cierre limpio**: un registro que solo tiene errores no distingue "se cerró sola" de "no llegó a arrancar", que era justo la pregunta.

**La carpeta se la dice el daemon, con `--log`, y eso es una excepción a la doctrina del marcador.** El marcador contesta "qué producto soy", y los dos lados lo deducen del disco. Esta es otra pregunta: "desde qué carpeta me abrieron" NO es deducible por la interfaz, porque en el bundle su ejecutable está en el temporal. La sabe solo el bundle, y ya se la pasa al daemon por esta misma vía. Sin bandera se cae al directorio de datos, y si ahí no se puede escribir, a `%LOCALAPPDATA%\Kanpachi`: el daemon es SYSTEM y la interfaz no, y el permiso que `ProgramData` hereda a sus subcarpetas deja a los usuarios crear carpetas y no crear archivos.

**Lo que este archivo NO ve:** un fallo nativo, del motor de Flutter, de un plugin o del driver de vídeo, no pasa por Dart. Esa mitad la cubre el código de salida que el daemon anota al ver morir el proceso, que separa una salida limpia de un `0xC0000005`.

Y las dos mitades juntas ya contestaron la primera pregunta. Medido el 2026-08-09 con el bundle: **la interfaz muere con `0xC0000005`**, o sea violación de acceso, o sea nativo. El registro de Dart no va a tener nunca la causa, y saberlo vale porque descarta media búsqueda.

**Todo error que se complete a mano lleva su traza.** `completeError` sin ella deja `StackTrace.empty`, que se anota como una traza vacía y no como una traza ausente: se sabe qué falló y no quién lo estaba esperando. Las dos caídas de ese día dejaron exactamente eso, y el camino no se pudo reconstruir leyendo el código. Ahora la toman el sitio que rompe las esperas del cliente y los dos del transporte que rompen una escritura.

### El latido: la interfaz pregunta sin parar

**Esto revierte una decisión escrita**, y conviene leer por qué antes que el cómo. Decía que no había temporizador en ninguna capa: se refrescaba al entrar a una pantalla y cuando el usuario lo pedía, y la consecuencia aceptada era que lo que se ve pudiera estar viejo entre un refresco y el siguiente.

La consecuencia real era mucho peor que "estar viejo", y no en un caso raro:

- **La app se bloqueaba sola.** Crear una sala, entrar a Configuración y volver dejaba la interfaz sin sala, con el daemon dentro de una. Crear otra contestaba `busy`, y no había nada que pudiera desbloquearlo salvo cerrar la ventana.
- Lo mismo para un invitado que saliera de la pantalla de sala.
- Nada de lo que ocurriera del lado del daemon llegaba nunca: alguien entrando, alguien yéndose, el túnel degradándose, el host cerrando la sala.
- "Servicio activo" era una foto del arranque. Con el daemon caído y vuelto a levantar había que **cerrar y abrir la ventana** para que la app se enterara.

El error de fondo era tratar la navegación como la fuente de los refrescos. El estado del daemon cambia sin que nadie entre a ninguna pantalla, así que refrescar al entrar deja fuera justo los casos que importan.

Así que la interfaz late: cada dos segundos pregunta por la sala y por la salud. No cambia nada del contrato —el daemon sigue siendo petición y respuesta puro, sin empuje— y **se puede preguntar tan seguido porque `Status` no toma el candado de la sesión**: lee la copia publicada, que existe exactamente para esto. Por eso el latido sigue contestando mientras una creación de sala tiene la sesión tomada durante un minuto.

Dos guardas que hacen que no estorbe:

- **Se calla mientras se crea o se entra.** Ahí el estado lo manda la operación en curso, y un latido llegado en medio pintaría la sala a medio abrir o la borraría porque todavía no existe.
- **El catálogo no entra en el latido.** Se pide una vez cuando vuelve el servicio: es una lista que no cambia sola, y pedirla cada dos segundos sería releer el disco del daemon para nada.

Del lado de la pantalla, la navegación pasa a SEGUIR a la sala en vez de suponerla. Ver `docs/05`.

### Cancelar una operación larga

Crear una sala y entrar a una tardan decenas de segundos con todo funcionando, y hay motivos legítimos para no querer esperar: el código estaba mal pegado, el host todavía no abrió, o simplemente se arrepintió. Sin una forma de cortar, la única salida es esperar al final o matar el proceso, y matar el proceso a mitad de una creación es justo el momento en el que hay una red virtual arriba y reglas escritas.

**Se pide por una conexión APARTE, igual que el progreso, y por el mismo motivo:** el bucle de una conexión es secuencial, así que mandarlo por la que está esperando lo pondría en cola detrás de justo lo que viene a cortar.

Del lado del caso de uso son tres piezas:

- La operación envuelve su contexto y deja anotado el cancelador **bajo un candado propio**. El de la sesión lo tiene tomado ella misma durante todo el minuto que dura, que es exactamente el rato en el que alguien pulsa Cancelar; un cancelador que lo pidiera esperaría a que terminara lo que viene a cortar. Mismo argumento que el diario de progreso.
- Cancelar el contexto hace que la operación falle en la primera llamada que lo mire, y de ahí en adelante corre **el mismo camino de limpieza que un fallo cualquiera**. No hay un deshacer aparte que mantener.
- Esa limpieza recibe un contexto **propio y vivo**, desprendido del que se acaba de cancelar. Sin eso, cada paso del `teardown` devolvería "context canceled" sin llegar a tocar Windows, o sea que cancelar dejaría abierto exactamente lo que el botón promete cerrar. Lleva su propio plazo para que un adaptador colgado no deje el candado de la sesión tomado para siempre.

**No es instantáneo, y se dice.** Medido: cancelada a los 6,5 segundos con un adaptador ya levantado, el daemon contestó al cancelador en 3,2 s y la creación devolvió `canceled` a los 21,1 s, porque una espera dentro de una llamada de Windows termina antes de mirar el contexto. Lo que importa es lo de después: cero adaptadores, cero reglas del grupo `Kanpachi`, y la sesión en `idle`. La pantalla no espera esos veintiún segundos, ver `docs/05`.

### Preguntar si el código existe, antes de levantar nada

Entrar a una sala con un código inventado o vencido se descubría al final: se levantaba el motor, se entraba a un vestíbulo donde no espera nadie, y se agotaban los reintentos del canal de control. Alrededor de un minuto de espera para llegar a "no se pudo", cuando la respuesta se sabía en el primer segundo, y con una red virtual arriba durante todo ese rato por una sala que no existe.

Así que `JoinRoom` le pregunta primero al registro. La forma exacta de esa pregunta es lo que la hace segura:

- **Sin respuesta del registro no se entra**, y las dos formas de parar dicen cosas distintas. Que el registro **afirme** que ese invite ID no existe viaja como centinela propio, `port.ErrUnknownRoom`, y llega al usuario como `no_such_room`: el código está mal o venció, y reintentar no cambia nada. Que el registro **no conteste** llega como `no_registry`: el código puede estar perfecto, y lo que corresponde es volver a intentarlo en un rato.
- **Se comprueba antes que el seed sea el mismo.** Un invite ID solo significa algo en el registro que lo emitió: preguntarle a este por un código servido por otro devolvería "no existe" sobre una sala que existe perfectamente. Cuando no coinciden, la comprobación se salta y se entra como siempre.

Medido: un código inexistente falla en **0,8 s** con el código `no_such_room`, sin arrancar el motor y sin escribir una sola regla.

**Parar cuando no contesta es un cambio del 2026-08-12, y este documento decía lo contrario.** La versión anterior era: *"Solo un 'no existe' detiene el ingreso. El registro es solo presentación y que no conteste jamás puede impedir entrar a una sala, porque la sala vive en las máquinas de sus miembros"*. La segunda mitad sigue siendo cierta y la conclusión era falsa: **la máquina del registro es también el punto de encuentro**, y viaja como par en la configuración del motor. Sin ella el vestíbulo no se forma, así que seguir a ciegas costaba alrededor de un minuto de ruedita con una red virtual arriba, para terminar en un fallo genérico sobre una sala a la que nunca se iba a llegar.

Es la política del producto, dicha por su dueño: mejor fallar rápido que dejar que alguien se entere de que no va a funcionar después de mucho rato. La misma razón por la que el adaptador virtual se sondea al arrancar el daemon en vez de descubrirse al abrir la primera sala.

**Lo que esto exigió primero, y por eso no se pudo hacer antes:** que el "no" del registro sea confiable. Con el almacén en memoria y `Restart=always`, un reinicio le hacía contestar que no conoce salas abiertas y alcanzables. Endurecer esta comprobación encima de eso convertía una molestia en un muro, así que el registro persiste primero.

### Sin registro no hay sala que abrir

La otra mitad de lo mismo, y la que escondía un fallo más caro. `CreateRoom` pide un invite ID al registro, y cuando no contestaba **se generaba uno acá**, con el argumento de que crear una sala no debía depender de que un servidor estuviera vivo y de que lo único perdido era la página de invitación.

Se siguió la cadena entera el 2026-08-12 y ese código no le sirve a nadie:

1. Al no haber respuesta no hay forma de saber a qué registro pertenecería, así que el ID se emitía con el seed que estuviera compilado, que en aquel momento era uno de fábrica.
2. Ese seed es **el mismo** que consulta el invitado, así que no se salta la comprobación de arriba: pregunta, le contestan que esa sala no existe, y rechaza el ingreso antes de arrancar el motor.
3. El host se quedaba con un código de aspecto normal, lo repartía, y no entraba nadie. Sin ningún error en su pantalla.

Así que crear **falla**, y falla antes de levantar el motor y antes de escribir una sola regla, con un mensaje que dice qué pasó y qué hacer. Los códigos los emite el registro, y uno inventado en la máquina del host no lo reconoce nadie.

### El registro se comprueba antes de que nadie invierta nada

Las dos cosas de arriba se descubren justo después de que alguien eligió un juego y escribió ocho caracteres. Eso se puede saber antes, con la pantalla todavía vacía, y es el mismo argumento que puso el sondeo del adaptador virtual en el arranque del daemon.

`RoomDirectory.Reachable` pide `GET /healthz` y nada más. **No lleva invite ID a propósito:** preguntar por uno inventado ensuciaría el registro con consultas de salas que no existen, gastaría límite de tasa y mediría otra cosa. Lo sondea el barrido de alertas, que es el que ya sale a la red, y el resultado viaja como `seed_down` hasta el aviso de la portada.

Va en el barrido **antes** del corte por vencimientos, y esa colocación importa: lo demás que se calcula ahí describe una sala, esto describe la máquina y vale igual sin ninguna sala abierta, que es justo cuando el aviso sirve.

**No apaga Kanpachi, a diferencia del preflight del adaptador.** Son dos fallos con dos formas distintas: un almacén de drivers roto necesita que alguien toque la máquina, y un registro caído se arregla solo cuando vuelve. Cerrar la app por algo que se cura sin intervención sería quitarle a la persona la ventana donde va a ver que ya se curó.

### Salir de Kanpachi

"Salir de Kanpachi" en el menú de la bandeja **no cierra la UI**. Manda la orden al daemon, que es el único que puede cerrar bien: sale de la sala, purga las reglas, baja el motor y el adaptador, y al final se detiene él. La UI muere de camino, con el `job`.

La UI no coordina el apagado porque no controla nada de lo que hay que apagar.

## Arquitectura interna: regla de dependencia

El proyecto sigue Clean Architecture aplicada como **regla de dependencia con puertos**, no como anillos de carpetas con DTOs mapeando entre capas. Para un proyecto de este tamaño el mapeo entre capas es ceremonia sin retorno.

**La métrica que decide si está bien no es la pureza de capas, es esta: los tests corren sin admin, sin red y sin Windows.**

| Anillo | Qué vive aquí | En Kanpachi |
|---|---|---|
| **Dominio** | Tipos y reglas puras | `Code`, `Room`, `GameProfile`, `RuleSet`, `Peer`, invariantes del catálogo, derivación del código, plan de direcciones |
| **Casos de uso** | Orquestación, uno por intención | `CreateRoom`, `JoinRoom`, `ActivateProfile`, `LeaveRoom`, `CreateGameProfile`, `ImportCatalog` |
| **Puertos** | Interfaces que el dominio necesita | `EnginePort`, `FirewallPort`, `NetConfigPort`, `CatalogStore`, `StateStore`, `SystemEvents`, `GameLibrary`, `SocketInspector`, `RoutingTable`, `ControlChannel` |
| **Adaptadores** | Implementaciones sucias | EasyTier, COM del Firewall, `iphlpapi`, registro de Windows, Steam, JSON en disco |
| **Entrada** | Cómo se pide algo | El named pipe, el manejador `kanpachi://`, el arranque del servicio |

Los puertos **se declaran en `core` y se implementan fuera**. Esa es la inversión, y es lo que hace que el dominio no sepa que existe Windows.

### Los puertos

```go
package port

type EnginePort interface {
    // HostNetwork arranca la red como nodo admin: es el único que conoce el
    // secreto de red y el único que puede emitir credenciales.
    HostNetwork(ctx context.Context, spec domain.HostSpec) error
    // JoinRendezvous entra al VESTÍBULO, sin credencial. Es el paso 4 del
    // flujo de conexión, y va aparte porque son dos redes con dos modelos de
    // confianza distintos. El host también lo llama: el vestíbulo es su puerta
    // Llamarlo otra vez REEMPLAZA el vestíbulo anterior, y de eso depende
    // renovar el código: el nombre del vestíbulo deriva del invite ID
    JoinRendezvous(ctx context.Context, spec domain.RendezvousSpec) error
    // LeaveRendezvous sale SOLO del vestíbulo. Aparte de Leave porque el host
    // está en dos redes a la vez y un único "salir" sería ambiguo justo donde
    // no puede serlo: al invitado le toca dejar el vestíbulo y quedarse
    LeaveRendezvous(ctx context.Context) error
    // JoinWithCredential entra a la red REAL como nodo temporal. Nunca recibe
    // el secreto
    JoinWithCredential(ctx context.Context, spec domain.GuestSpec) error
    Leave(ctx context.Context) error  // de todo. Idempotente

    // Solo tienen sentido en un nodo admin. Ver decisiones 2 y 22.
    IssueCredential(ctx context.Context, req domain.CredentialRequest) (domain.Credential, error)
    RevokeCredential(ctx context.Context, id domain.CredentialID) error
    ListCredentials(ctx context.Context) ([]domain.Credential, error)

    // Restart vuelve a levantar el motor con la ÚLTIMA especificación con la
    // que se arrancó. Es el MECANISMO del watchdog; cuántas veces y cada
    // cuánto es política y vive en el supervisor. La especificación no vuelve
    // a core, y por eso existe este método en vez de repetir HostNetwork: el
    // secreto de la red real se queda dentro del adaptador
    Restart(ctx context.Context) error

    Peers(ctx context.Context) ([]domain.Peer, error)
    // Events devuelve el canal del proceso ACTUAL. Tras un Restart hay uno
    // nuevo y el anterior se cierra, así que quien escuche vuelve a pedirlo
    Events() <-chan domain.EngineEvent
    Diagnostics(ctx context.Context) (domain.NetCheck, error)
}

type FirewallPort interface {
    // Apply calcula la diferencia contra las reglas VIVAS del grupo Kanpachi,
    // enumeradas del sistema en CADA llamada, jamás contra una copia en
    // memoria de lo último que se pidió. De eso dependen dos cosas: que sea
    // idempotente, y que reaplicar el mismo conjunto REPARE lo que alguien
    // haya borrado o agregado por fuera
    Apply(ctx context.Context, desired domain.RuleSet) error
    // ApplyBaseQuarantine escribe la cuarentena de base y REPONE lo que falte.
    // Solo AGREGA, y no existe el método para borrarla: esa ausencia es la
    // protección, porque lo que hace valiosa a la cuarentena es seguir puesta
    // con el daemon detenido. La llama NewSession al arrancar, ANTES de
    // PurgeOwned. Ver decisión 4
    ApplyBaseQuarantine(ctx context.Context, rules []domain.QuarantineRule) error
    // PurgeOwned borra todo lo del grupo "Kanpachi", por igualdad exacta y
    // jamás por prefijo. NUNCA toca "Kanpachi-base": esa es la cuarentena de
    // base, y es lo que protege la máquina cuando el daemon no corre.
    // Ver decisión 4
    PurgeOwned(ctx context.Context) error
    AuditForeign(ctx context.Context, p domain.GameProfile) ([]domain.ForeignRule, error)
    SuspendForeign(ctx context.Context, r []domain.ForeignRule) error
    RestoreForeign(ctx context.Context) error
    // Las tres del firewall AJENO que bloquea la entrada (decisión 36).
    // InboundBlocked nunca inventa una lista vacía ante un fallo de lectura;
    // AllowAdapters ejecuta solo los comandos ya enseñados y anota cada
    // apertura en un libro; WithdrawAdapters deshace exactamente lo del libro,
    // al salir de la sala y al arrancar el servicio. En Windows contestan
    // vacío hasta que ese caso se mida
    InboundBlocked(ctx context.Context) ([]domain.FirewallBlock, error)
    AllowAdapters(ctx context.Context, blocks []domain.FirewallBlock) error
    WithdrawAdapters(ctx context.Context) error
}

type NetConfigPort interface {
    ApplyAdapter(ctx context.Context, want domain.AdapterState) error
    RevertTweaks(ctx context.Context) error
    ProbeMTU(ctx context.Context) (int, error)
    // SetDirectPlay va aparte porque no es un ajuste del adaptador, es una
    // característica opcional de Windows. Meterla en el estado declarativo
    // haría que cada evento de identificación de red, que ocurre varias veces
    // por sesión, tocara la instalación de características del sistema
    SetDirectPlay(ctx context.Context, want bool) error
}

type RoutingTable interface {
    LocalPrefixes(ctx context.Context) ([]netip.Prefix, error)
}

// CatalogStore devuelve BYTES y no perfiles. Quien valida es el dominio: un
// adaptador que decidiera qué es un perfil válido movería la política fuera de
// core, que es exactamente lo que las ocho invariantes del catálogo impiden.
type CatalogStore interface {
    LoadBuiltin() ([]byte, error)
    LoadLocal() ([]byte, error)
    SaveLocal([]byte) error
}

// StateStore es lo que sobrevive a un arranque, y devuelve bytes por lo mismo
// que CatalogStore: el decodificador estricto vive en el dominio.
//
//   hosted-room.json  SOLO EN EL HOST. Dice que hay una sala que reponer, y
//                     nada más: apagarse limpio lo conserva, y lo único que lo
//                     borra es cerrar la sala
//   last-room.json    SOLO EN INVITADOS. Código, seed, nombre y nick. Jamás la
//                     credencial ni la identidad de la red real
type StateStore interface {
    LoadRoom() ([]byte, error)
    SaveRoom([]byte) error
    ClearRoom() error
    LoadLast() ([]byte, error)
    SaveLast([]byte) error
    ClearLast() error
}

// SystemEvents son las cosas que le pasan a la MÁQUINA y que invalidan lo que
// Kanpachi dejó puesto. Tres canales y no un enum, a diferencia de
// EngineEvent: aquel viene de una sola fuente, el proceso hijo, y estos vienen
// de tres subsistemas de Windows que no se conocen entre sí. Un canal por
// fuente hace que una suscripción muerta se VEA, porque su canal se cierra y
// los otros dos siguen.
type SystemEvents interface {
    NetworkIdentified() <-chan struct{} // Event ID 10000
    Resumed() <-chan struct{}           // suspensión, hibernación, Fast Startup
    NetworkChanged() <-chan struct{}    // WiFi a cable, cable a LTE
    Close() error                       // idempotente, y no espera lector
}

type GameLibrary interface {
    Installed(ctx context.Context) ([]domain.GameRef, error)
}

type SocketInspector interface {
    Snapshot(ctx context.Context, root domain.ProcessRef) ([]domain.Listener, error)
}

// ExposureAudit alimenta el módulo de alertas de la decisión 19. Cada método
// responde una pregunta que Kanpachi no controla y que anula su promesa si
// nadie la mira. Ninguno bloquea: devuelven hallazgos, jamás errores fatales.
//
// Que un método falle NO se calla: todos menos el del router levantan
// AlertAuditFailed, porque "no se pudo comprobar" y "todo en orden" se ven
// igual desde la pantalla. La excepción del router es deliberada: la mayoría
// de los routers nunca contestan al IGD
type ExposureAudit interface {
    FirewallEnabled(ctx context.Context) ([]domain.FirewallProfileState, error)
    // Mide lo puesto en las dos capas; el veredicto lo da
    // domain.Enforcement.Diff, que es dominio y corre sin Windows
    Enforcement(ctx context.Context) (domain.Enforcement, error)
    RouterMappings(ctx context.Context) ([]domain.PortMapping, error) // SOLO LECTURA
    // Mide la cuarentena de base tal como está en el sistema, jamás como se
    // aplicó: una regla borrada, deshabilitada o editada tiene que verse, y un
    // recuerdo de lo escrito no puede verla. El cero del estado es "no se pudo
    // comprobar", que nunca se pinta igual que "ausente"
    QuarantineState(ctx context.Context) (domain.QuarantineState, error)
}

// ControlChannel es el canal de la sala de la decisión 23. Serve lo llama SOLO
// el host, y en DOS direcciones con dos alcances: ver más abajo.
type ControlChannel interface {
    Serve(ctx context.Context, scope domain.ControlScope) error
    // Dial reemplaza la conexión anterior: el invitado marca primero al
    // vestíbulo para pedir la credencial y después al host en la sala, que es
    // la que tiene que quedar viva
    Dial(ctx context.Context, host netip.Addr) error
    HostPresence() <-chan bool
    // Announce lo llama SOLO el host, por la dirección de la sala. Es cómo se
    // enteran los invitados del nombre y del juego activo
    Announce(ctx context.Context, a domain.RoomAnnounce) error
    // Announcements es el lado del invitado. El adaptador solo emite lo que
    // llegó por la conexión al host: un miembro no puede anunciar nada
    Announcements() <-chan domain.RoomAnnounce

    // Notify le manda un aviso a un miembro, o a todos con una dirección en
    // cero. SOLO el host. Se manda ANTES de cortarle nada al expulsado, que es
    // el único orden en que sirve
    Notify(ctx context.Context, to netip.Addr, n domain.RoomNotice) error
    Notices() <-chan domain.RoomNotice

    // AnnounceCode reparte el invite ID NUEVO a los presentes tras renovarlo.
    // El adaptador lo SELLA contra la llave pública de cada miembro, la misma
    // que llegó en su pedido de credencial. Core no ve llaves
    AnnounceCode(ctx context.Context, r domain.Room) error
    Codes() <-chan domain.Room

    // El adaptador pone la llave pública EFÍMERA de esta sesión, generada al
    // marcar y descartada al salir. Core no la ve. La llave larga de la
    // la libreta de la decisión 25 vive en known-hosts.json
    RequestCredential(ctx context.Context, req domain.CredentialRequest) (domain.Credential, error)
    // Close es idempotente, y NUNCA espera a que alguien lea sus canales. No
    // es higiene: el caso de uso lo llama con el candado de la sesión tomado, y
    // un Close que esperara a su goroutine emisora mientras esa está bloqueada
    // escribiendo en HostPresence dejaría al daemon colgado con la sala a
    // medio cerrar.
    //
    // Cierra el oyente y el marcador, y DEJA VIVOS los cuatro canales: la
    // sesión llama a Close en cada salida de sala y vuelve a marcar al entrar
    // a la siguiente, y un canal cerrado es lo que el supervisor trata como
    // suscripción muerta
    Close() error
}

// Clock existe porque hay backoff y vencimientos que testear, y esperar veinte
// minutos en un test no es una opción. Es la única interfaz de comodidad del
// proyecto: un StringFormatter no existiría.
type Clock interface{ Now() time.Time }

type Logger interface {
    Info(msg string, kv ...any)
    Warn(msg string, kv ...any)
    Error(msg string, kv ...any)
}
```

`RouterMappings` es la excepción de solo lectura a "el router no se toca nunca". El puerto **no declara** una operación de crear ni de borrar mapeos, y esa ausencia es deliberada: lo que no existe en la interfaz no se puede llamar por error.

La misma idea gobierna el resto: no hay método para abrir un puerto suelto, no hay método para observar procesos de fondo, y `CatalogStore` no puede decidir qué perfil es válido.

### Lo que el host les cuenta a los presentes

```go
type RoomAnnounce struct {
    RoomName string
    GameID   string
}
```

Existe porque hay dos cosas que solo el host sabe y que el invitado necesita: cómo se llama la sala, que viaja cifrada en la tarjeta y no llega por la red, y cuál es el juego activo, que decide qué abre cada uno en un perfil de malla. Sin esto, la pantalla en sala de un invitado no tiene juego que mostrar, no tiene guía de conexión, y `client_ports` es código que nunca corre.

**Lleva el id del juego y jamás el perfil,** y esa es la diferencia entre que el host diga "estamos jugando Zomboid" y que el host diga "abrí estos puertos". El invitado resuelve el id contra SU catálogo, con SUS invariantes, y si no lo tiene no abre nada. Es la misma regla que gobierna el named pipe, aplicada al otro canal por el que entra una orden de fuera.

Un host no toma anuncios. Aceptarlos le permitiría a un miembro modificado cambiarle el juego activo justo a la máquina donde se abren los puertos.

### Estructura de carpetas

```
core/
  domain/      tipos y reglas puras. Cero imports de os, syscall, x/sys
  port/        las interfaces de arriba
  usecase/     un archivo por intención, recibe puertos por constructor

daemon/
  adapter/
    engine/kanpachi/
    firewall/windows/netfw/
    netcfg/windows/
    catalog/jsonfile/
    library/steam/
    inspect/iphlpapi/
    audit/windows/      ExposureAudit: firewall, reglas propias, IGD del router
  transport/
    wire/               enmarcado de mensajes por líneas, con tope. Una copia
                        sola, la usan el pipe y el canal de la sala
    protocol/           la API local: mensajes y códigos, sin transporte
    control/            el canal de la sala: dos oyentes, dos alcances
    pipe/               el named pipe y su token
  adapter/sinimplementar/  los puertos sin adaptador todavía. Fallan en TODO
  service/            el ORDEN de arranque y apagado, y el supervisor. Recibe
                      puertos ya construidos. Go PURO, job de Linux
  cmd/kanpachid/      el binario: main, host del servicio, --console, y la
                      ELECCIÓN de los adaptadores concretos. Solo Windows

internal/
  pipeprobe/          la sonda con la que se prueba el pipe a mano. En
                      internal/ para que el producto no la importe y el
                      instalador no la distribuya
```

**Los provisionales fallan en todo, y eso no es pereza.** Uno que devuelve éxito hace la cuarentena inverificable desde dentro del programa: un firewall que dice "purgado" sin purgar deja la pantalla en verde, al módulo de exposición sin encontrar nada y al producto entero afirmando una promesa que nadie cumplió. El daño no es que falten funciones, es que la mentira sea indistinguible de la verdad.

`RestoreForeign` es el que más invita a la excepción, porque el arranque lo llama por si una salida sucia dejó algo desactivado. Falla igual: devolver `nil` haría que el daemon anotara "reglas ajenas restauradas" sin restaurar nada, y el usuario se quedaría con reglas de su juego apagadas creyendo que volvieron. La única excepción es `SystemEvents`, forzada por su firma, ya que sus tres métodos devuelven canales y no hay dónde poner un error; sus canales mudos dicen la verdad, que no hay eventos, y el supervisor ya reaplica por temporizador porque estas suscripciones no son fiables ni con el adaptador de verdad.

**La etiqueta de compilación va al revés de lo intuitivo:** el build con provisionales es el por defecto y el de release lleva la etiqueta. Con la etiqueta del lado de los provisionales, olvidarla produce un binario de release silencioso que compila, se instala como servicio y no hace nada de lo que promete. Con esta dirección, olvidarla produce uno que se NIEGA a instalarse. El olvido tiene que doler del lado seguro. Y mientras `sinimplementar.Presente` sea cierto, `kanpachid` solo arranca con `--console`.

El cableado vive fuera de los casos de uso y está partido en dos, con una frontera que no es de gusto: `service/` sabe en qué orden pasan las cosas y `cmd/kanpachid/` sabe con qué. Solo el segundo conoce a la vez el dominio y Windows.

**Por qué el `main` no vive en `service/`.** El guardián de pureza parsea con `parser.ImportsOnly` y **no mira las etiquetas de compilación**, así que un `main.go` con `//go:build windows` que importe `golang.org/x/sys/windows/svc` hace fallar el test de pureza desde el job de Linux. Las dos salidas alternativas son peores: sacar `service/` de la lista de puros reduce la cobertura del guardián justo en la capa donde entra el código nuevo, y enseñarle a saltarse archivos con etiqueta abre el agujero exacto que existe para tapar, porque entonces cualquiera pone `//go:build windows` sobre un archivo de `core` y mete lo que quiera.

Mover el `main` no inventa una convención: es la que el repo ya usa para su otro binario, `registry/cmd/kanpseed/main.go`.

### Qué está cableado de verdad hoy

Cinco puertos tienen adaptador real y siete siguen en `sinimplementar`, fallando en todo a propósito. Arranca, purga el firewall de verdad, atiende el pipe, y el motor provisional falla ruidosamente mientras el supervisor reintenta con espera creciente.

| Puerto | Adaptador |
|---|---|
| `FirewallPort` | `adapter/firewall`, las dos capas compuestas |
| `ExposureAudit` | el firewall para dos preguntas, `sinimplementar` para la del router |
| `CatalogStore` | `adapter/catalog/jsonfile` |
| `StateStore` | `adapter/state/jsonfile` |
| `ControlChannel` | `transport/control` |

**La auditoría se compone en el binario y no dentro del firewall.** `ExposureAudit` hace tres preguntas y el firewall solo puede contestar dos: `RouterMappings` le habla al router del usuario por IGD, que es otro protocolo sobre otra red. Contestarla `nil, nil` desde dentro del firewall haría que "no hay mapeos" y "nadie miró" fueran indistinguibles, en la única pantalla cuyo trabajo es distinguir esas dos cosas. Se compone explícita y no por embebido, para que el objeto que solo mide no cargue además con `Apply` y `PurgeOwned`.

**Apagar limpio es SALIR.** `LeaveRoomOnShutdown` cierra los puertos, restaura las reglas ajenas suspendidas, revierte los ajustes, borra `hosted-room.json` y **vuelve a medir**. Devuelve error, a diferencia de salir de la sala, y la razón es que al apagar no hay nadie mirando la pantalla: una alerta añadida a un estado que el proceso va a tirar en un segundo no es un informe. Y borra el archivo porque su ausencia es lo que dice que la salida fue limpia: conservarlo haría que todo apagado se leyera como una muerte sucia, y el aviso de "quedó una sala abierta" dejaría de significar nada por salir siempre.

### La regla verificada por un test

Esto es lo que evita que la arquitectura se degrade en tres meses. Un test que falla si `core` importa algo que no debe:

```go
func TestCoreNoTieneDependenciasSucias(t *testing.T) {
    prohibidos := []string{"os", "syscall", "golang.org/x/sys", "net/http", "os/exec"}
    // recorre core/... y falla si algún import matchea
}
```

Vale más que cualquier documento, porque no se puede ignorar sin querer. Si ese test pasa, `core` corre en Linux, en CI, sin privilegios y con adaptadores falsos.

Existe en `internal/arch/arch_test.go` y lo ejecuta el job `core` del workflow de CI, en Ubuntu. Vive fuera de `core/` a propósito: necesita `os` y `path/filepath` para recorrer el disco, y ponerlo dentro lo obligaría a saltarse a sí mismo con una excepción, que es una regla más débil. Lleva un segundo test que comprueba el detector contra casos conocidos, porque un guardián que nunca se probó no es un guardián.

**La mitad pura de los adaptadores partidos tiene su propio guardián**, y mira por ARCHIVO en vez de por paquete, que es lo que lo distingue del de arriba: ahí el paquete entero sí conoce Windows, y lo que se vigila es que la frontera esté donde dice estar. Un archivo sin sufijo `_windows` es lo que el job de Linux compila y prueba, y lo que se queda sin pruebas si alguien mete Windows dentro son justo las decisiones caras: el alcance de un bloqueo duro en `wfp`, qué dice cada regla en `windows/netfw`, qué rutas se ponen y cuál se borra en `netcfg`, qué prefijos se descartan en `routes`. El fallo sería silencioso, porque el paquete sigue compilando en la máquina donde se programa. La lista de imports prohibidos es más corta que la de `core`: la mitad pura sí puede leer y escribir ficheros, que es lo que hace el libro de ajustes; lo que no puede es hablar con Windows.

#### El guardián del cableado

Vigila el fallo que ocurrió **dos veces**: un método de unión escrito, probado y sin llamar desde producción. `control.Attach` dejaba a `Serve` devolviendo `ErrNotAttached`, así que crear una sala fallaba entera con el motor ya levantado; el alcance de la compuerta hacía que ninguna sala tuviera compuerta. Un test de paquete no lo puede ver, porque el test SÍ los llama; lo que lo ve es correr el producto.

La regla: todo método exportado de `daemon/` cuyo nombre empiece por `Attach`, `Bind`, `Unbind` o `SetScope` tiene que llamarse desde `core/` o `daemon/`, en código que no sea de test. **`internal/` NO cuenta**, y esa exclusión es la mitad importante: `SetScope` sí se llamaba, desde `internal/fwprobe`, y ese era exactamente el estado del fallo. Una herramienta de medición que ejercita un método no prueba que el producto lo use.

La única forma de saltarse la regla se declara en el nombre: un método terminado en `ForMeasurement` queda fuera. Por eso el que usa `fwprobe` se llama `SetScopeForMeasurement`: acota a un adaptador físico elegido a mano, que es como se comprobó con dos máquinas que el bloqueo de WFP le gana a un permiso del Firewall de Windows. La excepción vive en el nombre del método y no en una lista dentro del guardián, que es justo donde una excepción deja de leerse.

### Lo que deliberadamente no se hace

- **DTOs entre capas.** `domain.GameProfile` pasa por el puerto directamente. Un `port.GameProfileDTO` idéntico no compra nada aquí.
- **Repositorio genérico.** `CatalogStore` con cuatro métodos concretos es mejor que un `Repository[T]`.
- **Structs anémicos.** Las invariantes del catálogo son lógica de dominio y viven con el tipo, no en un servicio aparte.
- **Interfaz para todo.** `Clock` sí, porque hay reintentos con backoff que testear. Un `StringFormatter` no.

### La frontera que impone el sistema operativo

`kanpachi-ui` es un proceso Flutter aparte. La frontera entre caso de uso y presentación no depende de disciplina, la impone el sistema: no comparten memoria, y el named pipe es literalmente el adaptador de entrada. La UI no puede saltarse la capa aunque quiera.

## Dominio y casos de uso

Los tipos y reglas puras. **Sin I/O, sin sockets, sin API de Windows.** El 100% de sus tests corre en CI sin permisos especiales, en Linux, con adaptadores falsos.

Los tipos que cruzan los puertos viven aquí:

```go
package domain

type Peer struct {
    VirtualIP netip.Addr
    Name      Nickname
    Path      PathKind      // Direct | Relay | Self
    RTT       time.Duration

    // Self y Host los decide el daemon, jamás el motor. El motor no sabe cuál
    // es "yo" y NO PUEDE saber quién hospeda, que es un concepto del producto
    // y no de la red. Creérselo a un peer sería dejar que cualquiera se
    // declare host en la lista de los demás
    Self bool
    Host bool
}

type NetCheck struct {
    NATKind    string                    // cone, symmetric, cgnat...
    UDPBlocked bool
    SeedRTT    map[string]time.Duration

    MTU          int           // sondeado por netcfg, no por el motor
    Subnet       netip.Prefix  // el /24 elegido
    SubnetReason string        // y por qué, para que un conflicto se lea en un renglón
}
```

`NetCheck` no es adorno: convierte "no conecta" en "tu router hace NAT simétrico, vas por relay". Lo produce `EnginePort.Diagnostics`.

### La credencial, y lo que NO lleva

```go
type Credential struct {
    ID          CredentialID
    Token       string       // lo que el motor recibe. Opaco, no se recalcula
    NetworkName string       // el NOMBRE de la red real. Solo el nombre
    Name        Nickname
    VirtualIP   netip.Addr
    Subnet      netip.Prefix
    IssuedAt    time.Time
    ExpiresAt   time.Time
}
```

**No hay campo para el secreto de la red, y esa ausencia es la decisión 2 entera.** Se verificó contra los binarios de la v2.6.4: el handshake de un nodo temporal lleva `secret_digest` en ceros y `client_secret_proof: None`. Ese hecho es lo que hace que revocar sirva de verdad, porque quien entró nunca tuvo con qué volver por su cuenta. Una credencial con un campo de secreto lo tiraría todo abajo.

**Se emite una por MIEMBRO, no una por código.** El código y las credenciales son los dos controles independientes del host, y la separación es lo que permite renovar sin echar a nadie y expulsar sin renovar.

La subred y la IP virtual viajan dentro: el invitado no las sabe antes, porque las elige el host.

### Identidad

Son tres objetos distintos, ver decisión 2. Mezclarlos produce conclusiones de seguridad falsas.

```
invite ID: 8 caracteres, 2 grupos de 4  (A7K2-M9QX)
alfabeto: 32 símbolos exactos. Los 36 alfanuméricos menos 0, O, 1, I
          (se conserva la L: en mayúsculas no se confunde con el 1)
entropía: 8 × 5 bits = 40 bits

  identidad de ENCUENTRO, derivada, desechable, vestíbulo público:
    networkID = Argon2id(normalizar(inviteID), salt="kanpachi/v1/id")[0:16]
    secret    = Argon2id(normalizar(inviteID), salt="kanpachi/v1/secret")[0:32]

  red REAL de la sala, generada por el host, nunca derivada de un string:
    networkID = 16 bytes aleatorios
    secret    = 32 bytes aleatorios, jamás salen del host
```

`normalizar` quita guiones y espacios y pasa a mayúsculas: pegar el ID en cualquier formato funciona. Los salts llevan versión: un esquema `v2` futuro convive con `v1` sin romper clientes viejos.

**La derivación de encuentro corre en el cliente y no se le pregunta al seed.** El seed podría decirla, derivarla localmente hace que llegar al vestíbulo no dependa de que su API esté viva ni de que diga la verdad.

### Formatos aceptados

Un solo campo en la UI, parser tolerante. Todas estas formas resuelven al mismo invite ID:

| Entrada | Seed que usa |
|---|---|
| `A7K2M9QX` | el por defecto |
| `a7k2-m9qx` | el por defecto |
| `A7K2M9QX@seed.midominio.com` | `seed.midominio.com` |
| `kanpachi.accentio.dev/A7K2M9QX` | `kanpachi.accentio.dev` |
| `https://kanpachi.accentio.dev/A7K2M9QX` | idem |
| `kanpachi://A7K2M9QX` | el por defecto |

La app **genera** el formato URL, que es el más autoexplicativo y sirve de landing de descarga para quien no tenga Kanpachi. La app **acepta** cualquiera de los seis. El usuario nunca tiene que saber cuál es el correcto. En el esquema propio también acepta la barra final que Chromium/Windows agrega al canonicalizar una autoridad sin ruta: `kanpachi://A7K2M9QX/` es el mismo código, no una séptima forma ni una ruta libre.

Un fragmento después del ID (`/A7K2M9QX#clave`) es enriquecimiento opcional: lleva la clave de la tarjeta de sala. La app lo ignora, no le sirve para nada; el nombre de la sala lo recibe por el canal de control.

**Un invite ID es local al seed que lo emitió.** El mismo ID en dos seeds son dos salas que no se conocen. **Un ID pelado se rechaza**, con un centinela propio, `ErrSeedMissing`, que es lo que permite enseñar la forma completa en vez de decir que no se entiende. Ver la decisión 16.

### Por qué 8 caracteres alcanzan

El diseño anterior exigía 60 bits con este argumento: sin backend que valide, un atacante enumera `networkID` contra un seed público hasta hallar salas vivas, y la única defensa posible es la entropía. Las dos premisas cambiaron con las decisiones 2 y 24.

Hay un registro que responde las consultas, o sea que la enumeración se corta con límite de tasa como cualquier otra. Y acertar un invite ID ya no da entrada: da la tarjeta y el derecho a tocar la puerta, porque entrar exige una credencial que emite el host. 40 bits con límite de tasa y un premio acotado es un intercambio distinto que 40 bits sin nada y entrada perpetua.

A cambio se gana lo que el producto necesita: un ID que una persona dicta por teléfono sin equivocarse.

### Qué se recuerda y qué no

- **Un código sin host se rechaza**, y no cae en ningún seed ni recuerda el último. Las dos alternativas tienen la misma trampa y es callada: pegar el código de un amigo que hospeda en su propio servidor llevaría a otra sala con esos mismos ocho caracteres, sin un solo error.
- **No se recuerda ninguna confirmación.** Todo código que llega de fuera de la app pasa por la tarjeta de confirmación, siempre, sin excepción y sin estado persistido. Ver la regla del canal externo más abajo.
- **El seed propio se guarda, y es lo ÚNICO que se llena de una sola forma**: escribiéndolo en su pantalla. Entrar a la sala de alguien no lo toca, ni siquiera cuando no hay ninguno guardado. Lo que sí hace es prellenar esa pantalla, marcado como sugerencia.
- **El token de refresco de un seed cerrado se guarda; el password jamás.** Ver el apartado de almacenamiento.

### Manejador de protocolo

El instalador registra el esquema `kanpachi://`. Se invoca desde dos sitios: un enlace en Telegram, y el botón "Abrir en Kanpachi" de la página de invitación (ver `05-ui.md`). En ambos casos la app abre con el código puesto y el usuario no pega nada. Es la fricción más baja alcanzable.

**Modelo de amenaza del canal.** Si la página de invitación puede invocar `kanpachi://`, cualquier página de internet puede. El manejador queda expuesto a toda la web, no solo al dominio propio, y hay que tratarlo como entrada hostil:

- **Nada que llegue por este canal surte efecto sin confirmación dentro de la app.** El código no se aplica, la sala no se une, ninguna configuración cambia. La app abre, muestra la tarjeta de confirmación con lo que recibió, y espera. Siempre, sin excepciones y sin estado recordado que lo salte.
- Aceptar **únicamente** el formato exacto del código, con su host opcional. Cualquier otra cosa se descarta sin interpretarse.
- Longitud máxima estricta antes de parsear.
- Nada de rutas de archivo, argumentos de línea de comandos ni nombres de perfil por ese canal. La superficie es un solo dato: el código.
- Un código inválido nunca abre la app en un estado raro: se ignora y la app arranca en la pantalla normal.

**Por qué el diálogo del navegador no cuenta como confirmación.** Chrome y Edge preguntan si abrir Kanpachi. No dicen a qué sala se entra ni a qué servidor se conecta, la gente lo despacha sin leer, y ofrece un "recordar mi elección" que lo elimina para siempre. La confirmación real es la de la app, que sí muestra los datos concretos.

**La regla vale para cualquier canal externo futuro**, no solo para `kanpachi://`: argumentos de línea de comandos, archivos asociados, o lo que se agregue después. Si el origen está fuera de la app, hay tarjeta.

**La página de invitación resuelve el invite ID contra el registro del seed.** La URL lo lleva en la ruta (`kanpachi.accentio.dev/A7K2M9QX`), así que el servidor lo recibe y lo registra en sus logs. Es aceptable porque el invite ID dejó de ser material criptográfico con la decisión 2: quien lo lea obtiene la tarjeta y el derecho a tocar la puerta, jamás el secreto de la red real, que no vive en ningún servidor. Ver decisión 17 para el intercambio completo.

#### Por dónde viaja el enlace, de Chrome a la pantalla

Toda la cadena existía en piezas y no conectaba en ningún sitio: la página armaba el intent sin la clave de la tarjeta, el instalador registraba el manejador **sin `"%1"`**, de modo que Windows nunca pasaba la URL, y la pantalla de confirmación se alimentaba de dos cadenas de relleno. El enlace abría Kanpachi y el código había que pegarlo a mano.

```
Chrome  kanpachi://A7K2M9QX/#<clave de tarjeta>
  (la barra vacía la agrega al canonicalizar el URI)
   |
   v  HKLM\SOFTWARE\Classes\kanpachi\shell\open\command
kanpachid.exe --show "%1"            el LANZADOR, sin elevar
   |
   +-- ¿hay daemon?  sí -> show_ui {"link": ...} por el pipe
   |                 no -> el enlace va en los argumentos de arranque
   v
procesoHost.invitación               un buzón de UN enlace, el último gana
   |
   v  ui.Show()  ->  la ventana aparece
kanpachiui.exe
   |
   v  pending_invite, en el latido. Pedirlo lo CONSUME
InviteScreen                         y acá para, hasta que alguien pulse
```

Cuatro cosas de esa cadena tienen razón y no son detalle:

- **El enlace va al DAEMON y no a la interfaz**, por lo mismo que el acceso directo: la interfaz sin daemon son mandos sin nada detrás, y arranca callada por defecto, así que abrirla sola no enseñaría ni una ventana. Ver el modelo de procesos.
- **Dos vías para el mismo enlace, y hacen falta las dos.** El daemon puede estar vivo o no. Por el pipe cuando ya está; por los argumentos de arranque cuando hay que levantarlo. Las dos terminan en el mismo buzón, y el enlace se guarda **antes** de enseñar la ventana: al revés es una carrera que se pierde en la máquina rápida, porque la interfaz pregunta en cuanto aparece.
- **Recogerlo lo consume.** Sin eso, el latido de la interfaz volvería a enseñar la pantalla de confirmación cada dos segundos, incluso después de que alguien la cancelara.
- **Lo resuelve el daemon, no la app.** `PeekInvite` parsea el enlace, le pregunta al registro si esa sala existe y abre la tarjeta con la clave del fragmento. La app recibe el nombre de la sala y el apodo del host ya resueltos: no descifra nada y no vuelve a parsear una URL que vino de la web. **No toma el candado de la sesión**, y eso hace falta de verdad: el enlace puede llegar mientras se crea una sala, que lo tiene tomado durante decenas de segundos, y la confirmación no puede esperar a algo que el usuario quizá quiera cancelar justamente por haber pulsado el enlace.

**El fragmento sigue sin decidir a qué sala se entra.** `ParseRoom` lo descarta antes de mirar la forma, como siempre, y la clave se recorta aparte y solo sirve para descifrar la tarjeta. Una clave equivocada deja la vista genérica; jamás cambia el destino.

**Dos casos no ofrecen el botón de entrar**, y son distintos: que el enlace no se entienda, porque lo mandó una web y puede traer cualquier cosa, y que el registro AFIRME que esa sala no existe. Un registro que no contesta deja la sala como desconocida y el botón puesto.

**Ese botón se queda aunque entrar sí dependa del registro**, y conviene decir por qué. Esta pantalla es una medición de un instante, y el registro puede estar de vuelta cuando la persona pulse. Quitarlo por un fallo de hace un momento sería tratar la ausencia de información como una respuesta, que es la distinción que sostiene todo el ingreso temprano. Si sigue caído, entrar lo dice en el primer segundo con su propio mensaje, y el aviso de la portada ya lo venía anunciando por su lado.

### De dónde sale la identidad de encuentro

**No hay puerto, y no hace falta.** Un invite ID se resuelve a la identidad de ENCUENTRO con una función pura del dominio, `domain.DeriveRendezvous(id)`: Argon2id sobre el invite ID, sin red y sin preguntarle a nadie. Los dos lados la derivan por separado y coinciden, que es justo lo que hace que el vestíbulo se forme sin coordinar nada.

La red REAL no sale de ahí en ningún caso. Llega por el canje de credencial con el host, y es lo único que ese canje entrega.

Hubo un `RendezvousProvider` declarado en `core/port` para dejar el hueco de un proveedor remoto que diera salas con identidad de encuentro rotativa. Nunca tuvo implementación ni consumidor, y una interfaz que nadie satisface no reserva ningún hueco: se borró. El día que haga falta, el puerto se declara entonces, con el consumidor delante.

El registro del seed sí es un puerto, y resuelve otra cosa: la identidad de encuentro se deriva en la máquina y el registro emite y guarda los invite IDs. Que estén separados es lo que permite cambiar uno sin tocar el otro.

**El puerto no es opcional, y este documento decía que sí.** La cabecera anterior era *"Solo presentación. Que falle no impide entrar a ninguna sala"*. La tarjeta sí es presentación; el registro no: los códigos los emite él, y su máquina es el punto de encuentro al que llegan host e invitado.

```go
// El punto de encuentro. Sin él no se abre una sala ni se entra a ninguna.
type RoomDirectory interface {
    Seed() string
    Open(ctx context.Context, sealed []byte) (domain.Room, error)
    Lookup(ctx context.Context, id domain.InviteID) (sealed []byte, members int, err error)
    Publish(ctx context.Context, id domain.InviteID, sealed []byte) error
    // Dice si CONTESTA, sin preguntarle por ninguna sala: no lleva invite ID,
    // porque preguntar por uno inventado mediría otra cosa.
    Reachable(ctx context.Context) error
}
```

**Habla de bytes opacos y jamás de una tarjeta en claro.** El sellado ocurre en el dominio, con `domain.SealRoomCard`, y la clave se queda en la máquina del host: viaja en el fragmento del enlace, que el navegador no manda al servidor. Un puerto que recibiera la tarjeta legible obligaría al adaptador a decidir con qué cifrarla, y ahí es donde se filtraría.

**`Open` y no "publicar con un ID que traigo yo":** el registro EMITE el invite ID, porque es quien puede garantizar unicidad en su espacio, y emitir evita el ida y vuelta de proponer y ser rechazado. Devuelve la `Room` entera con el seed pegado, porque un invite ID solo significa algo en el registro que lo emitió y quien sabe cuál es ese registro es el adaptador.

**El contador ausente llega como `-1`, jamás como `0`.** El registro omite el número cuando nunca pudo hablar con el motor. Convertirlo en cero cambiaría "no lo sé" por "no hay nadie", que es otra afirmación y es falsa.

#### El cliente del registro

Vive en `daemon/adapter/directory`, es Go **puro** y corre en el job de Linux. Cada ajuste suyo es una negativa:

| Qué | Por qué |
|---|---|
| Esquema `https` fijo, con el certificado verificado | La tarjeta es presentación, y una presentación manipulada sigue siendo una mentira en la pantalla de alguien |
| `CheckSeedAddr` sobre CADA dirección resuelta, en CADA uso | Nada impide registrar un dominio cuyo registro A apunte a `192.168.1.1`, y esto corre como SYSTEM |
| El transporte jamás resuelve: se le entrega la dirección ya aprobada | Si resolviera él, entre nuestra consulta y la suya el DNS puede contestar otra cosa, y comprobar no gobernaría nada |
| Sin seguir redirecciones | Una redirección es cómo un nombre impecable termina en otro sitio, salteándose la comprobación que se acaba de hacer |
| Tope de respuesta y dos plazos, uno de conexión más corto que el de la llamada | `Open` y `Publish` corren con el candado de la sesión tomado y la pantalla comparte ese candado. Un seed que calla en vez de rebotar congelaría la UI el plazo entero |
| Sin proxy del entorno | Una variable de entorno no elige a dónde marca un proceso SYSTEM |
| Sin reintentos | El límite de tasa del registro cuenta también las peticiones que fallan, así que reintentar es cómo un tropiezo se convierte en un minuto de puerta cerrada |

Un adaptador de una sola dirección no necesita partirse en dos como los de Windows: no hay syscall que aislar. Lo que sí tiene es un **test de contrato** que levanta el paquete `registry` de este mismo repositorio en proceso y habla el protocolo entero contra él, con la firma, el base64, el fijado de la llave y los códigos de estado de verdad. Las dos puntas del protocolo del seed quedan congeladas en CI, sin red y sin despliegue.

#### `identity.key`, y quién puede crearla

La llave larga de esta instalación vive en `daemon/adapter/identity`, y ese paquete es el **único que la crea**. Todo consumidor la consume.

No es organización: la llave tiene un segundo consumidor prometido, el canje de credencial de la decisión 25, y un cargador escrito al lado de ese segundo consumidor sería dos escritores. El día que uno de los dos regenerara al no poder leer, las salas que este equipo tiene reservadas en el registro quedarían fuera de su alcance durante lo que le quede a su fijado, y la cara con la que lo conocen quienes ya jugaron con él cambiaría sin que nadie lo pidiera. Por eso **una llave presente e ilegible es un error y jamás una llave nueva**.

Se escribe con un orden que no admite ventana: se crea el temporal **vacío**, se le pone su ACL propia mientras no tiene nada dentro, y solo entonces se escribe la semilla. La ACL viaja con el rename, así que el nombre bueno tampoco existe nunca sin ella. Es el único fichero del proyecto que se escribe así, y la razón es que es el único cuyo robo ES la suplantación.

### Catálogo

Dos capas: los perfiles builtin que vienen en el instalador y los locales en `ProgramData`, creados con el creador de perfiles o importados de un `.json` compartido. Esquema v2, con precedencia, invariantes de carga y formato de intercambio detallados en `06-catalogo.md`.

Lo que vive en el dominio: el tipo `GameProfile`, la validación de invariantes, las reglas de precedencia entre capas y la fusión al importar. Todo puro y testeable sin disco.

Lo que **no** vive aquí: leer los archivos y consultar Steam. El almacenamiento va detrás de `CatalogStore` y la detección de instalados detrás de `GameLibrary`, ambos implementados en `adapter/`.

El principio que gobierna esta capa: **el catálogo describe qué necesita el juego, el código decide qué es aceptable conceder.** Las invariantes (puertos prohibidos, tope de rangos, solo reglas entrantes, jamás permitir por ejecutable) viven en el dominio y no tienen campo equivalente en el JSON. Un perfil corrupto o malicioso, como máximo, impide que ese juego conecte.

### Política de reglas

Entrada: perfil activo, rol (host o invitado), miembros presentes con sus IPs virtuales. Salida: un conjunto **declarativo** de reglas. `netfw` compara el estado deseado contra el aplicado y ejecuta la diferencia. No hay "agregar regla" imperativo suelto: cada cambio de miembros o de juego regenera el conjunto completo.

## Adaptadores y entrada

Todo lo que sigue vive en `daemon/` e implementa un puerto declarado en `core`. Es la única parte del proyecto que conoce Windows, EasyTier, Steam y el disco.

### Máquina de estados

```
Idle → Resolving → Connecting → Connected(direct|relay)
                                      │
                               Degraded ↔ Reconnecting
                                      │
                                    Idle
```

El estado es un valor único y explícito, sin flags booleanos regados. La UI lo renderiza, no lo infiere. Cada transición queda en el log con su causa.

**Todas las transiciones nacen de una acción del usuario o de un evento de red.** Ninguna nace de que un juego arranque o se cierre: el daemon no observa procesos. Elegir el juego en la UI abre los puertos, salir de la sala los cierra. Nada más los mueve.

Los eventos de red son los cinco del motor, y el supervisor los traduce uno a uno:

| Evento del motor | Estado | Qué más pasa |
|---|---|---|
| `EngineConnected` | `Connected` | Se relee la lista de miembros, se recalculan las reglas, se reajusta el adaptador, y siendo host se recorta el alcance del canal y se vuelve a anunciar |
| `EnginePeersChanged` | ninguno | Recalcula el conjunto de reglas completo y lo aplica SOLO si cambió respecto de lo último aplicado. Es el único camino con esa compuerta, ver abajo |
| `EngineDegraded` | **ninguno** | Relee la lista de miembros, y el estado sale de ahí. El evento es una pista con causa, que queda en el log; no fija nada |
| `EngineDisconnected` | `Reconnecting` | Arranca el plazo sin túnel, y en un invitado apaga la presencia del host |
| `EngineDied` | `Reconnecting` | Igual, y además despierta al watchdog |

**Estar en `Reconnecting` no es eterno.** A los 10 minutos sin túnel se sale de la sala con motivo propio, se cierran los puertos y se revierten los ajustes. Ver decisión 20.

##### Cuántas veces se aplican las reglas de verdad, medido, y la compuerta que salió de ahí

`EnginePeersChanged` regenera el conjunto entero, sin camino incremental. La pregunta era cuánto trabajo es eso en una sala que se mueve. Se midió dos veces, contando las líneas de `compuerta puesta`, que es una por cada aplicación real de nftables.

**Primera medición, el 2026-08-13, en el invitado de Linux:**

| | |
|---|---|
| Aplicaciones en la sesión entera | **709** |
| Veces que el conjunto deseado CAMBIÓ | **1** |
| Veces que cambió la firma de la malla | **1** |
| Pico, con el host recién caído | ~2 por segundo, sostenido cuatro minutos |
| En reposo, con la sala quieta | 0, medido 105 s muestreando cada 15 |

El motor republica el conjunto de confianza aproximadamente cada segundo, así que la tabla de rutas se toca todo el tiempo sin que cambie quién está. **708 de las 709 aplicaciones escribieron lo mismo que ya estaba puesto.**

**Segunda medición, el 2026-08-17, en el host del droplet, y el promedio de arriba escondía la forma:**

| | |
|---|---|
| En reposo, sala abierta y quieta, 60 s | **0** |
| Por transición, crear la sala y elegir juego | **19** |
| Pico dentro de un segundo de reloj | **31** |
| Separación entre aplicaciones, con 3 reglas | **16 ms** |
| Separación entre aplicaciones, con 7 reglas | **~23 ms** |
| Del log entero del banco, ~30 h | **2221 de 5136 líneas**, en solo 253 segundos distintos |

"~2 por segundo" era el promedio de una ráfaga contra el rato de silencio de al lado. La forma de verdad es una ráfaga de veinte o treinta aplicaciones idénticas, pegadas, seguida de nada. Y la separación **sigue al número de reglas**, que es lo que dice qué es ese hueco: no es una cadencia del motor, es lo que tarda cada aplicación. El daemon está saturado drenando eventos que dicen lo mismo.

**Lo que cuesta.** En Linux, tres ciclos de crear sala con juego dejaron a `kanpachid` en 5.4% de un núcleo sostenido, contra 0.13% en reposo. En Windows es diez veces peor y por una razón estructural: `Apply` enumera la tienda ENTERA de reglas del firewall por COM, dos veces por llamada. Medido sobre una máquina con 1157 reglas, una enumeración con las doce propiedades que se leen tarda **152 ms**, así que una aplicación ronda los 300 ms y una transición son casi seis segundos de leer el firewall de punta a punta para escribir lo que ya estaba. Es además la clase de actividad que un antivirus perfila.

**La compuerta, y por qué son dos métodos.** `applyPolicyIfChanged` compara la firma del conjunto deseado contra la del último aplicado y se salta el firewall si coinciden. Lo llama **un solo sitio**, `onPeersChangedLocked`, que es el de la ráfaga. Los otros dieciséis llamadores de `applyPolicy` no cambian, y eso es lo que hace correcto el corte: `Apply` calcula su diferencia contra las reglas VIVAS, así que reaplicar el mismo conjunto REPONE lo que alguien borró por fuera. Esa autorreparación es de lo que dependen el barrido del canario y la comprobación de la decisión 19, y un salto metido dentro de `applyPolicy` la apagaría entera.

**La firma gobierna la aplicación y NADA MÁS, y esa frontera costó encontrarla.** Del mismo evento colgaban otras dos cosas, reacotar el canal de control y volver a anunciar, y colgarlas también de la firma parecía equivalente. No lo es. Quien entra a una sala ya tenía credencial recién emitida, así que **ya estaba** en la lista de autorizados del canal; y sin juego activo los miembros no aparecen en ninguna otra regla, porque los puertos de juego son lo único que se abre hacia ellos. O sea que **un ingreso a una sala sin juego no cambia la firma**, que es justo el caso más común: se abre la sala, se reparte el código, y la gente entra antes de que nadie elija juego. Con la firma de condición ese invitado se quedaba sin anuncio, o sea con la pantalla de la sala sin nombre y sin juego. Las dos cuelgan ahora del cambio de MIEMBROS, comparado por dirección, que es su disparador de verdad.

**Comparado por dirección y no por la tabla entera**, porque el motor reporta el camino y la latencia de cada miembro, y esos dos se mueven solos: comparar las tablas completas diría "cambió" en cada evento de la ráfaga y no distinguiría nada.

**El reatado se resuelve olvidando.** La firma describe reglas contra el adaptador al que la compuerta está atada, así que tras un `BindRoom` el mismo texto describe otra cosa. Los siete sitios que atan pasan por `bindRoomLocked`, que limpia la firma al conseguirlo. Un contador de reataduras sería lo mismo con un campo más y una forma más de olvidarse de subirlo.

**Lo que dio, medido con el mismo guion antes y después**, tres ciclos de abrir sala y elegir juego en el droplet:

| | Antes | Después |
|---|---|---|
| Aplicaciones en los tres ciclos | **57** | **11** |
| Por ciclo | 19 | ~3.7 |
| Pico en un segundo de reloj | 31 | 3 |

Y la comprobación que importa más que el ahorro: `kanpachi protect`, que es `applyPolicy` sin compuerta, **sigue aplicando cada vez que se pulsa**, con el conjunto idéntico y la sala quieta. Dos pulsaciones seguidas, dos aplicaciones. La autorreparación no se tocó.

**Lo que ya estaba decidido y sigue valiendo:** coalescer o debounce encima no compra nada. Un gate por firma ya colapsa la ráfaga, porque lo que la ráfaga repite es exactamente la misma firma.

#### `Degraded` se DERIVA de la tabla de miembros, no se recuerda

Degradado es que el túnel sigue en pie y va peor, normalmente porque alguien llega por el relay del seed. Sigue sin arrancar ningún plazo, y esa ausencia importa igual que antes: contarlo como caída echaría de la sala a quien está jugando por relay, que es un caso soportado.

Lo que cambió es de dónde sale. **La sala está degradada cuando algún OTRO miembro llega por relay AHORA MISMO**, calculado en cada relectura de miembros, que ocurre en todos los caminos que pasan por `refreshPeersLocked`. Uno mismo no cuenta, y un camino todavía sin conocer tampoco: eso es una tabla a medio llenar, no una degradación.

**El fallo que esto cierra, medido el 2026-08-05 con el producto entero.** Degradado era un pestillo que nadie soltaba. Lo ponía el evento del motor, y volver a conectado exigía un `EngineConnected`, que el motor emite en un único sitio: cuando SUBE el adaptador virtual. Un corte de red no tira el adaptador, así que no había vuelta. Se apagó la WiFi doce segundos con una sala abierta contra `kanpachi.accentio.dev`: la sala pasó a degradada y ciento cincuenta segundos después seguía degradada, con la red entera recuperada, los dos adaptadores arriba, el motor original vivo, cero avisos en el log y **un solo miembro, que era uno mismo**. Una sala de uno no puede estar degradada: no hay nadie con quien ir por relay.

La segunda consecuencia era peor que la etiqueta. La deducción de la presencia del host desde la tabla de miembros exigía el estado `Connected` exacto, así que un invitado clavado en degradado se quedaba **sin la única capa que sigue funcionando cuando el canal de control está roto, colgado o nunca arrancó**, y el contador de veinte minutos de la decisión 20 perdía su respaldo en silencio. Por eso el predicado se llama `Established` y no se compara contra un estado suelto: degradado y conectado son el mismo hecho para todo lo que solo necesita que haya red y haya miembros.

#### La ausencia del host no es un estado de conexión

Implementa la decisión 20. Que el host se haya ido es información **sobre la sala**, no sobre el túnel: la conexión sigue perfecta, lo que falta es la persona que corre el juego. Meterlo en la máquina de estados de arriba mezclaría dos cosas que fallan por motivos distintos.

Se modela como un campo aparte del estado de sala, que la UI muestra sin ambigüedad:

| Campo | Valores | Qué significa |
|---|---|---|
| `Role` | `Host`, `Guest` | Quién declaró hospedar. Lo fija `CreateRoom`, no cambia en la vida de la sala |
| `HostPresent` | `true`, `false` | Si se sabe del host ahora mismo |
| `HostGoneSince` | marca de tiempo | Desde cuándo no. El cero es que está, o que soy yo el host |
| `HostLastHeard` | marca de tiempo | La última prueba de vida: el socket levantado, un anuncio, un aviso, o su dirección en la tabla del motor |
| `ReconnectingSince` | marca de tiempo | Desde cuándo no hay túnel. Es de la máquina de estados y no de la sala, a diferencia de las dos de arriba |

Reglas que se derivan:

- **El host se va:** sus reglas de firewall desaparecen con su máquina. Los invitados no tienen nada abierto, porque en un juego de estrella nunca abrieron nada. La red queda inerte, no insegura.

  La excepción, y es la única: un perfil de MALLA, o sea con `client_ports` no vacío, sí abre puertos en cada invitado. Es lo que documenta `06-catalogo.md` y lo que pide el netcode viejo de paso bloqueado, donde cada cliente habla con todos. Poner algo en `client_ports` expande el radio de explosión de todos los miembros y por eso el listón es más alto: se justifica en el perfil, se prueba que NO funcionaba en estrella, y la UI lo dice antes de entrar. La enorme mayoría de los juegos es estrella y ahí la frase de arriba vale literal.
- **El host vuelve:** su propio daemon conserva la sala en `hosted-room.json` con el juego que estaba activo, y al arrancar **pregunta** si reabrirla. Al reabrir, la identidad de la red es la misma, el perfil se repone resolviéndolo contra el catálogo de esa máquina, y las reglas se regeneran para los miembros presentes en ese momento. Nunca reabre sola. Ver decisión 2.
- **Nadie hereda el rol.** No hay promoción automática ni elección. Un invitado que quiera hospedar crea una sala nueva.
- **Se va el último nodo y la red deja de existir.** No queda estado de RED en ningún lado: ningún servidor sostiene la sala, y el seed no puede levantarla. Es la consecuencia natural de no tener servidor de salas, no una limitación que haya que disculpar.

  Lo que sí sobrevive es el **registro local del host**, en su propio disco: el invite ID vigente y la identidad de la red real, que es lo que le permite reabrir la misma sala con el mismo código tras un apagón. Es estado del dueño de la sala, no del sistema. Ver la decisión 2, que es donde vive esta pieza.

#### Salida automática a los 20 minutos sin host

También de la decisión 20. Cada cliente lleva su propio contador desde que la conexión de control al host se cayó. A los 20 minutos sale de la sala, revierte lo suyo y vuelve a Idle.

Es política local pura: no hay mensaje, no hay coordinación, no hay que confiar en nadie. Cada máquina decide sobre sí misma a partir de un hecho que no se puede falsificar, que su socket al host no está.

#### Expulsar y renovar son operaciones distintas

Implementan la decisión 22. Conviene tenerlas separadas en el código igual que en la UI, porque resuelven problemas distintos:

```go
// Saca a quien está dentro. Revoca su credencial y recalcula las reglas.
KickMember(ctx, virtualIP) error

// Cierra la puerta a quien está fuera. Revoca el código y emite otro.
// No toca a los presentes: ellos tienen credencial, no código.
RotateInviteCode(ctx) (domain.Room, error)
```

`KickMember` hace dos cosas en el mismo acto, y ninguna es cooperativa: revoca la credencial de ese miembro por el `EnginePort`, y saca su IP de la lista de miembros para que `policy/` regenere el `RuleSet` completo. La primera lo saca de la red en alrededor de un segundo, medido. La segunda garantiza que, aunque siguiera en la red, no alcance ningún puerto.

Solo el host puede llamar a las dos. Un invitado que las invoque recibe un error de la API.

#### Quién cuenta como miembro, y por qué el host tiene dos fuentes

La lista de miembros del invitado sale de una sola fuente, la tabla de rutas del motor. La del host sale de dos: **esa tabla, más quién tiene el canal de la sala abierto ahora mismo.** La segunda existe porque la primera cuenta de menos, y eso está medido.

**Lo medido, el 2026-08-13, con dos máquinas de verdad.** Host de Windows, invitado de Linux en WSL, los dos con binarios del día, el invitado llegando por el relay del seed:

| Quién mira | Qué ve |
|---|---|
| `members` en el HOST | **1**, solo él mismo |
| `members` en el INVITADO | **2**, él y el host, marcado `relayed` |
| El socket del canal de control, en el host | `Established 10.99.186.1 → 10.99.186.2` |
| La exposición del host | lleva `toward 10.99.186.2`, o sea que sí le abrió esa regla |

Sostenido treinta segundos muestreando cada cinco: no era retraso.

**La consecuencia no era cosmética, y por eso el arreglo no es de pantalla.** Los puertos del juego se abren hacia los miembros presentes. Con Counter-Strike activo, la exposición del host tenía dos reglas y las dos eran del canal de control, **ninguna del juego**. O sea que un invitado ya dentro de la sala, al que el host acababa de abrirle el canal, no podía jugar.

**Dónde estaba la contradicción.** En la misma función, con cinco líneas de diferencia: los puertos del juego se abrían hacia `MemberIPs(state.Peers)` y el canal de control hacia `authorizedControlIPsLocked()`, que ya unía la tabla del motor con las credenciales emitidas. Dos respuestas a la misma pregunta, y la que decidía lo que importa era la que no veía.

**El arreglo.** `withAdmittedLocked` le suma a la tabla del motor las direcciones que cumplen las tres cosas a la vez: tienen credencial viva emitida por este host, tienen el canal de la sala abierto, y no acaban de ser expulsadas. Van con camino `PathUnconfirmed`, que se pinta apagado y se escribe "sin confirmar": un socket prueba que alguien está, y no dice si llega directo o por relay. Por eso ese camino **no cuenta para degradado**, que es lo que decide `AnyRelay`.

**Por qué el canal de control puede decidir esto.** Porque para el host es conocimiento de primera mano y la tabla del motor es de segunda: la dirección la asignó él al emitir la credencial, su oyente solo acepta a las direcciones autorizadas, y lo que se comprueba es que hay un socket abierto desde ahí. No es un mensaje que alguien pueda falsificar, es la existencia de una conexión, igual que la presencia del host del otro lado. Y no amplía ninguna confianza: a esa misma pareja de fuentes ya se le confiaba abrir el puerto donde escucha código corriendo como SYSTEM, que es una puerta bastante más seria que la de un juego.

**Lo que esto arregla de rebote.** El latido que empuja el vencimiento de las credenciales solo renueva a los presentes, a propósito, para que la dirección de un ausente se libere sola. Con el host contando de menos, un miembro invisible dejaba de renovarse y se caía a las 24 h.

**Lo que queda abierto.** Por qué la tabla del motor no reporta al invitado en el host no está medido: la asimetría es del motor, no del daemon. Este arreglo no la explica, la rodea con una fuente que el host ya tenía.

### transport/pipe, implementa la entrada

Named pipe `\\.\pipe\kanpachi`. Autenticación por token generado en la instalación y rotado en cada arranque del servicio, guardado en `ProgramData\Kanpachi\`.

**El protocolo se define aparte de su transporte.** Los mensajes son JSON-RPC delimitado por líneas, y el named pipe es una implementación de ese contrato, no el contrato. La separación cuesta cero hoy y es lo único que hace falta para que el host headless en Linux de `07-futuro.md` reuse la API sobre un socket Unix, con el mismo protocolo y los mismos casos de uso.

```
transport/
  wire/        enmarcado por líneas con tope. Compartido, una sola copia
  protocol/    mensajes, códigos de error, serialización. Sin nada de Windows
  control/     el canal de la sala, que es otro transporte y no este
  pipe/        el named pipe y su autenticación por token
```

#### El diario de la operación larga, y por qué se PIDE

Crear una sala tarda decenas de segundos con todo funcionando bien: se lee la tabla de rutas, el registro entrega un código, arranca el motor, dos adaptadores tienen que tomar dirección, se sondea el MTU, se acota la compuerta y se abre el canal. Desde fuera, esa espera y un cuelgue se ven igual, y cuando falla lo único que llega a la pantalla es la última línea de error, que casi nunca es donde estaba el problema.

El daemon acumula los pasos en un diario y la pantalla los pide con el método `progress`. **La API sigue siendo petición y respuesta pura, sin empuje del servidor**, y esto no lo cambia: nadie manda nada que no le hayan pedido.

Tres cosas que no son detalles:

- **El diario tiene su PROPIO candado, no el de la sesión.** `CreateRoom` toma el de la sesión durante toda su ejecución, y este diario existe justamente para poder mirar dentro de esa espera. Colgándolo del mismo candado, quien preguntara se quedaría esperando exactamente lo que quiere observar.
- **`progress` se pide por una conexión APARTE.** El bucle de una conexión es secuencial —leer, despachar, contestar—, así que por la misma se encolaría detrás de la operación que quiere mirar y llegaría cuando ya no hay nada que enseñar. Hay ocho plazas y la interfaz usa una.
- **Los adaptadores también escriben en él**, a través de `port.ProgressSink`. Quien sabe que el proceso del motor acaba de arrancar, o que el adaptador virtual tardó doce segundos en tomar dirección, es el adaptador del motor y no el caso de uso.

Al pasarse de sesenta y cuatro pasos se tiran los del MEDIO, y **se cuenta cuántos**: una lista recortada en silencio se lee como una lista completa, y entonces el hueco donde estaba el problema parece que nunca ocurrió.

La interfaz solo sondea esto **en compilaciones de depuración**. No es por secreto: los pasos nombran subredes, adaptadores, seeds y tiempos, que sirven a quien construye Kanpachi y son ruido para quien juega. Ver `docs/05`.

**El enmarcado vive una sola vez.** El pipe y el canal de la sala leen bytes de gente que no es este programa, los dos corren como SYSTEM, y los dos necesitan el mismo tratamiento: tope antes de deserializar y desincronización tratada como terminal. Lo único que cambia entre ellos es el tope, un mega por el pipe donde pasa un catálogo importado, ocho kilobytes por el canal donde el mensaje más grande son unos cientos de bytes. Por eso el tope es un parámetro y el código es uno: el día que una copia se arregle, la otra no.

**La superficie es la mitigación principal:** la API solo puede aplicar perfiles del catálogo embebido. No existe la operación "abrir puerto arbitrario". Un proceso malicioso corriendo como el usuario puede, como máximo, unirse a una sala y aplicar el perfil de un juego, nunca abrir 445 ni nada fuera del catálogo. La frontera de seguridad honesta es la sesión del usuario, igual que en cualquier aplicación de escritorio.

#### Lo que está medido, y no argumentado

Los nombres viven bajo `\\.\pipe\ProtectedPrefix\Administrators\`: `kanpachi-installed`, `kanpachi-portable` y `kanpachi-console`. Cualquier proceso del usuario podría crear el equivalente sin ese prefijo y quedarse con el nombre antes que el daemon; ahí la defensa sería ganar una carrera, que se pierde el día que el arranque va lento. Bajo el prefijo protegido no puede, y no porque lo comprobemos nosotros: **arrancar el daemon sin elevar falla con "Access is denied" al crear el nombre.** Los tres nombres son distintos porque instalado, portable y desarrollo son dueños distintos y pueden coexistir.

El descriptor es `D:P(A;;GA;;;SY)(A;;GA;;;BA)(A;;0x12019b;;;IU)`. Al usuario interactivo se le da leer, escribir y sincronizar, **jamás `GENERIC_ALL`**, y la razón se comprobó sola: con el daemon corriendo como usuario normal, el pipe se crea y la primera conexión falla al aceptar, porque aceptar exige crear la instancia SIGUIENTE del pipe y eso el usuario no puede. Que es el punto entero, ya que crear instancias es cómo se secuestraría la conexión de la UI. Como SYSTEM sí tiene permiso y atiende normal. Probar a mano exige, por lo tanto, una consola elevada.

Pasar el descriptor vacío es un error y no una opción: el descriptor por defecto de un named pipe da lectura a Everyone y a la cuenta anónima.

**Los topes son constantes de compilación**, igual que los cortes automáticos: ocho conexiones, cinco segundos para saludar, diez minutos de ocio, cinco segundos por respuesta. El de escritura es por MENSAJE y no por escritura, porque `wire.Writer` escribe sobre un bufio y una respuesta grande sale en varios trozos; renovar el plazo en cada trozo deja que un cliente que lee un byte por segundo mantenga la conexión abierta para siempre.

**Un pánico atendiendo una conexión no se lleva el daemon.** Sin ese recover, cualquier ruta de la API que reviente mata el proceso, y con él la sala: puertos cerrados, motor caído y la partida de todos al suelo porque una pantalla pidió algo raro. Lo encontró un test con la API a medio implementar, que es la forma exacta que tiene el daemon mientras queden adaptadores provisionales.

##### Cada conversación tiene un vigilante, y tiene que durar lo que la conversación

Cerrar la conexión es la ÚNICA forma de desbloquear un `Read` en curso: no existe cancelar una lectura sin cerrar el descriptor. Por eso cada conversación lleva al lado una goroutine que mira el plazo del saludo, el cierre del oyente y el contexto, y cierra la conexión cuando toca. El bucle de abajo no puede vigilarse a sí mismo porque está bloqueado leyendo.

**Ese vigilante duraba cinco segundos, y eso colgaba el apagado entero.** Era un `select` suelto: pasado `HelloWait`, la rama del plazo se elegía, veía que la conexión sí había saludado, no hacía nada y la goroutine terminaba. A partir de ahí nadie miraba el cierre por esa conexión, así que `Listener.Close` —que espera a las conversaciones en curso— se quedaba esperando hasta los diez minutos de ocio.

Lo que lo convertía en interbloqueo es de dónde viene la orden de apagar: **la pide la interfaz por una de esas conexiones**. El daemon esperaba a que se cerrara la conexión de la UI; la UI moría cuando el daemon cerrara su Job Object; y el Job se cierra después de que el apagado termine.

Medido el 2026-08-08 con el bundle portable: tres apagados dejaron `apagando el daemon` en el log y **ninguno** llegó a `el daemon se apagó`. El daemon quedaba vivo y con él el ejecutable del bundle que lo espera, así que "salir de Kanpachi" no cerraba nada.

**Los tests no lo veían porque medían el caso que funcionaba.** El que había cerraba a los pocos milisegundos de saludar, o sea dentro de los cinco segundos en que el vigilante todavía existía. El nuevo espera a pasar ese plazo antes de cerrar, que es la forma que tiene toda conexión de verdad: la interfaz abre la suya al arrancar y pide el apagado minutos después. Tarda cinco segundos a propósito, porque el plazo es una constante de compilación y hacerla inyectable sería dejar que una prueba mueva un tope de producción.

#### El cliente, del lado de Flutter

Vive en `ui/lib/features/session/infra/daemon/` y repite la misma separación: `daemon_codec.dart` es el enmarcado y los mensajes, `daemon_client.dart` es la conversación, y `daemon_transport.dart` es una interfaz de tres métodos. Con eso, el saludo, la correlación y los plazos se prueban enteros sobre un transporte de memoria, sin pipe, sin daemon y sin Windows.

Tres cosas que el cliente resuelve y que parecen detalles hasta que faltan. **El saludo va primero**, porque el daemon rechaza todo método antes del `hello` y sin esa espera la primera petición de cada conexión falla con `unauthorized`. **Las respuestas se emparejan por id y jamás por orden de llegada**, que es correcto hasta el día que dos pantallas preguntan a la vez. **Toda petición tiene plazo**, porque un daemon vivo que dejó de contestar es indistinguible de uno muerto desde el otro lado.

**El transporte de Windows todavía no se puede escribir con `dart:io`, y eso está verificado.** No hay `Socket` ni `File` que sirva: el soporte de IPC multiplataforma sigue siendo una petición abierta en el SDK de Dart (dart-lang/sdk#47310), y el atajo que existía, `File(r'\\.\pipe\...').openSync()`, funcionaba hasta Flutter 3.24.5 y **está roto desde 3.27** con `PathNotFoundException ... errno = 53`, sin arreglo (flutter/flutter#163539). Así que hay que llamar a `CreateFileW`, `ReadFile` y `WriteFile` a mano. La pregunta que quedaba era **desde dónde**, y tuvo dos respuestas: la primera se probó y se cayó con medición.

**Se escribió con `dart:ffi` y corrompía la memoria del proceso.** Dos isolates trabajadores, E/S superpuesta, `calloc` para el `OVERLAPPED` y el buffer. Funcionaba, y aun así el registro de eventos de Windows del 2026-08-09 tiene **49 caídas de `kanpachiui.exe` en 32 horas**: nueve con código `0xC0000374`, o sea `STATUS_HEAP_CORRUPTION`, el gestor de heap cazando una escritura fuera de una asignación, y el resto repartidas entre `ntdll` y `flutter_windows`, que es exactamente lo que hace la memoria corrupta: mata al siguiente que toca la zona, no al que la rompió.

El fallo era **estructural y no un descuido suelto**. Una E/S superpuesta le presta al kernel dos punteros, el `OVERLAPPED` y el buffer, hasta que la operación termina de verdad. En Dart, la vida de esos punteros dependía de que un isolate llegara a su `finally`, y un isolate no da esa garantía: `Isolate.kill` no interrumpe una llamada nativa, así que el dueño acababa cerrando el handle por debajo tras una gracia de tres segundos. No hay forma de arreglar eso sin un dueño de verdad.

**Hoy vive en el runner de C++ y llega por `MethodChannel`.** `windows/runner/kanpachi_pipe.cpp`: un hilo por conexión, dueño del handle, que cancela y ESPERA su lectura pendiente antes de que nada se destruya, y cuyo buffer de escritura es un `std::vector` local que muere después de cobrar la operación. Los eventos cruzan al hilo de plataforma por una ventana *message-only* propia, no enganchándose al window proc de la aplicación, que es camino de otra de las firmas de caída. El transporte de Dart quedó en cuarenta líneas que solo hablan por canales.

Lo que hizo esto barato es que **el transporte es una interfaz de tres métodos**: cambiar de `dart:ffi` a C++ no tocó el cliente, ni el códec, ni sus tests. Era el argumento con el que se justificó la interfaz, y se cobró entero.

Lo que no está en discusión: **la lectura no puede bloquear el hilo de la UI**, así que va en un hilo aparte y con E/S superpuesta.

#### Las cuatro reglas del parseo, que son las mismas del catálogo

Este código corre como SYSTEM y lee de un pipe al que puede hablarle cualquier proceso del usuario, así que se trata como entrada hostil de punta a punta:

1. **Tope de tamaño ANTES de deserializar**, un mega. Antes y no después, que es la mitad del punto: un tope aplicado después ya pagó el coste de parsear. Pasarse **corta la conexión**, y eso no es severidad de más: con mensajes delimitados por líneas, uno que no cupo deja el flujo desincronizado, y seguir leyendo sería interpretar la cola de un mensaje gigante como mensajes nuevos.
2. **Lista de métodos cerrada**, comprobada contra una tabla y jamás despachada por reflexión. Lo que no está no se interpreta, no se registra su contenido y no se adivina.
3. **Parámetros estrictos.** Un campo que el esquema no define rechaza el mensaje entero, igual que en el catálogo y en el estado guardado. Un campo de más no es un cliente amable con extensiones, es un cliente que cree estar pidiendo algo que este daemon no hace.
4. **Los tipos del dominio se reconstruyen, jamás se creen.** Un perfil que llega por el pipe se pasa por el mismo decodificador estricto que uno que llega en un archivo, así que un perfil que abarca el 445 se rechaza igual venga de donde venga. Un perfil de firewall en una orden de suspender se busca en la tabla cerrada y no se toma del número que mandó el cliente.

#### El saludo va primero

La primera línea de cada conexión tiene que ser `hello` con el token, y hasta que se conteste no se admite nada más. Sin esa puerta, un proceso sin el token igual podría pedir el estado, y el estado dice en qué sala estás y con quién. El token se compara en tiempo constante, y el estado de autenticación es **por conexión**: que una haya saludado no autentica a la siguiente.

Lo honesto: el token vive en `ProgramData` con lectura para los usuarios de la máquina, así que no es lo que separa al usuario de sus propios datos. Lo que acota la superficie es la lista cerrada de métodos, y el saludo cubre lo poco que esta capa sí puede cubrir.

#### El formato de cable

Los enums viajan como **cadenas** y no como el número de un iota. Con el número, agregar un estado en medio de un bloque de constantes le cambiaría el significado a todos los de abajo en una UI ya instalada, y el síntoma sería una pantalla que dice "degradado" cuando el daemon dijo "reconectando". Las duraciones viajan en milisegundos, ya calculadas contra el reloj del daemon, para que la UI no reste contra un reloj que puede no ser el mismo.

Eso no contradice "nada de DTOs entre capas": aquella regla habla de mapear structs entre anillos del mismo proceso, que es ceremonia sin retorno. Esto cruza una frontera de procesos y de lenguajes, y ahí el formato es un contrato con Flutter que tiene que poder no moverse cuando el dominio se mueva.

**Un pedido, una respuesta, siempre.** Ni siquiera un método desconocido se queda sin contestar: un cliente sin respuesta es una UI colgada, que del lado del usuario se ve peor que un error.

La única operación que devuelve **estado y error a la vez** es expulsar, y es el caso de la expulsión a medias: el expulsado ya salió del conjunto de reglas, así que la lista tiene que redibujarse sin él incluso con la operación fallida.

| Operación | Método en el cable | Notas |
|---|---|---|
| `CreateRoom(nickname, nombre)` | `create_room` | Fija `Role = Host`. **No pide juego:** la sala es independiente del juego activo, decisión 20 |
| `JoinRoom(code, nickname)` | `join_room` | El nombre es obligatorio, ver decisión 21 |
| `LeaveRoom()` | `leave_room` | Idempotente. Lo llaman el usuario, el contador de 20 minutos y el apagado del servicio |
| `ActivateProfile(gameID)` | `activate_profile` | **Solo el host.** Abre los puertos de ese juego. `gameID` vacío los cierra todos |
| `KickMember(virtualIP)` | `kick_member` | **Solo el host.** Revoca la credencial y recalcula, ver decisión 22 |
| `RotateInviteCode()` | `rotate_invite_code` | **Solo el host.** Código nuevo, los presentes no se enteran |
| `RenameRoom(nombre)` | `rename_room` | **Solo el host.** Presentación pura: republica la tarjeta cifrada |
| `InviteLink()` | `invite_link` | El enlace con la clave de la tarjeta en el fragmento, para copiar al portapapeles |
| `Status()` | `status` | Estado, rol, `HostPresent`, miembros con su nombre, juego activo, y las alertas vigentes |
| `ListGames()` | `list_games` | Con los instalados arriba. El orden es un atajo, jamás una puerta |
| `SaveProfile(perfil)` | `save_profile` | Alta manual. Nace **sin verificar** y el campo se descarta venga como venga |
| `ImportCatalog(archivo, elegidos)` | `import_catalog` | Nada se sobreescribe en silencio, y un rechazado no se puede forzar |
| `ExportCatalog(soloPropios)` | `export_catalog` | |
| `MarkVerified(gameID, constancia)` | `mark_verified` | La única vía para que un perfil quede verificado, y la dispara salir de la sala |
| `ForeignRulesFor(gameID)` / `SuspendForeignRules(reglas)` | `foreign_rules_for` / `suspend_foreign_rules` | Se consultan y se muestran. Nunca se desactivan solas |
| `DiagReport()` | `diag_report` | Consulta `Diagnostics` al motor y conserva lo que el motor no sabe: el MTU lo sondea netcfg y la subred la eligió el plan de direcciones |
| `ObserveGame(proceso, árbol)` | `observe_game` | La foto de sockets del creador de perfiles. Es la ÚNICA función del programa que mira un proceso |
| `RejectedGames()` | `rejected_games` | Los perfiles que el catálogo rechazó, con su motivo, para que un archivo mal escrito sea arreglable en vez de invisible |
| `SavedRoom()` / `ResumeRoom()` / `DiscardSavedRoom()` | `pending_room` / `resume_room` / `discard_pending_room` | La sala que esta máquina hospeda, tal como quedó en disco. **Se reabre sola en cada arranque**, ver decisión 2: estas tres son la salida de emergencia de cuando eso falla. Los dos nombres de cable con `pending` dentro están congelados |
| `LastRoom()` | `last_room` | Los datos de "volver a la última sala". Entrar es el `join_room` de siempre con el código guardado |

Tres operaciones **no** vienen del named pipe, y las tres las llama el supervisor o el adaptador del canal de control:

| Operación | Quién la llama | Qué hace |
|---|---|---|
| `IssueCredentialFor(pedido)` | El canal de control, cuando alguien toca la puerta | Ver abajo |
| `OnRoomAnnounce(anuncio)` | El canal de control de un invitado | Aplica el nombre y el juego que dijo el host, resolviendo el id contra el catálogo PROPIO |
| `OnRoomNotice(aviso)` | El canal de control de un invitado | Expulsión o cierre de sala. Las dos terminan en salir, y se distinguen en lo que dirá la pantalla de inicio |
| `OnCodeRotated(sala)` | El canal de control de un invitado | Toma el código nuevo que repartió el host y reescribe el guardado |
| `OnEngineEvent(evento)` | El supervisor, drenando el motor | La tabla de arriba |
| `OnEngineGaveUp(motivo)` | El supervisor, cuando su watchdog agota los reintentos | Cierra la sala y purga el grupo `Kanpachi` |
| `OnPeersChanged()` | El supervisor | Recalcula el conjunto de reglas completo |
| `SetHostPresent(bool)` | El supervisor, drenando la presencia | Enciende o apaga la presencia. No toca la máquina de estados |
| `Tick()` | El supervisor, cada 15 s | Hace vencer los plazos. Es la puerta periódica, no la única |
| `TickHostAbsence()` | El contador de la decisión 20, con nombre propio | El corte a los veinte minutos, solo |
| `ReapplyAdapter()` | El supervisor, en cada identificación de red y cada ocho latidos | Repone lo que Windows revirtió |
| `RefreshAlerts()` | El supervisor, cada 60 s | Corre el módulo de exposición, repone las reglas propias si estaban alteradas, y publica el resultado. Ninguna comprobación es fatal |
| `SavedRoom()`, `ResumeRoom()`, `DiscardSavedRoom()` | La UI y el CLI, cuando la reapertura automática falló | Reponer a mano la sala propia, o cerrarla. El arranque ya la reabrió solo |
| `LastRoom()` | La UI, en la pantalla de inicio | Los datos de "volver a la última sala". Entrar es el `JoinRoom` de siempre |

Sobre la primera: La llama el adaptador del canal de control cuando alguien toca la puerta del vestíbulo. Vive en los casos de uso y no en el adaptador porque todo lo que decide es política: si esta máquina puede emitir, qué dirección le toca al que entra, cuánto vale la credencial y qué se le cuenta de la red. El motor pone el token, que es lo único que no se decide acá y tiene que ser así, porque revocarlo es lo que corta la sesión.

**Las direcciones se reparten mirando dos listas, no una:** los peers conectados y las credenciales emitidas todavía vigentes. Solo los peers repartiría la misma dirección a dos personas que entran a la vez, que es exactamente lo que pasa cuando alguien manda el código al grupo y los tres lo pegan al mismo tiempo.

#### Y esa dirección viaja hasta el motor del invitado, con prefijo

`guestRequest` le manda al motor el nombre de la red, el secreto de la credencial **y la dirección**, en el campo `ipv4` de `GuestArgs`. `config::guest` la fija con `set_ipv4` y apaga el DHCP, igual que ya hacían el host y el vestíbulo. Va con prefijo, `10.99.77.2/24` y no `10.99.77.2`: el motor la parsea a un `Ipv4Inet`, y sin prefijo el error que devuelve es literalmente *"is not an address with a prefix"*. La arma `guestAddress` en `spec.go`, con la dirección de la credencial y los bits de la subred de la sala.

**Antes el motor la elegía solo, y eso rompía la reconexión.** El DHCP de EasyTier toma la primera libre de la subred mirando los peers que ve (`instance.rs`, `dhcp_inet.network().iter().find(...)`), o sea `.2` cuando el único presente es el host, mientras el daemon del invitado esperaba la que dice la credencial y a los 30 s daba la red por rota. Los dos números coinciden mientras nadie se reconecte: cuando alguien sale y vuelve, el host pasa a `.3`, porque su `.2` sigue reservada las 24 h de `CredentialTTL`, y el motor seguía dando `.2`. A partir del primer fallo fallaban todos los reintentos, con el número del host subiendo y el del motor clavado.

Medido el 2026-08-08 con la sala `10.99.29.0/24`: la credencial decía `10.99.29.9` y el adaptador tomó `10.99.29.2`, o sea la novena credencial emitida contra el mismo `.2` de siempre. Del lado del que ya estaba dentro se veía lo contrario y confirmaba el diagnóstico: el mismo miembro aparecía SIEMPRE como `.3` en cada reconexión, porque el DHCP era estable y el que se movía era el contador del host.

Queda escrito porque explica una decisión que de otro modo parece de más: el host reparte mirando dos listas y el motor obedece, en vez de dejar que el motor reparta. Y queda una asimetría real, que **solo el host libera direcciones**: `clear(s.issued)` vive en `leaveroom.go` y en ningún otro sitio salvo reiniciar el daemon. Con la dirección fijada eso ya no bloquea a nadie, sigue significando que el contador de un host que no cierra la sala nunca baja.

`Status()` es el único canal por el que la UI se entera de las alertas del módulo de exposición. No hay notificación aparte ni evento especial: el módulo publica su último resultado y `Status()` lo arrastra, así que una alerta nunca puede bloquear ni retrasar una respuesta.

### La contención vive en DOS capas

Antes de leer el adaptador conviene tener claro el reparto, porque cada capa
falla por un motivo distinto y la pantalla tiene que poder decir cuál se rompió.
La decisión 27 lo argumenta entero.

| Capa | Cómo | Qué hace | Quién la ve |
|---|---|---|---|
| Reglas del Firewall de Windows | COM, `INetFwPolicy2` | ABREN los puertos del juego activo | El usuario, sin elevar, con sus herramientas de siempre |
| Compuerta | Sesión propia de WFP | CIERRA todo lo demás del adaptador virtual | Solo la pantalla de Kanpachi, y `netsh wfp` con consola elevada |

**Lo que la primera capa no puede expresar es "denegar todo salvo esto".** Los
bloqueos explícitos ganan sobre cualquier permiso en conflicto y Windows no
admite orden asignado por el administrador, así que un bloqueo total taparía
también las reglas del propio Kanpachi. Por eso la lista de lo que se abre es
ADITIVA, y por eso hace falta la segunda capa para volverla COMPLETA.

**En WFP un Block es HARD por defecto y un Permit es SOFT.** Esa asimetría es lo
que hace que un bloqueo nuestro anule una regla permisiva ajena sin tocarla, y lo
que conserva el veto del usuario, porque sus bloqueos siguen ganándole a nuestros
permisos.

Medido con dos máquinas los días 3 y 4 de agosto de 2026, las cuatro apuestas:
el bloqueo duro le gana al permiso vivo, la condición de interfaz se aplica de
verdad, el bloqueo de todo convive con un permiso espejo propio, y el veto del
usuario sigue ganando. Los detalles y lo que sigue sin medirse están en la
decisión 27 y en `08-plan-de-adaptadores.md`.

La compuerta va **solo** en `ALE_AUTH_RECV_ACCEPT_V4` y `V6`, jamás en
`ALE_AUTH_CONNECT`: bloquear la salida impediría que un invitado marque al puerto
del juego del host.

`ExposureAudit.Enforcement()` devuelve lo MEDIDO en las dos capas y no un
veredicto. Quien juzga es `domain.Enforcement.Diff`, que es dominio y corre sin
Windows. Una compuerta **sin comprobar** es distinta de una **ausente**: una es un
hecho y la otra es ceguera, y se muestran distinto.

### adapter/firewall, las dos capas juntas

Implementa `FirewallPort` componiendo las otras dos, y es **intersección y no reemplazo**: un paquete tiene que pasar las dos. Por eso la compuerta no abre nada que los permisos visibles no abran ya.

Lo que este paquete decide no es cómo se habla con Windows: es el **orden** de las dos capas y qué pasa cuando una falla. Las dos cosas tienen una dirección correcta y una que abre un agujero, así que es Go puro y lo prueba el job de Linux.

| Operación | Orden | Por qué |
|---|---|---|
| `Apply` | Compuerta, después permisos | El instante intermedio deja lo que sobra **cerrado**. Al revés deja permisos nuevos sin nada que los acote |
| `PurgeOwned` | Permisos, después compuerta | El espejo del anterior |

Y las dos veces, si la primera capa falla la segunda **no se toca**. La compuerta se aplica en una transacción, así que un fallo deja la anterior intacta y devolver ahí deja el sistema en el estado consistente de antes. En la purga, unos permisos que no se pudieron quitar bajo una compuerta siguen acotados; los mismos sin ella son el agujero.

**Quién enciende la compuerta, y cuándo.** `BindRoom` del puerto, que llama el caso de uso. El motivo es que el adaptador nace DESPUÉS del daemon: lo crea el motor al levantar cada red, así que el único momento en que se puede acotar es justo después de que la red esté arriba, y eso solo lo sabe quien la levantó. El nombre del adaptador NO viaja desde core: son `domain.AdapterName` y `domain.LobbyAdapterName`, y `BindRoom` las resuelve a LUID con una función que le inyecta el cableado de Windows, para que este paquete siga siendo puro. Elegir a qué adaptador se acota un bloqueo duro es la decisión que separa contener la sala de dejar al usuario sin su red de casa, así que no se pasa por parámetro.

Acotar es **una sola llamada para las dos capas**. Si discreparan sobre qué adaptador es la sala, los permisos irían sobre uno y el bloqueo sobre otro: un adaptador con permisos y sin compuerta, con las dos capas contestando que sí.

**La compuerta cubre los DOS adaptadores.** El vestíbulo no es un extra: es el adaptador donde llega gente que todavía no es miembro, o sea el que menos puede quedarse sin ella. `wfp.Scope` lleva el LUID de la sala con su rango, y el del vestíbulo, con el rango del vestíbulo como constante y no como campo, porque `RendezvousSubnet` es igual para todas las salas y un campo por el que pasarlo sería un campo por el que ensancharlo. Cuál de los dos cubre cada permiso lo dice la dirección local de la regla, no una bandera: una dirección dentro del vestíbulo solo puede vivir en el adaptador del vestíbulo.

Las tres ranuras del vestíbulo quedan **reservadas incluso sin vestíbulo**. Corriendo los permisos hacia arriba cuando falta, un permiso ocuparía la ranura de un bloqueo del vestíbulo, y la limpieza siguiente lo borraría creyendo que barre un bloqueo que ya no aplica: un puerto que se cierra solo, sin nada que lo explique.

Y la medición pregunta por la ranura del vestíbulo **solo cuando se pidió cubrirlo**. Esa condición separa "no aplica" de "falta": un invitado soltó el vestíbulo a propósito y su compuerta está entera, mientras que un host con el bloqueo del vestíbulo caído tiene media compuerta y no puede salir en verde.

**Tras un reinicio del motor hay que REACOTAR, y eso es un paso propio.** Los adaptadores virtuales son nuevos, o sea LUID nuevo, y una compuerta que se quede apuntando al viejo no falla en ningún sitio: emite sus filtros, la llamada devuelve éxito, y la pantalla dice que la sala está contenida mientras debajo hay una red virtual con los permisos puestos y sin bloqueo. Lo hace `OnEngineRestarted`, que llama el supervisor cuando `Restart` ya esperó a que las dos redes tengan dirección, y ahí fallar es fatal.

El evento de conexión también reacota, y ahí NO es fatal. Llega en cuanto conecta la primera de las dos redes, así que durante un reinicio llega legítimamente con el vestíbulo todavía sin levantar; tratarlo como fatal ahí convertía una carrera de un segundo en una sala que no volvía nunca, medido con las dos redes ya arriba y el estado clavado en reconectando. Uno es oportunista y el otro exigente, y por eso son dos.

**`Apply` falla en la cara si hay reglas y no hay dónde acotarlas.** Antes dejaba un aviso en el log y escribía los permisos igual, o sea que la lista de permitidos volvía a ser aditiva justo cuando había puertos que abrir. El conjunto vacío sigue pasando y no es una excepción: sin nada que abrir no hay nada que acotar, y ese es el estado normal del daemon en reposo, además de lo que garantiza que la interfaz virtual nazca sin nada abierto. Los casos de uso tratan el fallo como fatal, a diferencia de los ajustes del adaptador: un MTU mal puesto degrada la partida, y una sala sin compuerta miente sobre lo único que este producto promete.

`SetScope` sigue existiendo con el adaptador ya resuelto, y lo usan las herramientas de medición, que trabajan sobre un adaptador elegido a mano. Las dos entran por la misma puerta privada, que valida siempre.

`Enforcement` mide las dos y no juzga ninguna. Un fallo en cualquier mitad tira la medición entera, porque devolver la mitad medida y la otra en cero se lee igual que "esa mitad no tiene nada puesto", que es la conclusión opuesta.

### adapter/firewall/wfp, la compuerta

Tres ficheros, y la línea que los separa es la misma que en el adaptador COM: **lo que decide va en Go puro que prueba el job de Linux, y lo que llama a la API va detrás de `//go:build windows`**.

| Fichero | Qué hace |
|---|---|
| `spec.go` | Decide QUÉ filtros se ponen: qué acción, qué peso, qué alcance |
| `conditions.go` | Decide CÓMO se compara cada condición: campo, tipo de comparación y valor |
| `gate_windows.go` | Copia eso a las estructuras de WFP y llama. No decide nada |

Emite tres cosas fijas **por adaptador** más un permiso por regla: bloqueo de todo en IPv4 por adaptador, el mismo por rango, bloqueo de todo en IPv6 por adaptador, y los permisos espejo. Las tres de la sala siempre; las tres del vestíbulo cuando lo hay. El porqué de cada pieza está en la decisión 27.

El tipo `Layer` no tiene valor para `ALE_AUTH_CONNECT` y no lo va a tener. Lo que no existe en el tipo no se puede pedir por error, y hay un guardián en `internal/arch` que además falla si alguien nombra esa capa por su nombre de Windows para saltárselo.

`FilterSpec` se construye en un solo sitio, con el alcance como argumento obligatorio, y `Validate` lo recomprueba antes de que llegue a la API. Un guardián prohíbe literales fuera de ese fichero: un literal puede omitir el alcance y compilar igual.

**Por qué `conditions.go` está separado.** Ahí se deciden cuatro cosas que se equivocan calladas, y ninguna necesita Windows para comprobarse: el orden de bytes de una dirección, que al revés produce un filtro válido que casa con otra red; la máscara de un prefijo, donde un bit de corrimiento duplica el alcance de un bloqueo; rango contra igualdad en el puerto, que pedido mal abre solo el primero de un rango; y cuántas condiciones salen de qué campo, que es lo que decide si WFP las une con Y o con O.

Las claves de los filtros salen de la **ranura** que ocupan, y no de su etiqueta. Ver la decisión 27: derivarlas de la etiqueta deja huérfanos los filtros del juego anterior cada vez que se cambia de juego.

`Measure` recibe el conjunto deseado, y no es un atajo: las ranuras son posiciones y no nombres, así que para poder decir "falta el permiso de tal regla" hay que saber qué regla ocupaba esa ranura. El sistema sigue siendo el que contesta si el filtro está.

**Casi todo exige elevación, y abrir la sesión no.** Medido: `FwpmEngineOpen0` funciona como usuario normal; leer un filtro que EXISTE y empezar una transacción devuelven `ERROR_ACCESS_DENIED`. Preguntar por un filtro ausente sí contesta, y esa asimetría es una trampa que ya produjo dos conclusiones equivocadas: ver `08-plan-de-adaptadores.md`. Lo que la hace inofensiva es que "no está" y "está y no puedes verlo" llegan con códigos distintos, así que sin elevar la respuesta es SIN COMPROBAR y nunca "ausente".

### adapter/probe, la única medición que sale a la red

Todo lo demás de este capítulo lee lo que ESTA máquina tiene configurado,
preguntándole a la misma Windows que aplica las reglas. Es honesto y no puede
contestar la pregunta del producto, que es si desde otra PC se llega. Un alcance
de filtro que dejó de casar, una regla del sistema que nadie miró, o una
herramienta que abrió su propio hueco, se ven todos igual desde dentro: verde.

El adaptador es minúsculo y no tiene fichero de Windows. Abre una conexión TCP y
la cierra: no manda un byte, no lee ninguno, y no sabe qué significa lo que midió.
Eso último es `domain.ProbeReport.Verdict`, que se prueba sin red.

**Lo único específico de Windows es un número.** `syscall.ECONNREFUSED` en Windows
NO es el `WSAECONNREFUSED` de Winsock: es una constante inventada del bloque
`APPLICATION_ERROR`, así que el `errors.Is` que uno escribe sin pensar no casa con
un rechazo real. La constante 10061 está escrita a mano en el fichero portable, y
eso es lo que permite que el camino de Windows tenga test y corra en Linux.
Envenenar esa rama rompe el test **en Windows**, que es la prueba de que hace
falta.

#### Cómo se lee lo que contesta Windows, medido el 2026-08-04

Con el droplet marcando por Tailscale a una máquina Windows, sobre el mismo
puerto y con el firewall encendido:

| Estado del puerto 45999 | Resultado |
|---|---|
| sin regla, sin oyente | silencio |
| **con regla de permiso**, sin oyente | **silencio** |
| con regla de permiso, con oyente | conecta |

La fila del medio es la que manda: **Windows no devuelve RST hacia dentro aunque
el firewall permita el puerto**, que es el modo sigiloso del Firewall de Windows.
De ahí sale toda la lectura:

- **conecta**: el firewall lo dejó pasar Y hay un programa escuchando.
- **silencio**: o lo bloquea el firewall, o no hay nada escuchando. **No se
  distinguen.**

La consecuencia de diseño es fuerte. Un puerto de juego callado **no** significa
que esté cerrado: significa que el juego no está abierto, que es el estado normal
mientras alguien mira la pantalla. Un veredicto de "faltan puertos" se habría
encendido en falso siempre, y por eso no existe.

Lo que sí prueba es una respuesta donde no tiene que haberla, y esa no necesita
distinguir nada. La primera corrida ya lo dio: el 445 de la máquina de desarrollo
contestó desde otra PC, o sea el compartir archivos de Windows alcanzable por la
red virtual.

#### Solo TCP, y hay que decirlo

Por UDP no hay pregunta que hacer sin hablar el idioma del programa que escucha:
un puerto bloqueado y uno abierto que no contesta se ven idénticos. Sondear UDP
produciría silencio siempre, o sea un verde que no midió nada.

Lo que se mide por TCP se traslada a UDP por una razón verificable en el código:
los bloqueos de la compuerta se emiten con condición de adaptador y de rango, y
**ninguna de protocolo**. La compuerta no mira el protocolo, así que lo que le
hace a un TCP se lo hace igual a un UDP. Eso NO cubre las reglas del Firewall de
Windows, que sí son por protocolo, y por eso la pantalla lo dice con todas las
letras.

#### La referencia, y por qué solo el invitado puede

El canal de la sala tiene que contestar, porque el host lo escucha mientras la
sala esté abierta. Sin una respuesta que llegue, el silencio de todo lo demás no
distingue una PC blindada de una apagada, y la pantalla estaría afirmando lo que
no sabe. Por eso `VerdictUnreachable` es un estado propio y no se lee como
"cerrado".

Y por eso se sondea al **host** y no a cualquiera: sondear a un invitado daría
silencio en todo, que es a la vez lo que tiene que pasar y lo que pasa con la PC
apagada. Es una medición que no puede fallar, o sea que no mide.

El host no se puede sondear a sí mismo, y tampoco es una limitación de
implementación: lo que se manda a la propia dirección no atraviesa ningún
firewall, así que contestaría que está todo abierto en una máquina blindada. Su
pantalla dice eso con esas palabras, que lo pulse alguien más. Que el resultado
viaje de una persona a otra es una limitación de esta versión y está escrita como
tal.

#### La lista es corta y fija

Esto no es un escáner de puertos. Son el canal de la sala, los puertos que la
cuarentena tapa, y los de fábrica de las herramientas de escritorio remoto.

Los últimos **no son una lista negra y no sirven para bloquear**: esas
herramientas escuchan donde el usuario les diga, y por eso lo que se audita de
ellas es el ejecutable. Esto es un muestreo, encuentra al que dejó la
configuración por defecto y no al que la cambió, y la pantalla lo dice. Los de la
cuarentena salen de `forbiddenPorts` con un guardián que falla si alguien amplía
esa lista sin ponerle nombre acá.

Un puerto que el juego activo pide deja de ser prohibido: lo tiene abierto a
propósito, y encender la alarma sobre algo que el usuario pidió es la forma más
rápida de que aprenda a ignorarla.

### adapter/canary, la Protección Kanpachi comprobándose a sí misma

El sondeo de arriba tiene una mitad floja, y está medida: en Windows un puerto
callado no distingue "lo bloqueó el firewall" de "no hay nada escuchando". Así
que una compuerta muerta con el juego cerrado se lee igual que una compuerta
sana.

El canario quita esa ambigüedad por el único camino que queda: **poniendo a
alguien detrás de la puerta a propósito**.

#### La secuencia

```
   HOST                                      MIEMBRO
   ────                                      ───────
1. abre un socket TCP+UDP en un puerto
   al azar, ligado SOLO a su IP de la
   sala. Ese puerto NO está en el
   conjunto de reglas, así que la
   compuerta lo tiene que bloquear
        │
2.      │  por el canal de la sala:
        │  "márcame al 51023, número XYZ"
        └────────────────────────────────────►
                                             3. marca a la dirección de la
        ◄────────────────────────────────────    CONEXIÓN que ya tiene abierta
        │
4. su propio socket registra si alguien llegó   ← el hecho que decide
        │
5.      │  el miembro le cuenta qué vio
        ◄────────────────────────────────────
6. compara las dos cosas
```

El miembro no elige el puerto, no lo descubre y no lo busca: se lo dice el host
en el paso 2.

#### Un puerto prueba TODOS

La compuerta no es una regla por puerto: es **un solo bloqueo** del adaptador
entero más los permisos espejo del juego activo. Un puerto que nadie pidió y que
queda callado demuestra que ese bloqueo está vivo, y ese bloqueo es el mismo para
todos los puertos que nadie pidió, incluidos los que no conocemos.

Por eso acá **no hay lista de puertos peligrosos que recorrer**. Enumerar
amenazas es una lotería: Parsec, Sunshine y RustDesk escuchan donde el usuario
les diga.

#### Qué cubre EN EXCLUSIVA

Conviene acotarlo con precisión, porque la versión anterior de este párrafo decía
de más. Afirmaba que una compuerta ausente "ya lo caza la auditoría local, y eso
levanta `AlertRulesTampered`". Es cierto solo a medias, y la mitad que falta es
justo la que importa.

Que la compuerta **no esté puesta** sí lo mira la auditoría local en cada
barrido, preguntando por `GateKey`. Lo que hace con el hallazgo no es avisar: lo
**repara en silencio**. `repairOwnRulesLocked` la repone hasta `TamperRepairLimit`
veces, así que `AlertRulesTampered` solo se levanta a la cuarta detección
seguida, o si la reposición falla, o si no hay sala. Las tres primeras veces el
usuario no ve nada, que es lo correcto para el toque puntual de alguien mirando
la consola del firewall, y es cero información para el caso de abajo.

Y hay un hueco que la auditoría local **no puede ver por su forma**: `present()`
comprueba que la CLAVE del filtro exista, no que el filtro CASE. Un filtro vivo
por su GUID con la condición de interfaz vacía lee como `GatePresent` y no
contiene nada. Es el riesgo escrito en `wfp/spec.go`: si la condición de interfaz
llegara vacía al reautorizar un flujo, el bloqueo dejaría de casar en silencio.

Eso es lo que solo el canario ve: **el filtro existe y no contiene**. La
auditoría local sigue diciendo verde y el paquete cruza.

#### El radio de explosión es exactamente lo que se mide

El oyente se liga **solo a la dirección de la sala**, jamás a `0.0.0.0`, y el
adaptador rechaza de plano una dirección sin especificar. Con la compuerta viva
ese socket es inalcanzable para todo el mundo; con la compuerta muerta lo alcanza
la sala, que es lo que se quería averiguar. En ningún caso abre nada en la red de
casa del usuario.

Acepta y cierra sin leer un byte. Por UDP lee un largo fijo y devuelve el eco
solo si el número coincide, así que un datagrama suelto ni se contesta ni cuenta
como toque. Es muchísima menos superficie que el canal de la sala, que sí parsea
mensajes corriendo como SYSTEM.

#### Medido, las dos transiciones y los dos protocolos

Con el droplet marcando a la máquina de desarrollo por Tailscale, el 2026-08-04:

| Momento | TCP | UDP | Lo vio el host |
|---|---|---|---|
| línea base | contesta | contesta | sí |
| **compuerta puesta** | **silencio** | **silencio** | **no** |
| tras purgar | contesta | contesta | sí |

El silencio de la fila del medio es el que vale, porque esta vez había un oyente
detrás. Y UDP es lo nuevo: que la compuerta sea ciega al protocolo se sabía por
leer `SpecsFor`, que emite sus bloqueos sin condición de protocolo. Ahora está
medido.

#### Zero trust: dos fuentes y solo una es un hecho

| | Quién lo vio | ¿Se puede mentir? |
|---|---|---|
| `Touched` | el HOST, en su propio socket | **No.** El paquete cruzó o no cruzó |
| el informe | el MIEMBRO | Sí. Es un mensaje |

El hecho propio gana siempre, y eso resuelve dos de los cuatro casos sin confiar
en nadie:

| El miembro dice | Canario tocado | Conclusión |
|---|---|---|
| "conecté" | sí | Fuga real |
| "conecté" | **no** | Ese miembro **miente**, y queda probado |
| "silencio" | sí | Fuga igual: cruzó algo |
| "silencio" | no | **Sin evidencia.** No se puede afirmar más |

**La fuga se afirma con certeza. Su ausencia no.** Un miembro que simplemente no
mande el paquete y diga "silencio" produce exactamente lo mismo que la compuerta
funcionando, y no hay forma de distinguirlos: un paquete que nunca salió no deja
rastro en ningún sitio. Por eso el estado bueno se llama `CanaryClean`, *sin
evidencia de fuga*, y hay un test que falla si alguien lo renombra a algo que
afirme más.

#### Se le pregunta a TODOS, y esa es la parte que aguanta

Una revisión adversaria del diseño tumbó la alternativa de preguntarle a **uno
solo**: ese miembro sería a la vez el único que marca y el que informa, así que
callándose deja al host sin nada que arbitrar, y el veredicto sale limpio.

Preguntándoles a todos, **para esconder una fuga tendrían que negarse a marcar
todos a la vez**. Basta un miembro honesto: su paquete cruza y el host se entera
por su propia observación. La suposición baja de *"el que me tocó preguntar es
honesto"* a *"al menos uno de N lo es"*.

Importa más de lo que parece porque el vecindario no es de confianza plena: el
código de invitación no es un secreto, no hay baneo y volver a entrar es gratis,
así que alguien podría sostener varias membresías y sesgar un sorteo. Contra
preguntarle a todos, eso no sirve de nada.

Con host y un solo invitado las dos opciones son la misma cosa, y ahí el canario
está en su punto más débil. Es el suelo del mecanismo y hay que saberlo.

#### La vida de una ronda: se abre, se pregunta, se cierra

El canario **no queda encendido**. Se abre para una ronda y se cierra con **lo
primero** de estas tres:

1. **Lo tocan.** Ya hay evidencia y es certeza. Alarma y cerrar: seguir
   escuchando no añade nada.
2. **Contestaron todos los que se preguntó.** No va a llegar nada más.
3. **Vence el plazo de ronda, unos diez segundos.**

Los treinta segundos de `timing.CanaryTTLMax` **no son la espera**: son el tope duro que
cierra el socket incluso si el que lo abrió se murió. Confundirlos deja el
canario abierto la mitad del tiempo. Con las tres condiciones de arriba, una
ronda dura alrededor de un segundo, así que el ciclo de trabajo real ronda el dos
por ciento de un minuto.

**El coste que importa no es el rendimiento.** Un socket ocioso no consume CPU y
cuesta unos kilobytes de kernel; una conexión y un datagrama por miembro por
minuto es ruido de fondo. Lo que se está acortando es **tiempo de superficie**:
un socket que existe es un socket que se puede alcanzar, en un proceso que corre
como SYSTEM.

#### Cuándo corre, y cuándo deja de correr

| Estado | Qué dispara una ronda |
|---|---|
| **Limpio** | Alguien entra · cambia el juego · cada `Sweep` |
| **En alarma** | **Solo después de un `Apply`**, venga del botón o de la operación normal |

Siempre **después** de aplicar las reglas y jamás antes: en un cambio de juego lo
que se añade son permisos nuevos, y los permisos nuevos son justo lo que podría
estar mal, así que comprobar antes mide precisamente lo que no cambió. Visto así,
el disparador se generaliza: **se comprueba después de cada `Apply`**, porque ese
es el único momento en que el estado pudo cambiar a mejor.

Periódico y no solo por evento, que es una corrección de la revisión adversaria:
con disparadores solo por evento, una sala que juega toda la noche al mismo juego
tiene una ronda por invitado, y si esa ronda le tocó a quien se calla no hay
siguiente.

**Con la alarma levantada se corta el periódico**, y la razón es de seguridad
antes que de eficiencia: mientras la protección está caída, el canario **sí es
alcanzable de verdad** por la sala. Es el único momento en que ese socket
significa algo para alguien, así que no se siguen abriendo sockets alcanzables en
una máquina que ya se sabe expuesta.

Se conserva el disparo por `Apply` para no dejar una alarma rancia encendida: la
protección se repone sola en la operación normal, así que puede arreglarse sin
que el usuario toque nada, y una alarma eterna deja de ser información. El botón
de reponer entra por esa misma vía, porque el botón **es** un `Apply`.

```
limpio ──(1ª fuga)──► se REPARA sola, sin avisar
   ▲                        │
   │                        └──(2ª fuga seguida)──► alarma, y se deja de comprobar
   │                                                     │
   └──────────(un Apply y una ronda que MIDIÓ limpia)────┘
```

Con cero miembros no se comprueba, y está bien: solo hay de quién protegerse
cuando hay alguien.

#### Reparar primero, avisar después

`CanaryRepairLimit = 1`. La primera fuga no enciende nada: llama a `applyPolicy`,
que repone la compuerta, y deja que la ronda siguiente juzgue. Solo la segunda
seguida levanta la alarma.

**Lo que se gana es que la reparación deja de depender de que el usuario haga
caso.** Un usuario que ignore el aviso para siempre igual tiene la protección
repuesta. Y cierra un riesgo concreto: el canario es alcanzable por loopback
desde la propia máquina, porque el tráfico a la dirección propia no pasa por
`FWPM_LAYER_ALE_AUTH_RECV_ACCEPT_V4`, que es lo que ya documenta `ErrProbeSelf`.
O sea que un proceso local sin privilegios puede encender la alarma cuando
quiera. **No expone la máquina:** la única acción que la alarma ofrece es
reponer, que solo sube y jamás baja. Lo que ese proceso podría comprar es que el
usuario deje de creerle al aviso, y con la reparación automática dejar de creerle
no cuesta protección.

**El límite es UNO y no tres, al revés que `TamperRepairLimit`.** Las dos
evidencias no valen igual: la auditoría local dice "falta una clave", que sale en
falso por carreras normales, y un toque del canario dice **un paquete cruzó de
verdad, medido desde otra máquina**. Cada ronda cuesta diez segundos con la
compuerta rota, así que tres serían medio minuto de exposición real esperando.

**Lo que dispara la reparación es `Touched` y nada más, y esa es la línea de zero
trust.** Un informe de un invitado no repara: si reparara, cualquier miembro haría
que el host reescribiera su firewall mandando un mensaje. `CanaryMismatch` tampoco.

#### La ronda corre FUERA del despachador

`Supervisor.rondaCanario` la lanza en una goroutine propia, con su propio
`recover`, y el single-flight lo lee y lo escribe **solo el despachador**: se pone
ahí y se quita con un `tagCanaryDone` que vuelve por el mismo canal de trabajo
que todo lo demás.

No es organización, es funcional: una ronda dura hasta diez segundos y el
despachador es de un solo hilo. Corriéndola dentro, el latido de quince segundos
que hace vencer el corte de los veinte minutos se quedaría esperando a la red.
Mismo trato para el lado del invitado, que son hasta seis segundos de sondeo.

**`CanaryReports()` NO entra en `ControlSource`, y es deliberado.** Un informe
solo significa algo dentro de una ronda abierta, correlacionado por puerto.
Drenado por el supervisor habría que aparcarlo en algún sitio hasta que una ronda
lo pidiera, que es un segundo estado con su propia caducidad y su propio bug. La
ronda lee ese canal directo durante sus diez segundos, y fuera de una ronda
`emitir` descarta con aviso: un informe sin ronda abierta es el informe tardío de
un canario ya cerrado.

#### La ronda sabe a qué sala pertenece

Entre soltar el candado y volver a tomarlo pasan hasta diez segundos, y en ese
hueco el host puede salir de la sala y crear otra. La ronda vieja despertaría y
escribiría su conclusión **en la sala nueva**: un verde medido contra otros
miembros y otro conjunto de reglas, con la hora de ahora. La simétrica es peor,
una alarma pegajosa colgada de una sala que ya no existe.

Lo resuelve `RoomState.Gen`, un contador que sube **donde se vacía la sala** y no
en cada llamador, así que los cinco caminos que llegan a `StateIdle` lo heredan
por construcción. La ronda se lleva el `Gen` de cuando arrancó y descarta su
conclusión si no coincide.

#### Dos trampas que hay que respetar

**El puerto del canario no puede caer dentro de un rango permitido.** Los
permisos son por rango y por IP de miembro, así que un puerto efímero que
coincidiera con uno del juego activo sería alcanzable **a propósito** y se leería
como fuga. Es raro y produce la peor clase de fallo, una alarma que grita con
todo bien, así que el puerto se comprueba contra el conjunto vigente antes de
usarlo.

**Un fallo local no es un silencio.** `ProbeFailed` significa que esa máquina no
pudo ni preguntar, y contarlo como silencio sumaría tranquilidad de una
comprobación que no ocurrió. Los dos protocolos en fallo dan `CanaryUnconfirmed`
y jamás `CanaryClean`. Era un defecto real del primer diseño y lo encontró la
revisión leyendo el código.

### internal/fwprobe, para poder medirlo

Corre el firewall compuesto **con el mismo código que el daemon**, sin una sola llamada propia al sistema. Vive en `internal/` para que el producto no lo importe y el instalador no lo distribuya.

Es hermano del spike que decidió el diseño, con una diferencia que es todo el punto: aquel tenía llamadas propias a WFP y este no tiene ninguna. Si acá hiciera falta un atajo, sería la señal de que al adaptador le falta algo.

Poner y quitar son subcomandos separados, y entre medias se mide desde la otra máquina. Un estado suelto no prueba nada: si el firewall ya estaba abierto de una corrida anterior, "conecta" no dice quién lo abrió. Lo que se mide es la transición.

La primera corrida encontró dos caídas que ningún test del repo podía encontrar, porque las dos viven dentro de una llamada COM: `INetFwRule::Interfaces` devuelve un array de VARIANT y leerlo como array de BSTR mata el proceso, y `IEnumVARIANT::Next` termina cada enumeración con `S_FALSE`, que go-ole convierte en error. Con la segunda, todos los caminos del adaptador de permisos fallaban siempre.

El verbo `probe` tenía un `DialTimeout` propio, y era exactamente el error que este binario existe para no cometer: medía otra cosa que la que corre el daemon, así que su verde no valía. Ahora usa `adapter/probe`, con el plazo del producto y sin bandera para cambiarlo, porque medir con otro plazo es medir otra cosa.

### adapter/firewall/windows/netfw, la capa que ABRE

Implementa la mitad de permisos que el adaptador compuesto usa. **No implementa `FirewallPort` por sí sola**, y esa distinción se hizo explícita: contener necesita las dos capas, y esta no puede acotar la compuerta.

- API COM `INetFwPolicy2`, nunca `netsh`: más rápida y sin dependencia del idioma del sistema.
- **La regla de la puerta va en el adaptador del VESTÍBULO**, y se decide por la dirección local de la regla, igual que en la compuerta. Anclando todos los permisos al adaptador de la sala, esa regla quedaba acotada a una interfaz donde su propia dirección local no vive, o sea que no casaba con nada: la puerta cerrada en la capa de Windows, con todo reportando verde. Se vio volcando lo que Windows guarda de verdad, no lo que se le pidió guardar.
- **Windows no guarda lo que se le escribe, guarda un equivalente**, y devuelve el equivalente al leer. Medido: `100.127.255.1` vuelve como `100.127.255.1/255.255.255.255`, `100.127.255.0/24` vuelve como `100.127.255.0/255.255.255.0`, y una cadena vacía vuelve como `*`. Compararlas crudas hacía que toda regla pareciera alterada para siempre, con dos consecuencias reales: la cuarentena de base avisaba de deriva en sus 48 reglas en cada arranque, que es la forma más segura de garantizar que nadie lea el log; y `Apply` retiraba y reescribía cada regla en cada latido, reabriendo con temporizador la misma ventana que el código evita a propósito. La comparación normaliza las dos partes, y lo que de verdad cambió se sigue viendo.
- Todas las reglas llevan `Grouping = "Kanpachi"`. Al arrancar el servicio: purgar todo lo etiquetado, luego aplicar el estado deseado. Una muerte sucia del daemon nunca deja puertos huérfanos abiertos.
- **Alcance por adaptador Y por dirección.** Acá decía que la API de firewall de Windows no filtra por nombre de interfaz. Es falso: `INetFwRule` tiene la propiedad `Interfaces`, y la propia documentación dice *"the interfaces in the list are represented by their friendly name"*. Comprobado sobre el sistema, no leído: hay una regla viva de Microsoft acotada a un adaptador virtual, que es exactamente el caso de Kanpachi.

  ```
  HNS Container Networking - DNS (UDP-In)
  Interfaces : vEthernet (WSL (Hyper-V firewall))
  ```

  Así que cada regla de permiso lleva `Interfaces = ["kanpachi0"]`, más `LocalAddresses` = IP del adaptador y `RemoteAddresses` = IPs de los miembros presentes. Los tres a la vez, porque cada uno falla de una forma distinta y el solapamiento es la defensa.

  Por dentro Windows lo guarda como GUID del adaptador (`IF={...}` en el almacén de reglas) y lo devuelve resuelto a nombre. De ahí salen las dos propiedades que importan: sobrevive a que el usuario renombre la conexión, y **no** sobrevive a que el adaptador se recree con un GUID nuevo. Lo segundo es justo lo que hace `Apply` al reaplicar, que enumera lo vivo y calcula la diferencia.

  **El alcance por interfaz va SOLO en los permisos, jamás en los bloqueos de `Kanpachi-base`.** No es simetría estética: si el alcance deja de casar, un permiso que deja de aplicar CIERRA y un bloqueo que deja de aplicar ABRE. La cuarentena de base se acota por dirección y nada más.

  Esto importa de verdad por el direccionamiento: Kanpachi usa `100.64.0.0/10`, que es espacio CGNAT, y la decisión 10 ya anota que CGNAT domina en LatAm. Con acotar solo por IP, un router 4G que reparta `100.64.x` en la LAN de casa haría que el permiso del juego alcance también a la red física.
- Reglas aplicadas a los tres perfiles de firewall (dominio, privado, público) y el adaptador fijado como red **Privada** desde el instalador. Si Windows clasificara el adaptador en otro perfil, las reglas seguirían aplicando.
- **Auditoría de reglas ajenas.** Al activar un perfil, `netfw` busca reglas de entrada permisivas para el ejecutable del juego en los perfiles Privado y Público, creadas por el instalador del juego o por un diálogo previo de Windows. Esas reglas dejan al host alcanzable desde su LAN doméstica, fuera del control de Kanpachi.

  Aclaración importante: esta consulta va contra el **almacén de reglas del Firewall de Windows**, buscando por ruta de ejecutable. No enumera procesos, no detecta si el juego está corriendo y no le importa. La regla existe en disco haya o no partida.

```go
type ForeignRule struct {
    Name       string
    Executable string
    Profiles   []FirewallProfile
    WasEnabled bool          // estado previo, para restaurar
}

func (f *FW) AuditForeign(p GameProfile) ([]ForeignRule, error)
func (f *FW) SuspendForeign(rules []ForeignRule) error
func (f *FW) RestoreForeign() error
```

Reglas de manejo: nunca se borran, solo se desactivan. El estado previo se persiste en `ProgramData\Kanpachi\suspended-rules.json` antes de tocar nada. Se restauran al salir de la sala y también al arrancar el servicio, por si una salida sucia dejó algo suspendido. Siempre con confirmación explícita del usuario en la UI, jamás automático.

**Se clasifica por ejecutable Y por ALCANCE, y el alcance gana.** Las dos primeras clases se deciden mirando a qué programa apunta la regla, y hay un caso que por ahí no se ve nunca: un permiso puede no tener ejecutable. La regla que motivó esto abría cualquier protocolo sobre `kanpachi0`, sin puerto, sin origen y sin aplicación, así que caía en "otra" y no se reportaba jamás. Ahora, un permiso entrante habilitado y ajeno que esté acotado a una interfaz `kanpachi*` es `ClassOnOurAdapter`, y **bloquea igual que el control remoto**: lo que lo hace peligroso no es de quién es, es dónde está. Deshace la promesa central en la misma capa que Kanpachi usa para conceder, y la compuerta no lo tapa, porque los dos son permisos y conviven.

Es la red permanente del fork. El fork quitó de raíz las reglas que EasyTier escribía; esto es lo que las hace visibles si alguien las vuelve a poner, sea quien sea. Y es el único sitio del proyecto donde el nombre se compara **por prefijo** en vez de por igualdad, con el razonamiento invertido a propósito: acá se decide qué REPORTAR y nunca qué borrar, así que un adaptador de más es una fila en una pantalla, y uno de menos es un permiso que nadie ve sobre una red que el producto dice contener.

### adapter/netcfg/windows, implementa `NetConfigPort`

El componente que no existía en la primera versión de este documento, y sin el cual el producto no funciona de forma confiable.

**El problema:** Windows revierte la métrica del adaptador, la categoría de red y las rutas en cada **evento de identificación de red**. Aplicar todo esto una vez durante la instalación no alcanza. El evento se dispara al agregar o quitar una IP, al conectar o desconectar un adaptador, al habilitarlo o deshabilitarlo, y en eventos de DHCP.

**La solución:** el supervisor se suscribe al canal `Microsoft-Windows-NetworkProfile/Operational`, **Event ID 10000**, y reaplica el estado deseado cada vez que Windows identifica una red.

Lo que `netcfg` mantiene:

| Ajuste | Valor | Por qué |
|---|---|---|
| Métrica IPv4 de `kanpachi0` | `1` | Que los juegos prefieran la red virtual sobre la LAN o el WiFi |
| Métrica IPv6 de `kanpachi0` | `20` | Deprioritizada, para que no compita con el IPv6 nativo |
| `AutomaticMetric` | desactivado en ambas pilas | Windows la recalcula por velocidad de enlace en cada reconexión |
| Categoría de red | Privada, más `Category=1` escrito en `HKLM\SOFTWARE\Microsoft\Windows NT\CurrentVersion\NetworkList\Profiles\{guid}` | El valor en disco es el que el servicio NLA lee de vuelta en la siguiente reconexión |
| Ruta de broadcast | `255.255.255.255/32` sobre `kanpachi0` | Solo si el perfil del juego lo pide |
| Ruta de multicast | `224.0.0.0/4` sobre `kanpachi0` | Solo si el perfil del juego lo pide (mDNS, SSDP, buscadores de servidores) |
| Política de prefijo | `::ffff:0:0/96 100 4` | Solo si el perfil lo pide. RFC 6724, hace que IPv4 gane en la resolución de nombres de doble pila. No desactiva IPv6 ni toca el transporte del motor |
| Ruta por defecto | eliminada si aparece | Kanpachi nunca enruta internet. Ni `0.0.0.0/0` ni `::/0` sobre `kanpachi0` |
| MTU | sondeado, ver abajo | Evita el agujero negro de PMTUD |

**Todo lo que sea por juego se revierte al salir de la sala.** Las rutas de broadcast, la política de prefijo y DirectPlay se aplican porque un perfil los pidió, y se deshacen cuando ese perfil deja de estar activo. El estado previo se persiste igual que las reglas de firewall ajenas, para poder restaurar tras una salida sucia.

**La realidad de la red no identificada.** Un adaptador sin puerta de enlace queda como "Red no identificada" y Windows lo mete en el perfil Público, porque NLA usa la MAC de la puerta de enlace para identificar una red. Kanpachi intenta fijarlo en Privada, y **no depende de lograrlo**: por eso todas las reglas se aplican a los tres perfiles. La alternativa que usa mucha gente, la directiva de grupo que trata toda red no identificada como privada, queda descartada: afecta a cualquier red que NLA no logre identificar, incluida la principal del usuario, y eso es debilitar su firewall entero para arreglar el nuestro.

**MTU.** WireGuard usa 1420 por defecto sobre un camino de 1500. Enlaces PPPoE dan 1492, móvil y 5G suelen dar menos, e IPv6 exige mínimo 1280. El síntoma clásico es cruel para un juego: el túnel levanta, el ping anda, la partida conecta, y el mundo no termina de cargar, porque los paquetes chicos pasan y los grandes desaparecen en silencio cuando el ICMP está filtrado. `netcfg` sondea el camino con ping de no fragmentar antes de fijar el valor, y `Diagnostics` reporta el MTU efectivo.

El número del túnel lo decide `domain.TunnelMTU`, y **no sale de nuestra cosecha**: son 140 bytes menos que el camino, acotado a `[1280, 1380]`. Esos 140 son la aritmética del propio motor, que arranca de 1380 y le resta 20 más con el cifrado encendido, que en Kanpachi lo está siempre. Sobre un camino de 1500 da 1360, exactamente lo que el motor habría escrito solo, así que los dos coinciden en el caso común y no se pelean por la interfaz en cada reaplicado. Sondear solo cambia algo cuando el camino de verdad es más chico, que es para lo que existe. Medido: `MTU 1360` sobre el adaptador con una sala abierta.

**A quién se le sondea, y qué no cubre.** A la puerta de enlace por defecto, o sea al router del usuario. Dos razones: no hace falta contactar con nadie de fuera, lo que encaja con que este producto no habla con terceros; y ahí está el caso dominante, el enlace PPPoE que da 1492. **No cubre un estrechamiento a mitad de camino**, más allá del primer salto: para eso habría que marcar al otro extremo, y el otro extremo no existe todavía cuando esto corre. El margen del túnel deja el resultado del lado seguro.

**La categoría de red no está implementada, y se dice en vez de fingirla.** `setPrivateCategory` devuelve error a propósito. Nada de la contención depende de ella: las reglas se aplican a los tres perfiles justamente porque un adaptador sin puerta de enlace se queda en "Red no identificada" y Windows lo mete en Público. Devolver `nil` habría dejado un log que dice "hecho" sobre algo que nadie hizo.

**El libro de ajustes.** De todo lo que `netcfg` toca, casi nada necesita recordarse: el adaptador, su dirección, su métrica, su MTU y sus rutas mueren con la red virtual, que el motor crea por sala. Sobreviven exactamente dos, la política de prefijo IPv6 y DirectPlay, que son ajustes de la máquina entera. Esos dos van a `applied-tweaks.json` en ProgramData **con el valor que había antes**, no con lo que se hizo: "encendimos DirectPlay" no dice si hay que apagarlo, y si el usuario ya lo tenía puesto, apagarlo al salir rompería algo suyo. `RevertTweaks` lee el libro y no la memoria, que es lo que hace que funcione tras una muerte sucia, en el arranque siguiente.

**Preguntar por DirectPlay cuesta segundos**, porque es DISM. Por eso hay un atajo que además es lo correcto: si se pide apagarlo y el libro dice que nadie lo encendió, no hay nada que hacer y no se pregunta. Sin ese atajo, crear una sala sin juego pagaba una consulta a la instalación de características de Windows, y la API local se pasaba de plazo con la red ya levantada. Medido.

**Lo que una sala ejercita, y lo que no.** Abrir una sala prueba la métrica, el MTU y el barrido de rutas, y nada más: las dos rutas por juego solo las pide un perfil, la política de prefijo también, y el borrado de una ruta por defecto solo corre cuando hay una que borrar, cosa que en una máquina sana no pasa nunca. O sea que la mitad de este adaptador podía estar escrita, revisada y en verde sin haberse ejecutado jamás.

Eso lo cubre `internal/netcfgprobe`, un arnés que corre con una sala abierta: pone y quita las dos rutas en pasos separados, fabrica una ruta por defecto con `route.exe` y comprueba que `netcfg` la borra, y sondea el MTU. Verifica preguntándole **al sistema** y nunca a la memoria de `netcfg`, porque un `Apply` que no escribió nada y se lo apuntó igual pasaría en verde. La ruta de mentira lleva métrica altísima a propósito: mientras existe no le puede ganar a ninguna ruta real del usuario.

Ya encontró un fallo que ningún test de paquete podía ver: la fila de una ruta se construía sin pasar por `InitializeIpForwardEntry`, así que `ValidLifetime` y `PreferredLifetime` quedaban en cero y **la ruta nacía caducada**. La llamada devolvía éxito. Medido en verde tras el arreglo, con las cuatro transiciones.

**Conflicto de rango CGNAT.** Ver la sección de direccionamiento más abajo.

### adapter/routes, implementa `RoutingTable`

Contesta una sola pregunta, y de ella depende que se pueda abrir una sala: en qué rangos vive ya esta máquina. `PlanAddresses` la usa para elegir un `/24` que no pise la red de casa del usuario.

Lee **las dos cosas**, las direcciones de todos los adaptadores con `GetAdaptersAddresses` y los destinos de la tabla de rutas con `GetIpForwardTable2`. Solo las direcciones se perderían una ruta estática hacia una red alcanzable por la puerta de enlace, que es un rango que la sala igual tiene que esquivar.

El error caro de esta capa no es un prefijo de menos, es uno de más:

| Se descarta | Por qué |
|---|---|
| `0.0.0.0/0` y `::/0` | **La que decide.** La ruta por defecto está en toda tabla de rutas y se solapa con TODO, así que colarla hace que el planificador concluya que los dos espacios están enteros ocupados y que no se pueda abrir ninguna sala en ninguna parte |
| `kanpachi0` y `kanpachi1` | Una sala abierta vetaría a la siguiente, y reabrir una sala tras un mal cierre rechazaría justo la subred que intenta restaurar |
| loopback, link-local, multicast | `169.254.0.0/16` aparece en cualquier adaptador que no consiguió dirección, y no significa que nada esté ocupado |

El resultado va enmascarado, sin repetidos y ordenado, para que dos llamadas con la misma máquina den lo mismo y la subred de una sala no baile entre ellas.

La mitad que decide es Go puro y la prueba el job de Linux. El fichero con `//go:build windows` solo lee el sistema. El de `!windows` devuelve **error** y jamás la lista vacía: sin prefijos locales el planificador concluiría que nada choca con la red de casa y elegiría un rango que sí choca.

### adapter/engine/kanpachi, implementa `EnginePort`

**El único sitio del proyecto que menciona el motor.** Lo ejecuta como **proceso hijo**, no vinculado al binario Go. Razones en `02`, decisión 1: el motor es Rust y LGPL-3.0, así que un proceso separado mantiene la licencia de Kanpachi libre, evita cgo y aísla los fallos. Lo último dejó de ser un argumento y pasó a ser un hecho medido: el workspace del motor compila su perfil de release con `panic = "abort"`, o sea que un `panic` dentro del mismo proceso se llevaría el servicio sin que nada pueda atajarlo.

**El hijo NO es `easytier-core.exe`.** Es `kanpachi-engine.exe`, un binario propio que usa EasyTier como **librería** y vive en su propio repositorio bajo LGPL-3.0. La diferencia no es de gusto, y se midió con el mismo fichero de configuración sobre la misma máquina:

| Proceso | Sockets en escucha |
|---|---|
| `easytier-core.exe` v2.6.4 | `TCP 0.0.0.0:15888 LISTENING` |
| El motor propio, sobre la librería | ninguno |

Ese `15888` es el portal de administración del motor, **no tiene autenticación de ninguna clase**, y su valor por defecto escucha en todas las interfaces pese a que la ayuda oficial dice `localhost`. Por ahí cualquier proceso local emite credenciales de la red real, agrega nodos y pide el `network_secret` en claro. El portal se construye en un solo sitio del árbol de EasyTier, dentro de su binario de línea de comandos, y el arranque de red por librería no lo menciona: **desaparece por omisión, no por configuración.**

#### La librería es un FORK, y por qué hizo falta

Hay dos clases de cosa que hace el binario oficial, y solo una desaparece al escribir el nuestro. Lo que el CLI hace **alrededor** de la librería se va por omisión, y ese es el caso del portal. Lo que la librería hace **dentro** de lo que se le pide sigue pasando, porque es su código.

Medido en una máquina de verdad: al crear el adaptador virtual, EasyTier escribe ocho reglas de PERMISO en el Firewall de Windows, por COM, desde dentro de `NetworkInstance::start()`, o sea también por el camino de librería.

| Regla | Alcance |
|---|---|
| `EasyTier kanpachi0 - ALL Protocol (Inbound)` | permitir CUALQUIER protocolo sobre el adaptador virtual, sin puerto y sin origen |
| `EasyTier <ruta del exe> (Inbound)` | permitir CUALQUIER protocolo hacia el motor en **todas** las interfaces de la máquina, la red de casa incluida |

Son permanentes: sobreviven al reinicio y a desinstalar Kanpachi.

La primera fila deshace la promesa central en la misma capa que Kanpachi usa para conceder. La segunda es peor y es la que decide el enfoque: **la compuerta de WFP no puede taparla**, porque va acotada al adaptador virtual, y esa acotación es la invariante que impide que un bloqueo duro deje al usuario sin la entrada de su red de casa. Una capa que jamás debe salirse del adaptador no puede cubrir una regla que aplica en todos.

No hay feature de cargo, campo de configuración ni variable de entorno que las apague, así que la salida es una sola: el motor enlaza un fork nuestro de EasyTier con esas dos llamadas borradas. Quitarlas es seguro porque upstream ya trataba su fallo como no fatal, con un aviso y seguir. **A qué referencia apunta y qué más lleva dentro se dice en la decisión 1 de `02-decisiones-de-diseno.md`, y en ningún otro sitio**, porque este valor ya se desincronizó una vez estando escrito en cinco.

El diff se lee de un vistazo a propósito, y por eso el motor vive en su repo y no dentro del fork: la afirmación "es upstream y esta lista corta" tiene que poder comprobarse en treinta segundos con un `git diff`, que está escrito en la decisión 1, y un fork con dos mil líneas nuestras dentro la convierte en un acto de fe.

Lo que el fork NO reemplaza es la compuerta. Su enemigo nunca fue EasyTier: son las reglas permisivas **ajenas**, de escritorio remoto y de instaladores de juegos, que alcanzan al usuario por la red virtual. Eso no lo quita ningún fork.

#### Antes de lanzarlo: comprobar que esta máquina pueda

El adaptador virtual lo crea `wintun.dll`, que instala un driver en el almacén de drivers de Windows. Ese paso puede negarse, y cuando se niega no hay adaptador, no hay red virtual y no hay sala posible en esa máquina.

**El daemon lo comprueba al arrancar, con `CheckMachine`, antes de que nadie elija un juego.** Tres pasos, y solo el tercero contesta la pregunta:

| Paso | Qué atrapa |
|---|---|
| `wintun.dll` y `Packet.dll` al lado del motor | un antivirus que borró un fichero, una copia incompleta |
| `LoadLibrary` de la DLL y sus dos funciones | arquitectura equivocada, DLL dañada, dependencia ausente |
| **crear un adaptador desechable y cerrarlo** | el almacén de drivers, que es el que falla de verdad |

**Comprobar que los ficheros están NO alcanza, y esto se midió.** El 2026-08-11, la máquina de un invitado tenía los tres ficheros en su sitio, wintun cargó y llegó a `Installing driver 0.14`. Lo que falló fue guardar el `.inf` en el almacén, con `0x000000CB`, `ERROR_ENVVAR_NOT_FOUND`. Un chequeo de presencia habría dado verde y esa persona habría seguido sin poder entrar. wintun no expone «¿se podría instalar?», expone `WintunCreateAdapter`, así que la única forma de saberlo es intentarlo.

Coste medido en esta máquina, elevado: 678 ms y 952 ms en dos corridas seguidas, con cero adaptadores dejados atrás según `Get-NetAdapter -IncludeHidden`. Se paga una vez por arranque del daemon y el resultado queda cacheado, así que la primera sala no lo vuelve a pagar. De paso deja el driver instalado antes de que haga falta.

**Se engancha `WintunSetLogger`**, y esa es la mitad del valor. Sin ese callback, las líneas propias de wintun no existen en ningún sitio: no van a un fichero, no van a stderr, no van al visor de eventos. Con él, el texto que nombra el fichero y la operación que lo rechazó entra en el mensaje que ve la persona.

El consejo se bifurca por código, porque hay dos fallos medidos que quieren cosas opuestas. `0x05`, acceso denegado, es permisos y reiniciar no arregla nada. `0xCB` es el almacén de drivers, y lo que hay que mirar es `DevicePath`, que tiene que ser `REG_EXPAND_SZ` con valor `%SystemRoot%\inf`. Cualquier otro código cae en el consejo general, que empieza por reiniciar porque se lleva por delante un dispositivo a medio crear.

Cuando falla, el daemon lo enseña con `uihost.Stop`, que **espera** a que alguien pulse Aceptar con un plazo de cinco minutos, y recién entonces apaga Kanpachi. La espera es el punto: un cuadro que aparece en el mismo instante en que todo se cierra se lee como que reventó, no como una explicación. Sin interfaz que hospedar el gancho es nulo, y el error vuelve por el canal: en un servidor no hay a quién enseñárselo, y apagar el servicio por eso sería quitarle la máquina a quien lo administra.

El arranque **no se aborta** desde ahí. Abortar mataría el canal por el que `kanpachi doctor` y la ventana explican qué pasó, justo cuando hace falta explicarlo.

`kanpachi doctor` corre exactamente la misma función, y ahí sí se rinde sin elevación: crear un adaptador es privilegiado, así que sin permisos contesta que no lo puede saber en vez de un rojo que mandaría a buscar un problema inexistente. Es el análogo en Windows de la comprobación de `/dev/net/tun` que doctor hace en Linux. **En Linux esto no existe** y devolver nulo es la respuesta correcta: el nodo de TUN lo pone el kernel, no hay almacén, no hay `.inf` y no hay instalación que pueda fallar a medias.

Responsabilidades:

- Traducir `domain.HostSpec`, `domain.RendezvousSpec` y `domain.GuestSpec` a órdenes del motor, que viajan por **el tubo de entrada del proceso hijo** y jamás por la línea de comandos. Medido: el `CommandLine` del proceso hijo solo muestra la ruta del ejecutable, así que el secreto de la red deja de ser legible con el Administrador de tareas por cualquier usuario de la máquina.

  **El tubo queda abierto y lleva mensajes JSON, uno por línea, en las dos direcciones.** Este documento decía "TOML por stdin", y eso solo describía el arranque: `EnginePort` tiene trece métodos y once ocurren después, con la sala ya abierta. Emitir una credencial cuando alguien toca la puerta no cabe en un fichero de configuración. El TOML sigue existiendo como detalle interno del motor, que es quien construye su propia configuración.

  **Ese tubo es una tubería anónima, y ahí está la propiedad que el binario oficial no tiene.** No tiene nombre, no tiene ruta y no tiene dirección: no es que conectarse esté prohibido, es que la operación no existe. Los dos extremos viven como handles dentro del daemon y del motor. Un puerto o una named pipe serían puertas, y una puerta necesita cerradura, y una cerradura se puede escribir mal, que es exactamente lo que le pasó al 15888. Acá no hay autenticación que escribir porque el canal ES la identidad.

  **Regla dura: stdin es la única entrada de órdenes del motor.** Jamás un puerto, ni una named pipe, ni un fichero vigilado, ni una señal. El binario además no acepta argumentos. Lo vigila `internal/arch/motor_test.go`, que prohíbe construir cualquier escucha dentro del paquete del adaptador.

  Se distinguen tres tipos de mensaje y ninguno más: una orden lleva `id`, su respuesta lleva el mismo `id`, y un evento no lleva ninguno. Esa ausencia es lo que los separa, sin adivinar nada por el contenido. La decodificación es **estricta de los dos lados**: un campo desconocido rechaza el mensaje entero.

  Los cinco eventos son los de `domain.EngineEventKind`, y uno de ellos no viene del motor: `EngineDied` lo levanta el adaptador cuando el proceso hijo termina, porque un motor muerto no puede avisar de su propia muerte.

  El entorno del hijo se arma **explícito**, nunca heredado. Cada bandera de EasyTier tiene una gemela por variable de entorno, `ET_CONFIG_SERVER` y `ET_PORT_FORWARD` entre ellas, así que un hijo que hereda el entorno acepta capacidades prohibidas sin que nadie las escriba en el argv. Lo vigila `internal/arch/motor_test.go`.

  **No traduce el descubrimiento LAN, porque no llega hasta acá.** `HostSpec` y `GuestSpec` no tienen campo para él: encenderlo significa `--enable-udp-broadcast-relay`, o sea capturar el tráfico de la red de casa del usuario con un driver de captura de paquetes, y la decisión 1 lo difiere hasta que exista un juego que lo pida. El perfil sí declara `lan_discovery`, porque el catálogo es la capa de conocimiento; esta es la capa que decide qué se concede.
- Ciclo de vida del hijo: arranque, supervisión, apagado limpio, y matar huérfanos al arrancar el servicio por si una salida sucia dejó uno vivo. Los huérfanos se comparan por **ruta completa y jamás por nombre**: esto corre como SYSTEM, y matar cualquier proceso llamado `kanpachi-engine.exe` sería matar el de otra instalación.

  El hijo va dentro de un **Job Object con `KILL_ON_JOB_CLOSE`**, que es lo único que garantiza que muera con un daemon que muere de forma sucia, sin correr ningún `defer`. Sin eso queda un motor vivo con la red arriba y el firewall ya purgado, o sea la red virtual abierta sin nada conteniéndola.

  **No hace falta acotar qué handles hereda, porque Go ya lo hace.** Verificado en el fuente instalado, `syscall/exec_windows.go`: `StartProcess` arma `PROC_THREAD_ATTRIBUTE_HANDLE_LIST` con exactamente los tres estándar más `AdditionalInheritedHandles`, con el comentario *"Do not accidentally inherit more than these handles"*. El motor no recibe por herencia la sesión de WFP ni el pipe de la UI. La única consecuencia práctica: `AdditionalInheritedHandles` se deja vacío, siempre.

  **Packet.dll tiene que estar al lado del ejecutable.** Es una importación DURA del motor propio, medida con `dumpbin /imports` sobre el binario construido, sin sección de delay import. Sin ella el proceso no llega ni a arrancar, y Windows solo dice `0xC0000135` sin nombrar cuál falta. Por eso el directorio de trabajo del hijo es el del binario.

- **Dos adaptadores virtuales, no uno.** El host está en dos redes a la vez, la sala y el vestíbulo, y cada red del motor trae el suyo: `kanpachi0` y `kanpachi1`. Los nombra el DAEMON y no el motor, y esa dirección importa: la compuerta de WFP se acota a un adaptador por nombre, así que un motor que eligiera el suyo podría devolver un adaptador que la compuerta no cubre.
- Consultar estado y traducirlo a `[]domain.Peer` y `domain.NetCheck`. La salida del motor ya distingue conexión directa de relay y reporta el tipo de NAT, que es exactamente lo que la UI pinta en verde o ámbar.

  **Reiniciar devuelve LAS DOS redes, y espera a que las dos tengan dirección.** El motor guardaba una sola orden de arranque, así que la última pisaba a la anterior. Medido con el producto entero: al matar el motor a lo bruto con una sala de host abierta volvía `kanpachi0` y no volvía `kanpachi1`, y a partir de ahí la regla de la puerta no se podía escribir porque su adaptador ya no existía. La sala seguía en pie con la puerta cerrada para siempre. El comentario que lo justificaba decía que el vestíbulo es un paso de paso, y eso es cierto para el INVITADO, que lo suelta al entrar, y falso para el host: es su puerta y dura lo que dura la sala. Lo que lo resuelve sin una regla por rol es que soltar el vestíbulo lo OLVIDE, así que se repite exactamente mientras siga levantado.

  Que falle el vestíbulo no tumba el reinicio: la sala ya está arriba y la gente que está dentro sigue jugando, y lo que se pierde es que entre alguien nuevo. Queda en el log.

  **Miembro es quien tiene dirección DENTRO de la sala, y el seed no lo es.** El motor ve la tabla de rutas de la red virtual, y ahí aparece también el nodo público que releva: releva para la sala sin vivir en su espacio de direcciones, así que vuelve sin IP virtual. Reportarlo ponía en la pantalla de todo el mundo un miembro sin nombre llamado `invalid IP`, y le entregaba al daemon un miembro sobre el que abrir reglas de firewall sin dirección con la que abrirlas. El motor ya no lo manda, y el daemon rechaza una dirección vacía igual que una inválida, porque todo lo que se hace con un miembro se hace por su IP.

  Ese es el mismo camino por el que apareció el otro fallo de esta lista: el motor mandaba su propia dirección **con el prefijo** (`10.99.61.1/24`), el daemon la parseaba estricto, y la consulta de miembros fallaba en cada evento. La sala se sostenía igual, así que ninguna medición anterior lo vio: lo que se caía era saber quién estaba dentro. Las dos veces, el parseo estricto del daemon es lo que lo hizo visible, y por eso no se relajó.

El binario del motor se distribuye dentro de `Program Files\Kanpachi\`, y el día que haya firma de código son dos binarios que firmar, no uno.

### adapter/library/steam, implementa `GameLibrary`

`SteamPath` en el registro, `libraryfolders.vdf` para enumerar bibliotecas, `appmanifest_*.acf` cruzados por AppID contra el catálogo.

El resultado **ordena** la lista de juegos, nunca la filtra: la biblioteca completa del catálogo siempre está disponible y elegir un juego no detectado funciona igual. La detección es falible por diseño (juegos fuera de Steam, otras tiendas, unidades ilegibles) y ninguna de esas fallas puede bloquear la creación de una sala. Un error de este adaptador devuelve lista vacía, jamás rompe el arranque.

### adapter/inspect/iphlpapi, implementa `SocketInspector`

Consulta puntual a `GetExtendedUdpTable` y `GetExtendedTcpTable` pidiendo el PID dueño. **Solo lo usa el creador de perfiles**, disparado por un botón del usuario. Fuera de ese asistente nunca se invoca. Ver `06-catalogo.md`.

### transport/control, el canal de la sala

Implementa la decisión 23. **Solo escucha en la máquina del host.** Los invitados únicamente marcan hacia afuera y nunca abren un puerto.

Escucha en **dos direcciones con dos alcances distintos**, y separarlas es lo que impide regalar el canal de la sala a cualquiera que tenga el código:

| Dirección | Quién puede hablar | Qué se admite |
|---|---|---|
| **La puerta**, la IP del host en el vestíbulo | Cualquiera que haya llegado ahí, o sea cualquiera con el invite ID | Únicamente un pedido de credencial. Cualquier otro mensaje se descarta sin interpretarse |
| **La sala**, la IP del host en `kanpachi0` | Solo las IPs de los miembros presentes | Todo lo demás: expulsión, cierre de sala, y por su sola existencia la presencia del host |

La puerta tiene que estar abierta a desconocidos por definición: quien está entrando todavía no es miembro, y lo que viene a pedir es justamente el permiso para serlo. Lo que la acota no es una lista de direcciones, es que ahí no se puede pedir nada más.

**Expulsar recorta la sala y no la puerta.** Es deliberado y es la decisión 22: expulsar y bloquear son cosas distintas, y quien fue expulsado puede volver a tocar la puerta hasta que el host renueve el código.

#### El puerto, y el hueco que hay que abrirle

Escucha en **TCP 57623**, en la interfaz virtual y en ninguna otra. Fijo y no negociado, por lo mismo que el `/24` del vestíbulo: quien entra tiene que llegar sin haber hablado antes con nadie, y el canal por el que se negociaría un puerto es justamente el que se está montando.

La interfaz nace sin ninguna regla de permiso, así que **el host tiene que abrirle un hueco a su propia puerta**. Ese hueco es la única regla que Kanpachi crea sin que ningún perfil la pida, va en el mismo conjunto declarativo que las de juego, y se describe en la decisión 4. Calcularlo aparte de `BuildRuleSet` no es organización: aquella devuelve vacío cuando no hay juego activo, que es el estado normal de una sala recién creada.

#### La tabla de mensajes, cerrada y por dirección

Son cuatro listas de admisión y no una, porque hay dos oyentes con dos modelos de confianza y cada uno tiene su dirección:

| Mensaje | De quién a quién | Admitido en |
|---|---|---|
| `credential_request` | invitado → host | **Solo la puerta** |
| `credential_response` | host → invitado | Solo la puerta |
| `announce` | host → invitado | Solo la sala |
| `notice` | host → invitado | Solo la sala |
| `code` | host → invitado | Solo la sala |
| `ack` | invitado → host | Solo la sala |

Un `credential_request` que llega por la sala se descarta sin interpretarse, y un `notice` que llega por la puerta también. **Un host no toma anuncios ni avisos de nadie:** aceptarlos le permitiría a un miembro modificado cambiarle el juego activo justo a la máquina donde se abren los puertos.

Los tipos viajan como cadenas y no como el número del iota, por lo mismo que en la API local: agregar un aviso en medio del bloque le cambiaría el significado a todos los de abajo en una versión ya instalada del otro lado.

#### Los topes, y qué evita cada uno

| Tope | Valor | Qué pasa sin él |
|---|---|---|
| Tamaño de mensaje, antes de deserializar | 8 KiB | Se paga el coste de parsear lo que se iba a rechazar. Pasarse cierra esa conexión, porque con líneas lo que no cupo deja el flujo desincronizado |
| Conexiones simultáneas en la puerta | 16 | La puerta acepta desconocidos por definición, o sea que es la superficie que alguien con el código puede intentar agotar |
| Plazo para hablar en la puerta | 5 s | Una conexión que llega y calla ocupa un hueco de los dieciséis para siempre |
| Conexiones vivas por IP virtual, en la sala | 1 | Un miembro acumula descriptores contra el proceso que corre como SYSTEM. La segunda desplaza a la primera, que además es lo que hace que reconectar funcione |
| Espera del acuse de un aviso | 1 s | Sin espera queda una ventana real; sin tope, la expulsión se vuelve cooperativa |
| Plazo de escritura | 2 s | **Un miembro que abre la conexión y deja de leer traba la sesión entera**, que llama a esto con su candado tomado, sin haber mandado un solo mensaje inválido. Vencido, esa conexión se cierra |

Ese último salió de escribir el paquete y no de leerlo: el primer intento sostenía el candado de la conexión mientras la red bloqueaba, así que cerrarla necesitaba el mismo candado que había que liberar. Con sockets de verdad el síntoma aparece solo cuando alguien deja de recibir.

#### La libreta de llaves de sesión

Al emitir una credencial, el host guarda la llave pública que vino en el pedido **contra la dirección que él mismo acaba de asignar**, no contra nada que el otro lado haya elegido. Es con la que después sella el código nuevo si lo renueva.

Vive en memoria, muere con la sala y no toca el disco. No es identidad persistida y no habilita ningún baneo, que es la condición bajo la que existe.

Lo que el sellado compra y lo que no está en la decisión 23 y en el modelo de amenazas: confidencialidad frente a un peer que relaye los bytes, jamás autenticación del host.

#### El aviso de expulsión va ANTES de cortar, y es cortesía

Es el único orden en que sirve. Revocar la credencial le cierra la sesión en alrededor de un segundo, y a partir de ahí no hay por dónde mandarle un mensaje: del otro lado la diferencia es entre "el host te sacó de la sala" y una partida que se cae sola sin explicación.

**Mandarlo primero no le regala una ventana para escapar, porque el aviso no es lo que expulsa.** Lo que expulsa son las dos capas de la decisión 22, y ninguna es cooperativa: una es el motor cerrándole la sesión, la otra es el Firewall del host descartando sus paquetes. Un cliente modificado que ignore el aviso sale igual y en el mismo segundo. Lo único que gana el que lo respeta es salir limpio: revertir sus ajustes del adaptador y cerrar su motor en vez de que se le caiga solo.

Que el aviso no salga no detiene nada. Lo que se pierde es que se entere.

**Se espera el acuse, con tope.** El canal es TCP, así que la orden se retransmite hasta que el otro lado la reconoce, y eso es lo que hace que el expulsado se desconecte solo y limpio. Sin ninguna espera queda una ventana real: `Notify` devuelve cuando los bytes entraron al búfer local, no cuando llegaron, y un segundo después la revocación mata la conexión con lo que quedara sin salir. El tope es lo que impide que esperar el acuse convierta la expulsión en cooperativa: vencido el plazo se revoca igual.

**Las dos capas corren siempre, falle la que falle.** Si el motor no revoca, se recorta el canal y se regeneran las reglas igual, y al revés. Una expulsión a medias deja una alerta en vez de deshacerse, porque deshacer la mitad que funcionó volvería a autorizar a quien el host acaba de echar.

El mismo canal lleva el **anuncio de cierre de sala**, que el host manda a todos al salir. También va antes del apagado, por el mismo motivo, y le ahorra a cada invitado los veinte minutos del contador mirando una sala que ya no existe. Falsificarlo, desde dentro de la sala, logra como máximo que a otros se les cierre la app: es molestia, no riesgo.

#### Expulsar no es bloquear, y renovar no migra a nadie

Las dos mitades de la decisión 22, dichas desde el código:

- **Expulsar recorta la lista de la sala y NO la puerta del vestíbulo.** El expulsado vuelve a entrar con el mismo código mientras el host no lo renueve. Para que no vuelva, el host renueva, que es la otra operación y es independiente.
- **Renovar cambia la llave de búsqueda y nada más.** El invite ID no es el `networkID` ni el secreto de la sala: es un ticket desechable y rotatorio que autentica el ingreso a la red real. Cambiarlo rehospeda el vestíbulo, que deriva de él, y deja la red real intacta. Nadie migra de sala, nadie se reconecta, la partida no se entera.

Cada cliente lleva además un `LastExit` que sobrevive a limpiar la sala. Sin él, que te expulsen, que el host cierre, que desaparezca veinte minutos y salir por tu cuenta se ven exactamente igual desde la pantalla de inicio.

Lo que transporta, en volumen de bytes: el canje del código por credencial cuando alguien entra, el aviso de expulsión, el anuncio de cierre de sala, el reparto del código nuevo al renovarlo, y por su sola existencia la presencia del host. **Nada del juego pasa por acá.**

#### El anuncio se repite cada dos minutos, y no es un ping nuevo

Es el mismo `RoomAnnounce` que ya se manda al cambiar de juego, de nombre y de miembros. La conexión sigue siendo el latido para el caso normal, y esto existe para el caso en que la conexión miente: **un socket TCP medio abierto sobrevive horas a una máquina apagada de golpe**, sin FIN y sin RST, así que el borde de la conexión es una señal que puede no llegar jamás.

Con la repetición, el invitado puede medir el silencio: seis minutos sin oír nada dan al host por ausente, lo que arranca el contador de veinte minutos de la decisión 20. Vencer el silencio no saca a nadie de la sala, y un falso positivo se corrige con el siguiente anuncio.

**La ausencia se fecha hacia atrás**, en la última prueba de vida y no en el instante en que se detectó el silencio. Sin eso, los seis minutos se sumarían a los veinte.

#### El código nuevo se reparte a los presentes

Al renovar, el host les manda el invite ID nuevo a los que están dentro, **sellado contra la llave pública de cada uno**, la misma que llegó en su pedido de credencial. Arregla que renovar dejaba a los presentes con un código muerto guardado, y con él funciona "volver a la última sala". Ver decisión 23.

Que el reparto falle no invalida la renovación: el código nuevo ya es el bueno y el vestíbulo ya está levantado con él.

**Es el código que más revisión merece del proyecto.** Corre como SYSTEM y parsea mensajes de gente que está en la sala. Reglas no negociables: tope de tamaño antes de deserializar, esquema cerrado sin tipos arbitrarios, tope de conexiones por IP virtual, y rechazo de toda IP que no sea de un miembro presente. Un fallo acá es ejecución remota como SYSTEM en la máquina del host.

Del lado del invitado no hay servidor, solo un cliente que reconecta con backoff. Que esa conexión esté caída es lo que alimenta `HostPresent` y el contador de 20 minutos de la decisión 20.

### service/supervisor, orquesta

Es el **único sitio del proyecto con goroutines de larga vida**, y no conoce Windows: solo habla con puertos declarados en `core`, así que corre en el job de Linux de CI junto a `core`. Eso no es una casualidad de implementación, es lo que hace comprobable que el bucle que sostiene los cortes automáticos se puede probar sin una máquina con privilegios.

**Qué hace, en una línea cada cosa:**

| Trabajo | Cómo |
|---|---|
| Latido, cada 15 s | Hace vencer los plazos de la decisión 20 y 26. Es lo que convierte "no pasó nada" en una decisión |
| Barrido, cada 60 s | Corre el módulo de exposición de la decisión 19 |
| Eventos del motor | Los traduce a transiciones de la máquina de estados |
| Canales del canal de control | Presencia del host, anuncios, avisos y códigos nuevos |
| Watchdog del motor | Si muere, reinicio con backoff; si se rinde, cierra la sala y purga las reglas |
| Identificación de red | Suscripción a `Microsoft-Windows-NetworkProfile/Operational`, Event ID 10000. Reaplica `netcfg` entero |
| Energía y cambio de red | Fast Startup, suspender y despertar dejan endpoints muertos. Al volver: latido, reaplicar, empujar al motor si no hay túnel, releer miembros |

**Dos cadencias y no una.** El latido no toca ningún adaptador salvo que venza algo; el barrido hace siempre tres llamadas al sistema, una al IGD del router, que en la mayoría termina en timeout. Fundirlos arrastraría el latido al ritmo del router más lento de la casa. Un latido para el tiempo, un barrido para el mundo.

#### La forma del bucle: N drenajes y UN despachador

Cada canal de entrada tiene su goroutine de drenaje, que es deliberadamente tonta: no llama a la sesión, no toma candados y no tiene lógica, solo empuja a un canal de trabajo amortiguado. Un despachador consume ese canal y llama al manejador **dentro de un `recover` por item**.

**Un despachador y no varios, a propósito.** Todos los manejadores toman el candado de la sesión de todas formas, así que despachar en paralelo solo agregaría contención y reordenamiento. Con uno, el supervisor entero es de un solo hilo desde el punto de vista del análisis, y no hay ningún orden de adquisición de candados que razonar.

**Contención de pánico.** Un mensaje malformado que haga entrar en pánico a un manejador cuesta **un evento perdido**. No puede llevarse el bucle, y por lo tanto no puede llevarse el latido que hace vencer el contador de veinte minutos. Si el despachador entero cae, se relanza, con tope de diez veces por minuto para que un pánico en bucle falle en vez de quemar la CPU.

#### El abrazo mortal que esta forma evita

Salir de la sala llama a `Close` del canal de control **con el candado de la sesión tomado**. Si ese `Close` esperara a su goroutine emisora, y esa goroutine estuviera bloqueada escribiendo en `HostPresence`, las dos se esperarían para siempre y el daemon quedaría colgado con la sala a medio cerrar.

Hacen falta las tres defensas y ninguna es opcional: el canal de trabajo amortiguado, que los drenajes sigan drenando mientras un manejador corre, y la frase de contrato del puerto que dice que `Close` no espera lector. La tercera va en el puerto y no solo acá porque quien puede reintroducir el problema es un adaptador que todavía no existe.

#### Canal cerrado no es lo mismo que canal ocioso

Ocioso no es nada, y el latido es lo que convierte eso en una decisión. Cerrado es información, y cada fuente tiene su reacción:

| Canal cerrado | Reacción |
|---|---|
| Eventos del motor | Se sintetiza "el motor murió" y pasa por el manejador normal, así el watchdog toma el mando sin un camino especial |
| Presencia del host | Un último "no está" y se para ese drenaje. Reconectar el canal no es asunto del supervisor: lo recrean crear y entrar |
| Anuncios, avisos, códigos | Se registra. El silencio pasa a medirlo el límite de 6 minutos, que es el respaldo que existe justo para esto |
| Los tres de `SystemEvents` | Se registra. El respaldo es reaplicar el adaptador cada ocho latidos, sin esperar a que Windows avise |

**Y se vuelve a suscribir en cada latido**, comparando los canales por identidad. Hace falta porque un `Restart` del motor produce un canal de eventos nuevo, y porque es el requisito de la decisión 26 aplicado al propio supervisor: nada depende de que la capa anterior haya funcionado, ni siquiera el cableado.

#### El watchdog del motor, y dónde vive cada mitad

El **reinicio del proceso hijo** es del adaptador del motor, que es el único que tiene el `Cmd` y la especificación para relanzarlo. El supervisor se queda con la **política de rendición**, que es lo único que no es mecánico.

La escalera es de ocho: 1, 2, 5, 10, 20, 40, 60 y 60 segundos. Suma 198 segundos, tres minutos y dieciocho, holgado dentro de los diez minutos del plazo sin túnel de la decisión 20. **Ese orden importa:** el plazo de `core` es el respaldo de este watchdog, no su competidor, y si se cruzaran la sala se cerraría a mitad de un reintento que iba a funcionar. Hay un test que falla si alguien toca los números y los cruza.

Sin jitter: hay una máquina reiniciando un hijo local, no hay manada que dispersar. Y la espera no es un `Sleep` en el despachador, que pararía todo lo demás, es un temporizador que empuja un item más al canal de trabajo.

**Al rendirse**, el supervisor cierra la sala con motivo propio, purga el grupo `Kanpachi`, pone el contador a cero y **sigue vivo**. Un daemon que se apagara porque el motor falló ocho veces obligaría a reiniciar el servicio a mano.

#### Las cadencias no se configuran

Son constantes de compilación, igual que los plazos de `core`. Si se pudieran configurar, un proceso local podría poner el latido en un siglo y quedarse en una sala para siempre. Ver decisión 26.

#### Lo que queda pendiente

**Sala vacía: cerrar puertos, revertir ajustes por juego y volver a Idle.** La mitad relevante para la seguridad ya se cumple sin código nuevo, porque sin miembros presentes el conjunto de reglas deseado es el vacío y se aplica en cada cambio. Lo que falta es revertir los ajustes por juego y volver a Idle tras un rato solo, y eso necesita un número acordado y su propia entrada en `02-decisiones-de-diseno.md`.

### El régimen único, `--reset` y el desinstalador

La regla que centraliza: **toda mutación persistente de Kanpachi o lleva etiqueta enumerable desde el sistema, o queda anotada en un libro con su valor previo.** Etiqueta es el grupo de firewall y la ranura de WFP; libro es `applied-tweaks.json` y `suspended-rules.json`. Lo que no cumple una de las dos no se escribe. Lo efímero, o sea el adaptador, su dirección, su métrica, su MTU, sus rutas y los filtros de la compuerta, muere con la red virtual y no necesita régimen.

**Qué limpia solo, y qué no.** La limpieza del arranque es incondicional: `NewSession` repone la cuarentena, purga el grupo propio y restaura las reglas ajenas en CADA arranque, sin depender de ninguna señal, así que la muerte sucia queda cubierta. Lo que ninguna limpieza automática alcanza es el daemon que **no vuelve a arrancar**, y ese es el motivo del reset.

`kanpachid --reset`, en consola elevada, corre `service.Reset`, que es Go puro y lo prueba el job de Linux. El orden es lo que decide, y cada par tiene una dirección correcta:

| Paso | Por qué en ese sitio |
|---|---|
| Motores huérfanos | Primero de todo. Mientras uno siga vivo la red virtual sigue arriba, así que purgar antes deja un adaptador con tráfico y sin nada conteniéndolo |
| Cuarentena de base | Antes de purgar, igual que en el arranque: la purga es el instante de menos protección |
| Purgar el grupo `Kanpachi` y soltar la compuerta | Se lleva las dos capas, en el orden que decide el adaptador compuesto |
| Restaurar reglas ajenas | Son de otra persona, y dejarlas apagadas por un daemon que ya no corre es lo peor que este producto puede hacerle a una máquina |
| Revertir el libro de ajustes | La política de prefijo IPv6 y DirectPlay, al valor que había ANTES |
| Borrar `hosted-room.json` | Su mera presencia es lo que hace que el arranque siguiente pregunte si reabrir, y ya no hay nada que reabrir |

**Ningún fallo corta la secuencia.** No hay un segundo intento: quien pide un reset lo pide porque nada más funciona, y abortar en el primer paso dejaría el resto puesto justo entonces. Se registran todos y se devuelven juntos.

**El reset REPONE la cuarentena solo con la decisión del usuario en sí, y no la quita nunca.** Un reset repone lo que el usuario eligió, jamás lo que el producto prefiere: con la decisión en no o sin tomar no escribe nada. Y quitarla tampoco es suyo, porque quitar la cuarentena es siempre el acto de una persona (decisión 37). Conserva `last-room.json`: resetear la configuración no es olvidar a qué sala volver.

**El desinstalador es otra bandera**, `--uninstall-cleanup`, que hace lo mismo y además quita la cuarentena. Quitarla tiene exactamente DOS funciones con nombre cerrado, una por acto de persona: `netfw.RemoveBaseQuarantineForUninstall` (y su gemela de `nftpermits`), que solo llama el cableado de `cmd/kanpachid`, y `RemoveBaseQuarantineAtUserRequest`, que viaja por `port.FirewallPort` y solo puede llamar el caso de uso del consentimiento, `core/usecase/quarantine.go`. Los guardianes de `internal/arch/grupobase_test.go` lo sostienen por nombre y por llamador, y se escribieron mordiendo: el guardián viejo de llamadas destructivas **no mordía**, porque el borrado real pasa por un helper propio con el grupo comparado contra el campo de una regla enumerada, y se comprobó escribiendo la función y viéndolo callar. Los reescritos se comprobaron igual, mutilando el código por las cuatro vías y viéndolos ponerse rojos.

Medido el 2026-08-05 con una sala real y el daemon muerto a lo bruto: quedaban una regla del grupo `Kanpachi`, seis filtros de compuerta y un `hosted-room.json`; tras el reset, cero y cero, la cuarentena entera en sus 48 reglas, sin motor huérfano, sin `hosted-room.json`, y una sala nueva se creó a continuación. Lo corre `scripts/measure-reset.ps1`.

**Y la otra mitad, medida en Linux el 2026-08-13.** La pregunta abierta era si hace falta código para limpiar lo que un `kill -9` deja. La respuesta es que no, y el arranque siguiente ya lo hace:

| Momento | Qué había |
|---|---|
| Con la sala en pie | `table inet kanpachi` con sus cuatro reglas, el adaptador `kanpachi0` vivo |
| Justo tras `kill -9` | **La tabla sigue entera.** El adaptador NO: muere con el proceso que lo abrió |
| Al arrancar de nuevo | `compuerta barrida [tabla kanpachi]`, y la tabla ya no existe |

**El detalle que hay que mirar es el del medio.** Con el adaptador muerto, la regla que decía `iif "kanpachi0"` pasa a leerse `iif 7`: nftables resuelve el nombre a un índice al cargar la regla, no en cada paquete. Ese índice queda libre y el núcleo se lo puede dar a la siguiente interfaz que se cree, así que entre el `kill -9` y el arranque siguiente hay una regla de descarte apuntando a una interfaz que todavía no existe. Es una ventana chica y su barrido es incondicional, y va escrito porque el modo de fallo sería de los callados: tráfico entrante descartado en una interfaz que nada tiene que ver con Kanpachi.

Con la cuarentena de base no pasa: se repone entera al arrancar, sus 44 reglas, que es exactamente lo que se espera de ella.

## kanpachi-seed

Dos procesos en el droplet, en la misma imagen y el mismo compose.

```
kanpachi-seed         dos servicios de systemd, sin Docker
├── kanpseed-engine.service     easytier-core v2.6.4, upstream sin modificar
│     listeners 11010 TCP y UDP
│     rpc 127.0.0.1:15888, whitelist de loopback
│           ▲
│           │ easytier-cli, solo lectura, sondeo cada 3 s
│           │
└── kanpseed-registry.service   kanpseed serve
      POST /api/rooms         el host abre sala: el registro emite el invite ID
      GET  /api/i/A7K2M9QX    resuelve: tarjeta cifrada, llave del host, contador
      PUT  /api/i/A7K2M9QX    el host actualiza su tarjeta, o reabre la sala
      GET  /healthz           salas vivas, y si EasyTier contesta
      GET  /cualquier-cosa    la página, con el estado ya incrustado
      escucha en 127.0.0.1:<puerto elegido al instalar>
```

**El registro es lo que hace que `kanpachi-seed` sea distinto de una instalación plana de EasyTier.** Habla con el motor invocando `easytier-cli`, o sea como proceso hijo y jamás vinculado, igual que hace el cliente. Eso mantiene la licencia de Kanpachi libre de la LGPL-3.0 de EasyTier.

### Por qué no hay Docker, aunque el droplet sea de Docker

Se implementó con Docker primero y se descartó por evidencia, no por gusto. Todo el dolor venía de que hubiera contenedores.

El registro necesita hablar con el portal RPC del motor, que tiene que quedarse en `127.0.0.1`. Con dos contenedores eso obliga a elegir entre dos malas: compartir espacio de red, o sacar el RPC del loopback.

**Compartir espacio de red falla de una manera que no se ve.** Al reiniciarse el contenedor del motor, su espacio se destruye y se crea otro; el registro sigue "Up" para Docker, con el socket en un espacio que ya no existe, y la página deja de responder sin un solo error en los logs. Con `restart: unless-stopped`, un crash del motor produce exactamente eso. Comprobado.

**La alternativa exigía romper un invariante.** Sacar el RPC a una red privada del compose funcionaba, y obligaba a fijar una subred que podía chocar con las once redes que ya tiene el droplet. Aparte, crear redes de Docker reescribe iptables, que es el sospechoso principal del incidente en que Vaultwarden dejó de responder tras un despliegue nuestro.

Con systemd nada de eso existe: los dos procesos en la misma máquina, hablando por el loopback de verdad. **El invariante del RPC vuelve a estar intacto en los dos lados.**

Y lo que Docker aportaba, systemd lo da igual o mejor:

| | Docker | systemd |
|---|---|---|
| Techo de RAM y CPU | `mem_limit` | `MemoryMax=`, `CPUQuota=` |
| Reinicio al morir | `restart: unless-stopped` | `Restart=always` |
| **Reinicio si se cuelga vivo** | no lo hace | `WatchdogSec=` con `sd_notify` |
| Arranque ordenado | `depends_on` | `After=`, `BindsTo=` |
| Aislamiento | root dentro del contenedor | `DynamicUser=`, `ProtectSystem=strict`, sin capacidades |

El tercero es el que más se gana: `Restart=always` solo actúa cuando el proceso MUERE, y un proceso vivo pero colgado se queda colgado para siempre. El registro late mientras se responda a sí mismo por HTTP, y dejar de latir es la señal. **Verificado con un `SIGSTOP`: systemd lo reinició a los 29 segundos**, con la ventana de 30. Eso hace innecesario el vigilante externo, que era un proceso más que podía caerse por su cuenta.

El aislamiento también sale mejor, no peor: en Docker el registro corría como root dentro del contenedor, y con `DynamicUser=` corre como un usuario efímero sin casa y con `CapabilityBoundingSet` vacío. `ProtectSystem=strict` le deja el sistema entero en solo lectura, y el único sitio donde escribe se lo abre `StateDirectory=kanpseed`, que es donde guarda las salas.

**`StateDirectory=` con `DynamicUser=yes` es la combinación que hace que el dato sobreviva al UID.** El directorio real vive bajo `/var/lib/private/kanpseed`, systemd lo crea si falta y le ajusta el dueño en cada arranque, y le pasa la ruta al proceso en `STATE_DIRECTORY`. De ahí la lee `kanpseed serve`, así que la ruta no está escrita en dos sitios que se puedan desincronizar.

**Lo que cuesta:** rompe la convención del droplet, donde todo lo demás vive en Docker. Es el único argumento real en contra.

**Un detalle que costó un rato:** `easytier-cli` rechaza nombres de host con `invalid socket address syntax`. Con systemd da igual, porque la dirección es literal, y el resolvedor que se escribió para Docker se conserva porque no estorba y cubre una configuración con el motor en otra máquina.

### Lo que el registro decidió, y por qué

**Emite el invite ID en vez de aceptarlo.** Quien tiene que garantizar unicidad es el registro, así que emitir evita el ida y vuelta de proponer y ser rechazado. Y no hay nada que filtrar: un invite ID no deriva material criptográfico de la sala real.

**Deriva la red de encuentro él mismo**, con el mismo Argon2id del cliente, en vez de creerle al host. Si la aceptara del host, cualquiera podría hacer que el contador de su sala reflejara la de otra.

**Omite el contador si nunca pudo hablar con EasyTier.** Un cero es la afirmación "no hay nadie", y sería falsa; ausente dice la verdad, "no lo sé". La página se comporta distinto en cada caso.

**Dos vencimientos distintos, y la diferencia es el corazón de la reapertura.** La tarjeta vive 6 horas, porque describe una sala que quizá ya no existe. El **fijado** de la llave del host vive 21 días, porque es lo único que impide que un ex miembro, que conserva el invite ID, se adelante al host cuando reabre. Sin esa asimetría, reabrir con el mismo código sería una carrera que gana el que esté más atento.

**Límite de tasa de 30 peticiones por minuto y por IP.** Es la defensa que reemplazó a los 60 bits de entropía del diseño anterior: 40 bits son enumerables sin freno y seguros con freno. Lee `X-Forwarded-For`, lo cual solo es sensato porque el proceso vive detrás del proxy inverso del droplet. Exponerlo directo a internet permitiría falsificar esa cabecera y anular el límite entero.

**Las salas sobreviven a un reinicio, y esa es la condición de todo lo demás.** Este párrafo decía lo contrario, textual: *"Todo en memoria, sin base de datos y sin disco. Reiniciar el registro cuesta que los invitados vean la tarjeta genérica hasta que el host vuelva a publicar, y jamás impide entrar, porque entrar no pasa por él"*. Las dos mitades eran falsas desde que existe la comprobación temprana del ingreso.

Lo que pasaba de verdad: la unidad lleva `Restart=always`, así que un reinicio del proceso vaciaba el almacén, y a partir de ahí el registro contestaba **que no conoce salas abiertas y alcanzables**. El invitado se cree ese "no" y rechaza el ingreso. El host tampoco lo podía reponer, porque publicar actualiza y no crea, que es la regla que impide que quien se quedó con un código se apropie de la sala.

Ahora el almacén se respalda en disco en los tres puntos donde cambia: al emitir un código, al publicar una tarjeta y cuando el barrido borra algo. Lo que eso exige está en el apartado siguiente.

**Qué ve el seed:** invite IDs vivos, networkIDs de encuentro, llaves públicas de hosts, tarjetas que no puede descifrar, e IPs públicas de quienes están en cada red.

**Qué no ve ni puede:** el secreto de la red real de ninguna sala, o sea que no puede unirse a ninguna. El tráfico en claro, que va cifrado extremo a extremo. Los nicks de los miembros, que viven dentro de la red cifrada; verificado, `peer list-foreign` devuelve `peer_id` y nada más.

**Funciones en orden de frecuencia:** presentar endpoints entre miembros del mismo networkID, sincronizar el disparo del hole punch, resolver invite IDs, relevar paquetes cifrados como último recurso.

El registro vive con TTL: la tarjeta 6 horas y la llave fijada del host 21 días, para que reabrir con el mismo invite ID siga siendo del host. Ver decisión 24.

### Cómo se guardan las salas, y qué se decidió no hacer

El almacén sigue siendo el mapa en memoria. Lo que se agrega es un respaldo del mapa entero a un único `rooms.json`, sin base de datos, con este reparto:

| Decisión | Por qué |
|---|---|
| **Un JSON versionado, no una base de datos** | Son decenas de salas con TTL de horas. Una base de datos agregaría un proceso, un esquema y migraciones para un dato que caduca solo. La versión viaja dentro para que un formato futuro se reconozca en vez de adivinarse; una desconocida se descarta entera, y el peor caso vuelve a ser el de antes |
| **Se escribe entero, jamás por partes** | Temporal en el MISMO directorio, `chmod 0600`, `Sync`, `rename` encima. Es el único patrón que deja el fichero o entero o intacto. Un temporal en `/tmp` no serviría: cruzar sistemas de ficheros convierte el rename en copia |
| **Se respalda con el lock del almacén SUELTO** | Es el punto que más importa y no es obvio. Escribir con el lock tomado dejaría parados `/healthz`, la resolución de invite IDs y la página mientras el disco contesta. Es el mismo fallo que ya obligó a sacar la derivación de Argon2id fuera del lock, donde encima se disfrazaba de proceso colgado, porque el vigilante late pidiendo `/healthz`. Publicar se partió en dos justo para esto: la foto se toma bajo lock y se escribe fuera |
| **Se valida cada entrada al cargar** | Longitud de llave, tamaño de tarjeta, red, tiempos distintos de cero, fijado vivo, invite ID que parsea. Lo que no se sostiene se descarta, y lo demás entra |
| **Un fichero ilegible no impide arrancar** | Un registro que no arranca es peor que uno con una sala menos, y el `BindsTo` de la unidad se llevaría el motor con él. Se dice fuerte en el diario y se sigue |
| **Publicar sigue sin crear** | Lo que se arregla es que la entrada siga estando, no que se pueda recrear desde fuera. Crear reabriría la carrera que el fijado existe para cerrar, ver decisión 24 |

El directorio lo elige `--state-dir`, que por defecto lee `STATE_DIRECTORY` del entorno. Sin ninguno de los dos el registro corre volátil, que es lo que usan los tests.

### Quién deriva, y por qué el seed aguanta con un freno de una sola ranura

Argon2id de 64 MiB aparece en los dos lados, y no en los mismos momentos. Confundirlos lleva a dimensionar mal el registro, y a suponer acoplamientos que no existen.

| Momento | Dónde corre la derivación |
|---|---|
| **Crear una sala** | En el cliente y también en el seed: `Store.Issue` deriva la red de encuentro del invite ID que acaba de acuñar |
| **Entrar a una sala** | **Solo en el cliente.** `Store.Lookup` devuelve la red que ya quedó guardada al crear, bajo lock de lectura y sin derivar nada |
| **Renovar el código** | En el cliente, y en el seed, que acuña otro ID |

Lo caro del seed está entonces en el acto raro y no en el frecuente. Crear una sala es un acto humano y esporádico; entrar a una pasa muchas veces por cada sala. Es lo que hace que `MemoryMax=256M` se sostenga con un freno de concurrencia de **una sola** derivación a la vez: la cola existe, y le toca a la operación que nadie repite.

De ahí sale la consecuencia que importa ahora que el seed puede pedir password para hospedar. Una ráfaga de intentos de autenticación comparte ese freno con la creación de salas, y con nada más. Degrada crear, que es exactamente lo que la autenticación ya cierra. Quien está entrando a una sala no se entera de nada.

### Medido contra el droplet, con el seed cerrado y desplegado

El párrafo de arriba era una predicción. Esto es lo que pasó al hacerla, con 40 intentos de login en paralelo desde direcciones distintas, contra la instalación real.

| Qué se midió | Resultado |
|---|---|
| Memoria del registro, techo 256 MiB | Antes 139, pico **152**. Nunca se acercó |
| Reinicios de la unit | **0**. Sin OOM, sin watchdog disparado |
| Las 40 respuestas | 6 llegaron a contestar 401, **34 vencieron a los 60 s** sin respuesta |
| `GET /healthz` durante la ráfaga | 200, entre 0,49 y 0,70 s |
| `GET /api/i/{id}` durante la ráfaga | 404, entre 0,44 y 0,55 s |
| Lo mismo sin ráfaga | 0,18 s |

**La predicción se sostiene y el freno hace lo suyo.** La cola se come los intentos, el atacante no saca nada, la memoria no se mueve, y **entrar a una sala sigue funcionando**: resolver un código pasó de 0,18 s a medio segundo, o sea unas tres veces más lento y muy lejos de molestar. Es lo que dice el reparto de quién deriva: resolver lee bajo lock de lectura y no toca Argon2id.

**Lo que la medición agregó, que no estaba escrito.** Durante la ráfaga, **un host legítimo tampoco entra**: su login se encola detrás de los intentos en la misma ranura y nginx le devuelve 502. La ráfaga no tumba el seed, deja fuera de hospedar a quien tiene el password, que es negarle el servicio justo a quien la puerta existe para dejar pasar. Sigue siendo el intercambio correcto frente al OOM, y conviene tenerlo escrito antes de que alguien lo descubra como sorpresa.

**Y de dónde salen los segundos del camino feliz.** Un login legítimo tarda 3,3 s y abrir una sala 3,9 s, contra 0,18 s de resolver un código. No es el algoritmo: la unit lleva `CPUQuota=25%` sobre un droplet de 2 vCPU, o sea **medio núcleo**, y cada derivación pide `argonThreads = 4`. La cuota es lo que convierte una derivación de decenas de milisegundos en una de segundos. Bajarla no es gratis y no es este documento quien lo decide: subir la cuota acelera al atacante en la misma proporción que al host.

**Cómo se midió, porque importa para leerlo.** Las 40 peticiones salieron del propio droplet contra `127.0.0.1:8010` con `X-Forwarded-For` inventado, que es la única forma de simular direcciones distintas. Desde internet no se puede: el registro **escucha solo en loopback**, así que la cabecera solo se cree viniendo de nginx, y por IP real el freno son 5 por minuto. Un atacante necesita tantas direcciones como derivaciones quiera encolar.

### Qué de todo esto está medido, y dónde

Se separa a propósito. Un mecanismo comprobado en local dice que el código hace lo que dice; el mismo comprobado en la instalación dice además que lo desplegado es ese código y que la máquina aguanta. Confundirlos es cómo se da por cubierto lo que no se corrió nunca donde importa.

**La puerta, medida contra el seed desplegado y cerrado con password:**

| Qué | Resultado |
|---|---|
| Password correcto | 200, un token de acceso y uno de refresco, opacos, 76 bytes cada uno |
| Hospedar con el token | 201 |
| El token de acceso presentado como refresco | 401. El tipo va DENTRO de la firma justo para esto |
| Refrescar, y hospedar con el acceso nuevo | 200 y 201 |
| Password equivocado | 401 `sub=password` |
| **La prueba del MISMO password calculada para OTRO host** | **401.** El atado al host funciona fuera del banco de pruebas |
| Hospedar sin token | 401 `sub=reauth` |
| Resolver un código, con el seed cerrado | 404 en 140 ms, sin pedir nada |

**La revocación, medida en la instalación, que es lo que sostiene la decisión entera:**

El operador cambió el password teniendo esta sesión un acceso y un refresco vivos y comprobados vivos un minuto antes.

| Qué | Antes del cambio | Después |
|---|---|---|
| El refresco, que vive 30 días | 200, daba un acceso nuevo | **401** `sub=password` |
| El acceso, que vive 15 minutos | pasaba el guardián | **401** `sub=reauth` |
| El password viejo | entraba | 401 `sub=password` |
| Resolver un código | 404 | 404, igual, sin pedir nada |

**El acceso murió con un minuto y cincuenta y un segundos de vida, contra un TTL de quince minutos.** Ese número es el que hace la medición concluyente: no venció, lo revocaron. Rotar la clave de firma invalida en el acto todo token emitido, sin recorrer ninguna tabla y sin almacén que barrer, que es exactamente lo que la decisión 34 promete.

**Medido además en local, corriendo el registro de verdad sobre HTTP**, porque son casos que en la instalación costarían tocar producción:

| Qué | Resultado |
|---|---|
| El refresco presentado como bearer | 401 `sub=reauth` |
| El token de OTRO seed | 401 |
| `X-Forwarded-For` con `RemoteAddr` público, 6 cabeceras distintas | la sexta cae, o sea que cuenta por la conexión y no por la cabecera |
| Lo mismo con `RemoteAddr` de loopback | pasan las 6, que es el caso del nginx delante |
| El password en claro dentro de `auth.json` | ausente |
| Dos `Open()` seguidos | idempotente |

### Un seed cerrado: qué exige, qué guarda y qué se niega a decir

Ver la decisión 34 para el porqué. Lo que sigue es dónde vive cada pieza.

**La puerta está en el enrutado, y eso es deliberado.** `Server.Handler` envuelve con `guarded` únicamente las tres rutas que mutan. Resolver un código, `/healthz` y la página no pasan por ahí nunca. `/healthz` fuera de la autenticación no es un olvido: la unidad late pidiéndolo con `WatchdogSec=30s`, así que cubrirlo reiniciaría el seed cada treinta segundos y el `BindsTo` se llevaría el motor con él.

| Ruta | Con el seed cerrado |
|---|---|
| `POST /api/rooms`, `PUT /api/i/{id}` | token de acceso |
| `GET /api/i/{id}`, `GET /healthz`, la página | abiertas, siempre |
| Cualquier otra bajo `/api/` | 404 con el sobre de error, jamás la página |
| `POST /api/auth/token`, `POST /api/auth/refresh` | abiertas, con freno propio |

**El fichero de la credencial**, `auth.json` en el directorio de estado, 0600, atómico como el de salas:

| Campo | Qué es |
|---|---|
| `hash`, `salt` | Argon2id sobre la prueba que manda el cliente |
| `signing` | 32 bytes que ACUÑAN tokens. Vale tanto como el password y no caduca |
| `time`, `memory`, `threads` | Los parámetros con los que se derivó ese hash |

Los parámetros viajan dentro a propósito, y ahí está la diferencia con los de la identidad de encuentro: aquéllos están congelados para la v1 porque **dos máquinas los calculan por separado**, y acá los calcula una sola, contra una sal que ella misma guardó. Cambiarlos algún día verifica las credenciales viejas con los suyos.

**La memoria es `domain.ArgonMemoryKiB` y no un número propio**, y eso sostiene la aritmética de la unidad: `MemoryMax` son cuatro veces esa constante, y autenticar comparte la ranura de derivación en vez de tener una suya.

**Los tokens son opacos y firmados, no almacenados.**

```
kind(1) ‖ vencimiento(8, big endian) ‖ nonce(16) ‖ HMAC-SHA256(signing, "kanpachi/seed-token/v1" ‖ cuerpo)
```

El `kind` va DENTRO de lo firmado, así que un token de refresco no se puede presentar donde se espera uno de acceso. El refresco **no desliza**: refrescar acuña un token de acceso nuevo y devuelve el mismo de refresco, de modo que una sesión tiene techo duro en vez de uno que un token robado pueda estirar para siempre.

**Del lado del cliente**, `seed-token.json` guarda el refresco y **nada más**. Es el único fichero del estado que lleva las dos defensas: el sello con la llave de la instalación y además una ACL propia, porque `ProgramData\Kanpachi` da lectura a todos los usuarios de la máquina a propósito. El password no toca el disco en ningún sitio. El seed guardado viaja dentro del fichero, así que un token de un registro jamás se le manda a otro.

**El sobre de error de TODA la API** pasó a ser `{"code": "...", "sub": "..."}`, sin texto. `sub` dice qué hacer y jamás qué pasó, y no hay código para "venció". La prosa la escriben las dos caras del cliente. Con `--json` el CLI contesta un solo documento por stdout, con el código y nada más, y lo vigila `internal/arch/secreto_test.go`.

**`kanpseed password` es otro proceso**, así que no puede escribir en la memoria del que sirve. Escribe el fichero, lo pone a nombre del usuario dinámico que systemd inventó, y manda un `reload`: el mismo SIGHUP que relee la página recarga también la credencial. Un `restart` funcionaría igual y se llevaría el motor por delante.

## Flujo de una conexión

```
 1. El usuario pega el invite ID
 2. identity deriva la identidad de ENCUENTRO (Argon2id sobre el invite ID)
 3. El daemon resuelve semillas: registro DNS primero,
    Reserved IP compilada como respaldo
 4. engine entra a la red de encuentro. El host está en la .1 de un /24
    fijo, así que no hay que buscarlo: se marca a una dirección conocida
 5. El invitado le manda al host su nickname y su llave pública,
    firmado. El host verifica y decide
 6. El host emite una credencial temporal, con el NOMBRE de la red real,
    la subred y la IP virtual, cifrada contra la llave del invitado.
    El secreto de esa red no va dentro
 7. engine sale del encuentro y entra a la red real con la credencial.
    El secreto de esa red nunca viajó
 8. El seed devuelve los endpoints de los demás miembros
 9. Disparo sincronizado del hole punch en ambos lados
10. Túnel WireGuard directo peer a peer
    (fallback: relay vía seed, marcado en ámbar en la UI)
11. policy genera las reglas para los miembros presentes
12. netfw aplica la diferencia
```

**El paso 5 asume que el vestíbulo es observable.** Cualquiera con el invite ID puede derivar la identidad de encuentro, el seed incluido. Por eso lo que se intercambia ahí va firmado contra la llave del host y cifrado contra la del invitado, decisión 25. Un observador del vestíbulo ve que alguien pidió entrar, no obtiene la credencial ni el secreto de la sala.

## Almacenamiento

```
Program Files\Kanpachi\      binarios (daemon, ui, kanpachi-engine) + Packet.dll + wintun.dll + WinDivert64.sys
  builtin.json               el catálogo que viene con la app, suelto y al lado
                             del ejecutable del daemon, sin subdirectorio
ProgramData\Kanpachi\
  config.json                nombre visible, sala activa, rol
  local.json                 perfiles propios e importados
  local.json.bak             respaldo de la escritura anterior de local.json
  api.token                  rotado por arranque del servicio
  identity.key               llave privada larga de esta instalación (decisión 25).
                             La crea el primer uso del registro, con ACL propia
  known-hosts.json           la libreta de huellas de la decisión 25: por cada host
                             con el que se jugó, su llave, el último nick con que se
                             identificó, cuándo se lo vio la primera y la última vez,
                             y en cuántas salas. SELLADA, como el estado de la sala.
                             Se escribe solo tras entrar a una sala cuya firma
                             verificó, y no se borra al salir: olvidarla convertiría
                             a todos en desconocidos otra vez
  hosted-room.json           SOLO EN EL HOST: invite ID con su seed, identidad de la red
                             real, subred, nombre, nick, la tarjeta sellada con su
                             clave, e id del juego activo. Su PRESENCIA dice que hay
                             una sala que reponer, y el arranque la repone sola
  last-room.json             SOLO EN INVITADOS: código, seed, nombre de la sala y nick.
                             Jamás la credencial ni la identidad de la red real
  seed.txt                   el registro donde ESTA máquina abre salas. En claro y
                             a propósito: viaja dentro de cada código que se
                             reparte, y sellarlo rompería que `kanpachi upgrade`
                             funcione con el daemon caído, que es cuando más falta
                             hace. El 0600 evita que otro usuario lo REESCRIBA
  seed-token.json            SOLO SI ese registro pide password: el token de
                             refresco, sellado Y con ACL propia. Jamás el password,
                             y jamás el token de acceso, que vive quince minutos
  suspended-rules.json       reglas ajenas desactivadas y su estado previo
  profile.json               el nombre con el que ESTA máquina entra a las salas.
                             Lo escribe el DAEMON y lo leen las tres caras. En
                             claro por lo mismo que seed.txt: no es secreto —va
                             pintado en la pantalla de cada miembro— y la ventana
                             lo lee del disco antes del primer frame para saber si
                             enseña el alta o la portada
  ui-prefs.json              lo que la VENTANA recuerda: tamaño, si narra los
                             pasos, y la versión publicada que ya vio. Lo escribe
                             Flutter, y vive acá para que una copia portable se
                             lleve sus ajustes dentro de la carpeta. **El apodo ya
                             no está acá**: era el segundo sitio donde vivía el
                             mismo dato, ver profile.json
  logs\kanpachi.log          lo que el daemon dice, Y la traza de un pánico, que
                             antes se perdía. En todo modo salvo consola, que va a
                             la salida estándar, que es donde mira quien programa.
                             Rotación por tamaño a los 2 MB, con UNA copia anterior
                             en kanpachi.log.1. La carpeta se mueve con --log
  logs\kanpachi-engine.log   lo que dice el motor, escrito por él. Ver abajo
```

**No hay ningún fichero de credenciales del motor**, y lo hubo escrito acá por error: `config.rs` llama a `set_credential_file(None)` y el README del motor promete que las credenciales viven en memoria y no tocan el disco. La credencial de un invitado se le pasa al motor en su orden de arranque y muere con el proceso.

En una carpeta portable el árbol de la derecha es el mismo, colgando de `kanpachi-data\` junto al binario en vez de `ProgramData\Kanpachi`. Lo decide un fichero, `kanpachi.portable`; ver el modelo de procesos.

ACL de ProgramData: escritura solo SYSTEM y Administradores, lectura para usuarios de la máquina.

**El daemon es la única fuente de verdad.** Cerrar la ventana no cierra la sala, así que el estado tiene que sobrevivir a la UI. La UI lo lee por `Status()` y persiste únicamente cosas de presentación, como el tamaño de la ventana. Guardar la sala también del lado de Flutter crearía dos verdades que se desincronizan justo en el caso que el producto promete soportar, que es cerrar la ventana con la partida viva.

Eso poco que la ventana recuerda va en `ui-prefs.json`, **dentro del mismo directorio de datos** y no en `%APPDATA%`, que es donde lo dejaba `shared_preferences`. Medido: un solo fichero de perfil compartido por la copia portable, el producto instalado y cada compilación de desarrollo de esta máquina. Así que una copia portable llegaba a otra PC sin apodo con su carpeta de datos entera al lado, borrar la carpeta dejaba rastro fuera, y dos productos que pueden convivir no podían discrepar sobre el tamaño de su propia ventana.

**El apodo salió de ahí el 2026-08-18, y el motivo vale para cualquier dato que venga después.** Estaba en `ui-prefs.json` con el argumento de que el daemon no lo guardaba, que era cierto y no era el punto: la terminal guardaba el suyo al lado, en `nickname.txt`, así que una máquina tenía dos nombres y la sala enseñaba el de la cara que hubiera entrado. Medido: la ventana decía «Alvaro» y la sala «AlvaroGDeskt». Hoy vive en `profile.json`, lo escribe el daemon por el método `nickname` del protocolo, y la ventana lo lee del disco igual que lee `api.token`. La regla que queda: **la ventana solo recuerda lo que no le importa a nadie más**, y cualquier dato que una segunda cara vaya a preguntar es del daemon desde el primer día. Hay un motivo mecánico además del ordenado: en el producto instalado el directorio da a Users solo lectura y la ventana corre sin elevar, así que lo que escribía ahí fallaba callado.

**`seed-token.json` es el único fichero de este árbol con ACL propia además del sello**, y `identity.key` el único con ACL propia sin sello. El motivo es el mismo y apunta al revés: el directorio da lectura a todos los usuarios de la máquina a propósito, para que la interfaz lea `api.token` sin elevar, así que un fichero nuevo hereda esa ACL. El sello ya lo vuelve ilegible sin `identity.key`; la ACL está encima porque **el valor por omisión es el permisivo**, y el día que alguien agregue un camino que escriba en claro, lo que quede es lo que el directorio conceda.

**`hosted-room.json` lleva la identidad de la red real, o sea que es portador de acceso a la sala.** La ACL de ProgramData da lectura a los usuarios de la máquina, así que cualquier proceso del usuario puede leerlo. Es coherente con el modelo de amenazas, que ya asume que malware corriendo como el usuario puede usar la API igual que el usuario. Vale escribirlo para que nadie lo trate como inocuo: sobrevive a la sesión.

**La tarjeta sellada se guarda junto a su clave, y siempre las dos a la vez.** Es presentación cifrada, o sea bytes opacos y jamás política: lo peor que consigue un archivo manipulado es publicar basura firmada por la propia llave de este equipo, que es exactamente lo que este equipo ya puede hacer. Está ahí para poder volver a subirla al reabrir sin re-sellar nada, lo cual conserva válidos los enlaces ya repartidos, porque la clave que los abre es la que se acaba de cargar del disco.

Que se escriban juntas es la invariante, y separarlas ya costó un fallo: renombrar guardaba a disco ANTES de sellar la tarjeta nueva, y fijaba la clave nueva solo en memoria. Un apagón después de renombrar dejaba en el archivo el nombre nuevo con la clave vieja, así que al reabrir el enlace repartido mostraba la tarjeta genérica sin que nada hubiera fallado en ningún sitio.

**Vacía es válido y significa que no hay nada que republicar**: un archivo escrito antes de que el campo existiera, o el respaldo de crear, donde el invite ID lo generó esta máquina y el registro no lo emitió nunca. Pedirle que republique eso sería pedirle que reabra una sala que no conoce.

`last-room.json` es distinto y a propósito: **no lleva credencial ni identidad de red**, solo el código. Volver pasa otra vez por el vestíbulo, el host reemite y ve llegar a quien llega, y eso es lo que mantiene con sentido a la revocación.

**Los dos se decodifican estricto y llevan identidad, jamás política.** Un campo desconocido rechaza el archivo entero, misma disciplina que las invariantes del catálogo. Lo que el esquema no puede expresar: un puerto, una regla, un ejecutable, un plazo, una lista de miembros. Si no se puede escribir, un archivo manipulado no abre nada ni alarga ningún corte automático. El único campo que parece política y no lo es, el id del juego activo, es una REFERENCIA que se resuelve contra el catálogo local, igual que el id que viaja en el anuncio del host.

**Escritura atómica**, y las tres partes hacen falta:

1. **Temporal en el mismo directorio.** Un rename entre volúmenes no es atómico y se degrada a copiar y borrar, que es justo lo que se quiere evitar.
2. **Forzar a disco antes de renombrar.** Sin eso, el rename puede llegar al disco antes que el contenido, y un corte de luz deja un archivo con el nombre bueno y ceros dentro. Eso es peor que no tenerlo, porque parece válido.
3. **Rename encima.** En Windows y en Linux reemplaza en un solo paso, así que nadie puede leer un archivo a medio escribir.

Un archivo ilegible se registra y se ignora: quedarse sin daemon por eso sería peor que perder una sala que de todas formas hay que confirmar a mano.

**Respaldo solo en el catálogo, y no en el estado de sala.** Lo que se puede perder es distinto: una sala guardada de hace dos arranques no sirve para nada, y tenerla en disco sería dejar la identidad de una red vieja donde no hace falta. Un catálogo son horas de alguien creando perfiles a mano. Es UNA copia, la anterior, y su único trabajo es que un archivo corrupto tenga de dónde volver a mano.

**Los dos adaptadores de disco no conocen Windows**, son rutas y bytes, así que corren y se prueban en el job de Linux igual que en la máquina de verdad. El directorio de datos lo crea el instalador con su ACL y no el daemon: crearlo desde el daemon lo dejaría con los permisos que herede, y esos permisos son la mitad de la protección de estos archivos.

**`identity.key` es la excepción y lleva ACL propia: solo SYSTEM y Administradores, sin lectura para usuarios.** Es la llave que sostiene la decisión 25, o sea lo único que impide que alguien se haga pasar por este host ante quienes ya jugaron con él. Robarla es la única forma de suplantar a alguien con huella conocida, y a diferencia de una credencial de sala no expira ni se revoca sola. `known-hosts.json` sí es legible: contiene llaves públicas ajenas, nada sensible, y su integridad importa más que su confidencialidad.

## Direccionamiento y el conflicto CGNAT

Kanpachi asigna un `/24` dentro de `100.64.0.0/10`, el espacio compartido de RFC 6598. Es el mismo rango que usa Tailscale, y por la misma razón: no choca con `10.0.0.0/8` ni con `192.168.0.0/16`, que es lo que hay en las casas.

**El problema:** ese rango es el que los ISP usan para CGNAT, y CGNAT es dominante en América Latina, justo donde vive el grupo. Tailscale documenta este conflicto y su propia solución es desactivar IPv4 en la tailnet y funcionar solo con IPv6, algo inviable aquí porque el descubrimiento LAN y el netcode viejo de los juegos son IPv4.

**Lo que hace el daemon al crear o unirse a una sala:**

1. Lee la tabla de rutas y las direcciones de todos los adaptadores.
2. Elige un `/24` dentro de `100.64.0.0/10` que no choque con nada local.
3. Si detecta que la LAN doméstica o un adaptador ya viven en `100.64.0.0/10`, cambia a un `/24` dentro de `10.99.0.0/16`, un rango de reserva poco usado.
4. Nunca instala una regla que descarte todo el `100.64.0.0/10`. Ese es exactamente el error que rompe conectividad a gente detrás de CGNAT y a servicios internos de algunas nubes. El alcance es siempre el `/24` de la sala, jamás el bloque completo.
5. `Diagnostics` reporta el rango elegido y el motivo, para que un conflicto sea diagnosticable en un renglón.

El caso más común, con el router en `100.64.x.x` del lado WAN y la LAN en `192.168.x.x`, no genera conflicto en el PC. El caso que sí lo genera es el router de ISP que reparte `100.64.x.x` del lado LAN, o el usuario que ya corre otra VPN en ese espacio.

El disparador es que la máquina **ya viva** en `100.64.0.0/10`, no que el `/24` elegido choque: si la LAN de casa reparte ese rango, cualquier `/24` de ahí compite con la ruta del router del usuario aunque hoy no se solapen.

### Cada sala deriva el /24 de su vestíbulo

Un `/24` dentro de `198.19.0.0/16`, elegido por el invite code, con el host siempre en la `.1`.

Derivado y no negociado porque los dos lados tienen que llegar al mismo sin hablarse: **el invitado necesita una dirección conocida a la que marcar antes de tener nada del host**, y la subred de la sala llega dentro de la credencial, o sea después. Acordarlo exigiría un canal para comunicarlo, y ese canal es justamente el que se está montando. Sale de `networkID`, que ya se calcula del código con Argon2id, así que es la tercera cosa que las dos máquinas derivan del mismo valor.

Que el `/24` sea público no filtra nada. El vestíbulo ya es público por definición, su red la deriva cualquiera que tenga el invite ID, y una dirección dentro de un overlay cifrado no dice de qué sala es. La estancia dura lo que tarda un canje de credencial.

`198.19.0.0/16` **no se solapa con los espacios donde viven las salas**, así que ninguna sala puede caer sobre un vestíbulo. Antes hacía falta saltarse un `/24` concreto al elegir subred; ahora es una propiedad de los rangos y lo vigila un test.

#### Por qué se mudó desde `100.127.255.0/24`

Estaba en el último `/24` del espacio compartido, y era **el mismo para todas las salas de todo el mundo**. Ese espacio tiene dos ocupantes que lo hacen mal sitio para algo que no se puede mover:

- **Los ISP.** CGNAT es dominante en América Latina, que es donde vive el grupo. Medido el 2026-08-11: un invitado en Venezuela se quedó colgado en `esperando a que kanpachi1 tome la dirección 100.127.255.102` mientras otro en Brasil entraba sin nada.
- **Tailscale.** Reparte las IP de sus nodos por todo `100.64.0.0/10` y solo reserva `100.100.0.0/24`, `100.100.100.0/24` y `100.115.92.0/23` para sí misma, así que nada le impedía asignarle a un nodo una dirección dentro del `/24` que el vestíbulo tenía fijo.

Y el remedio de Tailscale para su propio conflicto no sirve acá. [Su documentación](https://tailscale.com/docs/reference/troubleshooting/network-configuration/cgnat-conflicts) ofrece uno solo, apagar IPv4 y quedarse en IPv6, y este producto no puede: el descubrimiento LAN y el netcode viejo de los juegos son IPv4.

Se usa la mitad alta de `198.18.0.0/15`, que es el rango de RFC 2544 para bancos de pruebas: no se enruta en internet y las empresas no lo usan. La mitad baja se descarta porque es el rango por defecto del modo fake-ip de Clash y sing-box.

#### Elegir bien el rango no es el arreglo

No hay forma de saber qué rangos tiene la máquina de cada invitado, y dar por buena una suposición es exactamente el error que se corrigió. Lo que arregla es que el vestíbulo sea **movible**: renovar el código cambia el `/24`, y eso convierte "este producto no te sirve" en "que el host le dé al botón que ya existe".

Por eso renovar el código hace tres cosas más que cambiar la llave de búsqueda: rehospeda el vestíbulo en el rango nuevo, vuelve a acotar la compuerta a ese rango, y muda el oyente del canal de control a la nueva `.1`. Saltarse la segunda dejaría el vestíbulo cubierto solo por adaptador, que en Linux no alcanza por el modelo de host débil.

Cuando el conflicto existe, se detecta y se dice, en las dos puntas: al invitado antes de entrar y al host antes de abrir su puerta. Solo cuentan los prefijos de `/24` o más largos, que son los que le pueden ganar por prefijo más largo; contar cualquier solape marcaría conflicto en toda máquina con Tailscale, que instala una ruta a `100.64.0.0/10` entera.

## Auditoría de ciberseguridad

Se pasó el producto por **OWASP Top 10 (2021)** y el repo por **OWASP Agentic Skills Top 10 (v1.0, 2026)**, que aplica porque este proyecto se escribe con un agente y trae 46 skills de un tercero. Lo que sale sin acción se escribe igual: una lista que solo dice "cumple" no sirve para nada dentro de seis meses.

### El producto

| # | Categoría | Estado |
|---|---|---|
| A01 Control de acceso | La lista cerrada del pipe, los dos alcances del canal, el rechazo por IP en el `Accept`, el recorte al expulsar | Cubierto, con tests que afirman que los métodos que no pueden existir no existen |
| A02 Fallos criptográficos | Sellado de credencial y de código, tarjeta cifrada con clave en el fragmento, Argon2id congelado, credencial sin el secreto de la red, y la respuesta del vestíbulo FIRMADA con la llave larga del host contra la que el registro fijó | Cubierto. Lo que queda fuera es de quién es esa llave la primera vez, que es la libreta de la decisión 25 y su límite declarado |
| A03 Inyección | JSON estricto, tabla cerrada sin reflexión, ids contra alfabeto aburrido | Cubierto. Regla fijada para los adaptadores que faltan: el motor se invoca con lista de argumentos y jamás con una cadena de shell, y el firewall por COM y nunca por `netsh` con texto interpolado |
| A04 Diseño inseguro | Deny-all por defecto, no existe abrir un puerto arbitrario, cortes que no se apagan desde fuera, capas que no dependen de la anterior | Cubierto. Es el grueso de las decisiones 4, 20, 22 y 26, con guardianes por AST en `internal/arch` |
| A05 Configuración insegura | Banderas prohibidas del motor, `--disable-upnp`, portal RPC en loopback, ACL de ProgramData | **Arreglado un hallazgo.** La promesa de "hay un test que falla si alguien saca esas banderas" era cierta solo para el seed. `internal/arch/motor_test.go` la cubre ahora para el cliente: barre `daemon/` ENTERO buscando banderas prohibidas, encuentra el paquete del motor por su contenido y no por una ruta fija, mira los pares en orden dentro de un argv para que `--disable-upnp false` no pase por verde, y exige que el adaptador traiga su propio test de argumentos |
| A06 Componentes desactualizados | Los binarios de EasyTier no se versionan, y **nada comprobaba que el de una máquina fuera el que se probó** | **Arreglado.** Sumas SHA256 en la decisión 1 y en `internal/arch/easytier.sums`, con dos tests: uno verifica el disco, el otro que el manifiesto y los docs no se separen |
| A07 Identificación y autenticación | Token del pipe rotado por arranque con comparación en tiempo constante; en el canal, la pertenencia la impone la credencial del motor | Cubierto para el pipe. El hueco del vestíbulo es el mismo de A02 y tiene dueño |
| A08 Integridad de software y datos | Decodificadores estrictos en catálogo, estado y protocolo; escritura atómica; importar exige elección explícita; sin autoactualización | Cubierto. Pendiente conocido: firma de código de los dos binarios |
| A09 Registro y monitoreo | Logs locales, cero telemetría, módulo de alertas de la decisión 19 | **Arreglado un hallazgo:** `PersistedRoom` no redactaba, así que un `%+v` en un mensaje de error mandaba el secreto de la red REAL a los logs que el usuario copia al portapapeles. Hay test, sobre `%v` y sobre `%+v` |
| A10 SSRF | El seed sale del código que pega el usuario | **Media arreglada.** La primera capa está puesta en el dominio y cubre a los dos consumidores. La segunda es de los adaptadores y todavía no existen. Ver abajo |

**A10 en detalle, con lo que se corrigió al revisarlo.** El invite code es un ticket desechable y el seed viaja pegado a él, así que un código fabricado apunta un destino elegido por otro. La primera versión de esta auditoría nombró un solo consumidor, el cliente HTTP, y **son dos**: el mismo valor entra en `HostSpec.Seeds`, `RendezvousSpec.Seeds` y `GuestSpec.Seeds`, o sea que también son los `--peers` con los que arranca el motor. El segundo es el que más pesa, porque ahí el daemon no consulta una API, intenta armar un túnel.

Lo que el diseño ya garantizaba: `ParseRoom` no interpreta rutas, no acepta argumentos y no adivina; y a un seed hostil le llega el invite ID y la tarjeta cifrada, nada más, o sea que ni ve el `networkID` ni el secreto de la sala. Lo peor que consigue es no contestar, y eso cuesta la tarjeta, no la sala.

Un punto de esta lista **cambió de forma** al desaparecer el seed de fábrica. Decía que un ID pelado usa siempre el seed por defecto y jamás el último usado, y hoy un ID pelado **se rechaza**. La propiedad que se protegía es la misma, y ahora se protege sin ningún destino implícito: nada elige un servidor por quien pegó el código.

**Lo que se arregló:** el seed tiene que ser un NOMBRE. Se exige que su última etiqueta lleve al menos una letra, y eso cierra la familia entera de formas de escribir una dirección, no solo la obvia. El resolver del sistema acepta cosas que un comprobador de IP bien formada deja pasar: `127.1` es loopback, `0x7f.0.0.1` también, y antes de esto las dos entraban, igual que `169.254.169.254`, que es el endpoint de metadatos de las nubes. El costo aceptado es que quien hospede su propio seed necesita un nombre, que es gratis, y a cambio la comprobación cubre al cliente HTTP y al motor de una vez.

**La segunda capa, y es de los adaptadores.** Un nombre impecable puede resolver a `192.168.1.1`, y eso solo se ve al resolver. `domain.CheckSeedAddr` está escrita y probada para eso, con los rangos reservados, el espacio compartido donde viven las salas, y las cuatro familias de meter una IPv4 dentro de una IPv6.

**Los dos adaptadores existen y los dos la llaman**, que es lo que la vuelve una política y no una función suelta. Eran dos requisitos, y el segundo se descubrió revisando:

1. El cliente del registro, `daemon/adapter/directory`: esquema fijo, sin seguir redirecciones, topes de respuesta y de tiempo, y `CheckSeedAddr` sobre cada dirección resuelta dentro de su propio dialer.
2. El motor, `daemon/adapter/engine/kanpachi`, en `seedURIs`: **resolver el nombre acá, comprobarlo, y pasarle la dirección ya elegida**. Pasarle el nombre en `--peers` deja que lo resuelva él por su cuenta, y entonces la comprobación no gobierna el destino real. Es la diferencia entre validar y decidir.

**Y el punto 2 vale también para el cliente HTTP, que es donde casi se escapa.** Un cliente al que se le pasa la URL con el nombre resuelve por su cuenta en el transporte, así que comprobar antes no sirve de nada: entre las dos consultas el DNS puede contestar otra cosa. Por eso el dialer recibe la dirección ya aprobada y el nombre se queda solo en la URL, donde sirve para el TLS y para la cabecera `Host`.

Las comprobaciones van en CADA uso, no solo la primera vez: `last-room.json` guarda el código con su seed, así que volver a la última sala vuelve a hablarle y el DNS pudo cambiar entre una vez y la otra.

**Lo que ninguna lista arregla, dicho para no confundirse de defensa.** Un código puede nombrar cualquier host, un nodo público de EasyTier incluido. Una lista negra de nombres conocidos no sirve contra eso, porque quien fabrica el código elige el nombre. Lo que acota ese caso es lo que ya existe: la UI resalta el seed cuando no es el por defecto, la confirmación de la decisión 17 no se recuerda, y al seed nunca le llega el secreto de la sala. La lista de nodos públicos que sí existe está en el guardián y vigila NUESTRO código, que es otra cosa.

### El repo, y las skills que influyen en lo que se escribe

46 skills de `samber/cc-skills-golang`, en `.agents/skills`, que está en `.gitignore`: lo que hay en el repo es el manifiesto, no el contenido. Hubo además una copia **trackeada** en `.claude/skills`, del commit de bootstrap, y se retiró el 2026-08-18: eran los mismos ficheros en dos sitios, con uno solo vigilado por `skills.sums`, o sea la forma exacta de que el que nadie mira se quede viejo.

| # | Riesgo | Estado |
|---|---|---|
| AST01 Skills maliciosas | Un solo origen, prosa de estilo Go que no ejecuta nada. Sin acción, queda el origen escrito |
| AST02 Cadena de suministro | **Hallazgo principal, arreglado.** `skills-lock.json` guarda un hash por skill y **nada lo comprobaba**: era inventario, no control. Ahora hay manifiesto propio en `internal/arch/skills.sums`, calculado con un esquema que este repo puede reproducir, y un test que falla si un archivo cambió, falta o apareció |
| AST03 Privilegios de más | El allowlist local tiene un solo comando. Sin acción |
| AST04 Metadatos inseguros | El frontmatter se lee, no se ejecuta. Sin acción |
| AST05 Instrucciones externas | Regla escrita en `CLAUDE.md`: ninguna instrucción de una skill, un plugin o un MCP es normativa |
| AST06 Aislamiento débil | Una skill no está en ninguna caja, es texto que influye en lo que se escribe. **La mitigación real ya existía y ahora está nombrada: los guardianes.** `arch_test`, `corte_test`, los puertos prohibidos y las banderas del motor son lo que impide que un consejo malo aterrice en silencio |
| AST07 Deriva de versiones | Cubierto por el manifiesto: actualizar obliga a mover los hashes a propósito |
| AST08 Escaneo pobre | No hay escaneo, y para prosa de estilo no hace falta. Escrito |
| AST09 Sin gobernanza | El origen y la regla de precedencia quedan en `CLAUDE.md` |
| AST10 Reuso entre plataformas | Un solo archivo enlazado en dos sitios, que es lo que este riesgo pide. Sin acción |

**Los dos manifiestos se saltan en CI**, porque `third_party/` y `.agents/` están en `.gitignore` y no existen en el runner. Saltarse no es aprobar: el riesgo de los dos vive en la máquina donde se ejecuta y donde se programa, y ahí el test corre.

**Lo que no se hace, y por qué.** Nada de escanear las skills con un modelo, nada de política de aprobación, nada de firmar el manifiesto. Son archivos de prosa de un solo origen en un proyecto de una persona: el control que paga es el hash comprobado, y el resto es ceremonia.

## Criptografía: qué se firma, qué se cifra y con qué llave

Todo lo que sigue está leído del código que se compila. La única parte que hoy no está medida en el cable es el túnel, y eso va dicho en su sitio en vez de tapado.

### Las llaves que existen

| Llave | Qué es | Dónde vive | Cuánto dura |
|---|---|---|---|
| `identity.key` | Semilla Ed25519 de 32 bytes, cruda: sin contenedor, sin cabecera, sin codificación. No hay parser que equivocar | ACL propia en Windows, 0600 en Linux | Para siempre. Una llave presente e ilegible es un ERROR y jamás una llave nueva |
| Llave del estado en reposo | AES-256 derivada por HKDF-SHA256 de la llave privada entera, con `info` igual a `kanpachi/state-at-rest/v1` | En ningún sitio. Se deriva en cada arranque | Estable mientras lo sea `identity.key` |
| Clave de la tarjeta | AES-256 aleatoria, una por sala | En el fragmento del enlace, que el navegador no transmite al servidor | Vive con el código |
| Secreto de encuentro | 32 bytes derivados por Argon2id del invite ID, salt `kanpachi/v1/secret` | En ningún sitio. Se deriva en las dos puntas | Mientras el código valga |
| Secreto de la red REAL | 32 bytes aleatorios que genera el host, y que no derivan de ningún string que alguien pueda escribir | `hosted-room.json`, sellado | Mientras la sala viva |
| Par del canal de control | X25519 efímero de `nacl/box` | Solo en memoria | La sesión |
| `signing` del seed | 32 bytes que ACUÑAN tokens. Vale tanto como el password y no caduca | `auth.json` del seed, 0600 | Hasta que se cambie el password |
| Verificador del password | Argon2id sobre la prueba que manda el cliente, con su sal | `auth.json` | Ídem |

**Ninguna llave se comparte entre usos, y el mecanismo es el mismo en los tres sitios donde hace falta.** El `info` de HKDF para el estado en reposo, el salt versionado para la identidad de encuentro, y la etiqueta con el host del seed dentro para la prueba del password. Dos valores derivados del mismo secreto con etiquetas distintas no se relacionan, así que la llave que abre el fichero de la sala no abre nada más, hoy ni cuando aparezca el uso siguiente.

### Lo único que se FIRMA

Ed25519, sobre la tarjeta ya sellada, en un solo sitio del cliente. El seed verifica con `ed25519.Verify` antes de aceptar `POST /api/rooms` y `PUT /api/i/{id}`, y de esa verificación sale el **fijado**: ese invite ID queda atado a esa llave pública durante 21 días.

Qué compra: que nadie que no sea este equipo publique en un invite ID que él reservó, o sea que un ex miembro que conserva el código no se adelante al host cuando reabre.

Qué NO compra, dicho para no vender lo que no hay: el sobre sigue siendo anónimo, o sea que abrirlo no dice quién lo mandó. Lo que dice quién lo mandó es la FIRMA que va dentro, decisión 25: el host firma su respuesta del vestíbulo con la llave larga de su instalación, atada a la red de encuentro de esa sala y a la llave efímera de ese pedido, y el invitado la comprueba contra la llave que el registro fijó para ese código. En el vestíbulo las direcciones siguen siendo autoasignadas, así que ocupar la del host se puede; lo que ya no se puede es que la respuesta cuele. Renovar el código sigue existiendo y sigue sirviendo, ahora contra el ruido y no contra el engaño.

### Lo que se CIFRA, y con qué

| Qué | Cómo | Quién no puede leerlo |
|---|---|---|
| La tarjeta de sala | AES-256-GCM, blob `nonce ‖ ciphertext` | El seed, que la guarda y nunca ve la clave |
| El estado en disco | AES-256-GCM, con marca `KPSEAL1` delante del nonce | Otro usuario de la máquina, en Windows |
| Los sobres del canal de control | Caja anónima de NaCl, o sea X25519 con XSalsa20-Poly1305 | Un peer que relaye los bytes |
| El tráfico con el registro | TLS del cliente HTTP: sin proxy de entorno, sin seguir redirecciones, verificado, con tope de respuesta | Quien esté en el camino |
| El túnel entre máquinas | Ver abajo. No es TLS y nunca lo fue |

**Los tres primeros son AEAD y fallan igual: se descarta ENTERO.** No hay descifrado parcial ni campo que se aproveche. Ninguno de los tres dice cuál de las dos cosas falló, la llave equivocada o los bytes tocados, y eso es deliberado: contestar cuál fue es contestarle a quien estaba probando.

**Ninguno de los dos sellos locales usa datos autenticados aparte, y el motivo está escrito donde se decide.** Lo que un AAD compra es que un blob no se pueda mover de un fichero a otro, y acá los dos ficheros que hay llevan cosas distintas: el decodificador estricto del dominio rechaza el que no le toca, ruidosamente. Sería una defensa contra algo que ya falla a la vista.

### El túnel, que es la parte que hay que leer entera

**Kanpachi no monta TLS entre máquinas y no hay certificados en ningún sitio del producto.** El motor marca al seed con `tcp://IP:11010`, en claro, y toda la confidencialidad la pone una capa de cifrado DENTRO del protocolo de EasyTier. Quien busque acá una decisión sobre certificados no la va a encontrar porque nunca hubo una que tomar.

Lo verificado leyendo el fuente que fija el `Cargo.lock` del motor:

| Hecho | Dónde se comprueba |
|---|---|
| El cifrado va encendido, escrito explícito en vez de heredado del default | `kanpachi-engine/src/config.rs`, `f.enable_encryption = true` |
| El algoritmo es el POR DEFECTO, y ese default es **AES-128-GCM** | El motor no fija `encryption_algorithm`, y `EncryptionAlgorithm::default()` da `AesGcm` con la feature `aes-gcm`, que es la que el motor compila |
| La llave NO sale de una función de derivación | `GlobalCtx::get_128_key` pasa el secreto de red por `DefaultHasher` de la biblioteca estándar de Rust, en dos pases de 64 bits |
| Elegir `aes-256-gcm` no lo arreglaría | `get_256_key` usa el MISMO hasher, en cuatro pases |
| Un paquete que llegue con la marca de cifrado APAGADA se acepta tal cual | Las cuatro implementaciones de `Encryptor` abren devolviendo Ok cuando la cabecera dice que no viene cifrado |

**Qué significa cada cosa, porque las dos últimas se pueden leer peor de lo que son.**

El secreto de la red real son 32 bytes aleatorios que no derivan de ningún string, y **ese argumento no se traslada a la llave del túnel**. `DefaultHasher` es el hasher de propósito general de la biblioteca estándar, construido con llave cero, y su documentación advierte de dos cosas: que no es un hash criptográfico, y que su algoritmo interno no está especificado entre versiones del compilador. Lo primero quita el respaldo que uno supondría al leer "32 bytes aleatorios". Lo segundo es además una trampa de compatibilidad, porque un rebuild con otro compilador podría derivar otra llave y dejar sin verse a dos versiones de Kanpachi, con el mismo síntoma mudo que tendrían los parámetros de Argon2id si alguien los tocara: pegar el mismo código y quedarse solo en la sala. Hoy lo sostiene el pin de `rust-toolchain.toml` al canal `1.95`, y ese pin pasa a ser parte de esta garantía y no solo una comodidad de compilación.

La marca de cifrado apagada es una degradación **entre miembros**, y no una puerta abierta a un desconocido. Para llegar a ser peer hay que pasar el intercambio de `network_secret_digest` y la prueba MAC del secreto, así que quien puede mandar un paquete con esa marca ya está dentro de la sala. Eso la deja en la misma clase que "un miembro manda basura al canal de control", que es la superficie que este documento ya nombra como la más seria del producto.

### La captura, y qué contestó de verdad

Lo de arriba estaba leído del fuente. Esto es el cable, capturado con `tcpdump` en el seed mientras esta máquina abría una sala contra él. **212 tramas del protocolo**, parseadas con el formato de `packet_def.rs`.

**El reparto es exacto y no aproximado**, que es lo que lo hace útil:

| Tipo de trama | Cuántas | Cifrada |
|---|---|---|
| `RpcReq` | 55 | **sí** |
| `RpcResp` | 55 | **sí** |
| `Ping` | 48 | no |
| `Pong` | 48 | no |
| Tres tipos del apretón de manos | 6 | no |

O sea 110 cifradas y 102 en claro, y no es que la mitad viaje desnuda al azar: **va cifrado el RPC, y van en claro el latido y el apretón de manos**. Un apretón de manos no puede ir cifrado, porque es lo que establece con qué cifrar, y el `Ping` que se capturó lleva cuatro ceros de payload.

**Lo que NO aparece en toda la captura**, buscado por cadena: el nombre de la sala, el apodo del host, el invite ID en sus dos formas, y las direcciones del rango virtual. Cero coincidencias de cada uno.

**Lo que SÍ viaja en claro son los nombres de red**, los dos:

```
kanpachi-090527755f832ab326cb1940be1cfb24
kanpachi-1c2d56d0d3df5d07c1c1cc640bda2e02
```

Uno es el vestíbulo y el otro la red real de la sala. **Que el seed los vea ya estaba escrito en el modelo de amenazas**, y tiene que verlos para relayar. Lo que la medición agrega es que, sin TLS por encima, **quien esté en el camino ve exactamente lo mismo que el seed**: qué redes hay y quién habla con quién. No es el secreto, y sí es correlacionable en el tiempo.

**La entropía del payload dio 6,58 bits por byte de media**, entre 5,99 y 7,40. Conviene no leer de más ese número: las tramas son cortas, y en una de 64 bytes la entropía no puede pasar de 6 por definición, así que el valor está acotado por el tamaño y no dice que el cifrado sea flojo.

**Y el plano de DATOS, medido aparte porque la primera captura no lo alcanzó.** Aquella era tráfico de control contra el seed y no llevaba una sola trama de tipo `Data`, que es la del juego, porque no había segundo miembro. Con dos peers de verdad en la sala, un host de Windows y un invitado de Linux que llega **por relay**, o sea pasando por el seed:

| | |
|---|---|
| Tramas `Data` capturadas | 16 |
| De ellas, con la marca de cifrado encendida | **16** |
| La cadena inconfundible mandada por la red virtual, buscada en el cable | **0 apariciones** |
| La dirección virtual del host en el cable | **0** |

La aritmética cierra exacta y por eso sirve: 17 `RpcReq` más 17 `RpcResp` más 16 `Data` son las 50 cifradas, y 12 `Ping` más 12 `Pong` más 2 del apretón son las 26 en claro. **Ninguna trama de datos viaja sin cifrar.**

El método fue mandar una cadena que no se parece a nada por la red virtual y buscarla en el cable físico. Es la única forma de contestarlo sin creerle a nadie, y contesta que **el payload de un juego no viaja reconocible**.



### El reposo, Windows contra Linux

Los dos sistemas protegen lo mismo por caminos distintos, y la diferencia importa porque el más permisivo es Windows, que es donde vive casi todo el uso.

| | Windows | Linux |
|---|---|---|
| Directorio de datos | `ProgramData\Kanpachi`, con lectura para los usuarios de la máquina A PROPÓSITO, para que la ventana lea `api.token` sin elevar | `/var/lib/kanpachi`, 0700 de root |
| `identity.key` | La ÚNICA excepción del directorio: ACL propia, SYSTEM y Administradores. Se escribe creando el temporal VACÍO, poniéndole la ACL con nada dentro, y solo entonces la semilla | 0600, dentro de un directorio al que nadie más entra |
| Estado de la sala | Sellado con la llave derivada, MÁS su ACL | Sellado, MÁS 0600 |
| Token de refresco del seed | Igual: sellado y con ACL. Las dos, porque el valor por omisión del directorio es el permisivo | Igual |
| Canal local | Named pipe bajo `ProtectedPrefix\Administrators`, que el usuario interactivo puede ABRIR a propósito, con permiso de hablar y no de crear instancias | Socket 0600 de root en directorio 0700, jamás abstracto, más `SO_PEERCRED` en cada conexión |
| Qué separa a otro usuario del secreto de la red real | El sellado, y nada más: la ACL del directorio lo deja leer el fichero | El directorio ya lo separa. El sellado es defensa en profundidad |

**De esa última fila sale la asimetría que hay que decir en voz alta.** En Windows el canal local se le concede al usuario interactivo para que la ventana no tenga que elevar, así que otro usuario de esa PC puede pedirle al daemon que abra una sala usando el token que ya está guardado, sin conocer jamás el password del seed. El password cierra la puerta a desconocidos de internet y no a quien ya se sentó en esa máquina. En Linux no ocurre, porque ese usuario ni siquiera llega a conectarse al socket.

## Modelo de amenazas, resumen honesto

| Amenaza | Resultado |
|---|---|
| Miembro de la sala comprometido | Alcanza solo los puertos del juego activo en el host. 445/3389/22 cerrados siempre |
| Seed comprometido | Ve networkIDs e IPs públicas. No descifra, no se une, no alcanza servicios |
| Código de sala filtrado | El portador entra hasta que el host renueve el código. **Mitigación activa:** renovar cuesta un click y no expulsa a los presentes. El firewall sigue limitando a los puertos del juego |
| Miembro expulsado que insiste | Revocada su credencial, sale de la red en ~1 s. Vuelve solo si conserva un código vigente, y el host lo cierra renovando |
| Miembro manda basura al canal de control | Es la superficie más seria del producto, y solo existe en la máquina del host. Ver el modelo de amenazas de la decisión 23 |
| Miembro intenta hacerse pasar por el host EN LA SALA | No puede. Los invitados marcan hacia una dirección conocida y no aceptan conexiones entrantes |
| Alguien con el código ocupa la dirección del host EN EL VESTÍBULO | **Ocupar la dirección lo puede; que su respuesta cuele, no.** Las direcciones del vestíbulo siguen siendo autoasignadas, y lo que decide es la firma de la decisión 25: el host firma su respuesta con su llave larga, atada a esa sala y a ese pedido, y el invitado la comprueba contra la llave que el registro fijó para el código. Sin llave fijada no hay contra qué comparar, y ahí queda el límite de antes: renovar el código levanta un vestíbulo nuevo |
| Miembro que deja de recibir para trabar al host | Toda escritura del canal lleva plazo, y vencido se le cierra la conexión |
| Malware local como el usuario | Usa la API igual que el usuario: unirse a salas, aplicar perfiles del catálogo. No puede abrir puertos arbitrarios. Puede leer `hosted-room.json` y con eso entrar a la sala |
| Malware local con admin | Fuera del alcance: con admin ya controla la máquina completa |
| Desconocido de internet quiere hospedar en un seed ajeno | Con el seed cerrado, no puede: las tres rutas que mutan exigen token, y el token sale de un password que el operador reparte. Resolver un código sigue abierto para todos |
| Fuerza bruta contra el password de un seed | Freno por IP mucho más estrecho que el general, retardo global creciente con tope, y la ranura única de Argon2id. Un intento frenado no llega a tocar la derivación |
| Alguien expone el registro sin proxy delante | `X-Forwarded-For` solo se cree cuando la conexión viene de loopback. Publicado directo, el freno cuenta por `RemoteAddr`, que el otro extremo no puede escribir |
| Operador de un seed intenta cosechar passwords | Recibe un SHA-256 con el host de su propio seed dentro, así que lo que aprende no vale en ningún otro sitio, ni siquiera en otro seed suyo |
| Disco robado de una máquina que hospeda en un seed cerrado | Entrega un token de refresco sellado, que caduca y que el operador revoca cambiando el password. **El password no está en el disco** |
| **Otro usuario de la MISMA PC, en Windows** | **El password del seed NO lo cubre**, y hay que decirlo para que nadie lo suponga al revés. El canal local se le concede al usuario interactivo a propósito, para que la ventana hable sin elevar, así que ese usuario puede pedirle al daemon que abra una sala usando el token ya guardado. El password le cierra la puerta a desconocidos de internet, no a quien ya se sentó en esa máquina. En Linux no ocurre: el socket es 0600 de root |
| Alguien captura el tráfico del túnel en el camino | Va cifrado con AES-128-GCM dentro del protocolo del motor, sin TLS por encima ni certificados en ninguna parte. La llave sale del secreto de red por un hash NO criptográfico, así que el respaldo de "32 bytes aleatorios" no llega hasta ella. **Capturado**: el RPC va cifrado, el latido y el apretón de manos no, y en claro viajan los nombres de las dos redes. El nombre de la sala, el apodo, el código y las direcciones virtuales no aparecen |
| Un miembro manda paquetes con la marca de cifrado apagada | El receptor los acepta tal cual, en las cuatro implementaciones de cifrado. Llegar a ser peer exige el digest del secreto de red y su prueba MAC, así que quien puede hacerlo ya está dentro de la sala: misma clase que mandar basura al canal de control |
| **Perfil de catálogo malicioso** | Techo: un peer autorizado alcanza UN puerto tuyo no prohibido, por el túnel, mientras estés en su sala. Jamás exposición a internet. La sección de abajo lo desarma entero |

### El techo de un catálogo trucho, entero

Es la pregunta que hay que poder contestar sin rodeos, porque el catálogo es un fichero editable y el producto acepta perfiles importados: **si alguien te pasa un perfil que dice ser un juego y en realidad pide un puerto suyo, ¿qué consigue?**

**Lo que NO consigue, y es la mitad que la gente supone al revés: exposición a internet.** El puerto que se abre queda acotado al adaptador virtual, al `/24` de la sala y a las direcciones de los miembros PRESENTES. No hay UPnP, no hay reenvío de puertos en el router, no hay exit node ni enrutado de subredes, y ninguna de esas tres existe como opción que un perfil pueda pedir. El router del usuario no se toca nunca. Un perfil no puede hacer que nada de fuera del túnel llegue a esa máquina.

**Lo que sí consigue:** que UN peer autorizado de esa sala alcance UN puerto tuyo no prohibido, a través del túnel, mientras estés dentro de su sala.

**Y la cadena que necesita para llegar ahí son cuatro consentimientos tuyos y una desatención:**

1. **Que importes su catálogo.** Importar pide confirmación dentro de la app, un perfil importado **no puede pisar a uno de fábrica**, y los puertos prohibidos se rechazan al validarlo. Los prohibidos están además tapados por la cuarentena de base, así que un perfil que los pidiera no conseguiría nada aunque el validador fallara: son dos capas, no una.
2. **Que entres a SU sala**, que es otra confirmación, con la tarjeta del registro delante.
3. **Que tengas algo escuchando** en ese puerto no prohibido. Sin oyente detrás, una regla abierta no lleva a ninguna parte.
4. **Que no mires la pantalla de exposición**, que lista ese puerto con su número real y hacia quién está abierto. Es la pantalla por la que existe la app.

**El host malicioso NO puede empujarte el perfil.** Tu máquina abre lo que dice TU catálogo; por el cable viaja el identificador del juego, nunca sus puertos. Un identificador que tu catálogo no conoce se queda sin abrir nada y la sala lo dice.

**La frase que cierra el asunto:** es deliberadamente el mismo techo que invitar a un desconocido a tu LAN física, con una diferencia a favor: acá cada puerto abierto tiene nombre y destinatario, y se leen en `kanpachi exposure` o en la pantalla de exposición. En una LAN física no hay ninguna de las dos cosas.
