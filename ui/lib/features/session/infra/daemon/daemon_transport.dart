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
/// O sea que hay que llamar a `CreateFileW`, `ReadFile` y `WriteFile` a mano.
/// La pregunta es DESDE DÓNDE, y esa tuvo dos respuestas.
///
/// **Se probó desde `dart:ffi` y salió mal, con medición.** El registro de
/// eventos de Windows del 2026-08-09 tiene 49 caídas de `kanpachiui.exe` en 32
/// horas, nueve con `STATUS_HEAP_CORRUPTION`. Una E/S superpuesta le presta al
/// kernel el `OVERLAPPED` y el buffer hasta que la operación termina de verdad,
/// y en Dart esos punteros eran `calloc` sueltos cuya vida dependía de que un
/// isolate llegara a su `finally` — cosa que `Isolate.kill` no garantiza,
/// porque no interrumpe una llamada nativa.
///
/// Hoy vive en el runner de C++ y llega por `MethodChannel`, con un hilo dueño
/// del handle que cancela y espera su lectura antes de soltar nada. Ver
/// `pipe/windows_pipe_transport.dart` y `windows/runner/kanpachi_pipe.cpp`.
///
/// **La lectura no puede bloquear el hilo de la UI.** `ReadFile` sobre un pipe
/// se queda esperando, así que va en un hilo aparte y con E/S superpuesta. Un
/// cliente que congele la ventana mientras espera al daemon es peor que no
/// tener cliente.
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
