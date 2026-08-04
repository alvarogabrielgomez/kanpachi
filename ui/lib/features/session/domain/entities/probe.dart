import 'package:flutter/foundation.dart';
import 'package:kanpachi_ui/core/messages/message_keys.dart';

/// El sondeo desde otra máquina: la única comprobación que atraviesa la red.
///
/// # Por qué esto no es [ExposureReport]
///
/// Porque aquel lee lo que ESTA PC tiene configurado, preguntándole a la misma
/// Windows que aplica las reglas. Es honesto y no puede contestar la pregunta
/// del producto, que es si desde otra PC se llega. Un filtro que dejó de casar,
/// una regla del sistema que nadie miró, o una herramienta que abrió su propio
/// hueco, se ven todos igual desde dentro: verde.
///
/// # La referencia, que es lo que lo vuelve una medición
///
/// El canal de la sala tiene que contestar. Hasta que lo haga, el silencio de
/// todo lo demás no distingue una PC blindada de una apagada, y por eso
/// [ProbeVerdict.unreachable] existe como estado propio en vez de leerse como
/// "cerrado".
@immutable
class ProbeReport {
  const ProbeReport({
    required this.measured,
    required this.verdict,
    this.target = '',
    this.name = '',
    this.measuredAt,
    this.results = const <ProbeResult>[],
  });

  /// El informe de antes de pulsar nada. Es el estado inicial de la pantalla, y
  /// es deliberado que no diga "cerrado": no se ha probado.
  const ProbeReport.blind()
      : measured = false,
        verdict = ProbeVerdict.blind,
        target = '',
        name = '',
        measuredAt = null,
        results = const <ProbeResult>[];

  final bool measured;
  final ProbeVerdict verdict;

  /// La IP virtual a la que se marcó, y quién es.
  final String target;
  final String name;

  final DateTime? measuredAt;
  final List<ProbeResult> results;

  /// Los puertos que contestaron sin que nadie los pidiera. Es la lista que la
  /// alarma nombra: "hay algo abierto" sin decir qué manda a buscar a ciegas.
  List<ProbeResult> get leaks => results
      .where((ProbeResult r) => r.kind == ProbeKind.forbidden && r.reached)
      .toList();

  /// Cómo se llama la máquina sondeada, para no tener que enseñar un número.
  String get targetLabel => name.isNotEmpty ? name : target;

  /// La hora de la comprobación, `20:14:03`. Vacío si no se hizo.
  ///
  /// Va la hora y no "hace tanto" por lo mismo que en [ExposureReport]: un
  /// "hace 2 s" congelado en pantalla envejece mintiendo.
  String get measuredLabel {
    final DateTime? at = measuredAt;
    if (at == null) return '';
    String dos(int n) => n.toString().padLeft(2, '0');
    return '${dos(at.hour)}:${dos(at.minute)}:${dos(at.second)}';
  }

  static ProbeReport fromJson(Map<String, Object?> json) {
    final bool measured = json['measured'] == true;
    if (!measured) {
      // Ni filas ni hora ni destino, aunque el JSON las trajera. El daemon ya
      // garantiza que un sondeo ciego viaja vacío, y repetirlo acá cuesta una
      // línea: lo que no se pueda expresar mal, mejor.
      return const ProbeReport.blind();
    }

    final Object? at = json['measured_at_ms'];
    return ProbeReport(
      measured: true,
      verdict: ProbeVerdict.fromWire(json['verdict'] as String?),
      target: (json['target'] as String?) ?? '',
      name: (json['name'] as String?) ?? '',
      measuredAt: at is int
          ? DateTime.fromMillisecondsSinceEpoch(at, isUtc: true).toLocal()
          : null,
      results: <ProbeResult>[
        for (final Object? r in (json['results'] as List<Object?>?) ?? const <Object?>[])
          if (r is Map<String, Object?>) ProbeResult.fromJson(r),
      ],
    );
  }
}

/// Un puerto marcado, con lo que contestó.
@immutable
class ProbeResult {
  const ProbeResult({
    required this.port,
    required this.kind,
    required this.label,
    required this.outcome,
    this.rttMs = 0,
  });

  final int port;
  final ProbeKind kind;

  /// Lo que el usuario lee: el nombre de la herramienta, del juego, o del canal
  /// de la sala.
  final String label;

  final ProbeOutcome outcome;

  /// Solo tiene sentido con [ProbeOutcome.answered].
  final int rttMs;

  /// Si el paquete llegó a la otra máquina, que es la única lectura que decide
  /// algo: contestar y rebotar son las dos formas de que el firewall dejara
  /// pasar.
  bool get reached =>
      outcome == ProbeOutcome.answered || outcome == ProbeOutcome.refused;

  /// `445 · compartir archivos (SMB)`.
  String get title => '$port · $label';

  static ProbeResult fromJson(Map<String, Object?> json) => ProbeResult(
        port: (json['port'] as int?) ?? 0,
        kind: ProbeKind.fromWire(json['kind'] as String?),
        label: (json['label'] as String?) ?? '',
        outcome: ProbeOutcome.fromWire(json['outcome'] as String?),
        rttMs: (json['rtt_ms'] as int?) ?? 0,
      );
}
