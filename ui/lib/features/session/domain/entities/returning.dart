import 'package:flutter/foundation.dart';

/// Esta máquina volviendo a una sala en la que estuvo.
///
/// # No es reconectando, y no es reingresando
///
/// «Reconectando» es que se cayó el túnel **estando dentro** de una sala viva, y
/// está acotado a diez minutos. «Reingresando» es un invitado pidiéndole
/// credencial a su host otra vez, también desde dentro. Esto es no estar en
/// ninguna sala y seguir intentando, sin tope: lo que lo termina es que la sala
/// deje de existir, o que alguien lo diga.
///
/// Por eso convive con el campo de código en la portada. Se está esperando, no
/// se está dentro, y quien esté esperando puede cambiar de idea.
///
/// # Lo decide el daemon
///
/// La pantalla no deduce nada de esto. Si el objeto está, se está volviendo; si
/// no está, no. Es la misma regla que gobierna el resto del estado.
@immutable
class Returning {
  const Returning({
    required this.code,
    required this.seed,
    this.name = '',
    this.nextIn = Duration.zero,
    this.attempts = 0,
    this.reason = '',
  });

  final String code;
  final String seed;

  /// El nombre de la sala. Vacío cuando nunca tuvo uno.
  final String name;

  /// Cuánto falta para el intento siguiente. Cero es que hay uno corriendo.
  final Duration nextIn;

  /// Cuántos se llevan intentados. No acota nada: no hay número de fallos que
  /// signifique que la sala se acabó.
  final int attempts;

  /// Por qué falló el último, en las palabras del daemon. Vacío antes del
  /// primero.
  final String reason;

  /// Nulo cuando no se está volviendo a ninguna parte, que es el caso normal.
  static Returning? fromJson(Object? crudo) {
    if (crudo is! Map<String, Object?>) return null;
    final String code = crudo['code'] as String? ?? '';
    if (code.isEmpty) return null;
    return Returning(
      code: code,
      seed: crudo['seed'] as String? ?? '',
      name: crudo['name'] as String? ?? '',
      nextIn: Duration(
        milliseconds: (crudo['next_in_ms'] as num?)?.toInt() ?? 0,
      ),
      attempts: (crudo['attempts'] as num?)?.toInt() ?? 0,
      reason: crudo['reason'] as String? ?? '',
    );
  }

  @override
  bool operator ==(Object other) =>
      other is Returning &&
      other.code == code &&
      other.seed == seed &&
      other.name == name &&
      other.nextIn == nextIn &&
      other.attempts == attempts &&
      other.reason == reason;

  @override
  int get hashCode => Object.hash(code, seed, name, nextIn, attempts, reason);
}
