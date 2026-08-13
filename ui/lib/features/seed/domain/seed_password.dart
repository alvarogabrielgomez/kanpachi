/// La regla del password de un registro, espejo de `core/domain/seedauth.go`.
///
/// # Por qué está duplicada
///
/// Porque la pantalla tiene que decidir si habilita el botón antes de hablar con
/// nadie. Si los dos lados se separan, la app acepta lo que el daemon rechaza y
/// quien lo escribe concluye que la app está rota. Un test de lockstep lee las
/// constantes de Go y las compara con estas, que es el mecanismo que este
/// repositorio ya usa para los enums del cable.
///
/// # Lo que NO está acá, y no es un olvido
///
/// El hash. `SeedAuthProof` lleva el host del registro dentro y lo calcula el
/// daemon, que es quien sabe a qué registro abre salas esta máquina. Que la
/// interfaz lo calculara sería un segundo sitio donde equivocarse de host, y
/// equivocarse de host produce una credencial que no vale en ningún sitio.
abstract final class SeedPassword {
  /// Cuatro es bajo a propósito: lo que guarda esa puerta es el freno de tasa y
  /// el de memoria del registro, no la entropía de lo que alguien teclea. Un
  /// seed compartido con cinco amigos es el caso normal.
  static const int minLength = 4;

  /// Tope técnico y nada más. Existe para que nadie mande un megabyte a
  /// derivar, no para limitar a quien quiera una frase larga.
  static const int maxLength = 128;

  /// Si lo escrito sirve para mandarlo. Es la condición del botón.
  ///
  /// Cuenta CARACTERES y no bytes, igual que Go: una regla enunciada en
  /// caracteres que se aplicara en bytes dejaría pasar dos emoji como si fueran
  /// ocho letras.
  static bool isValid(String password) {
    final int n = password.runes.length;
    return n >= minLength && n <= maxLength;
  }
}
