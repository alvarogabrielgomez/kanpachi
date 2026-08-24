# Los relojes

Kanpachi hace sonar nueve bucles periódicos repartidos en cuatro procesos. Este documento dice de quién es cada uno, qué toca al sonar, y cuál pide el candado de la sesión.

Los valores y su porqué viven en `core/timing/timing.go`, que declara cada plazo con su razón al lado. Acá está lo que ese archivo no puede decir, porque no lo sabe: quién los hace sonar.

**Un reloj no es un plazo.** De la cincuentena de constantes de `core/timing`, tres hacen tickear algo en el daemon. El resto son cortes, esperas, reintentos y vencimientos, que se evalúan cuando alguien pasa por ahí. `internal/arch/corte_test.go` vigila solo una docena: las que, movidas desde fuera, dejarían a un agente externo conectado para siempre o martillando una máquina ajena.

## El daemon

Tres relojes en dos goroutines. Los dos primeros comparten el despachador de un solo hilo, así que **se estorban entre ellos**.

| Reloj | Dueño | Cada | Lee | Escribe | `Session.mu` |
|---|---|---|---|---|---|
| `SupervisorBeat` | `Supervisor`, despachador | 15 s | El reloj y el estado de la sala. Con sala de host, la tabla de sockets de la máquina | Plazos vencidos, salud del juego, presencia del host. Cada 8 latidos, los ajustes del adaptador | Sí, dentro de `Session.Tick` |
| `SupervisorSweep` | `Supervisor`, el mismo despachador | 60 s | El firewall, la cuarentena, el IGD del router, el `/healthz` del registro | `state.Alerts`, `SeedDown`, `state.Quarantine` | Sí, y solo al publicar. Las consultas corren sin candado |
| `MeshBeat` | `VigiaDeMalla`, goroutine propia | 1 s | `Engine.Peers` | El log, y nada más | **No.** Ahí está su razón de existir |

Detalles que la tabla no cabe:

- **El latido cuelga de sí mismo otras cuatro cadencias**, todas dentro de `enforceDeadlinesLocked`. Con sala de host: anunciar cada `AnnounceInterval`, republicar la tarjeta al registro cada `RepublishInterval`, renovar credenciales contra el motor cada `RenewInterval`, y avisar a los rezagados en cada latido. Medir la salud del juego corre en cada latido y cuesta dos llamadas al sistema.
- **`AdapterReapplyEvery` no es un reloj, es un contador.** Ocho latidos, o sea dos minutos, contados en latidos para que sigan al latido si alguien lo cambia.
- **El barrido va aparte del latido a propósito.** Hace siempre tres llamadas al sistema, una al IGD del router, que en la mayoría de los routers termina en timeout. Compartiendo reloj, el corte automático latiría al ritmo del router más lento de la casa. Lo defiende `TestElBarridoVaAparteDelLatido`.
- **El vigía anota el cambio, no el tick.** Sin cambios en la malla no escribe una línea. Su firma excluye el RTT, que se mueve solo.

## La ventana

Solo en Windows. Corre en su propio proceso, así que ninguno de los dos toca el candado de la sesión: le hablan al daemon por el pipe, y `Status` lee la copia publicada.

| Reloj | Dueño | Cada | Lee | Escribe | `Session.mu` |
|---|---|---|---|---|---|
| `kSessionBeat` | `SessionCubit` | 2 s | La sala, la salud, el enlace entrante. Sin sala, también la guardada y la última | `SessionState` | No aplica |
| `kProgressBeat` | `SessionCubit`, solo mientras hay operación larga | 400 ms | El diario de progreso | `SessionState.progress` | No aplica |

Los 400 ms existen porque `CreateRoom` tiene el candado tomado el minuto entero que dura, y el diario es lo único que se puede mirar dentro de esa espera.

Un latido no se solapa con el anterior: un pestillo se salta el tick si el previo sigue en vuelo. Sin él, con una llamada lenta, la cola no bajaba y la app pintaba «sin servicio» con el daemon vivo.

## El CLI

| Reloj | Dueño | Cada | Lee | Escribe | `Session.mu` |
|---|---|---|---|---|---|
| `LiveViewRefresh` | `kanpachi watch` | 1 s | `MethodStatus` | La terminal | No aplica |

Es la única vista viva en Linux. `internal/roomprobe` usa el mismo plazo para lo suyo, y ese solo compila en Windows.

## El registro, en el seed

| Reloj | Dueño | Cada | Lee | Escribe | `Session.mu` |
|---|---|---|---|---|---|
| Sondeo del contador | `Counter` | 3 s, por `--poll` | `easytier-cli peer list-foreign` contra el portal de loopback | El mapa de conteos en memoria que sirve la página | No aplica |
| `RegistrySweep` | `barrer` | 10 min | El almacén de salas | Descarta las de fijado vencido | No aplica |
| Watchdog de systemd | `Latido` | La mitad de `WatchdogSec` | Su propio `/healthz`, por el puerto | `WATCHDOG=1` al supervisor de systemd | No aplica |

El contador cachea a propósito: una avalancha de visitas a la página no se traduce en una avalancha de procesos hijos. El precio es que el conteo va un sondeo por detrás.

El watchdog late a la mitad del plazo para dejar margen a una pausa de GC. Y pregunta por el puerto en vez de mirar una variable en memoria, porque un servidor atascado contesta que sí a la variable.

## Los relojes del motor

No son de Kanpachi. Miden lo que después el daemon publica, así que su cadencia fija el techo de frescura de cualquier número que venga del motor.

| Reloj | Cada |
|---|---|
| Ping y pong por conexión | 1 s, con retroceso hasta 32 s en un enlace quieto |
| Reporte al peer-center | Solo al cambiar el conjunto de peers, o cada 60 s |
| Bajada del mapa global | 15 s |
| Reconstrucción de la tabla de rutas | Despierta al menos cada 1 s |

## Dónde se cuelga trabajo nuevo

El despachador del supervisor tiene un solo hilo, y el latido de 15 s es lo que hace vencer el corte de veinte minutos por ausencia del host. Trabajo que tarde segundos de red no va dentro.

El patrón para eso ya está escrito tres veces en `supervisor.go`, en la ronda del canario, el reingreso y la vuelta: una bandera de un solo vuelo que solo toca el despachador, la goroutine lanzada con su propio `recover`, y la bandera liberada por el canal de trabajo con un `select` sobre `ctx.Done()`.

Cada operación larga lleva **su propia** bandera. Compartir una haría que el día que sus condiciones se solapen, una apague a la otra en silencio.
