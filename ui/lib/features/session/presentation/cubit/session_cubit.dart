import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:kanpachi_ui/features/session/domain/entities/exposure.dart';
import 'package:kanpachi_ui/features/session/domain/daemon_failure.dart';
import 'package:kanpachi_ui/features/session/domain/entities/game.dart';
import 'package:kanpachi_ui/features/session/domain/entities/health.dart';
import 'package:kanpachi_ui/features/session/domain/entities/probe.dart';
import 'package:kanpachi_ui/features/session/domain/entities/room.dart';
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
  SessionCubit(this._repository) : super(const SessionState());

  final SessionRepository _repository;

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

  void setNickname(String value) =>
      emit(state.copyWith(nickname: value.trim()));

  /// Guarda qué juego se está confirmando. El diálogo lo lee de acá.
  void proposeGame(Game game) => emit(state.copyWith(pendingGame: game));

  void cancelProposal() => emit(state.copyWith(clearPending: true));

  Future<void> createRoom({required String name, Game? game}) async {
    emit(
      state.copyWith(
        phase: SessionPhase.creating,
        clearPending: true,
        clearRoom: true,
      ),
    );
    final Room room = await _repository.createRoom(
      name: name,
      nickname: state.nickname,
      game: game,
    );
    emit(state.copyWith(phase: SessionPhase.inRoom, room: room));
  }

  Future<void> joinRoom(String inviteId) async {
    emit(state.copyWith(phase: SessionPhase.joining, clearRoom: true));
    final Room room = await _repository.joinRoom(
      inviteId,
      nickname: state.nickname,
    );
    emit(state.copyWith(phase: SessionPhase.inRoom, room: room));
  }

  /// Abre un juego en la sala que ya existe.
  Future<void> applyGame(Game game) async {
    final Room? current = state.room;
    if (current == null) return;
    emit(state.copyWith(work: RoomWork.openingGame, clearPending: false));
    final Room updated = await _repository.setGame(current, game);
    emit(
      state.copyWith(room: updated, work: RoomWork.none, clearPending: true),
    );
  }

  Future<void> closeGame() async {
    final Room? current = state.room;
    if (current == null) return;
    emit(state.copyWith(work: RoomWork.closingGame));
    final Room updated = await _repository.setGame(current, null);
    emit(state.copyWith(room: updated, work: RoomWork.none));
  }

  Future<void> rename(String name) async {
    final Room? current = state.room;
    if (current == null) return;
    emit(state.copyWith(room: await _repository.renameRoom(current, name)));
  }

  Future<void> kick(Member member) async {
    final Room? current = state.room;
    if (current == null) return;
    emit(state.copyWith(room: await _repository.kick(current, member)));
  }

  Future<void> renewCode() async {
    final Room? current = state.room;
    if (current == null) return;
    emit(state.copyWith(room: await _repository.renewCode(current)));
  }

  Future<void> resolveForeignRule({required bool disable}) async {
    final Room? current = state.room;
    if (current == null) return;
    emit(
      state.copyWith(
        room: await _repository.resolveForeignRule(current, disable: disable),
      ),
    );
  }

  Future<void> leave() async {
    final Room? current = state.room;
    if (current != null) await _repository.leaveRoom(current);
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
  Future<void> quitKanpachi() => _repository.quitEverything();

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
  /// El fallo se deja subir a propósito. Reponer es la acción que arregla la
  /// alarma, así que tragarse el error dejaría al usuario mirando una alarma que
  /// sigue puesta sin saber que su intento ni siquiera llegó.
  Future<void> reapplyProtection() async {
    if (state.isReapplying) return;
    emit(state.copyWith(protection: ProtectionWork.reapplying));
    try {
      emit(state.copyWith(health: await _repository.reapplyProtection()));
    } finally {
      emit(state.copyWith(protection: ProtectionWork.none));
    }
  }

  Future<Game> saveManualGame(Game game) async {
    final Game saved = await _repository.saveManualGame(game);
    await loadCatalog();
    return saved;
  }

}
