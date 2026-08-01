import 'package:flutter/foundation.dart';
import 'package:kanpachi_ui/features/session/domain/entities/game.dart';
import 'package:kanpachi_ui/features/session/domain/entities/room.dart';

/// En qué anda la sesión ahora mismo.
enum SessionPhase {
  /// Sin sala. Es el estado por defecto y no un error.
  idle,

  /// Creando una sala: levantando la red y pidiendo el código.
  creating,

  /// Buscando una sala ajena y presentando el equipo con sus miembros.
  joining,

  /// Dentro de una sala.
  inRoom,
}

/// Qué se está aplicando dentro de una sala.
///
/// Está aparte de [SessionPhase] porque la sala NO desaparece mientras se
/// aplica: abrir un juego cambia los puertos, no la sala, y la pantalla tiene
/// que seguir mostrando los miembros y el código mientras tanto.
enum RoomWork { none, openingGame, closingGame }

@immutable
class SessionState {
  const SessionState({
    this.phase = SessionPhase.idle,
    this.room,
    this.work = RoomWork.none,
    this.catalog = const <Game>[],
    this.installed = const <Game>[],
    this.pendingGame,
    this.nickname = '',
  });

  final SessionPhase phase;
  final Room? room;
  final RoomWork work;

  final List<Game> catalog;
  final List<Game> installed;

  /// El juego que el usuario eligió y que el diálogo está confirmando. Vive
  /// acá y no en la pantalla porque el diálogo se abre desde tres sitios
  /// distintos y todos tienen que estar hablando del mismo juego.
  final Game? pendingGame;

  final String nickname;

  bool get hasRoom => room != null;
  bool get isBusy => work != RoomWork.none;

  SessionState copyWith({
    SessionPhase? phase,
    Room? room,
    bool clearRoom = false,
    RoomWork? work,
    List<Game>? catalog,
    List<Game>? installed,
    Game? pendingGame,
    bool clearPending = false,
    String? nickname,
  }) =>
      SessionState(
        phase: phase ?? this.phase,
        room: clearRoom ? null : (room ?? this.room),
        work: work ?? this.work,
        catalog: catalog ?? this.catalog,
        installed: installed ?? this.installed,
        pendingGame: clearPending ? null : (pendingGame ?? this.pendingGame),
        nickname: nickname ?? this.nickname,
      );
}
