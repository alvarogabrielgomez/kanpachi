import 'package:kanpachi_ui/features/session/domain/entities/exposure.dart';
import 'package:kanpachi_ui/features/session/domain/entities/game.dart';
import 'package:kanpachi_ui/features/session/domain/entities/health.dart';
import 'package:kanpachi_ui/features/session/domain/entities/pending_invite.dart';
import 'package:kanpachi_ui/features/session/domain/entities/pending_room.dart';
import 'package:kanpachi_ui/features/session/domain/entities/probe.dart';
import 'package:kanpachi_ui/features/session/domain/entities/progress.dart';
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
  ///
  /// El apodo es un ARGUMENTO y no un estado guardado, porque en el cable lo es:
  /// `create_room` y `join_room` lo reciben, y no existe ningún `set_nickname`
  /// entre los métodos del daemon. Un repositorio que fuera a buscarlo a algún
  /// sitio estaría inventando una capa de identidad que el protocolo no tiene, y
  /// el primer fallo sería una sala creada con un apodo que el usuario cambió
  /// tres pantallas atrás.
  Future<Room> createRoom({
    required String name,
    required String nickname,
    Game? game,
  });

  /// Entra a una sala con un invite ID.
  Future<Room> joinRoom(String inviteId, {required String nickname});

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

  /// La sala tal como el daemon la ve AHORA, o null si no hay ninguna.
  ///
  /// Existe porque el daemon es petición y respuesta puro: no empuja nada, así
  /// que la única forma de enterarse de que alguien entró, de que alguien se
  /// fue o de que el túnel se degradó es volver a preguntar.
  ///
  /// **Lo pregunta un latido, cada dos segundos.** Antes se refrescaba al
  /// entrar a una pantalla y cuando el usuario lo pedía, sin temporizador en
  /// ninguna capa, y eso resultó ser mucho peor que ver datos viejos: la app se
  /// bloqueaba sola. Ver [SessionCubit.watchSession] y `docs/03`.
  Future<Room?> currentRoom();

  /// El enlace `kanpachi://` que trajo el navegador, o null si no hay ninguno.
  ///
  /// **Pedirlo lo CONSUME**, y eso es parte del contrato: sin ello el latido
  /// volvería a enseñar la pantalla de confirmación cada dos segundos, incluso
  /// después de que alguien la cancelara.
  ///
  /// Viene ya resuelto contra el registro. La app no descifra la tarjeta ni
  /// parsea el enlace: eso vive en el dominio del daemon, que es donde está
  /// escrita y probada la frontera de entrada hostil.
  Future<PendingInvite?> pendingInvite();

  /// La sala que quedó abierta cuando el daemon murió sucio, o null si no hay.
  ///
  /// **A diferencia de [pendingInvite], preguntarlo NO lo consume.** Son dos
  /// cosas distintas: un enlace es un evento que ocurrió una vez y se atiende
  /// una vez, y esto es un archivo en disco que sigue ahí hasta que alguien
  /// decide qué hacer con él. Consumirlo al preguntar haría que cerrar la
  /// ventana perdiera la sala sin que nadie lo pidiera.
  Future<PendingRoom?> pendingRoom();

  /// Reabre esa sala: la misma red, el mismo código y el mismo enlace.
  ///
  /// Puede tardar, porque levanta el motor de verdad. No es un "restaurar
  /// pantalla": vuelve a crear la red real con la identidad guardada, así que
  /// quien siguiera dentro reconecta sin pedir credencial nueva.
  Future<Room> resumePendingRoom();

  /// Descarta esa sala. Borra el archivo y no vuelve a preguntar.
  Future<void> discardPendingRoom();

  /// Lo que la máquina tiene abierto AHORA, medido.
  ///
  /// No devuelve error a propósito: la pantalla que lo enseña tiene que decir
  /// algo siempre, y lo que dice cuando la medición falla ya viaja dentro del
  /// informe. Un error obligaría a quien llama a inventar qué pintar, y la
  /// respuesta cómoda sería enseñar la última lista buena, que es exactamente
  /// la mentira que hay que impedir.
  Future<ExposureReport> exposure();

  /// Marca los puertos del host DESDE esta máquina, que es la única
  /// comprobación del producto que atraviesa la red de verdad.
  ///
  /// Solo la puede correr un invitado. El host no se puede sondear a sí mismo,
  /// y no es una limitación de implementación: el tráfico a la propia dirección
  /// no atraviesa ningún firewall, así que contestaría que está todo abierto en
  /// una máquina blindada.
  ///
  /// Al revés que [exposure], esto sí puede fallar, y la asimetría tiene razón:
  /// acá los fallos son PRECONDICIONES que el usuario entiende y puede cambiar,
  /// no mediciones que salieron mal.
  Future<ProbeReport> probeHost();

  /// Los avisos que el daemon produjo SOLO, más la última ronda de la
  /// Protección Kanpachi.
  ///
  /// Se puede pedir sin sala, y ahí está media gracia: que el Firewall de
  /// Windows esté apagado es cierto en la portada, antes de que haya nada que
  /// hospedar.
  ///
  /// No devuelve error, por lo mismo que [exposure]: la pantalla que lo enseña
  /// tiene que decir algo siempre, y "todavía no se sabe" ya se puede expresar
  /// con [HealthReport.unknown].
  Future<HealthReport> health();

  /// Vuelve a aplicar la protección: el deny-all propio y la compuerta.
  ///
  /// **Es IDEMPOTENTE.** El daemon calcula la diferencia contra las reglas vivas
  /// del firewall, así que llamarlo con nada roto no toca nada. Por eso la UI lo
  /// puede ofrecer sin diálogo de confirmación.
  ///
  /// Devuelve el informe ya recalculado y no un acuse, porque lo que la pantalla
  /// necesita a continuación es redibujar SIN la alerta que acaba de resolverse.
  /// Con un acuse habría que volver a preguntar, y entre las dos llamadas la
  /// pantalla enseñaría una alarma que ya no es cierta.
  Future<HealthReport> reapplyProtection();

  /// Da de alta un juego que no estaba en el catálogo.
  Future<Game> saveManualGame(Game game);

  /// "Salir de Kanpachi": apagarlo todo, de forma coordinada.
  ///
  /// **La app NO coordina este apagado, solo lo pide.** No controla nada de lo
  /// que hay que apagar: la sala, las reglas del firewall, el motor y el
  /// adaptador virtual son del daemon. Él sale de la sala, purga, baja el motor,
  /// mata esta interfaz y se detiene al final.
  ///
  /// Por eso no devuelve nada y por eso puede fallar de forma rara: esta ventana
  /// se muere a mitad de la llamada, con el mismo apagado que pidió. Una
  /// excepción acá casi siempre significa que funcionó.
  Future<void> quitEverything();

  /// Lee, y opcionalmente cambia, si Kanpachi arranca con Windows.
  ///
  /// Una sola operación para las dos cosas porque son la misma pregunta, y la
  /// pantalla que mueve el interruptor necesita el valor de vuelta para
  /// dibujarlo. Devuelve lo que quedó puesto DE VERDAD, releído del sistema:
  /// devolver lo que se pidió enseñaría como aceptado un cambio que Windows
  /// pudo rechazar.
  Future<bool> autostart({bool? enabled});

  /// Cuts short the long operation the daemon has in flight.
  ///
  /// Returns whether there was one to cut. False is not an error: it is what
  /// pressing Cancel in the instant the operation was finishing on its own
  /// looks like, and the screen finds out from the operation's own result.
  ///
  /// Asked down a connection of its own, for the same reason as [progress].
  Future<bool> cancel();

  /// Steps of the long operation the daemon has in flight, or the last one.
  ///
  /// # Why it is PULLED
  ///
  /// The local API is pure request and response, with no server push, and this
  /// does not change that: the daemon piles the steps on its side and whoever
  /// wants them asks. See `docs/03`.
  ///
  /// Asked down a connection of its own, because the one carrying the long
  /// operation is busy carrying it.
  Future<Progress> progress();
}
