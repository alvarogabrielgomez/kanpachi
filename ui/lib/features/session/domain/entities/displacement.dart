import 'package:flutter/foundation.dart';

/// Qué habría que dejar atrás para entrar a una sala AHORA MISMO.
///
/// # Lo calcula el daemon, y esta clase solo lo lee
///
/// Es la regla del proyecto aplicada a este caso: las decisiones viven en el
/// daemon y las caras pintan. Él sabe si hay sala, de quién es y si hay una
/// vuelta en marcha, y publica la respuesta en el estado y en la vista previa
/// de un código. La terminal la lee desde el primer día; esta ventana la
/// deducía por su cuenta de «hay vuelta», que era una tercera copia de una
/// regla ya contestada, y solo la aplicaba en dos de los seis sitios que
/// entran a una sala.
///
/// El daemon no puede preguntar: corre como SYSTEM, sin ventana y sin
/// terminal. Por eso la mitad que decide y la mitad que pregunta están
/// separadas, y solo la primera se comparte. Ver `core/domain/displacement.go`.
///
/// # Su ausencia es el caso normal
///
/// Casi siempre entrar no cuesta nada: no hay sala y no hay a dónde volver.
/// Entonces el campo no viaja y esto es nulo.
@immutable
class Displacement {
  const Displacement({
    required this.kind,
    required this.code,
    this.seed = '',
    this.name = '',
    this.members = 0,
  });

  /// Nulo cuando entrar no cuesta nada, que es la respuesta habitual.
  static Displacement? fromJson(Object? crudo) {
    if (crudo is! Map<String, Object?>) return null;
    final DisplaceKind kind = DisplaceKind.fromWire(crudo['kind'] as String?);
    if (kind == DisplaceKind.nothing) return null;
    return Displacement(
      kind: kind,
      code: crudo['code'] as String? ?? '',
      seed: crudo['seed'] as String? ?? '',
      name: crudo['name'] as String? ?? '',
      members: (crudo['members'] as num?)?.toInt() ?? 0,
    );
  }

  final DisplaceKind kind;

  final String code;
  final String seed;

  /// El nombre de lo que se deja atrás. Vacío cuando nunca tuvo uno.
  final String name;

  /// Cuánta gente MÁS se cae. Solo lo llena [DisplaceKind.closeRoom], que es el
  /// único caso donde entrar le cuesta algo a terceros.
  final int members;

  /// Cómo nombrar lo que se deja: su nombre, y el código cuando no tiene.
  String get label => name.isEmpty ? code : name;

  @override
  bool operator ==(Object other) =>
      other is Displacement &&
      other.kind == kind &&
      other.code == code &&
      other.seed == seed &&
      other.name == name &&
      other.members == members;

  @override
  int get hashCode => Object.hash(kind, code, seed, name, members);
}

/// Qué clase de cosa está en medio.
///
/// Las tres cuestan cosas distintas y por eso no se colapsan en un booleano:
/// cerrar la sala propia la termina para todos, salir de la ajena solo cuesta
/// tu sitio, y dejar de volver no cuesta nada que exista todavía.
enum DisplaceKind {
  /// El cable no mandó nada: entrar no cuesta nada.
  nothing(''),

  /// Estás DENTRO de la sala de otro. Entrar a otra es salir de esta.
  leaveRoom('leave_room'),

  /// Hospedas la tuya. Entrar a otra la **termina**: se borra su fichero, se
  /// retira del registro, y su código deja de resolver. No queda nada que
  /// reabrir.
  closeRoom('close_room'),

  /// Estás volviendo a una sala. No hay nada montado, así que no se cae nada:
  /// lo que cuesta es dejar de intentarlo.
  stopReturning('stop_returning'),

  /// Una clase que esta versión de la ventana no conoce.
  ///
  /// Existe para que un daemon más nuevo no consiga que se entre a una sala sin
  /// preguntar: lo desconocido se pregunta igual, con el texto genérico. Se
  /// pierde el copy bueno, no se pierde la pregunta. Mismo criterio que
  /// [ProgressScope.unknown] y que los avisos de salud.
  unknown('?');

  const DisplaceKind(this.wire);

  final String wire;

  static DisplaceKind fromWire(String? s) {
    if (s == null || s.isEmpty) return nothing;
    for (final DisplaceKind v in values) {
      if (v.wire == s) return v;
    }
    return unknown;
  }
}
