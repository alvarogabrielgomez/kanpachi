import 'package:kanpachi_ui/core/messages/message_keys.dart';
import 'package:kanpachi_ui/features/shell/presentation/cubit/shell_cubit.dart';

/// A qué pantalla lleva un fallo, cuando lleva a alguna.
///
/// Son los DOS que se arreglan yendo a un sitio, y son dos pantallas distintas
/// porque son dos cosas: falta elegir el servidor, o falta la contraseña de uno
/// ya elegido. El resto de los fallos se quedan como aviso, que es lo correcto
/// para lo que se arregla reintentando o no se arregla.
///
/// # Quién lo lee, y por qué son dos preguntas
///
/// El oyente del marco lo usa para NAVEGAR, y cada sitio que pinta un
/// [FailureNotice] lo usa para CALLARSE: un fallo que ya llevó a su pantalla no
/// se cuenta además como aviso, porque el aviso diría «ese servidor pide
/// contraseña» debajo de la pantalla que la está pidiendo. Vivía privado en el
/// marco y los avisos no podían preguntarlo, así que el mismo fallo se
/// enseñaba dos veces.
AppScreen? screenForFailure(String? code) {
  if (code == FailureCode.noOwnSeed.wire) return AppScreen.seed;
  if (code == FailureCode.seedPassword.wire) return AppScreen.seedPassword;
  return null;
}
