import 'package:flutter/foundation.dart';
import 'package:kanpachi_ui/core/messages/message_keys.dart';
import 'package:kanpachi_ui/features/session/domain/entities/game.dart';

/// Cómo llega el tráfico hasta un miembro.
enum PeerPath {
  /// Conexión directa, que es lo normal y lo deseable.
  direct('direct', 'directo'),

  /// A través del seed. Funciona igual, va más lento, y se dice para que
  /// nadie culpe al juego de una latencia que es de la red.
  relay('relay', 'relay'),

  /// Uno mismo. No hay ruta que describir.
  self('self', '');

  const PeerPath(this.wire, this.label);

  /// What the daemon sends. Kept apart from [label] because they used to be the
  /// same string by accident: the daemon was sending its own Spanish log word
  /// and this enum happened to store that word as the display label, so the two
  /// agreed without anybody deciding they should.
  final String wire;

  /// What the screen shows.
  final String label;

  /// Unknown counts as [direct], which is the reading that does not accuse the
  /// network of anything. Marking an unknown path as relay would paint the room
  /// amber over a word this version has not heard of.
  static PeerPath fromWire(String? s) => switch (s) {
    'relay' => PeerPath.relay,
    'self' => PeerPath.self,
    _ => PeerPath.direct,
  };
}

/// Qué eres tú en esta sala.
enum RoomRole {
  host('host'),
  guest('guest');

  const RoomRole(this.wire);

  final String wire;

  static RoomRole fromWire(String? s) =>
      s == 'host' ? RoomRole.host : RoomRole.guest;
}

/// Alguien dentro de la sala.
@immutable
class Member {
  const Member({
    required this.name,
    required this.address,
    required this.path,
    this.latencyMs,
    this.isHost = false,
    this.isSelf = false,
  });

  factory Member.fromJson(Map<String, Object?> json) {
    final int rtt = (json['rtt_ms'] as num? ?? 0).toInt();
    return Member(
      name: json['name'] as String? ?? '',
      address: json['ip'] as String? ?? '',
      path: PeerPath.fromWire(json['path'] as String?),
      // Zero is "not measured yet", which is not the same as zero milliseconds.
      // Showing "0 ms" next to somebody who just joined would be a number the
      // product invented.
      latencyMs: rtt > 0 ? rtt : null,
      isHost: json['host'] as bool? ?? false,
      isSelf: json['self'] as bool? ?? false,
    );
  }

  final String name;

  /// La IP dentro de la LAN virtual. Es la que se pega en el juego.
  final String address;

  final PeerPath path;
  final int? latencyMs;
  final bool isHost;
  final bool isSelf;

  /// La línea de debajo del nombre: `100.87.3.2 · directo · 45 ms`.
  String get meta {
    if (isSelf) return address;
    final List<String> parts = <String>[address, path.label];
    if (latencyMs != null) parts.add('$latencyMs ms');
    return parts.where((String p) => p.isNotEmpty).join(' · ');
  }

  /// Se muestra "(tú)" y "(host)" pegado al nombre porque en una lista de
  /// cuatro nicks parecidos, saber cuál eres tú a la primera importa más que
  /// la pulcritud de la columna.
  String get displayName {
    if (isSelf && isHost) return '$name (tú, host)';
    if (isSelf) return '$name (tú)';
    if (isHost) return '$name (host)';
    return name;
  }
}

// El estado de la conexión ya no vive acá: es `ConnState`, en
// core/messages/message_keys.dart, porque lo dice el daemon y no lo deduce la
// pantalla. Llevaba su propio texto dentro, que es copy escondido dentro del
// dominio: el enum decidía cómo se explica un estado, y eso lo decide el
// catálogo de mensajes.

/// Qué se hizo con la regla de firewall que dejó el propio juego.
enum ForeignRuleState {
  /// No hay ninguna. Es el caso normal y no dice nada en pantalla.
  ///
  /// Existe aparte de [kept] porque no son lo mismo: aquel es una decisión del
  /// usuario sobre una regla que existe, y este es que no había regla. Sin este
  /// valor, una sala sin reglas ajenas tendría que fingir una de las otras.
  none,

  /// Existe y sigue activa: el juego queda alcanzable en las redes de esta
  /// máquina que Kanpachi no controla, con Kanpachi cerrado.
  ///
  /// **No afecta a la sala**, y decía lo contrario. La compuerta es un bloqueo
  /// duro de WFP acotado al adaptador virtual, y en WFP un Block gana sobre
  /// cualquier permiso ajeno: dentro de la sala esta regla no consigue nada y a
  /// un expulsado lo sigue tapando. Lo que queda expuesto es la red de casa,
  /// justamente porque la compuerta no sale del adaptador virtual.
  open,

  /// Desactivada mientras dure la sala, y se restaura al salir.
  disabled,

  /// El usuario decidió dejarla. Se respeta y no se vuelve a preguntar.
  kept,
}

/// Una sala viva.
@immutable
class Room {
  const Room({
    required this.name,
    required this.code,
    required this.members,
    required this.selfIsHost,
    this.link = '',
    this.seed,
    this.game,
    this.missingGameId,
    this.localIp,
    this.subnet,
    this.hostName,
    this.hostLeft = false,
    this.hostGoneFor,
    this.reconnectingFor,
    this.network = ConnState.connected,
    // **`none` and not `open`, and that is a correctness fix.**
    //
    // The wire does NOT carry this: it comes from a separate query,
    // `foreign_rules_for`, which only some paths make. With `open` as the
    // default, every room that had not been asked about came back asserting
    // there was an open foreign rule — so the warning appeared after any
    // refresh, naming the active game, with nobody having looked. A default
    // that claims a finding is a finding invented by a constructor.
    this.foreignRule = ForeignRuleState.none,
    this.foreignRuleClass = RuleClass.game,
    this.foreignRuleProgram,
  });

  /// Builds a room out of a `RoomView`.
  ///
  /// [game] comes from OUTSIDE because the wire only carries the game's id and
  /// name, never its ports, and [gameAddress] needs a port. Whoever calls this
  /// already fetched the catalog, so resolving the id belongs there. Passing it
  /// as an argument keeps the dependency visible instead of hiding it behind a
  /// global the entity would have to reach for.
  factory Room.fromJson(Map<String, Object?> json, {Game? game}) {
    final int ausente = (json['host_gone_for_ms'] as num? ?? 0).toInt();
    final int reconectando = (json['reconnecting_for_ms'] as num? ?? 0).toInt();
    final Object? peers = json['peers'];

    return Room(
      name: json['name'] as String? ?? '',
      code: json['code'] as String? ?? '',
      link: json['link'] as String? ?? '',
      seed: json['seed'] as String?,
      members: <Member>[
        if (peers is List<Object?>)
          for (final Object? p in peers)
            if (p is Map<String, Object?>) Member.fromJson(p),
      ],
      selfIsHost: RoomRole.fromWire(json['role'] as String?) == RoomRole.host,
      game: game,
      missingGameId: json['missing_game'] as String?,
      localIp: json['local_ip'] as String?,
      subnet: json['subnet'] as String?,
      // Inverted on purpose: the daemon says who IS there, and the screen talks
      // about who left.
      hostLeft: !(json['host_present'] as bool? ?? true),
      hostGoneFor: ausente > 0 ? Duration(milliseconds: ausente) : null,
      reconnectingFor: reconectando > 0
          ? Duration(milliseconds: reconectando)
          : null,
      // Unknown state counts as idle, which is the only safe reading: it
      // promises nothing about there being a room.
      network: ConnState.fromWire(json['conn'] as String?) ?? ConnState.idle,
    );
  }

  final String name;

  /// El invite ID en su forma legible, con guiones.
  final String code;

  /// El enlace completo que se copia y se pega en Telegram.
  ///
  /// Lleva el seed de la sala en el host y la clave de la tarjeta en el
  /// fragmento, y las dos partes hacen falta: sin el host, quien se autohospeda
  /// reparte enlaces al servidor de otro; sin la clave, la página no puede
  /// descifrar la tarjeta y quien lo reciba ve «una sala de Kanpachi» en vez del
  /// nombre y de quién le invitó.
  ///
  /// **Viene armado del daemon.** No se compone acá, y no es una preferencia: la
  /// clave de la tarjeta no viaja en ningún otro campo, así que un enlace
  /// compuesto en la pantalla solo puede salir sin ella. Vacío cuando el daemon
  /// no lo mandó, y entonces el botón de copiar no se dibuja: pegar una forma a
  /// medias es peor que no ofrecer el botón.
  final String link;

  final List<Member> members;
  final bool selfIsHost;

  /// El seed del código, que viaja junto a él.
  ///
  /// Hace falta para volver a la última sala: un código sin su seed no se puede
  /// resolver, porque el seed es parte de a dónde hay que preguntar.
  final String? seed;

  /// El perfil que la sala reanudada pide y este catálogo no tiene.
  ///
  /// Pasa al reabrir una sala guardada tras desinstalar el juego o borrar un
  /// perfil propio. La sala está entera; lo que falta es saber qué puertos
  /// abrir.
  final String? missingGameId;

  /// Tu dirección dentro de la red virtual.
  ///
  /// Viene aparte de la tabla de miembros porque justo después de crear la
  /// sala esa tabla puede venir vacía, y la pantalla igual quiere enseñarla.
  final String? localIp;

  /// El rango que la sala eligió. Es diagnóstico, no va en la tarjeta.
  final String? subnet;

  /// Cuánto lleva el host ausente, y por qué importa: a los veinte minutos la
  /// sala se cierra sola. Sin esta cuenta no se puede avisar antes.
  final Duration? hostGoneFor;

  /// Lo mismo para el túnel, que está acotado a diez minutos.
  final Duration? reconnectingFor;

  /// `null` es una sala sin juego, que es un estado normal y no un error: la
  /// sala se crea vacía y el juego se elige adentro.
  final Game? game;

  final String? hostName;

  /// El host cerró su lado. La sala sigue en pie, pero si el juego corría en
  /// su PC no hay a qué conectarse.
  final bool hostLeft;

  final ConnState network;
  final ForeignRuleState foreignRule;

  /// De qué es la regla ajena que se encontró.
  ///
  /// Decide qué se le cuenta al usuario y si puede despacharla. Una de más para
  /// el juego se ofrece desactivar y se puede dejar; una de control remoto hay
  /// que resolverla antes de abrir la sala, porque entrega teclado, pantalla y
  /// archivos a cualquiera que tenga el código, y el código no es un secreto.
  final RuleClass foreignRuleClass;

  /// El programa al que apunta esa regla, para poder nombrarlo.
  ///
  /// `null` cuando el daemon no lo mandó. La pantalla no inventa uno: sin
  /// nombre, el aviso se muestra igual y sin el detalle.
  final String? foreignRuleProgram;

  /// Si la regla ajena impide abrir la sala.
  ///
  /// Se deriva de la clase acá y no viene aparte porque el que decide es el
  /// daemon: esta es la copia de su veredicto. Bloquean dos clases, por razones
  /// distintas del mismo peso: el control remoto entrega teclado, pantalla y
  /// archivos; un permiso ajeno sobre nuestro adaptador abre la red virtual por
  /// fuera de Kanpachi, y la compuerta no lo tapa porque los dos son permisos.
  bool get foreignRuleBlocks =>
      foreignRule == ForeignRuleState.open &&
      (foreignRuleClass == RuleClass.remoteControl ||
          foreignRuleClass == RuleClass.onOurAdapter);

  /// La dirección que se pega en el juego: la del host y el puerto del juego.
  String get gameAddress {
    final Member host = members.firstWhere(
      (Member m) => m.isHost,
      orElse: () => members.first,
    );
    return '${host.address} : ${game?.primaryPort ?? ''}';
  }

  Room copyWith({
    String? name,
    List<Member>? members,
    Game? game,
    bool clearGame = false,
    bool? hostLeft,
    ConnState? network,
    ForeignRuleState? foreignRule,
    RuleClass? foreignRuleClass,
    String? foreignRuleProgram,
  }) => Room(
    name: name ?? this.name,
    code: code,
    link: link,
    members: members ?? this.members,
    selfIsHost: selfIsHost,
    seed: seed,
    game: clearGame ? null : (game ?? this.game),
    missingGameId: missingGameId,
    localIp: localIp,
    subnet: subnet,
    hostName: hostName,
    hostLeft: hostLeft ?? this.hostLeft,
    hostGoneFor: hostGoneFor,
    reconnectingFor: reconnectingFor,
    network: network ?? this.network,
    foreignRule: foreignRule ?? this.foreignRule,
    foreignRuleClass: foreignRuleClass ?? this.foreignRuleClass,
    foreignRuleProgram: foreignRuleProgram ?? this.foreignRuleProgram,
  );
}
