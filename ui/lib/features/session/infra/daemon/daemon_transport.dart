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
  /// Los bytes que llegan del daemon. Un solo suscriptor.
  Stream<List<int>> get incoming;

  /// Manda bytes. Completa cuando salieron, no cuando alguien los leyó.
  Future<void> send(List<int> bytes);

  /// Cierra. Es IDEMPOTENTE: lo llama el camino de error, que puede correr
  /// antes de que se haya abierto nada.
  Future<void> close();
}

/// El transporte falló y la conversación no puede seguir.
///
/// Va aparte de un error de la API: aquel es el daemon diciendo que no, este es
/// no haber podido preguntar. Para el usuario acaban en el mismo sitio, y para
/// quien lee el log no.
class DaemonUnreachable implements Exception {
  const DaemonUnreachable(this.reason);

  final String reason;

  @override
  String toString() => 'DaemonUnreachable: $reason';
}
