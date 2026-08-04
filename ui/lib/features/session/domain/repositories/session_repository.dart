import 'package:kanpachi_ui/features/session/domain/entities/exposure.dart';
import 'package:kanpachi_ui/features/session/domain/entities/game.dart';
import 'package:kanpachi_ui/features/session/domain/entities/room.dart';

/// Lo que la UI necesita del daemon.
///
/// Existe como contrato desde ahora, con el daemon todavía sin escribir, por
/// una razón concreta: fija QUÉ puede pedir la interfaz. Todo lo que no esté
/// acá es algo que la UI no puede hacer, y eso incluye cosas que sería fácil
/// colar — leer un archivo del disco, mirar procesos, abrir un puerto por su
/// cuenta. La UI corre sin privilegios y este contrato es dónde eso se ve.
///
/// Cuando exista el named pipe, la implementación real sustituye a la falsa y
/// ni las pantallas ni los cubits se enteran.
abstract interface class SessionRepository {
  /// El catálogo completo, con los instalados ya marcados.
  Future<List<Game>> catalog();

  /// Los juegos detectados en esta PC, para la portada.
  Future<List<Game>> installedGames();

  /// Crea una sala. `game` en null es una sala vacía, que es lo normal: el
  /// juego se elige adentro.
  Future<Room> createRoom({required String name, Game? game});

  /// Entra a una sala con un invite ID.
  Future<Room> joinRoom(String inviteId);

  /// Abre un juego en la sala, o lo cierra si `game` es null. Devuelve la
  /// sala ya con los puertos aplicados.
  Future<Room> setGame(Room room, Game? game);

  Future<Room> renameRoom(Room room, String name);

  /// Expulsa a alguien. No revoca el código: quien fue expulsado puede volver
  /// si todavía lo tiene, y por eso la UI ofrece renovar a continuación.
  Future<Room> kick(Room room, Member member);

  /// Emite un código nuevo. Los que ya están dentro se quedan.
  Future<Room> renewCode(Room room);

  /// Desactiva la regla de firewall que dejó el propio juego, o la deja como
  /// está. Lo que se desactive se restaura al salir de la sala.
  Future<Room> resolveForeignRule(Room room, {required bool disable});

  Future<void> leaveRoom(Room room);

  /// Lo que la máquina tiene abierto AHORA, medido.
  ///
  /// No devuelve error a propósito: la pantalla que lo enseña tiene que decir
  /// algo siempre, y lo que dice cuando la medición falla ya viaja dentro del
  /// informe. Un error obligaría a quien llama a inventar qué pintar, y la
  /// respuesta cómoda sería enseñar la última lista buena, que es exactamente
  /// la mentira que hay que impedir.
  Future<ExposureReport> exposure();

  /// Da de alta un juego que no estaba en el catálogo.
  Future<Game> saveManualGame(Game game);
}
