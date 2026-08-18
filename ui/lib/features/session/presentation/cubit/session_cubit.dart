import 'dart:async';

import 'package:kanpachi_ui/core/platform/app_preferences.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:kanpachi_ui/features/session/domain/entities/exposure.dart';
import 'package:kanpachi_ui/features/session/domain/daemon_failure.dart';
import 'package:kanpachi_ui/core/messages/message_keys.dart';
import 'package:kanpachi_ui/features/session/domain/entities/action_failure.dart';
import 'package:kanpachi_ui/features/session/domain/entities/game.dart';
import 'package:kanpachi_ui/features/session/domain/entities/progress.dart';
import 'package:kanpachi_ui/features/session/domain/entities/health.dart';
import 'package:kanpachi_ui/features/session/domain/entities/own_seed.dart';
import 'package:kanpachi_ui/features/session/domain/entities/pending_invite.dart';
import 'package:kanpachi_ui/features/session/domain/entities/saved_room.dart';
import 'package:kanpachi_ui/features/session/domain/entities/probe.dart';
import 'package:kanpachi_ui/features/session/domain/entities/room.dart';
import 'package:kanpachi_ui/features/session/domain/repositories/session_repository.dart';
import 'package:kanpachi_ui/features/session/presentation/cubit/session_state.dart';
import 'package:kanpachi_ui/core/timing/app_timing.dart';

/// El estado de la sesión: la sala, el catálogo y lo que se está aplicando.
///
/// Es uno solo para toda la app y no uno por pantalla, a diferencia del resto
/// de cubits de la casa. El motivo: seis pantallas leen y escriben la MISMA
/// sala, y partirlo en seis dejaría seis copias que hay que sincronizar a
/// mano. Cuando exista el daemon, este cubit pasa a ser lo que escucha su
/// stream de eventos, que sigue siendo uno solo por definición.
class SessionCubit extends Cubit<SessionState> {
  SessionCubit(
    this._repository, {

    /// Where the nickname is remembered between runs.
    ///
    /// Null in tests, which build this cubit directly and have nothing to
    /// persist. The name still works in memory; only the remembering is gone.
    AppPreferences? preferences,
    String nickname = '',

    /// Whether to narrate what the daemon is doing. Seeded from the stored
    /// preference, which defaults to on while developing.
    bool verbose = false,

    /// Si esta ventana la abrió el daemon MIENTRAS reabría la sala del arranque
    /// anterior.
    ///
    /// Lo único que hace es decidir la fase de partida, y con ella lo que se
    /// pinta en el PRIMER fotograma: la espera en vez de la portada. No es un
    /// dato de la sala y no se guarda en ningún sitio; el primer latido lo pisa
    /// con lo que el daemon diga de verdad, sea la sala abierta, sea el fallo
    /// de no haber podido reabrirla.
    bool resumingHostedRoom = false,
    // The lint asks for `this._preferences`, which Dart does not allow: a
    // named parameter cannot start with an underscore. The field stays
    // private and the parameter stays nameable.
    // ignore: prefer_initializing_formals
  }) : _preferences = preferences,
       super(
         SessionState(
           nickname: nickname,
           verbose: verbose,
           // `creating` y no una fase propia: reabrir la sala propia ES abrirla,
           // con los mismos pasos y los mismos textos. Una fase nueva sería un
           // quinto caso en cada `switch` que hay sobre esto para decir
           // exactamente lo mismo.
           phase: resumingHostedRoom
               ? SessionPhase.creating
               : SessionPhase.idle,
         ),
       );

  final SessionRepository _repository;
  final AppPreferences? _preferences;

  /// Poll of the daemon's step diary while a long operation runs.
  ///
  /// Only exists while there IS a long operation: every wait starts it and the
  /// end of the wait stops it. It used to depend on [SessionState.verbose] as
  /// well, and no longer does — see [_watchProgress].
  Timer? _pollingProgreso;

  /// Runs one user action and turns any failure into STATE.
  ///
  /// # Why failures do not escape
  ///
  /// Because a screen cannot catch an exception. Before this, an action that
  /// failed either threw into nowhere or was swallowed, and on screen both
  /// look identical: the spinner stops and nothing else changes. The user
  /// cannot tell "it failed" from "it worked and the screen is stale", so they
  /// press again — and pressing again is exactly what must not happen after
  /// something that may have half executed.
  ///
  /// `onFail` is the phase to fall back to. It exists for the two operations
  /// that move the phase forward before they can succeed: creating and
  /// joining. Without it, a failure leaves a spinner spinning towards nowhere.
  Future<void> _try(
    FailedAction action,
    Future<void> Function() body, {
    SessionPhase? onFail,
  }) async {
    if (!isClosed) emit(state.copyWith(clearFailure: true));
    try {
      await body();
    } on DaemonError catch (e) {
      // The daemon answered, and said no. It carries a closed code, so the
      // catalog can say the right sentence instead of a generic one.
      await _fallo(action, e.message, code: e.code, onFail: onFail);
    } on DaemonUnreachable catch (e) {
      // The daemon never answered. Different sentence and different fix, so
      // it is deliberately not folded into the case above.
      if (!isClosed) emit(state.copyWith(daemonDown: true));
      await _fallo(action, e.reason, onFail: onFail);
    } on Object catch (e) {
      // Anything else is a bug on this side, and it still has to reach the
      // screen: an action that vanishes without a word is the failure mode
      // this whole method exists to remove.
      await _fallo(action, e.toString(), onFail: onFail);
    }
  }

  Future<void> _fallo(
    FailedAction action,
    String reason, {
    String? code,
    SessionPhase? onFail,
  }) async {
    // The failure the user asked for. Cancelling makes the daemon abort the
    // operation, and aborting it produces an error, correctly — a red notice
    // over a button somebody just pressed is noise, so it is swallowed here
    // and nowhere else.
    if (_canceling) {
      _canceling = false;
      return;
    }
    // The steps of what just failed, so "ver detalles" has something to show.
    // Only when narrating, and best effort: if the daemon cannot be reached to
    // ask, the failure is still reported, just without its breadcrumbs.
    Progress? pasos = state.progress;
    if (state.verbose) {
      try {
        pasos = await _repository.progress();
      } on Object {
        // Keep whatever the poll had already collected.
      }
    }
    if (isClosed) return;
    emit(
      state.copyWith(
        phase: onFail,
        failure: ActionFailure(
          action: action,
          reason: reason,
          code: code,
          progress: pasos,
        ),
        progress: pasos,
      ),
    );
  }

  /// Dismisses the failure notice.
  void clearFailure() => emit(state.copyWith(clearFailure: true));

  /// Cuts short the room being created or joined.
  ///
  /// # Why it does not just navigate away
  ///
  /// Because the daemon would keep going. Creating a room lifts an engine,
  /// takes an address on two adapters and writes firewall rules; walking off
  /// the screen would leave all of that up, owned by a room the app no longer
  /// thinks it is opening. So this asks the daemon to stop, and the daemon
  /// undoes what it got as far as — the same teardown a failure runs.
  ///
  /// The phase drops to idle HERE and not when the daemon answers. What the
  /// person asked for is to stop waiting, and the operation may take a couple
  /// of seconds to notice: leaving the spinner up meanwhile makes the button
  /// look broken and invites a second press.
  Future<void> cancelPending() async {
    // [SessionState.canCancelWait] y no `isWaiting`: salir de una sala también
    // es una espera y no se puede cortar. Ver ese getter.
    if (!state.canCancelWait) return;
    // Remembered so the failure this produces can be swallowed. The daemon
    // answers a cancelled operation with an error, correctly, and putting a
    // red notice on screen over a button the person just pressed is noise.
    _canceling = true;
    emit(state.copyWith(phase: SessionPhase.idle, clearFailure: true));
    _stopWatching();
    try {
      await _repository.cancel();
    } on Object {
      // Nothing to show, and swallowing it is the right call here rather than
      // laziness: the window is already back home by the line above, and the
      // worst case of a cancel that did not arrive is a daemon that finishes
      // what it was doing, which [_afterWait] then undoes. Reporting it would
      // put a failure notice on screen about the failure of an undo.
    }
  }

  /// Whether the user pressed Cancel during the operation in flight.
  bool _canceling = false;

  /// What happens after a wait ends, whichever way it ended.
  ///
  /// # The race this exists for
  ///
  /// Cancel can arrive too late. The daemon answers `canceled: false`, the
  /// operation finishes, and a room opens that the person explicitly asked not
  /// to have — with the app already back on the home screen. Two things are
  /// wrong with leaving that: the room is real, with an engine and firewall
  /// rules behind it, and the app would be showing no room while the daemon
  /// holds one. So it is left, which is the only answer that keeps the two
  /// sides agreeing.
  ///
  /// It also clears the flag, and that half matters on its own: without it a
  /// cancel that missed would leave the flag up and swallow the NEXT genuine
  /// failure.
  Future<void> _afterWait() async {
    if (!_canceling) return;
    _canceling = false;
    if (isClosed || state.room == null) return;
    await leave();
  }

  /// Turns the step-by-step narration on or off, and remembers it.
  ///
  /// It no longer stops the poll on the spot, and that changed with the wait
  /// screen: the steps also drive the progress bar and the phrase that rotates
  /// while a room opens, which everybody sees. Switching the narration off
  /// mid-creation now hides the panel and leaves the bar moving — killing the
  /// poll would freeze the screen of somebody who only asked for less text.
  /// Outside a wait there is nothing to watch, so it stops as before.
  void setVerbose({required bool enabled}) {
    emit(state.copyWith(verbose: enabled));
    if (!enabled && !state.isWaiting) _stopWatching();
    unawaited(_preferences?.setVerbose(enabled: enabled));
  }

  /// Starts polling the daemon's step diary.
  ///
  /// # Why it runs for everybody now
  ///
  /// It used to bail out when the narration was off, because the step panel was
  /// the only thing reading it. It is not anymore: the bar and the rotating
  /// phrase of the wait screen are drawn from these same steps, so with the
  /// poll gated the screen of anybody who had not turned narration on stood
  /// still for the forty seconds a room takes to open — which is exactly what
  /// the new screen exists to fix.
  ///
  /// What it costs is one request every 400 ms down a local named pipe, for the
  /// seconds a wait lasts, over the SPARE connection so it never queues behind
  /// the operation it is watching. `verbose` still decides whether the step
  /// panel is painted.
  void _watchProgress() {
    _pollingProgreso?.cancel();
    // The previous operation's steps go FIRST, before the new ones arrive.
    // They outlive their wait on purpose — the failure notice reads them when
    // something goes wrong — but the bar is drawn from their count, so leaving
    // them there showed a room being closed at 95% for the 400 ms it takes the
    // first sample of the closing to land.
    emit(state.copyWith(clearProgress: true));
    _pollingProgreso = Timer.periodic(kProgressBeat, (_) async {
      try {
        final Progress p = await _repository.progress();
        if (!isClosed) emit(state.copyWith(progress: p));
      } on Object {
        // A poll that fails changes nothing: the operation it watches is the
        // one that decides, and killing it over a missed sample would remove
        // the panel exactly when it gets interesting.
      }
    });
  }

  void _stopWatching() {
    _pollingProgreso?.cancel();
    _pollingProgreso = null;
  }

  // ------------------------------------------------------------------ latido

  /// Pregunta por la sala y por la salud, sin parar, mientras la app viva.
  ///
  /// # Por qué existe, y qué decisión revierte
  ///
  /// `docs/03` decía **"no hay temporizador en ninguna capa"**: se refrescaba
  /// al entrar a una pantalla y cuando el usuario lo pedía. La consecuencia
  /// aceptada era que lo que se ve pudiera estar viejo. Resultó ser mucho peor
  /// que eso, y no en un caso raro:
  ///
  ///  - Crear una sala, ir a Configuración y volver dejaba a la app **sin
  ///    sala**, con el daemon dentro de una. Crear otra contestaba `busy`, así
  ///    que la app quedaba bloqueada sin nada que la desbloqueara.
  ///  - Lo mismo para un invitado que saliera de la pantalla de sala.
  ///  - Nada de lo que pasara del lado del daemon llegaba nunca: alguien
  ///    entrando, alguien yéndose, el túnel degradándose, la sala cerrándose.
  ///  - "Servicio activo" era una foto del arranque. Con el daemon caído y
  ///    vuelto a levantar había que **cerrar y abrir la ventana** para que la
  ///    app se enterara.
  ///
  /// El daemon es petición y respuesta puro y no empuja nada, así que la única
  /// forma de saber es preguntar. Refrescar "al entrar a una pantalla" no
  /// alcanza porque el estado cambia sin que nadie entre a ninguna pantalla.
  ///

  Timer? _pulso;

  /// Arranca el latido. Idempotente.
  void watchSession() {
    _pulso?.cancel();
    _pulso = Timer.periodic(kSessionBeat, (_) => unawaited(_beat()));
    unawaited(_beat());
    // Una vez, fuera del latido. Es una llamada al daemon por el pipe local, no
    // a la red, y lo que trae no se mueve salvo que alguien lo escriba. Un
    // fallo acá no rompe nada: la barra dice lo que sabe y el diálogo de
    // confianza vuelve a preguntar antes de abrir cualquier sala.
    unawaited(ownSeed().catchError((Object _) => const OwnSeed()));
  }

  /// Una ronda: la sala y la salud, tal como las ve el daemon AHORA.
  ///
  /// Los fallos no suben y no rompen nada visible. Un latido perdido es lo
  /// normal cuando el daemon se está reiniciando, y lo que se hace con él es
  /// marcar que no hay servicio, que es exactamente lo que la barra de estado
  /// tiene que decir en ese momento.
  /// Si hay un latido en vuelo. Ver [_beat].
  bool _latiendo = false;

  Future<void> _beat() async {
    // Mientras se crea o se entra, el estado lo manda la operación en curso.
    // Un latido que llegara en medio pintaría la sala a medio abrir, o la
    // borraría porque todavía no existe.
    if (isClosed || state.isWaiting) return;

    // **Un latido no se solapa con el anterior.**
    //
    // El temporizador dispara cada dos segundos sin mirar si el anterior
    // terminó, y cada latido son tres peticiones POR EL MISMO canal, que el
    // daemon atiende de a una. Con una llamada lenta —el barrido del firewall
    // lo era— el segundo latido se encolaba detrás del primero, el tercero
    // detrás del segundo, y la cola ya no bajaba: las peticiones empezaban a
    // reventar su plazo de diez segundos y la app pintaba "sin servicio" con el
    // daemon perfectamente vivo. Saltarse un tick es gratis; encolarlo cuesta
    // la conexión.
    if (_latiendo) return;
    _latiendo = true;
    try {
      final Room? sala = await _repository.currentRoom();
      final HealthReport salud = await _repository.health();
      // El enlace `kanpachi://` que trajo el navegador. Pedirlo lo CONSUME del
      // lado del daemon, así que un enlace se atiende una vez y no vuelve a
      // aparecer después de cancelarlo. Se pide en el latido y no al abrir la
      // ventana porque puede llegar en cualquier momento: con la ventana
      // escondida, con otra pantalla delante, o con una sala ya abierta.
      final PendingInvite? incoming = await _repository.pendingInvite();
      // La sala del arranque anterior. Solo se pregunta SIN sala: con una sala
      // abierta no hay nada que ofrecer reabrir, y el daemon rechazaría el
      // intento igual. Es además una llamada menos por latido en el caso
      // normal, que es estar dentro de una sala.
      final SavedRoom? anterior = sala == null
          ? await _repository.savedRoom()
          : null;
      if (isClosed) return;
      emit(
        state.copyWith(
          room: sala,
          clearRoom: sala == null,
          health: salud,
          invite: incoming,
          savedRoom: anterior,
          clearSavedRoom: anterior == null,
          daemonDown: false,
          // Volver a estar dentro de una sala tras haberla perdido de vista
          // tiene que devolver también la fase, o la app se queda con la sala
          // puesta y la pantalla de portada.
          phase: sala == null ? SessionPhase.idle : SessionPhase.inRoom,
        ),
      );
      // El catálogo se pide UNA vez, cuando vuelve el servicio. No entra en el
      // latido: es una lista que no cambia sola, y pedirla cada dos segundos
      // sería releer el disco del daemon para nada.
      if (state.catalog.isEmpty) unawaited(loadCatalog());
    } on Object {
      if (!isClosed) emit(state.copyWith(daemonDown: true));
    } finally {
      // En `finally` y no al final del `try`: el `return` de arriba cuando el
      // cubit se cierra saldría por encima, y dejar la bandera puesta apagaría
      // el latido para siempre.
      _latiendo = false;
    }
  }

  void _stopBeating() {
    _pulso?.cancel();
    _pulso = null;
  }

  @override
  Future<void> close() {
    _stopWatching();
    _stopBeating();
    return super.close();
  }

  /// Pide el catálogo al daemon.
  ///
  /// No deja subir el fallo, y no es pereza: esto corre al construir el cubit,
  /// o sea al arrancar la ventana. Un servicio que no está dejaría la app sin
  /// abrirse, que es la peor forma posible de contar que el servicio no está.
  /// Lo que sí hace es MARCARLO, para que la portada lo diga en vez de pintarse
  /// vacía y perfecta.
  Future<void> loadCatalog() async {
    try {
      final List<Game> catalog = await _repository.catalog();
      final List<Game> installed = await _repository.installedGames();
      if (isClosed) return;
      emit(
        state.copyWith(
          catalog: catalog,
          installed: installed,
          daemonDown: false,
        ),
      );
    } on DaemonUnreachable {
      if (isClosed) return;
      emit(state.copyWith(daemonDown: true));
    }
  }

  /// Sets the name you are seen by, and remembers it.
  ///
  /// Remembering matters more than it looks: without it, onboarding came back
  /// on every start, and the daemon rejects `create_room` without a name — so
  /// a forgotten nickname is not a cosmetic annoyance, it is an app that
  /// cannot open a room.
  void setNickname(String value) {
    final String clean = value.trim();
    emit(state.copyWith(nickname: clean));
    unawaited(_preferences?.setNickname(clean));
  }

  /// Guarda qué juego se está confirmando. El diálogo lo lee de acá.
  void proposeGame(Game game) => emit(state.copyWith(pendingGame: game));

  void cancelProposal() => emit(state.copyWith(clearPending: true));

  // ----------------------------------------------- la intención de hospedar

  /// La sala que quedó A MEDIO ABRIR mientras se resuelve un requisito.
  ///
  /// # El hueco que cierra
  ///
  /// Abrir una sala puede toparse con dos pantallas por el camino: elegir el
  /// servidor, y escribir su contraseña. Las dos son transitorias, y sin esto
  /// se comportaban como destinos: se guardaba lo escrito y se volvía a la
  /// portada, con la sala que motivó todo el viaje sin abrir. Quien pulsó
  /// «Crear» tenía que volver a pulsarlo, y saber que tenía que hacerlo.
  ///
  /// Con la intención recordada, esas pantallas CONTINÚAN: elegido el servidor
  /// se vuelve al diálogo de confianza, y aceptada la contraseña se crea la
  /// sala directamente, porque la confianza ya se dio.
  ///
  /// # Por qué son campos y no estado
  ///
  /// Nada se redibuja con esto: lo leen tres callbacks en momentos puntuales.
  /// En el estado sería una razón más de rebuild para todas las pantallas que
  /// hacen `watch`, a cambio de nada.
  String? _pendingHostName;
  Game? _pendingHostGame;

  /// Si hay una creación de sala esperando a que se resuelva un requisito.
  bool get hasHostIntent => _pendingHostName != null;

  /// El nombre con el que se iba a abrir. Null sin intención pendiente.
  String? get hostIntentName => _pendingHostName;

  /// Recuerda que se estaba por abrir una sala. Lo llama quien desvía el flujo
  /// hacia una pantalla transitoria, ANTES de desviarlo.
  void rememberHostIntent({required String name, Game? game}) {
    _pendingHostName = name;
    _pendingHostGame = game;
  }

  /// Olvida la intención. Lo llama cada salida que ABANDONA el flujo: la
  /// flecha de volver de las dos pantallas, y cancelar el diálogo de
  /// confianza. Sin esto, la intención sobreviviría al abandono y guardar el
  /// servidor desde Configuración, semanas después, abriría una sala que ya
  /// nadie estaba abriendo.
  void dropHostIntent() {
    _pendingHostName = null;
    _pendingHostGame = null;
  }

  /// Reintenta la creación pendiente, con el nombre y el juego de entonces.
  ///
  /// Falso si no había nada pendiente. La confianza NO se vuelve a pedir: se
  /// dio al confirmar el diálogo que arrancó esta creación, y lo único que se
  /// interpuso fue un requisito que ya se resolvió.
  Future<bool> resumeHostIntent() async {
    final String? name = _pendingHostName;
    if (name == null) return false;
    return createRoom(name: name, game: _pendingHostGame);
  }

  /// Opens a room. Returns whether it opened.
  ///
  /// **The caller must not navigate until this answers.** It used to navigate
  /// to the room screen at the same time as asking, so a creation that failed
  /// left the app parked on a room screen with no room: an empty window with no
  /// way back, because every control on it needs a room to act on. That is
  /// worse than any error message, and it is the reason this returns a bool
  /// instead of void.
  Future<bool> createRoom({
    required String name,
    Game? game,
    bool replace = false,
  }) async {
    emit(
      state.copyWith(
        phase: SessionPhase.creating,
        clearPending: true,
        clearRoom: true,
      ),
    );
    // The step diary, which the wait screen turns into a bar and a phrase.
    _watchProgress();
    await _try(FailedAction.createRoom, onFail: SessionPhase.idle, () async {
      final Room room = await _repository.createRoom(
        name: name,
        nickname: state.nickname,
        game: game,
        replace: replace,
      );
      emit(state.copyWith(phase: SessionPhase.inRoom, room: room));
    });
    _stopWatching();
    await _afterWait();
    final bool ok = !isClosed && state.room != null;
    // La intención queda puesta SOLO cuando el fallo se arregla en una pantalla
    // y volver a intentar tiene sentido: falta el servidor, o su contraseña.
    // Cualquier otro final la borra, incluido el éxito: una intención que
    // sobreviva a su sala es una sala fantasma esperando un descuido.
    final String? code = isClosed ? null : state.failure?.code;
    if (!ok &&
        (code == FailureCode.seedPassword.wire ||
            code == FailureCode.noOwnSeed.wire)) {
      rememberHostIntent(name: name, game: game);
    } else {
      dropHostIntent();
    }
    return ok;
  }

  /// Joins a room. Returns whether it joined. Same rule as [createRoom].
  Future<bool> joinRoom(String inviteId, {bool replace = false}) async {
    emit(state.copyWith(phase: SessionPhase.joining, clearRoom: true));
    _watchProgress();
    await _try(FailedAction.joinRoom, onFail: SessionPhase.idle, () async {
      final Room room = await _repository.joinRoom(
        inviteId,
        nickname: state.nickname,
        replace: replace,
      );
      emit(state.copyWith(phase: SessionPhase.inRoom, room: room));
    });
    _stopWatching();
    await _afterWait();
    return !isClosed && state.room != null;
  }

  /// Lo que se sabe de un código PEGADO, sin entrar y sin tocar la sesión.
  ///
  /// Lo pide la portada antes de enseñar el diálogo de confianza, porque es de
  /// donde sale la huella de quien hospeda. Null cuando no se pudo resolver, y
  /// eso NO impide nada: el diálogo se enseña igual, y entrar tropieza después
  /// con el mismo registro caído y con su mensaje.
  Future<PendingInvite?> previewInvite(String code) =>
      _repository.previewInvite(code);

  /// Descarta el enlace que llegó de fuera, sin entrar a nada.
  ///
  /// Es el «Cancelar» de la pantalla de confirmación, y no le pide nada al
  /// daemon: recogerlo ya lo consumió allá. Lo único que queda por hacer es
  /// olvidarlo acá.
  void dismissInvite() => emit(state.copyWith(clearInvite: true));

  /// Entra a la sala que trajo el enlace.
  ///
  /// Se manda el enlace ENTERO y no el código suelto. Los dos funcionan, y el
  /// entero conserva el seed: un código pelado significa la semilla por
  /// defecto, así que recortarlo mandaría a otro servidor a quien recibió una
  /// invitación de un Kanpachi autohospedado.
  Future<bool> acceptInvite() async {
    final PendingInvite? invitation = state.invite;
    if (invitation == null || !invitation.understood) return false;
    emit(state.copyWith(clearInvite: true));
    return joinRoom(invitation.link);
  }

  /// Abre un juego en la sala que ya existe.
  Future<void> applyGame(Game game) async {
    final Room? current = state.room;
    if (current == null) return;
    emit(state.copyWith(work: RoomWork.openingGame, clearPending: false));
    await _try(FailedAction.activateProfile, () async {
      final Room updated = await _repository.setGame(current, game);
      emit(
        state.copyWith(room: updated, work: RoomWork.none, clearPending: true),
      );
    });
    // The work flag comes down whatever happened. Left up on failure, every
    // button in the room stays grey and the only way out is restarting.
    if (!isClosed) emit(state.copyWith(work: RoomWork.none));
  }

  Future<void> closeGame() async {
    final Room? current = state.room;
    if (current == null) return;
    emit(state.copyWith(work: RoomWork.closingGame));
    await _try(FailedAction.activateProfile, () async {
      final Room updated = await _repository.setGame(current, null);
      emit(state.copyWith(room: updated, work: RoomWork.none));
    });
    if (!isClosed) emit(state.copyWith(work: RoomWork.none));
  }

  Future<void> rename(String name) async {
    final Room? current = state.room;
    if (current == null) return;
    await _try(FailedAction.renameRoom, () async {
      emit(state.copyWith(room: await _repository.renameRoom(current, name)));
    });
  }

  Future<void> kick(Member member) async {
    final Room? current = state.room;
    if (current == null) return;
    await _try(FailedAction.kickMember, () async {
      emit(state.copyWith(room: await _repository.kick(current, member)));
    });
  }

  /// Renueva el código de la sala.
  ///
  /// Lleva bandera propia, y no es cosmético: por dentro habla con el registro
  /// del seed, que es la única operación de la sala que depende de internet, y
  /// su plazo son treinta segundos. Sin nada que lo diga, el botón parece
  /// muerto y se vuelve a pulsar, que son dos rotaciones seguidas: la segunda
  /// mata el código que la primera acaba de repartir.
  ///
  /// La bandera baja pase lo que pase. Dejada arriba tras un fallo, el botón
  /// se queda apagado para siempre y la única salida sería reiniciar.
  Future<void> renewCode() async {
    final Room? current = state.room;
    if (current == null || state.isRenewingCode) return;
    emit(state.copyWith(code: CodeWork.renewing));
    await _try(FailedAction.rotateInviteCode, () async {
      emit(state.copyWith(room: await _repository.renewCode(current)));
    });
    if (!isClosed) emit(state.copyWith(code: CodeWork.none));
  }

  /// Reabre la sala que quedó del arranque anterior.
  ///
  /// Lleva fase `creating` aunque no cree nada, y es a propósito: por dentro
  /// levanta el motor igual que crear, tarda lo mismo, y sin fase la ventana se
  /// queda muda hasta noventa segundos. Lo que se ve al terminar es la misma
  /// sala, con el mismo código y el mismo enlace que ya se repartió.
  Future<bool> resumeSavedRoom() async {
    if (state.savedRoom == null) return false;
    emit(state.copyWith(phase: SessionPhase.creating, clearSavedRoom: true));
    _watchProgress();
    await _try(FailedAction.resumeRoom, onFail: SessionPhase.idle, () async {
      final Room sala = await _repository.resumeSavedRoom();
      emit(state.copyWith(phase: SessionPhase.inRoom, room: sala));
    });
    _stopWatching();
    await _afterWait();
    return !isClosed && state.room != null;
  }

  /// Descarta esa sala. El código viejo queda muerto y no se vuelve a preguntar.
  ///
  /// Se limpia del estado ANTES de llamar, para que el diálogo se cierre en el
  /// acto: el latido no lo va a volver a traer, porque descartar borra el
  /// archivo del que salía. Si la llamada falla, el siguiente latido lo vuelve
  /// a ofrecer, que es lo correcto: no se descartó nada.
  Future<void> discardSavedRoom() async {
    if (state.savedRoom == null) return;
    emit(state.copyWith(clearSavedRoom: true));
    await _try(FailedAction.discardSavedRoom, () async {
      await _repository.discardSavedRoom();
    });
  }

  /// Resuelve una regla ajena: la desactiva mientras juegas, o la deja.
  ///
  /// Lleva bandera de trabajo, y no es cosmético. Escribir en el almacén de
  /// reglas de Windows por COM tarda alrededor de un segundo, y sin nada que lo
  /// diga el botón parece muerto: se pulsa otra vez, y eso son dos escrituras
  /// del firewall a la vez. Es el mismo argumento que apaga el botón de reponer
  /// la protección.
  Future<void> resolveForeignRule({required bool disable}) async {
    final Room? current = state.room;
    if (current == null || state.isBusy) return;
    emit(state.copyWith(work: RoomWork.resolvingForeign));
    await _try(FailedAction.suspendForeignRules, () async {
      emit(
        state.copyWith(
          room: await _repository.resolveForeignRule(current, disable: disable),
        ),
      );
    });
    // Baja pase lo que pase. Dejada arriba tras un fallo, la sala entera se
    // queda en gris y la única salida es reiniciar.
    if (!isClosed) emit(state.copyWith(work: RoomWork.none));
  }

  /// Leaves the room.
  ///
  /// **The local state is cleared even when the daemon said no**, and that is
  /// deliberate. Staying in a room the user asked to leave, with the buttons
  /// live, invites a second attempt at something that may have half happened.
  /// The failure is shown, the screen goes home, and the next refresh brings
  /// back the truth if the room really is still open.
  ///
  /// # Por qué lleva fase, igual que crear y entrar
  ///
  /// Porque tarda lo mismo y por lo mismo. Por dentro cierra los puertos,
  /// restaura las reglas ajenas que se hubieran suspendido, suelta la
  /// compuerta, revierte los ajustes del adaptador, cierra el canal y baja la
  /// red cifrada, que es esperar a que otro proceso termine. Sin fase, la
  /// pantalla de la sala se quedaba quieta con los botones vivos varios
  /// segundos: se ve exactamente igual que un botón que no hizo nada, y lo que
  /// invita es a pulsarlo otra vez.
  ///
  /// La fase la decide el ROL, y el daemon narra con la misma distinción: al
  /// host se le cierra la sala para todos, y decirle "saliendo" mentiría por
  /// omisión sobre lo que les pasa a los demás.
  Future<void> leave() async {
    final Room? current = state.room;
    if (current != null) {
      emit(
        state.copyWith(
          phase: current.selfIsHost
              ? SessionPhase.closing
              : SessionPhase.leaving,
          clearFailure: true,
        ),
      );
      // El diario de pasos, igual que al abrir: acá también hay una espera con
      // barra y frase que alimentar.
      _watchProgress();
      await _try(FailedAction.leaveRoom, () => _repository.leaveRoom(current));
      _stopWatching();
    }
    if (isClosed) return;
    emit(
      state.copyWith(
        phase: SessionPhase.idle,
        clearRoom: true,
        clearPending: true,
        work: RoomWork.none,
      ),
    );
  }

  /// "Salir de Kanpachi": apagarlo todo.
  ///
  /// **No sale de la sala primero.** Podría parecer más ordenado y sería peor:
  /// serían dos operaciones donde la primera puede fallar dejando la segunda a
  /// medias, y el daemon ya sale de la sala como primer paso de su propio
  /// apagado. Él es quien sabe el orden, porque es quien tiene las piezas.
  ///
  /// No emite estado nuevo. Para cuando esto vuelva, esta ventana está muerta o
  /// a punto: el daemon se la lleva con el job. Pintar algo sería pintar sobre
  /// un proceso que ya no está.
  ///
  /// Guarded anyway, for the one case where it comes back: a console-mode
  /// daemon does not host the interface and answers that it cannot. Then this
  /// window survives, and it has to say why nothing happened.
  Future<void> quitKanpachi() =>
      _try(FailedAction.quit, () => _repository.quitEverything());

  /// Lee, y opcionalmente cambia, si Kanpachi arranca con Windows.
  Future<bool> autostart({bool? enabled}) =>
      _repository.autostart(enabled: enabled);

  /// Cambia el nombre con el que se va a abrir la próxima sala.
  ///
  /// Lo llaman los DOS sitios que lo editan, y por eso está acá: el campo de la
  /// portada y el diálogo de confianza. Ver [SessionState.roomNameDraft].
  void setRoomNameDraft(String name) =>
      emit(state.copyWith(roomNameDraft: name));

  /// Lee, y opcionalmente cambia, el registro en el que esta máquina abre salas.
  ///
  /// Pasa en crudo, sin `_try`, y eso es deliberado: `_try` deja el fallo en el
  /// estado de la sesión para que lo pinte el aviso de la portada, y este error
  /// pertenece al campo donde alguien acaba de escribir. La pantalla lo atrapa y
  /// lo enseña ahí, al lado de lo que hay que corregir.
  Future<OwnSeed> ownSeed({String? seed}) async {
    final OwnSeed v = await _repository.ownSeed(seed: seed);
    // Se guarda lo LEÍDO y no lo pedido: lo que se persiste es el nombre ya
    // normalizado, así que quedarse con lo escrito enseñaría la intención en
    // vez de lo que quedó puesto.
    if (!isClosed) emit(state.copyWith(ownSeed: v.configured));
    return v;
  }

  /// Entrega el password del registro propio, para poder HOSPEDAR en él.
  ///
  /// En crudo y sin `_try`, por lo mismo que [ownSeed]: el fallo pertenece al
  /// campo donde alguien acaba de escribir, y dejarlo en el estado de la sesión
  /// lo pintaría en la portada, lejos de lo que hay que corregir.
  ///
  /// **No emite nada.** El password no toca el estado del cubit: lo único que
  /// existe de él es el texto del campo, que muere con la pantalla. Lo que
  /// sobrevive lo guarda el daemon, sellado, y no vuelve por acá.
  Future<void> seedPassword(String password) =>
      _repository.seedPassword(password);

  /// Lo que la máquina tiene abierto AHORA, medido.
  ///
  /// # Por qué pasa por acá si no toca el estado
  ///
  /// Porque una pantalla no resuelve dependencias. Todas las de esta app leen
  /// cubits del contexto, y la que se saltó esa regla para ir al contenedor por
  /// su cuenta reventó en el test de layout, que las pinta sin montar la app
  /// entera. Reenviar dos líneas acá es más barato que un contenedor de mentira
  /// en cada test que pinte una pantalla.
  ///
  /// No emite estado, y eso también es a propósito: es una medición puntual que
  /// la pantalla pide y guarda mientras está viva. Meterla en [SessionState]
  /// haría que una medición vieja sobreviviera a la pantalla que la pidió, que
  /// es justo la lista rancia pintada de verde que ese diseño existe para
  /// impedir.
  Future<ExposureReport> exposure() => _repository.exposure();

  /// Marca los puertos del host DESDE esta máquina.
  Future<ProbeReport> probeHost() => _repository.probeHost();

  /// Vuelve a auditar las reglas ajenas del firewall, ahora.
  ///
  /// Lo llama la pantalla de la sala al aparecer, que es cuando ese aviso se
  /// lee. Fuera de eso la auditoría se repite sola cada dos minutos, al cambiar
  /// de juego y al entrar alguien nuevo: barre el almacén de reglas ENTERO de
  /// Windows, así que repetirla con el latido costaba CPU en las máquinas
  /// viejas, tenía la forma que un antivirus marca, y ahogaba el único canal
  /// que hay con el daemon.
  Future<void> recheckForeignRules() async {
    _repository.recheckForeignRules();
    await refresh();
  }

  /// Vuelve a preguntar por los avisos y por la Protección Kanpachi.
  ///
  /// Se puede llamar sin sala, y así lo hace la portada: los avisos de salud de
  /// la máquina no esperan a que el usuario abra una.
  Future<void> refreshHealth() async {
    final HealthReport salud = await _repository.health();
    if (isClosed) return;
    emit(state.copyWith(health: salud));
  }

  /// Vuelve a preguntar por TODO: la sala y la salud.
  ///
  /// # Por qué hay que pedirlo, y por qué no hay temporizador
  ///
  /// El daemon es petición y respuesta puro: no empuja nada, así que la única
  /// forma de enterarse de que alguien entró, de que alguien se fue, de que el
  /// túnel se degradó o de que saltó el canario es volver a preguntar. Y no hay
  /// temporizador en ninguna capa, por decisión: se refresca al ENTRAR a una
  /// pantalla y cuando el usuario lo pide.
  ///
  /// **La consecuencia se acepta a sabiendas:** entre un refresco y el
  /// siguiente, lo que se ve puede haber dejado de ser cierto. Las acciones no
  /// sufren, porque cada una devuelve la sala recalculada por el daemon.
  ///
  /// Los fallos NO se dejan subir. Refrescar es de fondo, sin que nadie lo
  /// haya pedido, y un daemon que no contesta no puede tumbar la pantalla que
  /// el usuario está mirando: se queda lo último que sí se supo.
  Future<void> refresh() async {
    if (state.isRefreshing) return;
    emit(state.copyWith(refreshing: true));
    try {
      final Room? sala = await _repository.currentRoom();
      final HealthReport salud = await _repository.health();
      if (isClosed) return;
      if (sala == null) {
        emit(state.copyWith(health: salud));
        return;
      }
      emit(state.copyWith(room: sala, health: salud, daemonDown: false));
    } on DaemonUnreachable {
      if (!isClosed) emit(state.copyWith(daemonDown: true));
    } on Object {
      // Queda lo anterior. Ver arriba.
    } finally {
      emit(state.copyWith(refreshing: false));
    }
  }

  /// Repone la protección y guarda lo que el daemon contestó.
  ///
  /// La bandera de trabajo se pone ANTES de llamar y se quita en un `finally`.
  /// El botón se apaga con ella, y eso no es cosmético: sin apagarlo, un doble
  /// clic manda dos escrituras del firewall a la vez.
  ///
  /// The failure is reported and not swallowed. Reapplying is the action that
  /// clears the alarm, so a silent failure leaves somebody staring at an alarm
  /// that is still up, with no idea their attempt never even arrived.
  Future<void> reapplyProtection() async {
    if (state.isReapplying) return;
    emit(state.copyWith(protection: ProtectionWork.reapplying));
    await _try(FailedAction.reapplyProtection, () async {
      emit(state.copyWith(health: await _repository.reapplyProtection()));
    });
    if (!isClosed) emit(state.copyWith(protection: ProtectionWork.none));
  }

  /// El interruptor de la cuarentena de base, en la dirección que diga
  /// `enabled`. La decisión ES la operación, y lo que vuelve es la salud con
  /// la medición fresca, jamás un acuse: el interruptor se redibuja de lo que
  /// quedó puesto de verdad.
  ///
  /// Misma bandera de trabajo que reponer la protección y por lo mismo: sin
  /// apagar el interruptor mientras corre, un doble clic manda dos escrituras
  /// del firewall a la vez.
  Future<void> setQuarantine({required bool enabled}) async {
    if (state.isTogglingQuarantine) return;
    emit(state.copyWith(protection: ProtectionWork.togglingQuarantine));
    await _try(FailedAction.quarantine, () async {
      emit(
        state.copyWith(health: await _repository.quarantine(enabled: enabled)),
      );
    });
    if (!isClosed) emit(state.copyWith(protection: ProtectionWork.none));
  }

  /// Saves a profile the user typed in.
  ///
  /// Returns null when it did not save, so the screen knows not to navigate
  /// away. The reason why is already on screen by then, put there by [_try].
  Future<Game?> saveManualGame(Game game) async {
    Game? saved;
    await _try(FailedAction.saveProfile, () async {
      saved = await _repository.saveManualGame(game);
      await loadCatalog();
    });
    return saved;
  }
}
