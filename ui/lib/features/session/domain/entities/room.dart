import 'package:flutter/foundation.dart';
import 'package:kanpachi_ui/features/session/domain/entities/game.dart';

/// Cómo llega el tráfico hasta un miembro.
enum PeerPath {
  /// Conexión directa, que es lo normal y lo deseable.
  direct('directo'),

  /// A través del seed. Funciona igual, va más lento, y se dice para que
  /// nadie culpe al juego de una latencia que es de la red.
  relay('relay'),

  /// Uno mismo. No hay ruta que describir.
  self('');

  const PeerPath(this.label);

  final String label;
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

/// El estado de la conexión con el resto de la sala.
enum NetworkHealth {
  ok(''),
  degraded('Conexión inestable, reintentando'),
  reconnecting('Reconectando…');

  const NetworkHealth(this.message);

  final String message;
}

/// Qué se hizo con la regla de firewall que dejó el propio juego.
enum ForeignRuleState {
  /// Existe y sigue activa: el juego es alcanzable sin pasar por el control
  /// de Kanpachi, así que expulsar a alguien no lo tapa.
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
    this.game,
    this.hostName,
    this.hostLeft = false,
    this.network = NetworkHealth.ok,
    this.foreignRule = ForeignRuleState.open,
  });

  final String name;

  /// El invite ID en su forma legible, con guiones.
  final String code;

  final List<Member> members;
  final bool selfIsHost;

  /// `null` es una sala sin juego, que es un estado normal y no un error: la
  /// sala se crea vacía y el juego se elige adentro.
  final Game? game;

  final String? hostName;

  /// El host cerró su lado. La sala sigue en pie, pero si el juego corría en
  /// su PC no hay a qué conectarse.
  final bool hostLeft;

  final NetworkHealth network;
  final ForeignRuleState foreignRule;

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
    NetworkHealth? network,
    ForeignRuleState? foreignRule,
  }) =>
      Room(
        name: name ?? this.name,
        code: code,
        members: members ?? this.members,
        selfIsHost: selfIsHost,
        game: clearGame ? null : (game ?? this.game),
        hostName: hostName,
        hostLeft: hostLeft ?? this.hostLeft,
        network: network ?? this.network,
        foreignRule: foreignRule ?? this.foreignRule,
      );
}
