/// Lo que el icono de la bandeja tiene que decir en un momento dado.
///
/// Es una sola línea a propósito. El menú de una bandeja se lee de reojo,
/// mientras se juega, y lo único que hace falta saber sin abrir la ventana es
/// si hay sala y cuánta gente hay dentro.
class TrayStatus {
  const TrayStatus({required this.line, required this.hasRoom});

  const TrayStatus.noRoom() : line = 'Sin sala', hasRoom = false;

  final String line;
  final bool hasRoom;

  @override
  bool operator ==(Object other) =>
      other is TrayStatus && other.line == line && other.hasRoom == hasRoom;

  @override
  int get hashCode => Object.hash(line, hasRoom);
}

/// El icono de Kanpachi en la bandeja del sistema.
///
/// Existe como contrato y no como llamada directa al plugin porque es lo que
/// sostiene la promesa central de la app: cerrar la ventana NO cierra la sala.
/// Si la ventana se esconde y no queda icono, la sala sigue viva y no hay
/// forma de volver a ella — así que esto no es decoración, es la única puerta
/// de vuelta.
abstract interface class TrayPresence {
  /// Pone el icono y su menú. Idempotente: llamarlo dos veces no duplica el
  /// icono.
  Future<void> start({
    required TrayStatus status,
    required void Function() onOpen,
    required void Function() onLeaveRoom,
    required void Function() onQuit,
  });

  /// Reescribe el menú cuando cambia la sala.
  Future<void> update(TrayStatus status);

  Future<void> stop();
}
