import 'package:flutter/foundation.dart';

/// Qué protocolo abre un rango de puertos.
enum PortProtocol {
  tcp('TCP', 'tcp'),
  udp('UDP', 'udp'),
  both('TCP/UDP', 'both');

  const PortProtocol(this.label, this.wire);

  /// What the screen shows.
  final String label;

  /// What the daemon sends and expects. Kept apart from [label] because they
  /// are two different jobs: one can be reworded for the user any day, and
  /// changing the other silently breaks the contract.
  final String wire;

  /// Unknown means UDP, which is the safe read of an unknown protocol: it is
  /// what the overwhelming majority of game profiles use, and a game whose
  /// ports do not open is a visible failure, unlike one that opens too many.
  static PortProtocol fromWire(String s) => switch (s.toLowerCase()) {
    'tcp' => PortProtocol.tcp,
    'both' || 'tcp/udp' => PortProtocol.both,
    _ => PortProtocol.udp,
  };
}

/// De dónde salió un perfil, que es la confianza que se muestra en la lista.
enum GameOrigin {
  /// Vino con la app, revisado por el autor.
  builtin('builtin'),

  /// Dado de alta en esta máquina con el creador de perfiles.
  mine('mine'),

  /// Importado de un `.json` que compartió alguien.
  imported('imported');

  const GameOrigin(this.wire);

  final String wire;

  static GameOrigin fromWire(String s) => switch (s) {
    'builtin' => GameOrigin.builtin,
    'imported' => GameOrigin.imported,
    _ => GameOrigin.mine,
  };
}

/// Cómo se une la gente a la partida, según el perfil.
enum ConnectHintKind {
  directIp('direct_ip'),
  lanBrowser('lan_browser'),
  steamFriends('steam_friends');

  const ConnectHintKind(this.wire);

  final String wire;

  static ConnectHintKind? fromWire(String? s) => switch (s) {
    'direct_ip' => ConnectHintKind.directIp,
    'lan_browser' => ConnectHintKind.lanBrowser,
    'steam_friends' => ConnectHintKind.steamFriends,
    _ => null,
  };
}

/// Un rango de puertos con su protocolo.
///
/// Rango y no puerto suelto porque la mayoría de los juegos abren varios
/// contiguos (Zomboid 16261-16262, Valheim 2456-2458), y obligar a una fila
/// por puerto convertiría el alta manual en un trámite.
@immutable
class PortRule {
  const PortRule({required this.range, required this.protocol});

  /// Built from the wire shape, which is two numbers and not a string. A range
  /// of one port comes back as `from == to`, and it is written as a single
  /// number because that is what a person expects to read.
  factory PortRule.fromJson(Map<String, Object?> json) {
    final int from = (json['from'] as num? ?? 0).toInt();
    final int to = (json['to'] as num? ?? from).toInt();
    return PortRule(
      range: from == to ? '$from' : '$from-$to',
      protocol: PortProtocol.fromWire(json['proto'] as String? ?? ''),
    );
  }

  /// Tal como lo escribió quien dio de alta el juego: `27015` o `2456-2458`.
  /// Se guarda como texto a propósito: validarlo es del daemon, que es quien
  /// va a abrir el puerto de verdad, y adelantar esa validación acá crearía
  /// dos reglas que se pueden separar.
  final String range;

  final PortProtocol protocol;

  String get label => '$range ${protocol.label}';

  Map<String, Object?> toProfileJson() => <String, Object?>{
    'proto': protocol.wire,
    'range': range,
  };

  PortRule copyWith({String? range, PortProtocol? protocol}) =>
      PortRule(range: range ?? this.range, protocol: protocol ?? this.protocol);

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
    required this.id,
    required this.name,
    required this.rules,
    this.clientRules = const <PortRule>[],
    this.installed = false,
    this.origin = GameOrigin.mine,
    this.verified = false,
    this.hintKind,
    this.hintText,
    this.coverUrl,
  });

  factory Game.fromJson(Map<String, Object?> json) => Game(
    id: json['id'] as String? ?? '',
    name: json['name'] as String? ?? '',
    rules: _reglas(json['host_ports']),
    clientRules: _reglas(json['client_ports']),
    installed: json['installed'] as bool? ?? false,
    origin: GameOrigin.fromWire(json['origin'] as String? ?? ''),
    verified: json['verified'] as bool? ?? false,
    hintKind: ConnectHintKind.fromWire(json['hint_kind'] as String?),
    hintText: json['hint_text'] as String?,
  );

  static List<PortRule> _reglas(Object? crudo) {
    if (crudo is! List<Object?>) return const <PortRule>[];
    return <PortRule>[
      for (final Object? e in crudo)
        if (e is Map<String, Object?>) PortRule.fromJson(e),
    ];
  }

  /// El id del catálogo, que es por lo que el daemon lo conoce.
  ///
  /// Sin él no se puede activar un perfil: `activate_profile` pide el id y no
  /// el nombre. Es también lo que identifica a un juego, porque dos perfiles
  /// pueden llamarse igual y ser distintos.
  final String id;

  final String name;

  /// Los puertos que se abren en el HOST.
  final List<PortRule> rules;

  /// Los puertos que se abren en cada invitado, y casi siempre está vacío.
  ///
  /// No vacío significa topología de MALLA: cada invitado abre esos puertos
  /// hacia todos los demás miembros. `06-catalogo.md` exige avisarlo en
  /// pantalla ANTES de entrar, y esta lista es lo único que permite saberlo.
  final List<PortRule> clientRules;

  /// Detectado en disco. Ordena y sugiere; jamás filtra — toda detección falla
  /// alguna vez, y un juego que Kanpachi no supo ver tiene que poder elegirse
  /// igual desde la biblioteca.
  final bool installed;

  /// De qué capa vino. Tres valores y no un booleano, porque la confianza que
  /// se muestra son tres cosas distintas: viene con la app, lo hiciste tú, o te
  /// lo pasó alguien.
  final GameOrigin origin;

  /// Alguien jugó una partida real con este perfil y dijo que funcionó. Sin
  /// esto la lista lo muestra como "sin probar".
  final bool verified;

  final ConnectHintKind? hintKind;

  /// El texto exacto de cómo se une la gente, que es lo que la pantalla de la
  /// sala muestra debajo de la dirección.
  final String? hintText;

  /// No viaja por el cable. Es local, y decirlo evita que alguien lo busque.
  final String? coverUrl;

  bool get isMesh => clientRules.isNotEmpty;

  bool get isManual => origin == GameOrigin.mine;

  /// Los puertos en una línea: `16261-16262 UDP`.
  String get portsLabel =>
      rules.isEmpty ? '—' : rules.map((PortRule r) => r.label).join(' · ');

  /// El primer puerto, que es al que se conectan los demás.
  String get primaryPort {
    if (rules.isEmpty) return '';
    final RegExpMatch? m = RegExp(r'\d+').firstMatch(rules.first.range);
    return m?.group(0) ?? '';
  }

  /// The full v2 profile object `save_profile` expects.
  ///
  /// Every level of it is decoded with unknown fields forbidden on the daemon
  /// side, so this has to be exact: one extra key rejects the whole profile.
  ///
  /// `origin` is deliberately absent. The daemon pins it to `mine`, and sending
  /// it would be a claim the client has no standing to make.
  Map<String, Object?> toProfileJson() => <String, Object?>{
    'id': id,
    'schema': 2,
    'name': name,
    'detect': <String, Object?>{},
    'host_ports': <Object?>[for (final PortRule r in rules) r.toProfileJson()],
    'client_ports': <Object?>[
      for (final PortRule r in clientRules) r.toProfileJson(),
    ],
    'lan_discovery': false,
    'system_tweaks': <String, Object?>{
      'broadcast_route': false,
      'multicast_route': false,
      'prefer_ipv4': false,
      'directplay': false,
    },
    'connect_hint': <String, Object?>{
      // Mandatory on the daemon side: an empty kind fails validation and the
      // profile is rejected whole. The manual form does not ask for it yet, so
      // direct IP is the default, which is what the overwhelming majority of
      // games use and the only one that works without a lobby.
      'kind': (hintKind ?? ConnectHintKind.directIp).wire,
      'text_es': hintText ?? 'En el juego: escribe la IP del host.',
    },
    'bind_hint': <String, Object?>{},
  };

  Game copyWith({String? id, String? name, List<PortRule>? rules}) => Game(
    id: id ?? this.id,
    name: name ?? this.name,
    rules: rules ?? this.rules,
    clientRules: clientRules,
    installed: installed,
    origin: origin,
    verified: verified,
    hintKind: hintKind,
    hintText: hintText,
    coverUrl: coverUrl,
  );

  /// Identity is the id and not the name, and that is not a detail: two
  /// profiles can carry the same name and be different games, and the daemon
  /// only ever speaks about them by id.
  @override
  bool operator ==(Object other) => other is Game && other.id == id;

  @override
  int get hashCode => id.hashCode;
}

/// Turns a name into an id the daemon will accept.
///
/// `validProfileID` on the other side takes lowercase letters, digits and
/// dashes, one to sixty-four bytes, no dash at either end. The manual form only
/// asks for a name, so somebody has to do this, and doing it here means the
/// user sees the rejection before the round trip.
///
/// Returns an empty string when nothing survives, which the caller has to treat
/// as "ask for another name" rather than send.
String slugForProfile(String name) {
  final StringBuffer out = StringBuffer();
  bool guion = false;
  for (final int unidad in name.toLowerCase().runes) {
    final String c = String.fromCharCode(unidad);
    final bool bueno = RegExp(r'[a-z0-9]').hasMatch(c);
    if (bueno) {
      out.write(c);
      guion = false;
      continue;
    }
    if (!guion && out.isNotEmpty) {
      out.write('-');
      guion = true;
    }
  }
  String s = out.toString();
  while (s.endsWith('-')) {
    s = s.substring(0, s.length - 1);
  }
  return s.length > 64 ? s.substring(0, 64).replaceAll(RegExp(r'-+$'), '') : s;
}
