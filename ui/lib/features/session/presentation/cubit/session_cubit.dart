import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:kanpachi_ui/features/session/domain/entities/game.dart';
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

  Future<void> loadCatalog() async {
    final List<Game> catalog = await _repository.catalog();
    final List<Game> installed = await _repository.installedGames();
    emit(state.copyWith(catalog: catalog, installed: installed));
  }

  void setNickname(String value) => emit(state.copyWith(nickname: value.trim()));

  /// Guarda qué juego se está confirmando. El diálogo lo lee de acá.
  void proposeGame(Game game) => emit(state.copyWith(pendingGame: game));

  void cancelProposal() => emit(state.copyWith(clearPending: true));

  Future<void> createRoom({required String name, Game? game}) async {
    emit(state.copyWith(
      phase: SessionPhase.creating,
      clearPending: true,
      clearRoom: true,
    ));
    final Room room = await _repository.createRoom(name: name, game: game);
    emit(state.copyWith(phase: SessionPhase.inRoom, room: room));
  }

  Future<void> joinRoom(String inviteId) async {
    emit(state.copyWith(phase: SessionPhase.joining, clearRoom: true));
    final Room room = await _repository.joinRoom(inviteId);
    emit(state.copyWith(phase: SessionPhase.inRoom, room: room));
  }

  /// Abre un juego en la sala que ya existe.
  Future<void> applyGame(Game game) async {
    final Room? current = state.room;
    if (current == null) return;
    emit(state.copyWith(work: RoomWork.openingGame, clearPending: false));
    final Room updated = await _repository.setGame(current, game);
    emit(state.copyWith(
      room: updated,
      work: RoomWork.none,
      clearPending: true,
    ));
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
    emit(state.copyWith(
      room: await _repository.resolveForeignRule(current, disable: disable),
    ));
  }

  Future<void> leave() async {
    final Room? current = state.room;
    if (current != null) await _repository.leaveRoom(current);
    emit(state.copyWith(
      phase: SessionPhase.idle,
      clearRoom: true,
      clearPending: true,
      work: RoomWork.none,
    ));
  }

  /// Vuelve a preguntar por los avisos y por la Protección Kanpachi.
  ///
  /// Se puede llamar sin sala, y así lo hace la portada: los avisos de salud de
  /// la máquina no esperan a que el usuario abra una.
  Future<void> refreshHealth() async {
    emit(state.copyWith(health: await _repository.health()));
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

  /// Sustituye la sala entera. Lo usa la barra de prototipo para plantar un
  /// escenario sin pasar por los tiempos de espera.
  void debugReplaceRoom(Room room) => emit(state.copyWith(
        phase: SessionPhase.inRoom,
        room: room,
        work: RoomWork.none,
      ));
}
