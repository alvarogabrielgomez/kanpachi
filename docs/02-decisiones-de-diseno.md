# Decisiones de diseño

Cada decisión relevante, las alternativas consideradas y la razón de la elección. El formato es deliberado: cuando algo se cuestione en el futuro, aquí está el contexto completo para reabrirlo con criterio.

## 1. Motor de red: EasyTier como subproceso, detrás de un puerto

**Alternativas:** Headscale envuelto con una API propia. Construir desde cero sobre wireguard-go. EasyTier vinculado al binario Go. EasyTier como proceso hijo gestionado.

**Elección:** EasyTier ejecutado como **proceso hijo** (`easytier-core`), gestionado por el daemon y accedido siempre a través de `EnginePort`.

**Por qué EasyTier.** Ya resuelve lo difícil: NAT traversal por UDP, cifrado WireGuard, relay de respaldo, y un relay de broadcast UDP que hace funcionar el descubrimiento LAN de los juegos clásicos. Su modelo de identidad nativo es `--network-name` más `--network-secret`, exactamente lo que produce el código de sala. Headscale exigía envolver su modelo de usuarios y pre-auth keys para simular salas anónimas, trabajo que no aporta al producto. Desde cero eran meses para llegar a un ~70% de conexiones directas, contra el 90%+ que da un coordinador maduro.

**Por qué subproceso y no vinculado.** Tres razones que apuntan al mismo lado:

1. **Licencia.** EasyTier es **LGPL-3.0**, verificado en el repositorio oficial. Las menciones a Apache 2.0 que circulan vienen de forks antiguos y de metadatos rancios en crates.io, no las creas. Con LGPL, vincular estáticamente desde un binario Go obliga a permitir la revinculación, algo incómodo de cumplir con la compilación estática de Go. Ejecutarlo como proceso separado es mera agregación: no hay vinculación, la LGPL no se propaga al código de Kanpachi y su licencia queda libre.
2. **Frontera de lenguaje.** EasyTier es Rust, el daemon es Go. La alternativa de vinculación implica cgo o FFI, con su costo de compilación cruzada, empaquetado y depuración. `easytier-go` existe y usa WebAssembly en vez de cgo, es muy joven y también LGPL-3.0, así que no resuelve el punto 1.
3. **Aislamiento de fallos.** El watchdog del supervisor ya asume que el motor puede morir y reiniciarse. Eso solo tiene sentido con un proceso aparte: un `panic` de Rust dentro del mismo proceso se llevaría el servicio entero.

**Costos aceptados:**

- Dos binarios que firmar en vez de uno, el día que haya firma.
- Ciclo de vida del proceso hijo: arranque, supervisión, apagado limpio, huérfanos si el servicio muere de forma sucia. Se maneja en `adapter/engine/easytier`.
- Comunicación por IPC en vez de llamadas en proceso. Irrelevante en volumen: son órdenes de control y consultas de estado, el tráfico del juego jamás pasa por ahí.
- Dependencia de la calidad y el ritmo de EasyTier. `EnginePort` existe para que ese costo tenga salida: cambiar de motor toca una implementación, no el producto.

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

**El portal RPC del motor es su panel de control.** Va fijado a `127.0.0.1` con `--rpc-portal-whitelist 127.0.0.1`. Si fuera alcanzable desde la red virtual, un miembro de la sala podría manejarle el motor a otro.

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

- Deny all en la interfaz virtual, en ambas direcciones, desde la instalación.
- Se abre únicamente lo que pide el perfil del juego activo, únicamente en el host, únicamente hacia las IPs de miembros presentes en la sala.
- ICMP echo permitido, para que el diagnóstico funcione.
- 445, 3389, 22 y todo lo no listado: cerrado siempre. La API no tiene forma de abrirlos.
- Jamás exit node, jamás subnet routing, jamás IP forwarding. No existen como opción.

**La UI lo hace visible:** "Zomboid, 2 puertos UDP, visibles para 4 personas". La seguridad que no se ve no genera confianza.

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
| Reglas base de Kanpachi ausentes o alteradas | Autochequeo del grupo `Kanpachi` | Que la cuarentena no está aplicada |

**Excepción explícita a "el router no se toca nunca".** La consulta al IGD es de **solo lectura**. Kanpachi jamás crea ni borra un mapeo, jamás pide uno. Leer para detectar una exposición sirve a la invariante, escribir la violaría. La distinción va en el código como dos operaciones separadas, con la de escritura inexistente.

**Tres reglas de manejo:**

1. **Las alertas nunca bloquean.** Informan. Una alerta que impida crear una sala convierte cualquier falso positivo en alguien que se queda sin jugar, y eso contradice el principio de que la detección ordena y jamás filtra.
2. **Asíncrono y aislado.** Corre en su propio ciclo, con su propio timer. Un fallo del módulo no toca la conexión, ni el firewall, ni el estado de la sala.
3. **Cada alerta dice qué pasa, qué significa para el usuario y qué hacer**, en ese orden, igual que el resto de los textos del producto. Ver `05-ui.md`.

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

**Descartado: la expulsión inmediata coordinada por mensaje.** Sería cooperativa, no hay servidor que la imponga y un cliente modificado se queda igual, así que daría sensación de control sin control. El timeout local no necesita que nadie obedezca: es cada máquina decidiendo sobre sí misma.

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
Gabriel  ──┼──►  puerto de control, ÚNICAMENTE en el host
Alvaro   ──┘     alcance = miembros presentes. Los clientes nunca escuchan
```

**Por qué no un canal donde todos escuchan.** Sería un puerto abierto en cada PC, atendido por un daemon que corre como SYSTEM, parseando mensajes de gente semi-confiable. Un agujero deliberado en el deny-all, y de lejos la mayor superficie de ataque del producto: un fallo de parseo ahí es ejecución remota como SYSTEM en la máquina de cada miembro. Con el host como único oyente, **el deny-all de los invitados queda literalmente intacto** y la superficie se concentra en una sola máquina, la del host, que ya acepta más exposición por definición.

**Por qué no el seed.** Le daría poder sobre el contenido de las salas y contradice que solo vea networkIDs opacos e IPs públicas.

**Una conexión, cuatro trabajos.** Cada conexión de control resuelve de una sola vez cosas que si no habría que construir por separado:

| Función | Cómo sale |
|---|---|
| Emitir la credencial al que entra | Es el paso 2 del canje de la decisión 2 |
| Expulsar | El host cierra esa conexión y revoca |
| Presencia del host | Si la conexión cae, el host no está. Si vuelve, volvió |
| Latido | Es la misma conexión, no hace falta un ping aparte |

**La propiedad que hace que esto sea barato:** una conexión TCP caída es información confiable **sin necesidad de confiar en nadie**. No es un mensaje que alguien pueda falsificar, es la ausencia de un socket. Por eso la detección de presencia y el timeout de 20 minutos de la decisión 20 no necesitan firma, autenticación ni credenciales.

### Modelo de amenazas del canal

Superficie nueva, así que se escribe entera:

| Amenaza | Mitigación |
|---|---|
| Miembro manda mensajes malformados al host | El host corre como SYSTEM: parseo estricto, tope de tamaño, sin reflexión, sin deserializar tipos arbitrarios. Es el código que más revisión merece del proyecto |
| Miembro falsifica una expulsión | No puede: expulsar lo ejecuta el host sobre sí mismo, no es un mensaje que alguien pida |
| Miembro falsifica "la sala se cerró" | Lo peor que logra es que a otros se les cierre la app. Molestia, no riesgo, y ya está dentro de la sala |
| Miembro se hace pasar por el host | No puede: los clientes marcan hacia una dirección conocida, no aceptan conexiones entrantes |
| Inundación de conexiones al host | Tope de conexiones por IP virtual, y solo se aceptan IPs de miembros presentes |

**Lo que este canal NO transporta:** nada del juego. El tráfico de la partida va por su propio camino P2P y jamás pasa por acá. Son órdenes de control y estado, en volumen de bytes.

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

**Consecuencia en el copy.** La página no dice "Humberto te invitó a su sala", que es una afirmación de identidad que ninguna versión de esto respalda. Dice quién **se identifica** como el host. Hay un test que falla si alguien devuelve la frase original. Ver `05-ui.md`.

### Costos aceptados

- Estado nuevo en el cliente: la llave propia y la libreta de huellas conocidas. Va en `ProgramData`, ver `03-arquitectura.md`.
- Reinstalar Windows genera una llave nueva, así que el host pierde sus invite IDs fijados y sus amigos ven el aviso de huella cambiada. Se mitiga con el TTL de semanas del registro, y renovar el código ya es una función que existe por decisión 22.
- Sin la libreta, la firma sola no prueba el nombre. La continuidad es lo que da la garantía, la firma es el mecanismo.

La UI muestra la lista completa a todos, y eso es deliberado: ocultarla en pantalla no la ocultaría en la red, y aparentar una privacidad que no existe es peor que no tenerla.
