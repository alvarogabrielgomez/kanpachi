import 'package:kanpachi_ui/core/messages/message_keys.dart';
import 'package:kanpachi_ui/features/session/domain/entities/exposure.dart';
import 'package:kanpachi_ui/features/session/domain/entities/game.dart';
import 'package:kanpachi_ui/features/session/domain/entities/health.dart';
import 'package:kanpachi_ui/features/session/domain/entities/probe.dart';
import 'package:kanpachi_ui/features/session/domain/entities/room.dart';
import 'package:kanpachi_ui/features/session/domain/repositories/session_repository.dart';
import 'package:kanpachi_ui/features/session/infra/daemon/daemon_client.dart';
import 'package:kanpachi_ui/features/session/infra/daemon/daemon_codec.dart';
import 'package:kanpachi_ui/features/session/infra/daemon/daemon_connector.dart';
import 'package:kanpachi_ui/features/session/infra/daemon/daemon_methods.dart';
import 'package:kanpachi_ui/features/session/infra/daemon/daemon_transport.dart';

/// El contrato de la UI, hablado contra el daemon de verdad.
///
/// # Lo que hace además de traducir
///
/// **Reintenta UNA vez, y solo lo que nunca llegó.** Sin polling, la conexión
/// se cae sola cada diez minutos de inactividad, así que la primera llamada
/// después de un rato larga la que estaba y abre otra. La línea la marca la
/// escritura: lo que falló antes de escribir no llegó al daemon y repetirlo es
/// gratis; lo que venció después pudo ejecutarse, y repetir `create_room` sería
/// crear una segunda sala.
///
/// **Resuelve el juego contra el catálogo.** El cable manda el id del juego y
/// su nombre, jamás sus puertos, y la dirección que se pega en el juego
/// necesita un puerto. El catálogo se pide una vez y se guarda.
///
/// # Lo que NO hace
///
/// No sondea, no tiene temporizadores y no refresca solo. El daemon es
/// petición y respuesta puro, así que quien decide cuándo volver a preguntar es
/// la pantalla.
class PipeSessionRepository implements SessionRepository {
  PipeSessionRepository(this._connector);

  final DaemonConnector _connector;

  /// El catálogo, para poder resolver el id que manda el cable.
  List<Game> _catalogo = const <Game>[];

  /// Las reglas ajenas tal como llegaron, para poder devolverlas enteras.
  ///
  /// Se guardan crudas porque en Windows una regla no tiene identificador
  /// estable: `suspend_foreign_rules` espera de vuelta la misma vista que
  /// mandó, y quedarse solo con lo que la pantalla muestra la haría
  /// irreconstruible.
  List<Map<String, Object?>> _ajenas = const <Map<String, Object?>>[];

  // ---------------------------------------------------------------- catálogo

  @override
  Future<List<Game>> catalog() async {
    final List<Object?> crudo = await _lista(DaemonMethods.listGames);
    _catalogo = <Game>[
      for (final Object? g in crudo)
        if (g is Map<String, Object?>) Game.fromJson(g),
    ];
    return _catalogo;
  }

  @override
  Future<List<Game>> installedGames() async {
    final List<Game> todos = await catalog();
    return todos.where((Game g) => g.installed).toList(growable: false);
  }

  @override
  Future<Game> saveManualGame(Game game) async {
    await _mapa(DaemonMethods.saveProfile, <String, Object?>{
      'profile': game.toProfileJson(),
      'replace': false,
    });
    // El catálogo se vuelve a pedir en vez de agregarlo a mano: el daemon
    // decide la precedencia entre capas, y adivinarla acá crearía dos verdades.
    await catalog();
    return _catalogo.firstWhere(
      (Game g) => g.id == game.id,
      orElse: () => game,
    );
  }

  // -------------------------------------------------------------------- sala

  @override
  Future<Room> createRoom({
    required String name,
    required String nickname,
    Game? game,
  }) async {
    final Map<String, Object?> creada = await _mapa(
      DaemonMethods.createRoom,
      <String, Object?>{'nickname': nickname, 'name': name},
    );
    if (game == null) return _sala(creada);
    // Crear no lleva juego, por decisión 20: la sala nace vacía y el juego se
    // elige adentro. Cuando la pantalla ya trae uno, son dos llamadas.
    return setGame(await _sala(creada), game);
  }

  @override
  Future<Room> joinRoom(String inviteId, {required String nickname}) async =>
      _sala(
        await _mapa(DaemonMethods.joinRoom, <String, Object?>{
          'code': inviteId,
          'nickname': nickname,
        }),
      );

  @override
  Future<Room> setGame(Room room, Game? game) async {
    // El id vacío es legal del otro lado y cierra todos los puertos.
    final Map<String, Object?> st = await _mapa(
      DaemonMethods.activateProfile,
      <String, Object?>{'game': game?.id ?? ''},
    );
    return _conReglasAjenas(await _sala(st));
  }

  @override
  Future<Room> renameRoom(Room room, String name) async =>
      _sala(await _mapa(DaemonMethods.renameRoom, <String, Object?>{
        'name': name,
      }));

  @override
  Future<Room> renewCode(Room room) async =>
      _sala(await _mapa(DaemonMethods.rotateInviteCode));

  @override
  Future<Room> kick(Room room, Member member) async {
    try {
      return await _sala(
        await _mapa(DaemonMethods.kickMember, <String, Object?>{
          'ip': member.address,
        }),
      );
    } on DaemonError catch (e) {
      // La expulsión a medias trae la sala JUNTO al error, a propósito: la
      // lista se redibuja sin el expulsado y el aviso se muestra igual. Tirar
      // el estado dejaría en pantalla a alguien que ya no está.
      final Map<String, Object?>? conEstado = e.resultMap;
      if (e.resolved == FailureCode.kickPartial && conEstado != null) {
        return _sala(conEstado);
      }
      rethrow;
    }
  }

  @override
  Future<Room> resolveForeignRule(Room room, {required bool disable}) async {
    if (!disable) {
      _ajenas = const <Map<String, Object?>>[];
      return room.copyWith(foreignRule: ForeignRuleState.kept);
    }
    if (_ajenas.isEmpty) {
      return room.copyWith(foreignRule: ForeignRuleState.none);
    }
    await _mapa(DaemonMethods.suspendForeignRules, <String, Object?>{
      'rules': _ajenas,
    });
    _ajenas = const <Map<String, Object?>>[];
    return room.copyWith(foreignRule: ForeignRuleState.disabled);
  }

  @override
  Future<void> leaveRoom(Room room) async {
    await _mapa(DaemonMethods.leaveRoom);
  }

  // ------------------------------------------------------------ diagnósticos

  @override
  Future<ExposureReport> exposure() async {
    try {
      return ExposureReport.fromJson(await _mapa(DaemonMethods.exposure));
    } on Object {
      // No devuelve error a propósito, y el contrato lo dice: la pantalla que
      // lo enseña tiene que decir algo SIEMPRE, y el informe ciego ya expresa
      // "todavía no se sabe". Enseñar la última lista buena sería la mentira
      // que esta pantalla existe para impedir.
      return const ExposureReport.blind();
    }
  }

  @override
  Future<ProbeReport> probeHost() async =>
      ProbeReport.fromJson(await _mapa(DaemonMethods.probeHost));

  @override
  Future<HealthReport> health() async {
    try {
      // No hay método propio: los avisos y el canario viajan dentro del estado,
      // porque los produjo el daemon solo y no hacía falta un segundo canal.
      return HealthReport.fromJson(await _mapa(DaemonMethods.status));
    } on Object {
      return const HealthReport.unknown();
    }
  }

  @override
  Future<HealthReport> reapplyProtection() async =>
      HealthReport.fromJson(await _mapa(DaemonMethods.reapplyProtection));

  // ------------------------------------------------------------------ plomería

  /// Convierte una vista de sala en entidad, resolviendo el juego.
  Future<Room> _sala(Map<String, Object?> json) async {
    final String id = json['game'] as String? ?? '';
    Game? juego;
    if (id.isNotEmpty) {
      if (_catalogo.isEmpty) await catalog();
      for (final Game g in _catalogo) {
        if (g.id == id) {
          juego = g;
          break;
        }
      }
      // El daemon lo conoce y este catálogo no: se arma uno con lo que vino,
      // que es el nombre. Sin puertos la dirección sale sin puerto, y eso es
      // mejor que una sala sin juego.
      juego ??= Game(
        id: id,
        name: json['game_name'] as String? ?? id,
        rules: const <PortRule>[],
      );
    }
    return Room.fromJson(json, game: juego);
  }

  /// Pregunta por las reglas ajenas del juego activo y las pega a la sala.
  Future<Room> _conReglasAjenas(Room sala) async {
    final Game? juego = sala.game;
    if (juego == null) {
      _ajenas = const <Map<String, Object?>>[];
      return sala.copyWith(foreignRule: ForeignRuleState.none);
    }

    final List<Object?> crudo = await _lista(
      DaemonMethods.foreignRules,
      <String, Object?>{'game': juego.id},
    );
    _ajenas = <Map<String, Object?>>[
      for (final Object? r in crudo)
        if (r is Map<String, Object?>) r,
    ];
    if (_ajenas.isEmpty) {
      return sala.copyWith(foreignRule: ForeignRuleState.none);
    }

    // La que manda es la que bloquea, y si ninguna bloquea la primera. La
    // pantalla enseña una sola, así que enseñar la más grave es lo único que no
    // esconde nada.
    final Map<String, Object?> peor = _ajenas.firstWhere(
      (Map<String, Object?> r) => r['blocking'] == true,
      orElse: () => _ajenas.first,
    );
    return sala.copyWith(
      foreignRule: ForeignRuleState.open,
      foreignRuleClass: RuleClass.fromWire(peor['class'] as String?),
      foreignRuleProgram: peor['executable'] as String?,
    );
  }

  Future<Map<String, Object?>> _mapa(
    String metodo, [
    Map<String, Object?>? params,
  ]) => _conReintento(
    (DaemonClient c) => c.call(metodo, params),
  );

  Future<List<Object?>> _lista(
    String metodo, [
    Map<String, Object?>? params,
  ]) => _conReintento((DaemonClient c) => c.callList(metodo, params));

  /// Una llamada, con un solo reintento y solo cuando es seguro.
  Future<T> _conReintento<T>(Future<T> Function(DaemonClient) llamada) async {
    try {
      return await llamada(await _connector.client());
    } on DaemonUnreachable catch (e) {
      if (!e.safeToRetry) rethrow;
      await _connector.drop();
      return llamada(await _connector.client());
    }
  }
}
