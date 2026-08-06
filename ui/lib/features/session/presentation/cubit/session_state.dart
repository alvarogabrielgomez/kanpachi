import 'package:flutter/foundation.dart';
import 'package:kanpachi_ui/features/session/domain/entities/action_failure.dart';
import 'package:kanpachi_ui/features/session/domain/entities/game.dart';
import 'package:kanpachi_ui/features/session/domain/entities/progress.dart';
import 'package:kanpachi_ui/features/session/domain/entities/health.dart';
import 'package:kanpachi_ui/features/session/domain/entities/pending_invite.dart';
import 'package:kanpachi_ui/features/session/domain/entities/pending_room.dart';
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
enum RoomWork {
  none,
  openingGame,
  closingGame,

  /// Resolviendo una regla ajena: desactivarla o dejarla como está.
  ///
  /// Es trabajo de la sala y no de la protección, aunque hable del firewall:
  /// desactivar una regla recalcula los puertos de la sala y devuelve la sala
  /// entera, así que mientras corre los botones de la sala no pueden estar
  /// vivos. Tarda de verdad, alrededor de un segundo, porque escribe en el
  /// almacén de reglas de Windows por COM.
  resolvingForeign,
}

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
    this.failure,
    this.progress,
    this.verbose = false,
    this.invite,
    this.pendingRoom,
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

  /// The last action the user asked for that did not happen.
  ///
  /// Lives in the state instead of escaping as an exception because a screen
  /// cannot catch one. Before this, a failing action either threw into nowhere
  /// or was swallowed, and on screen both look the same: the spinner stops and
  /// nothing changes. The user cannot tell "it failed" from "it worked and the
  /// screen is stale", so they press again.
  ///
  /// Cleared when the next action starts, or when the user dismisses it.
  final ActionFailure? failure;

  /// Steps of the long operation in flight, or null when nobody asked.
  ///
  /// Only filled while [verbose] is on: with it off nothing polls, so this
  /// stays null and the panel that reads it never paints.
  final Progress? progress;

  /// Whether this window narrates what the daemon is doing.
  ///
  /// Seeded from [AppPreferences.verbose], which defaults to on while
  /// developing and off in the product. It gates three things that belong
  /// together: the step panel on the wait screen, the poll that fills it, and
  /// the "ver detalles" button on a failure. Gating them separately is how you
  /// end up with a details button that opens on nothing.
  final bool verbose;

  /// El enlace `kanpachi://` que llegó de fuera y espera confirmación.
  ///
  /// Vive en el estado y no como argumento de una pantalla porque quien lo
  /// descubre es el latido, no una navegación: el enlace puede llegar con la
  /// ventana escondida, con otra pantalla abierta o con una sala en marcha.
  final PendingInvite? invite;

  /// La sala del arranque anterior, esperando que alguien diga qué hacer.
  ///
  /// Vive en el estado por lo mismo que [invite]: quien la descubre es el
  /// latido y no una navegación. La diferencia es que preguntar por ella NO la
  /// consume, así que sigue ofreciéndose hasta que se reabre o se descarta.
  final PendingRoom? pendingRoom;

  bool get hasRoom => room != null;

  /// Whether a long operation is in flight: creating a room or joining one.
  ///
  /// The two together, because everything that reads this treats them the
  /// same: they are the two waits with a Cancel button, and the two the daemon
  /// can be asked to cut short.
  bool get isWaiting =>
      phase == SessionPhase.creating || phase == SessionPhase.joining;

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
    ActionFailure? failure,
    bool clearFailure = false,
    Progress? progress,
    bool clearProgress = false,
    bool? verbose,
    PendingInvite? invite,
    bool clearInvite = false,
    PendingRoom? pendingRoom,
    bool clearPendingRoom = false,
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
    failure: clearFailure ? null : (failure ?? this.failure),
    progress: clearProgress ? null : (progress ?? this.progress),
    verbose: verbose ?? this.verbose,
    invite: clearInvite ? null : (invite ?? this.invite),
    pendingRoom: clearPendingRoom ? null : (pendingRoom ?? this.pendingRoom),
  );
}
