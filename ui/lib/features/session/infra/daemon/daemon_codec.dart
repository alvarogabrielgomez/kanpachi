import 'dart:convert';

import 'package:kanpachi_ui/core/messages/message_keys.dart';

/// El tope de un mensaje, en bytes. El mismo que aplica el daemon.
///
/// Existe del lado del cliente y no solo del suyo por la razón de siempre: un
/// tope que solo comprueba una punta no es un tope. Si el daemon se volviera
/// loco y mandara una línea infinita, sin esto la app se comería la memoria
/// esperando el salto de línea que nunca llega.
const int kMaxMessageBytes = 1 << 20;

/// Una petición a la API local. Espejo de `protocol.Request`.
class DaemonRequest {
  const DaemonRequest({required this.id, required this.method, this.params});

  final int id;
  final String method;
  final Map<String, Object?>? params;

  Map<String, Object?> toJson() => <String, Object?>{
        'id': id,
        'method': method,
        if (params != null) 'params': params,
      };
}

/// Una respuesta. Lleva resultado o error, jamás los dos.
class DaemonResponse {
  const DaemonResponse({required this.id, this.result, this.error});

  final int id;
  final Map<String, Object?>? result;
  final DaemonError? error;

  bool get isError => error != null;
}

/// El fallo de una operación, con su código cerrado.
///
/// Se decide por [code] y jamás por [message]: el texto viaja para el log y
/// para el diagnóstico que el usuario copia, no para mostrarlo tal cual. Quien
/// convierte el código en algo que se lee es el catálogo de mensajes.
class DaemonError implements Exception {
  const DaemonError({required this.code, required this.message});

  /// El código tal como vino. Se guarda crudo además de resuelto, porque un
  /// daemon más nuevo puede mandar uno que esta app no conoce y perderlo
  /// dejaría el log sin la única pista útil.
  final String code;
  final String message;

  FailureCode? get resolved => FailureCode.fromWire(code);

  @override
  String toString() => 'DaemonError($code): $message';
}

/// El mensaje que llegó no tiene la forma del contrato.
class DaemonProtocolError implements Exception {
  const DaemonProtocolError(this.reason);

  final String reason;

  @override
  String toString() => 'DaemonProtocolError: $reason';
}

/// Convierte el flujo de bytes en respuestas, y las peticiones en bytes.
///
/// # El enmarcado
///
/// Una línea de JSON por mensaje, UTF-8, terminada en `\n`. Es lo que hace el
/// daemon, y la razón por la que se implementa a mano en vez de con
/// `utf8.decoder.transform(LineSplitter())` es el tope: un `LineSplitter` sin
/// límite acumula hasta quedarse sin memoria si el otro lado nunca manda el
/// salto de línea. El tope tiene que vivir en el troceo, no después.
///
/// Pasado el tope se rinde y no intenta resincronizar. Es lo mismo que hace el
/// daemon con su `ErrTooLarge`: quien mandó una línea imposible ya perdió el
/// hilo de la conversación, y seguir leyendo es interpretar la mitad de un
/// mensaje como si fuera uno entero.
class DaemonCodec {
  DaemonCodec({this.maxBytes = kMaxMessageBytes});

  final int maxBytes;

  final List<int> _buffer = <int>[];
  bool _dead = false;

  /// Codifica una petición como la línea que espera el daemon.
  List<int> encode(DaemonRequest request) =>
      utf8.encode('${jsonEncode(request.toJson())}\n');

  /// Mete bytes y saca las respuestas completas que hayan quedado dentro.
  ///
  /// Devuelve una lista porque una lectura del transporte puede traer media
  /// respuesta, una entera, o tres seguidas. Suponer que cada lectura es un
  /// mensaje es el error clásico de quien enmarca sobre un flujo.
  List<DaemonResponse> feed(List<int> chunk) {
    if (_dead) {
      throw const DaemonProtocolError(
        'el flujo ya se dio por perdido tras un mensaje por encima del tope',
      );
    }

    final List<DaemonResponse> out = <DaemonResponse>[];
    for (final int byte in chunk) {
      if (byte == 0x0a) {
        final List<int> linea = List<int>.of(_buffer);
        _buffer.clear();
        // Una línea vacía no es un mensaje: es lo que deja un `\r\n` o un
        // salto de más. Se salta en vez de fallar.
        if (linea.isEmpty) continue;
        out.add(decode(linea));
        continue;
      }

      _buffer.add(byte);
      if (_buffer.length > maxBytes) {
        _buffer.clear();
        _dead = true;
        throw DaemonProtocolError(
          'el daemon mandó un mensaje de más de $maxBytes bytes sin cerrar la '
          'línea, así que se corta la conversación en vez de seguir leyendo a '
          'ciegas',
        );
      }
    }
    return out;
  }

  /// Interpreta una línea suelta.
  DaemonResponse decode(List<int> linea) {
    final Object? crudo;
    try {
      crudo = jsonDecode(utf8.decode(linea));
    } on FormatException catch (e) {
      throw DaemonProtocolError('la respuesta no es JSON válido: ${e.message}');
    }

    if (crudo is! Map<String, Object?>) {
      throw const DaemonProtocolError('la respuesta no es un objeto');
    }

    final Object? id = crudo['id'];
    if (id is! int) {
      // Sin id no se puede saber a qué petición contesta, así que ni siquiera
      // se puede fallar contra quien esperaba.
      throw const DaemonProtocolError('la respuesta no trae id');
    }

    final Object? error = crudo['error'];
    if (error is Map<String, Object?>) {
      return DaemonResponse(
        id: id,
        error: DaemonError(
          code: error['code'] as String? ?? 'internal',
          message: error['message'] as String? ?? '',
        ),
      );
    }

    final Object? result = crudo['result'];
    return DaemonResponse(
      id: id,
      result: result is Map<String, Object?> ? result : null,
    );
  }
}
