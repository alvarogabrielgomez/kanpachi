import 'package:flutter/foundation.dart';

/// La sala que quedó abierta cuando el daemon murió sucio.
///
/// **Su sola existencia es la señal de mal cierre.** Salir de la sala o apagar
/// bien borra el archivo del disco, y morir sin poder despedirse lo deja: un
/// corte de luz, un cuelgue, un fin de proceso a la fuerza. Por eso no hace
/// falta ninguna bandera `dirty` dentro, que sería un campo más que alguien
/// puede escribir a mano.
///
/// Existe para PREGUNTAR, nunca para reabrir sola. Es la misma invariante que
/// gobierna [PendingInvite]: nada que llegue de fuera de la app surte efecto sin
/// confirmación dentro de la app, y acá lo de fuera es un archivo del arranque
/// anterior.
///
/// Reabrir devuelve el MISMO código y el MISMO enlace: la identidad de la red
/// real y la clave de la tarjeta viajan en ese archivo, así que los enlaces ya
/// repartidos siguen valiendo y quien estuviera dentro puede volver.
@immutable
class PendingRoom {
  const PendingRoom({
    required this.code,
    this.seed = '',
    this.name = '',
    this.gameId = '',
    this.subnet = '',
    this.savedAt,
  });

  factory PendingRoom.fromJson(Map<String, Object?> json) => PendingRoom(
    code: json['code'] as String? ?? '',
    seed: json['seed'] as String? ?? '',
    name: json['name'] as String? ?? '',
    gameId: json['game'] as String? ?? '',
    subnet: json['subnet'] as String? ?? '',
    savedAt: DateTime.tryParse(json['saved_at'] as String? ?? '')?.toLocal(),
  );

  /// El invite ID en su forma legible. Es el mismo que se reparte al reabrir.
  final String code;

  final String seed;

  /// El nombre que tenía la sala. Vacío es legal: una sala puede no tenerlo.
  final String name;

  /// El juego que estaba activo, por id.
  ///
  /// Se guarda el id y no el perfil entero porque al reabrir se resuelve contra
  /// el catálogo de HOY: si alguien desinstaló ese juego, la sala vuelve sin él
  /// en vez de no volver.
  final String gameId;

  final String subnet;

  /// Cuándo se guardó por última vez. `null` cuando no vino o no se entendió.
  ///
  /// Es el dato que le deja decidir a la persona: una sala de hace diez minutos
  /// casi siempre se quiere de vuelta, y una de hace tres días casi nunca.
  final DateTime? savedAt;

  /// Si hay de verdad una sala que ofrecer.
  bool get understood => code.isNotEmpty;
}
