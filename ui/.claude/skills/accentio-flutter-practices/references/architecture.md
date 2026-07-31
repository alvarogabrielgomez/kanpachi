# Arquitectura — Clean Dart por feature + Cubit

## 1. Layout de carpetas

```
lib/
├── core/                       # cross-cutting, sin dueño de feature
│   ├── constants/              # constants.dart: TODAS las versiones de schema/wire
│   ├── design_system/          # tokens + átomos + moléculas (ver design_system.md)
│   ├── failures/               # Failure hierarchy + AppErrorCode + copy
│   ├── database/               # ObjectBoxDatabase: store + accessors de cajas
│   ├── l10n/                   # resolución de strings sin BuildContext
│   ├── router/                 # app_router.dart (go_router) + señales de redirect
│   └── shared/                 # utilidades genuinamente compartidas
│
├── features/<feature>/
│   ├── domain/
│   │   ├── entities/           # clases de negocio (+ anotaciones de DB local)
│   │   ├── repositories/       # contratos: métodos que devuelven Stream<DTO>
│   │   ├── services/           # lógica pura, static, clock inyectado
│   │   └── use_cases/          # interfaz + *_impl.dart (DTO→Entity + reglas)
│   ├── data/
│   │   ├── dto/                # *_request_dto.dart / *_response_dto.dart
│   │   └── repositories/       # *_remote_repository.dart (with Api)
│   ├── infra/                  # datasources locales, engines, storage, clients
│   └── presentation/
│       ├── cubit/              # *_cubit.dart + *_state.dart
│       ├── pages/
│       └── widgets/            # widgets exclusivos de la feature
│
├── integrations/               # el mundo exterior
│   ├── environments/           # RemoteService + EnvironmentConfig
│   ├── helpers/                # http_helper.dart (HttpHelper + handle())
│   └── packages/               # dio/ · get_it/ · alice/ — un dir por paquete
│
└── ioc/                        # injector.dart (abstracción) + ioc_manager.dart
```

**Regla de dependencias:** `presentation → domain ← data`. `domain` no conoce
Dio ni el shape concreto de los DTOs más allá de las firmas. `integrations` y
`core` son cross-cutting y no pueden importar features.

## 2. Persistencia local: ObjectBox, sin DTOs de persistencia

**La DB local de la casa es [ObjectBox](https://pub.dev/packages/objectbox)**
para todo lo que persista en el device — Android, iOS, Windows, macOS, Linux.
Es NoSQL orientado a objetos: guarda la clase Dart directo, sin ORM ni SQL de
por medio, y con eso desaparece la razón de existir de la capa de mapeo.

Clean Dart tradicional pone `infra/models` con DTOs de persistencia. **Se omite
esa capa**: la entidad de dominio lleva sus propias anotaciones (`@Entity`,
`@Id`) y el datasource local devuelve `domain/entities/` sin mapear. Elimina un
mapeo entero y deja las entities como la única representación de la data.

**No aplica al wire**: los DTOs de red (`data/dto/`) siguen existiendo, porque
el shape del backend no es el shape del dominio y no debe contagiarse.

Ver [templates/database/objectbox_database.dart](../templates/database/objectbox_database.dart)
para el store, los accessors de cajas y las reglas de schema. Las que más
muerden:

- **`openStore()` una sola vez**, en `main()`, antes del IoC, y registrado como
  singleton. Dos stores sobre el mismo directorio = `Cannot open store`.
- **El `@Id int` no es la clave de negocio.** Es local al store y se reasigna en
  un restore. La referencia estable entre dispositivos y payloads es un
  `clientId` (UUID) que asigna la app.
- **Los enums se persisten por índice**: reordenar los valores reinterpreta la
  data ya escrita. Agregar al final, y exponer el enum por getter/setter sobre
  la columna `int`.
- **`objectbox-model.json` se commitea y no se edita a mano** — tiene los UIDs
  de cada campo; lo escribe `build_runner`. Borrarlo hace que el generador crea
  que todo es nuevo y la data existente queda ilegible.
- **Escrituras compuestas dentro de `store.runInTransaction(TxMode.write, …)`**:
  un fallo a mitad deja la data vieja intacta en vez de un estado mixto.
- **Las queries viven en un repositorio de `infra/`**; el cálculo sobre los
  resultados va en una función pura de `domain/` que recibe la lista ya
  cargada — así se testea sin abrir un store.

Alternativas (Isar, Drift, sqflite) sólo si hay una razón concreta y explícita
para el proyecto; el default es ObjectBox.

## 3. DI — `Injector` abstracto sobre `get_it`

Toda resolución pasa por `Injector.instance`. La abstracción existe para poder
cambiar de contenedor tocando un archivo. Ver
[templates/ioc/](../templates/ioc/).

La implementación **desregistra antes de registrar** — sin eso, hot reload y
registros repetidos en tests explotan.

`IocManager.register()` corre una vez en `main()`, después de inicializar la DB
local. **El orden importa**: lo que depende de otro va después (el
`DioHttpHelper` necesita el `AuthInterceptor`, así que `_registerHttp` corre
tras `_registerAuth`).

Patrón por feature, sin excepciones:

```
repository  → registerLazySingleton   (sin estado por-request)
use cases   → registerLazySingleton
cubit       → registerFactory         (uno por pantalla)
```

Un servicio de larga vida (engine, storage, coordinator) es `lazySingleton`.
Un `registerSingleton` eager sólo para lo que ya está construido (la DB).

## 4. El flujo de datos, punta a punta

```
Repository (data)  →  Stream<DTO>  →  UseCase (DTO→Entity)  →  Cubit  →  UI
        │                                                        │
   .handle<T>() lanza Failure tipado ──────────────────────────┘  catch en onError
```

- **Repo**: `with Api`, un `static const` por path, `yield* api.get(...).handle(mapper:)`.
- **Use case**: drena con `.first` (o `.drain()`), transforma DTO→Entity, aplica
  reglas. **No atrapa Failures.**
- **Cubit**: se suscribe, `onError` recibe el `Failure` tipado y lo mapea a copy
  **por el código numérico**, nunca por `message` (que cambia y puede venir en
  otro idioma).

Ejemplos completos en [templates/feature/](../templates/feature/).

## 5. Engines — concerns de larga vida

Un concern especializado y lifecycle-aware (sync, notificaciones, timers,
scheduler) vive en su propio **engine**: una clase autocontenida, inicializada
independientemente. **No se centralizan** en un bootstrap compartido: cada uno
tiene que ser confiable solo. Nunca refactorizar engines que funcionan hacia un
core común.

**Regla de oro: cómputo puro separado de I/O.**

- Lo determinístico (mapear, calcular, decidir) es `static` puro, **sin I/O y
  sin clock ambiente** (el clock se inyecta) → 100% unit-testeable sin DB ni
  plugins.
- El engine de I/O hace los side effects y orquesta.

Forma canónica del engine de I/O:

```dart
class XEngine with WidgetsBindingObserver {
  bool _initialized = false;

  Future<void> initialize() async {
    if (_initialized) return;          // idempotente
    _initialized = true;
    WidgetsBinding.instance.addObserver(this);
    // ...
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed) _onResume();   // con debounce propio
  }
}
```

Registrado como `lazySingleton` en `ioc_manager.dart`, resuelto e inicializado
**una vez** en `main()`, después del engine previo.

**Trampa de los plugins process-singleton** (p. ej.
`FlutterLocalNotificationsPlugin`): permiten un solo `initialize()` global con
los callbacks de tap. Ese `initialize()` tiene **un dueño único**; los demás
engines crean sus canales y agendan, pero **nunca** re-llaman `initialize()` —
clobbearían los callbacks del dueño. Si un engine no-dueño necesita reaccionar
a un tap, el dueño le expone un `registerForeignTapHandler(id, onTap)`.

**Puertos de dominio para no invertir la dependencia:** si un engine de `infra`
necesita disparar algo que hoy implementa un cubit, define una interfaz en
`domain/` que el cubit implementa. `infra` nunca importa `presentation`.

## 6. Routing con `go_router` + señales

`core/router/app_router.dart` define las rutas y **un `redirect` central**. No
hay `AuthGate` widget: el callback consulta el estado de boot/auth y las señales
pendientes, y decide la ruta.

`refreshListenable` adapta N streams (auth, boot, conflictos, locks) en un
`ChangeNotifier` que re-dispara el callback. **El orden de los guards importa** y
se documenta arriba del callback.

**Patrón "router signal"** — singleton mínimo que fuerza una pantalla:

```dart
class XNotifier {
  XNotifier._();
  static final instance = XNotifier._();
  final _controller = StreamController<void>.broadcast();
  Stream<void> get changes => _controller.stream;

  bool get hasPending => _pending != null;
  void notify(Object payload) { _pending = payload; _controller.add(null); }
  void clear() { _pending = null; _controller.add(null); }   // ← el emit en clear NO es opcional
}
```

**Invariante:** `clear()` DEBE emitir en el stream que el router observa. Si no,
la salida de la pantalla forzada depende de que el caller navegue explícito, y
el día que uno no lo haga el usuario queda trabado ahí.

No hacer una clase base para las señales: los payloads difieren y la jerarquía
forzaría el mínimo común. Se copia el patrón entero, incluido el emit en clear.

Rutas anidadas pueden seguir usando `Navigator.push` imperativo — conviven. Ojo:
una página empujada imperativamente **tapa** la que el redirect puso debajo; si
una señal tiene que ganar siempre, agregar un listener en `main()` que haga
`popUntil((r) => r.isFirst)` cuando la señal se enciende.

## 7. Versionado centralizado

Todas las versiones de schema/wire viven en `core/constants/constants.dart`.
Nunca hardcodear `'sync.foo.v2'` por el código.

```dart
const int kShareSchemaVersion    = 1;   // formato de import/export
const int kSnapshotSchemaVersion = 1;   // payload de backup/restore
```

**Cuándo bumpear:** cada vez que cambie el shape de un payload. Cambiar el shape
sin bumpear no rompe nada el mismo día — rompe el día del restore, con dos
formatos incompatibles diciendo ser la misma versión.

**Migraciones one-shot: el default es que NO haya.** Mientras el producto siga
pre-lanzamiento, la respuesta a "esta data quedó mal" es resetear la versión y
limpiar, no escribir un backfill. Un backfill sólo se justifica cuando hay data
que no se puede perder, y entonces lleva ficha con su condición de borrado.

## 8. Tests

```bash
flutter test                       # suite completa
flutter test test/features/<x>     # una feature
flutter test --coverage
```

- Mocks con `mocktail`. Fallbacks de tipos custom en `setUpAll`:
  `registerFallbackValue(MyRequestDto(...))`.
- Para drenar microtasks de un cubit: listener manual a `cubit.stream` +
  `await Future.delayed(Duration.zero)`.
- **Nada de fixtures acoplados a valores que el producto cambia por diseño**
  (una versión de asset, un contador). Derivar el valor del artefacto y
  verificar la RELACIÓN, no el número de hoy.
- **Candados transversales** (los que barren el repo con regex o comparan dos
  fuentes): incluir un "canario" que exija un mínimo de casos encontrados. Si el
  extractor se rompe, el barrido se queda en cero y el grupo entero pasa por no
  mirar nada — verde por vacío.
- Sin el repo hermano al lado, un test de lockstep se declara `skip:` **con
  motivo**; nunca pasa en silencio.

## 9. Comandos

```bash
flutter pub get
dart run build_runner build --delete-conflicting-outputs   # .g.dart / .tailor.dart / .freezed.dart
flutter analyze
flutter test
```

En Windows `build_runner` tarda minutos y su progreso es CR-only: **parece
colgado y no lo está**. Nunca lanzar dos en paralelo, y matarlo a mitad borra
los `.g.dart` generados.
