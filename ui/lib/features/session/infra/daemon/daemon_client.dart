import 'dart:async';

import 'package:kanpachi_ui/features/session/domain/daemon_failure.dart';
import 'package:kanpachi_ui/features/session/infra/daemon/daemon_codec.dart';
import 'package:kanpachi_ui/features/session/infra/daemon/daemon_methods.dart';
import 'package:kanpachi_ui/features/session/infra/daemon/daemon_transport.dart';

/// La conversación con el daemon.
///
/// # Qué resuelve
///
/// Tres cosas que parecen detalles hasta que faltan:
///
///  - **El saludo va primero.** El daemon rechaza cualquier método antes del
///    `hello` con el token, así que el cliente no deja salir nada hasta que el
///    saludo volvió. No es cortesía: sin esto, la primera petición de cada
///    conexión falla con `unauthorized` y el usuario ve un error inventado.
///  - **Las respuestas vuelven por id, no por orden.** Cada petición espera en
///    su propio `Completer`. Suponer que la respuesta N corresponde a la
///    petición N es correcto hasta el día que dos pantallas preguntan a la vez.
///  - **Nada espera para siempre.** Toda petición tiene plazo, y el plazo es
///    POR MÉTODO: ver `daemon_methods.dart`. Un daemon vivo que dejó de
///    contestar es indistinguible de uno muerto desde acá, y una ruedita eterna
///    es la forma más común de que una app mienta.
///
/// # Lo que NO hace
///
/// No reconecta, no reintenta y no traduce a copy. Reconectar es política y
/// vive más arriba; el texto lo pone el catálogo de mensajes.
class DaemonClient {
  DaemonClient({
    required this.transport,
    required this.token,
    Duration Function(String method)? timeoutFor,
  }) : _timeoutFor = timeoutFor ?? defaultTimeoutFor;

  final DaemonTransport transport;

  /// El token de `api.token`. Se lee de disco en CADA conexión: el daemon lo
  /// rota una vez por vida del proceso y borra el archivo al salir, así que uno
  /// recordado entre arranques es siempre el equivocado.
  final String token;

  final Duration Function(String method) _timeoutFor;

  final DaemonCodec _codec = DaemonCodec();
  final Map<int, Completer<DaemonResponse>> _esperando =
      <int, Completer<DaemonResponse>>{};

  StreamSubscription<List<int>>? _sub;
  int _siguienteId = 0;
  bool _saludado = false;
  bool _cerrado = false;
  bool _muerto = false;

  /// Si esta conversación ya no sirve para nada.
  ///
  /// Lo mira [DaemonConnector], que memoriza el cliente vivo: sin esto seguía
  /// entregando el mismo cadáver, y la ventana se quedaba **congelada sin
  /// decirlo**. Medido el 2026-08-09: el daemon cortó por silencio, la interfaz
  /// lo supo a las 15:31:26, y estuvo diecisiete minutos en pantalla sin hablar
  /// con nadie, con el estado de antes dibujado, hasta que el proceso se cayó.
  bool get isDead => _muerto || _cerrado;

  /// Abre el transporte, arranca la escucha y saluda. Idempotente: saludar dos
  /// veces no manda dos saludos ni abre dos veces.
  Future<void> connect() async {
    if (_saludado) return;

    await transport.connect();

    _sub = transport.incoming.listen(
      _onBytes,
      onError: (Object e) =>
          _morir('el transporte falló: $e', DaemonUnreachableKind.linkLost),
      onDone: () =>
          _morir('el daemon cerró la conexión', DaemonUnreachableKind.linkLost),
    );

    await _send(DaemonMethods.hello, <String, Object?>{'token': token});
    _saludado = true;
  }

  /// Llama a un método que contesta con un objeto.
  ///
  /// Lanza [DaemonError] si el daemon dijo que no, y [DaemonUnreachable] si no
  /// se pudo ni preguntar. Son distintos a propósito: quien lee el log tiene
  /// que poder separar "el producto rechazó la operación" de "no hubo con quién
  /// hablar".
  Future<Map<String, Object?>> call(
    String method, [
    Map<String, Object?>? params,
  ]) async {
    final DaemonResponse r = await _llamar(method, params);
    return r.resultMap ?? const <String, Object?>{};
  }

  /// Llama a un método que contesta con una lista.
  ///
  /// Existe aparte y no como un `Object?` que cada llamador destripa porque el
  /// contrato del daemon ya sabe cuál es cuál: cinco de los veintisiete
  /// contestan con un array, y el resto con un objeto. Elegir el método correcto
  /// es una decisión de quien llama, no un `is` en tiempo de ejecución.
  Future<List<Object?>> callList(
    String method, [
    Map<String, Object?>? params,
  ]) async {
    final DaemonResponse r = await _llamar(method, params);
    return r.resultList ?? const <Object?>[];
  }

  Future<DaemonResponse> _llamar(
    String method,
    Map<String, Object?>? params,
  ) async {
    if (!_saludado) {
      throw const DaemonUnreachable(
        'se llamó a un método antes de saludar: el daemon rechaza todo hasta '
        'el hello',
        kind: DaemonUnreachableKind.notConnected,
      );
    }
    return _send(method, params);
  }

  Future<DaemonResponse> _send(
    String method,
    Map<String, Object?>? params,
  ) async {
    if (_cerrado) {
      throw const DaemonUnreachable(
        'el cliente ya está cerrado',
        kind: DaemonUnreachableKind.notConnected,
      );
    }

    final int id = ++_siguienteId;
    final Completer<DaemonResponse> espera = Completer<DaemonResponse>();
    _esperando[id] = espera;

    // **El plazo se arma ANTES de escribir, y se le pone un oyente en el acto.**
    //
    // Entre registrar el completer y llegar a esperarlo hay un `await` sobre la
    // escritura. Si el daemon corta justo ahí, [_matarTodo] completa con un
    // error un futuro que todavía no tiene a nadie escuchando, y eso es un error
    // asíncrono HUÉRFANO: sube hasta la zona de `main()` y queda anotado como un
    // fallo de la interfaz que nadie pidió.
    //
    // **Armar el plazo antes NO alcanza, y eso costó una vuelta entera.** Mueve
    // el huérfano en vez de quitarlo: el que se queda sin oyente pasa a ser el
    // futuro que devuelve `timeout`, porque a ese nadie lo espera hasta después
    // de la escritura. Medido el 2026-08-09 a las 17:50 y ocho veces más, con la
    // traza apuntando a `_matarTodo` desde el `onDone` del transporte.
    //
    // Lo que lo cierra es el oyente de abajo. Marca el futuro como observado
    // desde ya, y `await` más abajo agrega un segundo oyente que recibe lo
    // mismo: un `Future` admite varios, y basta con que UNO llegue a tiempo.
    //
    // De paso el plazo pasa a cubrir también la escritura, que es lo correcto:
    // el plazo es del MÉTODO, y escribir es parte del método. Antes una
    // escritura que no volviera se quedaba esperando sin plazo ninguno.
    final Duration plazo = _timeoutFor(method);
    final Future<DaemonResponse> respuestaFutura = espera.future.timeout(plazo);
    unawaited(respuestaFutura.then<void>((_) {}, onError: (Object _) {}));

    try {
      await transport.send(
        _codec.encode(DaemonRequest(id: id, method: method, params: params)),
      );
    } on Object catch (e) {
      _esperando.remove(id);
      throw DaemonUnreachable(
        'no se pudo escribir al daemon: $e',
        kind: DaemonUnreachableKind.writeFailed,
      );
    }

    final DaemonResponse respuesta;
    try {
      respuesta = await respuestaFutura;
    } on TimeoutException {
      // Se saca del mapa: si contesta tarde, la respuesta llega a un completer
      // que ya nadie mira y completarlo dos veces sería un error de estado.
      _esperando.remove(id);
      throw DaemonUnreachable(
        'el daemon no contestó a $method en ${plazo.inSeconds} s',
        kind: DaemonUnreachableKind.timedOut,
      );
    }

    final DaemonError? error = respuesta.error;
    if (error != null) throw error;
    return respuesta;
  }

  void _onBytes(List<int> bytes) {
    final List<DaemonResponse> respuestas;
    try {
      respuestas = _codec.feed(bytes);
    } on DaemonProtocolError catch (e) {
      // Un flujo que dejó de tener forma no se puede recuperar leyendo más.
      _morir(e.reason, DaemonUnreachableKind.linkLost);
      return;
    }

    for (final DaemonResponse r in respuestas) {
      // An id of zero is not a late answer, it is the daemon saying it could
      // not attribute the failure: `too_large` and an unreadable envelope both
      // come back that way, and after `too_large` the daemon closes the pipe.
      // Ids on this side start at one, so without this the response matches
      // nobody, gets dropped in silence, and whoever asked hangs until their
      // own deadline. It is terminal for the same reason the size cap is: the
      // byte stream is out of step and reading more means taking half a message
      // for a whole one.
      if (r.id == 0) {
        _morir(
          'el daemon contestó sin poder decir a qué petición: '
          '${r.error?.code ?? "sobre ilegible"}',
          DaemonUnreachableKind.linkLost,
        );
        return;
      }

      final Completer<DaemonResponse>? espera = _esperando.remove(r.id);
      // Una respuesta a un id que nadie espera es lo que deja un plazo vencido.
      // Se descarta en silencio: fallar acá castigaría a quien sí espera.
      espera?.complete(r);
    }
  }

  /// La conversación se acabó: se rompen las esperas Y se suelta todo.
  ///
  /// # Por qué no basta con romper las esperas
  ///
  /// Porque eso era lo único que se hacía, y dejaba **la mitad del apagado sin
  /// hacer**. El cliente seguía pareciendo vivo, así que el conector seguía
  /// entregándolo; el transporte no se cerraba, así que se quedaban en pie el
  /// isolate que escribe, sus cuatro reservas nativas, el handle del pipe y el
  /// evento de parada. Uno de cada por cada corte, y el daemon corta cada diez
  /// minutos de silencio, que es el caso NORMAL de esta app.
  ///
  /// Cerrar desde acá es reentrante a propósito: [close] vuelve a llamar a
  /// [_matarTodo], y para entonces ya no queda nadie esperando.
  void _morir(String motivo, DaemonUnreachableKind kind) {
    _muerto = true;
    _matarTodo(motivo, kind);
    unawaited(close());
  }

  /// Rompe todas las esperas con el mismo motivo.
  ///
  /// Sin esto, que el daemon muera deja cada `await` colgado hasta su plazo, y
  /// la app se queda con las ruedas girando una a una en vez de decir de una
  /// vez que no hay nadie del otro lado.
  void _matarTodo(String motivo, DaemonUnreachableKind kind) {
    final List<Completer<DaemonResponse>> pendientes =
        List<Completer<DaemonResponse>>.of(_esperando.values);
    _esperando.clear();

    // **Con traza, y esa traza es el entregable.**
    //
    // `completeError` sin ella deja `StackTrace.empty`, y un error así que se
    // escape hasta la zona de `main()` queda anotado como `traza []`: se sabe
    // QUÉ falló y no se sabe quién lo estaba esperando. Pasó de verdad, dos
    // veces, en el `kanpachi-ui.log` del 2026-08-09, y el camino no se pudo
    // reconstruir leyendo el código. Tomada acá apunta a quien rompió la
    // espera, que es la mitad que faltaba.
    final StackTrace desde = StackTrace.current;
    for (final Completer<DaemonResponse> c in pendientes) {
      if (!c.isCompleted) {
        c.completeError(DaemonUnreachable(motivo, kind: kind), desde);
      }
    }
  }

  /// Cierra. IDEMPOTENTE, por lo mismo que del lado del daemon: lo llama el
  /// camino de error, que puede correr antes de que se haya abierto nada.
  Future<void> close() async {
    if (_cerrado) return;
    _cerrado = true;

    _matarTodo('el cliente se cerró', DaemonUnreachableKind.linkLost);
    await _sub?.cancel();
    _sub = null;
    await transport.close();
  }
}
