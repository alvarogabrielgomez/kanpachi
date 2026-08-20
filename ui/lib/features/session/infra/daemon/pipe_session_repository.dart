import 'package:kanpachi_ui/core/messages/message_keys.dart';
import 'package:kanpachi_ui/features/session/domain/entities/exposure.dart';
import 'package:kanpachi_ui/features/session/domain/entities/game.dart';
import 'package:kanpachi_ui/features/session/domain/entities/health.dart';
import 'package:kanpachi_ui/features/session/domain/entities/last_room.dart';
import 'package:kanpachi_ui/features/session/domain/entities/engine_info.dart';
import 'package:kanpachi_ui/features/session/domain/entities/own_seed.dart';
import 'package:kanpachi_ui/features/session/domain/entities/pending_invite.dart';
import 'package:kanpachi_ui/features/session/domain/entities/saved_room.dart';
import 'package:kanpachi_ui/features/session/domain/entities/probe.dart';
import 'package:kanpachi_ui/features/session/domain/entities/progress.dart';
import 'package:kanpachi_ui/features/session/domain/entities/room.dart';
import 'package:kanpachi_ui/features/session/domain/daemon_failure.dart';
import 'package:kanpachi_ui/features/session/domain/repositories/session_repository.dart';
import 'package:kanpachi_ui/features/session/infra/daemon/daemon_client.dart';
import 'package:kanpachi_ui/features/session/infra/daemon/daemon_connector.dart';
import 'package:kanpachi_ui/features/session/infra/daemon/daemon_methods.dart';

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
  PipeSessionRepository(this._connector, {DateTime Function()? now})
    : _now = now ?? DateTime.now;

  final DaemonConnector _connector;

  /// El reloj de la caché de reglas ajenas. Inyectable para poder medir su
  /// caducidad sin esperarla.
  final DateTime Function() _now;

  /// El catálogo, para poder resolver el id que manda el cable.
  List<Game> _catalogo = const <Game>[];

  /// Las reglas ajenas tal como llegaron, para poder devolverlas enteras.
  ///
  /// Se guardan crudas porque en Windows una regla no tiene identificador
  /// estable: `suspend_foreign_rules` espera de vuelta la misma vista que
  /// mandó, y quedarse solo con lo que la pantalla muestra la haría
  /// irreconstruible.
  List<Map<String, Object?>> _ajenas = const <Map<String, Object?>>[];

  /// Lo que la última auditoría dejó pegado a la sala, para volver a pegarlo
  /// sin repetir el barrido.
  ForeignRuleState _estadoAjenas = ForeignRuleState.none;
  RuleClass _claseAjenas = RuleClass.game;
  String? _programaAjeno;

  /// Con qué se calculó lo anterior: cuándo, para qué juego, y con quiénes
  /// dentro. Los tres son disparadores; ver [_hayQueBarrer].
  DateTime? _barridoEn;
  String? _barridoDelJuego;
  Set<String> _barridoConMiembros = const <String>{};

  /// Cada cuánto se repite el barrido cuando no pasa nada más.
  ///
  /// Dos minutos, y el número lo puso el usuario mirando su parque de
  /// máquinas. Antes esto corría con el latido, o sea SESENTA veces por cada
  /// una de estas.
  static const Duration _cadenciaAjenas = Duration(minutes: 2);

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

  // ----------------------------------------------------------------- proceso

  @override
  Future<void> quitEverything() async {
    // **Sin reintento y tragándose el fallo.** Esta llamada mata el proceso que
    // la está haciendo: el daemon contesta, apaga, y por el camino se lleva
    // esta ventana con el job. Que el enlace se corte a mitad es el resultado
    // ESPERADO, no un error, y reintentar sería pedir un segundo apagado a un
    // daemon que ya se está apagando.
    try {
      final DaemonClient c = await _connector.client();
      await c.call(DaemonMethods.shutdown);
    } on DaemonUnreachable {
      // El enlace se cayó, que es exactamente lo que pasa cuando funciona.
    } on DaemonError {
      // El daemon dijo que no puede. En modo consola no hospeda la interfaz y
      // eso es legítimo; la ventana se cierra igual, que es lo que el usuario
      // pidió.
    }
  }

  @override
  Future<bool> autostart({bool? enabled}) async {
    final Map<String, Object?> r = await _mapa(
      DaemonMethods.autostart,
      <String, Object?>{'enabled': ?enabled},
    );
    return r['enabled'] as bool? ?? false;
  }

  @override
  Future<OwnSeed> ownSeed({String? seed}) async {
    final Map<String, Object?> r = await _mapa(
      DaemonMethods.ownSeed,
      <String, Object?>{'seed': ?seed},
    );
    return OwnSeed(
      configured: r['seed'] as String? ?? '',
      suggested: r['suggested'] as String? ?? '',
    );
  }

  @override
  Future<MachineNickname> nickname({String? nickname}) async {
    final Map<String, Object?> r = await _mapa(
      DaemonMethods.nickname,
      <String, Object?>{'nickname': ?nickname},
    );
    return MachineNickname(
      chosen: r['nickname'] as String? ?? '',
      suggested: r['suggested'] as String? ?? '',
    );
  }

  @override
  Future<EngineInfo> engineInfo() async {
    final Map<String, Object?> r = await _mapa(DaemonMethods.engineInfo, null);
    return EngineInfo(
      build: r['build'] as String? ?? '',
      lib: r['lib'] as String? ?? '',
    );
  }

  @override
  Future<void> seedPassword(String password) async {
    // `_mapa` and not a typed reader: the answer is an empty acknowledgement on
    // purpose, so there is nothing to read out of it. See
    // `protocol.MethodSeedPassword`.
    await _mapa(DaemonMethods.seedPassword, <String, Object?>{
      'password': password,
    });
  }

  @override
  Future<bool> cancel() async {
    // Over a SPARE connection, and here it is not an optimisation, it is the
    // only way it can work: the connection this repository normally uses is
    // parked inside the very `create_room` this is trying to cut short, and a
    // connection's server loop is sequential.
    final DaemonClient c = await _connector.spare();
    try {
      final Map<String, Object?> r =
          await c.call(DaemonMethods.cancel) as Map<String, Object?>? ??
          <String, Object?>{};
      return r['canceled'] == true;
    } finally {
      await c.close();
    }
  }

  @override
  Future<Progress> progress() async {
    // Over a SPARE connection, and that is the whole point: the one this
    // repository normally uses is parked inside `create_room`, and a
    // connection's server loop is sequential. Down that one, this would be
    // answered when there is nothing left to watch.
    //
    // Opened and closed per poll. Keeping it alive would hold one of the
    // daemon's eight slots for as long as the app is open, to be used for a
    // few seconds every time somebody creates a room.
    final DaemonClient c = await _connector.spare();
    try {
      final Map<String, Object?> r =
          await c.call(DaemonMethods.progress) as Map<String, Object?>? ??
          <String, Object?>{};
      return Progress.fromJson(r);
    } finally {
      await c.close();
    }
  }

  // -------------------------------------------------------------------- sala

  @override
  Future<Room> createRoom({
    required String name,
    required String nickname,
    Game? game,
    bool replace = false,
  }) async {
    final Map<String, Object?> creada = await _mapa(
      DaemonMethods.createRoom,
      <String, Object?>{
        'nickname': nickname,
        'name': name,
        if (replace) 'replace': true,
      },
    );
    if (game == null) return _sala(creada);
    // Crear no lleva juego, por decisión 20: la sala nace vacía y el juego se
    // elige adentro. Cuando la pantalla ya trae uno, son dos llamadas.
    return setGame(await _sala(creada), game);
  }

  @override
  Future<Room> joinRoom(
    String inviteId, {
    required String nickname,
    bool replace = false,
  }) async => _sala(
    await _mapa(DaemonMethods.joinRoom, <String, Object?>{
      'code': inviteId,
      'nickname': nickname,
      // Se omite en falso, que es lo que el daemon espera: su cero rechaza, y
      // eso hace que olvidarlo sea seguro en vez de destructivo.
      if (replace) 'replace': true,
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
  Future<Room> renameRoom(Room room, String name) async => _sala(
    await _mapa(DaemonMethods.renameRoom, <String, Object?>{'name': name}),
  );

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

  /// Resuelve el aviso, y deja la caché diciendo lo mismo que la pantalla.
  ///
  /// Lo decidido acá se sabe sin preguntar: dejarla puesta es `kept` y
  /// desactivarla es `disabled`, y en los dos casos el almacén de reglas acaba
  /// de quedar como este código dice. Volver a barrer para enterarse de lo que
  /// uno mismo hizo sería pagar mil llamadas COM por una respuesta conocida, y
  /// además pisaría la decisión del usuario con un `none` a los dos segundos.
  @override
  Future<Room> resolveForeignRule(Room room, {required bool disable}) async {
    if (!disable) {
      _ajenas = const <Map<String, Object?>>[];
      _recuerda(ForeignRuleState.kept);
      return room.copyWith(foreignRule: ForeignRuleState.kept);
    }
    if (_ajenas.isEmpty) {
      _recuerda(ForeignRuleState.none);
      return room.copyWith(foreignRule: ForeignRuleState.none);
    }
    await _mapa(DaemonMethods.suspendForeignRules, <String, Object?>{
      'rules': _ajenas,
    });
    _ajenas = const <Map<String, Object?>>[];
    _recuerda(ForeignRuleState.disabled);
    return room.copyWith(foreignRule: ForeignRuleState.disabled);
  }

  /// Fija el estado resuelto y estira el plazo, sin tocar los otros
  /// disparadores: cambiar de juego o que entre alguien siguen barriendo.
  void _recuerda(ForeignRuleState estado) {
    _estadoAjenas = estado;
    _claseAjenas = RuleClass.game;
    _programaAjeno = null;
    _barridoEn = _now();
  }

  @override
  Future<void> leaveRoom(Room room) async {
    await _mapa(DaemonMethods.leaveRoom);
  }

  @override
  Future<Room?> currentRoom() async {
    final Map<String, Object?> st = await _mapa(DaemonMethods.status);
    // Sin código no hay sala: `status` contesta igual cuando no hay ninguna, y
    // el estado vacío no es un error, es la portada.
    if ((st['code'] as String? ?? '').isEmpty) return null;
    // Foreign rules are resolved HERE too, and not only when the game changes.
    // They are not on the wire: they come from a second query, so a room that
    // skips it carries whatever the entity defaults to. Asking on every
    // refresh is what makes the warning true at the moment it is painted —
    // rules appear when a game installs itself and disappear when somebody
    // removes them, neither of which passes through this app.
    return _conReglasAjenas(await _sala(st));
  }

  @override
  Future<PendingInvite?> pendingInvite() async {
    final Map<String, Object?> v = await _mapa(DaemonMethods.pendingInvite);
    // Sin enlace no hay nada pendiente, que es la respuesta normal: esto se
    // pregunta en cada latido y casi siempre no hay nada. El daemon contesta un
    // objeto vacío en vez de un error, porque «no hay» no es un fallo.
    final String link = v['link'] as String? ?? '';
    if (link.isEmpty) return null;
    return PendingInvite.fromJson(v);
  }

  @override
  Future<PendingInvite?> previewInvite(String link) async {
    if (link.trim().isEmpty) return null;
    try {
      final Map<String, Object?> v = await _mapa(
        DaemonMethods.previewInvite,
        <String, Object?>{'link': link},
      );
      if ((v['code'] as String? ?? '').isEmpty) return null;
      return PendingInvite.fromJson(v);
    } on Object {
      // Preguntar esto es un extra: lo que se pierde si falla es la huella del
      // host en el diálogo, jamás la entrada. Se traga a propósito, y el
      // registro caído se vuelve a encontrar al entrar, con su mensaje, que es
      // el sitio donde una caída sí tiene que verse.
      return null;
    }
  }

  @override
  Future<SavedRoom?> savedRoom() async {
    final Map<String, Object?> v = await _mapa(DaemonMethods.savedRoom);
    // El daemon contesta `{"found": false}` cuando no hay nada, que es la
    // respuesta normal: esto se pregunta en cada latido y casi siempre no hay
    // ninguna sala del arranque anterior. No es un error.
    if ((v['found'] as bool?) != true) return null;
    final Object? sala = v['room'];
    if (sala is! Map<String, Object?>) return null;
    final SavedRoom pendiente = SavedRoom.fromJson(sala);
    // Sin código no hay nada que ofrecer, y ofrecer una sala sin código dejaría
    // un diálogo del que no se puede salir haciendo lo que propone.
    return pendiente.understood ? pendiente : null;
  }

  @override
  Future<LastRoom?> lastRoom() async {
    final Map<String, Object?> v = await _mapa(DaemonMethods.lastRoom);
    // `{"found": false}` es la respuesta normal: casi siempre no se estuvo en
    // ninguna sala ajena. No es un error.
    if ((v['found'] as bool?) != true) return null;
    final Object? sala = v['room'];
    if (sala is! Map<String, Object?>) return null;
    final LastRoom last = LastRoom.fromJson(sala);
    return last.understood ? last : null;
  }

  @override
  Future<void> forgetLastRoom() async {
    await _mapa(DaemonMethods.forgetLastRoom);
  }

  @override
  Future<Room> resumeSavedRoom() async =>
      _conReglasAjenas(await _sala(await _mapa(DaemonMethods.resumeRoom)));

  @override
  Future<void> discardSavedRoom() async {
    await _mapa(DaemonMethods.discardSavedRoom);
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

  @override
  Future<HealthReport> quarantine({bool? enabled}) async =>
      HealthReport.fromJson(
        await _mapa(DaemonMethods.quarantine, <String, Object?>{
          if (enabled != null) 'set': enabled ? 'on' : 'off',
        }),
      );

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

  /// Pega a la sala lo que dijo la última auditoría, barriendo solo si toca.
  ///
  /// # Por qué esto NO va con el latido
  ///
  /// Iba, y era el trabajo más caro de la app con diferencia. El barrido le
  /// pide al almacén de reglas del Firewall de Windows TODAS las reglas de la
  /// máquina y las recorre buscando el ejecutable del juego y las herramientas
  /// de control remoto: **1009 reglas revisadas, medido en una máquina de
  /// verdad**, y cada revisión es una ristra de llamadas COM al servicio del
  /// firewall. Corriendo cada dos segundos, eso es medio millón de llamadas por
  /// cada veinte minutos de sala.
  ///
  /// Cuesta de tres formas distintas, y las tres importan:
  ///
  ///  - **CPU en máquinas viejas**, que es donde se juega a esto.
  ///  - **Un antivirus mirando**: un proceso que enumera el almacén de reglas
  ///    del firewall sin parar tiene la forma exacta de lo que un antivirus
  ///    está entrenado para marcar.
  ///  - **El canal con el daemon**, que es uno solo y se atiende de a una
  ///    petición. Un barrido lento encola detrás lo barato —`status`, el
  ///    enlace pendiente— hasta que revientan su plazo de diez segundos, y la
  ///    app pinta "sin servicio" con el daemon perfectamente vivo. Medido:
  ///    barridos de hasta 5,9 s con la máquina en reposo.
  ///
  /// # Y por qué se puede espaciar sin perder el aviso
  ///
  /// Porque estas reglas **no cambian solas**. Aparecen cuando alguien instala
  /// un juego o cuando el juego se registra en el firewall, y desaparecen
  /// cuando alguien las borra. Son actos de una persona, no eventos de red.
  /// Preguntarlo treinta veces por minuto era releer algo que cambia una vez
  /// por semana.
  Future<Room> _conReglasAjenas(Room sala) async {
    final Game? juego = sala.game;
    if (juego == null) {
      _ajenas = const <Map<String, Object?>>[];
      _olvidarBarrido();
      return sala.copyWith(foreignRule: ForeignRuleState.none);
    }

    final Set<String> miembros = <String>{
      for (final Member m in sala.members) m.address,
    };

    if (!_hayQueBarrer(juego.id, miembros)) return _pegado(sala);

    final List<Object?> crudo = await _lista(
      DaemonMethods.foreignRules,
      <String, Object?>{'game': juego.id},
    );
    _ajenas = <Map<String, Object?>>[
      for (final Object? r in crudo)
        if (r is Map<String, Object?>) r,
    ];
    _barridoEn = _now();
    _barridoDelJuego = juego.id;
    _barridoConMiembros = miembros;

    if (_ajenas.isEmpty) {
      _estadoAjenas = ForeignRuleState.none;
      _claseAjenas = RuleClass.game;
      _programaAjeno = null;
      return _pegado(sala);
    }

    // La que manda es la que bloquea, y si ninguna bloquea la primera. La
    // pantalla enseña una sola, así que enseñar la más grave es lo único que no
    // esconde nada.
    final Map<String, Object?> peor = _ajenas.firstWhere(
      (Map<String, Object?> r) => r['blocking'] == true,
      orElse: () => _ajenas.first,
    );
    _estadoAjenas = ForeignRuleState.open;
    _claseAjenas = RuleClass.fromWire(peor['class'] as String?);
    _programaAjeno = peor['executable'] as String?;
    return _pegado(sala);
  }

  /// La sala con lo que se sabe de las reglas ajenas, sin preguntar nada.
  Room _pegado(Room sala) => sala.copyWith(
    foreignRule: _estadoAjenas,
    foreignRuleClass: _claseAjenas,
    foreignRuleProgram: _programaAjeno,
  );

  /// Los cuatro disparadores, y ninguno más.
  ///
  /// Tres son sucesos y el cuarto es la red que los cubre a todos. Cambiar de
  /// juego cambia qué ejecutable se busca, así que lo anterior deja de
  /// significar nada. Que entre alguien nuevo es el momento en que una regla
  /// abierta pasa de ser un dato a ser una puerta con alguien delante. Y entrar
  /// a la pantalla de la sala es cuando el aviso se mira, que es lo que lo hace
  /// valer.
  bool _hayQueBarrer(String juego, Set<String> miembros) {
    final DateTime? ultimo = _barridoEn;
    if (ultimo == null) return true;
    if (_barridoDelJuego != juego) return true;
    // Solo quien LLEGÓ. Que alguien se vaya no abre ninguna regla, y barrer al
    // salir cada miembro convertiría una sala que se vacía en una ráfaga de
    // barridos.
    if (miembros.any((String m) => !_barridoConMiembros.contains(m))) {
      return true;
    }
    return _now().difference(ultimo) >= _cadenciaAjenas;
  }

  void _olvidarBarrido() {
    _barridoEn = null;
    _barridoDelJuego = null;
    _barridoConMiembros = const <String>{};
    _estadoAjenas = ForeignRuleState.none;
    _claseAjenas = RuleClass.game;
    _programaAjeno = null;
  }

  /// Tira lo sabido para que el siguiente vistazo a la sala vuelva a barrer.
  ///
  /// Lo llama la pantalla de la sala al aparecer. Es un método y no un barrido
  /// directo porque quien arma la sala es [currentRoom], y tener dos sitios
  /// que barren sería tener dos cachés.
  @override
  void recheckForeignRules() => _olvidarBarrido();

  Future<Map<String, Object?>> _mapa(
    String metodo, [
    Map<String, Object?>? params,
  ]) => _conReintento((DaemonClient c) => c.call(metodo, params));

  Future<List<Object?>> _lista(String metodo, [Map<String, Object?>? params]) =>
      _conReintento((DaemonClient c) => c.callList(metodo, params));

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
