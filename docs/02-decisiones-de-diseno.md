# Decisiones de diseño

Cada decisión relevante, las alternativas consideradas y la razón de la elección. El formato es deliberado: cuando algo se cuestione en el futuro, aquí está el contexto completo para reabrirlo con criterio.

## 1. Motor de red: un binario propio sobre la librería de EasyTier, como subproceso

**Alternativas:** Headscale envuelto con una API propia. Construir desde cero sobre wireguard-go o sobre Nebula. EasyTier vinculado al binario Go por cgo. El `easytier-core.exe` oficial como proceso hijo. Un fork de EasyTier con parches propios. Un binario propio que use EasyTier como librería.

**Elección:** **`kanpachi-engine.exe`**, un binario propio en Rust que declara EasyTier como **librería**, ejecutado como **proceso hijo** del daemon y accedido siempre a través de `EnginePort`. Vive en un repositorio aparte bajo LGPL-3.0.

Y la librería que declara es **un fork nuestro**, `alvarogabrielgomez/EasyTier`, tag `v2.6.4-kanpachi.1`. Esa segunda mitad se decidió después, al medir, y tiene su propia sección más abajo: el binario propio quita lo que el CLI hace ALREDEDOR de la librería, y no quita lo que la librería hace DENTRO de lo que se le pide.

### Por qué el binario oficial no sirve

El `easytier-core.exe` que se distribuye abre un **portal de administración sin autenticación de ninguna clase**. Por ahí, cualquier proceso local emite credenciales válidas de la red real, agrega nodos, reenvía puertos por debajo del cálculo de reglas, y pide el `network_secret`, que llega **en claro**.

No es un descuido que vayan a arreglar. La autenticación se pidió en el issue 925 y se **rechazó a propósito** a favor de una lista blanca de IP, en el PR 929. Esa lista no sirve para esto: filtra **después** del accept y su valor por defecto ya incluye `127.0.0.0/8`, así que no distingue un proceso local de otro. Todos llegan como `127.0.0.1`.

Y el default es peor de lo que dice su propia ayuda. Medido con `netstat` contra el binario fijado, usando un fichero de configuración que **no nombra el portal en ninguna parte**:

```
TCP    0.0.0.0:15888          0.0.0.0:0              LISTENING
```

Todas las interfaces, no `localhost`. O sea alcanzable desde la LAN de casa y desde la propia red virtual.

### Por qué la librería sí

El portal se construye en **un solo sitio de todo el árbol de EasyTier**, y ese sitio está dentro de su binario de línea de comandos. El arranque de red por librería no lo menciona. Comprobado con el mismo fichero de configuración, la misma máquina y el mismo momento:

| Proceso | Sockets en escucha |
|---|---|
| `easytier-core.exe` v2.6.4 | `TCP 0.0.0.0:15888 LISTENING` |
| Binario propio sobre la librería | ninguno |

**El portal desaparece por omisión, no por configuración.** No hay bandera que apagar ni lista blanca que mantener.

Lo que el producto necesita del motor sale entero de la librería, y se verificó uno por uno antes de comprometerse: arrancar y parar la red, emitir y revocar credenciales, la lista de pares, el diagnóstico de NAT, y un canal de eventos que empuja en vez de que haya que preguntar. Ese canal ni siquiera está disponible por el camino oficial, que lo descarta.

La configuración entra por **stdin**, así que el secreto de la red no pisa la línea de comandos. Medido: el `CommandLine` del hijo solo muestra la ruta del ejecutable. Con el binario oficial el secreto es legible con el Administrador de tareas por cualquier usuario de la máquina.

### El canal de órdenes: una tubería anónima, y ninguna otra entrada

Este párrafo decía "TOML por stdin", y eso describía solo el arranque. `EnginePort` tiene trece métodos y once ocurren **después**, con la sala ya abierta: emitir una credencial cuando alguien toca la puerta no cabe en un fichero de configuración. La forma real es **el tubo de entrada abierto, con mensajes JSON, uno por línea, en las dos direcciones**. El TOML se queda como detalle interno del motor, que es quien construye el `TomlConfigLoader`.

**Por qué esa forma y no un puerto o una named pipe.** La tubería que Windows fabrica al crear el proceso hijo es **anónima**: no tiene nombre, no tiene ruta, no tiene dirección. No es que esté protegida, es que **no existe la operación de conectarse a ella**. Los únicos dos extremos viven como handles dentro del daemon y del motor.

| Opción | Cómo llega un tercero | Qué la protege |
|---|---|---|
| Puerto en localhost | `connect()` a la dirección | nada, o un token que hay que escribir bien |
| Named pipe | abrirla por su nombre | una ACL que hay que escribir bien |
| **Tubería anónima** | **no existe la operación** | **el kernel** |

Las dos primeras son puertas, y una puerta necesita cerradura, y una cerradura se puede escribir mal: el portal 15888 del binario oficial es exactamente ese fallo. Acá **no hay autenticación que escribir porque el canal ES la identidad**: el único que puede tener el otro extremo es quien creó el proceso.

**El límite, dicho sin adornos.** Un proceso ya corriendo como SYSTEM con privilegio de depuración puede duplicarle un handle al daemon. Ese atacante ya es administrador total de la máquina y puede apagar el firewall o reemplazar el binario, así que no es el enemigo de este diseño. El enemigo real es el programa **sin privilegios**, el mod de un juego o el `.exe` que pasó un amigo, y contra el binario oficial ese programa pide el secreto de la red con un `connect` y sin UAC.

**Regla dura: stdin es la ÚNICA entrada de órdenes del motor.** Jamás un segundo canal, ni puerto, ni named pipe, ni fichero vigilado, ni señal. El binario **no acepta argumentos**: un `argv` con algo más que la ruta sale con error. Ejecutarlo a mano arranca otra instancia vacía, que no conoce el secreto de ninguna sala y no toca los túneles del daemon.

### Los eventos de capacidades prohibidas se descartan en silencio

El bus de eventos de EasyTier es un `tokio::sync::broadcast` **dentro de nuestro proceso**. No es una superficie: no hay socket, no hay fichero, y nadie fuera del motor puede escucharlo. Lo que se decide acá no es si se expone, se decide qué se hace con lo que llega.

EasyTier emite 24 clases de evento y el dominio de Kanpachi tiene 5, cerradas a propósito. Cuatro de las 24 anuncian capacidades que el producto prohíbe: `ListenerAdded`, `PortForwardAdded`, `VpnPortalStarted` y `UdpBroadcastRelayStartResult`. Si alguna llegara significaría que el motor hizo algo que la configuración le prohíbe.

**Se descartan, sin traducir y sin registrar.** La alternativa evaluada era escucharlas y usarlas de alarma en tiempo de ejecución.

Lo que sostiene que descartarlas sea aceptable: **el test de invariante de sockets mide exactamente lo mismo**, arranca el motor con la configuración real, con el TUN levantado, y falla si aparece cualquier puerto en escucha. La pregunta "¿la configuración de verdad apagó eso?" ya tiene quien la conteste, y la contesta en CI, antes de que el binario salga.

Lo que se pierde, dicho para que nadie lo descubra tarde: **la detección en la máquina del usuario.** El caso que quedaría mudo es el que pasa en CI y falla en el campo, por una diferencia de Windows o una carrera de arranque. El día que aparezca uno así, esta decisión se reabre con ese caso como argumento.

### El fork, que primero se descartó y después hizo falta igual

Esta sección decía "por qué NO un fork", con un argumento que sigue siendo cierto: entre el tag `v2.6.4` y la rama de desarrollo hay **606 ficheros y +129.000 líneas** movidas por un solo cambio, y el árbol entero del RPC se borró y renació en otro sitio. Un parche nuestro contra eso hay que **reescribirlo**, no rebasarlo.

Lo que cambió no es el coste del fork: es que apareció algo que la librería hace y que no se puede apagar. **Hay dos clases de cosa que hace el binario oficial, y solo una desaparece al escribir el nuestro.**

| Clase | Dónde vive | ¿La quita escribir nuestro binario? |
|---|---|---|
| Lo que el CLI hace **alrededor** de la librería | su `main`, su `cli` | **Sí.** Ese es el portal 15888 |
| Lo que la librería hace **dentro** de lo que se le pide | `virtual_nic.rs` | **No.** Es su código |

Medido en una máquina de verdad: al crear el adaptador virtual, EasyTier escribe **ocho reglas de permiso** en el Firewall de Windows, por COM, desde dentro de `NetworkInstance::start()`, o sea también por el camino de librería. Son permanentes: sobreviven al reinicio y a desinstalar Kanpachi.

| Regla | Alcance |
|---|---|
| `EasyTier kanpachi0 - ALL Protocol (Inbound)` | cualquier protocolo sobre el adaptador virtual, sin puerto y sin origen |
| `EasyTier <ruta del exe> (Inbound)` | cualquier protocolo hacia el motor en **todas** las interfaces de la máquina, la red de casa incluida |

La primera deshace la promesa central en la misma capa que Kanpachi usa para conceder. La segunda es la que decidió el enfoque: **la compuerta de WFP no puede taparla**, porque va acotada al adaptador virtual, y esa acotación es la invariante que impide que un bloqueo duro deje al usuario sin la entrada de su red de casa. Una capa que jamás debe salirse del adaptador no puede cubrir una regla que aplica en todos.

No hay feature de cargo, campo de configuración ni variable de entorno que las apague. Y no es un defecto de upstream: su propósito declarado es el proxy de subredes y el de KCP, que Kanpachi corre apagados. **Es una diferencia de producto.**

Así que el fork es `v2.6.4` con esas dos llamadas borradas y **nada más**: `1 fichero, 8 inserciones, 31 borrados`, y las ocho inserciones son comentarios. El coste de mantenerlo es el que dice el párrafo de arriba, y por eso el diff se mantiene así de chico: subir de versión es rebasar dos borrados, no reescribir un parche.

**Por eso el motor NO vive dentro del fork, y son tres repos.** La afirmación "esto es upstream y nada más" tiene que poder comprobarse en treinta segundos con `git diff v2.6.4 v2.6.4-kanpachi.1`, y un fork con dos mil líneas nuestras dentro la convierte en un acto de fe. Mantener la línea de cambios separada es además lo que deja actualizar algún día.

Lo que el fork NO reemplaza es la compuerta. Su enemigo nunca fue EasyTier: son las reglas permisivas **ajenas**, de escritorio remoto y de instaladores de juegos, que alcanzan al usuario por la red virtual. Eso no lo quita ningún fork. Y la red permanente contra que esto vuelva a pasar tampoco es el fork: es que `AuditForeign` clasifica como ajeno cualquier permiso entrante acotado a una interfaz `kanpachi*` que no sea nuestro, y esa clase BLOQUEA la sala.

### La versión se fija a propósito

`v2.6.4` es la **última publicada**, no una vieja. No hay tag posterior. Lo único más nuevo es la rama de desarrollo, sin versión, sin notas de cambios y sin binarios. La rama trae una forma elegante de apagar el portal al compilar; llegar a ella cuesta abandonar el tag y fijar un commit suelto, y esa conversación se abre cuando haya una razón, no antes.

### El seed es el caso contrario

En el droplet sigue corriendo `easytier-core` oficial sin modificar, y está bien que así sea: es una máquina nuestra, sin usuarios, donde el portal local no es una superficie que le importe a nadie más. La razón de todo lo anterior es la máquina **del usuario**.

**Y poner el fork ahí no cambiaría nada, medido el 2026-08-06.** El diff entero vive en `virtual_nic.rs`, y las dos llamadas que borra son `crate::arch::windows::add_self_to_firewall_allowlist()` y `add_interface_to_firewall_allowlist()`, las dos tras `#[cfg(target_os = "windows")]`. En un binario de Linux ese código **no se compila**, y además el seed arranca con `--no-tun true`, así que jamás construye la `VirtualNic` donde viven. Un `easytier-core` de Linux del fork se comporta igual que el oficial. La consistencia sería de tag, no de máquina.

Lo que sí costaría: el fork **no publica releases ni tiene workflows** (comprobado contra su API). Usarlo obligaría a compilar Rust para linux/amd64 y linux/arm64 por nuestra cuenta, hostear esos binarios y fijar SHA256 nuevos a mano, contra los 60 segundos que hoy cuesta bajar el zip oficial con su hash ya fijado. Se decidió no hacerlo.

**PENDIENTE de revisar, y conviene mirarlo con calma.** Que el seed levante el portal quedó como herencia del arranque oficial, no como una decisión que alguien tomara mirando el droplet. Las tres preguntas a contestar cuando toque:

1. **¿Lo usa alguien?** `registry/counter.go` habla con `easytier-cli` contra el portal para contar miembros. Si esa es la única razón, hay que ver si el conteo puede salir de otro lado.
2. **¿Está de verdad acotado?** **Sí. Medido en el droplet el 2026-08-06**, que es lo que faltaba: `ss -lnt` lo enseña como `LISTEN 127.0.0.1:15889` y en ninguna otra dirección, con el `ExecStart` real llevando `--rpc-portal 127.0.0.1:15889 --rpc-portal-whitelist 127.0.0.1`. El par llega entero y el portal no sale de loopback.

   **Ojo con el número: es 15889 y no 15888.** El puerto NO es fijo, lo elige `PuertoLibre` desde 15888 y en esta máquina el 15888 ya estaba tomado. Este documento decía "15888" como si fuera una constante, y quien fuera a comprobarlo con ese número no habría encontrado nada y habría concluido lo contrario de lo que pasa. El dato de verdad vive en `/etc/kanpachi/seed.json`, campo `puerto_rpc`.
3. **¿Cambia el modelo de amenazas ahora?** "Máquina nuestra sin usuarios" es cierto hoy. La lista de puertos del droplet abiertos a internet ya tiene sus propios pendientes, y un portal sin autenticación en loopback deja de ser inocuo el día que alguien consiga ejecutar cualquier cosa en esa máquina.

Lo natural es que el seed también termine sobre el motor propio, con lo cual el portal desaparece de los dos lados. Eso es trabajo aparte y va después del motor del cliente.

**Por qué EasyTier.** Ya resuelve lo difícil: NAT traversal por UDP, cifrado WireGuard, relay de respaldo, y un relay de broadcast UDP que hace funcionar el descubrimiento LAN de los juegos clásicos. Su modelo de identidad nativo es `--network-name` más `--network-secret`, exactamente lo que produce el código de sala. Headscale exigía envolver su modelo de usuarios y pre-auth keys para simular salas anónimas, trabajo que no aporta al producto. Desde cero eran meses para llegar a un ~70% de conexiones directas, contra el 90%+ que da un coordinador maduro.

**Por qué subproceso y no vinculado.** Tres razones que apuntan al mismo lado:

1. **Licencia.** EasyTier es **LGPL-3.0**, verificado en el `LICENSE` del propio tag v2.6.4 que tenemos fijado, que abre con `GNU LESSER GENERAL PUBLIC LICENSE Version 3`. No siempre lo fue: el proyecto era **Apache-2.0 hasta el 2025-06-07**, y el corte cae entre v2.3.1 y v2.3.2. O sea que el crate `2.0.3` que sigue publicado en crates.io es código de la era Apache de verdad, y no un metadato rancio, que es lo que afirmaba este párrafo antes de que alguien lo comprobara. Con LGPL, vincular estáticamente desde un binario Go obliga a permitir la revinculación, algo incómodo de cumplir con la compilación estática de Go. Ejecutarlo como proceso separado es mera agregación: no hay vinculación, la LGPL no se propaga al código de Kanpachi y su licencia queda libre.
2. **Frontera de lenguaje.** EasyTier es Rust, el daemon es Go. La alternativa de vinculación implica cgo o FFI, con su costo de compilación cruzada, empaquetado y depuración. `easytier-go` existe y usa WebAssembly en vez de cgo, es muy joven y también LGPL-3.0, así que no resuelve el punto 1.
3. **Aislamiento de fallos.** El watchdog del supervisor ya asume que el motor puede morir y reiniciarse. Eso solo tiene sentido con un proceso aparte: un `panic` de Rust dentro del mismo proceso se llevaría el servicio entero. Medido y no razonado: el workspace de EasyTier compila su perfil de release con `panic = "abort"`, así que no hay `catch_unwind` que pueda atajarlo.

**Costos aceptados:**

- Dos binarios que firmar en vez de uno, el día que haya firma.
- Ciclo de vida del proceso hijo: arranque, supervisión, apagado limpio, huérfanos si el servicio muere de forma sucia. Se maneja en `adapter/engine/easytier`.
- Comunicación por IPC en vez de llamadas en proceso. Irrelevante en volumen: son órdenes de control y consultas de estado, el tráfico del juego jamás pasa por ahí.
- Dependencia de la calidad y el ritmo de EasyTier. `EnginePort` existe para que ese costo tenga salida: cambiar de motor toca una implementación, no el producto.

### Los binarios fijados, con su suma

Los binarios pesan unos 80 MB y no se versionan, así que hasta la auditoría de ciberseguridad no había **nada** que dijera que el `easytier-core.exe` de una máquina era el del release que se probó. El daemon lo ejecuta como proceso hijo con los privilegios del servicio, o sea que la pregunta no es menor.

Release oficial de EasyTier **v2.6.4**, Windows x86_64. SHA256:

| Archivo | SHA256 |
|---|---|
| `easytier-core.exe` | `da7eb2d24b5416f3d3407636949e964a0750e3f9dc53a828cb6799a57ead445d` |
| `easytier-cli.exe` | `d8783e851e944b44a9b71b39fd02f227ec0a2a82b3165c55ead5dd32dcde53a1` |
| `easytier-web.exe` | `59448d5fefbb6e73c1525c167bbbcd1c20df45d2f196f11ba6203dc9cfd64757` |
| `easytier-web-embed.exe` | `38b32ae7cf07c8ceaea3daf55d0ecb823cc6885d611d302450750fa1dff0edf5` |
| `wintun.dll` | `e5da8447dc2c320edc0fc52fa01885c103de8c118481f683643cacc3220dafce` |
| `Packet.dll` | `c7c03a87eac7243ccbe331554624b18803010b740e311fc8cfddb573096eacac` |
| `WinDivert64.sys` | `8da085332782708d8767bcace5327a6ec7283c17cfb85e40b03cd2323a90ddc2` |

Los mismos valores viven en `internal/arch/easytier.sums`, y hay dos tests que los sostienen: uno verifica el disco contra el manifiesto, el otro verifica que el manifiesto y esta tabla no se separen. Actualizar de versión es cambiar las dos cosas a propósito, que es exactamente lo que tiene que costar.

**De los siete, el daemon solo ejecuta `easytier-core.exe` y consulta con `easytier-cli.exe`.** `wintun.dll` es el adaptador virtual, y no aparece en la tabla de importaciones porque el motor lo carga en caliente. `easytier-web` es un panel y no lo usa nada.

**`Packet.dll` sí lo usa, y esto hay que decirlo distinto de como estaba.** Este documento afirmaba que `Packet.dll` y `WinDivert64.sys` venían en el release y no los usaba nada. Es falso, y se ve en el binario fijado con `dumpbin /imports`:

```
packet.dll
      3 PacketGetAdapterNames
     12 PacketSendPacket
```

Es una importación **dura**, sin sección de delay import en todo el ejecutable, o sea que `easytier-core.exe` no llega ni a arrancar si `Packet.dll` no está al lado. Y `PacketSendPacket` es inyección de paquetes crudos sobre un adaptador. La capacidad viaja dentro del motor desde el instante de la carga, se encienda o no el descubrimiento LAN.

Del lado del fuente pasa lo mismo con el driver: `windivert` es una dependencia **no opcional** para Windows x86_64 (`easytier/Cargo.toml:274-277`), con la feature `static`, así que ninguna combinación de features de cargo lo saca del grafo. Lo que `--enable-udp-broadcast-relay` decide es si la capacidad se **usa**, jamás si viaja.

Consecuencia práctica, y es la que importa: **la cuarentena del producto no puede apoyarse en que el motor sea incapaz.** Se apoya en el firewall y en que Kanpachi no encienda la bandera. Lo primero ya está en la decisión 4; lo segundo lo vigila `internal/arch/motor_test.go`.

### Distribuirlos tiene una obligación, y hoy no se cumple

El instalador reparte los binarios de EasyTier, que son **LGPL-3.0** desde el 2025-06-07. Repartirlos, incluso entre amigos, es *convey* según la GPLv3 sección 0. Eso obliga a tres cosas que hoy no están:

1. **Aviso visible** de que el producto incluye EasyTier y de bajo qué licencia.
2. **Copia de la LGPL-3.0 y de la GPLv3.** Las dos, porque la LGPL-3.0 está redactada como un conjunto de permisos adicionales sobre la GPLv3 y no se sostiene sola. El repo de EasyTier ni siquiera incluye la GPLv3, así que hay que traerla de gnu.org.
3. **Acceso al fuente correspondiente.** Se cumple barato por la sección 6(d): un enlace al tag `v2.6.4` del repo oficial, publicado junto a la descarga del instalador.

**Falta además revisar las otras tres.** `WinDivert64.sys`, `wintun.dll` y `Packet.dll` no son de EasyTier y llevan licencias propias, con términos de redistribución que nadie miró todavía. `Packet.dll` viene del linaje WinPcap/Npcap, que es el más restrictivo de los tres. Es trabajo pendiente e independiente de qué motor se termine usando.

### Los defaults del motor no son los nuestros

Verificado contra `easytier-core --help` de la v2.6.4, que es la versión fijada. Este bloque existe porque el motor trae capacidades que contradicen invariantes del producto, y varias vienen encendidas.

**El caso grave, porque es un default:**

```
--disable-upnp    disable runtime UPnP/NAT-PMP port mapping for eligible
                  listeners; automatic port mapping is enabled by default
```

EasyTier mapea puertos en el router del usuario **salvo que se le prohíba explícitamente**. La invariante dice que el router no se toca nunca. O sea `--disable-upnp true` no es una preferencia, es obligatorio en todo arranque, y lleva un test que falla si alguien lo quita.

**Flags que expresan capacidades prohibidas y van siempre apagadas:**

| Flag | Qué haría | Invariante que rompe |
|---|---|---|
| `--enable-exit-node`, `--exit-nodes` | Enrutar internet por otro nodo | Jamás exit node |
| `--proxy-networks` | Exportar subredes locales a la sala | Jamás subnet routing |
| `--vpn-portal` | Levantar un servidor WireGuard | Nada escucha en público |
| `--socks5` | Proxy de acceso a la red virtual | Superficie sin razón |
| `--accept-dns` | Magic DNS, modifica el DNS del sistema | No se tocan ajustes globales |

**Cómo arranca un cliente:** con `--no-listener`. El cliente jamás escucha en un puerto público. Comprobado que igual resuelve su dirección pública por STUN y perfora NAT, o sea la conectividad no depende de escuchar. **Solo el seed escucha.**

**El portal RPC del motor es su panel de control, y su default es peor de lo que decía este documento.** Acá se leía que el portal "va fijado a `127.0.0.1`". Eso era una intención nuestra, jamás una descripción del motor, y el fuente del tag fijado lo desmiente en `easytier/src/rpc_service/api.rs:178-196`:

```rust
fn parse_rpc_portal(rpc_portal: Option<String>) -> anyhow::Result<SocketAddr> {
    if let Some(Ok(port)) = rpc_portal.as_ref().map(|s| s.parse::<u16>()) {
        Ok(SocketAddr::from(([0, 0, 0, 0], port)))     // <-- puerto a secas
    } else {
        ...
        select_proper_rpc_port(&mut rpc_addr)?;         // <-- None => ([0,0,0,0], 0)
```

O sea que `--rpc-portal 15888` escucha en **todas las interfaces**, y no pasarla tampoco ayuda: sin bandera arma `0.0.0.0:0` y busca puerto libre entre 15888 y 15900. El texto de ayuda oficial dice *listen on 12345 of localhost*, y el código hace otra cosa.

**Kanpachi tiene que pasar el par completo, `--rpc-portal 127.0.0.1:<puerto>`, siempre.** Un puerto suelto es un arranque expuesto a la LAN de casa y a la propia red virtual. El guardián tiene que exigir el par y no el literal `127.0.0.1` suelto.

**La whitelist no cubre el hueco que de verdad preocupa.** `--rpc-portal-whitelist` filtra **después** del accept y su valor por defecto ya incluye `127.0.0.0/8`. Sirve contra un vecino de LAN, jamás contra otro proceso de la propia máquina, porque todos los procesos locales llegan como `127.0.0.1`. Y el portal **no tiene autenticación de ninguna clase**: fue pedida en el issue 925 y rechazada a propósito a favor de la whitelist, en el PR 929.

**Segunda cerradura, a nivel del motor.** Existen `--tcp-whitelist` y `--udp-whitelist`, que restringen puertos dentro del propio motor, por debajo del Firewall de Windows. Es defensa en profundidad real: una regla de firewall mal calculada no alcanzaría para exponer un puerto que el motor también rechaza. Vale evaluarlas al construir `policy/`.

**El descubrimiento LAN tiene un costo que hay que decidir aparte.** La flag es `--enable-udp-broadcast-relay`, y su documentación dice: *capture local UDP broadcast packets from physical interfaces and forward them to EasyTier peers. Requires administrator privileges.* O sea captura tráfico de la red de casa del usuario, con un driver de captura de paquetes (WinDivert, que viene en la distribución del motor). Es lo que alimentaría el flag `lan_discovery` del catálogo.

**Para la v1 no hace falta.** El caso de uso central es Project Zomboid, que es `direct_ip`: el jugador escribe la IP del host. Ningún juego del catálogo inicial necesita broadcast. Encender un driver de captura de paquetes para que un juego aparezca solo en una lista es un intercambio que se decide cuando exista ese juego, con su propia entrada en este documento, no antes.

**Un comportamiento del motor que afecta a `netcfg`.** El conector TCP enumera todas las IPv4 locales y bindea el socket saliente a cada una antes de conectar. En una máquina con adaptadores virtuales falla contra cada `169.254.x.x` y sigue. La consecuencia para nosotros es que, una vez creado `kanpachi0`, **su IP virtual entra en esa enumeración**, y el motor intentará salir hacia el seed por la propia interfaz virtual. Para eso existe `--bind-device`, que ata el conector a interfaces físicas. Hay que verificarlo en la primera prueba con adaptador real y fijarlo en consecuencia.

## 2. El código es un ticket de ingreso, no el secreto de la red

**Alternativas:** backend de salas que canjea códigos (el diseño inicial, kanpachi-rooms). Derivación local pura, donde el código **es** el secreto. Código como ticket que el host canjea por credenciales.

**Elección:** el código es un ticket. El host lo valida y entrega credenciales temporales. El backend de salas sigue eliminado: la autoridad es el host, no un servidor.

### Por qué la derivación local pura no alcanzaba

El diseño anterior derivaba todo del código:

```
código  ──Argon2id──►  networkID + secret
```

Simple y sin infraestructura. Tiene un límite que no se puede rodear: **si el secreto se calcula localmente a partir del código, quien tiene el código tiene el secreto para siempre.** Revocar significaría "este código ya no produce ese secreto", y no hay a quién preguntarle, porque es una función pura y no una consulta.

De ahí salían tres carencias, y las tres importaban:

- No se podía expulsar a nadie de la red, solo cerrarle el firewall.
- No se podía renovar el código de una sala sin mudar a todos a otra red, cosa que corta el túnel con la partida viva y desconecta a todo el mundo.
- No se podía autenticar un mensaje del host, porque el secreto lo tienen todos y cualquier miembro puede firmar como si fuera él.

### La autoridad que faltaba ya existía

Sale de una regla del propio producto: **la sala necesita al host activo**. El host está en línea por definición, así que puede decidir en el momento del ingreso. No es un servidor, es su propio daemon.

```
1. El código deriva una identidad de encuentro, desechable
2. El que entra contacta al host y demuestra que tiene el código
3. El host mira su registro: ¿ese código sigue vivo?
4. Si sí, le emite una credencial temporal
5. Entra a la red real con esa credencial, y la red real NUNCA cambia
```

El código deja de ser material criptográfico y pasa a ser una llave que alguien puede cambiar la cerradura.

### Los tres identificadores, que no son lo mismo

Esta decisión parte en tres lo que antes era un solo string. Confundirlos es el error más fácil de cometer acá, y lleva a conclusiones de seguridad falsas en las dos direcciones.

| Nombre | Qué es | Quién lo conoce |
|---|---|---|
| **Invite ID** | 8 alfanuméricos, `A7K2M9QX`. Llave de búsqueda, no material criptográfico | El seed lo emite y lo guarda. Viaja en la URL, en los logs y por Telegram |
| **Identidad de encuentro** | `networkID` + `secret` derivados del invite ID con Argon2id | Cualquiera que tenga el invite ID, el seed incluido |
| **Red real de la sala** | `networkID` + `secret` propios, aleatorios, generados por el host | Solo el host. Los demás entran con credencial y jamás lo reciben |

Que el seed pueda derivar la identidad de encuentro **no es una fuga, es el diseño**: el encuentro es un vestíbulo público y desechable, y lo único que pasa ahí es el pedido de credencial. Todo lo que importe en ese vestíbulo va firmado contra la llave del host, ver decisión 25.

La red real no deriva de ningún string que alguien pueda escribir. Ni el seed, ni quien adivine un invite ID, ni quien lea un log llegan a ella. El invite ID te lleva a la puerta, la credencial te deja pasar, y quien emite credenciales es el host.

### Verificado contra los binarios, no contra la documentación

Esta decisión se apoya en el sistema de credenciales de EasyTier. Se probó con dos instancias locales sobre la v2.6.4 fijada, y las tres preguntas que podían tumbarla salieron bien:

| Pregunta | Resultado |
|---|---|
| ¿Un nodo temporal entra sin `--network-secret`? | Sí, conectó en 2 s |
| ¿Revocar expulsa a un nodo ya conectado? | Sí, **en 1 segundo** |
| ¿El nodo temporal recibe el secreto de red? | **No.** Su handshake lleva `secret_digest` en ceros y `client_secret_proof: None`, contra el digest real que aparece sin credenciales |

Ese tercer punto es el que hace que revocar sirva de verdad. En el diseño ingenuo, quien ya entró se quedó con el secreto y revocarle el código no cambia nada. Acá nunca lo tuvo.

### Cómo se mapea al producto

| Concepto del producto | Mecanismo |
|---|---|
| Código de invitación | Credencial con `--credential-id` estable y `--reusable true`, o sea un código que sirve para varios panas |
| Renovar el código | Revocar ese id y emitir otro. Los que ya entraron siguen dentro, porque tienen credencial y no código |
| Expulsar a alguien | Revocar **su** credencial. Sale de la red en un segundo |
| Reabrir la sala más tarde | El host persiste su registro y honra el mismo código, incluso sobre una red subyacente nueva |
| Miembros que no relevan tráfico ajeno | `--allow-relay` apagado por defecto en cada credencial |

**Una consecuencia que vale más de lo que parece:** como el código dejó de ser la red, "la misma sala" sobrevive a que la red subyacente cambie por completo. La sala pasa a ser un objeto durable que el host posee, en vez de una consecuencia matemática de un string. Reiniciar, cambiar de red o migrar el esquema de derivación dejan de romper la invitación.

### La sala mal cerrada del host

Un apagón deja al host sin mandar el aviso de cierre. Al volver, el daemon detecta la sala anterior y **pregunta**: reabrirla con el mismo código, o cerrarla.

**El archivo ES la señal.** Salir limpio lo borra, morir sucio lo deja. No hay bandera `dirty` dentro, porque una bandera es un campo más que alguien puede escribir a mano y este hecho no se puede falsificar desde dentro del archivo.

**Dos reglas de orden, y las dos son invariantes:**

1. **Se purga primero.** El arranque ya borra las reglas del grupo `Kanpachi` y restaura las ajenas suspendidas, así que la máquina está en deny-all mientras la pregunta está en pantalla.
2. **Nunca reconecta solo.** Siempre pregunta. Es la invariante de que nada que llegue de fuera de la app surte efecto sin confirmación dentro de la app, y acá lo de fuera es un archivo del arranque anterior.

**La ventana encaja sola con el contador de la decisión 20.** Los invitados guardan una credencial contra la red real, así que un host que vuelve dentro de los veinte minutos se los encuentra reconectando solos. Uno que tarda más reabre una sala vacía con el mismo código, que es igual de correcto y era el otro caso a cubrir.

**Lo que el archivo lleva y lo que no.** Lleva **identidad y referencias, jamás política**: el invite ID con su seed, la identidad de la red real, la subred, el nombre, el nick, la clave de la tarjeta, y el **id del juego activo**. Lo que no se puede escribir ahí: un puerto, un rango, una regla, un ejecutable, un plazo, una lista de miembros. Si no se puede expresar, un archivo manipulado no abre nada ni compra tiempo de más en ninguna sala.

**El id del juego no es política**, y por eso sí entra. Es la misma referencia que ya viaja en el anuncio del host, donde la regla es que lleva el id y jamás el perfil: al reabrir se resuelve contra el catálogo de ESA máquina, con sus invariantes y sus puertos prohibidos. Lo peor que logra un archivo manipulado es nombrar otro juego que ya está en el catálogo, o sea algo que el usuario podía elegir con dos clics. Si el perfil ya no está, la sala reabre sin juego.

**Los miembros y las reglas no se restauran del disco.** Los miembros son lo que reporte el motor y las reglas se recalculan desde ahí. Restaurarlas abriría puertos hacia direcciones que hoy pueden no ser de nadie, que es justo lo que la cuarentena por defecto existe para impedir. Con cero miembros presentes el conjunto deseado es el vacío, así que reponer el juego no abre nada hasta que haya alguien de verdad.

**Se falla en vez de mudar la subred.** Una laptop que abrió la sala en casa y la reabre en la oficina puede tener ahora una LAN que pisa ese rango. Elegir otra subred rompería justo la reconexión por la que esto existe, porque las credenciales emitidas apuntan a la vieja. Se dice que no cabe, el usuario descarta y crea una sala nueva.

### Volver a la última sala, del lado del invitado

Simétrico y mucho más chico. El invitado guarda **el código, el seed, el nombre de la sala y su nick**, y nada más.

**Jamás la credencial y jamás la identidad de la red real.** Volver pasa otra vez por el vestíbulo: el host reemite y ve llegar a quien llega, y eso es lo que mantiene con sentido a la revocación. Un archivo con credencial dentro sería una llave de sala tirada en disco que sobrevive a la sesión.

El código guardado se mantiene vigente cuando el host lo renueva, porque se lo reparte a los presentes en ese mismo acto. Ver decisión 23.

### Costos aceptados

- **Entrar depende de que el host esté alcanzable en ese instante.** Si está reconectando, los ingresos fallan y hay que reintentar. Se gana control, se pierde robustez, y es un intercambio explícito.
- Aparece estado persistido que antes no existía: el registro de códigos vivos del host. Vive en `ProgramData`, ver `03-arquitectura.md`.
- El daemon del host pasa a atender un canal de ingreso, o sea superficie nueva. Se trata en la decisión 23.
- Dos incógnitas por verificar cuando el esqueleto ande: si la credencial sobrevive al reinicio del admin vía `--credential-file`, y qué le pasa a un nodo temporal cuando el admin se va.

### Lo que NO cambia

Sigue sin haber cuentas, sin correo, sin registro y sin contraseñas de nadie. **El secreto de la red real sigue sin viajar a ningún servidor.** La derivación local sigue existiendo, con el mismo Argon2id y los mismos salts versionados, para producir la identidad de encuentro del paso 1.

La derivación, que ahora produce solo la identidad de encuentro del paso 1:

```
invite ID (8 caracteres, 40 bits)
   ├─ Argon2id(inviteID, "kanpachi/v1/id")     → networkID de encuentro (16 bytes)
   └─ Argon2id(inviteID, "kanpachi/v1/secret") → secret de encuentro    (32 bytes)
```

Los parámetros de Argon2id están congelados para la v1 y hay un vector dorado en los tests que rompe si alguien los toca. El motivo es que los dos lados derivan por separado y sin hablarse: cambiar un parámetro produce otro networkID y las dos personas dejan de verse, con un síntoma imposible de diagnosticar en producción, "pegué el mismo código y estoy solo en la sala". Un esquema nuevo se agrega como `v2`, jamás editando los valores de `v1`.

**Se deriva en el cliente y no se le pregunta al seed.** El seed podría decir cuál es la red de encuentro de un invite ID, derivarla localmente hace que llegar al vestíbulo no dependa de que la API del seed esté viva ni de que diga la verdad.

### Lo que sí cambió, y hay que decirlo

Dos afirmaciones del diseño anterior dejaron de ser ciertas.

**"El seed solo ve networkIDs opacos e IPs públicas."** Ya no. El seed lleva un registro de salas: invite IDs vivos, su networkID de encuentro, la llave pública del host y una tarjeta de presentación. Ver decisión 24. Lo que sigue sin ver es el secreto de la red real, o sea que sigue sin poder entrar a ninguna sala.

**"Hacen falta 60 bits para que nadie enumere salas vivas."** Ese argumento se apoyaba en que no había backend, o sea que nadie podía limitar la tasa de intentos y la única defensa era la entropía. Ahora hay un registro que responde las consultas, así que la enumeración se corta donde se cortan todas las enumeraciones, con límite de tasa. Y el premio de acertar bajó de "entro para siempre" a "veo la tarjeta y toco la puerta". Por eso el invite ID baja a 8 caracteres, que es lo que una persona dicta por teléfono sin equivocarse.

Lo que **dejó de ser cierto** es que quien tenga el código entre para siempre. Ese era el costo aceptado del diseño anterior, y las credenciales lo eliminan: el host renueva el código, revoca una credencial, o las dos cosas.

## 3. Sin cuentas y sin links de invitación

**Evidencia directa del grupo:** un link de invitación de un dominio desconocido activó la alarma de phishing ("están pasando onion links", "¿tengo que poner la cédula también?"). El flujo con cuenta detuvo en seco a la mitad del grupo.

**Elección:** la invitación es un código que se pega en un campo. Sin correo, sin navegador, sin registro en ningún lado. El código viaja por el canal que el grupo ya usa, Telegram.

## 4. Cuarentena por defecto: la red existe, la exposición no

**El problema que originó todo:** en una LAN virtual plana, un solo equipo infectado alcanza a los demás en cada puerto que escuche (SMB 445, RDP 3389, servicios de desarrollo en 0.0.0.0). La discusión nunca fue de confianza entre amigos, fue de radio de explosión.

**Elección:**

- Cuarentena en la interfaz virtual desde la instalación, en los tres perfiles.
- Se abre únicamente lo que pide el perfil del juego activo, únicamente en el host, únicamente hacia las IPs de miembros presentes en la sala.
- 445, 3389, 22 y todo lo no listado: cerrado siempre. La API no tiene forma de abrirlos.
- Jamás exit node, jamás subnet routing, jamás IP forwarding. No existen como opción.

**La UI lo hace visible:** "Zomboid, 2 puertos UDP, visibles para 4 personas". La seguridad que no se ve no genera confianza.

### La cuarentena NO es un deny-all, y la diferencia es funcional

Este documento decía "deny all en la interfaz virtual, en ambas direcciones". Implementado literal, deja al host con la sala abierta y el juego sin conectar. La documentación del Firewall de Windows lo dice textual:

> Explicit block rules take precedence over any conflicting allow rules. More specific rules take precedence over less specific rules, **except if there are explicit block rules**.
>
> — *Windows Firewall Rules*, Microsoft Learn

O sea que un bloqueo entrante gana sobre las reglas de permiso que Kanpachi crea para el juego activo. No hay desempate por especificidad y Windows tampoco admite orden asignado por el administrador: el bloqueo gana y punto. Con la dirección de salida pasa lo mismo por otro lado: bloquear la salida del adaptador virtual impide que un invitado marque al puerto del juego del host, que es el caso central del producto.

**Lo que sostiene la promesa es la AUSENCIA.** La entrada ya viene bloqueada por defecto en los tres perfiles de Windows, así que no tener reglas de permiso ya es el deny-all, y es un deny-all que ninguna regla nuestra puede tapar. La cuarentena de base no tiene que declararlo, tiene que hacer lo que la ausencia no puede:

| Qué | Por qué |
|---|---|
| Bloqueo **entrante** de los puertos prohibidos, en todas las interfaces | Es lo que gana contra una regla permisiva que dejó el instalador de un juego, que es el escenario de la decisión 19. **No se acota ni por IP ni por adaptador**, y la razón es la misma que ya está en `core/domain/policy.go`: un bloqueo acotado que deja de casar ABRE. El instalador tampoco podría acotarlo, porque la `/24` de la sala se elige en tiempo de ejecución |
| Bloqueo **saliente** de los mismos puertos | Esto es el "en ambas direcciones" que sí se sostiene: impide que algo infectado en esta máquina barra SMB por la sala, sin tocar el tráfico del juego |

**Jamás se solapa con lo que Kanpachi abre**, y eso no es cuidado, es imposible por construcción: esos puertos no se pueden expresar en un perfil, lo impide `forbiddenPorts` en el dominio y lo recomprueba `BuildRuleSet`. Encima es un upgrade sobre lo que había escrito, porque convierte la invariante de puertos prohibidos de una ausencia ("no hay forma de abrirlos") en una presencia que gana sobre reglas ajenas.

### Dos grupos, con dueños distintos

Las tres reglas de arriba son estáticas: no cambian con la sala, ni con el juego, ni con los miembros. Y no pueden vivir en el conjunto declarativo, por dos razones que apuntan al mismo sitio.

La primera es de tipos. La base necesita **bloqueo** y necesita **salida**, y `FirewallRule` no puede expresar ninguno de los dos, a propósito: no hay campo para acción ni para dirección, y esas ausencias son invariantes. Meterla en el `RuleSet` obliga a agregarle a un tipo cuya seguridad viene justamente de no tenerlos.

La segunda es de ciclo de vida. El daemon purga por grupo al arrancar. Con un solo grupo, o la base sobrevive a la purga y entonces "purgar todo lo etiquetado" es falso, o no sobrevive y la cuarentena dura hasta el primer reinicio del servicio. El fallo sería invisible: con la base borrada todo sigue funcionando igual, solo que la máquina queda expuesta.

| Grupo | Quién lo pone | Qué lleva | Cuándo se va |
|---|---|---|---|
| `Kanpachi` | El daemon, en cada cambio de sala | Las reglas del juego activo y el hueco del canal | Purga del arranque, o salir de la sala |
| `Kanpachi-base` | El daemon, en cada arranque, y solo AGREGA | La cuarentena de arriba | El desinstalador |

**El daemon jamás la BORRA,** así que la cuarentena sigue puesta con el servicio detenido, deshabilitado o a medio desinstalar. Lo vigila `internal/arch/grupobase_test.go`, que además comprueba lo que un lector distraído rompería sin notarlo: `Kanpachi` es prefijo de `Kanpachi-base`, así que una purga escrita con `HasPrefix` en vez de igualdad se lleva la cuarentena por delante.

**Alternativa descartada:** meter la base dentro del conjunto deseado que el daemon reaplica. Deja de existir cada vez que el daemon no está, que es justo cuando más hace falta, y paga el costo de agregarle acción y dirección a `FirewallRule`.

### La cuarentena la escribe el daemon, porque no hay instalador

Esto decía que la ponía el instalador y que el daemon jamás la nombraba. **No hay instalador**, y una cuarentena que depende de un programa que no existe es una promesa apagada: la máquina arranca sin ella y nadie se entera, porque todo sigue funcionando igual.

**Elección:** la escribe el daemon en cada arranque, con `ApplyBaseQuarantine`, antes de `PurgeOwned`.

Antes de la purga y no después, porque la purga es el instante de menos protección de todo el arranque, con las reglas de la sala anterior cayendo. Y su fallo es fatal: un daemon que no pudo escribirla es un daemon con la promesa apagada, y arrancar igual le dejaría al usuario la app abierta diciendo que todo está bien encima de una máquina sin lo único que la protege con el servicio parado.

**Lo que cambia es quién la pone. Lo que NO cambia es lo que la hace valiosa,** y por eso la regla que protege se movió en vez de desaparecer:

| Antes | Ahora |
|---|---|
| El daemon jamás nombra el grupo base | Lo nombra **un solo paquete**, `daemon/adapter/firewall/windowscom/` |
| — | **Ninguna llamada destructiva de `daemon/` puede apuntarle**, comprobado por AST |
| — | **No existe el método para borrarla en `FirewallPort`**, comprobado sobre la interfaz |

El tercero es el que de verdad reemplaza al primero. Un barrido de llamadas caza a quien la borre hoy; una comprobación sobre la interfaz caza a quien haga posible borrarla mañana, y la capacidad es lo que hay que impedir.

`ApplyBaseQuarantine` **solo agrega**. Repone lo que falte y reactiva una regla propia que alguien haya desactivado, en el sitio, sobre el objeto exacto que devuelve la enumeración. Una regla cuyo alcance cambió se avisa en el log y se deja: reescribirla obligaría a borrarla primero, y esa capacidad vale más que cerrar una edición que un administrador hizo a conciencia en su propia máquina.

**Alternativa descartada:** reponerla como "purgo y vuelvo a escribir", que es la forma natural de reponer cualquier cosa. Convierte cada arranque del servicio en una ventana sin protección, y si ese arranque falla a la mitad, la ventana no se cierra nunca.

**Alternativa descartada:** ensanchar `FirewallRule` con un campo de acción para que la cuarentena entre en el `RuleSet`. Ese tipo es el que produce `BuildRuleSet` a partir de un perfil del catálogo, o sea de un archivo que el usuario puede importar: con un campo de acción ahí, **un perfil de juego podría emitir bloqueos**. Hoy la invariante "un perfil solo ABRE" la sostiene el tipo. Se resolvió con un tipo aparte, `QuarantineRule`, que no tiene campo de acción y por lo tanto solo puede expresar bloqueos.

### Sin permiso de ICMP echo

Esta cuarentena prometía un permiso de ICMP echo "para que el diagnóstico funcione". **No se escribe.**

**Ninguna función del producto depende de él.** El sondeo de MTU manda el ping hacia AFUERA, y la salida no la bloquea nadie. La latencia de un miembro la mide el motor por su propio protocolo. O sea que el diagnóstico funciona igual sin la regla.

Y el costo no era pequeño: sería la única regla de la cuarentena que **abre en vez de cerrar**, sin acotar, o sea contestando el ping en toda red a la que la máquina se conecte. Para siempre, y con Kanpachi apagado, porque de eso trata este grupo. Se paga una superficie permanente por un diagnóstico que nadie usa.

**El disparador para reabrirlo** está en `07-futuro.md`: un caso de uso concreto que necesite que esta máquina CONTESTE un ping, y una forma de acotarlo al adaptador virtual que no lo convierta en nada cuando el adaptador no existe.

**Consecuencia para el adaptador del firewall, cuando se escriba:** con el bloqueo explícito de puertos prohibidos puesto, `SuspendForeign` deja de hacer falta para SMB y RDP, porque el bloqueo ya gana sobre esas reglas ajenas. Le queda su caso real, que es una regla permisiva del propio juego en su propio puerto.

### El único hueco que no pide ningún perfil

El canal de la sala de la decisión 23 necesita que el host escuche en un puerto de la interfaz virtual, y la interfaz nace en cuarentena. O sea que la cuarentena tiene exactamente una excepción que no pide ningún perfil, y conviene nombrarla en vez de descubrirla:

| Regla | Dónde escucha | Quién puede llegar | Mientras |
|---|---|---|---|
| La puerta | La IP del host en el vestíbulo | El `/24` fijo del vestíbulo entero | Haya sala abierta |
| La sala | La IP del host en `kanpachi0` | Solo las IPs de los miembros presentes | Haya al menos un miembro |

**Solo en el host, y el puerto es del producto, no de ningún juego.** Un invitado no escucha nada, así que su cuarentena queda literalmente intacta.

**La puerta es la única regla del programa que se expresa con un prefijo y no con una lista.** No hay alternativa: quien está entrando todavía no es miembro y su dirección en el vestíbulo es aleatoria, así que la regla tiene que existir antes de saber quién va a llegar. Lo que impide que ese campo se convierta en la forma de escribir "cualquiera" es que se valida contra el `/24` del vestíbulo, y cualquier otro prefijo se rechaza con un error propio. Lo que la acota de verdad no es la regla: es que por la puerta solo se puede pedir una cosa, una credencial.

**Se recalcula con los miembros presentes, como las de juego.** Consecuencia buena y gratis: expulsar cierra el hueco para el expulsado en el firewall, y no solo en la lista del oyente. Son dos capas que fallan por motivos distintos, que es la doctrina de la decisión 26 aplicada acá.

Lo que NO vive en el conjunto declarativo: la cuarentena de base, o sea el bloqueo de los puertos prohibidos en las dos direcciones. La escribe el daemon en cada arranque con el grupo `Kanpachi-base`, y jamás la borra. Es la instalación, no la sala.

## 5. Cliente solo Windows en la v1

**Elección:** Windows desde el primer commit, único cliente de la v1.

**Razones.** Todo el grupo juega en Windows. Hay una razón de proceso igual de importante: en un proyecto de fines de semana el recurso escaso es la motivación, y la motivación se sostiene jugando con lo que se construye. Desarrollar primero en Linux optimizaba horas de compilación, no el resultado.

Linux vive como servidor (el seed corre sobre systemd en el droplet) y la interfaz `netfw` queda declarada por si un día se agrega el cliente.

## 6. Servicio residente + UI sin privilegios

**Alternativas:** app monolítica elevada. Servicio bajo demanda con permisos de arranque relajados. Servicio residente.

**Elección:** servicio residente con arranque automático retrasado, UI normal sin privilegios.

**Razones.** Un solo UAC en toda la vida del producto, durante la instalación. El servicio inactivo pesa unos 20 MB. La alternativa bajo demanda exigía relajar el descriptor de seguridad del servicio con `sc sdset`, una superficie más que asegurar sin beneficio real.

## 7. El instalador configura todo

**Regla de producto:** si algo puede detectarse, no se pregunta. Si no puede detectarse, se elige un default sensato. Nunca se configura.

Ruta de Steam por registro, juegos por manifiestos, rango de IP evitando colisión con la LAN real, MTU por sondeo, semillas compiladas más DNS. El detalle está en `04-flujos-y-configuracion.md`.

**Inno Setup y no MSI:** MSI aporta despliegue por directiva de grupo, irrelevante aquí. Inno da un exe único, script legible, desinstalador decente y modo silencioso.

## 8. Catálogo en capas, verificado por uso real

**Regla dura:** ningún perfil llega a estado verificado sin haberse probado en una partida real con dos personas o más. No se puede marcar a mano. El campo `verified` guarda fecha, autor, método y versión del juego. Un perfil con puertos adivinados destruye más confianza que un juego ausente.

**Tres capas con precedencia:** builtin (viene en el instalador), mine (creado con el creador de perfiles), imported (compartido por alguien). Nada se sobreescribe en silencio y el origen se muestra siempre en la lista de juegos.

**El catálogo describe, el código decide.** Las invariantes (puertos prohibidos, tope de rangos, solo entrantes, jamás permitir por ejecutable) viven en código sin campo equivalente en el JSON. Por eso compartir perfiles entre personas es seguro: lo peor que puede hacer un perfil malicioso es impedir que ese juego conecte.

**Compartir sin firma criptográfica, deliberadamente.** Una firma daría una sensación de autoridad que no corresponde. Lo que protege a quien importa son las invariantes, que corren de todos modos, más el origen visible en la UI.

**La detección de instalados ordena, jamás filtra.** Los juegos detectados aparecen arriba como atajo, la biblioteca completa siempre está a un click y elegir algo no detectado funciona igual, sin advertencias. La detección falla por razones normales (juegos fuera de Steam, Epic, GOG, Xbox PC, unidades ilegibles, manifiestos con formato inesperado) y ninguna puede impedir crear una sala. Kanpachi no tiene por qué tener la razón sobre qué hay instalado en la máquina del usuario.

El flag `lan_discovery` se decide por juego: activa el relay de broadcast solo para los títulos que descubren partidas por LAN. Activarlo globalmente sería ruido y superficie innecesaria.

Detalle completo en `06-catalogo.md`.

## 9. Rendezvous en el droplet existente, relay de datos mínimo

**Elección:** el droplet de DigitalOcean que ya se paga hace de seed: presenta a los peers y coordina el hole punch. El relay de datos queda desactivado o con tope estricto.

**Razones.** El rendezvous es tráfico de kilobytes, cabe gratis en un servidor que ya existe. El coordinador es lo que sube la tasa de conexión directa del ~70% al 90%+. El relay de datos es el único costo variable y el único vector de abuso, por eso se separa a un VPS dedicado el día que el producto sea público, no antes.

**Detalle de infraestructura:** la semilla compilada es la Reserved IP del droplet, nunca la IP pública de la máquina. La IP reservada se mueve a otro servidor sin releasear el cliente.

## 10. Direccionamiento desde 100.64.0.0/10, con salida de emergencia

Asignar `192.168.1.0/24` a la sala rompería internet a media casa del grupo. Por eso se usa `100.64.0.0/10`, el espacio compartido de RFC 6598, que es el mismo motivo por el que Tailscale lo eligió: no choca con `10.0.0.0/8` ni `192.168.0.0/16`.

**El costo, y es serio para este grupo.** Ese rango es justamente el que los ISP usan para CGNAT, y CGNAT es dominante en América Latina. Tailscale documenta el conflicto, y su solución oficial es desactivar IPv4 en la tailnet y funcionar solo con IPv6. Para nosotros eso es inviable: el descubrimiento LAN y el netcode de los juegos viejos son IPv4.

**Nuestra respuesta:** el daemon lee la tabla de rutas antes de asignar, elige un `/24` libre dentro de `100.64.0.0/10`, y si detecta que la LAN o algún adaptador ya vive en ese bloque, se muda a `10.99.0.0/16`. El rango elegido y el motivo salen en `Diagnostics`.

**Un error que no vamos a cometer:** Tailscale instala una regla que descarta todo el tráfico de `100.64.0.0/10`, y eso ha roto conectividad a usuarios detrás de CGNAT y a servicios internos de algunas nubes que usan ese espacio. Nuestro alcance es siempre el `/24` de la sala, jamás el bloque completo.

## 11. Sin telemetría

Cero datos salientes en el modo privado. El diagnóstico es un botón que copia un reporte local al portapapeles, el usuario decide si lo comparte y con quién. Los logs son locales, rotados, en texto legible.

## 12. Auditoría de exposición fuera de Kanpachi

**El hueco:** Kanpachi controla lo que entra por `kanpachi0`. No controla en qué interfaces escucha el servidor del juego. Zomboid y la mayoría hacen bind a `0.0.0.0`, o sea escuchan también en la tarjeta física, y muchos instaladores de juegos agregan por su cuenta una regla permisiva en el Firewall de Windows. Resultado: el host puede quedar alcanzable desde su LAN doméstica con Kanpachi impecable.

**El hueco tiene una segunda mitad, y es peor.** Una regla ajena que permite la entrada "en cualquier interfaz" no se limita a la tarjeta física, **también aplica a `kanpachi0`**. O sea deja ese ejecutable abierto a todos los miembros de la sala, por fuera del conjunto de reglas que Kanpachi calcula, y por fuera del alcance de una expulsión (ver decisión 22). El escenario concreto: el instalador de un juego dejó una regla permisiva, y ahora cualquiera que tenga el código alcanza ese proceso sin importar el perfil activo ni quién esté autorizado.

Por eso la auditoría no es un detalle de cortesía sobre la red de casa, es parte de la contención dentro de la propia sala, y por eso la alerta se muestra siempre, incluso cuando el usuario no tiene una LAN doméstica que le preocupe.

**Alternativas:** ignorarlo por estar fuera del alcance. Forzar el bind acotado del juego. Detectar y avisar.

**Elección:** detectar y avisar, con desactivación temporal opcional.

Al activar un perfil, `netfw` audita si existe una regla de entrada permisiva para el ejecutable del juego en los perfiles Privado o Público. Si la hay, la UI lo dice y ofrece desactivarla mientras dure la sala, restaurándola al salir.

**Razones.** Forzar el bind acotado depende de que cada juego lo permita y de editar sus archivos, invasivo y frágil. Ignorarlo contradice la promesa central del producto: si Kanpachi existe para que nadie exponga su máquina, avisar de una exposición que ya está ahí es exactamente su trabajo. Refuerza el mensaje: Kanpachi no solo evita abrir puertos, cierra los que otros dejaron abiertos.

**Costo aceptado:** tocar reglas que Kanpachi no creó. Mitigación: nunca se borran, solo se desactivan; se registra su estado previo; se restauran al salir de la sala y también en el arranque del servicio si quedó algo pendiente por una salida sucia. Siempre con confirmación explícita del usuario, jamás automático.

## 13. Kanpachi no vigila qué ejecutas

**Alternativas:** detectar que arrancó un juego del catálogo y aplicar su perfil solo. Detectarlo y sugerirlo con un banner. No detectar nada.

**Elección:** no detectar nada. El estado de la sala es siempre manual.

**Razones.**

1. **Abrir puertos sin que nadie lo pida contradice el producto entero.** El valor de Kanpachi es que la exposición sea explícita y visible en pantalla. Automatizarla la vuelve invisible, que es exactamente el defecto de Hamachi.
2. **Detectar ejecución exige vigilancia permanente.** Sería un servicio elevado observando qué programas abre el usuario, todo el tiempo. Mucho más invasivo que el problema que resuelve, e imposible de defender ante alguien desconfiado.
3. **Falsos positivos.** Abrir el juego para jugar solo no debería abrir puertos de nada.

La versión "sugerir en vez de aplicar" se evaluó y se descartó por la razón 2: sugerir también exige vigilar.

**La única excepción, acotada:** el modo observación del creador de perfiles toma una foto de la tabla de sockets de un proceso. Es opt-in, ocurre dentro de un asistente que el usuario abrió a propósito, lo dispara el usuario con un botón, solo mientras esa ventana está abierta, y no se ejecuta jamás durante el juego normal. Detalle en `06-catalogo.md`.

**Nota relacionada:** la auditoría de reglas ajenas de la decisión 12 consulta el **Firewall de Windows** por ruta de ejecutable. No mira procesos en ejecución.

## 14. Jamás habilitar Detección de redes ni Compartir archivos

**El caso de estudio:** [Zerotier_Gaming_Fix](https://github.com/gomaaz/Zerotier_Gaming_Fix) es un proyecto que arregla el multijugador LAN sobre ZeroTier en Windows. Su lista de ajustes es excelente y la adoptamos casi entera en `netcfg`. Hay un punto donde tomamos el camino contrario: ese fix habilita explícitamente los grupos de reglas de **Detección de redes** y **Compartir archivos e impresoras**.

**Por qué nosotros no.** Esos grupos se habilitan **por perfil de firewall**, no por adaptador. Encenderlos para que funcione la red virtual los enciende también en cualquier otra red que esté en el mismo perfil, incluida la LAN de casa del usuario. O sea, el arreglo abre SMB en la red doméstica para que un juego se vea en la lista de partidas. Es literalmente el escenario que originó Kanpachi.

**Qué hacemos en su lugar.** Nuestras reglas se acotan por `LocalAddresses` y `RemoteAddresses`, así que el efecto queda contenido a la red virtual y a los miembros presentes. Cuando un juego necesita broadcast, se resuelve con la ruta `255.255.255.255/32` y el relay de broadcast del motor, sin tocar grupos de reglas globales.

445, 3389, 22 y todo lo no listado en el perfil siguen cerrados en cualquier caso, incluso si el usuario lo pidiera.

## 15. No depender de que Windows clasifique bien la red

Un adaptador sin puerta de enlace queda como "Red no identificada" y cae en el perfil Público, porque NLA identifica redes por la MAC de la puerta de enlace. Kanpachi intenta fijar la categoría en Privada, con el valor escrito también en el registro para que sobreviva a las reidentificaciones, **y no depende de conseguirlo**: todas las reglas se aplican a los tres perfiles.

**Descartado:** la directiva de grupo que trata toda red no identificada como privada. Afecta a cualquier red que NLA no logre identificar, incluida la conexión principal del usuario. Debilitar el firewall del usuario para arreglar nuestro adaptador es inaceptable en un producto cuyo argumento central es la contención.

## 16. El seed viaja dentro del código

**El problema:** si el proyecto se abre, la gente que no quiera depender del seed de Accentio necesita apuntar al suyo. Pedir una URL de servidor en la primera pantalla es fricción del tipo exacto que este producto existe para eliminar.

**Alternativas:** pantalla de configuración inicial pidiendo la URL. Lista comunitaria de seeds. El seed embebido en el propio código de sala.

**Elección:** embebido en el código, con parser tolerante a seis formatos y la forma URL como la que se genera por defecto.

```
A7K2M9QX                          → seed por defecto
kanpachi.accentio.dev/A7K2M9QX    → ese seed
A7K2M9QX@seed.midominio.com       → ese seed
```

**Un invite ID solo significa algo en el seed que lo emitió.** No es global, es local a un registro. `A7K2M9QX` en `kanpachi.accentio.dev` y `A7K2M9QX` en `seed.midominio.com` son dos salas distintas que no se conocen. La forma URL lleva su seed encima y por eso es la que se genera; un ID pelado significa el seed por defecto, jamás el último usado.

**Razones.**

1. **La independencia la paga quien la quiere, no sus invitados.** El self-hoster configura su URL una vez en Avanzado, y desde ahí todos los códigos que genera la llevan solos. Sus amigos no configuran nada.
2. **La forma URL puede existir de verdad.** Quien haga click sin tener Kanpachi cae en una landing que explica qué hacer y ofrece la descarga. Resuelve invitación y distribución en el mismo string.
3. **Un solo campo tolerante evita enseñar formatos.** El usuario pega lo que le llegó y funciona.

**La tensión, nombrada.** Un código con forma de URL trae de vuelta la estética que asustó al grupo con el link de Tailscale ("están pasando onion links", "¿tengo que poner la cédula?"). La diferencia está en qué pasa al usarlo: aquel abría un navegador y pedía login, este se pega en la app y no pide nada, y si alguien lo hace click igual encuentra una landing propia en vez de un formulario. La decisión depende de que esa landing exista y sea obvia.

**Costos aceptados y sus mitigaciones:**

- Pegar un código puede conectarte al servidor de un desconocido. Ese servidor ve tu IP pública, el invite ID que consultaste y la red de encuentro que le corresponde. Jamás ve el secreto de la sala real, así que no puede unirse a ella. La confirmación dentro de la app es obligatoria y no se recuerda, ver decisión 17.
- El manejador `kanpachi://` es superficie de ataque clásica. Validación estricta de todo lo que entre por ahí.
- **El seed tiene que ser un nombre y no una dirección.** Salió de la auditoría de ciberseguridad: de ese valor salen dos destinos, la consulta al registro y los `--peers` con los que arranca el motor, así que un literal de IP hace que el daemon, que corre como SYSTEM, hable con la red de casa de quien pegó el código o con su propia máquina. Se exige que la última etiqueta lleve una letra, que cierra también las formas legadas que el resolver acepta y un comprobador de IP bien formada deja pasar, `127.1` y `0x7f.0.0.1` entre ellas. Quien hospede el suyo necesita un nombre, que es gratis. Un nombre bien formado puede resolver igual a una dirección privada, y eso lo comprueba el adaptador al conectar con `domain.CheckSeedAddr`, en cada uso.
- Un código pelado siempre usa el seed por defecto, nunca el último usado. Recordar el último produce fallos inexplicables cuando un amigo manda un código de otra procedencia.

## 17. La página de invitación, con el invite ID en la ruta

**El patrón:** el mismo de Discord. La URL resuelve a una página que muestra a qué sala te invitaron, ofrece "Abrir en Kanpachi", dispara el manejador de protocolo con un click, y avisa que si no pasó nada es porque falta instalar la app.

**El formato:**

```
kanpachi.accentio.dev/A7K2M9QX            funciona pelado
kanpachi.accentio.dev/A7K2M9QX#clave      además muestra el nombre de la sala
```

### Por qué el invite ID pasó del fragmento a la ruta

El diseño anterior lo ponía después del `#` porque los navegadores no envían el fragmento al servidor, y así los logs no podían contener códigos de sala. Ese argumento **se apoyaba en que el código era el secreto**. Ya no lo es, y con la decisión 2 el seed tiene que conocer el invite ID de todas formas para resolverlo. Esconderlo del servidor que lo emitió no protege nada, y cuesta caro:

| En la ruta se gana | Detalle |
|---|---|
| Render en el servidor | La página llega armada, sin parpadeo y sin depender de que corra JavaScript |
| Vista previa en el chat | Telegram y Discord piden la URL y muestran una tarjeta con el contador |
| URL que se dicta por teléfono | `kanpachi.accentio.dev/A7K2M9QX`, sin el `#` que se veía raro |
| Una sola forma de resolver | La app y la página consultan el mismo registro, no dos caminos que se desincronizan |

**Lo que se pierde, dicho sin maquillar.** Los logs del servidor pasan a contener invite IDs. Hoy provablemente no pueden contener nada de una sala. Quien lea un log obtiene lo mismo que quien recibe el link reenviado: la tarjeta y la posibilidad de tocar la puerta. No obtiene el secreto de la red real, que no está en ningún servidor, y entrar sigue exigiendo una credencial que emite el host. El cambio es real y acotado, y se compensa con límite de tasa y retención corta.

### La tarjeta va cifrada, el contador no

La tarjeta de sala guarda nombre y nick del host. Va cifrada con una clave que **viaja en el fragmento**, o sea que el operador del seed guarda y sirve bytes que no puede leer. El contador de miembros sale del networkID de encuentro, que el seed ya ve, así que no necesita clave.

De ahí sale que el fragmento sea **enriquecimiento opcional**: un ID dictado por teléfono y tecleado a mano abre la página igual, con la tarjeta genérica y el contador vivo. El nombre de la sala aparece solo cuando llegó el link entero.

Consecuencia aceptada: la vista previa en el chat dice "Sala de Kanpachi, 4 jugando" y nunca el nombre de la sala, porque el servidor que la genera tampoco puede leerlo. Es el único punto reversible de esta decisión. Volverla plana daría previa rica a cambio de que el operador vea el nombre de cada sala y el nick de cada host.

**Tres reglas de la página:**

1. **El intent se dispara con un click, jamás al cargar.** Los navegadores penalizan la navegación a esquemas externos sin gesto del usuario, y una página que abre programas sola es lo que un desconfiado espera de un link sospechoso.
2. **No se intenta detectar si la app está instalada.** No hay forma confiable, el truco del temporizador falla distinto en cada navegador. El mensaje de "¿no pasó nada? no lo tienes instalado" cubre el caso sin adivinar y sin equivocarse.
3. **Las tres salidas conviven:** abrir, descargar, o copiar el código a mano. El código siempre visible es la vía que funciona pase lo que pase.

**Consecuencia de seguridad, importante.** Si esa página puede invocar `kanpachi://`, **cualquier página de internet puede**. El manejador de protocolo queda expuesto a toda la web, no solo al dominio propio. De ahí sale una regla general del producto:

> **Nada que llegue de fuera de la app surte efecto sin una confirmación dentro de la app.**

La app abre, muestra qué recibió (sala y servidor) y espera un click. Siempre, sin excepciones, **sin ningún estado recordado que permita saltarse la pantalla**. El diálogo del navegador no cuenta como confirmación: pregunta si abrir Kanpachi, no dice a qué sala ni a qué servidor, la gente lo despacha sin leer, y ofrece un "recordar mi elección" que lo elimina para siempre.

Una simplificación cae de aquí: como la confirmación es siempre, **desaparece el estado de hosts de confianza recordados** que tenía el diseño anterior. Menos que persistir, menos que sincronizar, menos formas de equivocarse.

La regla vale para cualquier canal externo que se agregue después: argumentos de línea de comandos, archivos asociados, lo que sea. Si el origen está fuera de la app, hay tarjeta.

Además, validación de entrada hostil en ese canal: solo el formato exacto del código, tope de longitud antes de parsear, nada de rutas ni argumentos, y un código inválido nunca deja la app en un estado raro.

## 18. Alcance negativo explícito

La v1 no hace: compartir archivos, chat, voz, salas persistentes, cuentas, panel web, móvil, macOS, autoupdate. Cada "y si también..." va a `07-futuro.md`, no al código. El alcance negativo escrito es la defensa contra el scope creep, uno de los tres riesgos reales del proyecto.

**"Salas persistentes" significa una sala que existe sin nadie dentro**, sostenida por un servidor al que se puede volver cuando sea. Eso sigue fuera de la v1 y vive en `07-futuro.md`.

Lo que la decisión 2 sí hace, y conviene no confundir, es que **el host reabra la suya con el mismo código**. La diferencia es dónde vive el estado: ahí es un archivo en el disco del dueño de la sala, y si él no la reabre no existe para nadie. Acá sería un servidor sosteniéndola para todos.

## 19. Módulo de alertas de exposición

**El hueco:** hay tres cosas fuera del control de Kanpachi que anulan su promesa entera, y las tres son silenciosas. El Firewall de Windows apagado, un puerto reenviado en el router, y las reglas permisivas que dejó el instalador de un juego. Con cualquiera de las tres, Kanpachi puede estar impecable y el usuario expuesto.

**Alternativas:** ignorarlo por estar fuera del alcance. Que el seed sondee desde afuera el puerto público de cada miembro. Chequeos locales asíncronos.

**Elección:** chequeos locales, en un módulo asíncrono que no bloquea nada y publica alertas por la API.

**Por qué el seed no sondea.** Para probar si un puerto está abierto al público, el seed necesita saber cuál es. El puerto 16261 es Zomboid, el 25565 es Minecraft. O sea el seed aprendería qué juega cada quien, exactamente lo que este documento y `07-futuro.md` prohíben incluso en el escenario público con métricas opt-in. Los chequeos locales responden la misma pregunta sin pagar ese precio, y de paso funcionan con el seed caído.

Qué revisa:

| Chequeo | Cómo | Qué dice la alerta |
|---|---|---|
| Firewall de Windows desactivado | `INetFwPolicy2`, perfil por perfil | Que la contención de Kanpachi vive ahí, y que conviene encenderlo |
| Mapeo de puerto en el router | Consulta de solo lectura al IGD por UPnP o NAT-PMP | Que hay un puerto abierto a internet, y que Kanpachi no lo necesita |
| Regla ajena permisiva del juego | Almacén de reglas del Firewall, por ruta de ejecutable | Que ese juego queda alcanzable fuera de Kanpachi, ver decisión 12 |
| Reglas de Kanpachi ausentes o alteradas | Autochequeo del grupo `Kanpachi` | Que la sala no está aplicada como se declaró |
| Alguno de los dos chequeos locales no contestó | El propio módulo, al fallar la consulta | Que nadie está mirando, que no es lo mismo que "todo bien" |

**Excepción explícita a "el router no se toca nunca".** La consulta al IGD es de **solo lectura**. Kanpachi jamás crea ni borra un mapeo, jamás pide uno. Leer para detectar una exposición sirve a la invariante, escribir la violaría. La distinción va en el código como dos operaciones separadas, con la de escritura inexistente.

**Tres reglas de manejo:**

1. **Las alertas nunca bloquean.** Informan. Una alerta que impida crear una sala convierte cualquier falso positivo en alguien que se queda sin jugar, y eso contradice el principio de que la detección ordena y jamás filtra.
2. **Asíncrono y aislado.** Corre en su propio ciclo, con su propio timer. Un fallo del módulo no toca la conexión, ni el firewall, ni el estado de la sala.
3. **Cada alerta dice qué pasa, qué significa para el usuario y qué hacer**, en ese orden, igual que el resto de los textos del producto. Ver `05-ui.md`.

### La única que Kanpachi sí repara es la suya

De las cuatro, tres describen la máquina del usuario y Kanpachi las dice sin tocarlas: su firewall, su router y las reglas que instaló un juego suyo son suyos. La cuarta es distinta, porque son **las reglas del grupo `Kanpachi`**, o sea la propia declaración del producto.

**Reponerlas no es arreglarle la máquina al usuario, es volver a hacer cierta su propia declaración.** Un hallazgo que se denuncia y no se repara deja la cuarentena rota mientras el usuario lee el aviso. Funciona porque el puerto del firewall es declarativo y calcula la diferencia contra las reglas VIVAS del grupo, no contra un recuerdo en memoria: reaplicar el mismo conjunto repone lo que alguien borró y quita lo que alguien agregó.

**Se repone tres veces y después se avisa.** Tres distingue los dos casos que producen el mismo síntoma: el toque puntual de alguien mirando la consola del firewall, que se arregla con una reaplicación, y algo que las quita en bucle, que suele ser un antivirus. Contra lo segundo, insistir es pelearse a golpe de COM y eso no lo gana nadie.

### El módulo también avisa cuando deja de mirar

**El agujero que esto tapa:** con las consultas fallando, el módulo producía **cero alertas** y la pantalla quedaba en verde. El módulo que existe para avisar que la promesa se rompió no podía avisar que él mismo había dejado de mirar, y desde la pantalla "no se pudo comprobar" y "todo en orden" se veían idénticos.

**Elección:** una alerta propia, `AlertAuditFailed`, cuando falla cualquiera de los dos chequeos **locales**, el estado del firewall o las reglas propias.

**No es pegajosa.** Se recalcula en cada barrido, igual que la del firewall apagado, así que en cuanto la consulta vuelve a contestar el aviso se va solo. Pegajosa se quedaría encendida para siempre tras el primer fallo de COM, porque solo `DropAlerts` la quita y nadie tendría motivo para llamarla, y una alerta eterna deja de ser información.

**La consulta al router NO cuenta.** Falla en la mayoría de los routers del mundo, que nunca contestan al IGD, así que incluirla dejaría la alerta encendida en casi todas las máquinas. Una alerta que está siempre no significa nada, y peor, enseña a ignorar las que sí importan.

**Alternativa descartada:** que un adaptador provisional devuelva datos falsos "en la dirección segura", tipo los tres perfiles apagados. Levanta `AlertFirewallOff`, que es **falso**, porque el firewall del usuario está encendido. Una alerta que miente rompe la decisión 19 más que el silencio: quien la investiga y no encuentra nada aprende a no investigar la siguiente.

## 20. La sala vive mientras haya alguien conectado

**El malentendido que esta decisión evita:** la palabra "sala" nombra tres cosas distintas, y confundirlas lleva a diseñar un servidor de salas que este producto no tiene.

| Qué | Qué es | Quién manda |
|---|---|---|
| **La red** | La red cifrada derivada del código | Nadie. Existe mientras quede al menos un nodo conectado |
| **La sala** | La gente conectada en este momento | Nadie, en el sentido de un servidor con estado |
| **El servidor del juego** | Un proceso en la máquina del host | El host, por completo |

**Elección:** la red existe mientras quede un nodo. Cuando el último se desconecta, deja de existir, y no queda estado en ningún lado. Volver a jugar exige un código nuevo, y eso cuesta un click.

**El host se va y puede volver.** Si el host cierra el juego o reinicia la máquina, sus puertos se cierran y los demás se quedan en una red inerte: nadie tiene nada abierto, porque los clientes nunca abren puertos. La UI lo dice con esas palabras y ofrece salir. Cuando el host vuelve, reactiva el perfil y las reglas se regeneran para los miembros presentes.

**Salida automática por inactividad del host, a los 20 minutos.** Cada cliente cuenta por su lado, sin coordinación y sin mensajes: si el host lleva 20 minutos ausente, sale de la sala y vuelve a Idle. Resuelve el caso real de que el host reinicie la máquina y se olvide de abrir Kanpachi, dejando a los demás en una sala sin sentido.

Veinte minutos y no dos: el margen tiene que cubrir un reinicio con holgura. Una salida inmediata echaría a todo el mundo cada vez que al host se le cierra el juego, que es el caso más común de todos.

**Los veinte minutos cuentan desde la última PRUEBA DE VIDA del host, no desde que este código se enteró.** Cuando el host se da por ausente por silencio y no por caída de socket, la marca se fecha hacia atrás, en el último momento en que se supo de él. Sin eso, el límite de silencio se sumaría al contador y veinte minutos serían veintiséis.

**Descartado: la expulsión inmediata coordinada por mensaje.** Sería cooperativa, no hay servidor que la imponga y un cliente modificado se queda igual, así que daría sensación de control sin control. El timeout local no necesita que nadie obedezca: es cada máquina decidiendo sobre sí misma.

### Saber que el host no está: tres señales independientes

Una sola señal es una sola forma de fallar. El contador de 20 minutos se arma con cualquiera de estas tres, y ninguna necesita que las otras funcionen:

| Señal | Latencia | Qué la produce | Qué falla que las otras cubren |
|---|---|---|---|
| **La conexión de control cae** | segundos | El socket al host se cierra | Un socket TCP medio abierto sobrevive horas a una máquina apagada de golpe, sin FIN y sin RST. Esta señal puede no llegar nunca |
| **Silencio del host, 6 minutos** | 6 min | El host reanuncia cada 2 minutos, y se dejaron de recibir tres anuncios seguidos | Necesita que el canal de control exista. Si el adaptador del canal está roto o nunca arrancó, no mide nada |
| **El host no está en la tabla de miembros del motor** | segundos | El motor propaga la tabla entera a cada nodo, así que la `.1` del host está o no está | No dice nada del canal de control: un host cuya máquina sigue en la red con Kanpachi colgado sigue apareciendo |

**La tabla de miembros solo puede APAGAR la presencia, jamás encenderla.** La asimetría es el punto entero. Que el motor reporte al host prueba que su nodo está en la red, y no prueba que su canal de control funcione. Encenderla desde ahí desarmaría el contador con evidencia que no lo respalda, y el caso real que rompería es el host que dejó la máquina encendida con la app colgada.

### El túnel también tiene un plazo, y es más corto

Hermano del anterior, del lado de la red y no de la persona. Si esta máquina se queda sin túnel y no vuelve en **10 minutos**, sale de la sala, cierra los puertos y revierte sus ajustes.

**Diez y no veinte, y la asimetría tiene razón.** La ausencia del host es que falta la persona con la red impecable, y ahí esperar un reinicio completo vale la pena. Esto es lo contrario: sostener una sala sin red durante veinte minutos solo consigue que el usuario mire una pantalla que miente.

El motivo de salida es propio, "la conexión con la sala no volvió", y no se reusa el de "no se pudo entrar": ese texto, dicho después de hora y media jugando, es mentira.

**Es el respaldo del watchdog del supervisor, no su competidor.** El watchdog agota sus ocho reintentos en 3 minutos y 18 segundos, o sea bastante antes. Ese orden importa: si los dos plazos se cruzaran, la sala se cerraría a mitad de un reintento que iba a funcionar. Hay un test que falla si alguien toca los números y los cruza.

### La sala es independiente del juego activo

Son dos capas, y confundirlas lleva a rediseñar la sala cada vez que cambia el juego:

| Capa | Qué es | Qué la cambia |
|---|---|---|
| **Sala** | Estar en la red cifrada con los demás | Entrar, salir, renovar el código, ser expulsado |
| **Juego activo** | Puertos abiertos, únicamente en el host | El host elige otro juego, o ninguno |

Estar en una sala **sin juego activo es un estado válido y es el estado por defecto al entrar**: red cifrada, cero puertos abiertos, nadie alcanza a nadie.

Que el host cambie de juego no toca la sala. `policy/` recalcula el `RuleSet`, `netfw` aplica la diferencia, y nadie se reconecta ni vuelve a pegar un código. Es la misma operación que ejecuta un cambio de miembros.

**Nadie hereda el rol de host.** Una sala tiene un host, el que la creó. Si se va, no hay elección ni promoción automática. Que otro hospede significa crear una sala nueva, que es un click, y evita el caso de dos máquinas abriendo puertos para el mismo juego sin que nadie lo haya pedido.

**Lo que "ser host" sí significa,** que es propiedad real y conviene nombrarla: elige el juego, los puertos se abren únicamente en su máquina, se cierran cuando sale, y decide a quién expulsa. Nadie más puede abrir nada en esa sala.

## 21. Nombre obligatorio, sin verificar

**El problema:** en la lista de miembros hay que poder distinguir a las personas. Sin eso, expulsar a alguien es adivinar.

**Elección:** la UI pide un nombre la primera vez, obligatorio, máximo 12 caracteres alfanuméricos. Se manda al entrar y se muestra en la lista de todos.

**Qué NO es, y es deliberado:** no es único, no se verifica, no se registra en ningún lado, no es una cuenta. Dos personas pueden llamarse igual. Es una etiqueta para que un humano reconozca a otro humano en una lista corta de amigos, nada más. Tratarlo como identidad sería exactamente la clase de promesa falsa que este producto evita.

**Beneficio adicional que resuelve un problema de privacidad.** El motor publica un nombre de equipo a todos los peers, y por defecto ese nombre es el de la máquina en Windows, que en la práctica suele contener el nombre real de la persona. Con el nombre obligatorio, ese valor se reemplaza y el nombre real deja de viajar.

**Validación estricta**, porque el valor viaja a las pantallas de otras personas: solo alfanuméricos, tope de 12, sin espacios, sin caracteres de control, sin caracteres invisibles ni de otros alfabetos que permitan suplantar a otro miembro visualmente.

## 22. Expulsar es revocar la credencial, con el firewall como segunda capa

**El problema:** el host necesita poder sacar a alguien que no reconoce, sin que haya un servidor que expulse a nadie.

**Elección:** expulsar revoca la credencial de esa persona. Sale de la red. En el mismo acto, el host quita su IP virtual del conjunto de miembros y `policy/` regenera el `RuleSet` completo.

**Dos capas que fallan por motivos distintos, y esa es la gracia:**

| Capa | Qué hace | Si falla la otra |
|---|---|---|
| **Revocar la credencial** | Lo saca de la red en ~1 s, medido | Aunque el motor tuviera un bug, el firewall ya no lo autoriza |
| **Recalcular el `RuleSet`** | Su IP deja de estar en `RemoteAddresses` | Aunque siguiera en la red, no alcanza ningún puerto |

Ninguna de las dos es cooperativa. No hay mensaje que el expulsado pueda ignorar: una es el motor cerrándole la sesión, la otra es el Firewall de Windows del host descartando sus paquetes.

**Las dos corren en el mismo acto y ninguna aborta a la otra.** Es el sentido literal de la tabla de arriba, y el código lo tenía en serie: un error del motor devolvía antes de recortar el firewall, así que un bug del motor le dejaba la sesión abierta Y el puerto autorizado, que es exactamente lo contrario de lo prometido. Ahora se ejecutan las dos siempre y los errores se juntan.

**Una expulsión a medias es un estado VISIBLE, no un rollback.** Si una capa falla, la lista de miembros dice que no está y la máquina puede seguir autorizándolo, y eso se avisa con una alerta que sobrevive al refresco del módulo de exposición. No se deshace la mitad que sí funcionó: deshacerla volvería a autorizar a quien el host acaba de echar. La UI ofrece renovar el código ahí mismo, que es el control que cierra la puerta de verdad.

**No hay ni va a haber baneo.** Banear exige guardar identidad por peer, y este producto no la guarda. El expulsado que vuelve con el mismo código entra de nuevo, y eso es deliberado: el kick saca de la sala, no impide volver. Lo único que impide volver es renovar el código.

**Expulsar recorta el canal en dos sitios, y ninguno depende del otro.** La lista del oyente, que cierra en el acto la conexión de quien ya no es miembro, y el conjunto de reglas del firewall, que se recalcula sin su dirección. Son dos capas con dos causas de fallo distintas, que es la doctrina de la decisión 26 aplicada a la superficie que corre como SYSTEM.

**El límite honesto de eso, escrito para que nadie lo descubra por su cuenta:** el que vuelve toca la puerta del vestíbulo con una dirección ALEATORIA, así que ni siquiera se parece al que se echó. Reconocerlo exigiría la llave de la decisión 25, que todavía no está implementada. Mientras no lo esté, la respuesta a "no quiero que vuelva" es una sola y es renovar el código.

**Qué pasa si intenta volver.** Con el mismo código, y si el host no lo renovó, entra de nuevo como si nada. Eso es deliberado: expulsar y bloquear son cosas distintas. Para que no vuelva, el host **renueva el código** (decisión 2), y ahí el código viejo deja de servir mientras los que ya están adentro siguen dentro, porque tienen credencial y no código.

**Los dos controles del host, que son independientes:**

- **Renovar el código** → nadie nuevo entra con el viejo. No afecta a los presentes.
- **Revocar una credencial** → esa persona sale ya. No afecta al código.

**El límite honesto, que sigue existiendo:** el nombre no se verifica y el identificador de máquina se puede cambiar, así que alguien decidido puede volver a pedir el código nuevo haciéndose pasar por otro. Contra eso lo que hay es que el host sepa a quién se lo da. Es un producto para un grupo de amigos, y esa es la frontera de confianza real.

## 23. El canal de control: solo el host escucha

**El problema:** varias piezas necesitan que las máquinas de la sala se digan cosas. Emitir credenciales al que entra (decisión 2), avisar de una expulsión, anunciar que la sala se cierra, y saber si el host sigue vivo.

**Alternativas:** un canal donde todos escuchan. Que el seed relaye los mensajes. Que solo el host escuche.

**Elección:** solo el host escucha. Los clientes marcan hacia afuera.

```
Santiago ──┐
Gabriel  ──┼──►  TCP 57623, ÚNICAMENTE en el host, en la interfaz virtual
Alvaro   ──┘     alcance = miembros presentes. Los clientes nunca escuchan
```

El puerto es fijo y no negociado, por lo mismo que el `/24` del vestíbulo: quien entra tiene que llegar sin haber hablado antes con nadie, y el canal por el que se negociaría un puerto es justamente el que se está montando.

**Por qué no un canal donde todos escuchan.** Sería un puerto abierto en cada PC, atendido por un daemon que corre como SYSTEM, parseando mensajes de gente semi-confiable. Un agujero deliberado en el deny-all, y de lejos la mayor superficie de ataque del producto: un fallo de parseo ahí es ejecución remota como SYSTEM en la máquina de cada miembro. Con el host como único oyente, **el deny-all de los invitados queda literalmente intacto** y la superficie se concentra en una sola máquina, la del host, que ya acepta más exposición por definición.

**Por qué no el seed.** Le daría poder sobre el contenido de las salas y contradice que solo vea networkIDs opacos e IPs públicas.

**Una conexión, cinco trabajos.** Cada conexión de control resuelve de una sola vez cosas que si no habría que construir por separado:

| Función | Cómo sale |
|---|---|
| Emitir la credencial al que entra | Es el paso 2 del canje de la decisión 2 |
| Expulsar | El host le avisa por ahí, y recién después revoca y recalcula. El aviso es cortesía: ver `03-arquitectura.md` |
| Presencia del host | Si la conexión cae, el host no está. Si vuelve, volvió |
| Latido | Es la misma conexión, no hace falta un ping aparte |
| Repartir el código nuevo | Al renovar, el host se lo manda cifrado a los que están dentro |

**Sobre el latido, un matiz que no lo contradice.** La conexión sigue siendo el latido para el caso normal, y no se agrega ningún ping. Lo que se agrega es que el host **repite cada 2 minutos el anuncio de sala que ya existía**, el mismo que manda al cambiar de juego, de nombre o de miembros. El motivo es que el borde de la conexión es una señal que puede no llegar jamás: un socket TCP medio abierto sobrevive horas a una máquina apagada de golpe. Con la repetición, el silencio pasa a ser medible.

### El código nuevo viaja a los que están dentro

Renovar el código tenía un efecto secundario que nadie había nombrado: los presentes se quedaban con el viejo guardado. Siguen en la sala, la partida no se entera, y el día que quieran volver tienen un código muerto.

**La confianza ya está dada.** Están dentro porque el host los dejó entrar, y renovar con ellos dentro es exactamente la señal de que se quedan. Merecen el código nuevo sin tener que pedirlo, y con él funciona "volver a la última sala".

**Va cifrado, no en claro.** Se sella contra la llave pública de cada miembro, la misma que llegó en su pedido de credencial, así que el código es legible solo por aquel para quien es. No es una llave nueva ni un almacén nuevo: vive lo que dura la sesión de esa persona y se descarta al salir, o sea que no es identidad persistida y no habilita ningún baneo.

**Y el motivo de sellarlo no es que el código valga.** El invite code es un ticket desechable, existe justamente para no tener que repartir un secreto, y renovarlo no recrea la sala: cambia la llave de búsqueda, la red real conserva su identidad y los presentes ni se enteran. Se sella para que **renovar conserve su efecto**. El motor puede relayar tráfico por otro peer, así que repartirlo en claro le devolvería al que acaba de quedar afuera el ticket que la renovación existía para quitarle.

Que el reparto falle no invalida nada. El código nuevo ya es el bueno y el vestíbulo ya está levantado con él; lo que se pierde es que el otro lo tenga guardado.

**La propiedad que hace que esto sea barato:** una conexión TCP caída es información confiable **sin necesidad de confiar en nadie**. No es un mensaje que alguien pueda falsificar, es la ausencia de un socket. Por eso la detección de presencia y el timeout de 20 minutos de la decisión 20 no necesitan firma, autenticación ni credenciales.

### Modelo de amenazas del canal

Superficie nueva, así que se escribe entera:

| Amenaza | Mitigación |
|---|---|
| Miembro manda mensajes malformados al host | El host corre como SYSTEM: parseo estricto, tope de tamaño, sin reflexión, sin deserializar tipos arbitrarios. Es el código que más revisión merece del proyecto |
| Miembro falsifica una expulsión | No puede: expulsar lo ejecuta el host sobre sí mismo, no es un mensaje que alguien pida |
| Miembro falsifica "la sala se cerró" | Lo peor que logra es que a otros se les cierre la app. Molestia, no riesgo, y ya está dentro de la sala |
| Miembro falsifica un código nuevo | No puede: los invitados no escuchan, y solo procesan lo que llegó por SU conexión al host. Un host modificado que mande un código falso logra que a ese invitado le falle su "volver a la última sala", y nada más |
| Miembro se hace pasar por el host EN LA SALA | No puede: los clientes marcan hacia una dirección conocida y no aceptan conexiones entrantes, y ahí las direcciones las asignó el host dentro de la credencial |
| Alguien con el código ocupa la dirección del host EN EL VESTÍBULO | **Puede, y hoy no hay defensa criptográfica.** Ahí las direcciones son autoasignadas, y verificar exigiría la llave larga de la decisión 25, que sigue diferida. Lo que acota el daño: solo lo intenta quien ya tiene el código, y renovar levanta un vestíbulo nuevo derivado del ID nuevo, con lo que el ocupante queda solo en el viejo. La víctima entra a la red del impostor, no a la del host, o sea que el daño es no entrar a la sala que quería |
| Un tercero lee la credencial o el código nuevo al relayarlos | Los dos van sellados contra la llave de sesión del destinatario. Eso compra CONFIDENCIALIDAD frente a quien transporta los bytes, y no autenticación del host: la caja es anónima porque no hay llave larga contra la cual verificar |
| Inundación de conexiones al host | Tope de conexiones simultáneas en la puerta, plazo para hablar, y una sola conexión viva por IP virtual: la segunda desplaza a la primera |
| Un miembro deja de recibir y traba al host | Toda escritura lleva plazo. Sin él, un cliente que abre la conexión y no lee nunca deja trabada la sesión entera, que la llama con su candado tomado, **sin haber mandado un solo mensaje inválido**. Vencido el plazo, esa conexión se cierra |

**Lo que este canal NO transporta:** nada del juego. El tráfico de la partida va por su propio camino P2P y jamás pasa por acá. Son órdenes de control y estado, en volumen de bytes.

### Las reglas del oyente, que son las mismas del catálogo

Este código corre como SYSTEM y lee de gente que está en la sala, así que se trata como entrada hostil de punta a punta. Los detalles de implementación viven en `03-arquitectura.md`; lo que hay que saber acá es que son cinco y ninguna es opcional: tope de tamaño antes de deserializar, tabla de mensajes cerrada sin reflexión, esquema estricto donde un campo de más rechaza el mensaje entero, dos alcances con dos listas de admisión distintas, y tipos del dominio que se reconstruyen por sus parsers en vez de creerse.

**El aviso de expulsión se acusa, con tope.** El canal es TCP, así que la orden se retransmite hasta que el otro lado la reconoce, y eso es lo que hace que el expulsado se desconecte solo y limpio. Sin ninguna espera queda una ventana real: mandar devuelve cuando los bytes entraron al búfer local, y un segundo después la revocación mata la conexión con lo que quedara sin salir. El tope es lo que impide que esperar el acuse convierta la expulsión en cooperativa: vencido, se revoca igual.

### Los peers se ven entre ellos, y no se puede evitar

Conviene escribirlo acá porque es la pregunta que sigue naturalmente. El motor es una malla completa con propagación de rutas tipo OSPF: cada nodo aprende la tabla de peers entera, porque así es como enruta. No hay flag que la oculte.

O sea Santiago **ve** que Victor está en la sala, con su IP virtual, su nombre y su latencia. Lo que no puede es **alcanzarlo**, porque los invitados no abren puertos.

## 24. El seed lleva un registro de salas

**El problema:** la página de invitación tiene que mostrar a qué sala te invitaron y quién la abrió, y la app tiene que resolver un invite ID a una sala concreta. Un HTML estático no puede hacer ninguna de las dos.

**Alternativas:** meter los datos en el fragmento del link, o sea que los escriba quien manda el link. Un backend de salas completo, que es el `kanpachi-rooms` que la decisión 2 eliminó. Un registro mínimo dentro del propio seed.

**Elección:** registro mínimo en el seed. Es un binario Go nuestro que corre al lado de EasyTier, dentro de la misma imagen. **Ese binario es lo que hace que `kanpachi-seed` sea algo distinto de una instalación plana de EasyTier.**

### Por qué no en el fragmento

La forma barata era que el app del host escribiera nombre y nick dentro del link. Se descartó por el caso de reenvío: quien recibe un link legítimo puede editar el fragmento y atribuirse la sala antes de reenviarlo. Con el registro, el dato viene de la sala y no del último que tocó el string. Aparte queda fresco cuando la sala se renombra, y el contador de miembros es imposible de otra forma.

**Lo que el registro NO resuelve, y conviene no engañarse:** no impide que alguien abra su propia sala poniéndose "Humberto". El nickname no se verifica por decisión 21. Contra eso sirve la decisión 25, no esta.

### Qué guarda y qué no

```
inviteID (8 chars, emitido por el seed)
   → networkID de encuentro
   → llave pública del host
   → tarjeta cifrada (nombre de sala, nick), bytes opacos para el seed
   → contador de miembros, leído en vivo del RPC de EasyTier
```

| Guarda | No guarda ni puede |
|---|---|
| Invite IDs vivos y su red de encuentro | El secreto de la red real de la sala |
| La llave pública del host | Ninguna llave privada |
| Una tarjeta que no puede descifrar | Nombres de sala ni nicks en claro |
| Un contador por red | Quién es cada miembro. Los nicks viven dentro de la red cifrada |

En memoria, con TTL. Muere con la sala, salvo la llave fijada del host, que sobrevive semanas para que reabrir con el mismo ID siga siendo del host.

### El contador sale de EasyTier, verificado

`easytier-cli peer list-foreign` sobre el portal RPC del seed devuelve peers agrupados por nombre de red, sin cooperación del host y sin unirse a nada:

```json
{ "kanpachi-deadbeefcafe": { "peers": [ {"peer_id": 3389069210}, {"peer_id": 546534178} ] },
  "kanpachi-otrasala99":   { "peers": [ {"peer_id": 2231640205} ] } }
```

Probado contra la v2.6.4 fijada, con un seed en modo servidor público y tres clientes en dos redes distintas. El mismo JSON confirma lo otro que importa: **ahí no hay hostnames.** El nick viaja dentro de la red cifrada y el seed relaya sin descifrar, así que ni queriendo podría publicarlo. Esa es la razón de que la tarjeta exista.

### Quién puede escribir una tarjeta

El problema no es obvio y por eso se escribe. La clave de cifrado de la tarjeta tiene que derivarse de algo que el navegador del invitado tenga, porque si no la página no podría descifrarla. O sea que **cualquiera que reciba el link puede producir una tarjeta válida**, y el registro, que es HTTP plano y no conoce la sala, no tiene con qué distinguirlo del host.

Se cierra con firma, no con la clave:

1. La tarjeta va firmada por la llave larga del host, decisión 25.
2. El registro **fija la primera llave pública** que ve para ese invite ID y rechaza toda actualización firmada por otra.
3. La página verifica la firma también, así que un registro comprometido que sirva basura no valida en el cliente.

El orden juega a favor: la sala no existe hasta que el host la abre y nadie puede entrar antes que él, o sea que **el host es primero por definición** y no hay carrera que perder. Para la reapertura, la llave fijada sobrevive a la muerte de la sala.

### El ID lo emite el seed

Se evaluó que lo generara el host y el seed solo verificara disponibilidad. Emitirlo el seed es mejor por dos razones y ningún costo: quien tiene que garantizar unicidad es el registro, así que emitir evita el ida y vuelta de proponer y ser rechazado; y no hay nada que filtrar, porque un invite ID no deriva material criptográfico de la sala real. La objeción de que un seed comprometido emitiría IDs predecibles solo pesaba cuando el ID **era** la llave de la red, y con la decisión 2 dejó de serlo.

### Costos aceptados

- El seed deja de ser apátrida. Estado en memoria, con TTL, sin base de datos y sin disco.
- Superficie HTTP nueva expuesta a internet. Límite de tasa obligatorio en resolución y en registro, porque 40 bits son enumerables sin él.
- Quien se autohospeda necesita este binario. Va dentro de la misma imagen, así que le sale configurado de fábrica.
- Los logs pasan a contener invite IDs, ver decisión 17.

## 25. La llave del host, contra la suplantación

**El problema, planteado por el caso concreto:** Mallory abre una sala normal, se pone de nick "Humberto", y manda el link desde el dominio real. La página consulta el registro auténtico y muestra "Humberto te invitó". Ni el registro ni el fragmento lo impiden: el nickname no es único ni se verifica, por decisión 21, y el creador de la sala elige el suyo.

**Elección:** continuidad de llave, el mismo mecanismo de SSH y de Signal.

```
1. Cada instalación genera un par de llaves al primer arranque
2. La tarjeta de sala va firmada por la llave del host
3. El app recuerda qué huella tenía cada persona con la que jugó
4. Nick conocido con huella distinta, aviso fuerte antes de entrar
```

Para suplantar a Humberto ante alguien que ya jugó con él hace falta **robarle la llave**, no elegir un nick.

### Dos propiedades que valen escribir

**El registro no necesita ser confiable.** Solo transporta bytes firmados. La verificación ocurre en el cliente contra una llave que ya conocía, así que un seed comprometido que sirva una tarjeta falsa no valida.

**La página web no puede hacer esta verificación, el app sí.** El navegador no tiene memoria de con quién jugaste. Entonces el reparto es: la página muestra el nick sin afirmar identidad, y el app, tras el click, es donde se toma la decisión con su memoria local. "Humberto, ya jugaste con él en 3 salas" contra "Humberto, esta NO es la llave del Humberto que conoces".

**La huella tampoco se muestra en la página, y eso se evaluó.** La primera versión la ponía al lado del nick. Se sacó porque una huella sin nada contra qué compararla no informa, decora, y peor: aparenta una verificación que en esa pantalla no ocurre. Mostrarla donde nadie puede juzgarla enseña a ignorarla, que es exactamente lo que hay que evitar para que signifique algo donde sí se juzga.

### El hueco que queda

La primera vez que alguien te invita no hay con qué comparar. Es el mismo hueco que tiene Signal y se cubre igual, comparando la huella por otro canal si el caso lo amerita. Escribirlo importa para no vender una garantía que no existe.

**Y mientras esta decisión siga sin implementarse, el hueco es más grande y hay que decirlo entero.** En el vestíbulo las direcciones son autoasignadas, así que **alguien que tenga el código puede ocupar la dirección del host y contestar el pedido de credencial antes que él**. El canal de la sala sella lo que manda contra la llave de sesión del destinatario, y eso compra confidencialidad frente a quien relaye los bytes, no autenticación: una firma hecha con una llave efímera que llega en el mismo mensaje no prueba nada, y por eso la caja es anónima en vez de aparentar algo.

Lo que acota el daño hoy, sin llave larga:

- Solo lo puede intentar quien ya tiene el código, o sea alguien a quien ya invitaron.
- Lo que consigue es que la víctima entre a SU red en vez de a la del host. No lee la sala real, no obtiene su identidad de red y no expulsa a nadie.
- **Renovar el código lo desarma**, porque el vestíbulo deriva del invite ID: con un ID nuevo hay un vestíbulo nuevo y el ocupante se queda solo en el viejo.

**Disparador para implementar esta decisión:** que Kanpachi deje de ser privado, o el primer caso real de un código compartido fuera del grupo. Lo que la vuelve necesaria no es la criptografía, es que el círculo de quien tiene un código deje de ser gente conocida.

**Consecuencia en el copy.** La página no dice "Humberto te invitó a su sala", que es una afirmación de identidad que ninguna versión de esto respalda. Dice quién **se identifica** como el host. Hay un test que falla si alguien devuelve la frase original. Ver `05-ui.md`.

### Costos aceptados

- Estado nuevo en el cliente: la llave propia y la libreta de huellas conocidas. Va en `ProgramData`, ver `03-arquitectura.md`.
- Reinstalar Windows genera una llave nueva, así que el host pierde sus invite IDs fijados y sus amigos ven el aviso de huella cambiada. Se mitiga con el TTL de semanas del registro, y renovar el código ya es una función que existe por decisión 22.
- Sin la libreta, la firma sola no prueba el nombre. La continuidad es lo que da la garantía, la firma es el mecanismo.

La UI muestra la lista completa a todos, y eso es deliberado: ocultarla en pantalla no la ocultaría en la red, y aparentar una privacidad que no existe es peor que no tenerla.

## 26. Ninguna capa depende de que la anterior haya funcionado

**El problema:** casi todo lo que Kanpachi tiene que detectar es una ausencia. Que el host se fue, que el túnel se cayó, que a alguien lo echaron. Una ausencia no llega por un mensaje, y **cualquier mecanismo que la detecte puede no dispararse nunca**: un socket que no se entera, un adaptador roto, una goroutine muerta, una suscripción de Windows que caducó en silencio.

**El malentendido que evita:** que "hay un contador de 20 minutos" significa que la sala se cierra a los 20 minutos. No significa nada de eso si nadie mueve el contador. Durante todo el desarrollo de `core` ese contador existió, estuvo probado, y era **inalcanzable en tiempo de ejecución**, porque no había supervisor que lo llamara.

**Elección:** cada detección tiene al menos un respaldo que no comparte causa de fallo con ella, y ningún respaldo necesita que el de arriba haya corrido.

| # | Señal | Latencia | Cooperativa | Qué falla que las de abajo cubren |
|---|---|---|---|---|
| 0 | El host avisa: expulsión o cierre de sala | instantánea | **sí** | El aviso puede no salir, no llegar o no procesarse |
| 1 | La conexión de control se cae | segundos | no | Un socket medio abierto no se entera de una máquina apagada de golpe |
| 2 | El host no está en la tabla de miembros del motor | segundos | no | El motor sigue reportando a un host con la app colgada |
| 3 | Silencio del host, 6 minutos | 6 min | no | Necesita que el canal de control exista |
| 4 | Contador de ausencia, 20 minutos | 20 min | no | Necesita que algo llame al latido |
| 5 | El motor avisa que murió | segundos | no | El motor puede morir sin decirlo |
| 6 | El watchdog del supervisor se rinde | 3 min 18 s | no | El supervisor puede estar colgado |
| 7 | Sin túnel, 10 minutos | 10 min | no | Necesita que algo llame al latido |

### Las tres reglas que hacen que esto funcione de verdad

**1. Los plazos se evalúan desde varias puertas, no solo desde el latido.** Cada entrada del daemon que OBSERVA algo comprueba los vencimientos antes de hacer su trabajo: un cambio de miembros, un anuncio del host, un evento del motor, el barrido de exposición. Si la goroutine del latido se muere, los contadores siguen venciendo con lo siguiente que entre.

Las entradas que expresan una INTENCIÓN del usuario no lo hacen, y esa asimetría es deliberada. Expulsar, elegir juego, renovar el código y renombrar tienen que fallar con un error preciso, y que "expulsar" se convierta en silencio en "saliste de la sala" es peor que un contador que vence un latido más tarde.

**2. Un fallo en una capa no puede llevarse a la siguiente.** Las dos capas de la expulsión corren en el mismo acto y ninguna aborta a la otra. Un pánico atendiendo un evento cuesta ese evento y no el bucle. Un canal de entrada cerrado se registra y el latido sigue.

**3. Nada de esto se configura desde fuera.** Los plazos son constantes de compilación, ninguna operación de la API local los toca, y la cadencia del supervisor tampoco se configura. Hay un test de arquitectura que falla si alguno deja de ser `const`, si aparece un método exportado que huela a apagarlo, o si un plazo se convierte en un campo del estado de la sala, que es como entraría desde un archivo de disco.

### El límite honesto

**Si el daemon entero se cuelga, no hay capa de software que lo cubra desde dentro.** El proceso hijo del motor seguiría conectado y las reglas aplicadas. La mitigación no es otra capa de lógica, es del sistema operativo: el motor arranca dentro de un Job Object de Windows con `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`, así el hijo muere con el padre pase lo que pase. Va con el adaptador del motor.

### Descartado: reintentar el aviso hasta que lo confirmen

Se evaluó al diseñar el aviso de expulsión. Reintentar sin tope convierte una operación no cooperativa en una que espera al otro lado, o sea le devuelve al expulsado el poder de decidir cuándo se va. Lo que se hace es esperar el acuse **con tope**: pasado el plazo se revoca igual, y lo único que se pierde es que se entere.

## 27. La contención vive en DOS capas, y la segunda es un filtro propio

**Decidido, y las cuatro apuestas de las que dependía están MEDIDAS** con dos máquinas reales los días 3 y 4 de agosto de 2026.

### El problema que la decisión 4 dejó abierto

La decisión 4 explica por qué la cuarentena de base **no puede ser un deny-all**: los bloqueos explícitos del Firewall de Windows ganan sobre cualquier permiso en conflicto, sin desempate por especificidad y sin orden asignable por el administrador. Un bloqueo total taparía también las reglas del juego activo que crea el propio daemon.

La consecuencia es que **la lista de lo que Kanpachi abre es ADITIVA y no completa**. Sirve para decir "esto se abrió", y no puede decir "y nada más". Mientras eso sea así, una regla permisiva ajena, la que deja el instalador de Parsec o de Sunshine, alcanza al usuario por la red virtual y expulsar a alguien no lo tapa. Es la decisión 19 en su peor forma, y es el único camino conocido por el que un miembro de la sala consigue teclado, pantalla y sistema de archivos del host.

### Lo que se hace

**Intersección, no reemplazo.** Los permisos se quedan donde están y se suma una segunda capa.

| Capa | Quién la pone | Qué hace |
|---|---|---|
| Instalador, grupo `Kanpachi-base` | COM | Los puertos prohibidos en las dos direcciones, sin acotar. **No cambia** |
| Daemon, grupo `Kanpachi` | COM | Los permisos del juego activo. Son los que ABREN. **No cambia** |
| Daemon, compuerta | Sesión propia de WFP | Un bloqueo de todo lo entrante acotado al adaptador virtual, más permisos espejo del mismo conjunto |

La compuerta va **solo** en `ALE_AUTH_RECV_ACCEPT_V4` y `V6`. Jamás en `ALE_AUTH_CONNECT`: bloquear la salida impediría que un invitado marque al puerto del juego del host, que es el caso central del producto.

### Por qué funciona: la asimetría de WFP

En WFP, un filtro **Block es HARD por defecto**, o sea *"cannot be permitted at another sub-layer"*, y un filtro **Permit es SOFT**, o sea *"can be blocked at another sub-layer"*.

La regla permisiva de Parsec vive en el sublayer de Windows y es un permiso soft. Un bloqueo duro nuestro sobre el adaptador virtual la anula, **sin tocarla y sin pedirle nada al usuario**.

### Lo que se midió, y cómo

Todo con dos PCs en la misma red, el invitado conectando contra el host.

| Qué | Resultado |
|---|---|
| Bloqueo duro nuestro contra permiso vivo del Firewall | permiso solo: conecta 6 ms. Permiso más bloqueo: **timeout**. Bloqueo quitado: conecta otra vez 20 ms |
| La condición de interfaz se aplica | acotado al adaptador por el que llega el invitado: **no conecta**. Acotado a OTRO adaptador: **conecta** |
| El bloqueo de todo con permiso espejo | el puerto exceptuado **conecta**, cualquier otro **no**, con el permiso de Windows vivo |
| El veto del usuario | un bloqueo suyo sobre el puerto exceptuado lo tira, con nuestro permiso espejo intacto |

**Cada una va y VUELVE.** Un solo "no conecta" no distingue un filtro de un cable suelto, del wifi o del listener caído. Los pasos de vuelta descartan las tres cosas, y la comparación con y sin alcance cambia una sola variable.

### Lo que NO se rompe, y es el punto

- El grupo `Kanpachi` **sigue existiendo y siendo visible**. `wf.msc`, `Get-NetFirewallRule` y toda la base de conocimiento del usuario siguen sirviendo.
- **El usuario conserva el veto**, medido: sin hard permits y sin el sublayer de peso máximo, sus bloqueos siguen ganando. La decisión 4 se sostiene tal cual.
- **No es todo o nada.** El bloqueo de todo entra solo y vale por sí mismo: con eso y nada más, el adaptador virtual queda inalcanzable salvo por lo que los permisos abran.
- **Reiniciar el servicio no corta la partida.** Los flujos se reautorizan y los permisos del Firewall siguen vivos.
- Si el filtro deja de casar, **degrada a la contención de antes**, que es la del Firewall de Windows. Nunca por debajo.

### Lo que se descartó

**Mudar los permisos a WFP y vaciar el grupo COM.** Se pierde la visibilidad, que es lo que permite al usuario auditar con sus herramientas y a nosotros diagnosticar por teléfono. Y no compra nada: la compuerta ya cierra el caso.

**Hard permits.** Cerrarían el agujero igual y le quitarían al usuario el veto sobre su propia máquina. Es exactamente lo que la decisión 4 protege.

**El sublayer de peso máximo.** Mismo problema por otra vía.

**Cuarentena WFP persistente puesta por el instalador.** El servicio base de filtrado la deshabilita en silencio si el proveedor no arranca como servicio automático, y una protección que se apaga sin avisar es peor que no tenerla.

### Lo que sigue sin medirse

**La reautorización de un flujo YA establecido.** Todo lo medido son conexiones nuevas. Si la condición de interfaz llegara vacía al reautorizar, tras reiniciar el servicio o al cambiar de adaptador, la compuerta dejaría de casar en silencio. La mitigación prevista es emitir el bloqueo **dos veces**, por LUID del adaptador y por dirección local sobre el rango de la sala, para que ninguna de las dos sea el único asidero.

### Qué emite la compuerta, y por qué cada pieza

**Dos bloqueos de todo en IPv4, no uno.** Uno acotado por el LUID del adaptador y otro por el rango de la sala. El LUID es lo preciso y el rango es el respaldo, y existe por el único riesgo que las mediciones no cubrieron: si la condición de interfaz llegara vacía al reautorizar un flujo ya establecido, un bloqueo solo dejaría de casar en silencio. Con los dos, ninguno es el único asidero. El rango no puede pisar la red de casa porque la `/24` de la sala se elige en tiempo de ejecución contra las redes que la máquina ya tiene, que es la decisión 10.

**IPv6 queda bloqueado en el adaptador, y SIN permisos espejo.** Kanpachi direcciona en IPv4 dentro de `100.64.0.0/10`, así que cualquier cosa que llegue por IPv6 a ese adaptador no es de Kanpachi. Dejarla pasar sería un agujero con la puerta de al lado cerrada, y es de los que nadie mira porque la pantalla habla de puertos y no de familias de direcciones.

**Un permiso espejo por regla, con MÁS peso que el bloqueo.** Si un permiso no le gana al bloqueo dentro del sublayer propio, el puerto del juego no se abre y la sala no sirve. Los pesos concretos están medidos funcionando.

**Los rangos de puertos viajan como rango, no expandidos.** El catálogo no pone tope a la amplitud, así que un perfil puede pedir `27000-27100` legítimamente: expandir eso a cien filtros sería absurdo y rechazarlo rompería perfiles que el dominio acepta. WFP tiene condición de rango.

**Ningún filtro sale sin alcance, y se comprueba por tres vías.** Es argumento obligatorio del único constructor, se revalida justo antes de llegar a la API de Windows, y un guardián de arquitectura prohíbe construir un filtro a mano fuera del fichero que los define. La redundancia es deliberada: un filtro sin alcance compila, pasa los tests funcionales, pinta verde, y aplica a todos los adaptadores de la máquina. Con un bloqueo duro, eso deja al usuario sin la entrada de su red de casa. Leyendo el diff tampoco se ve, porque la diferencia entre el filtro correcto y el catastrófico es un campo que NO está.

**Un alcance RELLENO no es un alcance que acote.** `0.0.0.0/0` es un prefijo perfectamente válido y casa con toda dirección local de la máquina: el mismo desastre de arriba, pasando por delante del guardián porque el campo está puesto y el tipo es correcto. Así que el rango de la sala se exige del tamaño de una sala y dentro de los espacios donde las salas viven. Un `/16` del espacio compartido tampoco vale: bloquearía 255 salas ajenas de las que esta máquina no sabe nada.

### Las dos trampas de cómo WFP combina condiciones

**Condiciones del MISMO campo se unen con O; las de campos distintos, con Y.** De ahí sale casi todo lo demás.

Es lo que se quiere para los miembros: tres direcciones remotas son tres condiciones y significan "cualquiera de estas". No existe una condición con varios valores dentro, y creer que sí produciría un permiso que abre solo para el primer miembro.

Y es una trampa para el alcance local: un filtro que llevara a la vez el rango de la sala y la dirección del host no acota dos veces, **ensancha**. En un permiso eso abre el puerto a toda la sala en vez de a la IP del host, y el filtro se lee perfectamente razonable. Está prohibido llevar las dos.

**Una condición de dirección IPv4 en la capa IPv6 no casa con nada, y no falla.** El bloqueo de IPv6 quedaría puesto sin bloquear, y la medición lo contaría como presente. También está prohibido.

### La clave de un filtro sale de su RANURA, no de su nombre

Es lo que permite limpiar sin recordar nada entre arranques, que es de lo que depende que una muerte sucia del daemon no deje nada abierto.

Derivando la clave del nombre, el permiso espejo lleva dentro el nombre de la regla, o sea el juego. **Cambiar de juego cambia las claves y deja huérfanos los filtros del juego anterior**: un puerto abierto que ya nadie pidió, invisible, porque un filtro de WFP no sale ni en `wf.msc` ni en `Get-NetFirewallRule`.

Con la clave por ranura el conjunto de claves posibles es fijo y conocido, así que barrer las ranuras borra todo lo que la compuerta pueda haber puesto alguna vez, sin enumerar nada. Hay un tope de ranuras, y emitir de más se rechaza en vez de recortarse: un conjunto aplicado a medias deja al usuario creyendo que la sala está configurada mientras un jugador no puede entrar.

El nombre sigue siendo descriptivo y viaja al nombre visible del filtro, que es lo que se lee en `netsh wfp show filters`. Uno identifica y el otro explica.

### La sesión y la transacción

**La sesión NO es dinámica y NO es persistente**, y las dos mitades son por motivos distintos.

No dinámica, porque los filtros de una sesión dinámica se borran cuando el proceso muere. Eso quitaría justo lo que hace falta: que una muerte sucia del daemon deje la sala **contenida** en vez de abierta, y que la limpieza al arrancar sea una operación de verdad. Es la misma doctrina que la purga del grupo propio en las reglas del firewall.

No persistente, porque así reiniciar la máquina se lo lleva todo. Es la red de seguridad final: si el cierre falla y la limpieza falla, un reinicio deja la máquina como estaba.

**Aplicar reescribe el conjunto entero dentro de una transacción.** Un filtro de WFP no se puede editar en sitio, así que cambiar algo es borrar y volver a poner, y entre las dos cosas hay una ventana donde el bloqueo no está. Esa ventana está en el cable. Con transacción no existe: se publica el conjunto entero o no se publica nada. De paso, reaplicar el mismo conjunto REPARA lo que alguien haya borrado por fuera, igual que en las reglas del firewall.

### La compuerta cubre los DOS adaptadores, y sin ella no se abre nada

Las dos mitades de esto se decidieron al CABLEARLA, que fue cuando se descubrió que **no la encendía nadie**: el método de alcance existía, estaba probado, y solo lo llamaba la herramienta de medición. En el daemon real la contención del adaptador virtual eran únicamente las reglas del Firewall de Windows.

**Los dos adaptadores.** El host vive en dos redes a la vez, la sala y el vestíbulo, y el vestíbulo es donde llega gente que **todavía no es miembro**: es el que menos puede quedarse sin compuerta. Su rango es constante y no viaja como campo, porque un campo por el que pasarlo es un campo por el que ensancharlo, y cuál de los dos cubre cada permiso se lee de la dirección local de la regla en vez de una bandera. Las ranuras del vestíbulo quedan reservadas incluso sin vestíbulo: corriendo los permisos hacia arriba, uno ocuparía la ranura de un bloqueo y la limpieza siguiente lo borraría creyendo que barre un bloqueo que ya no aplica, o sea un puerto que se cierra solo sin nada que lo explique.

**Y el invitado acota igual**, que no es un extra: el conjunto de reglas le abre sus puertos de cliente, así que también escribe permisos y también necesita quién los acote frente a los demás miembros de la sala.

**Aplicar falla en la cara si hay reglas y no hay dónde acotarlas.** Antes dejaba un aviso en el log y escribía los permisos igual, o sea que la lista de permitidos volvía a ser ADITIVA justo cuando había puertos que abrir. El conjunto vacío sigue pasando y no es una excepción: sin nada que abrir no hay nada que acotar, y ese es el estado normal del daemon en reposo, además de lo que garantiza que la interfaz virtual nazca sin nada abierto. Los casos de uso lo tratan como fatal, a diferencia de los ajustes del adaptador: un MTU mal puesto degrada la partida, y una sala sin compuerta miente sobre lo único que este producto promete.

**Quién la enciende: el caso de uso**, porque quien sabe cuándo existe el adaptador es quien levantó la red. Lo crea el motor, así que no existe cuando arranca el daemon. El nombre del adaptador no viaja desde el núcleo: elegir a qué adaptador se acota un bloqueo duro es la decisión que separa contener la sala de dejar al usuario sin su red de casa.

### Consecuencia para el dominio

`OwnRulesIntact(bool)` no vale para esto: un booleano no puede decir QUÉ falta ni en qué capa. Lo reemplaza `Enforcement()`, que devuelve lo MEDIDO en las dos capas y deja que juzgue `Enforcement.Diff`, que es dominio y se testea sin Windows. El adaptador mide y el dominio juzga, por la misma razón por la que `Apply` calcula la diferencia contra las reglas vivas y jamás contra un recuerdo en memoria.

Un estado de compuerta **sin comprobar** es distinto de **ausente**, y se muestran distinto: uno es un hecho y el otro es ceguera. Misma doctrina que `AlertAuditFailed`.

---

## 28. La Protección Kanpachi se comprueba a sí misma, desde otra máquina

La decisión 27 puso la compuerta. Esta contesta la pregunta que aquella dejó
abierta: **cómo se sabe que la compuerta está conteniendo de verdad.**

### El problema, medido y no supuesto

Todo lo que Kanpachi enseña hoy lee lo que ESTA máquina tiene configurado,
preguntándole a la misma Windows que aplica las reglas. Es honesto y no puede
contestar la pregunta del producto, que es si desde otra PC se llega. El riesgo
que la propia decisión 27 nombra, que la condición de interfaz llegue vacía al
reautorizar un flujo y el bloqueo deje de casar **en silencio**, se ve verde
desde dentro por definición.

Y sondear desde fuera tampoco alcanza tal cual. Medido el 2026-08-04, con el
droplet marcando a la máquina de desarrollo por Tailscale, mismo puerto y
firewall encendido:

| Estado del puerto | Resultado |
|---|---|
| sin regla, sin oyente | silencio |
| **con regla de permiso, sin oyente** | **silencio** |
| con regla de permiso, con oyente | conecta |

La fila del medio manda: **Windows no devuelve RST hacia dentro**, ni siquiera con
el firewall permitiendo el puerto, que es su modo sigiloso. Así que un puerto callado no
distingue "lo bloqueó el firewall" de "no hay nada escuchando", y una compuerta
muerta con el juego cerrado se lee igual que una compuerta sana.

### La decisión

El host abre **un oyente que existe para ser bloqueado**, en un puerto al azar
ligado solo a su dirección de la sala, y le pide a los miembros que lo marquen.
Como se sabe con certeza que hay alguien detrás de esa puerta, el silencio pasa a
tener una sola lectura.

**Un puerto prueba todos.** La compuerta no es una regla por puerto: es un
bloqueo del adaptador entero más los permisos espejo del juego. Un puerto que
nadie pidió y que queda callado demuestra que ese bloqueo está vivo, y ese
bloqueo es el mismo para todos los puertos que nadie pidió, incluidos los que no
conocemos. Por eso no hay lista negra de puertos peligrosos: enumerar amenazas es
una lotería, y Parsec y compañía escuchan donde el usuario les diga.

### Alternativas descartadas

**Una lista de puertos conocidos de escritorio remoto.** Es lo que se construyó
primero y se quedó corto: encuentra a quien dejó la configuración de fábrica y no
a quien la cambió, y por UDP no puede preguntar nada. Comprobar el bloqueo
general las cubre a todas. La lista sobrevive únicamente como información
secundaria.

**Usar un puerto que YA tenga algo escuchando** en vez de abrir uno. Sale más
barato, no abre ningún socket nuevo, y se descartó: si la compuerta está rota, la
comprobación termina con un miembro **conectado de verdad a un servicio real** del
host. Un canario que no lleva a ninguna parte es preferible a usar uno que sí
sirve para algo.

**Preguntarle a UN miembro al azar.** Tumbado por una revisión adversaria del
diseño: ese miembro sería a la vez el único que marca y el que informa, así que
callándose deja al host sin nada que arbitrar y el veredicto sale limpio. Se le
pregunta a **todos**, y entonces para esconder una fuga tendrían que negarse a
marcar todos a la vez. La suposición baja de "el que me tocó preguntar es
honesto" a "al menos uno de N lo es", que importa porque el código no es secreto,
no hay baneo y volver a entrar es gratis.

**Que el seed compruebe desde internet.** Contesta otra pregunta, que es si el
router tiene algo abierto, y es valiosa. Queda fuera de esta decisión por un
problema sin resolver: con CGNAT la IP pública **no es del usuario**, la comparte
con vecinos, y marcarle sería marcarle a la máquina de otro. Ver `07-futuro.md`.

### Zero trust: dos fuentes y solo una es un hecho

`Touched` lo ve el host en su propio socket y no se puede falsificar. El informe
del miembro es un mensaje y se puede mentir. El hecho propio gana siempre, y eso
resuelve dos de los cuatro casos sin confiar en nadie:

| El miembro dice | Canario tocado | Conclusión |
|---|---|---|
| "conecté" | sí | Fuga real |
| "conecté" | **no** | Ese miembro miente, y queda probado |
| "silencio" | sí | Fuga igual: cruzó algo |
| "silencio" | no | **Sin evidencia.** No se puede afirmar más |

**La fuga se afirma con certeza. Su ausencia no.** Un miembro que no mande el
paquete y diga "silencio" produce lo mismo que la compuerta funcionando, y no hay
forma de distinguirlos: un paquete que nunca salió no deja rastro. Por eso el
estado bueno se llama *sin evidencia de fuga* y no "está cerrado", y hay un test
que falla si alguien lo renombra a algo que afirme más.

Un fallo LOCAL del que marca tampoco es un silencio. `ProbeFailed` en los dos
protocolos significa que esa máquina no pudo ni preguntar, y contarlo como
silencio sumaría tranquilidad de una comprobación que no ocurrió.

### El ciclo, y por qué se apaga cuando salta la alarma

Una ronda se cierra con lo primero de tres cosas: que lo toquen, que contesten
todos, o un plazo corto de unos diez segundos. El tope de treinta segundos no es
la espera, es el cierre duro por si muere quien lo abrió.

Con la alarma levantada **se corta la comprobación periódica**, y la razón es de
seguridad antes que de eficiencia: mientras la protección está caída, ese canario
es alcanzable de verdad por la sala, y no se siguen abriendo sockets alcanzables
en una máquina que ya se sabe expuesta. Se conserva el disparo **después de cada
`Apply`**, que es el único momento en que el estado pudo cambiar a mejor, y evita
dejar una alarma rancia encendida cuando la protección se repuso sola.

### Lo que esto le cuesta al modelo de amenazas

Un oyente más en un proceso que corre como SYSTEM, unos segundos por minuto. Se
acepta por tres razones, y las tres tienen que seguir siendo ciertas:

1. **No parsea nada.** Por TCP acepta y cierra sin leer un byte; por UDP lee un
   largo fijo y devuelve el eco solo si el número coincide.
2. **Se liga solo a la dirección de la sala**, jamás a `0.0.0.0`, y el adaptador
   rechaza de plano una dirección sin especificar. No abre nada en la red de casa
   del usuario en ningún caso.
3. **Su radio de explosión es exactamente lo que mide.** Con la compuerta viva es
   inalcanzable para todo el mundo; con la compuerta muerta lo alcanza la sala,
   que es lo que se quería averiguar.

Es muchísima menos superficie que el canal de la sala de la decisión 23, que sí
parsea mensajes de miembros corriendo como SYSTEM.

### El mensaje nuevo, y el campo que no existe

El pedido viaja por el canal de la sala con un puerto y un número, y **sin campo
de dirección**. El miembro marca a la dirección de la conexión que ya tiene
abierta contra el host. Con un campo de destino, este mensaje convertiría el
canal en un **escáner de puertos por encargo** contra terceros, con el tráfico
saliendo de las casas de los miembros. Lo que lo impide no es una comprobación que
alguien pueda borrar: es que el tipo no lo puede expresar. Igual al revés, el
informe no lleva remitente, así que nadie puede informar por otro.

---

## 29. Un régimen único para toda mutación, con un hard reset que lo deshace

**Alternativas:** confiar en la limpieza del arranque, que ya es incondicional. Un desinstalador que lo sepa todo. Un comando de reparación que reinstale. Un régimen único con dos banderas.

**Elección:** **el régimen único.** Toda mutación persistente de Kanpachi o lleva etiqueta enumerable desde el sistema, o queda anotada en un libro con su valor previo. Etiqueta es el grupo de firewall y la ranura de WFP; libro es `applied-tweaks.json` y `suspended-rules.json`. Lo que no cumple una de las dos no se escribe.

Lo efímero queda fuera del régimen y no necesita nada: el adaptador virtual, su dirección, su métrica, su MTU, sus rutas y los filtros de la compuerta mueren con la red virtual, que el motor crea por sala. Eso está medido: **sin sala abierta no existe ningún adaptador.**

### Por qué hace falta un reset, si la limpieza del arranque ya es incondicional

Porque la limpieza del arranque necesita un arranque. `NewSession` repone la cuarentena, purga el grupo propio y restaura las reglas ajenas en **cada** arranque, sin depender de ninguna señal, así que la muerte sucia está cubierta. Lo que no cubre es el daemon que **no vuelve a arrancar**: una configuración corrupta, un adaptador que ya no monta, un motor huérfano peleándose por la red virtual. Ahí hace falta limpiar sin pasar por el camino que está roto, y por eso `--reset` no monta la sesión: exigir que funcione justo lo que el usuario dice que no funciona no es una reparación.

### La asimetría entre resetear y desinstalar

**`--reset` REPONE la cuarentena de base y no la quita.** Lo que hace valiosa a la cuarentena es seguir puesta con el daemon detenido, deshabilitado o a medio desinstalar. Un reset se pide justo cuando la configuración está corrupta y nada arranca: quitarla ahí destruiría exactamente lo que protege del caso que motivó el reset.

Y **conserva `last-room.json`**. Resetear la configuración no tiene por qué borrar a qué sala volver: son dos cosas distintas, y juntarlas convierte una limpieza en una pérdida.

Quitar la cuarentena es de `--uninstall-cleanup`, y esa capacidad vive en **una sola función** con el nombre largo a propósito, fuera de todo puerto del núcleo. Está cerrada por tres vías: la interfaz que el daemon usa no declara nada que pueda quitarla; un guardián exige que sea la única función del daemon que a la vez nombre el grupo base y llame a algo que borra; y otro exige que solo la llame el cableado del binario. El primero de esos guardianes se escribió porque el que ya existía **no mordía**, y eso se comprobó escribiendo la función y viéndolo callar.

### Ningún paso corta la secuencia

No hay un segundo intento. Quien pide un reset lo pide porque nada más funciona, así que abortar en el primer paso que falla dejaría el resto puesto justo entonces. Se registran todos los fallos y se devuelven juntos.

El orden sí decide, y cada par tiene una dirección correcta: los motores huérfanos primero, porque mientras uno siga vivo la red virtual sigue arriba y purgar antes dejaría un adaptador con tráfico y sin nada conteniéndolo; la cuarentena antes de la purga, porque la purga es el instante de menos protección; la compuerta se suelta después de los permisos, igual que al salir de la sala.

---

## 30. Una carpeta portable, decidida por un fichero y no por una bandera

**Alternativas:** solo el instalador. Un segundo ejecutable "portable". Una bandera de línea de comandos que active el modo. Un fichero marcador junto al binario.

**Elección:** **el fichero marcador**, `kanpachi.portable`. Con él presente, el daemon guarda sus datos en `kanpachi-data\` al lado del ejecutable y corre en su propio proceso; sin él, todo sigue exactamente como estaba.

Solo el instalador no alcanzaba para el caso que motivó esto, que es mandarle Kanpachi a alguien en un ZIP para probar. Un segundo ejecutable repite el problema que ya resolvió el patrón de un binario con parámetros: dos ficheros que hay que mantener sincronizados y que se rompen sin dar error.

**La bandera es la que estuvo cerca, y falla por un motivo mecánico.** La pregunta "esto es portable" la tienen que contestar igual tres procesos que no comparten línea de comandos: el lanzador, que arranca de un doble clic sin argumentos; el daemon, que nace después; y la interfaz de Flutter, que es otro ejecutable. Con una bandera habría que acordarse de pasarla en los tres, y olvidarla en uno es silencioso: el daemon escribiría su token junto al binario y la interfaz lo buscaría en ProgramData, con el síntoma de "no hay servicio" delante de un servicio corriendo. Ese fallo exacto ya ocurrió cuatro veces en este repositorio con el nombre del pipe. Con un fichero, ser portable es una propiedad de la CARPETA y los tres la deducen sin hablar entre ellos.

### Lo que cuesta, y por qué se acepta

| | Instalado | Portable |
|---|---|---|
| UAC | uno, al instalar | uno por arranque |
| Datos | ProgramData con ACL del instalador | junto al binario, con los permisos de donde esté la carpeta |
| Arranque con Windows | sí | no |
| Pipe | `kanpachi-installed` | `kanpachi-portable` |
| Instancia de UI | `Kanpachi-UI-installed-*` | `Kanpachi-UI-portable-*` |

El UAC por arranque es consecuencia directa de no haber instalado nada: el permiso de arrancar el servicio se lo concede el instalador al usuario con `sc sdset`, y una carpeta copiada no concedió nada. Se acepta porque el portable no reemplaza al instalador, lo acompaña: es para probar y para repartir, no para el amigo que va a jugar todas las semanas.

**La separación de canal y ventana salió de un fallo medido en `v0.1.1`.** Había una UI portable viva y se instaló Kanpachi. Los dos usaban el mismo pipe y el mismo evento `Local\Kanpachi-UI-instancia-unica`: la UI instalada nacía, encontraba a la portable y se cerraba; la portable se traía al frente, leía SU token y lo mandaba al daemon instalado, que lo rechazaba correctamente. El watchdog vio cuatro muertes rápidas y apagó el servicio. La solución no es matar el portable desde el instalador —eso mezclaría los productos— sino que cada uno tenga canal, token e instancia propios.

### El daemon portable no es SYSTEM, y eso rompió lo que nadie esperaba

Salió corriéndolo, no leyéndolo, y es lo más caro que tuvo esta decisión. El daemon instalado es un servicio como SYSTEM en la sesión 0, y lanza la interfaz cruzando a la sesión de quien usa la máquina con `WTSQueryUserToken`. **Esa llamada exige `SE_TCB_NAME`, que solo tiene LocalSystem.** El daemon portable es el usuario elevado, ya dentro de su sesión, así que falla, y falla de la peor forma: arranca, escucha en su pipe, y se queda corriendo sin nada en pantalla, que es la forma exacta que la invariante de la bandeja prohíbe.

La vía obvia tampoco sirve, y también hizo falta medirlo: pedir el token enlazado del propio proceso elevado devuelve un token de suplantación de nivel identificación, porque la documentación reserva el primario para quien tenga `SeTcbPrivilege`. Duplicarlo a primario falla con `ERROR_BAD_IMPERSONATION_LEVEL`.

Lo que funciona es tomar prestado el token del Explorador de esta sesión, que es el proceso que ya está corriendo con el token que se quiere: mismo usuario, sin elevar, y primario. Se lanza con `CreateProcessWithTokenW`, que pide `SeImpersonatePrivilege` en vez de `SE_ASSIGNPRIMARYTOKEN`; el primero lo tiene un administrador elevado y el segundo no. Dos detalles más que costaron un intento cada uno: el privilegio hay que ENCENDERLO, tenerlo no basta, y el duplicado hay que pedirlo con sus derechos explícitos, porque heredar "los mismos que el original" lo dejaba sin `TOKEN_ASSIGN_PRIMARY`.

**El resultado se comprobó:** la interfaz de una carpeta portable corre SIN elevar, igual que la instalada, y muere con el daemon por el mismo job.

## 31. El modo seguro va en TODOS los nodos, no solo en el invitado

**Alternativas:** dejarlo solo en el invitado, que es quien lleva credencial. Ponerlo también en el seed. Ponerlo en los cuatro sitios: seed, host, vestíbulo e invitado.

**Elección:** **en los cuatro.** Un invitado abre con credencial y no con el secreto de la red, y el modo seguro es lo que esa credencial necesita para valer.

**Estuvo solo en el invitado, y ninguna sala pasó nunca de una persona.** El fallo se midió el 2026-08-07 con dos máquinas de verdad: el invitado hacía todo bien —el registro confirmaba el código, entraba al vestíbulo, pedía la credencial y la recibía— y después se quedaba treinta segundos esperando un adaptador que jamás aparecía. Lo que llegaba al usuario era esto:

```
entrando a la sala: la red arrancó y el adaptador no quedó utilizable:
el adaptador "kanpachi0" no tomó la dirección 10.99.244.2 en 30s:
context deadline exceeded (route ip+net: no such network interface)
```

Y lo que pasaba de verdad estaba en el diario del droplet, tres capas más arriba:

```
ERROR easytier::instance::listeners: handle conn error
  error=WaitRespError("unexpected packet type during handshake: 13")
```

La cadena entera, de la causa al síntoma:

1. Una credencial obliga a abrir la conexión con un handshake de Noise: `NoiseHandshakeMsg1`, que es el paquete **13**.
2. `peer_conn.rs` toma esa rama con `is_secure_mode_enabled() && packet_type == NoiseHandshakeMsg1`. El seed arrancaba sin modo seguro, así que caía al `else` final y cerraba la conexión. 236 veces seguidas en la última medición, una por segundo.
3. Sin conexión no hay peers, y **el DHCP de EasyTier no reparte dirección mientras la lista de rutas esté vacía** (`instance.rs`: `if routes.is_empty() { continue; }`).
4. Sin dirección no se crea el adaptador. De ahí `no such network interface`, que es lo único que veía el usuario: treinta segundos de espera por una conexión rechazada en el primer paquete.

**Por qué el host y el vestíbulo también, y no solo el seed.** El seed es el que desbloquea entrar; el host es el que decide si se juega por P2P o por relay. Un invitado siempre abre con Noise, así que un host sin modo seguro le rechaza la conexión DIRECTA y la sala entera termina relevando por el droplet cada paquete del juego, que es exactamente lo que este producto existe para no hacer. Los tests de credenciales de EasyTier lo encienden en todos los nodos de la topología, el administrador incluido.

**Lo que NO cambia.** El host y el vestíbulo se identifican con el secreto de la red y abren con `PacketType::HandShake`, que conserva su rama de siempre: un seed con modo seguro sigue atendiendo a los clientes que no lo tienen. Por eso el orden del despliegue es seed primero y clientes después, y no al revés.

**Ninguno guarda clave.** Sin clave declarada se genera un par en cada arranque. Lo que se autentica acá es la credencial del invitado, no la identidad del servidor, y una clave que sobreviviera a los reinicios sería un secreto en disco a cambio de nada.

### Por qué esto sobrevivió a toda la batería de pruebas

Porque **nada en el repositorio ejercitaba el camino del invitado**. `medir-motor-punta-a-punta.ps1` y `engineprobe` crean salas, que es la mitad que funciona; el `join` con credencial no lo corría nadie. El primer intento real fue el de una persona instalando el instalador, y es el que lo encontró.

Y costó encontrarlo más de lo debido por una segunda razón: **el motor del cliente no deja rastro**. No instala ningún `subscriber` de `tracing`, y su `stderr` va al del daemon, que como servicio con `-H windowsgui` no existe. Todo lo que EasyTier dijo del fallo se tiró. En el droplet, la misma causa estaba escrita en una línea.
