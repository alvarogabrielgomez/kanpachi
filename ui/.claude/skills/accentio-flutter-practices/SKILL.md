---
name: accentio-flutter-practices
description: Accentio Studios' Flutter house style — Clean Dart por feature + Cubit, DI con Injector/get_it, ObjectBox para la DB local (Android/iOS/Windows), stack HTTP Dio (HttpHelper + handle() + Failure tipado + AuthInterceptor con refresh single-flight), Alice como inspector debug-only, y design system modular token-driven (ThemeExtension + theme_tailor, atomic design) con candados de test. Usar al arrancar un proyecto Flutter nuevo, al portar estas técnicas a un repo existente, o al revisar que una feature/pantalla siga la arquitectura de la casa.
---

# Accentio Flutter practices

Cómo Accentio construye apps Flutter. Replicable en cualquier proyecto: nada acá
depende de un dominio concreto.

Seis decisiones que definen el estilo:

1. **Clean Dart por feature + Cubit**, sin capa de DTOs de persistencia: la DB
   local guarda entidades de dominio directo.
2. **La DB local es [ObjectBox](https://pub.dev/packages/objectbox)** — todo lo
   que persista en el device (Android, iOS, Windows, macOS, Linux) va ahí.
3. **Todo se resuelve por un `Injector`** propio (abstracción sobre `get_it`),
   registrado en un `IocManager` central con orden explícito.
4. **La red es un solo `Dio`** detrás de `HttpHelper`; los repos devuelven
   `Stream<DTO>` vía `.handle<T>(mapper:)`, y **todo** no-2xx sale como
   `Failure` tipado con el código numérico del backend.
5. **El design system es una capa propia** (`core/design_system/`), token-driven,
   con un componente por arquetipo y candados de test que prohíben salirse.
6. **Cómputo puro separado de I/O.** Lo determinístico es `static` puro con el
   clock inyectado; el I/O vive en engines autocontenidos.

## Cuándo usar cada referencia

| Querés… | Leé |
|---|---|
| Esqueleto, carpetas, DI, ObjectBox, engines, tests | [references/architecture.md](references/architecture.md) |
| Red: Dio, interceptors, failures, refresh, Alice | [references/http_dio.md](references/http_dio.md) |
| Tokens, atomic design, organización del DS, candados | [references/design_system.md](references/design_system.md) |
| Código copy-paste listo para el día 1 | [templates/](templates/) |

Los templates, por carpeta:

| Carpeta | Qué trae |
|---|---|
| `templates/ioc/` | `injector.dart` · `get_it_injector.dart` · `ioc_manager.dart` |
| `templates/integrations/` | `environment_config.dart` · `dio_http_helper.dart` · `http_helper.dart` (verbos + `handle()` + tabla de failures) · `api.dart` · `alice_debug_inspector.dart` |
| `templates/core/` | `failure.dart` · `app_error_code.dart` |
| `templates/auth/` | `auth_interceptor.dart` (bearer + refresh single-flight) |
| `templates/database/` | `objectbox_database.dart` (store, cajas, reglas de schema) |
| `templates/design_system/` | tokens (color/typography/spacing) · `context_ext.dart` · `app_theme.dart` · `app_button.dart` · `app_card.dart` · `responsive.dart` · barrel |
| `templates/tests/` | `ds_purity_test.dart` · `import_purity_test.dart` |
| `templates/feature/` | una feature completa de punta a punta (entity → dto → repo → use case → cubit) |

Los `templates/` son **código real, no pseudocódigo**: nombres de identificador,
firmas y comentarios están pensados para pegarse tal cual y renombrar sólo el
prefijo del proyecto. Los nombres importan — el resto del stack los asume.

## Orden de arranque de un proyecto nuevo

Cada paso depende del anterior. No saltear.

1. **Esqueleto + DI** — `lib/{core,features,integrations,ioc}`,
   `injector.dart` + `get_it_injector.dart` + `ioc_manager.dart`.
   Deps: `get_it`, `flutter_bloc`.
   Si la app persiste algo en el device: `ObjectBoxDatabase.create()` en
   `main()` **antes** del IoC, y registrarla como singleton.
2. **Entornos** — `remote_service.dart` (enum de microservicios) +
   `environment_config.dart` (URLs por `String.fromEnvironment` + timeout).
3. **Failures + error codes** — `failure.dart` (jerarquía + mixin `AuthCoded`) y
   `app_error_code.dart` espejando el backend. Definir el set `refreshable`.
4. **HTTP** — `http_helper.dart` (verbos + `handle()` + `_checkFailures`) y
   `dio_http_helper.dart`. **Preservar el `validateStatus` que excluye 401.**
5. **Auth interceptor** — bearer + refresh single-flight + retry con Dio fresco.
6. **Alice** — inspector debug-only enganchado al MISMO Dio. `kDebugMode` guard.
7. **Design system** — tokens (color por `ThemeExtension`+`theme_tailor`;
   spacing/motion/breakpoints `static const`), `app_theme.dart`, barrel, y los
   primeros átomos (`AppButton`, `AppCard`, `AppField`).
8. **Candados** — `ds_purity_test.dart` + `import_purity_test.dart` desde el
   día 1, cuando el allowlist puede estar vacío. Después ya es tarde.
9. **Primera feature** — `domain/ data/ infra/ presentation/` completo como
   plantilla de las que siguen (ver `templates/feature/`).

## Reglas duras (no negociables)

- **Nunca métodos que retornan Widget** (`Widget _buildFoo(context)`). Widget =
  clase (`class _Foo extends StatelessWidget`), `const` siempre que se pueda.
  Un helper method cae siempre en la rama `update()` de `Element.updateChild`:
  pierde el skip total que un `const` habilita, y el `setState` del padre
  rebuildea todo el subtree.
- **Los componentes dependen SÓLO de tokens.** Ni un `Color(0x…)`, ni un
  `Colors.*`, ni un `fontFamily:` literal fuera de `tokens/`. Un color
  hardcodeado adentro de un componente lo saca del sistema: deja de responder al
  theme y nadie se entera hasta que alguien lo ve en pantalla.
- **ObjectBox para persistencia local**, con la entidad de dominio como fila.
  `openStore()` una sola vez; `objectbox-model.json` se commitea y no se edita a
  mano.
- **`_checkFailures` es el único traductor status→Failure.** Lo comparten el
  success path de `handle()` y el `_rethrowAsFailure` del helper.
- **Los Failure nunca se swallowean** en data/domain. Suben hasta el
  `onError` del cubit, que es el único que los mapea a copy.
- **Sin `catch` vacío.** Loguear la excepción tragada o relanzar; un comentario
  no alcanza (Sonar S108/S2486).
- **Candado sin allowlist de deuda.** Un allowlist que crece es deuda escondida:
  o se arregla el caso, o se decide caso por caso con el dueño. Cuando escribas
  un candado, **mutilá un lado y confirmá que se pone rojo** — un candado que no
  muerde es peor que ninguno, porque da confianza falsa.
- **Todo DTO espejado back↔front lleva test de lockstep** que compare el set de
  claves. Un comentario "mantener en sync" no es un candado.
- **Versionado centralizado.** Los `kXSchemaVersion` viven en un solo
  `constants.dart`; se bumpean cada vez que cambia el shape de un payload.

## Checklist de review de una feature

- [ ] `domain/` no importa Dio ni el `Store` de ObjectBox; la entidad puede
      llevar sus anotaciones, las queries viven en `infra/`.
- [ ] El repo devuelve `Stream<DTO>`; el use case transforma DTO→Entity.
- [ ] El cubit atrapa el `Failure` en `onError` y mapea por código, no por
      subtipo ni por `message`.
- [ ] Registro DI: repo y use cases `lazySingleton`, cubit `factory`.
- [ ] Cero widget-returning methods; widgets `const` donde se pueda.
- [ ] Cero color/tipografía fuera de tokens (lo verifica `ds_purity_test`).
- [ ] La lógica determinística está en una función pura testeable sin plugins,
      con el clock inyectado.
- [ ] Si toca un payload espejado con el backend: bump de versión + test de
      lockstep actualizado.
