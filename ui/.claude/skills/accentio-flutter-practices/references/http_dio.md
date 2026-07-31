# Stack HTTP — Dio + Failure tipado + Alice

Modelo mental en una frase: **cada repositorio es un objeto delgado que obtiene
un `HttpHelper` apuntado a un microservicio, llama un verbo, y encadena
`.handle<T>(mapper:)` que convierte el `Future<HttpResponse>` en un `Stream<T>`
que emite una vez en éxito o lanza un `Failure` tipado en error.**

Código completo en [templates/integrations/](../templates/integrations/),
[templates/core/](../templates/core/) y
[templates/auth/](../templates/auth/).

## 1. Las piezas

| Pieza | Archivo | Rol |
|---|---|---|
| `RemoteService` | `integrations/environments/remote_service.dart` | enum de microservicios |
| `EnvironmentConfig` | `integrations/environments/environment_config.dart` | URLs (`String.fromEnvironment`) + timeout |
| `DioHttpHelper` | `integrations/packages/dio/dio_http_helper.dart` | el **único** `Dio`; dueño de los interceptors |
| mixin `Api` | `integrations/packages/dio/api.dart` | un getter por servicio, resuelve el helper |
| `HttpHelper` + `handle()` | `integrations/helpers/http_helper.dart` | verbos + `_checkFailures` (tabla única status→Failure) |
| `AuthInterceptor` | `features/auth/infra/auth_interceptor.dart` | bearer + refresh single-flight |
| `Failure` + `AuthCoded` | `core/failures/failure.dart` | jerarquía de errores tipados |
| `AppErrorCode` | `core/failures/app_error_code.dart` | espejo del enum del backend |
| `AliceDebugInspector` | `integrations/packages/alice/` | inspector HTTP debug-only |

## 2. El truco crítico del `validateStatus`

```dart
validateStatus: (s) => s != null && s != 401 && s < 500,
```

Acepta como "éxito" todo `< 500` **excepto 401**. Es deliberado:

- **401** → excluido → Dio lanza `DioException` → corre `AuthInterceptor.onError`
  → puede hacer el refresh-and-retry silencioso.
- **Cualquier otro 4xx** (400/403/404/409/426) → pasa por el success path →
  llega al body parser de `_checkFailures`, que lo mapea a `Failure` tipado con
  su código de app.

Si el predicado incluyera 401, el interceptor **nunca** correría `onError`: el
usuario sería deslogueado al primer access token vencido en vez de recuperarse
transparentemente. No tocar sin entender esto.

## 3. El baile del refresh

El 401 está sobrecargado server-side: "access token vencido" (refresh arregla),
"sin token" (no ayuda), "usuario borrado" (choca con la misma pared), "refresh
reusado" (terminal). **Sólo se intenta el refresh cuando el backend dice
explícitamente que el problema es el access token**, vía el código numérico del
body:

```dart
if (authCode == null || !AppErrorCode.refreshable.contains(authCode)) {
  return handler.next(err);        // no refrescable → sube el 401 crudo
}
```

Cuatro puntos no-obvios que hay que preservar al portarlo:

1. **Single-flight**: `_inflightRefresh ??= _doRefresh().whenComplete(...)`. Un
   burst de 401 paralelos dispara un solo refresh; el resto espera el mismo
   `Future`.
2. **Distinguir refresh-muerto de refresh-transiente.** `UnauthorizedFailure`
   (el server dijo 401 al refrescar) → `storage.clear()` + login. Cualquier otra
   excepción (timeout, 5xx, DNS) → **preservar la sesión** y subir el 401
   original. Sin este guard, un tick de red flaky desloguea a un usuario con
   credenciales válidas.
3. **Retry con `Dio()` fresco, sin este interceptor** — anti-recursión.
4. **`_doRefresh` resuelve el repo lazy** (`Injector.instance.resolve<...>()`)
   para romper la dependencia circular de registro: el interceptor se registra
   antes que el repo.

**Rutas públicas** (login, refresh, register, forgot/reset password, redeem de
invites, branding pre-login) bypassean bearer y refresh. `_isPublic` limpia
query strings y fragmentos antes de matchear, y soporta patrones dinámicos
(`/tenants/<id>/branding`).

## 4. `_checkFailures` — la tabla única

Único lugar que traduce un response no-2xx a `Failure`. Lo comparten el success
path de `handle()` y el `_rethrowAsFailure` de los verbos, así el código opaco
se adjunta idéntico venga por donde venga.

Orden: **primero las subclases tipadas con metadata** (por `code` de app),
después el switch por status.

```dart
if (appCode == AppErrorCode.someDomainConflict && data is Map<String, dynamic>) {
  throw SomeDomainFailure.fromData(data);      // lleva campos estructurados
}
switch (statusCode) {
  case 400: throw BadRequestFailure(message, appCode);
  case 401: throw UnauthorizedFailure(appCode);
  case 403: throw ForbiddenFailure(appCode);
  case 404: throw NotFoundFailure();
  case 409: throw ConflictFailure(message, appCode);
  default:  throw CommonFailure('HTTP $statusCode');
}
```

**Consecuencia para los callers:** si el `await` de un verbo retornó, fue 2xx.
Un caller raw (sin `.handle()`) **nunca** debe chequear `isSuccess()` para
detectar errores — el choke point ya los convirtió en throw. Antes de tener ese
choke point, un 409 con body raro se leía como "éxito raro" y el error moría en
un `CommonFailure` genérico.

Si un código tiene que encender una señal global (una pantalla de conflicto, un
lock), **encenderla dentro de `_checkFailures`**, al construir el failure: así
un caller que se trague el throw no puede ocultar el evento.

## 5. `handle<T>()` vs `handleVoid()`

```dart
yield* api.post(_login, data: req.toJson())
    .handle<LoginResponseDto>(mapper: (r) => LoginResponseDto.fromJson(r as Map<String, dynamic>));

yield* api.post(_logout, data: req.toJson()).handleVoid();   // 204 sin body
```

- `handle()` con éxito y `data == null` lanza `CommonFailure('Empty response
  body')` a propósito: es la señal de que ese endpoint quería `handleVoid()`.
- Si el `mapper` explota parseando, sale `JsonParseFailure(error:)`, no el error
  crudo del parser.

## 6. Contrato del wire

El backend emite, en **error**:

```json
{
  "statusCode": 409,
  "code": 4001,
  "message": "Human readable",
  "data": { "…": "metadata estructurada, opcional" },
  "timestamp": "…",
  "path": "/some/endpoint"
}
```

- `code` (int) es lo que el cliente switchea. **El `message` no se parsea nunca**:
  cambia, y puede venir en otro idioma.
- `data` alimenta las subclases tipadas, para que la UI no re-parsee el body.
- En **éxito** no hay envelope: el body es directo el payload que el mapper lee.

Bandas de código recomendadas: `1xxx` auth · `2xxx` permisos · `3xxx` validación
· `4xxx` sync/dominio · `9xxx` genérico.

**Riesgo de drift:** si cliente y backend se desincronizan en la numeración, un
código refresh-eligible se demota en silencio a 401 genérico. No hay check
runtime que lo atrape → mantener el enum en lockstep, y si el backend está en el
mismo workspace, escribir un test que compare los dos archivos.

## 7. Espejos back↔front: todos llevan candado

Las dos suites son ciegas a la costura: `flutter test` y `npx jest` pueden estar
los dos en verde con el espejo roto, y el fallo aparece recién en runtime. Con
`forbidNonWhitelisted: true` en el `ValidationPipe` de Nest, además, **una clave
que el DTO no declare tumba el request entero con 400**.

Forma del candado (ver el patrón, no el archivo):

- El **nombre de la clase es el vínculo**: cada clase Dart que toca el wire se
  busca por nombre contra todo `src/**` del backend; si existe allá, se comparan
  los sets de claves. Un espejo nuevo entra al candado por existir — no hay
  inventario que mantener.
- Sólo entran las clases que **tocan el wire** (declaran `fromJson`/`toJson`,
  propio o generado). El criterio se deriva, no es una lista de excepciones.
- Aplanar la herencia de los dos lados. Un union type de TS = una jerarquía
  sellada en Dart.
- **Sin allowlist de diferencias**: un campo que sobra de un lado es un bug o es
  payload muerto.
- Canario de cantidad mínima de pares + probar que muerde.

## 8. Alice — inspector HTTP debug-only

`alice_lightweight`, enganchado al **mismo** `Dio` que usa la app, así todo
request aparece en el inspector.

```dart
if (kDebugMode) {
  await AliceDebugInspector.attach(
    dio: Injector.instance.resolve<DioHttpHelper>().dio,
    navigatorKey: rootNavigatorKey,
    registerTapHandler: timerService.registerForeignTapHandler,   // opcional
  );
}
```

- `attach()` retorna `null` fuera de debug y es idempotente; el doble guard
  (`kDebugMode` afuera + adentro) ahorra los lookups en release y deja el código
  tree-shakeable.
- **Dos interceptors, en orden**: el de Alice primero (captura todo), después el
  propio que cuenta requests.
- `alice_lightweight` v3 sacó la notificación built-in. Se rehace con
  `flutter_local_notifications`: una notificación sticky de prioridad baja con
  el contador de llamadas; el tap abre el inspector.
- **No llamar `initialize()` del plugin de notificaciones acá.** Es
  process-singleton y clobbearía los callbacks del dueño; se pide un
  `registerTapHandler` al dueño (ver "engines" en architecture.md). El canal se
  crea implícito en el primer `show()`.
- `DioHttpHelper.dio` se expone **sólo** para este tooling. Producción usa
  `withBaseUrl(service)`.

## 9. Invariantes que no se pueden romper

- `validateStatus` excluye **sólo** 401.
- El refresh corre **sólo** para códigos en `AppErrorCode.refreshable`.
- El retry post-refresh usa un `Dio()` **sin** el `AuthInterceptor`.
- `_checkFailures` es el único traductor status→Failure.
- Repos devuelven `Stream<DTO>`; use cases transforman a Entity; cubits atrapan
  el `Failure` en `onError`. Nunca se swallowean en data/domain.
- Alice sólo en `kDebugMode`, sobre el Dio compartido, sin re-inicializar
  plugins ajenos.

## 10. Seguridad — el wire nunca lleva la contraseña en claro

Aunque haya HTTPS: cualquier proxy, TLS terminator, túnel de dev o log de
request del lado server vería el plaintext y lo podría replicar. El cliente
hashea antes de enviar y el server aplica su propio hash encima (double hash).

```dart
hex = sha256(utf8(password) + utf8(email.toLowerCase().trim()))
```

El email canonicalizado hace de salt determinístico per-user, sin round-trip
extra de fetch-salt. Output: 64 hex lowercase, que el server valida con
`/^[a-f0-9]{64}$/` antes de su bcrypt.

- El algoritmo vive en **un** archivo (`core/security/password_hasher.dart`) y
  es espejo exacto del backend.
- Se hashea en el **use case**, antes de construir el DTO: cubits y páginas
  siguen pasando plaintext y el hashing es invisible para presentation.
- Los campos del DTO se llaman `passwordHash` / `newPasswordHash`, no `password`.
- El min-length se enforcea en el cliente (el server sólo ve 64 hex).
- **Un solo formato, sin camino de migración.** Un endpoint que contesta "tenés
  que migrar la contraseña" antes de validar credenciales es un oráculo de
  existencia de cuentas: cualquiera prueba un email con una password inventada y
  aprende si existe. Si hay que rotar el formato, la recuperación por
  "olvidé mi contraseña" ya escribe el formato nuevo.
