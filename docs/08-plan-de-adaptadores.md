# Los adaptadores reales: que el daemon deje de fingir

Actualizado el 2026-08-03, después de una tanda de mediciones que cambió dos
decisiones de fondo y descubrió un agujero que estaba vivo.

## Contexto

La rebanada del pipe cerró en `c71783c`. Todo lo que se puede escribir sin tocar
Windows está escrito y corre en CI: dominio, casos de uso, protocolo, canal de la
sala, named pipe, cableado, catálogo de mensajes y cliente Dart.

Lo que falta es **todo lo que toca Windows de verdad**. Hoy son nueve
provisionales en `daemon/adapter/sinimplementar` que fallan en todo, a propósito:
un provisional que devuelve éxito hace la cuarentena inverificable.

**El orden es libre**, y eso sigue siendo lo mejor de esta fase: cada adaptador
que se escriba deja de fallar y el resto sigue gritando. No hay big bang.

---

# Lo que cambió, y por qué

Todo lo de abajo se **midió**. Los docs eran un levantamiento inicial y varias de
sus afirmaciones no sobrevivieron al contacto con el sistema.

| Se creía | Se midió |
|---|---|
| El portal RPC del motor va fijado a `127.0.0.1` | El default es `0.0.0.0:15888`, con un config que ni lo nombra |
| El motor es `easytier-core.exe` | Es un binario **propio** sobre la librería, y así el portal no existe |
| `Packet.dll` y `WinDivert64.sys` no los usa nada | `packet.dll` es importación **dura**; el motor no arranca sin él |
| La API del firewall no filtra por interfaz | `INetFwRule::Interfaces` existe, y Microsoft la usa en producción |
| Tailscale usa WFP para ser invisible | Deja reglas visibles. Sus motivos publicados son otros tres |
| Las menciones a Apache-2.0 son metadatos rancios | EasyTier **fue** Apache-2.0 hasta 2025-06-07 |
| `AuditForeign` cubre las reglas ajenas | Solo mira el ejecutable del **juego activo** |

Commits: `78c0e26`, `ec371d9`, `0d06433`.

---

# 0. El agujero que está vivo hoy

**Va primero, son ~30 líneas, y no depende de nada de lo demás.**

```go
AuditForeign(ctx context.Context, p domain.GameProfile) ([]domain.ForeignRule, error)
```

Busca por la ruta del ejecutable **del juego activo**. O sea que una regla
permisiva de **Parsec, Sunshine, Moonlight, RustDesk, AnyDesk o TeamViewer nunca
se audita**.

Ese es el único camino conocido por el que un miembro de la sala, o alguien
expulsado que vuelve porque el código no es secreto y no hay baneo, consigue
**teclado, pantalla y sistema de archivos** del host. La invariante del producto,
violada entera, con Kanpachi funcionando impecable.

Los puertos que dan escritorio remoto **estándar** ya están tapados por
`forbiddenPorts` (22, 135, 137-139, 445, 3389, 3702, 5357-5358, 5985, 5986). Los de estas
herramientas son **arbitrarios y configurables**, así que no hay lista negra que
sirva.

**Qué se hace ahora:** `AuditForeign` busca además por una lista corta de
ejecutables de acceso remoto. No cierra el agujero, lo hace **visible**, que es
lo mínimo que la decisión 19 exige. Cerrarlo de verdad viene en el paso 4 del
firewall, y solo sobre el adaptador virtual.

Va primero por ser barato y ortogonal, jamás porque el resto esté lejos en el
tiempo. Todo el firewall se escribe seguido.

---

# Qué se puede escribir y qué se puede PROBAR

Son dos cosas distintas y conviene no confundirlas.

**Se escribe entero, de corrido.** El código no espera a nada.

**Lo que NO se puede verificar en la máquina de desarrollo:**

| Necesita | Por qué |
|---|---|
| Consola **elevada** | Solo para ESCRIBIR en WFP, y eso corrige lo que decía acá |
| **Segunda máquina** | Las mediciones A y B, y la prueba del conjunto |
| ~~**Seed alcanzable**~~ | **RESUELTO el 2026-08-03.** Se apagó el proxy de Cloudflare y `kanpachi.accentio.dev` sirve la página en 443 y el motor en 11010, por el mismo nombre |
| Adaptador `kanpachi0` **vivo** | El LUID, y el segundo puerto de magic DNS, que solo aparece con TUN levantado |

Todo lo que dependa de esos cuatro queda escrito y **marcado**, con la prueba
concreta que lo confirma. Nada se da por bueno sin medirlo.

**Dónde está de verdad la frontera de la elevación**, medido el 2026-08-04
corriendo el adaptador sin elevar:

| Llamada | Sin elevar |
|---|---|
| `FwpmEngineOpen0` | funciona |
| `FwpmFilterGetByKey0`, filtro **ausente** | funciona: contesta `FWP_E_FILTER_NOT_FOUND` |
| `FwpmFilterGetByKey0`, filtro **puesto** | `0x5`, o sea `ERROR_ACCESS_DENIED` |
| `FwpmTransactionBegin0` | `0x5` |

El renglón del medio costó **dos conclusiones equivocadas seguidas**. La primera
medición se hizo con la máquina limpia, contestó "no encontrado", y de ahí salió
escrito que leer no exige administrador. Con la compuerta puesta contesta `0x5`.
O sea que leer un filtro que existe también exige elevación, y lo anterior
funcionaba solo porque no había nada que leer. Es el caso de manual de por qué
una ausencia no puede ser una medición.

**Lo que salva esto de ser un verde falso** es que los dos casos llegan con
códigos distintos. Si compartieran código, una medición sin elevar informaría la
compuerta como ausente teniéndola puesta. Como no lo comparten, el adaptador
contesta SIN COMPROBAR, que es la verdad.

Consecuencia de producto: la pantalla de exposición **no** puede medir la
compuerta sin elevar. Quien la mide es el daemon, que corre como servicio del
sistema.

---

# El motor: `kanpachi-engine.exe`

**Decidido y probado.** Binario propio en Rust, en un repositorio aparte bajo
LGPL-3.0, que declara EasyTier como **librería** por `git` contra el tag
**`v2.6.4`**, ejecutado como proceso hijo.

## Lo que se midió antes de comprometerse

Mismo fichero de configuración, misma máquina, mismo momento:

| Proceso | Sockets en escucha |
|---|---|
| `easytier-core.exe` v2.6.4 | `TCP 0.0.0.0:15888 LISTENING` |
| Binario propio sobre la librería | **ninguno** |

Ese portal **no tiene autenticación de ninguna clase**, y por ahí cualquier
proceso local emite credenciales de la red real, agrega nodos, reenvía puertos, y
pide el `network_secret` en claro. La autenticación se pidió upstream y se
**rechazó a propósito**.

`ApiRpcServer::new` aparece en **un solo sitio** de todo el árbol, dentro del
binario de línea de comandos. El arranque por librería no lo menciona: **el
portal desaparece por omisión, no por configuración.**

## Lo demás que se verificó funcionando

- **Credenciales**: `peers/credential_manager.rs` expone `generate_credential`,
  `revoke_credential` y `list_credentials` como `pub fn`. Sin portal.
- **Eventos push**: `NetworkInstance::start()` devuelve un `EventBusSubscriber`.
  El camino oficial lo **descarta**; el nuestro se lo queda.
- **Estado**: `get_running_info()` llama a los servicios en proceso, sin socket.
  Peers, rutas, node info, STUN, tipo de NAT.
- **Secreto fuera de `argv`**: la config entra por **stdin como TOML**. Medido:
  el `CommandLine` del hijo solo muestra la ruta del ejecutable.

## El fork: por qué se descartó, y por qué después hizo falta igual

Lo que se descartó, y sigue descartado, es **seguir una rama viva parcheándola**.
Entre `v2.6.4` y la rama de desarrollo hay **606 ficheros y +129.000 líneas** en
un solo cambio, y el árbol del RPC se borró y renació en otro sitio: esos parches
habría que **reescribirlos**, no rebasarlos.

Lo que se hizo es otra cosa, y el argumento de arriba no la toca: un fork
**fijado al mismo tag** que ya se consumía, tocando **un solo fichero de
fuente**, con 31 líneas borradas y 8 de comentario en su lugar. No hay rama que
seguir y no hay nada que reescribir; subir de versión sigue siendo un acto
deliberado, y ahora incluye reponer dos borrados.

Hizo falta porque apareció algo que consumir la librería no quita. El portal
`15888` desaparecía por omisión porque lo construye el **binario** oficial
alrededor de la librería. Escribir en el firewall lo hace la **librería misma**,
dentro de `NetworkInstance::start()`, sin bandera ni configuración que lo apague.
Ver `03-arquitectura.md`, sección del motor, y la decisión 1 en `02`.

**El motor no vive dentro del fork, y es deliberado.** El valor entero del fork
es que su diff se lea de un vistazo: "es upstream y nada más" o se comprueba en
treinta segundos con `git diff v2.6.4 v2.6.4-kanpachi.1`, o es un acto de fe. Con
el motor dentro, ese fichero quedaría mezclado con dos mil líneas nuestras, y
cada subida de versión traería además conflictos en su workspace, su
`rust-toolchain.toml` y su CI, que no tienen nada que ver con lo que se cambia.

## Por qué NO cgo

Tres motivos independientes: cgo exige mingw y EasyTier solo compila y testea
`-msvc`; el workspace usa `panic = "abort"`, así que un panic del motor mataría
al daemon que corre como SYSTEM; y es la única opción que mete al daemon Go
dentro de la Combined Work de la LGPL.

## El build, con las trampas ya resueltas

Seis, todas encontradas a los golpes:

| Síntoma | Causa | Arreglo |
|---|---|---|
| `zstd-sys` no compila, `VCINSTALLDIR = None` | `cc` halla `cl.exe` sin entorno MSVC | correr bajo `vcvars64` |
| `prost-wkt-types` panic | el `PROTOC` que baja `build.rs` vale solo para él | `PROTOC` global |
| `kcp-sys` panic | usa bindgen, necesita `libclang` | instalar LLVM y fijar `LIBCLANG_PATH` |
| `zstd-sys` falla con entorno correcto | ruta de ~250 chars; **`cl.exe` no es long-path aware** | build en ruta corta |
| `7z is needed to unpack libraries!`, panic del `build.rs` de easytier | `thunk-rs`, que easytier invoca en Windows x86 y x86_64 | instalar 7-Zip y meter `C:\Program Files\7-Zip` en el PATH |
| `LNK1181: cannot open input file 'Packet.lib'` | **la ruta de búsqueda que emite easytier es RELATIVA** | `build.rs` propio que emite la ruta absoluta |

**La sexta es la que decidía si el enfoque entero servía, y ya está medida.** El
`build.rs` de EasyTier emite, literal:

```
println!("cargo:rustc-link-search=native=easytier/third_party/x86_64/");
```

Cargo corre el linker desde la raíz del paquete que se está construyendo, o sea
el repo del motor, así que esa ruta apunta a un sitio que no existe. Todo el
árbol compila y **falla solo el enlace final**, con la línea del linker mostrando
las dos mitades del problema juntas:

```
"/LIBPATH:easytier/third_party/x86_64/"   <-- relativa
"Packet.lib"                              <-- pedida igual
LINK : fatal error LNK1181: cannot open input file 'Packet.lib'
```

**El arreglo NO es copiar el fichero al repo.** Eso resolvería la ruta relativa y
metería un binario de linaje WinPcap/Npcap, con términos de redistribución que
nadie revisó, dentro de un repositorio público. En su lugar hay un `build.rs`
propio que localiza el checkout que cargo ya desempaquetó y emite la ruta
**absoluta**. No redistribuye nada. El sitio se busca en vez de escribirse porque
la ruta del checkout lleva un hash que cargo considera detalle interno; hay
además una variable de escape, `KANPACHI_ENGINE_LINK_SEARCH`, para árboles
vendorizados.

## Lo que la fase 0 midió, y lo que NO

Con una sonda de tres líneas que solo nombra un tipo de la librería:

| Pregunta | Resultado |
|---|---|
| ¿El crate enlaza como dependencia git desde otro workspace? | **Sí**, con el `build.rs` propio. 590 paquetes, exit 0 |
| ¿El binario arranca? | **Sí**. `inst_name=default`, exit 0 |
| ¿Resuelven `Packet.dll`, `wintun.dll` y WinDivert en ejecución? | **NO MEDIDO** |

La tercera fila hay que leerla, porque es fácil creer lo contrario. `dumpbin
/imports` sobre la sonda da `kernel32`, `bcryptprimitives`, `ntdll`,
`api-ms-win-core-synch` y `msvcrt`, y **nada más**: el linker descartó el código
nativo que nadie llama, así que la sonda corrió sin ninguna de las tres DLL al
lado. Eso prueba el enlace y no prueba el empaquetado. La medición de verdad
llega con el motor arrancando una `NetworkInstance`, en la fase 3.

Ese `msvcrt.dll` en la tabla, en lugar de la UCRT moderna, es VC-LTL haciendo su
trabajo: confirma medida la base de Windows 7 del punto anterior.

**La quinta hay que leerla despacio, porque no es una molestia de instalación.**
El `build.rs` de EasyTier llama a `thunk::thunk()` sin condición ninguna en
Windows x86_64, y eso **descarga 17,8 MB de la red durante la compilación** y los
descomprime con `7z`. O sea que compilar el motor trae un binario de terceros que
no está en ningún manifiesto, sobre un canal que nadie fijó, en la máquina de
quien compila. Consecuencias, en orden de importancia:

- **El CI necesita red dentro del build**, no solo para bajar crates. Un fallo de
  ese servidor rompe la compilación sin que nada del repo haya cambiado.
- **El motor queda construido contra una base de Windows 7.** Verificado en el
  `Cargo.toml` del checkout, línea 328: `thunk-rs` entra con
  `features = ["win7"]`. Eso enlaza VC-LTL5 5.2.2 con la plataforma
  `6.0.6000.0` y el objeto `YY_Thunks_for_Win7.obj` de YY-Thunks 1.1.7, o sea
  una CRT de la era Vista más una capa de compatibilidad. Kanpachi es Windows 10
  y 11: **no aporta nada y viaja igual.** Quitarlo exige parchear a EasyTier, que
  es justo lo que la decisión 1 descarta, así que se documenta y se acepta.
- **Es material para `NOTICE.md`.** Son terceros con licencia propia dentro del
  artefacto que se distribuye.

Toolchain medido el 2026-08-05 en el equipo de desarrollo: rustup 1.29.0 con
`1.95.0` fijado por el repo del motor (conviviendo con el `stable` 1.97.1 de la
máquina, que no se usa), MSVC 14.44.35207, protoc 35.1, libclang 22.1.8, 7-Zip
26.02. Son **590 paquetes**. En CI: `windows-latest` más `ilammy/msvc-dev-cmd`,
gratis en repo público.

## Alcance del repo del motor

1. `main.rs` que lee TOML por stdin, arranca la instancia y emite eventos y
   estado por stdout como JSON por líneas.
2. **Test de invariante en CI**: arrancar con la config real, **con TUN
   levantado**, y fallar si aparece **cualquier** socket en LISTEN.

   Este test existe porque me equivoqué. Dije que había un solo sitio donde se
   abre un puerto y había **dos**: el segundo es el de magic DNS, en
   `127.0.0.1:49813`, y mi primera prueba no lo vio porque usó `no_tun = true`.
   Ese puerto solo aparece con `accept_dns`, que viene en `false` y que Kanpachi
   ya prohíbe. La lección no es cuál gana: es que **la ausencia de puertos no
   puede ser una creencia**.
3. **Features de cargo reducidas, y el resultado está medido.** El default trae
   `socks5`, `magic-dns`, `wireguard` y `faketcp`, que Kanpachi no usa. Compilar
   sin ellas **borra la capacidad del binario**, que es más fuerte que apagarla
   con una bandera: una bandera deja el código dentro y confía en la bandera.

   Comprobado con `cargo tree -i` sobre el árbol ya reducido, que es lo único
   que distingue una feature quitada de una que uno cree haber quitado:

   | Crate | Qué traía | Estado |
   |---|---|---|
   | `hickory-server` | el servidor de magic DNS | **fuera del grafo** |
   | `boringtun-easytier` | el portal WireGuard de `--vpn-portal` | **fuera** |
   | `flume` | faketcp | **fuera** |
   | `windivert` | el driver de captura de paquetes | **sigue dentro** |

   La primera fila es la que más vale: magic DNS es lo que abre el puerto de
   loopback que la primera medición de sockets no vio. Ya no está apagado, no
   está.

   **La última fila es la limitación, y hay que leerla.** `windivert` es una
   dependencia incondicional en Windows x86 y x86_64, así que ninguna
   combinación de features lo saca. Por eso la cuarentena del producto no se
   apoya en que el motor sea incapaz: se apoya en el firewall.

   `tun` se queda por definición, y `enable_encryption` NO depende de la feature
   `wireguard`: esa feature es `boringtun`, o sea el servidor de VPN, y el
   cifrado del túnel es `encryption_algorithm`, que sigue puesto. Confundirlas
   habría sacado el cifrado creyendo sacar un portal.

   La combinación reducida compila: 5 min 32 s, exit 0, y el protocolo contesta
   igual en la prueba de humo.
4. Los ficheros de licencia: aviso, copia de LGPL-3.0 **y** de GPLv3 (EasyTier no
   incluye la segunda), y enlace al tag. **No es deuda de hoy**: se comprobó que
   `third_party/` está en `.gitignore`, que no hay instalador, y que la ruta
   `/descargar` sirve la misma página HTML. Hoy no se distribuye nada.

## Los tres fallos que solo aparecieron al correrlo

Ninguno de los tres lo habría encontrado un test unitario, y los tres eran
graves. Se anotan porque la lección es la misma en los tres y vale más que los
arreglos.

| Síntoma | Causa | Qué se veía |
|---|---|---|
| Un mensaje ilegible no recibía respuesta | Al fallar el parseo no se recuperaba el `id` | El llamador esperaba para siempre una respuesta que no iba a llegar |
| El motor no terminaba al cerrarse stdin | `Engine` guarda un clon del emisor, así que soltar el que tenía el bucle no cerraba el canal | Dos motores vivos minutos después, con la red virtual arriba |
| **El motor moría un instante después de arrancar** | `exec.CommandContext` con el contexto de la LLAMADA, que lleva su propio `defer cancel()` | `HostNetwork` devolvía éxito y la red se caía sola, con un `exit status 1` que no dice nada |

El tercero es el peor de los tres y el más instructivo. **La orden se respondía
con éxito**, porque la respuesta llegaba antes de que corriera el `cancel`. Un
test del adaptador con un proceso de mentira habría pasado en verde, porque el
fallo no está en lo que se manda ni en lo que se contesta: está en cuánto vive
el proceso, y eso solo se ve arrancando uno de verdad y mirándolo un rato
después.

Y un cuarto, este del adaptador contra el supervisor:

**`Events()` devolvía un canal CERRADO mientras no hubiera proceso**, y el
supervisor lee un canal cerrado como muerte del motor, cosa que dice en su propio
código. O sea que "todavía no arrancó" y "se murió" eran el mismo hecho para
quien escucha. Con el daemon recién arrancado y sin ninguna sala, el watchdog
gastó sus ocho intentos y **cerró una sala que el usuario nunca creó**. Ahora el
canal vive lo que vive el adaptador y la muerte viaja como `EngineDied`.

## Lo que ya está resuelto y hay que sostener

- **Job Object** con `KILL_ON_JOB_CLOSE`: el motor muere con el daemon. Sin eso
  quedan huérfanos con la red arriba y el firewall ya purgado.
- **`--peers` con la dirección ya resuelta**, jamás con el nombre. Pasa por
  `domain.CheckSeedAddr`, que está escrita, probada, y **no la llama nadie**.
- **El secreto no vuelve a core.** `Restart` reusa la última especificación
  guardada dentro del adaptador.
- **Entorno explícito**, nunca heredado. Cada bandera tiene gemela `ET_*`, y
  `--disable-env-parsing` **no** cubre esto: su ayuda dice *"in config file"*, o
  sea la interpolación dentro del fichero.

---

# El firewall: híbrido, y en este orden

**Decidido.** Los permisos se quedan en COM y visibles. Se suma un sublayer
propio de WFP con un `blockAll` acotado al adaptador. **Intersección, no
reemplazo.**

## Por qué funciona

Por defecto en WFP: un filtro **Block es hard** (*"cannot be permitted at another
sub-layer"*) y un filtro **Permit es soft** (*"can be blocked at another
sub-layer"*).

La regla permisiva de Parsec vive en el sublayer de Windows y es un soft permit.
Un hard block nuestro sobre `kanpachi0` **la anula**, sin tocarla y sin pedirle
nada al usuario.

## Lo que NO se rompe, y es el punto

- El grupo `Kanpachi` **sigue existiendo y siendo visible**. `wf.msc`,
  `Get-NetFirewallRule` y toda la base de conocimiento siguen sirviendo.
- **El usuario conserva el veto**: sin hard permits ni sublayer máximo, sus
  bloqueos siguen ganando sobre Kanpachi. La decisión 4 se sostiene tal cual.
- **No es todo o nada.** El `blockAll` entra solo y se prueba.
- **Reiniciar el servicio no corta la partida.** Los flujos se reautorizan y los
  permisos COM siguen vivos.
- Si el filtro deja de casar, **degrada a lo de hoy**, nunca por debajo.

## El reparto por capa

| Capa | Quién | Qué |
|---|---|---|
| Daemon, en cada arranque | COM, grupo `Kanpachi-base` | Los 12 puertos prohibidos en las dos direcciones, **todas** las interfaces. Solo agrega: no existe el método para borrarla. |
| Daemon, permisos | COM, grupo `Kanpachi` | `Apply(RuleSet)` igual que hoy. Son los que **abren**. Se suma `Interfaces = ["kanpachi0"]`. |
| Daemon, compuerta | Sesión WFP propia | `blockAll` sobre `kanpachi0`, más permisos espejo soft del **mismo** `RuleSet`. Solo en `ALE_AUTH_RECV_ACCEPT_V4/V6`. |

**La sesión NO acabó siendo dinámica**, que es lo que decía este plan antes de escribirla. Con sesión dinámica los filtros se borran al morir el proceso, y eso quita justo lo que hace falta: que una muerte sucia del daemon deje la sala contenida y no abierta, y que la limpieza al arrancar sea una operación de verdad. La red de seguridad que aporta el reinicio se conserva igual, porque la sesión tampoco es persistente. Ver decisión 27.

**Nunca en `ALE_AUTH_CONNECT`**: bloquear la salida impediría que el invitado
marque al host.

## Un bloqueante documental que hay que arreglar antes: HECHO

`docs/04` paso 8 le pedía al instalador bloquear **"sobre la IP del adaptador"**.
El instalador no puede saber esa IP: la `/24` se elige **por sala, en tiempo de
ejecución**. Corregido en `docs/04` y en la tabla de la decisión 4. La cuarentena
de base no se acota ni por IP ni por adaptador, y la razón ya está escrita en
`core/domain/policy.go`: un bloqueo acotado que deja de casar **abre**.

## El orden

1. **Paso 0**: el arreglo de `AuditForeign` de arriba. ~30 líneas, sin WFP.
2. **Paso 1, core**: `domain.AppliedRule`, `domain.Enforcement`, la función pura
   de diff. `OwnRulesIntact(bool)` muere y nace `Enforcement()`, que devuelve lo
   **medido** en las dos capas y deja que juzgue el dominio. Aterriza **antes**
   de una línea de adaptador.
3. ~~**Paso 2, spike fuera del repo**~~: **HECHO el 2026-08-03 y salió que sí.**
   Ver "La prueba que lo decide" más abajo. El bloqueo duro le gana al permiso
   vivo del Firewall de Windows.
4. ~~**Paso 3, piso de WFP**~~: **HECHO.** Salió sin vendorizar nada:
   `wireguard-windows` habría traído su propio árbol de tipos por cinco
   condiciones y dos capas, y las llamadas ya estaban medidas en el spike. Son
   `fwpuclnt.dll` por `LazySystemDLL` y las estructuras escritas a mano.
5. ~~**Paso 4, el `blockAll` solo**~~: **HECHO**, y no entró solo: pasos 4 y 5
   son el mismo `Apply` porque la reescritura es del conjunto entero dentro de
   una transacción, y partirla habría dejado una ventana con el bloqueo puesto
   y los permisos sin poner. **Medido funcionando el 2026-08-04.**
6. ~~**Paso 5, permisos espejo**~~: **HECHO.** El diff por `providerKey` que
   pedía este plan no hizo falta: reescribir entero dentro de la transacción es
   más simple, repara lo que alguien borró por fuera, y ahorra la superficie de
   API de enumerar. **Medido funcionando el 2026-08-04**: un permiso propio SOFT
   sobrevive junto al bloqueo propio HARD, y el veto del usuario le sigue
   ganando a los dos.
7. ~~**Paso 6**~~: **HECHO.** `Enforcement()` en la pantalla con marca de cuándo
   se midió, y el botón que vale más que toda la pantalla, que corre en el
   invitado contra la IP virtual del host.

   Lo que cambió al medirlo, y era una pieza del diseño: se creía que un puerto
   permitido sin oyente devuelve RST, así que un rebote distinguiría "el firewall
   lo deja pasar" de "el firewall lo bloquea". **Es falso en Windows.** Medido el
   2026-08-04 con la misma regla y el mismo puerto: con regla y sin oyente,
   silencio; con regla y con oyente, conecta. Es el modo sigiloso del Firewall de
   Windows, y de ahí salen dos consecuencias:

   - **El veredicto "faltan puertos" no existe.** Un puerto de juego callado
     significa que el juego no está abierto, que es el estado normal mientras
     alguien mira la pantalla. Se habría encendido en falso siempre.
   - **Hace falta una referencia viva**, y por eso el sondeo va contra el host:
     el canal de la sala es el único puerto que se sabe abierto. Sin él, "no
     contesta nadie" se lee igual con la máquina blindada que con la máquina
     apagada.

   La primera corrida encontró algo real en la máquina de desarrollo: el 445
   contestó desde otra PC. El compartir archivos de Windows, alcanzable por la
   red virtual, que es exactamente el escenario que este producto existe para
   evitar.
8. ~~**Paso 7, guardián nuevo** en `internal/arch`~~: **HECHO**, y encontró tres
   cosas que este plan no había previsto. Un filtro sin alcance **no falla en
   ningún test** y aplica a todos los adaptadores de la máquina; con hard block
   eso deja al usuario sin red de casa. Lo que apareció al escribirlo:

   - El propio guardián estaba roto. Eximía por nombre de fichero y no por ruta,
     y en `daemon/` hay varios `spec.go`, así que pasaba en verde con el literal
     prohibido delante. Solo lo encontró envenenarlo.
   - `Scope.Valid()` aceptaba `0.0.0.0/0`. Un prefijo relleno no es un prefijo
     que acote, y ese caso caminaba por delante del guardián.
   - Las claves salían de la etiqueta, o sea que cambiar de juego dejaba
     huérfanos los filtros del juego anterior. Ahora salen de la ranura.

## Lo que NO se hace, decidido explícitamente

No se mudan los permisos a WFP. No se usan hard permits. No se usa el sublayer
máximo. No se vacía el grupo COM. No se borra `SuspendForeign`, que sigue
sirviendo para la LAN de casa. No se intenta cuarentena WFP persistente en el
instalador: BFE la deshabilita en silencio si el servicio no es auto-start.

## La prueba que lo decide: HECHA, y sale que SÍ

**Medido el 2026-08-03 con dos máquinas reales en la misma LAN.** La apuesta del
diseño híbrido está confirmada.

El spike se replanteó antes de correrlo, y ese replanteo es la mitad del
resultado. La pregunta que decide todo es de **arbitraje de WFP**, no de red
virtual, así que no hace falta ni el motor, ni una sala, ni el seed: se contesta
igual sobre el adaptador físico. Eso convirtió una prueba bloqueada por tres
cosas en una prueba de media hora.

| Paso | Estado del host | Desde la otra PC |
|---|---|---|
| 1 | permiso del Firewall puesto, sin filtro | **CONECTA**, 6 ms |
| 2 | permiso puesto **y filtro WFP puesto** | **NO CONECTA**, timeout |
| 3 | filtro quitado, permiso puesto otra vez | **CONECTA**, 20 ms |

**Va y VUELVE, y eso es lo que la hace evidencia.** Un solo "no conecta" no
distingue el bloqueo de un cable suelto, del Wi-Fi, o del listener caído. El
paso 3 los descarta a los tres.

Durante el paso 2 se comprobó en el host, desde fuera del spike: el listener
seguía vivo, los dos permisos seguían activos, y había **cero** reglas de bloqueo
en el Firewall de Windows. Lo único que podía tirar ese paquete era el filtro.

**Conclusión: un Block duro en un sublayer propio anula un Permit vivo del
sublayer de Windows, sin tocarlo y sin pedirle nada al usuario.** Es la
asimetría documentada por Microsoft, y ahora también medida acá.

### Las tres que faltaban, medidas el 2026-08-04

Las cuatro salieron del lado bueno. **El diseño híbrido queda validado entero.**

**1. La condición de interfaz SÍ se aplica.** Era el riesgo que podía dejar todo
el trabajo en cero.

| Alcance del bloqueo del 45871 | 45871 | 45872 |
|---|---|---|
| `-adapter "Wi-Fi"`, por donde llega el invitado | **NO CONECTA** 6.001s | CONECTA 6ms |
| `-adapter "Tailscale"`, otro adaptador | **CONECTA** 8ms | CONECTA 5ms |

Mismo filtro, mismo puerto, misma máquina: lo único que cambió fue el LUID y el
resultado se dio la vuelta. Una sola variable.

De regalo, la fila de arriba prueba que la condición de PUERTO y la de INTERFAZ
se combinan bien: el 45872 siguió conectando mientras el 45871 estaba bloqueado.
Si el filtro casara con todo, habrían caído los dos.

**2. El `blockAll` con permiso espejo funciona, y las dos mitades importan.**

| Puerto | Resultado con `blockall -adapter "Wi-Fi" -except 45871` |
|---|---|
| 45872 | **NO CONECTA** 6.001s, con el permiso del Firewall de Windows vivo |
| 45871 | **CONECTA** 6ms, por el permiso espejo SOFT |

Que el 45872 caiga prueba el bloqueo total. Que el 45871 pase prueba que un
permiso propio SOFT sobrevive junto a un bloqueo propio HARD, que es la parte de
la que depende que el juego pueda abrir su puerto y que nadie había comprobado.

**3. El veto del usuario sigue ganando.** Con el `blockAll` y el permiso espejo
puestos, se añadió un bloqueo del usuario en su Firewall sobre el 45871: pasó de
conectar a **NO CONECTAR**. Otra comparación de una sola variable, con nuestro
permiso intacto. La decisión 4 se sostiene.

### Un control que apareció solo

Durante las mediciones 2 y 3 había además dos reglas `Query User{...}` de tipo
Allow que el diálogo de Windows había creado sobre el binario. **Eso hace el
resultado más fuerte y no más débil**: había TRES permisos cubriendo esos puertos
y el `blockAll` les ganó igual.

Y dejó un control perfecto sin buscarlo: el 45871 y el 45872 tenían exactamente
la misma cobertura en el Firewall de Windows, y lo único que los diferenciaba era
nuestro permiso espejo. Uno conectó y el otro no, lo que aísla ese permiso como
la única causa posible.

Se confirmaron vivas por otro camino, y este cerró el círculo entero. Tras un
`clean` el invitado **seguía conectando**, porque esas reglas cubren el binario
completo en cualquier puerto y `clean` no las conoce: no son suyas. Quitadas a
mano, y con el listener todavía vivo y sin ningún permiso puesto, el invitado
**dejó de conectar a los dos puertos**.

Eso confirma tres cosas de golpe: que esas reglas eran lo que dejaba pasar el
tráfico, que el deny-all por defecto de Windows está operando de verdad, y que la
máquina quedó limpia según una medición externa y no solo según lo que dice la
propia herramienta.

### Lo que sigue sin probarse

1. **La reautorización de un flujo YA existente.** Lo medido son conexiones
   nuevas. El riesgo escrito era más fino: que la condición de interfaz llegue
   vacía al reautorizar, por ejemplo tras reiniciar el servicio o al cambiar el
   adaptador. La mitigación de emitir el bloqueo dos veces, por LUID y por
   dirección local del `/24`, sigue valiendo como cinturón y tirantes.
2. **No se usó Parsec de verdad**, sino un listener propio. Para el arbitraje da
   igual, porque una regla por programa y una por puerto viven las dos en el
   sublayer de Windows, y eso no cambia quién gana.

### Lo que se aprendió de la herramienta, y vale para el piso

- **Los códigos de error de WFP no se ponen de memoria.** Dos de tres estaban
  mal: `FILTER_NOT_FOUND` es `0x80320003` y no `0x...08`, y `SUBLAYER_NOT_FOUND`
  es `0x80320007` y no `0x...05`.
- **El HRESULT vuelve con el signo extendido en x64.** `0x80320003` llega como
  `0xFFFFFFFF80320003`, así que comparar el `uintptr` crudo contra la constante
  no casa jamás, y un "ya existe" o un "no encontrado" se leen como fallo duro.
  Hay que recortar a 32 bits.
- **El diálogo del Firewall de Windows 11 es modal y obliga a elegir**, y las dos
  respuestas contaminan una medición: "Permitir" crea reglas que la herramienta
  no controla, y **"Cancelar" crea reglas de BLOQUEO** que le ganan al permiso.
  La salida es poner una regla del propio binario ANTES de que escuche, y así el
  diálogo no llega a salir. Comprobado en las dos direcciones: quitando esa
  regla, el diálogo vuelve.
- **Una sesión no dinámica y no persistente** es la combinación correcta para
  esto: sobrevive al proceso, así que abrir y cerrar pueden ser dos corridas
  distintas, y muere en el reinicio, que es la red de seguridad final.

---

# Lo que queda abierto, y son decisiones tuyas

1. **El uso legítimo de escritorio remoto.** Si alguien usa Parsec o Moonlight a
   propósito entre sus propias máquinas y están en la sala, el `blockAll` lo mata
   sobre la IP virtual. El síntoma es *"anda por la LAN y no por Kanpachi"*, sin
   regla que mirar. Hay que decidir si existe una vía para permitirlo desde la
   UI, y esa vía es justo lo que el modelo está para impedir.
2. **El seed ya es alcanzable, y queda una decisión de infra.** Se apagó el proxy
   de Cloudflare el 2026-08-03 y `kanpachi.accentio.dev` resuelve a la Reserved
   IP del droplet, `45.55.123.251`: 443 sirve la página con su Let's Encrypt de
   NPMplus, y 11010 llega al motor. **Un solo nombre sirve las dos cosas**, que
   es lo que `DefaultSeedHost` asume hoy.

   Lo que queda abierto es que el DNS en gris **no gusta** y se acepta por ahora.
   Volver a encender el proxy rompería el 11010, porque Cloudflare solo reenvía
   una lista fija de puertos HTTP. La alternativa es separar el nombre del
   registro del nombre del motor, y eso **sí toca el dominio**: hoy los dos salen
   de la misma constante. Que toque código no es un argumento en contra.
3. **La categoría de red del adaptador.** `docs/04` paso 6 dice Privada, con una
   razón de UX real. Sigue sin resolverse contra el argumento de que la sala son
   desconocidos con un ticket.
4. **La contradicción de `policy.go`.** Hoy dice que acotar un bloqueo por
   interfaz abre. **Sigue siendo cierto.** Lo que cambia es la consecuencia,
   porque los permisos COM quedan debajo. Hay que escribirlo así o en seis meses
   alguien lo "arregla" al revés.
5. **El riesgo que podía dejar el trabajo en cero, ahora acotado.** Medido el
   2026-08-04: la condición de interfaz **sí se aplica** en conexiones nuevas,
   comprobado por los dos lados, acotando a la interfaz por la que llega el
   invitado y a otra distinta. Lo que queda sin medir es más estrecho: que la
   condición llegue vacía al **reautorizar un flujo YA existente**, tras
   reiniciar el servicio o al cambiar el adaptador. **La mitigación ya está
   escrita**: el `blockAll` sale dos veces, por LUID y por dirección local sobre
   el `/24` de la sala, con un test que falla si alguien deja uno solo.

   Y ahora hay un segundo cerrojo, que es el importante: **el sondeo desde otra
   PC**. Ese fallo es invisible desde dentro por definición, porque la pantalla
   lee lo que esta máquina tiene configurado y la configuración seguiría
   impecable. Sondear desde fuera es la única comprobación que puede desmentirla,
   y la pantalla la ofrece.

---

# El resto de los adaptadores

| # | Adaptador | Desbloquea | Riesgo | Estado |
|---|---|---|---|---|
| 1 | **Motor** | Todo lo demás | Alto | **HECHO** |
| 2 | **Firewall** | La promesa central | Alto | **HECHO**, y su compuerta sin cablear, ver abajo |
| 3 | **RoutingTable** | Que se pueda elegir subred, o sea crear una sala | Bajo | **HECHO** |
| 4 | **netcfg** | Que el túnel sea usable | Medio | **HECHO** |
| 5 | **RoomDirectory** | Crear y entrar con código | Medio | pendiente |
| 6 | **Auditoría** | Que las alertas digan la verdad | Bajo | pendiente |
| 7 | **SystemEvents** | Que los ajustes sobrevivan a Windows | Medio | pendiente |
| 8 | **Steam** | Comodidad. Ordena, jamás filtra | Bajo | pendiente |
| 9 | **iphlpapi** | Solo el creador de perfiles | Bajo | pendiente |
| 10 | **Rendezvous** | Argon2id local puro | Bajo | pendiente |

El 10 es Go **puro**, sin red y sin Windows, así que es el más barato y entra en
la lista de `puros` del guardián de arquitectura.

`RoutingTable` salió antes de lo previsto porque era lo que faltaba para que
`create_room` llegara a alguna parte: fallaba en `planSubnet`, antes de tocar el
motor. Su riesgo bajó de medio a bajo al escribirlo, porque todo lo que decide
resultó ser filtrado puro y se prueba en Linux.

## Los dos hallazgos que aparecieron al preparar el túnel

Los dos medidos en la máquina de desarrollo, y ninguno visible desde dentro del
programa antes de mirar.

**1. El motor escribía en el firewall.** Ocho reglas de permiso al crear el
adaptador, desde dentro de la librería. Una abría el adaptador virtual a todo, y
otra permitía cualquier protocolo entrante hacia el ejecutable **en todas las
interfaces**. Esa segunda es la que decidió el enfoque, porque la compuerta no
puede taparla sin dejar de estar acotada al adaptador, que es lo único que impide
dejar al usuario sin su red de casa. Resuelto con el fork. Ver arriba.

Medido con el motor del fork, elevado, contra `kanpachi.accentio.dev`. Se mide la
TRANSICIÓN y no un estado suelto, que es la disciplina de siempre: un grupo vacío
después no prueba nada si nunca se iba a llenar.

| Comprobación | Resultado |
|---|---|
| Reglas del grupo `EasyTier` antes | 0, tras limpiar las 17 que dejó el motor viejo |
| Adaptador `kanpachi0` | Up, TUN real |
| Reglas del grupo `EasyTier` **con el adaptador arriba** | **0** |
| Sockets TCP en escucha | ninguno |
| Endpoints UDP ligados | **0** |
| Secreto en la línea de comandos | no, solo la ruta del exe |
| Conexión al seed | `45.55.123.251`, con `peers_changed` de un miembro real |
| Salida al cerrar stdin | limpia, exit 0 |

Los cero endpoints UDP corrigen algo que se dio por supuesto acá: se creyó que el
motor tenía sockets UDP ligados para el P2P y que quitar el permiso entrante del
ejecutable podía costar conectividad directa. Con un seed alcanzado por `tcp://`
no abre ninguno. **Lo que esto NO prueba** es el caso de dos máquinas
agujereando NAT por UDP, que necesita la otra punta y llega con el directorio.

Quedan 17 reglas menos en la máquina, y ninguna es de Kanpachi: eran del motor
viejo y de los avisos de Windows durante el desarrollo. Las quita
`scripts/limpiar-reglas-del-motor.ps1`, que va en seco salvo con `-Aplicar`.

**2. La compuerta de WFP no la enciende nadie.** `firewall.SetScope` solo lo
llama `internal/fwprobe`. En el daemon real `specsFor` devuelve nil y `Apply`
deja un aviso en el log, así que hoy la contención del adaptador virtual son
únicamente las reglas del Firewall de Windows. **No es un olvido:** `SetScope`
necesita el LUID del adaptador, y el adaptador no existe hasta que el motor lo
crea. Cablearlo es trabajo del túnel y va con `netcfg`, que es quien espera a que
el adaptador aparezca.

Juntos eran el agujero. Por separado cada uno es la mitad, y por eso ninguno de
los dos se veía.

## La sala de verdad: ABIERTA, y los tres fallos que hicieron falta para llegar

Medido el 2026-08-05, elevado, con el daemon de verdad y contra
`kanpachi.accentio.dev`. `create_room` devolvió `connected`, con adaptador
`kanpachi0` arriba, IP virtual, y el Job Object llevándose al motor.

Para llegar hicieron falta tres arreglos, y **ninguno lo encuentra un test
unitario**, porque los tres son de CABLEADO o de TIEMPO y no de lógica:

1. **`RoutingTable` no existía.** Fallaba en `planSubnet`, antes de tocar el
   motor. Es el adaptador de este commit.
2. **`control.Attach` no lo llamaba nadie.** El comentario del propio método
   dice *"El cableado construye uno, después el otro, y los une"*, y `main.go`
   no hacía la última parte, así que `Serve` devolvía `ErrNotAttached` y crear
   una sala fallaba entera con el motor ya levantado. Estaba escrito, estaba
   probado, y solo lo llamaban los tests.
3. **Las órdenes de arranque volvían antes de que la red existiera.** El motor
   contesta cuando acepta la orden, no cuando el adaptador tomó su dirección.
   El canal de control liga en la IP del host dentro del vestíbulo, y Windows
   contestaba `The requested address is not valid in its context`. Ahora
   `HostNetwork`, `JoinRendezvous` y `JoinWithCredential` esperan a la dirección,
   con plazo y error si no llega: una sala que dice estar abierta sobre un
   adaptador que nunca apareció es peor que una que no abrió.

**El patrón que dejan los dos primeros vale más que los arreglos.** `SetScope` y
`Attach` estaban escritos, probados y sin llamar desde producción. Un test de
paquete no lo ve, porque el test SÍ los llama. Lo que lo ve es correr el
producto, y lo que lo evitaría es un guardián sobre `cmd/kanpachid` que exija que
cada método de unión declarado en un adaptador aparezca en el cableado.

Lo que la medición reveló de paso, y no estaba previsto: esta máquina tiene una
dirección de Tailscale en `100.65.79.92/32`, dentro del espacio compartido, así
que el plan de direcciones mandó la sala al espacio de reserva:

```
"subnet": "10.99.169.0/24",
"subnet_reason": "esta máquina ya usa 100.65.79.92/32 dentro del espacio
                  compartido, la sala va en 10.99.0.0/16"
```

Es el conflicto CGNAT de la decisión de direccionamiento, funcionando en la
primera ejecución real y sin que nadie lo montara a propósito.

**RoomDirectory paga la deuda escrita en `docs/CLAUDE.md`**: `domain.CheckSeedAddr`
está escrita y probada y **ningún adaptador la llama porque ninguno existe**. Se
llama sobre lo que resolvió el DNS y en **cada** uso, porque un nombre impecable
puede apuntar a `192.168.1.1`.

---

# Del lado de la UI

1. **El transporte del pipe en Dart**, probando `dart_ipc` primero. El disparador
   para internalizarlo: que no soporte el prefijo protegido, que bloquee el
   isolate, o que deje de mantenerse.
2. **Una línea de `ioc_manager.dart`** para que `SessionRepository` sea el real.
   El comentario del propio archivo ya lo dice.
3. ~~**La pantalla de exposición**~~: **HECHA**, con las dos filas y el bloque
   del sondeo debajo. Un informe ciego no puede llevar lista, y eso está cerrado
   en cuatro sitios: el cero de `MeasuredAt`, el constructor ciego, la vista de
   cable al salir y la entidad de Dart al entrar.
4. ~~**Que el HOST pueda pedirlo**~~: **el mensaje del canal está HECHO**, con el
   destino sacado de la conexión y sin campo de dirección en el cable. Lo que
   queda es el caso de uso y la pantalla.
5. **La Protección Kanpachi en la UI**: el aviso de que no está puesta y el botón
   idempotente de reponerla. Ver `05-ui.md`. El aviso NO nombra ningún puerto: lo
   que falla es la contención entera, y el canario vive en un puerto al azar que
   ya se cerró.

---

# Verificación

```bash
go build ./... && go vet ./... && go test -race -count=1 ./...
gofmt -l ./core ./internal ./daemon
cd ui && flutter analyze && flutter test
```

A mano, en Windows, con consola elevada:

```
go run ./daemon/cmd/kanpachid --console -data C:\ruta
go run ./internal/kanpctl -data C:\ruta status
```

Por adaptador, la prueba que de verdad importa:

- **Motor**: matarlo con el Administrador de tareas y comprobar que el hijo muere
  con él. Es lo único que prueba el Job Object.
- **Firewall**: la medición A y B de arriba.
- **netcfg**: cambiar de WiFi a cable con la sala abierta y ver que la métrica y
  las rutas vuelven solas.
- **El conjunto**: dos máquinas, un código, una partida.
