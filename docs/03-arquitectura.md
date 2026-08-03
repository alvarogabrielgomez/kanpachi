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
│      ├── domain/    tipos y reglas puras, invariantes, plazos      │
│      ├── port/      las interfaces que el dominio necesita         │
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
    // PurgeOwned borra todo lo del grupo "Kanpachi", por igualdad exacta y
    // jamás por prefijo. NUNCA toca "Kanpachi-base": esa es la cuarentena que
    // puso el instalador, y es lo que protege la máquina cuando el daemon no
    // corre. Ver decisión 4
    PurgeOwned(ctx context.Context) error
    AuditForeign(ctx context.Context, p domain.GameProfile) ([]domain.ForeignRule, error)
    SuspendForeign(ctx context.Context, r []domain.ForeignRule) error
    RestoreForeign(ctx context.Context) error
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
//   room.json       SOLO EN EL HOST. Salir limpio lo borra y morir sucio lo
//                   deja, así que su sola presencia al arrancar es la señal de
//                   mal cierre. No hay bandera "dirty" dentro
//   last-room.json  SOLO EN INVITADOS. Código, seed, nombre y nick. Jamás la
//                   credencial ni la identidad de la red real
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
// Que un método falle NO se calla: los dos primeros levantan AlertAuditFailed,
// porque "no se pudo comprobar" y "todo en orden" se ven igual desde la
// pantalla. El tercero no, y esa asimetría es deliberada: la mayoría de los
// routers nunca contestan al IGD
type ExposureAudit interface {
    FirewallEnabled(ctx context.Context) ([]domain.FirewallProfileState, error)
    OwnRulesIntact(ctx context.Context) (bool, error)
    RouterMappings(ctx context.Context) ([]domain.PortMapping, error) // SOLO LECTURA
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
    // decisión 25 iría acá el día que exista
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
    engine/easytier/
    firewall/windowscom/
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
  kanpctl/            la herramienta con la que se prueba el pipe a mano. En
                      internal/ para que el producto no la importe y el
                      instalador no la distribuya
```

**Los provisionales fallan en todo, y eso no es pereza.** Uno que devuelve éxito hace la cuarentena inverificable desde dentro del programa: un firewall que dice "purgado" sin purgar deja la pantalla en verde, al módulo de exposición sin encontrar nada y al producto entero afirmando una promesa que nadie cumplió. El daño no es que falten funciones, es que la mentira sea indistinguible de la verdad.

`RestoreForeign` es el que más invita a la excepción, porque el arranque lo llama por si una salida sucia dejó algo desactivado. Falla igual: devolver `nil` haría que el daemon anotara "reglas ajenas restauradas" sin restaurar nada, y el usuario se quedaría con reglas de su juego apagadas creyendo que volvieron. La única excepción es `SystemEvents`, forzada por su firma, ya que sus tres métodos devuelven canales y no hay dónde poner un error; sus canales mudos dicen la verdad, que no hay eventos, y el supervisor ya reaplica por temporizador porque estas suscripciones no son fiables ni con el adaptador de verdad.

**La etiqueta de compilación va al revés de lo intuitivo:** el build con provisionales es el por defecto y el de release lleva la etiqueta. Con la etiqueta del lado de los provisionales, olvidarla produce un binario de release silencioso que compila, se instala como servicio y no hace nada de lo que promete. Con esta dirección, olvidarla produce uno que se NIEGA a instalarse. El olvido tiene que doler del lado seguro. Y mientras `sinimplementar.Presente` sea cierto, `kanpachid` solo arranca con `--console`.

El cableado vive fuera de los casos de uso y está partido en dos, con una frontera que no es de gusto: `service/` sabe en qué orden pasan las cosas y `cmd/kanpachid/` sabe con qué. Solo el segundo conoce a la vez el dominio y Windows.

**Por qué el `main` no vive en `service/`.** El guardián de pureza parsea con `parser.ImportsOnly` y **no mira las etiquetas de compilación**, así que un `main.go` con `//go:build windows` que importe `golang.org/x/sys/windows/svc` hace fallar el test de pureza desde el job de Linux. Las dos salidas alternativas son peores: sacar `service/` de la lista de puros reduce la cobertura del guardián justo en la capa donde entra el código nuevo, y enseñarle a saltarse archivos con etiqueta abre el agujero exacto que existe para tapar, porque entonces cualquiera pone `//go:build windows` sobre un archivo de `core` y mete lo que quiera.

Mover el `main` no inventa una convención: es la que el repo ya usa para su otro binario, `registry/cmd/kanpseed/main.go`.

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

La app **genera** el formato URL, que es el más autoexplicativo y sirve de landing de descarga para quien no tenga Kanpachi. La app **acepta** cualquiera de los seis. El usuario nunca tiene que saber cuál es el correcto.

Un fragmento después del ID (`/A7K2M9QX#clave`) es enriquecimiento opcional: lleva la clave de la tarjeta de sala. La app lo ignora, le sirve para nada, el nombre de la sala lo recibe por el canal de control.

**Un invite ID es local al seed que lo emitió.** El mismo ID en dos seeds son dos salas que no se conocen. Un ID pelado usa el seed por defecto, jamás el último usado.

### Por qué 8 caracteres alcanzan

El diseño anterior exigía 60 bits con este argumento: sin backend que valide, un atacante enumera `networkID` contra un seed público hasta hallar salas vivas, y la única defensa posible es la entropía. Las dos premisas cambiaron con las decisiones 2 y 24.

Hay un registro que responde las consultas, o sea que la enumeración se corta con límite de tasa como cualquier otra. Y acertar un invite ID ya no da entrada: da la tarjeta y el derecho a tocar la puerta, porque entrar exige una credencial que emite el host. 40 bits con límite de tasa y un premio acotado es un intercambio distinto que 40 bits sin nada y entrada perpetua.

A cambio se gana lo que el producto necesita: un ID que una persona dicta por teléfono sin equivocarse.

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

**La página de invitación resuelve el invite ID contra el registro del seed.** La URL lo lleva en la ruta (`kanpachi.accentio.dev/A7K2M9QX`), así que el servidor lo recibe y lo registra en sus logs. Es aceptable porque el invite ID dejó de ser material criptográfico con la decisión 2: quien lo lea obtiene la tarjeta y el derecho a tocar la puerta, jamás el secreto de la red real, que no vive en ningún servidor. Ver decisión 17 para el intercambio completo.

### Punto de extensión de identidad

```go
// Resuelve un invite ID a la identidad de ENCUENTRO, jamás a la red real.
// La red real solo llega por el canje de credencial con el host.
type RendezvousProvider interface {
    // Devuelve también la Room, o sea el invite ID con su seed: un invite ID
    // solo significa algo en el registro que lo emitió, y quien resuelve la
    // entrada es el único que sabe cuál era.
    Resolve(input string) (domain.Room, domain.Rendezvous, error)
}
```

`LocalDerivation` es la v1: Argon2id sobre el invite ID, sin red y sin preguntarle a nadie. Un proveedor remoto daría salas con identidad de encuentro rotativa sin tocar UI ni daemon.

El registro del seed se consume por un puerto aparte, porque es opcional por diseño: entrar funciona sin él, lo que se pierde es la tarjeta de presentación.

```go
// Solo presentación. Que falle no impide entrar a ninguna sala.
type RoomDirectory interface {
    Lookup(inviteID domain.InviteID) (domain.RoomCard, error)
    Publish(card domain.RoomCard, signer domain.Signer) error
}
```

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
| `EnginePeersChanged` | ninguno | Recalcula el conjunto de reglas completo. Es la misma operación que un cambio de juego |
| `EngineDegraded` | `Degraded` | **Nada más, y esa ausencia importa:** degradado es que el túnel sigue en pie y va peor, normalmente por relay, que es un caso soportado. Contarlo como caída echaría de la sala a quien está jugando por relay |
| `EngineDisconnected` | `Reconnecting` | Arranca el plazo sin túnel, y en un invitado apaga la presencia del host |
| `EngineDied` | `Reconnecting` | Igual, y además despierta al watchdog |

**Estar en `Reconnecting` no es eterno.** A los 10 minutos sin túnel se sale de la sala con motivo propio, se cierran los puertos y se revierten los ajustes. Ver decisión 20.

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
- **El host vuelve:** su propio daemon conserva la sala en `room.json` con el juego que estaba activo, y al arrancar **pregunta** si reabrirla. Al reabrir, la identidad de la red es la misma, el perfil se repone resolviéndolo contra el catálogo de esa máquina, y las reglas se regeneran para los miembros presentes en ese momento. Nunca reabre sola. Ver decisión 2.
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

**El enmarcado vive una sola vez.** El pipe y el canal de la sala leen bytes de gente que no es este programa, los dos corren como SYSTEM, y los dos necesitan el mismo tratamiento: tope antes de deserializar y desincronización tratada como terminal. Lo único que cambia entre ellos es el tope, un mega por el pipe donde pasa un catálogo importado, ocho kilobytes por el canal donde el mensaje más grande son unos cientos de bytes. Por eso el tope es un parámetro y el código es uno: el día que una copia se arregle, la otra no.

**La superficie es la mitigación principal:** la API solo puede aplicar perfiles del catálogo embebido. No existe la operación "abrir puerto arbitrario". Un proceso malicioso corriendo como el usuario puede, como máximo, unirse a una sala y aplicar el perfil de un juego, nunca abrir 445 ni nada fuera del catálogo. La frontera de seguridad honesta es la sesión del usuario, igual que en cualquier aplicación de escritorio.

#### Lo que está medido, y no argumentado

El nombre vive bajo `\\.\pipe\ProtectedPrefix\Administrators\kanpachi`. Cualquier proceso del usuario puede crear `\\.\pipe\kanpachi` y quedarse con el nombre antes que el servicio, y ahí la defensa sería ganar una carrera, que se pierde el día que el arranque va lento. Bajo el prefijo protegido no puede, y no porque lo comprobemos nosotros: **arrancar el daemon sin elevar falla con "Access is denied" al crear el nombre.** El modo consola usa otro nombre, porque con el mismo un proceso sin privilegios ocuparía el de producción arrancando nuestro propio binario con `--console`.

El descriptor es `D:P(A;;GA;;;SY)(A;;GA;;;BA)(A;;0x12019b;;;IU)`. Al usuario interactivo se le da leer, escribir y sincronizar, **jamás `GENERIC_ALL`**, y la razón se comprobó sola: con el daemon corriendo como usuario normal, el pipe se crea y la primera conexión falla al aceptar, porque aceptar exige crear la instancia SIGUIENTE del pipe y eso el usuario no puede. Que es el punto entero, ya que crear instancias es cómo se secuestraría la conexión de la UI. Como SYSTEM sí tiene permiso y atiende normal. Probar a mano exige, por lo tanto, una consola elevada.

Pasar el descriptor vacío es un error y no una opción: el descriptor por defecto de un named pipe da lectura a Everyone y a la cuenta anónima.

**Los topes son constantes de compilación**, igual que los cortes automáticos: ocho conexiones, cinco segundos para saludar, diez minutos de ocio, cinco segundos por respuesta. El de escritura es por MENSAJE y no por escritura, porque `wire.Writer` escribe sobre un bufio y una respuesta grande sale en varios trozos; renovar el plazo en cada trozo deja que un cliente que lee un byte por segundo mantenga la conexión abierta para siempre.

**Un pánico atendiendo una conexión no se lleva el daemon.** Sin ese recover, cualquier ruta de la API que reviente mata el proceso, y con él la sala: puertos cerrados, motor caído y la partida de todos al suelo porque una pantalla pidió algo raro. Lo encontró un test con la API a medio implementar, que es la forma exacta que tiene el daemon mientras queden adaptadores provisionales.

#### El cliente, del lado de Flutter

Vive en `ui/lib/features/session/infra/daemon/` y repite la misma separación: `daemon_codec.dart` es el enmarcado y los mensajes, `daemon_client.dart` es la conversación, y `daemon_transport.dart` es una interfaz de tres métodos. Con eso, el saludo, la correlación y los plazos se prueban enteros sobre un transporte de memoria, sin pipe, sin daemon y sin Windows.

Tres cosas que el cliente resuelve y que parecen detalles hasta que faltan. **El saludo va primero**, porque el daemon rechaza todo método antes del `hello` y sin esa espera la primera petición de cada conexión falla con `unauthorized`. **Las respuestas se emparejan por id y jamás por orden de llegada**, que es correcto hasta el día que dos pantallas preguntan a la vez. **Toda petición tiene plazo**, porque un daemon vivo que dejó de contestar es indistinguible de uno muerto desde el otro lado.

**El transporte de Windows todavía no se puede escribir con `dart:io`, y eso está verificado.** No hay `Socket` ni `File` que sirva: el soporte de IPC multiplataforma sigue siendo una petición abierta en el SDK de Dart (dart-lang/sdk#47310), y el atajo que existía, `File(r'\\.\pipe\...').openSync()`, funcionaba hasta Flutter 3.24.5 y **está roto desde 3.27** con `PathNotFoundException ... errno = 53`, sin arreglo (flutter/flutter#163539). Quedan dos caminos y los dos pasan por `dart:ffi` con `CreateFileW`, `ReadFile` y `WriteFile`: escribirlo en el repo sin dependencias, o tomar `dart_ipc`, que ya lo hace con E/S superpuesta y tiene siete likes en pub.

**Decidido: se prueba con `dart_ipc` primero y se decide viendo.** Escribir la E/S superpuesta a mano para descubrir después que la librería servía es trabajo tirado, y al revés se mide en una tarde. Lo que hace reversible la prueba es que el transporte es una interfaz de tres métodos: cambiarlo no toca ni el cliente ni el códec ni sus tests. **El disparador para internalizarlo** es cualquiera de estos tres, y conviene escribirlo ahora que no duele: que la librería no soporte el prefijo protegido del nombre, que bloquee el isolate de la UI, o que deje de mantenerse. Que quedarse sea el camino de menor resistencia es el riesgo conocido de esta elección.

Lo que no está en discusión: **la lectura no puede bloquear el isolate de la UI**, así que va en un isolate aparte o con E/S superpuesta.

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
| `PendingRoom()` / `ResumeRoom()` / `DiscardPendingRoom()` | `pending_room` / `resume_room` / `discard_pending_room` | La sala que quedó abierta tras un mal cierre. **Nunca se reabre sola**, ver decisión 2 |
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
| `PendingRoom()`, `ResumeRoom()`, `DiscardPendingRoom()` | La UI, al arrancar, si hubo mal cierre | Preguntar por la sala del arranque anterior. Nunca se reabre sola |
| `LastRoom()` | La UI, en la pantalla de inicio | Los datos de "volver a la última sala". Entrar es el `JoinRoom` de siempre |

Sobre la primera: La llama el adaptador del canal de control cuando alguien toca la puerta del vestíbulo. Vive en los casos de uso y no en el adaptador porque todo lo que decide es política: si esta máquina puede emitir, qué dirección le toca al que entra, cuánto vale la credencial y qué se le cuenta de la red. El motor pone el token, que es lo único que no se decide acá y tiene que ser así, porque revocarlo es lo que corta la sesión.

**Las direcciones se reparten mirando dos listas, no una:** los peers conectados y las credenciales emitidas todavía vigentes. Solo los peers repartiría la misma dirección a dos personas que entran a la vez, que es exactamente lo que pasa cuando alguien manda el código al grupo y los tres lo pegan al mismo tiempo.

`Status()` es el único canal por el que la UI se entera de las alertas del módulo de exposición. No hay notificación aparte ni evento especial: el módulo publica su último resultado y `Status()` lo arrastra, así que una alerta nunca puede bloquear ni retrasar una respuesta.

### adapter/firewall/windowscom, implementa `FirewallPort`

- API COM `INetFwPolicy2`, nunca `netsh`: más rápida y sin dependencia del idioma del sistema.
- Todas las reglas llevan `Grouping = "Kanpachi"`. Al arrancar el servicio: purgar todo lo etiquetado, luego aplicar el estado deseado. Una muerte sucia del daemon nunca deja puertos huérfanos abiertos.
- **Alcance por adaptador Y por dirección.** Acá decía que la API de firewall de Windows no filtra por nombre de interfaz. Es falso: `INetFwRule` tiene la propiedad `Interfaces`, y la propia documentación dice *"the interfaces in the list are represented by their friendly name"*. Comprobado sobre el sistema, no leído: hay una regla viva de Microsoft acotada a un adaptador virtual, que es exactamente el caso de Kanpachi.

  ```
  HNS Container Networking - DNS (UDP-In)
  Interfaces : vEthernet (WSL (Hyper-V firewall))
  ```

  Así que cada regla de permiso lleva `Interfaces = ["kanpachi0"]`, más `LocalAddresses` = IP del adaptador y `RemoteAddresses` = IPs de los miembros presentes. Los tres a la vez, porque cada uno falla de una forma distinta y el solapamiento es la defensa.

  Por dentro Windows lo guarda como GUID del adaptador (`IF={...}` en el almacén de reglas) y lo devuelve resuelto a nombre. De ahí salen las dos propiedades que importan: sobrevive a que el usuario renombre la conexión, y **no** sobrevive a que el adaptador se recree con un GUID nuevo. Lo segundo es justo lo que hace `Apply` al reaplicar, que enumera lo vivo y calcula la diferencia.

  **El alcance por interfaz va SOLO en los permisos, jamás en los bloqueos de `Kanpachi-base`.** No es simetría estética: si el alcance deja de casar, un permiso que deja de aplicar CIERRA y un bloqueo que deja de aplicar ABRE. La cuarentena del instalador se acota por dirección y nada más.

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

### adapter/engine/kanpachi, implementa `EnginePort`

**El único sitio del proyecto que menciona el motor.** Lo ejecuta como **proceso hijo**, no vinculado al binario Go. Razones en `02`, decisión 1: el motor es Rust y LGPL-3.0, así que un proceso separado mantiene la licencia de Kanpachi libre, evita cgo y aísla los fallos. Lo último dejó de ser un argumento y pasó a ser un hecho medido: el workspace del motor compila su perfil de release con `panic = "abort"`, o sea que un `panic` dentro del mismo proceso se llevaría el servicio sin que nada pueda atajarlo.

**El hijo NO es `easytier-core.exe`.** Es `kanpachi-engine.exe`, un binario propio que usa EasyTier como **librería** y vive en su propio repositorio bajo LGPL-3.0. La diferencia no es de gusto, y se midió con el mismo fichero de configuración sobre la misma máquina:

| Proceso | Sockets en escucha |
|---|---|
| `easytier-core.exe` v2.6.4 | `TCP 0.0.0.0:15888 LISTENING` |
| El motor propio, sobre la librería | ninguno |

Ese `15888` es el portal de administración del motor, **no tiene autenticación de ninguna clase**, y su valor por defecto escucha en todas las interfaces pese a que la ayuda oficial dice `localhost`. Por ahí cualquier proceso local emite credenciales de la red real, agrega nodos y pide el `network_secret` en claro. El portal se construye en un solo sitio del árbol de EasyTier, dentro de su binario de línea de comandos, y el arranque de red por librería no lo menciona: **desaparece por omisión, no por configuración.**

Responsabilidades:

- Traducir `domain.HostSpec`, `domain.RendezvousSpec` y `domain.GuestSpec` a la configuración del motor, que viaja por **stdin como TOML** y jamás por la línea de comandos. Medido: el `CommandLine` del proceso hijo solo muestra la ruta del ejecutable, así que el secreto de la red deja de ser legible con el Administrador de tareas por cualquier usuario de la máquina.

  El entorno del hijo se arma **explícito**, nunca heredado. Cada bandera de EasyTier tiene una gemela por variable de entorno, `ET_CONFIG_SERVER` y `ET_PORT_FORWARD` entre ellas, así que un hijo que hereda el entorno acepta capacidades prohibidas sin que nadie las escriba en el argv. Lo vigila `internal/arch/motor_test.go`.

  **No traduce el descubrimiento LAN, porque no llega hasta acá.** `HostSpec` y `GuestSpec` no tienen campo para él: encenderlo significa `--enable-udp-broadcast-relay`, o sea capturar el tráfico de la red de casa del usuario con un driver de captura de paquetes, y la decisión 1 lo difiere hasta que exista un juego que lo pida. El perfil sí declara `lan_discovery`, porque el catálogo es la capa de conocimiento; esta es la capa que decide qué se concede.
- Ciclo de vida del hijo: arranque, supervisión, apagado limpio, y matar huérfanos al arrancar el servicio por si una salida sucia dejó uno vivo.
- Consultar estado y traducirlo a `[]domain.Peer` y `domain.NetCheck`. La salida del motor ya distingue conexión directa de relay y reporta el tipo de NAT, que es exactamente lo que la UI pinta en verde o ámbar.

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

La interfaz nace en cuarentena, o sea sin ninguna regla de permiso, así que **el host tiene que abrirle un hueco a su propia puerta**. Ese hueco es la única regla que Kanpachi crea sin que ningún perfil la pida, va en el mismo conjunto declarativo que las de juego, y se describe en la decisión 4. Calcularlo aparte de `BuildRuleSet` no es organización: aquella devuelve vacío cuando no hay juego activo, que es el estado normal de una sala recién creada.

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

El aislamiento también sale mejor, no peor: en Docker el registro corría como root dentro del contenedor, y con `DynamicUser=` corre como un usuario efímero sin casa, sin disco escribible y con `CapabilityBoundingSet` vacío.

**Lo que cuesta:** rompe la convención del droplet, donde todo lo demás vive en Docker. Es el único argumento real en contra.

**Un detalle que costó un rato:** `easytier-cli` rechaza nombres de host con `invalid socket address syntax`. Con systemd da igual, porque la dirección es literal, y el resolvedor que se escribió para Docker se conserva porque no estorba y cubre una configuración con el motor en otra máquina.

### Lo que el registro decidió, y por qué

**Emite el invite ID en vez de aceptarlo.** Quien tiene que garantizar unicidad es el registro, así que emitir evita el ida y vuelta de proponer y ser rechazado. Y no hay nada que filtrar: un invite ID no deriva material criptográfico de la sala real.

**Deriva la red de encuentro él mismo**, con el mismo Argon2id del cliente, en vez de creerle al host. Si la aceptara del host, cualquiera podría hacer que el contador de su sala reflejara la de otra.

**Omite el contador si nunca pudo hablar con EasyTier.** Un cero es la afirmación "no hay nadie", y sería falsa; ausente dice la verdad, "no lo sé". La página se comporta distinto en cada caso.

**Dos vencimientos distintos, y la diferencia es el corazón de la reapertura.** La tarjeta vive 6 horas, porque describe una sala que quizá ya no existe. El **fijado** de la llave del host vive 21 días, porque es lo único que impide que un ex miembro, que conserva el invite ID, se adelante al host cuando reabre. Sin esa asimetría, reabrir con el mismo código sería una carrera que gana el que esté más atento.

**Límite de tasa de 30 peticiones por minuto y por IP.** Es la defensa que reemplazó a los 60 bits de entropía del diseño anterior: 40 bits son enumerables sin freno y seguros con freno. Lee `X-Forwarded-For`, lo cual solo es sensato porque el proceso vive detrás del proxy inverso del droplet. Exponerlo directo a internet permitiría falsificar esa cabecera y anular el límite entero.

**Todo en memoria, sin base de datos y sin disco.** Reiniciar el registro cuesta que los invitados vean la tarjeta genérica hasta que el host vuelva a publicar, y jamás impide entrar, porque entrar no pasa por él.

**Qué ve el seed:** invite IDs vivos, networkIDs de encuentro, llaves públicas de hosts, tarjetas que no puede descifrar, e IPs públicas de quienes están en cada red.

**Qué no ve ni puede:** el secreto de la red real de ninguna sala, o sea que no puede unirse a ninguna. El tráfico en claro, que va cifrado extremo a extremo. Los nicks de los miembros, que viven dentro de la red cifrada; verificado, `peer list-foreign` devuelve `peer_id` y nada más.

**Funciones en orden de frecuencia:** presentar endpoints entre miembros del mismo networkID, sincronizar el disparo del hole punch, resolver invite IDs, relevar paquetes cifrados como último recurso.

El registro vive en memoria con TTL, sin base de datos y sin disco, salvo la llave fijada del host, que sobrevive semanas para que reabrir con el mismo invite ID siga siendo del host. Ver decisión 24.

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
Program Files\Kanpachi\      binarios (daemon, ui, easytier-core) + wintun.dll + perfiles builtin
ProgramData\Kanpachi\
  config.json                nombre visible, sala activa, rol
  api.token                  rotado por arranque del servicio
  identity.key               llave privada larga de esta instalación (decisión 25)
  known-hosts.json           libreta de huellas: nick visto, llave con que se lo vio
  room.json                  SOLO EN EL HOST: invite ID con su seed, identidad de la red
                             real, subred, nombre, nick, clave de la tarjeta e id del
                             juego activo. Su PRESENCIA al arrancar es la señal de mal
                             cierre: salir limpio lo borra
  last-room.json             SOLO EN INVITADOS: código, seed, nombre de la sala y nick.
                             Jamás la credencial ni la identidad de la red real
  suspended-rules.json       reglas ajenas desactivadas y su estado previo
  easytier-credentials.json  el --credential-file del motor
  logs\                      texto plano, rotación por tamaño
```

ACL de ProgramData: escritura solo SYSTEM y Administradores, lectura para usuarios de la máquina.

**El daemon es la única fuente de verdad.** Cerrar la ventana no cierra la sala, así que el estado tiene que sobrevivir a la UI. La UI lo lee por `Status()` y persiste únicamente cosas de presentación, como el tamaño de la ventana. Guardar la sala también del lado de Flutter crearía dos verdades que se desincronizan justo en el caso que el producto promete soportar, que es cerrar la ventana con la partida viva.

**`room.json` lleva la identidad de la red real, o sea que es portador de acceso a la sala.** La ACL de ProgramData da lectura a los usuarios de la máquina, así que cualquier proceso del usuario puede leerlo. Es coherente con el modelo de amenazas, que ya asume que malware corriendo como el usuario puede usar la API igual que el usuario. Vale escribirlo para que nadie lo trate como inocuo: sobrevive a la sesión.

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

### El vestíbulo tiene un /24 fijo

`100.127.255.0/24`, el último del espacio compartido, con el host siempre en la `.1`.

Fijo y no negociado porque los dos lados tienen que llegar al mismo sin hablarse: **el invitado necesita una dirección conocida a la que marcar antes de tener nada del host**, y la subred de la sala llega dentro de la credencial, o sea después. Elegirlo al azar exigiría un canal para comunicarlo, y ese canal es justamente el que se está montando.

Que sea el mismo para todas las salas no filtra nada. El vestíbulo ya es público por definición, su red la deriva cualquiera que tenga el invite ID, y una dirección dentro de un overlay cifrado no dice de qué sala es. La estancia dura lo que tarda un canje de credencial.

Ese `/24` **nunca se le entrega a una sala**. Si coincidieran, entrar a la sala cortaría la conexión que se está usando para pedir la credencial, y el fallo aparecería una vez de cada dieciséis mil.

## Auditoría de ciberseguridad

Se pasó el producto por **OWASP Top 10 (2021)** y el repo por **OWASP Agentic Skills Top 10 (v1.0, 2026)**, que aplica porque este proyecto se escribe con un agente y trae 46 skills de un tercero. Lo que sale sin acción se escribe igual: una lista que solo dice "cumple" no sirve para nada dentro de seis meses.

### El producto

| # | Categoría | Estado |
|---|---|---|
| A01 Control de acceso | La lista cerrada del pipe, los dos alcances del canal, el rechazo por IP en el `Accept`, el recorte al expulsar | Cubierto, con tests que afirman que los métodos que no pueden existir no existen |
| A02 Fallos criptográficos | Sellado de credencial y de código, tarjeta cifrada con clave en el fragmento, Argon2id congelado, credencial sin el secreto de la red | Cubierto con **un hueco nombrado**: sin la decisión 25 no hay autenticación del host en el vestíbulo |
| A03 Inyección | JSON estricto, tabla cerrada sin reflexión, ids contra alfabeto aburrido | Cubierto. Regla fijada para los adaptadores que faltan: el motor se invoca con lista de argumentos y jamás con una cadena de shell, y el firewall por COM y nunca por `netsh` con texto interpolado |
| A04 Diseño inseguro | Deny-all por defecto, no existe abrir un puerto arbitrario, cortes que no se apagan desde fuera, capas que no dependen de la anterior | Cubierto. Es el grueso de las decisiones 4, 20, 22 y 26, con guardianes por AST en `internal/arch` |
| A05 Configuración insegura | Banderas prohibidas del motor, `--disable-upnp`, portal RPC en loopback, ACL de ProgramData | **Arreglado un hallazgo.** La promesa de "hay un test que falla si alguien saca esas banderas" era cierta solo para el seed. `internal/arch/motor_test.go` la cubre ahora para el cliente: barre `daemon/` ENTERO buscando banderas prohibidas, encuentra el paquete del motor por su contenido y no por una ruta fija, mira los pares en orden dentro de un argv para que `--disable-upnp false` no pase por verde, y exige que el adaptador traiga su propio test de argumentos |
| A06 Componentes desactualizados | Los binarios de EasyTier no se versionan, y **nada comprobaba que el de una máquina fuera el que se probó** | **Arreglado.** Sumas SHA256 en la decisión 1 y en `internal/arch/easytier.sums`, con dos tests: uno verifica el disco, el otro que el manifiesto y los docs no se separen |
| A07 Identificación y autenticación | Token del pipe rotado por arranque con comparación en tiempo constante; en el canal, la pertenencia la impone la credencial del motor | Cubierto para el pipe. El hueco del vestíbulo es el mismo de A02 y tiene dueño |
| A08 Integridad de software y datos | Decodificadores estrictos en catálogo, estado y protocolo; escritura atómica; importar exige elección explícita; sin autoactualización | Cubierto. Pendiente conocido: firma de código de los dos binarios |
| A09 Registro y monitoreo | Logs locales, cero telemetría, módulo de alertas de la decisión 19 | **Arreglado un hallazgo:** `PersistedRoom` no redactaba, así que un `%+v` en un mensaje de error mandaba el secreto de la red REAL a los logs que el usuario copia al portapapeles. Hay test, sobre `%v` y sobre `%+v` |
| A10 SSRF | El seed sale del código que pega el usuario | **Media arreglada.** La primera capa está puesta en el dominio y cubre a los dos consumidores. La segunda es de los adaptadores y todavía no existen. Ver abajo |

**A10 en detalle, con lo que se corrigió al revisarlo.** El invite code es un ticket desechable y el seed viaja pegado a él, así que un código fabricado apunta un destino elegido por otro. La primera versión de esta auditoría nombró un solo consumidor, el cliente HTTP, y **son dos**: el mismo valor entra en `HostSpec.Seeds`, `RendezvousSpec.Seeds` y `GuestSpec.Seeds`, o sea que también son los `--peers` con los que arranca el motor. El segundo es el que más pesa, porque ahí el daemon no consulta una API, intenta armar un túnel.

Lo que el diseño ya garantizaba: un ID pelado usa siempre el seed por defecto y jamás el último usado; `ParseRoom` no interpreta rutas, no acepta argumentos y no adivina; y a un seed hostil le llega el invite ID y la tarjeta cifrada, nada más, o sea que ni ve el `networkID` ni el secreto de la sala. Lo peor que consigue es no contestar, y eso cuesta la tarjeta, no la sala.

**Lo que se arregló:** el seed tiene que ser un NOMBRE. Se exige que su última etiqueta lleve al menos una letra, y eso cierra la familia entera de formas de escribir una dirección, no solo la obvia. El resolver del sistema acepta cosas que un comprobador de IP bien formada deja pasar: `127.1` es loopback, `0x7f.0.0.1` también, y antes de esto las dos entraban, igual que `169.254.169.254`, que es el endpoint de metadatos de las nubes. El costo aceptado es que quien hospede su propio seed necesita un nombre, que es gratis, y a cambio la comprobación cubre al cliente HTTP y al motor de una vez.

**Lo que falta, y es de los adaptadores.** Un nombre impecable puede resolver a `192.168.1.1`, y eso solo se ve al resolver. `domain.CheckSeedAddr` está escrita y probada para eso, con los rangos reservados, el espacio compartido donde viven las salas, y las cuatro familias de meter una IPv4 dentro de una IPv6. **Hoy no la llama nadie**, porque ninguno de los dos adaptadores existe todavía.

Cuando se escriban, dos requisitos y el segundo es el que se descubrió revisando:

1. El cliente del registro: esquema y puerto fijos, sin seguir redirecciones, topes de respuesta y de tiempo, y `CheckSeedAddr` sobre cada dirección resuelta.
2. El motor: **resolver el nombre acá, comprobarlo, y pasarle la dirección ya elegida**. Pasarle el nombre en `--peers` deja que lo resuelva él por su cuenta, y entonces la comprobación no gobierna el destino real. Es la diferencia entre validar y decidir.

Las comprobaciones van en CADA uso, no solo la primera vez: `last-room.json` guarda el código con su seed, así que volver a la última sala vuelve a hablarle y el DNS pudo cambiar entre una vez y la otra.

**Lo que ninguna lista arregla, dicho para no confundirse de defensa.** Un código puede nombrar cualquier host, un nodo público de EasyTier incluido. Una lista negra de nombres conocidos no sirve contra eso, porque quien fabrica el código elige el nombre. Lo que acota ese caso es lo que ya existe: la UI resalta el seed cuando no es el por defecto, la confirmación de la decisión 17 no se recuerda, y al seed nunca le llega el secreto de la sala. La lista de nodos públicos que sí existe está en el guardián y vigila NUESTRO código, que es otra cosa.

### El repo, y las skills que influyen en lo que se escribe

46 skills de `samber/cc-skills-golang`, en `.agents/skills` y enlazadas desde `.claude/skills`.

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

## Modelo de amenazas, resumen honesto

| Amenaza | Resultado |
|---|---|
| Miembro de la sala comprometido | Alcanza solo los puertos del juego activo en el host. 445/3389/22 cerrados siempre |
| Seed comprometido | Ve networkIDs e IPs públicas. No descifra, no se une, no alcanza servicios |
| Código de sala filtrado | El portador entra hasta que el host renueve el código. **Mitigación activa:** renovar cuesta un click y no expulsa a los presentes. El firewall sigue limitando a los puertos del juego |
| Miembro expulsado que insiste | Revocada su credencial, sale de la red en ~1 s. Vuelve solo si conserva un código vigente, y el host lo cierra renovando |
| Miembro manda basura al canal de control | Es la superficie más seria del producto, y solo existe en la máquina del host. Ver el modelo de amenazas de la decisión 23 |
| Miembro intenta hacerse pasar por el host EN LA SALA | No puede. Los invitados marcan hacia una dirección conocida y no aceptan conexiones entrantes |
| Alguien con el código ocupa la dirección del host EN EL VESTÍBULO | **Puede, y hoy no hay defensa criptográfica.** Ahí las direcciones son autoasignadas y verificar exige la llave larga de la decisión 25, que sigue diferida. La víctima entra a la red del impostor en vez de a la del host, o sea que el daño es no entrar a la sala que quería. Renovar el código lo desarma, porque el vestíbulo deriva del invite ID |
| Miembro que deja de recibir para trabar al host | Toda escritura del canal lleva plazo, y vencido se le cierra la conexión |
| Malware local como el usuario | Usa la API igual que el usuario: unirse a salas, aplicar perfiles del catálogo. No puede abrir puertos arbitrarios. Puede leer `room.json` y con eso entrar a la sala |
| Malware local con admin | Fuera del alcance: con admin ya controla la máquina completa |
