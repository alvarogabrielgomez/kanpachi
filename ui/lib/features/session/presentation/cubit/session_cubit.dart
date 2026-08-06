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
import 'package:kanpachi_ui/features/session/domain/entities/probe.dart';
import 'package:kanpachi_ui/features/session/domain/entities/room.dart';
import 'package:kanpachi_ui/features/session/infra/daemon/daemon_codec.dart';
import 'package:kanpachi_ui/features/session/domain/repositories/session_repository.dart';
import 'package:kanpachi_ui/features/session/presentation/cubit/session_state.dart';

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
    // The lint asks for `this._preferences`, which Dart does not allow: a
    // named parameter cannot start with an underscore. The field stays
    // private and the parameter stays nameable.
    // ignore: prefer_initializing_formals
  }) : _preferences = preferences,
       super(SessionState(nickname: nickname, verbose: verbose));

  final SessionRepository _repository;
  final AppPreferences? _preferences;

  /// Poll of the daemon's step diary while a long operation runs.
  ///
  /// Only exists while [SessionState.verbose] is on. With it off nothing
  /// starts it, so the field stays null and the daemon never sees the extra
  /// connection.
  Timer? _pollingProgreso;

  /// How often the steps are asked for.
  ///
  /// Fast enough that a step which lands mid-wait is seen while it still means
  /// something, slow enough that a ninety-second creation costs a couple of
  /// hundred round trips on a local pipe rather than thousands.
  static const Duration _cadenciaProgreso = Duration(milliseconds: 400);

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

  /// Turns the step-by-step narration on or off, and remembers it.
  ///
  /// Turning it OFF stops the poll on the spot rather than at the end of the
  /// operation. Somebody who switches this off mid-creation is asking for the
  /// traffic to stop, not to stop in a while.
  void setVerbose({required bool enabled}) {
    emit(state.copyWith(verbose: enabled));
    if (!enabled) _stopWatching();
    unawaited(_preferences?.setVerbose(enabled: enabled));
  }

  /// Starts polling the daemon's step diary. Does nothing when not narrating.
  void _watchProgress() {
    if (!state.verbose) return;
    _pollingProgreso?.cancel();
    _pollingProgreso = Timer.periodic(_cadenciaProgreso, (_) async {
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

  @override
  Future<void> close() {
    _stopWatching();
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

  /// Opens a room. Returns whether it opened.
  ///
  /// **The caller must not navigate until this answers.** It used to navigate
  /// to the room screen at the same time as asking, so a creation that failed
  /// left the app parked on a room screen with no room: an empty window with no
  /// way back, because every control on it needs a room to act on. That is
  /// worse than any error message, and it is the reason this returns a bool
  /// instead of void.
  Future<bool> createRoom({required String name, Game? game}) async {
    emit(
      state.copyWith(
        phase: SessionPhase.creating,
        clearPending: true,
        clearRoom: true,
      ),
    );
    // Only whoever asked to be narrated to pays for the poll.
    _watchProgress();
    await _try(FailedAction.createRoom, onFail: SessionPhase.idle, () async {
      final Room room = await _repository.createRoom(
        name: name,
        nickname: state.nickname,
        game: game,
      );
      emit(state.copyWith(phase: SessionPhase.inRoom, room: room));
    });
    _stopWatching();
    return !isClosed && state.room != null;
  }

  /// Joins a room. Returns whether it joined. Same rule as [createRoom].
  Future<bool> joinRoom(String inviteId) async {
    emit(state.copyWith(phase: SessionPhase.joining, clearRoom: true));
    _watchProgress();
    await _try(FailedAction.joinRoom, onFail: SessionPhase.idle, () async {
      final Room room = await _repository.joinRoom(
        inviteId,
        nickname: state.nickname,
      );
      emit(state.copyWith(phase: SessionPhase.inRoom, room: room));
    });
    _stopWatching();
    return !isClosed && state.room != null;
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

  Future<void> renewCode() async {
    final Room? current = state.room;
    if (current == null) return;
    await _try(FailedAction.rotateInviteCode, () async {
      emit(state.copyWith(room: await _repository.renewCode(current)));
    });
  }

  Future<void> resolveForeignRule({required bool disable}) async {
    final Room? current = state.room;
    if (current == null) return;
    await _try(FailedAction.suspendForeignRules, () async {
      emit(
        state.copyWith(
          room: await _repository.resolveForeignRule(current, disable: disable),
        ),
      );
    });
  }

  /// Leaves the room.
  ///
  /// **The local state is cleared even when the daemon said no**, and that is
  /// deliberate. Staying in a room the user asked to leave, with the buttons
  /// live, invites a second attempt at something that may have half happened.
  /// The failure is shown, the screen goes home, and the next refresh brings
  /// back the truth if the room really is still open.
  Future<void> leave() async {
    final Room? current = state.room;
    if (current != null) {
      await _try(FailedAction.leaveRoom, () => _repository.leaveRoom(current));
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
