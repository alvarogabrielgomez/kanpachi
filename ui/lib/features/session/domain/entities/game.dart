import 'package:flutter/foundation.dart';

/// Qué protocolo abre un rango de puertos.
enum PortProtocol {
  tcp('TCP'),
  udp('UDP'),
  both('TCP/UDP');

  const PortProtocol(this.label);

  final String label;
}

/// Un rango de puertos con su protocolo.
///
/// Rango y no puerto suelto porque la mayoría de los juegos abren varios
/// contiguos (Zomboid 16261-16262, Valheim 2456-2458), y obligar a una fila
/// por puerto convertiría el alta manual en un trámite.
@immutable
class PortRule {
  const PortRule({required this.range, required this.protocol});

  /// Tal como lo escribió quien dio de alta el juego: `27015` o `2456-2458`.
  /// Se guarda como texto a propósito: validarlo es del daemon, que es quien
  /// va a abrir el puerto de verdad, y adelantar esa validación acá crearía
  /// dos reglas que se pueden separar.
  final String range;

  final PortProtocol protocol;

  String get label => '$range ${protocol.label}';

  PortRule copyWith({String? range, PortProtocol? protocol}) => PortRule(
        range: range ?? this.range,
        protocol: protocol ?? this.protocol,
      );

  @override
  bool operator ==(Object other) =>
      other is PortRule && other.range == range && other.protocol == protocol;

  @override
  int get hashCode => Object.hash(range, protocol);
}

/// Un juego del catálogo.
@immutable
class Game {
  const Game({
    required this.name,
    required this.rules,
    this.installed = false,
    this.manual = false,
    this.coverUrl,
  });

  final String name;
  final List<PortRule> rules;

  /// Detectado en disco. Ordena y sugiere; jamás filtra — toda detección falla
  /// alguna vez, y un juego que Kanpachi no supo ver tiene que poder elegirse
  /// igual desde la biblioteca.
  final bool installed;

  /// Dado de alta a mano por el usuario, no del catálogo que viene con la app.
  final bool manual;

  final String? coverUrl;

  /// Los puertos en una línea: `16261-16262 UDP`.
  String get portsLabel =>
      rules.isEmpty ? '—' : rules.map((PortRule r) => r.label).join(' · ');

  /// El primer puerto, que es al que se conectan los demás.
  String get primaryPort {
    if (rules.isEmpty) return '';
    final RegExpMatch? m = RegExp(r'\d+').firstMatch(rules.first.range);
    return m?.group(0) ?? '';
  }

  @override
  bool operator ==(Object other) => other is Game && other.name == name;

  @override
  int get hashCode => name.hashCode;
}
