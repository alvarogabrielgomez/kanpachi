import 'package:flutter/foundation.dart';
import 'package:kanpachi_ui/core/messages/message_keys.dart';

/// La Protección Kanpachi: la comprobación que atraviesa la red de verdad.
///
/// # Qué comprueba, y por qué nada más lo comprueba
///
/// La compuerta es un bloqueo sobre el adaptador virtual entero. Comprobarla
/// desde dentro de la PC solo puede preguntar si el filtro EXISTE, no si CASA.
/// Un filtro vivo que no contiene nada se ve verde desde dentro.
///
/// El canario lo mide por fuera: el host abre un oyente en un puerto que la
/// compuerta tiene que bloquear, y le pide a TODOS los de la sala que lo
/// marquen. Si alguien llega, el paquete cruzó.
///
/// # Las dos fuentes, y solo una es un hecho
///
/// [touched] lo vio el HOST con su propio socket, y no se puede falsificar.
/// [answers] son mensajes de los miembros, y un mensaje se puede mentir. Van por
/// separado porque juntarlas tiraría justo lo que hace creíble la frase.
///
/// # Lo que la pantalla NO puede decir
///
/// Que la compuerta funciona. [CanaryVerdict.clean] significa que no hay
/// evidencia de fuga, que es más débil: un miembro que se quede quieto produce
/// ese mismo estado. Lo único que se afirma con certeza es
/// [CanaryVerdict.leaking].
@immutable
class CanaryCheck {
  const CanaryCheck({
    required this.measured,
    required this.verdict,
    this.port = 0,
    this.touched = false,
    this.measuredAt,
    this.asked = const <String>[],
    this.answers = const <CanaryAnswer>[],
  });

  /// La comprobación de antes de que ocurriera ninguna. Es el estado inicial, y
  /// es deliberado que no diga nada bueno: no se ha comprobado.
  const CanaryCheck.blind()
    : measured = false,
      verdict = CanaryVerdict.blind,
      port = 0,
      touched = false,
      measuredAt = null,
      asked = const <String>[],
      answers = const <CanaryAnswer>[];

  final bool measured;
  final CanaryVerdict verdict;

  /// El puerto que se abrió, para poder nombrarlo.
  final int port;

  /// Lo que vio el propio host. Es lo único de acá que se afirma con certeza.
  final bool touched;

  final DateTime? measuredAt;

  /// Los nombres de TODOS a los que se preguntó.
  final List<String> asked;
  final List<CanaryAnswer> answers;

  /// Si esto tiene que llegar a la pantalla del usuario.
  ///
  /// **Es información, no la alarma.** La alarma la decide el daemon y llega
  /// como `AlertKind.canaryLeaking`, y solo aparece cuando ya intentó reponer la
  /// protección y no aguantó. Una fuga reparada sola no tiene que pintar nada:
  /// enterar al usuario de un problema ya arreglado es enseñarle a ignorar los
  /// avisos.
  bool get needsAttention =>
      verdict == CanaryVerdict.leaking || verdict == CanaryVerdict.mismatch;

  /// Cuántos de los preguntados llegaron a contestar.
  String get answeredLabel => '${answers.length} de ${asked.length}';

  /// La hora de la comprobación, `20:14:03`. Vacío si no se hizo.
  ///
  /// Va la hora y no "hace tanto" por lo mismo que en el sondeo: un "hace 2 s"
  /// congelado en pantalla envejece mintiendo.
  String get measuredLabel {
    final DateTime? at = measuredAt;
    if (at == null) return '';
    String dos(int n) => n.toString().padLeft(2, '0');
    return '${dos(at.hour)}:${dos(at.minute)}:${dos(at.second)}';
  }

  static CanaryCheck fromJson(Map<String, Object?> json) {
    final bool measured = json['measured'] == true;
    if (!measured) {
      // Ni puerto ni hora ni respuestas, aunque el JSON las trajera. El daemon
      // ya garantiza que una comprobación ciega viaja vacía, y repetirlo acá
      // cuesta una línea: lo que no se pueda expresar mal, mejor.
      return const CanaryCheck.blind();
    }

    final Object? at = json['measured_at_ms'];
    return CanaryCheck(
      measured: true,
      verdict: CanaryVerdict.fromWire(json['verdict'] as String?),
      port: (json['port'] as int?) ?? 0,
      touched: json['touched'] == true,
      measuredAt: at is int
          ? DateTime.fromMillisecondsSinceEpoch(at, isUtc: true).toLocal()
          : null,
      asked: <String>[
        for (final Object? a
            in (json['asked'] as List<Object?>?) ?? const <Object?>[])
          if (a is String) a,
      ],
      answers: <CanaryAnswer>[
        for (final Object? a
            in (json['answers'] as List<Object?>?) ?? const <Object?>[])
          if (a is Map<String, Object?>) CanaryAnswer.fromJson(a),
      ],
    );
  }
}

/// Lo que contestó UNO de los preguntados. Es una pista y no una prueba.
@immutable
class CanaryAnswer {
  const CanaryAnswer({
    required this.from,
    required this.tcp,
    required this.udp,
  });

  /// De quién, para que la pantalla pueda decir "desde la PC de Fulano".
  final String from;

  final ProbeOutcome tcp;
  final ProbeOutcome udp;

  /// Si este miembro afirma que llegó por algún protocolo.
  bool get reached =>
      tcp == ProbeOutcome.answered ||
      tcp == ProbeOutcome.refused ||
      udp == ProbeOutcome.answered ||
      udp == ProbeOutcome.refused;

  static CanaryAnswer fromJson(Map<String, Object?> json) => CanaryAnswer(
    from: (json['from'] as String?) ?? '',
    tcp: ProbeOutcome.fromWire(json['tcp'] as String?),
    udp: ProbeOutcome.fromWire(json['udp'] as String?),
  );
}
