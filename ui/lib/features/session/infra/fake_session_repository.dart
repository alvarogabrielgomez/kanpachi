import 'dart:math';

import 'package:kanpachi_ui/features/session/domain/entities/game.dart';
import 'package:kanpachi_ui/features/session/domain/entities/room.dart';
import 'package:kanpachi_ui/features/session/domain/repositories/session_repository.dart';

/// La implementación de mentira, mientras el daemon no existe.
///
/// Sirve para dos cosas y no hay que confundirlas: hoy es lo único que hay, y
/// mañana, cuando exista el named pipe, sigue siendo lo que hace testeables
/// las pantallas sin levantar un servicio de Windows ni una red virtual.
///
/// Las esperas artificiales están puestas a propósito. Crear una sala tarda de
/// verdad — hay que levantar la red y hablar con el seed — y una UI que se ve
/// instantánea acá y lenta en producción esconde justo los estados que hay que
/// diseñar bien.
class FakeSessionRepository implements SessionRepository {
  FakeSessionRepository({Random? random}) : _random = random ?? Random();

  final Random _random;

  static const Duration _createDelay = Duration(milliseconds: 1700);
  static const Duration _joinDelay = Duration(milliseconds: 1600);
  static const Duration _applyDelay = Duration(milliseconds: 1500);
  static const Duration _closeDelay = Duration(milliseconds: 900);

  final List<Game> _manual = <Game>[];

  static final List<Game> _catalog = <Game>[
    _game('Project Zomboid', '16261-16262', PortProtocol.udp, installed: true),
    _game('Minecraft (Java)', '25565', PortProtocol.tcp, installed: true),
    _game('Terraria', '7777', PortProtocol.tcp, installed: true),
    _game('Valheim', '2456-2458', PortProtocol.udp),
    _game('Factorio', '34197', PortProtocol.udp),
    _game('Stardew Valley', '24642', PortProtocol.udp),
    _game('Left 4 Dead 2', '27015', PortProtocol.udp),
    _game("Don't Starve Together", '10999', PortProtocol.udp),
    _game('Killing Floor 2', '7777', PortProtocol.udp),
    _game('ARK: Survival Evolved', '7777', PortProtocol.udp),
    _game('Rust', '28015', PortProtocol.udp),
    _game('7 Days to Die', '26900', PortProtocol.udp),
    _game('Barotrauma', '27015', PortProtocol.udp),
    _game('Satisfactory', '7777', PortProtocol.udp),
    _game('Core Keeper', '27015', PortProtocol.udp),
    _game('Palworld', '8211', PortProtocol.udp),
    _game('V Rising', '9876-9877', PortProtocol.udp),
    _game('Unturned', '27015', PortProtocol.udp),
  ];

  static Game _game(
    String name,
    String range,
    PortProtocol protocol, {
    bool installed = false,
  }) =>
      Game(
        name: name,
        installed: installed,
        rules: <PortRule>[PortRule(range: range, protocol: protocol)],
      );

  @override
  Future<List<Game>> catalog() async => <Game>[..._manual, ..._catalog];

  @override
  Future<List<Game>> installedGames() async =>
      _catalog.where((Game g) => g.installed).toList();

  @override
  Future<Room> createRoom({required String name, Game? game}) async {
    await Future<void>.delayed(_createDelay);
    return Room(
      name: name,
      code: _newCode(),
      selfIsHost: true,
      game: game,
      members: const <Member>[
        Member(
          name: 'Alvaro',
          address: '100.87.3.1',
          path: PeerPath.self,
          isHost: true,
          isSelf: true,
        ),
        Member(
          name: 'Gabriel',
          address: '100.87.3.2',
          path: PeerPath.direct,
          latencyMs: 45,
        ),
        Member(
          name: 'Santiago',
          address: '100.87.3.3',
          path: PeerPath.relay,
          latencyMs: 140,
        ),
        Member(
          name: 'Humberto',
          address: '100.87.3.4',
          path: PeerPath.direct,
          latencyMs: 28,
        ),
      ],
    );
  }

  @override
  Future<Room> joinRoom(String inviteId) async {
    await Future<void>.delayed(_joinDelay);
    return Room(
      name: 'La Guarida',
      code: inviteId,
      selfIsHost: false,
      hostName: 'Humberto',
      game: _catalog.first,
      members: const <Member>[
        Member(
          name: 'Humberto',
          address: '100.87.3.1',
          path: PeerPath.direct,
          latencyMs: 28,
          isHost: true,
        ),
        Member(
          name: 'Gabriel',
          address: '100.87.3.2',
          path: PeerPath.direct,
          latencyMs: 45,
        ),
        Member(
          name: 'Santiago',
          address: '100.87.3.3',
          path: PeerPath.relay,
          latencyMs: 140,
        ),
        Member(
          name: 'Alvaro',
          address: '100.87.3.4',
          path: PeerPath.self,
          isSelf: true,
        ),
      ],
    );
  }

  @override
  Future<Room> setGame(Room room, Game? game) async {
    await Future<void>.delayed(game == null ? _closeDelay : _applyDelay);
    return room.copyWith(
      game: game,
      clearGame: game == null,
      foreignRule: ForeignRuleState.open,
    );
  }

  @override
  Future<Room> renameRoom(Room room, String name) async =>
      room.copyWith(name: name);

  @override
  Future<Room> kick(Room room, Member member) async => room.copyWith(
        members: room.members
            .where((Member m) => m.name != member.name)
            .toList(growable: false),
      );

  @override
  Future<Room> renewCode(Room room) async => Room(
        name: room.name,
        code: _newCode(),
        members: room.members,
        selfIsHost: room.selfIsHost,
        game: room.game,
        hostName: room.hostName,
        network: room.network,
        foreignRule: room.foreignRule,
      );

  @override
  Future<Room> resolveForeignRule(Room room, {required bool disable}) async =>
      room.copyWith(
        foreignRule:
            disable ? ForeignRuleState.disabled : ForeignRuleState.kept,
      );

  @override
  Future<void> leaveRoom(Room room) async {}

  @override
  Future<Game> saveManualGame(Game game) async {
    _manual.insert(0, game);
    return game;
  }

  /// El alfabeto es el mismo que el de `core/domain` en Go: sin 0, 1, I ni O,
  /// para que no haya pareja ambigua al dictar un código por voz.
  static const String _alphabet = '23456789ABCDEFGHJKLMNPQRSTUVWXYZ';

  String _newCode() {
    final String raw = List<String>.generate(
      8,
      (_) => _alphabet[_random.nextInt(_alphabet.length)],
    ).join();
    return '${raw.substring(0, 4)}-${raw.substring(4)}';
  }
}
