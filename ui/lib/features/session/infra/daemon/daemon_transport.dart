import 'dart:async';

/// Por dónde viajan los bytes hasta el daemon.
///
/// # Por qué es una interfaz y no directamente el named pipe
///
/// Es la misma separación que el daemon hace del otro lado, y con las mismas
/// palabras. De `protocol.go`: *"Los mensajes son JSON-RPC delimitado por
/// líneas. El named pipe de Windows es una implementación de este contrato, no
/// el contrato"*.
///
/// Acá compra tres cosas concretas:
///
///  - El cliente y el códec se prueban enteros sobre un transporte de memoria,
///    sin pipe, sin daemon y sin Windows. Son la parte con lógica.
///  - El transporte real se puede cambiar sin tocar nada de arriba, y va a
///    hacer falta: ver la nota de abajo.
///  - El host headless de `07-futuro.md` reusa todo esto sobre un socket Unix.
///
/// # Lo que hay que saber antes de escribir el transporte de Windows
///
/// **`dart:io` no habla named pipes de Windows.** No hay `Socket` ni `File` que
/// sirva:
///
///  - El soporte de IPC multiplataforma es una petición abierta en el SDK de
///    Dart, no una función existente (dart-lang/sdk#47310).
///  - `File(r'\\.\pipe\loquesea').openSync()` funcionaba hasta Flutter 3.24.5 y
///    **está roto desde 3.27**, con `PathNotFoundException ... errno = 53`.
///    Sigue abierto y sin arreglo (flutter/flutter#163539, verificado en agosto
///    de 2026). O sea que ese camino no es que sea frágil: hoy no funciona.
///
/// Quedan dos caminos y los dos pasan por `dart:ffi`, o sea `CreateFileW`,
/// `ReadFile` y `WriteFile`. Escribirlo acá sin dependencias, o tomar
/// `dart_ipc`, que ya lo hace con E/S superpuesta. Lo segundo tiene 7 likes en
/// pub, y esta es la superficie de la API local de un daemon que corre como
/// SYSTEM: la decisión de meter esa dependencia es del dueño del proyecto, no
/// mía, y el repo ya verifica lo que consume en vez de confiarlo.
///
/// **La lectura no puede bloquear el isolate de la UI.** `ReadFile` sobre un
/// pipe se queda esperando, así que va en un isolate aparte o con E/S
/// superpuesta. Un cliente que congele la ventana mientras espera al daemon es
/// peor que no tener cliente.
abstract interface class DaemonTransport {
  /// Opens the link. Nothing else on this interface works before it returns.
  ///
  /// It exists because [incoming] cannot exist before the handle does, and
  /// because a connection is a ONE-SHOT object: the greeting, the request ids
  /// and the closed flag are all per connection and none of them can be reset.
  /// Reconnecting is throwing this away and building another one, so somebody
  /// has to own the opening, and hiding it in the constructor would make the
  /// one operation that can fail invisible.
  Future<void> connect();

  /// Los bytes que llegan del daemon. Un solo suscriptor.
  Stream<List<int>> get incoming;

  /// Manda bytes. Completa cuando salieron, no cuando alguien los leyó.
  Future<void> send(List<int> bytes);

  /// Cierra. Es IDEMPOTENTE: lo llama el camino de error, que puede correr
  /// antes de que se haya abierto nada.
  Future<void> close();
}

/// Where a failure happened, which is what decides whether retrying is safe.
///
/// This is not a taxonomy for the log. Retrying a request that may already have
/// executed is how you get two rooms, or a member kicked twice. The line runs
/// through the write: anything that failed at or before it never reached the
/// daemon, and anything after it might have.
enum DaemonUnreachableKind {
  /// The pipe could not be opened, or a method was called before the greeting.
  /// Nothing was ever sent, so retrying is free.
  notConnected,

  /// The write itself threw. The bytes did not leave.
  writeFailed,

  /// The method's budget expired. The request may still be running on the other
  /// side: `create_room` takes about a minute of real work, and a retry would
  /// build a second room while the first one finishes.
  timedOut,

  /// The stream errored or closed with requests in flight. The request may have
  /// executed and the answer is what got lost.
  linkLost,
}

/// El transporte falló y la conversación no puede seguir.
///
/// Va aparte de un error de la API: aquel es el daemon diciendo que no, este es
/// no haber podido preguntar. Para el usuario acaban en el mismo sitio, y para
/// quien lee el log no.
class DaemonUnreachable implements Exception {
  const DaemonUnreachable(
    this.reason, {
    this.kind = DaemonUnreachableKind.linkLost,
  });

  final String reason;

  /// Defaults to the kind that does NOT retry. A failure whose origin nobody
  /// bothered to state is exactly the one that should not be repeated blindly.
  final DaemonUnreachableKind kind;

  /// Whether a caller may send the same request again on a fresh connection.
  bool get safeToRetry =>
      kind == DaemonUnreachableKind.notConnected ||
      kind == DaemonUnreachableKind.writeFailed;

  @override
  String toString() => 'DaemonUnreachable(${kind.name}): $reason';
}
