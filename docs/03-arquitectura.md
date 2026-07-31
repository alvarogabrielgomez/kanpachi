# Arquitectura

## Vista general

```
┌──────────────────────────── PC Windows ────────────────────────────┐
│                                                                    │
│  kanpachi-ui  (Flutter, usuario, sin privilegios)                  │
│      │                                                             │
│      │  named pipe \\.\pipe\kanpachi  +  token                     │
│      ▼                                                             │
│  kanpachi-daemon  (servicio Windows, SYSTEM)                       │
│  ├── api/         superficie mínima: crear, unirse, salir,         │
│  │                estado, juegos, diagnóstico                      │
│  ├── supervisor/  máquina de estados, watchdog, eventos de         │
│  │                energía, de red y de identificación de red       │
│  ├── netfw/       reglas de firewall vía INetFwPolicy2 (COM)       │
│  ├── netcfg/      ajustes del adaptador que Windows revierte:      │
│  │                métrica, categoría, rutas, MTU, prefijos         │
│  └── kanpachi-core                                                 │
│      ├── engine/    interfaz Engine + implementación EasyTier      │
│      ├── identity/  código → networkID + secret                    │
│      ├── catalog/   perfiles, detección de Steam                   │
│      └── policy/    perfil + miembros → reglas declarativas        │
│                                                                    │
│  adaptador Wintun "kanpachi0"  ← creado por el instalador          │
└────────────────────────────────│───────────────────────────────────┘
                                 │
                                 │  internet
                                 ▼
        kanpachi-seed (droplet, Docker)         otros peers
        rendezvous + coordinación de      ◄──── P2P directo WireGuard
        hole punch + relay de último            (o vía relay si el
        recurso                                  NAT no cede)
```

## Arquitectura interna: regla de dependencia

El proyecto sigue Clean Architecture aplicada como **regla de dependencia con puertos**, no como anillos de carpetas con DTOs mapeando entre capas. Para un proyecto de este tamaño el mapeo entre capas es ceremonia sin retorno.

**La métrica que decide si está bien no es la pureza de capas, es esta: los tests corren sin admin, sin red y sin Windows.**

| Anillo | Qué vive aquí | En Kanpachi |
|---|---|---|
| **Dominio** | Tipos y reglas puras | `Code`, `Room`, `GameProfile`, `RuleSet`, `Peer`, invariantes del catálogo, derivación del código, plan de direcciones |
| **Casos de uso** | Orquestación, uno por intención | `CreateRoom`, `JoinRoom`, `ActivateProfile`, `LeaveRoom`, `CreateGameProfile`, `ImportCatalog` |
| **Puertos** | Interfaces que el dominio necesita | `EnginePort`, `FirewallPort`, `NetConfigPort`, `CatalogStore`, `GameLibrary`, `SocketInspector`, `RoutingTable` |
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
    // JoinWithCredential entra como nodo temporal. Nunca recibe el secreto.
    JoinWithCredential(ctx context.Context, spec domain.GuestSpec) error
    Leave(ctx context.Context) error

    // Solo tienen sentido en un nodo admin. Ver decisiones 2 y 22.
    IssueCredential(ctx context.Context, req domain.CredentialRequest) (domain.Credential, error)
    RevokeCredential(ctx context.Context, id domain.CredentialID) error
    ListCredentials(ctx context.Context) ([]domain.Credential, error)

    Peers(ctx context.Context) ([]domain.Peer, error)
    Events() <-chan domain.EngineEvent
    Diagnostics(ctx context.Context) (domain.NetCheck, error)
}

type FirewallPort interface {
    Apply(ctx context.Context, desired domain.RuleSet) error  // declarativo, calcula la diferencia
    PurgeOwned(ctx context.Context) error                     // todo lo del grupo "Kanpachi"
    AuditForeign(ctx context.Context, p domain.GameProfile) ([]domain.ForeignRule, error)
    SuspendForeign(ctx context.Context, r []domain.ForeignRule) error
    RestoreForeign(ctx context.Context) error
}

type NetConfigPort interface {
    ApplyAdapter(ctx context.Context, want domain.AdapterState) error
    RevertTweaks(ctx context.Context) error
    ProbeMTU(ctx context.Context) (int, error)
}

type RoutingTable interface {
    LocalPrefixes(ctx context.Context) ([]netip.Prefix, error)
}

type CatalogStore interface {
    LoadBuiltin() ([]domain.GameProfile, error)
    LoadLocal() ([]domain.GameProfile, error)
    SaveLocal([]domain.GameProfile) error
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
type ExposureAudit interface {
    FirewallEnabled(ctx context.Context) ([]domain.FirewallProfileState, error)
    OwnRulesIntact(ctx context.Context) (bool, error)
    RouterMappings(ctx context.Context) ([]domain.PortMapping, error) // SOLO LECTURA
}
```

`RouterMappings` es la excepción de solo lectura a "el router no se toca nunca". El puerto **no declara** una operación de crear ni de borrar mapeos, y esa ausencia es deliberada: lo que no existe en la interfaz no se puede llamar por error.

### Estructura de carpetas

```
core/
  domain/      tipos y reglas puras. Cero imports de os, syscall, x/sys
  port/        las interfaces de arriba
  usecase/     un archivo por intención, recibe puertos por constructor

daemon/
  adapter/
    engine/easytier/
    firewall/windowscom/
    netcfg/windows/
    catalog/jsonfile/
    library/steam/
    inspect/iphlpapi/
    audit/windows/      ExposureAudit: firewall, reglas propias, IGD del router
  transport/
    protocol/           mensajes y códigos, sin transporte ni Windows
    pipe/               el named pipe y su token
  service/            main, host del servicio, cableado de dependencias
```

El cableado vive en un solo sitio, `service/`. Es el único lugar del proyecto que conoce a la vez el dominio y los adaptadores concretos.

### La regla verificada por un test

Esto es lo que evita que la arquitectura se degrade en tres meses. Un test que falla si `core` importa algo que no debe:

```go
func TestCoreNoTieneDependenciasSucias(t *testing.T) {
    prohibidos := []string{"os", "syscall", "golang.org/x/sys", "net/http", "os/exec"}
    // recorre core/... y falla si algún import matchea
}
```

Vale más que cualquier documento, porque no se puede ignorar sin querer. Si ese test pasa, `core` corre en Linux, en CI, sin privilegios y con adaptadores falsos.

> **Estado: pendiente.** Todavía no existe el módulo Go ni el workflow de CI. El primer commit que cree el módulo debe traer este test y el workflow que lo ejecuta. Hasta entonces la regla se sostiene a mano, y eso es deuda declarada, no el estado deseado.

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
    Name      string
    Path      PathKind      // Direct | Relay
    RTT       time.Duration
}

type NetCheck struct {
    NATKind    string                    // cone, symmetric, cgnat...
    UDPBlocked bool
    SeedRTT    map[string]time.Duration
}
```

`NetCheck` no es adorno: convierte "no conecta" en "tu router hace NAT simétrico, vas por relay". Lo produce `EnginePort.Diagnostics`.

### Identidad

```
código: 12 caracteres, 3 grupos de 4  (KANP-7X4M-B2QF)
alfabeto: 32 símbolos exactos. Los 36 alfanuméricos menos 0, O, 1, I
          (se conserva la L: en mayúsculas no se confunde con el 1)
entropía: 12 × 5 bits = 60 bits

networkID = Argon2id(normalizar(código), salt="kanpachi/v1/id")[0:16]
secret    = Argon2id(normalizar(código), salt="kanpachi/v1/secret")[0:32]
```

`normalizar` quita guiones y espacios y pasa a mayúsculas: pegar el código en cualquier formato funciona. Los salts llevan versión: un esquema `v2` futuro convive con `v1` sin romper clientes viejos.

### Formatos aceptados

Un solo campo en la UI, parser tolerante. Todas estas formas producen el mismo `networkID` y el mismo `secret`:

| Entrada | Seed que usa |
|---|---|
| `KANP7X4MB2QF` | el por defecto |
| `kanp-7x4m-b2qf` | el por defecto |
| `KANP-7X4M-B2QF@seed.midominio.com` | `seed.midominio.com` |
| `kanpachi.accentio.dev/#KANP-7X4M-B2QF` | `kanpachi.accentio.dev` |
| `https://kanpachi.accentio.dev/#KANP-7X4M-B2QF` | idem |
| `kanpachi://KANP-7X4M-B2QF` | el por defecto |

La app **genera** el formato URL con fragmento, que es el más autoexplicativo, sirve como landing de descarga para quien no tenga Kanpachi instalado, y mantiene el código fuera de los logs del servidor. La app **acepta** cualquiera de los seis. El usuario nunca tiene que saber cuál es el correcto.

El host del seed es solo transporte: **no entra en la derivación**. La misma sala es alcanzable por cualquier ruta si alguien conoce el código.

### Los 12 caracteres no se negocian

`KANP-7X4M` serían 40 bits. Como no hay backend que valide códigos, un atacante puede enumerar `networkID` contra un seed público hasta encontrar salas vivas. Con 60 bits eso es inviable por diseño, sin depender de que el seed limite tasa. La forma URL absorbe la longitud extra sin costo de usabilidad.

### Qué se recuerda y qué no

- **Un código sin host siempre usa el seed por defecto**, jamás el último usado. Recordar el último tiene una trampa real: tras entrar una vez a un seed ajeno, un código pelado de otro amigo fallaría sin explicación.
- **No se recuerda ninguna confirmación.** Todo código que llega de fuera de la app pasa por la tarjeta de confirmación, siempre, sin excepción y sin estado persistido. Ver la regla del canal externo más abajo.
- El único estado guardado de identidad es el seed propio del usuario, si configuró uno en Avanzado.

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

**La página de invitación no ve el código.** La URL lleva el código en el fragmento (`kanpachi.accentio.dev/#KANP-7X4M-B2QF`), que los navegadores no envían al servidor. El JavaScript de la página lo lee del lado del cliente y arma el `kanpachi://`. Los logs del servidor no pueden contener códigos de sala porque nunca los reciben.

### Punto de extensión de identidad

```go
type IdentityProvider interface {
    Resolve(input string) (networkID, secret []byte, meta RoomMeta, err error)
}
```

`LocalCodeProvider` es la v1. Un `RemoteRoomProvider` daría salas persistentes con nombre sin tocar UI ni daemon.

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

#### La ausencia del host no es un estado de conexión

Implementa la decisión 20. Que el host se haya ido es información **sobre la sala**, no sobre el túnel: la conexión sigue perfecta, lo que falta es la persona que corre el juego. Meterlo en la máquina de estados de arriba mezclaría dos cosas que fallan por motivos distintos.

Se modela como un campo aparte del estado de sala, que la UI muestra sin ambigüedad:

| Campo | Valores | Qué significa |
|---|---|---|
| `Role` | `Host`, `Guest` | Quién declaró hospedar. Lo fija `CreateRoom`, no cambia en la vida de la sala |
| `HostPresent` | `true`, `false` | Si el peer que hospeda está en la tabla de peers |

Reglas que se derivan:

- **El host se va:** sus reglas de firewall desaparecen con su máquina. Los invitados no tienen nada abierto porque nunca abrieron nada. La red queda inerte, no insegura.
- **El host vuelve:** su propio daemon conserva la sala activa en `config.json`, reactiva el perfil, y las reglas se regeneran para los miembros presentes en ese momento.
- **Nadie hereda el rol.** No hay promoción automática ni elección. Un invitado que quiera hospedar crea una sala nueva.
- **Se va el último nodo y la red deja de existir.** No hay estado persistido en ningún lado, así que volver exige un código nuevo. Es la consecuencia natural de no tener servidor de salas, no una limitación que haya que disculpar.

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

### transport/pipe, implementa la entrada

Named pipe `\\.\pipe\kanpachi`. Autenticación por token generado en la instalación y rotado en cada arranque del servicio, guardado en `ProgramData\Kanpachi\`.

**El protocolo se define aparte de su transporte.** Los mensajes son JSON-RPC delimitado por líneas, y el named pipe es una implementación de ese contrato, no el contrato. La separación cuesta cero hoy y es lo único que hace falta para que el host headless en Linux de `07-futuro.md` reuse la API sobre un socket Unix, con el mismo protocolo y los mismos casos de uso.

```
transport/
  protocol/    mensajes, códigos de error, serialización. Sin nada de Windows
  pipe/        el named pipe y su autenticación por token
```

**La superficie es la mitigación principal:** la API solo puede aplicar perfiles del catálogo embebido. No existe la operación "abrir puerto arbitrario". Un proceso malicioso corriendo como el usuario puede, como máximo, unirse a una sala y aplicar el perfil de un juego, nunca abrir 445 ni nada fuera del catálogo. La frontera de seguridad honesta es la sesión del usuario, igual que en cualquier aplicación de escritorio.

| Operación | Notas |
|---|---|
| `CreateRoom(nickname)` | Fija `Role = Host`. **No pide juego:** la sala es independiente del juego activo, decisión 20 |
| `JoinRoom(code, nickname)` | El nombre es obligatorio, ver decisión 21 |
| `LeaveRoom()` | |
| `ActivateProfile(gameID)` | **Solo el host.** Abre los puertos de ese juego. `gameID` vacío los cierra todos |
| `KickMember(virtualIP)` | **Solo el host.** Revoca la credencial y recalcula, ver decisión 22 |
| `RotateInviteCode()` | **Solo el host.** Código nuevo, los presentes no se enteran |
| `Status()` | Estado, rol, `HostPresent`, miembros con su nombre, juego activo, y las alertas vigentes |
| `ListGames()` | |
| `DiagReport()` | |

`Status()` es el único canal por el que la UI se entera de las alertas del módulo de exposición. No hay notificación aparte ni evento especial: el módulo publica su último resultado y `Status()` lo arrastra, así que una alerta nunca puede bloquear ni retrasar una respuesta.

### adapter/firewall/windowscom, implementa `FirewallPort`

- API COM `INetFwPolicy2`, nunca `netsh`: más rápida y sin dependencia del idioma del sistema.
- Todas las reglas llevan `Grouping = "Kanpachi"`. Al arrancar el servicio: purgar todo lo etiquetado, luego aplicar el estado deseado. Una muerte sucia del daemon nunca deja puertos huérfanos abiertos.
- Alcance por dirección, no por adaptador: la API de firewall de Windows no filtra por nombre de interfaz, así que cada regla lleva `LocalAddresses` = IP del adaptador kanpachi0 y `RemoteAddresses` = IPs de los miembros presentes.
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

func (f *FW) AuditForGame(p GameProfile) ([]ForeignRule, error)
func (f *FW) SuspendForeign(rules []ForeignRule) error
func (f *FW) RestoreForeign() error
```

Reglas de manejo: nunca se borran, solo se desactivan. El estado previo se persiste en `ProgramData\Kanpachi\suspended-rules.json` antes de tocar nada. Se restauran al salir de la sala y también al arrancar el servicio, por si una salida sucia dejó algo suspendido. Siempre con confirmación explícita del usuario en la UI, jamás automático.

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

**Conflicto de rango CGNAT.** Ver la sección de direccionamiento más abajo.

### adapter/engine/easytier, implementa `EnginePort`

**El único sitio del proyecto que menciona EasyTier.** Lo ejecuta como **proceso hijo** (`easytier-core`), no vinculado al binario Go. Razones en `02`, decisión 1: EasyTier es LGPL-3.0 y es Rust, así que un proceso separado mantiene la licencia de Kanpachi libre, evita cgo y aísla los fallos.

Responsabilidades:

- Traducir `domain.JoinSpec` a los parámetros del proceso: `--network-name` y `--network-secret` derivados del código, semillas con `-p`, relay de broadcast según el perfil activo.
- Ciclo de vida del hijo: arranque, supervisión, apagado limpio, y matar huérfanos al arrancar el servicio por si una salida sucia dejó uno vivo.
- Consultar estado y traducirlo a `[]domain.Peer` y `domain.NetCheck`. La salida del motor ya distingue conexión directa de relay y reporta el tipo de NAT, que es exactamente lo que la UI pinta en verde o ámbar.

El binario del motor se distribuye dentro de `Program Files\Kanpachi\`, y el día que haya firma de código son dos binarios que firmar, no uno.

### adapter/library/steam, implementa `GameLibrary`

`SteamPath` en el registro, `libraryfolders.vdf` para enumerar bibliotecas, `appmanifest_*.acf` cruzados por AppID contra el catálogo.

El resultado **ordena** la lista de juegos, nunca la filtra: la biblioteca completa del catálogo siempre está disponible y elegir un juego no detectado funciona igual. La detección es falible por diseño (juegos fuera de Steam, otras tiendas, unidades ilegibles) y ninguna de esas fallas puede bloquear la creación de una sala. Un error de este adaptador devuelve lista vacía, jamás rompe el arranque.

### adapter/inspect/iphlpapi, implementa `SocketInspector`

Consulta puntual a `GetExtendedUdpTable` y `GetExtendedTcpTable` pidiendo el PID dueño. **Solo lo usa el creador de perfiles**, disparado por un botón del usuario. Fuera de ese asistente nunca se invoca. Ver `06-catalogo.md`.

### transport/control, el canal de la sala

Implementa la decisión 23. **Solo escucha en la máquina del host**, sobre la IP de `kanpachi0`, con alcance limitado a las IPs de los miembros presentes. Los invitados únicamente marcan hacia afuera y nunca abren un puerto.

Lo que transporta, en volumen de bytes: el canje del código por credencial cuando alguien entra, el aviso de expulsión, el anuncio de cierre de sala, y por su sola existencia la presencia del host. **Nada del juego pasa por acá.**

**Es el código que más revisión merece del proyecto.** Corre como SYSTEM y parsea mensajes de gente que está en la sala. Reglas no negociables: tope de tamaño antes de deserializar, esquema cerrado sin tipos arbitrarios, tope de conexiones por IP virtual, y rechazo de toda IP que no sea de un miembro presente. Un fallo acá es ejecución remota como SYSTEM en la máquina del host.

Del lado del invitado no hay servidor, solo un cliente que reconecta con backoff. Que esa conexión esté caída es lo que alimenta `HostPresent` y el contador de 20 minutos de la decisión 20.

### service/supervisor, orquesta

- Watchdog del motor: si EasyTier muere, reinicio con backoff, límite de intentos, purga de reglas si se rinde.
- **Contador de ausencia del host**, solo en invitados: 20 minutos sin canal de control implica salir de la sala. Ver decisión 20.
- **Eventos de identificación de red:** suscripción a `Microsoft-Windows-NetworkProfile/Operational` Event ID 10000. Dispara la reaplicación completa de `netcfg`. Sin esto, los ajustes se pierden solos y el usuario ve que "ayer funcionaba".
- Eventos de energía: Fast Startup y suspender/despertar dejan endpoints muertos y sesiones colgadas. En cada resume: revalidar, rehacer hole punch, reconectar solo.
- Eventos de cambio de red: WiFi a cable, cable a LTE. Nueva IP pública implica renegociar sin perder la sala.
- Sala vacía: cerrar puertos, revertir ajustes por juego, volver a Idle.

## kanpachi-seed

Un contenedor EasyTier en el droplet con listeners explícitos en 11010 TCP y UDP.

**Qué ve:** networkIDs (opacos, derivados) y las IPs públicas de quienes están en cada sala.

**Qué no ve ni puede:** el secret (nunca viaja), el tráfico en claro (WireGuard extremo a extremo), unirse a una sala (le falta el secret). Auditable, porque el código del cliente está a la vista del grupo.

**Funciones en orden de frecuencia:** presentar endpoints entre miembros del mismo networkID, sincronizar el disparo del hole punch, relevar paquetes cifrados como último recurso.

## Flujo de una conexión

```
1. El usuario pega el código
2. identity deriva networkID + secret
3. El daemon resuelve semillas: registro DNS primero,
   Reserved IP compilada como respaldo
4. engine anuncia el networkID al seed
5. El seed devuelve los endpoints de los demás miembros
6. Disparo sincronizado del hole punch en ambos lados
7. Túnel WireGuard directo peer a peer
   (fallback: relay vía seed, marcado en ámbar en la UI)
8. policy genera las reglas para los miembros presentes
9. netfw aplica la diferencia
```

## Almacenamiento

```
Program Files\Kanpachi\      binarios (daemon, ui, easytier-core) + wintun.dll + perfiles builtin
ProgramData\Kanpachi\
  config.json                nombre visible, sala activa, rol
  api.token                  rotado por arranque del servicio
  room.json                  SOLO EN EL HOST: código vigente y registro de credenciales emitidas
  last-room.json             SOLO EN INVITADOS: última sala, para "volver a la última sala"
  suspended-rules.json       reglas ajenas desactivadas y su estado previo
  easytier-credentials.json  el --credential-file del motor
  logs\                      texto plano, rotación por tamaño
```

ACL de ProgramData: escritura solo SYSTEM y Administradores, lectura para usuarios de la máquina.

**El daemon es la única fuente de verdad.** Cerrar la ventana no cierra la sala, así que el estado tiene que sobrevivir a la UI. La UI lo lee por `Status()` y persiste únicamente cosas de presentación, como el tamaño de la ventana. Guardar la sala también del lado de Flutter crearía dos verdades que se desincronizan justo en el caso que el producto promete soportar, que es cerrar la ventana con la partida viva.

**`room.json` y `last-room.json` contienen credenciales de sala.** La ACL de ProgramData da lectura a los usuarios de la máquina, así que cualquier proceso del usuario puede leerlos. Es coherente con el modelo de amenazas, que ya asume que malware corriendo como el usuario puede usar la API igual que el usuario. Vale escribirlo para que nadie los trate como inocuos: son portadores de acceso a la sala y sobreviven a la sesión.

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

## Modelo de amenazas, resumen honesto

| Amenaza | Resultado |
|---|---|
| Miembro de la sala comprometido | Alcanza solo los puertos del juego activo en el host. 445/3389/22 cerrados siempre |
| Seed comprometido | Ve networkIDs e IPs públicas. No descifra, no se une, no alcanza servicios |
| Código de sala filtrado | El portador entra hasta que el host renueve el código. **Mitigación activa:** renovar cuesta un click y no expulsa a los presentes. El firewall sigue limitando a los puertos del juego |
| Miembro expulsado que insiste | Revocada su credencial, sale de la red en ~1 s. Vuelve solo si conserva un código vigente, y el host lo cierra renovando |
| Miembro manda basura al canal de control | Es la superficie más seria del producto, y solo existe en la máquina del host. Ver el modelo de amenazas de la decisión 23 |
| Miembro intenta hacerse pasar por el host | No puede. Los invitados marcan hacia una dirección conocida y no aceptan conexiones entrantes |
| Malware local como el usuario | Usa la API igual que el usuario: unirse a salas, aplicar perfiles del catálogo. No puede abrir puertos arbitrarios. Puede leer `room.json` y con eso entrar a la sala |
| Malware local con admin | Fuera del alcance: con admin ya controla la máquina completa |
