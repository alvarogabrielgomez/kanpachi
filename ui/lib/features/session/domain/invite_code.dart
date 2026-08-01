/// El invite ID, con las mismas reglas que `core/domain` en Go.
///
/// Está duplicado entre Dart y Go a la fuerza: la app tiene que decidir si
/// habilita "Unirse" antes de hablar con nadie. Si los dos lados se separan,
/// la app acepta códigos que el daemon rechaza y el usuario concluye que la
/// app está rota. Cualquier cambio acá va acompañado del mismo cambio allá.
abstract final class InviteCode {
  /// Sin 0, 1, I ni O: no hay pareja ambigua que adivinar cuando alguien dicta
  /// un código en voz alta.
  static const String alphabet = '23456789ABCDEFGHJKLMNPQRSTUVWXYZ';

  static const int length = 8;

  /// Quita separadores y pasa a mayúsculas, igual que `normalizeInviteID`.
  static String normalize(String raw) {
    final StringBuffer out = StringBuffer();
    for (final String c in raw.toUpperCase().split('')) {
      if (c == '-' || c == ' ' || c == '_' || c == '\t') continue;
      out.write(c);
    }
    return out.toString();
  }

  /// Deja sólo lo que existe en el alfabeto. Descartar al vuelo lo que no
  /// pertenece es lo que hace útil que el alfabeto no tenga caracteres
  /// ambiguos; aceptarlos para rechazarlos al final desperdicia esa decisión.
  static String keepValid(String raw) {
    final StringBuffer out = StringBuffer();
    for (final String c in raw.toUpperCase().split('')) {
      if (alphabet.contains(c)) out.write(c);
    }
    return out.toString();
  }

  /// La forma canónica con guion: `A7K2-M9QX`.
  ///
  /// El guion es presentación y nada más: se ve, se copia y se dicta con él, y
  /// viaja sin él. Quien lo recibe lo descarta igual, pero mandarlo obligaría
  /// a que TODOS lo descarten, y basta uno que no lo haga para que la sala
  /// "no exista" sin nada que lo explique.
  static String format(String normalized) => normalized.length <= 4
      ? normalized
      : '${normalized.substring(0, 4)}-${normalized.substring(4)}';

  /// Enmascara lo que se está escribiendo: filtra, corta a ocho y pone el
  /// guion solo, para que el campo tenga siempre la forma del ejemplo.
  static String mask(String raw) {
    final String valid = keepValid(raw);
    return format(
      valid.length <= length ? valid : valid.substring(0, length),
    );
  }

  static bool isComplete(String raw) {
    final String n = normalize(raw);
    if (n.length != length) return false;
    for (int i = 0; i < n.length; i++) {
      if (!alphabet.contains(n[i])) return false;
    }
    return true;
  }
}
