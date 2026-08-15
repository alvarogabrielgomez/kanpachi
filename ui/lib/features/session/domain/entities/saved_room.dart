import 'package:flutter/foundation.dart';

/// La sala que ESTA máquina hospeda, tal como quedó guardada en disco.
///
/// # Qué significa que exista, y qué dejó de significar
///
/// Que hay una sala que reabrir, y nada más. Antes significaba que la última
/// salida fue sucia, porque salir borraba el archivo y morir lo dejaba; esa
/// lectura se acabó cuando apagarse limpio pasó a conservarlo también. Lo único
/// que lo borra es cerrar la sala.
///
/// # No es una pregunta esperando respuesta
///
/// El daemon la reabre solo en cada arranque, con el mismo código y la misma
/// red. Esto se lee para el caso en que esa reapertura FALLE, que es lo único
/// que la deja a la vista: entonces la ventana ofrece reabrirla a mano, o
/// cerrarla. Ver [SavedRoomNotice].
///
/// Reabrir devuelve el MISMO código y el MISMO enlace: la identidad de la red
/// real y la clave de la tarjeta viajan en ese archivo, así que los enlaces ya
/// repartidos siguen valiendo y quien estuviera dentro puede volver.
@immutable
class SavedRoom {
  const SavedRoom({
    required this.code,
    this.seed = '',
    this.name = '',
    this.gameId = '',
    this.subnet = '',
    this.savedAt,
  });

  factory SavedRoom.fromJson(Map<String, Object?> json) => SavedRoom(
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

  /// El código como se enseña, con su registro. Mismo motivo que
  /// [Room.displayCode]: pelado no identifica una sala.
  String get displayCode => seed.isEmpty ? code : '$code@$seed';

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
