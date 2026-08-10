import 'dart:async';

import 'package:flutter/services.dart';
import 'package:kanpachi_ui/core/platform/app_log.dart';
import 'package:kanpachi_ui/features/session/domain/daemon_failure.dart';
import 'package:kanpachi_ui/features/session/infra/daemon/daemon_transport.dart';
import 'package:kanpachi_ui/features/session/infra/daemon/pipe/pipe_names.dart';

/// El named pipe del daemon, hablado por el runner de C++.
///
/// # Por qué esto ya no usa `dart:ffi`
///
/// Porque lo hacía, y corrompía la memoria del proceso. Medido el 2026-08-09
/// sobre 32 horas de registro de eventos de Windows: **49 caídas de
/// `kanpachiui.exe`**, nueve de ellas con código `0xC0000374`, que es
/// `STATUS_HEAP_CORRUPTION` — el gestor de heap cazando una escritura fuera de
/// una asignación. Las demás salían en `ntdll` y en `flutter_windows`, que es lo
/// que hace la memoria corrupta: mata al siguiente que toca la zona.
///
/// La causa de fondo era estructural y no un descuido suelto: una `ReadFile`
/// superpuesta le regala al kernel el `OVERLAPPED` y el buffer hasta que la
/// operación termina de verdad, y en Dart esos dos punteros eran `calloc`
/// sueltos cuya vida dependía de que un isolate llegara a su `finally`. Un
/// isolate no da esa garantía: `Isolate.kill` no interrumpe una llamada nativa,
/// y el dueño cerraba el handle por debajo tras una gracia de tres segundos.
///
/// Ahora el dueño es un hilo de C++ que cancela y ESPERA su lectura antes de
/// que nada se destruya. Ver `windows/runner/kanpachi_pipe.cpp`.
///
/// # Lo que sigue siendo verdad
///
/// **Un proceso sin privilegios PUEDE abrir este pipe.** El prefijo
/// `ProtectedPrefix\Administrators\` limita quién CREA el nombre, no quién lo
/// abre, y el descriptor del daemon le da al usuario interactivo exactamente
/// lectura, escritura y sincronización. Que la ventana corra sin elevar es el
/// diseño, no una fuga.
class WindowsPipeTransport implements DaemonTransport {
  WindowsPipeTransport({String? name, this.busyRetries = 3})
    : name = name ?? PipeNames.defaultName;

  /// Qué pipe. Ver [PipeNames.defaultName].
  final String name;

  /// Cuántas veces volver cuando el daemon tiene todas sus instancias
  /// ocupadas. `MaxConns` es 8 del otro lado, así que es un caso real con
  /// varias ventanas abiertas, y se despeja en milisegundos.
  final int busyRetries;

  static const MethodChannel _methods = MethodChannel('kanpachi/pipe');

  /// **El id lo pone Dart, y no C++.** Si lo devolviera `open`, los bytes
  /// llegarían por el canal de eventos mientras la respuesta del canal de
  /// métodos sigue viajando, y no hay orden garantizado entre dos canales: los
  /// primeros bytes de una conexión rápida no tendrían a quién entregarse.
  /// Poniéndolo acá, el buzón está registrado antes de que exista el handle.
  static int _nextId = 0;

  int? _id;
  bool _closing = false;

  final StreamController<List<int>> _incoming =
      StreamController<List<int>>.broadcast();

  @override
  Stream<List<int>> get incoming => _incoming.stream;

  @override
  Future<void> connect() async {
    if (_id != null) return;

    final int id = ++_nextId;
    _PipeEvents.instance.register(id, this);
    try {
      await _methods.invokeMethod<void>('open', <String, Object?>{
        'id': id,
        'name': name,
        'busyRetries': busyRetries,
      });
    } on PlatformException catch (e) {
      _PipeEvents.instance.forget(id);
      throw DaemonUnreachable(
        _whyItDidNotOpen(_win32Of(e)),
        kind: DaemonUnreachableKind.notConnected,
      );
    } on MissingPluginException catch (e) {
      _PipeEvents.instance.forget(id);
      // Pasa fuera de la aplicación: `dart run` de una herramienta no tiene
      // motor, así que no tiene canales. Se dice con esas palabras en vez de
      // dejar el error crudo de Flutter, que se lee como un bug del producto.
      throw DaemonUnreachable(
        'el canal nativo del pipe no está: esto solo funciona dentro de la '
        'aplicación, no en un proceso de Dart suelto ($e)',
        kind: DaemonUnreachableKind.notConnected,
      );
    }
    _id = id;
  }

  @override
  Future<void> send(List<int> bytes) async {
    final int? id = _id;
    if (id == null || _closing) {
      throw const DaemonUnreachable(
        'el canal con el daemon no está abierto',
        kind: DaemonUnreachableKind.notConnected,
      );
    }
    try {
      await _methods.invokeMethod<void>('send', <String, Object?>{
        'id': id,
        'bytes': Uint8List.fromList(bytes),
      });
    } on PlatformException catch (e) {
      throw DaemonUnreachable(
        'no se pudo escribir en el canal del daemon, error ${_win32Of(e)} de '
        'Windows',
        kind: DaemonUnreachableKind.writeFailed,
      );
    }
  }

  @override
  Future<void> close() async {
    if (_closing) return;
    _closing = true;

    final int? id = _id;
    _id = null;
    if (id != null) {
      _PipeEvents.instance.forget(id);
      try {
        await _methods.invokeMethod<void>('close', <String, Object?>{'id': id});
      } on PlatformException catch (e, s) {
        AppLog.error('cerrando el canal del daemon', e, s);
      } on MissingPluginException catch (e, s) {
        AppLog.error('cerrando el canal del daemon', e, s);
      }
    }

    if (!_incoming.isClosed) await _incoming.close();
  }

  /// Bytes del daemon. Lo llama [_PipeEvents], que es quien escucha el canal.
  void _onData(Uint8List bytes) {
    if (!_incoming.isClosed) _incoming.add(bytes);
  }

  /// El canal se acabó. `why` distingue los tres casos que antes eran todos
  /// «error de Windows 0» y por lo tanto no distinguían nada.
  void _onClosed(String why, int error) {
    if (_closing) return;
    AppLog.info(
      'el canal con el daemon se acabó',
      'motivo $why error de Windows $error',
    );
    // Cerrar el stream aterriza en el `onDone` de `DaemonClient`, que rompe
    // todas las peticiones pendientes de una vez en lugar de dejarlas expirar
    // una por una.
    if (!_incoming.isClosed) _incoming.close();
  }

  /// El código de Windows que el runner mandó como detalle, o 0.
  static int _win32Of(PlatformException e) {
    final Object? details = e.details;
    return details is int ? details : 0;
  }

  /// El mensaje de un fallo al abrir, que es el que más se ve.
  String _whyItDidNotOpen(int error) {
    if (error == _Win32.fileNotFound) {
      return 'el servicio de Kanpachi no está escuchando en $name';
    }
    if (error == _Win32.pipeBusy) {
      return 'el servicio de Kanpachi tiene todas sus conexiones ocupadas';
    }
    if (error == _Win32.accessDenied) {
      return 'Windows negó el acceso al canal de Kanpachi';
    }
    return 'no se pudo abrir el canal de Kanpachi, error $error de Windows';
  }
}

/// Los tres errores de Windows que esta pantalla sabe explicar con palabras.
///
/// Están acá como números y no importando `win32` a propósito: este archivo
/// dejó de hablar FFI, y traer el paquete entero por tres constantes volvería a
/// atar la capa a algo que ya no usa.
abstract final class _Win32 {
  static const int fileNotFound = 2;
  static const int accessDenied = 5;
  static const int pipeBusy = 231;
}

/// Reparte los eventos del canal nativo entre las conexiones abiertas.
///
/// # Por qué hay un repartidor y no un canal por conexión
///
/// Porque hay más de una conexión a la vez: `DaemonConnector.spare()` abre una
/// SEGUNDA, y tiene que hacerlo — el bucle de servidor de una conexión es
/// secuencial, así que preguntar por el progreso de `create_room` por la misma
/// conexión haría cola detrás de la operación que quiere mirar.
///
/// Un `EventChannel` por conexión obligaría a crear canales con nombre dinámico
/// en las dos puntas y a esperar a que Dart se suscriba antes de que lleguen
/// bytes. Uno solo con el id dentro del evento no tiene esa carrera.
class _PipeEvents {
  _PipeEvents._() {
    _subscription = const EventChannel(
      'kanpachi/pipe/events',
    ).receiveBroadcastStream().listen(_onEvent, onError: _onError);
  }

  static final _PipeEvents instance = _PipeEvents._();

  final Map<int, WindowsPipeTransport> _open =
      <int, WindowsPipeTransport>{};

  // Se guarda aunque no se cancele nunca: vive lo que el proceso, y una
  // suscripción sin referencia es justo lo que un análisis de fugas señala.
  // ignore: unused_field
  late final StreamSubscription<dynamic> _subscription;

  void register(int id, WindowsPipeTransport transport) {
    _open[id] = transport;
  }

  void forget(int id) {
    _open.remove(id);
  }

  void _onEvent(Object? event) {
    if (event is! Map) return;
    final Object? id = event['id'];
    if (id is! int) return;
    final WindowsPipeTransport? transport = _open[id];
    // Un id desconocido no es un error: el runner avisa del cierre aunque este
    // lado ya haya cerrado y se haya borrado del mapa.
    if (transport == null) return;

    if (event['kind'] == 'data') {
      final Object? bytes = event['bytes'];
      if (bytes is Uint8List) transport._onData(bytes);
      return;
    }

    _open.remove(id);
    final Object? why = event['why'];
    final Object? error = event['error'];
    transport._onClosed(
      why is String ? why : 'desconocido',
      error is int ? error : 0,
    );
  }

  void _onError(Object error, StackTrace stack) {
    // No se traga: si el canal nativo se rompe, todas las conexiones se quedan
    // mudas y sin esto no habría dónde leerlo.
    AppLog.error('el canal de eventos del pipe', error, stack);
  }
}
