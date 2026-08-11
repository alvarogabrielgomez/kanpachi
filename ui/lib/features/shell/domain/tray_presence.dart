/// Lo que el icono de la bandeja tiene que decir en un momento dado.
///
/// Es una sola línea a propósito. El menú de una bandeja se lee de reojo,
/// mientras se juega, y lo único que hace falta saber sin abrir la ventana es
/// si hay sala y cuánta gente hay dentro.
class TrayStatus {
  const TrayStatus({
    required this.line,
    required this.hasRoom,
    this.leaving = false,
  });

  const TrayStatus.noRoom()
    : line = 'Sin sala',
      hasRoom = false,
      leaving = false;

  final String line;
  final bool hasRoom;

  /// La salida está en curso.
  ///
  /// Existe porque salir tarda: por dentro cierra puertos, restaura reglas
  /// ajenas, revierte los ajustes del adaptador y baja la red cifrada. Desde la
  /// bandeja eso eran varios segundos en los que el menú seguía diciendo lo
  /// mismo, así que la única lectura posible era que el clic no había hecho
  /// nada, y lo natural es volver a pulsarlo.
  ///
  /// Además de contarlo, deshabilita la entrada. Ver [TrayPresence].
  final bool leaving;

  @override
  bool operator ==(Object other) =>
      other is TrayStatus &&
      other.line == line &&
      other.hasRoom == hasRoom &&
      other.leaving == leaving;

  @override
  int get hashCode => Object.hash(line, hasRoom, leaving);
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
