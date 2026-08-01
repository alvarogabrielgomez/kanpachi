# CLAUDE.md

Instrucciones de proyecto para Kanpachi. Léelas completas antes de tocar código.

## Qué es esto

Kanpachi es una LAN virtual privada para jugar entre amigos en Windows. Pegas un código, eliges el juego, juegas. Crea una red cifrada P2P entre las máquinas de una sala, abre únicamente los puertos del juego elegido y mantiene cerrado todo lo demás. Sin cuentas, sin configuración, sin abrir puertos en el router.

Alcance actual: **privado, solo para el grupo de amigos del autor**. La arquitectura contempla abrirlo al público más adelante, eso vive en `docs/07-futuro.md` y no se implementa antes de tiempo.

## Documentación

Está en `docs/`. Son la fuente de verdad del diseño, no notas sueltas.

| Documento | Qué contiene | Léelo antes de |
|---|---|---|
| `01-que-es-kanpachi.md` | Visión, partes, principios, lo que Kanpachi NO sabe | Cualquier cosa. Es el README |
| `02-decisiones-de-diseno.md` | Cada decisión con alternativas y razón | Proponer cambiar un enfoque |
| `03-arquitectura.md` | Componentes, interfaces, modelo de amenazas | Tocar `core`, `daemon` o el seed |
| `04-flujos-y-configuracion.md` | Flujo del jugador y del host, instalador, droplet | Tocar el instalador o el despliegue |
| `05-ui.md` | Pantallas, estados, textos, página de invitación | Tocar la UI o cualquier copy |
| `06-catalogo.md` | Perfiles de juegos, creador, import y export | Tocar el catálogo o agregar juegos |
| `07-futuro.md` | Qué se difirió, qué lo activaría, qué se descartó | Proponer una función nueva |

**Antes de proponer algo que parezca obvio, busca en `02` y `07`.** Muchas ideas razonables ya se evaluaron y se descartaron con motivo: detección de ejecución de juegos, MSIX y Microsoft Store, habilitar Compartir archivos, exit node, compartir archivos, chat. Si vas a reabrir una, hazlo citando la decisión y el argumento nuevo.

## Mantener los documentos al día

**Un cambio de comportamiento que no está en los docs es un bug.** La actualización va en el mismo commit que el código, nunca "después".

| Si cambias | Actualiza |
|---|---|
| Un enfoque, un tradeoff, algo que descartaste | `02`, con alternativas y razón |
| Una interfaz, un componente, el modelo de amenazas | `03` |
| El instalador, el despliegue del seed, el diagnóstico | `04` |
| Cualquier texto visible o pantalla | `05` |
| El esquema de perfiles o el flujo del catálogo | `06` |
| Algo que decides posponer o no hacer | `07`, con su disparador |
| Un principio o una parte del producto | `01` |

Reglas de escritura:

- Los siete documentos tienen que ser consistentes entre sí. Mismo código de ejemplo, mismos puertos, mismo nombre de adaptador y de servicio.
- Cuando una decisión nueva invalide una vieja, corrige la vieja. No dejes dos versiones conviviendo.
- Cuando algo se descarte, escribe **por qué**, no solo que se descartó. El propósito es que nadie lo reabra sin argumento nuevo.
- Español, sin conjunciones adversativas (`pero`, `sin embargo`, `aunque`, `sino`), sin guiones largos como conectores. Usa comas.

## Invariantes que no se negocian

Están en los docs con su razón. Se listan aquí porque romperlas es el error más caro posible.

**Firewall y exposición**

- La interfaz virtual nace con deny all en ambas direcciones y en los tres perfiles de firewall.
- Solo se abre lo que pide el perfil del juego activo, solo en el host, solo hacia IPs de miembros presentes.
- Puertos prohibidos siempre, sin excepción ni forma de expresarlos en un perfil: 22, 135, 137, 138, 139, 445, 3389, 5985, 5986.
- Jamás habilitar los grupos Detección de redes ni Compartir archivos e impresoras. Se activan por perfil de firewall y abrirían SMB en la LAN de casa del usuario.
- Jamás exit node, subnet routing ni IP forwarding. No existen como opción.
- Jamás una ruta por defecto `0.0.0.0/0` o `::/0` sobre el adaptador. Si aparece, se borra.
- El router del usuario no se toca nunca. Sin port forwarding, sin UPnP. **Ojo: el motor mapea puertos por defecto.** Todo arranque de `easytier-core` lleva `--disable-upnp true`, con un test que falla si alguien lo saca. La única lectura permitida al router es la consulta al IGD del módulo de alertas, que jamás escribe.
- **El cliente nunca escucha en un puerto público.** Arranca con `--no-listener`. Solo el seed escucha.
- **El canal de control solo escucha en el host.** Los invitados marcan hacia afuera y no abren nada. Ese código corre como SYSTEM y parsea entrada de la sala: tope de tamaño, esquema cerrado, solo IPs de miembros presentes.
- Flags del motor que expresan capacidades prohibidas y van siempre apagadas: `--enable-exit-node`, `--exit-nodes`, `--proxy-networks`, `--vpn-portal`, `--socks5`, `--accept-dns`. El portal RPC va fijado a `127.0.0.1`.

**Privilegios y canales**

- Nada que llegue de fuera de la app surte efecto sin confirmación dentro de la app. Siempre, sin estado recordado que permita saltarla.
- El manejador `kanpachi://` es entrada hostil: solo el formato exacto del código, tope de longitud, nada de rutas ni argumentos.
- La API local acepta únicamente perfiles del catálogo. No existe la operación "abrir puerto arbitrario".
- El daemon no observa procesos fuera del creador de perfiles. No detecta que abriste un juego.

**Datos**

- Cero telemetría. Los logs son locales y el diagnóstico se copia al portapapeles.
- **El seed nunca recibe el secreto de la red real de una sala.** Lo genera el host, aleatorio, y no deriva de ningún string que alguien pueda escribir. El seed sí conoce invite IDs y la identidad de encuentro que deriva de ellos: es un vestíbulo público y desechable por diseño, decisión 2. Confundir los tres identificadores lleva a conclusiones de seguridad falsas en las dos direcciones.
- El registro del seed guarda tarjetas de sala **cifradas con una clave que viaja en el fragmento**. Si algún cambio hace que el servidor pueda leer nombres de sala o nicks, es una decisión de producto que se escribe en la 17, no un detalle de implementación.
- El instalador jamás agrega exclusiones de antivirus.
- Los perfiles del catálogo describen lo que el juego necesita. El código decide qué es aceptable conceder. Un perfil corrupto, como máximo, impide que ese juego conecte.

## Estructura y convenciones

El proyecto usa **Clean Architecture**, aplicada como regla de dependencia con puertos. No como anillos de carpetas con DTOs mapeando entre capas: ese mapeo es ceremonia sin retorno a esta escala.

**La regla:** las dependencias apuntan hacia adentro. `domain` no conoce a nadie. Los **puertos se declaran en `core` y se implementan fuera**. El dominio no sabe que existe Windows.

**La métrica que dice si está bien:** los tests de `core` corren sin admin, sin red y sin Windows. Si eso deja de ser cierto, la arquitectura se rompió.

| Anillo | Qué vive ahí |
|---|---|
| Dominio | Tipos y reglas puras: `Code`, `Room`, `GameProfile`, `RuleSet`, `Peer`, invariantes del catálogo, derivación del código |
| Casos de uso | Uno por intención: `CreateRoom`, `JoinRoom`, `ActivateProfile`, `LeaveRoom`, `ImportCatalog` |
| Puertos | `EnginePort`, `FirewallPort`, `NetConfigPort`, `CatalogStore`, `GameLibrary`, `SocketInspector`, `RoutingTable` |
| Adaptadores | EasyTier, COM del Firewall, `iphlpapi`, registro, Steam, JSON en disco |
| Entrada | Named pipe, manejador `kanpachi://`, arranque del servicio |

```
core/       domain/ port/ usecase/         sin I/O, sin syscalls, sin API de Windows
daemon/     adapter/ transport/ service/   Go, servicio de Windows, elevado
ui/         Flutter desktop, sin privilegios
seed/       El compose del droplet: easytier-core + kanpachi-registry
registry/   El binario Go del seed y su Dockerfile. Resuelve invite IDs,
            guarda tarjetas que no puede leer, cuenta miembros, sirve la página
invite/     index.html. Un solo archivo: el registry lo sirve con el estado
            incrustado, y abierto desde el disco funciona igual pidiéndolo
docs/       Los siete documentos
```

Qué respetar al escribir código:

- **Un import prohibido en `core` es un error de arquitectura, no un detalle.** Nada de `os`, `syscall`, `golang.org/x/sys`, `net/http`, `os/exec` dentro de `core/`.
- **Falta el test que verifica esto.** Todavía no hay módulo Go ni CI. El primer commit que cree el módulo debe traer el test de pureza de imports y el workflow que lo corre. Hasta entonces la regla se sostiene a mano, y eso es deuda, no estado deseado.
- Si algo necesita privilegios o API de Windows, va en un adaptador detrás de un puerto declarado en `core`.
- **El cableado de dependencias vive solo en `service/`.** Es el único sitio que conoce a la vez el dominio y los adaptadores concretos. Ningún caso de uso construye su propio adaptador.
- El motor vive detrás de `EnginePort`. Nada fuera de `daemon/adapter/engine/easytier/` menciona EasyTier. **Se ejecuta como proceso hijo (`easytier-core`), nunca vinculado al binario Go:** EasyTier es LGPL-3.0 y es Rust, el proceso separado mantiene la licencia de Kanpachi libre y evita cgo.
- `FirewallPort.Apply` es declarativo: recibe el estado deseado y calcula la diferencia. No hay "agregar regla" imperativo suelto.
- Nada de DTOs entre capas, repositorios genéricos, structs anémicos ni interfaces decorativas. `Clock` sí, porque hay backoff que testear. `StringFormatter` no.

Otras convenciones:

- **Cliente solo Windows.** El seed es Linux por ser servidor, eso no abre la puerta a un cliente Linux.
- **El usuario nunca abre una terminal.** Si una función necesita que alguien corra un comando, está mal diseñada.
- Modo desarrollo: el daemon corre como aplicación de consola con `--console`, sin reinstalar el servicio.
- Todas las reglas de firewall llevan el grupo `Kanpachi`. Al arrancar: purgar lo etiquetado, luego aplicar el estado deseado.
- No compilar con `-ldflags="-s -w"`. Dispara falsos positivos de Defender sobre binarios Go.

## Cómo trabajar aquí

- Si una tarea toca seguridad, di explícitamente qué invariante aplica y cómo la respetas.
- Si algo del código contradice los docs, es una discrepancia que hay que resolver, no algo que se ignora. Pregunta cuál de los dos está mal.
- Prefiere leer el documento relevante antes de inferir el diseño del código.
