import 'package:flutter/foundation.dart';
import 'package:kanpachi_ui/features/session/domain/entities/game.dart';
import 'package:kanpachi_ui/features/session/domain/entities/health.dart';
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

/// Qué se está haciendo con la protección.
///
/// Está aparte de [RoomWork] porque reponer la protección **no es trabajo de la
/// sala**: se puede pedir sin sala, no cambia el juego ni los miembros, y no
/// tiene que apagar los botones de la sala mientras corre. Metido en el mismo
/// enum, pulsar el botón de la alarma dejaría la pantalla entera en gris.
enum ProtectionWork { none, reapplying }

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
    this.health = const HealthReport.unknown(),
    this.protection = ProtectionWork.none,
    this.refreshing = false,
    this.daemonDown = false,
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

  /// Lo que el daemon vigila solo: los avisos y la Protección Kanpachi.
  ///
  /// Vive acá y no dentro de [room] porque **la mitad de esto existe sin sala**.
  /// Que el Firewall de Windows esté apagado se sabe en la portada, y ahí es
  /// donde más sirve enterarse. Ver [HealthReport].
  final HealthReport health;

  /// Si se está reponiendo la protección ahora mismo.
  final ProtectionWork protection;

  /// Si se está volviendo a preguntar por la sala y la salud.
  ///
  /// Va aparte de [RoomWork] y de [ProtectionWork] por lo mismo que aquellos
  /// están separados entre sí: refrescar no cambia nada, así que no puede
  /// apagar los botones de la sala mientras corre. Solo sirve para no lanzar
  /// dos refrescos encima y para que el botón sepa que está ocupado.
  final bool refreshing;

  /// No se pudo hablar con el servicio de Kanpachi.
  ///
  /// Es distinto de que el daemon dijera que no: aquello es un fallo de la
  /// operación, con su código y su mensaje, y esto es que no hubo con quién
  /// hablar. Sin este dato la portada se pintaría vacía y perfecta, sin
  /// juegos y sin avisos, que es exactamente igual a como se ve una máquina
  /// sana. Un producto que no distingue "todo bien" de "no pude preguntar"
  /// miente en el peor momento.
  final bool daemonDown;

  bool get hasRoom => room != null;
  bool get isBusy => work != RoomWork.none;
  bool get isReapplying => protection == ProtectionWork.reapplying;
  bool get isRefreshing => refreshing;

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
    HealthReport? health,
    ProtectionWork? protection,
    bool? refreshing,
    bool? daemonDown,
  }) => SessionState(
    phase: phase ?? this.phase,
    room: clearRoom ? null : (room ?? this.room),
    work: work ?? this.work,
    catalog: catalog ?? this.catalog,
    installed: installed ?? this.installed,
    pendingGame: clearPending ? null : (pendingGame ?? this.pendingGame),
    nickname: nickname ?? this.nickname,
    health: health ?? this.health,
    protection: protection ?? this.protection,
    refreshing: refreshing ?? this.refreshing,
    daemonDown: daemonDown ?? this.daemonDown,
  );
}
