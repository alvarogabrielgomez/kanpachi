import 'dart:math';

import 'package:kanpachi_ui/core/messages/message_keys.dart';
import 'package:kanpachi_ui/features/session/domain/entities/canary.dart';
import 'package:kanpachi_ui/features/session/domain/entities/exposure.dart';
import 'package:kanpachi_ui/features/session/domain/entities/game.dart';
import 'package:kanpachi_ui/features/session/domain/entities/health.dart';
import 'package:kanpachi_ui/features/session/domain/entities/probe.dart';
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
  }) => Game(
    id: slugForProfile(name),
    name: name,
    installed: installed,
    origin: GameOrigin.builtin,
    rules: <PortRule>[PortRule(range: range, protocol: protocol)],
  );

  @override
  Future<List<Game>> catalog() async => <Game>[..._manual, ..._catalog];

  @override
  Future<List<Game>> installedGames() async =>
      _catalog.where((Game g) => g.installed).toList();

  @override
  Future<Room> createRoom({
    required String name,
    required String nickname,
    Game? game,
  }) async {
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
  Future<Room> joinRoom(String inviteId, {required String nickname}) async {
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
        foreignRule: disable
            ? ForeignRuleState.disabled
            : ForeignRuleState.kept,
      );

  @override
  Future<void> leaveRoom(Room room) async {}

  /// Una medición plausible, con el hueco del canal incluido.
  ///
  /// Lleva la fila del canal de control a propósito: es la que el usuario no va
  /// a encontrar en el perfil de su juego, así que es justo la que la pantalla
  /// tiene que saber explicar sin que nadie tenga una sala real abierta.
  @override
  Future<ExposureReport> exposure() async {
    await Future<void>.delayed(const Duration(milliseconds: 400));
    return ExposureReport(
      measured: true,
      measuredAt: DateTime.now(),
      gate: GateState.present,
      ports: const <ExposedPort>[
        ExposedPort(
          proto: 'udp',
          from: 16261,
          to: 16262,
          applied: true,
          isControl: false,
          reachableBy: <String>['100.64.1.5', '100.64.1.6'],
        ),
        ExposedPort(
          proto: 'tcp',
          from: 57623,
          to: 57623,
          applied: true,
          isControl: true,
          reachableBy: <String>['100.64.1.5', '100.64.1.6'],
        ),
      ],
    );
  }

  /// El sondeo de mentira sale LIMPIO, y con la referencia contestando.
  ///
  /// Limpio y no con una fuga porque el estado normal es ese, y una pantalla de
  /// desarrollo que enseña siempre la alarma hace que la alarma deje de leerse.
  /// La fuga se prueba en el test de la pantalla, que es donde toca.
  @override
  Future<ProbeReport> probeHost() async {
    await Future<void>.delayed(const Duration(milliseconds: 900));
    return ProbeReport(
      measured: true,
      verdict: ProbeVerdict.sealed,
      target: '100.64.1.1',
      name: 'alvaro',
      measuredAt: DateTime.now(),
      results: const <ProbeResult>[
        ProbeResult(
          port: 445,
          kind: ProbeKind.forbidden,
          label: 'compartir archivos (SMB)',
          outcome: ProbeOutcome.silent,
        ),
        ProbeResult(
          port: 3389,
          kind: ProbeKind.forbidden,
          label: 'Escritorio remoto',
          outcome: ProbeOutcome.silent,
        ),
        ProbeResult(
          port: 5938,
          kind: ProbeKind.forbidden,
          label: 'TeamViewer',
          outcome: ProbeOutcome.silent,
        ),
        ProbeResult(
          port: 16261,
          kind: ProbeKind.game,
          label: 'Project Zomboid',
          outcome: ProbeOutcome.answered,
          rttMs: 14,
        ),
        ProbeResult(
          port: 57623,
          kind: ProbeKind.reference,
          label: 'el canal de la sala',
          outcome: ProbeOutcome.answered,
          rttMs: 11,
        ),
      ],
    );
  }

  /// La salud de mentira llega CON la alarma del canario puesta.
  ///
  /// Al revés que [probeHost], que sale limpio a propósito, y la asimetría tiene
  /// motivo: aquel se dispara con un botón y se puede ver en los dos estados
  /// cuando uno quiera, y esto llega solo. Sin la alarma puesta acá, la banda de
  /// la Protección Kanpachi y su botón no se pueden ver nunca sin un daemon y
  /// una compuerta rota de verdad, o sea que se romperían sin que nadie lo
  /// notara.
  ///
  /// Las otras dos son las que la portada enseñaba antes con una lista fija
  /// escrita a mano. Ahora salen de acá, que es de donde van a salir siempre.
  @override
  Future<HealthReport> health() async {
    await Future<void>.delayed(const Duration(milliseconds: 250));
    return _health;
  }

  /// Reponer arregla LO SUYO y nada más.
  ///
  /// Se va la alarma del canario y se quedan las otras dos, y eso no es una
  /// simplificación del falso: es la verdad. Volver a escribir la compuerta no
  /// enciende el Firewall de Windows ni cierra un puerto del router.
  @override
  Future<HealthReport> reapplyProtection() async {
    await Future<void>.delayed(const Duration(milliseconds: 700));
    _health = HealthReport(
      alerts: _health.alerts
          .where((HealthAlert a) => a.kind != AlertKind.canaryLeaking)
          .toList(growable: false),
      canary: CanaryCheck(
        measured: true,
        verdict: CanaryVerdict.clean,
        port: _health.canary.port,
        measuredAt: DateTime.now(),
        asked: _health.canary.asked,
      ),
    );
    return _health;
  }

  HealthReport _health = HealthReport(
    alerts: const <HealthAlert>[
      HealthAlert(wire: 'firewall_off', kind: AlertKind.firewallOff),
      HealthAlert(wire: 'router_mapping', kind: AlertKind.routerMapping),
      HealthAlert(wire: 'canary_leaking', kind: AlertKind.canaryLeaking),
    ],
    canary: CanaryCheck(
      measured: true,
      verdict: CanaryVerdict.leaking,
      port: 51234,
      touched: true,
      measuredAt: DateTime.now(),
      asked: const <String>['Gabriel', 'Santiago', 'Humberto'],
      answers: const <CanaryAnswer>[
        CanaryAnswer(
          from: 'Gabriel',
          tcp: ProbeOutcome.answered,
          udp: ProbeOutcome.silent,
        ),
      ],
    ),
  );

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
