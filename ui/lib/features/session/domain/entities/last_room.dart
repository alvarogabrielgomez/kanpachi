import 'package:flutter/foundation.dart';

/// La última sala AJENA en la que esta máquina estuvo, tal como el daemon la
/// recuerda.
///
/// # Qué significa que exista con [autoReturn] apagado
///
/// Que la máquina salió a propósito o la expulsaron, y el daemon conserva el
/// camino de vuelta sin recorrerlo solo. Es la mitad manual de la promesa de
/// la 0.4.0: ser expulsado no borra la sala, deja de volverse solo. La vuelta
/// es entrar otra vez con el mismo código, con la misma confirmación de
/// confianza que la primera vez.
///
/// Con [autoReturn] encendido no hay nada que ofrecer: el daemon está
/// volviendo solo y eso ya lo cuenta `returning` con su propio aviso. La
/// ventana solo enseña esta entidad en el caso apagado — se supo el
/// 2026-08-18 que ese caso existía en el CLI y el asistente y la ventana no
/// llamaba jamás al método.
@immutable
class LastRoom {
  const LastRoom({
    required this.code,
    this.seed = '',
    this.name = '',
    this.nick = '',
    this.autoReturn = false,
    this.savedAt,
  });

  factory LastRoom.fromJson(Map<String, Object?> json) => LastRoom(
    code: json['code'] as String? ?? '',
    seed: json['seed'] as String? ?? '',
    name: json['name'] as String? ?? '',
    nick: json['nick'] as String? ?? '',
    autoReturn: json['auto_return'] as bool? ?? false,
    savedAt: DateTime.tryParse(json['saved_at'] as String? ?? '')?.toLocal(),
  );

  final String code;
  final String seed;
  final String name;
  final String nick;
  final bool autoReturn;
  final DateTime? savedAt;

  /// La forma completa que entra por el campo de código: CÓDIGO@servidor.
  String get invite => seed.isEmpty ? code : '$code@$seed';

  /// Sin código no hay vuelta que ofrecer, igual que en SavedRoom.
  bool get understood => code.isNotEmpty;
}
