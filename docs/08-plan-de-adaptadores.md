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
`forbiddenPorts` (22, 135, 137-139, 445, 3389, 5985, 5986). Los de estas
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
| Consola **elevada** | WFP entero. Medido: `netsh wfp show filters` da `ERROR_ACCESS_DENIED` sin elevar, y `FwpmFilterCreateEnumHandle0` también |
| **Segunda máquina** | Las mediciones A y B, y la prueba del conjunto |
| **Seed alcanzable** | Hoy bloqueado por el proxy de Cloudflare, ver la lista de abierto |
| Adaptador `kanpachi0` **vivo** | El LUID, y el segundo puerto de magic DNS, que solo aparece con TUN levantado |

Todo lo que dependa de esos cuatro queda escrito y **marcado**, con la prueba
concreta que lo confirma. Nada se da por bueno sin medirlo.

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
| Daemon, compuerta | Sesión WFP dinámica | `blockAll` a peso 0 sobre `kanpachi0`, más permisos espejo soft del **mismo** `RuleSet`. Solo en `ALE_AUTH_RECV_ACCEPT_V4/V6`. |

**Nunca en `ALE_AUTH_CONNECT`**: bloquear la salida impediría que el invitado
marque al host.

## Un bloqueante documental que hay que arreglar antes

`docs/04` paso 8 le pide al instalador bloquear **"sobre la IP del adaptador"**.
El instalador no puede saber esa IP: la `/24` se elige **por sala, en tiempo de
ejecución**. Se borra esa frase. La cuarentena de base no se acota ni por IP ni
por adaptador, y la razón ya está escrita en `core/domain/policy.go`: un bloqueo
acotado que deja de casar **abre**.

## El orden

1. **Paso 0**: el arreglo de `AuditForeign` de arriba. ~30 líneas, sin WFP.
2. **Paso 1, core**: `domain.AppliedRule`, `domain.Enforcement`, la función pura
   de diff. `OwnRulesIntact(bool)` muere y nace `Enforcement()`, que devuelve lo
   **medido** en las dos capas y deja que juzgue el dominio. Aterriza **antes**
   de una línea de adaptador.
3. **Paso 2, spike fuera del repo**: ~150 líneas, media tarde, dos máquinas.
   Decide todo. Si sale mal, no hay nada que revertir.
4. **Paso 3, piso de WFP**: ~500-700 líneas, vendorizando y podando
   `wireguard-windows` bajo MIT con atribución. Solo dos capas y cinco
   condiciones.
5. **Paso 4, el `blockAll` solo**: ~120 líneas. **Vale por sí solo**: con esto y
   nada más, `kanpachi0` queda inalcanzable salvo por lo que los permisos COM
   abran. Es el 80% del valor.
6. **Paso 5, permisos espejo**: ~250 líneas, con transacción y diff por
   `providerKey`.
7. **Paso 6**: `Enforcement()` en la pantalla, con marca de cuándo se midió, y el
   botón que vale más que toda la pantalla: **"probar desde otra PC"**, que corre
   en el invitado contra la IP virtual del host.
8. **Paso 7, guardián nuevo** en `internal/arch`: ningún filtro se construye sin
   condición de alcance. Un filtro sin alcance **no falla en ningún test** y
   aplica a todos los adaptadores de la máquina; con hard block eso deja al
   usuario sin red de casa. Es el fallo más caro posible y no se ve leyendo el
   diff.

## Lo que NO se hace, decidido explícitamente

No se mudan los permisos a WFP. No se usan hard permits. No se usa el sublayer
máximo. No se vacía el grupo COM. No se borra `SuspendForeign`, que sigue
sirviendo para la LAN de casa. No se intenta cuarentena WFP persistente en el
instalador: BFE la deshabilita en silencio si el servicio no es auto-start.

## La prueba que lo decide

Dos máquinas, un juego con UDP continuo, y Parsec instalado en el host con su
regla permisiva viva.

- **Medición A**: desde el invitado, conectar a Parsec contra la IP virtual.
  **Tiene que conectar.** Si no, el agujero no es lo que creemos y toda esta
  ronda se cae.
- **Medición B**: el spike instala tres filtros y tienen que pasar cuatro cosas a
  la vez: Parsec deja de conectar, el juego sigue, el invitado sigue jugando sin
  tener ningún permiso, y la red de casa del host queda intacta.

---

# Lo que queda abierto, y son decisiones tuyas

1. **El uso legítimo de escritorio remoto.** Si alguien usa Parsec o Moonlight a
   propósito entre sus propias máquinas y están en la sala, el `blockAll` lo mata
   sobre la IP virtual. El síntoma es *"anda por la LAN y no por Kanpachi"*, sin
   regla que mirar. Hay que decidir si existe una vía para permitirlo desde la
   UI, y esa vía es justo lo que el modelo está para impedir.
2. **El seed no es alcanzable como peer.** `kanpachi.accentio.dev` resuelve a IPs
   de **Cloudflare**. El 443 sirve la página de invitación; el 11010 del motor no
   llega, y no depende del estado del droplet. Salidas: un nombre aparte en gris,
   quitarle el proxy, o probar `wss://` antes de resignar Cloudflare, que
   Cloudflare sí proxea WebSocket sobre 443. **Bloquea la prueba de dos
   máquinas.**
3. **La categoría de red del adaptador.** `docs/04` paso 6 dice Privada, con una
   razón de UX real. Sigue sin resolverse contra el argumento de que la sala son
   desconocidos con un ticket.
4. **La contradicción de `policy.go`.** Hoy dice que acotar un bloqueo por
   interfaz abre. **Sigue siendo cierto.** Lo que cambia es la consecuencia,
   porque los permisos COM quedan debajo. Hay que escribirlo así o en seis meses
   alguien lo "arregla" al revés.
5. **El riesgo que puede dejar el trabajo en cero.** Si la condición de interfaz
   llega vacía al reautorizar un flujo, el `blockAll` deja de casar **en
   silencio** y la pantalla diría verde. No se cerró leyendo la doc. Mitigación
   de 20 líneas: emitir el `blockAll` **dos veces**, por LUID y por dirección
   local sobre el `/24` de la sala.

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
