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

## Por qué NO un fork

Entre `v2.6.4` y la rama de desarrollo hay **606 ficheros y +129.000 líneas** en
un solo cambio, y el árbol del RPC se borró y renació en otro sitio. Los parches
habría que **reescribirlos**, no rebasarlos.

## Por qué NO cgo

Tres motivos independientes: cgo exige mingw y EasyTier solo compila y testea
`-msvc`; el workspace usa `panic = "abort"`, así que un panic del motor mataría
al daemon que corre como SYSTEM; y es la única opción que mete al daemon Go
dentro de la Combined Work de la LGPL.

## El build, con las trampas ya resueltas

Cuatro, todas encontradas a los golpes:

| Síntoma | Causa | Arreglo |
|---|---|---|
| `zstd-sys` no compila, `VCINSTALLDIR = None` | `cc` halla `cl.exe` sin entorno MSVC | correr bajo `vcvars64` |
| `prost-wkt-types` panic | el `PROTOC` que baja `build.rs` vale solo para él | `PROTOC` global |
| `kcp-sys` panic | usa bindgen, necesita `libclang` | extraer `libclang.dll` del instalador LLVM con 7z |
| `zstd-sys` falla con entorno correcto | ruta de ~250 chars; **`cl.exe` no es long-path aware** | build en ruta corta |

Toolchain: rustup con `1.95` (lo pinea el repo), MSVC 14.44, protoc 29.3,
libclang 19.1.7. Build de ~18 min. En CI: `windows-latest` más
`ilammy/msvc-dev-cmd`, gratis en repo público.

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
3. Features de cargo elegidas a propósito. El default trae `socks5`, `magic-dns`,
   `wireguard` y `faketcp`, que Kanpachi no usa. Compilar sin ellas **borra la
   capacidad del binario**, que es más fuerte que vigilar el argv. Ojo: `tun` se
   queda, y `windivert` no se puede sacar en ningún caso.
4. Los ficheros de licencia: aviso, copia de LGPL-3.0 **y** de GPLv3 (EasyTier no
   incluye la segunda), y enlace al tag. **No es deuda de hoy**: se comprobó que
   `third_party/` está en `.gitignore`, que no hay instalador, y que la ruta
   `/descargar` sirve la misma página HTML. Hoy no se distribuye nada.

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
| Instalador | COM, grupo `Kanpachi-base` | Los 9 puertos prohibidos en las dos direcciones, **todas** las interfaces, más ICMP echo. **No cambia nada.** |
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
7. **Paso 6**: `Enforcement()` en la pantalla, con marca de cuándo se midió, y el
   botón que vale más que toda la pantalla: **"probar desde otra PC"**, que corre
   en el invitado contra la IP virtual del host.
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

---

# El resto de los adaptadores

| # | Adaptador | Desbloquea | Riesgo |
|---|---|---|---|
| 1 | **Motor** | Todo lo demás | Alto |
| 2 | **Firewall** | La promesa central | Alto |
| 3 | **netcfg** | Que el túnel sea usable | Medio |
| 4 | **RoomDirectory** | Crear y entrar con código | Medio |
| 5 | **Auditoría** | Que las alertas digan la verdad | Bajo |
| 6 | **SystemEvents** | Que los ajustes sobrevivan a Windows | Medio |
| 7 | **Steam** | Comodidad. Ordena, jamás filtra | Bajo |
| 8 | **iphlpapi** | Solo el creador de perfiles | Bajo |
| 9 | **Rendezvous** | Argon2id local puro | Bajo |

El 9 es Go **puro**, sin red y sin Windows, así que es el más barato y entra en
la lista de `puros` del guardián de arquitectura.

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
3. **La pantalla de exposición**, alimentada por `Enforcement()`. Dos filas, no
   una: los puertos abiertos, y *"todo lo demás en kanpachi0 está bloqueado"*. Si
   la enumeración falla, la pantalla dice **"Kanpachi no pudo leer lo que tiene
   puesto"** en vez de mostrar la última lista buena. Misma doctrina que
   `AlertAuditFailed`.

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
