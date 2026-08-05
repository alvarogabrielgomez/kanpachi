import 'package:flutter/foundation.dart';
import 'package:kanpachi_ui/core/messages/message_keys.dart';

/// Lo que Kanpachi tiene abierto, MEDIDO.
///
/// # Por qué esto no es la lista de puertos del perfil
///
/// Porque el perfil dice lo que se PIDIÓ y esto dice lo que HAY. La diferencia
/// entre las dos es justamente lo que esta pantalla existe para enseñar: un
/// puerto pedido que el sistema no tiene puesto significa que alguien no va a
/// poder entrar, y sin medir eso no se ve hasta que un amigo se queda fuera.
///
/// # Las dos filas, y la segunda es la que importa
///
/// Los puertos abiertos son una. La otra es [gate], que dice que **todo lo demás
/// del adaptador virtual está cerrado**. Esa segunda convierte la lista de
/// arriba de "lo que Kanpachi abrió" en "lo que hay abierto". Sin ella la lista
/// es cierta y engañosa a la vez: enumera lo propio sin decir que la puerta de
/// al lado la puede tener abierta cualquiera.
@immutable
class ExposureReport {
  const ExposureReport({
    required this.measured,
    required this.gate,
    this.measuredAt,
    this.ports = const <ExposedPort>[],
    this.unexpected = const <String>[],
  });

  /// El informe ciego: no se pudo medir.
  ///
  /// Es el valor por defecto de todo lo que todavía no preguntó, y eso es
  /// deliberado: el estado inicial de la pantalla tiene que ser "no lo sé" y
  /// jamás "no hay nada abierto".
  const ExposureReport.blind()
    : measured = false,
      gate = GateState.unknown,
      measuredAt = null,
      ports = const <ExposedPort>[],
      unexpected = const <String>[];

  /// Si esto es una medición o una ceguera.
  ///
  /// En false la pantalla dice que Kanpachi no pudo leer lo que tiene puesto, y
  /// jamás la última lista buena. Misma doctrina que [AlertKind.auditFailed].
  final bool measured;

  /// Cuándo se midió. Null si no se midió.
  final DateTime? measuredAt;

  final List<ExposedPort> ports;
  final GateState gate;

  /// Reglas del grupo de Kanpachi que nadie pidió. Kanpachi es el único que
  /// escribe ahí, así que una de más significa que alguien lo tocó.
  final List<String> unexpected;

  /// Los puertos que el juego activo pidió, sin el hueco del canal de la sala.
  List<ExposedPort> get gamePorts =>
      ports.where((ExposedPort p) => !p.isControl).toList();

  /// Si algo que Kanpachi pidió abrir no está abierto.
  bool get hasMissing => ports.any((ExposedPort p) => !p.applied);

  /// La hora de la medición, `20:14:03`.
  ///
  /// Va la hora y no "hace tanto" porque esta pantalla no se refresca sola: un
  /// "hace 2 s" congelado en pantalla envejece mintiendo, y una hora fija no.
  /// Vacío si no se midió, que es cuando la pantalla no enseña lista.
  String get measuredLabel {
    final DateTime? at = measuredAt;
    if (at == null) return '';
    String dos(int n) => n.toString().padLeft(2, '0');
    return '${dos(at.hour)}:${dos(at.minute)}:${dos(at.second)}';
  }

  static ExposureReport fromJson(Map<String, Object?> json) {
    final bool measured = json['measured'] == true;
    if (!measured) {
      // Ni lista ni hora, aunque el JSON las trajera. El daemon ya garantiza
      // que un informe ciego viaja vacío, y repetirlo acá cuesta una línea:
      // lo que no se pueda expresar mal, mejor.
      return const ExposureReport.blind();
    }

    final Object? at = json['measured_at_ms'];
    return ExposureReport(
      measured: true,
      measuredAt: at is int
          ? DateTime.fromMillisecondsSinceEpoch(at, isUtc: true).toLocal()
          : null,
      gate: GateState.fromWire(json['gate'] as String?),
      ports: <ExposedPort>[
        for (final Object? p
            in (json['ports'] as List<Object?>?) ?? const <Object?>[])
          if (p is Map<String, Object?>) ExposedPort.fromJson(p),
      ],
      unexpected: <String>[
        for (final Object? u
            in (json['unexpected'] as List<Object?>?) ?? const <Object?>[])
          if (u is String) u,
      ],
    );
  }
}

/// Un hueco abierto, con lo que hace falta para poder leerlo.
@immutable
class ExposedPort {
  const ExposedPort({
    required this.proto,
    required this.from,
    required this.to,
    required this.applied,
    required this.isControl,
    this.reachableBy = const <String>[],
  });

  final String proto;
  final int from;
  final int to;

  /// Quién puede alcanzarlo: direcciones de miembros y rangos, en una sola
  /// lista porque para quien lee son lo mismo.
  ///
  /// Que venga vacía JAMÁS significa cualquiera: el daemon no puede expresar
  /// eso, y una regla sin alcance remoto no llega a existir.
  final List<String> reachableBy;

  /// Si el sistema lo tiene puesto AHORA. En false, Kanpachi lo pidió y no
  /// está, o sea que alguien no va a poder entrar.
  final bool applied;

  /// El hueco del canal de la sala, que lo abre Kanpachi por su cuenta. Se
  /// distingue porque el usuario no lo va a encontrar en el perfil del juego.
  final bool isControl;

  /// `16261` o `16261-16262`, que es como se lee un rango.
  String get portLabel => from == to ? '$from' : '$from-$to';

  /// `udp 16261`, la línea del puerto.
  String get label => '${proto.toUpperCase()} $portLabel';

  static ExposedPort fromJson(Map<String, Object?> json) => ExposedPort(
    proto: (json['proto'] as String?) ?? '',
    from: (json['from'] as int?) ?? 0,
    to: (json['to'] as int?) ?? 0,
    applied: json['applied'] == true,
    isControl: json['control'] == true,
    reachableBy: <String>[
      for (final Object? m
          in (json['members'] as List<Object?>?) ?? const <Object?>[])
        if (m is String) m,
      for (final Object? n
          in (json['nets'] as List<Object?>?) ?? const <Object?>[])
        if (n is String) n,
    ],
  );
}
