/// No se pudo hablar con el servicio de Kanpachi.
///
/// # Por qué vive en el dominio y no al lado del pipe
///
/// Porque es parte del contrato, no del transporte. Quien llama al repositorio
/// tiene que poder distinguir "el daemon dijo que no" de "no hubo con quién
/// hablar" **sin importar la capa que habla el named pipe**, y el día que el
/// host sin ventana de `07-futuro.md` hable por un socket de Unix, esta
/// distinción sigue siendo la misma y el error sigue siendo este.
library;

import 'package:kanpachi_ui/core/messages/message_keys.dart';

/// El daemon rechazó una operación, con su código cerrado.
///
/// Vive junto a [DaemonUnreachable] porque las dos son parte del contrato que
/// consume la presentación: una dice que el daemon contestó que no y la otra
/// que no hubo con quién hablar. Ninguna depende de que el transporte sea un
/// named pipe.
class DaemonError implements Exception {
  const DaemonError({required this.code, required this.message, this.result});

  /// El código tal como vino. Se guarda crudo además de resuelto, porque un
  /// daemon más nuevo puede mandar uno que esta app no conoce y perderlo
  /// dejaría el diagnóstico sin la única pista útil.
  final String code;
  final String message;

  /// La carga que acompañó al error, cuando existe.
  ///
  /// `kick_partial` trae además la sala recalculada para poder quitar de la UI
  /// a quien ya salió aunque una de las dos capas de expulsión haya fallado.
  final Object? result;

  FailureCode? get resolved => FailureCode.fromWire(code);

  Map<String, Object?>? get resultMap {
    final Object? value = result;
    return value is Map<String, Object?> ? value : null;
  }

  @override
  String toString() => 'DaemonError($code): $message';
}

/// Dónde ocurrió el fallo, que es lo que decide si repetir es seguro.
///
/// No es una taxonomía para el log. Repetir una petición que pudo haberse
/// ejecutado es como salen dos salas, o alguien expulsado dos veces. La línea
/// la marca la escritura: lo que falló antes de ella no llegó al daemon, y lo
/// que falló después pudo llegar.
enum DaemonUnreachableKind {
  /// No se pudo abrir el canal, o se llamó a un método antes del saludo. No se
  /// envió nada, así que repetir sale gratis.
  notConnected,

  /// La escritura falló. Los bytes no salieron.
  writeFailed,

  /// Venció el plazo del método. La petición puede seguir corriendo del otro
  /// lado: crear una sala es alrededor de un minuto de trabajo real, y repetir
  /// levantaría una segunda mientras la primera termina.
  timedOut,

  /// El canal se cortó con peticiones en vuelo. La petición pudo ejecutarse y
  /// lo que se perdió es la respuesta.
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

  /// Por defecto, el tipo que NO se reintenta. Un fallo cuyo origen nadie se
  /// molestó en decir es justo el que no hay que repetir a ciegas.
  final DaemonUnreachableKind kind;

  /// Si se puede mandar la misma petición otra vez sobre un canal nuevo.
  bool get safeToRetry =>
      kind == DaemonUnreachableKind.notConnected ||
      kind == DaemonUnreachableKind.writeFailed;

  @override
  String toString() => 'DaemonUnreachable(${kind.name}): $reason';
}
