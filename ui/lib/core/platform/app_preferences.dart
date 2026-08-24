import 'dart:convert';
import 'dart:io';
import 'dart:ui' show Size;

import 'package:flutter/foundation.dart';
import 'package:kanpachi_ui/core/platform/app_log.dart';
import 'package:kanpachi_ui/core/platform/user_dir.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';

/// What this window remembers between runs.
///
/// # What belongs here, and what does not
///
/// Presentation only. **The room, the members, the active game and the
/// protection live in the daemon and are never mirrored here**: closing the
/// window does not close the room, so the state has to survive the window and
/// not the other way round. Keeping a copy on this side would create two
/// truths that drift apart in exactly the case the product promises to
/// support. See `docs/03`.
///
/// What is left is the handful of things the daemon has no opinion about: the
/// window size, and whether this window narrates what the daemon is doing.
///
/// **Autostart is deliberately NOT here.** Whether Kanpachi comes up with
/// Windows is the Service Control Manager's answer to give, and it is asked
/// over the pipe. A copy on this side would be a second truth for one fact,
/// and the one on screen would be the one that does not decide.
///
/// **And the NICKNAME is no longer here either**, for exactly that reason. It
/// lived here on the argument that the daemon did not store it, which was true
/// and beside the point: the terminal kept its own copy in the same folder, so
/// one machine had two names and the room showed whichever face entered it.
/// Measured on 2026-08-18. It is daemon state now, read from `profile.json` by
/// [MachineProfile] and written over the pipe. That also fixes the harder half:
/// in the installed product this window cannot write in the data directory at
/// all, so what looked like saving a name was doing nothing.
///
/// # Why a file of ours and not `shared_preferences`
///
/// Because `shared_preferences` writes to `%APPDATA%\<company>\<product>`, and
/// that is a place a portable copy has no business touching. Measured: the
/// nickname, the window size and the narration flag were living in one file
/// shared by the portable copy, the installed product and every debug run on
/// this machine. So a portable Kanpachi arrived on another PC with no name
/// even though its data folder travelled whole, deleting the folder left that
/// file behind, and two builds could not disagree about the size of a window.
///
/// # Dónde acaba el fichero
///
/// **En la carpeta del daemon cuando se puede escribir ahí, y si no en la de la
/// persona.** En una copia portable se puede, y eso es lo que sostiene su
/// promesa: todo lo que recuerda vive en la carpeta desde la que se abrió. En el
/// producto instalado no se puede, porque los usuarios solo LEEN
/// `C:\ProgramData\Kanpachi`, así que va a `%LOCALAPPDATA%\Kanpachi`. Que es
/// además donde le tocaba: un tamaño de ventana es de quien mira la ventana, no
/// de la máquina. La regla entera está en [UserDir]; quién decide la carpeta del
/// daemon, en `PipeNames.dataDir`.
class AppPreferences {
  AppPreferences._(this._file, this._values, this._defaultVerbose);

  /// The file, beside the token and the identity key.
  static const String fileName = 'ui-prefs.json';

  static const String _verboseKey = 'verbose';
  static const String _windowWidthKey = 'window_width';
  static const String _windowHeightKey = 'window_height';
  static const String _pendingUpdateKey = 'pending_update';

  /// Null when there is nowhere to write, which is the case in tests. Every
  /// setter is then a no-op that still answers from memory.
  final File? _file;

  final Map<String, Object?> _values;

  /// What [verbose] answers while nobody has chosen. See that getter.
  final bool _defaultVerbose;

  /// Opens the store. Call once, before the first frame.
  ///
  /// `dir` and `defaultVerbose` are both decided by the caller and not read
  /// here on purpose: they depend on whether this is a portable copy and on
  /// what the daemon said on the command line, and that question belongs to
  /// `PipeNames`, which lives a layer out. This file must not reach into a
  /// feature to answer it.
  ///
  /// # Dónde acaba, y por qué no basta con `dir`
  ///
  /// Se prueban dos sitios en orden: la carpeta del daemon, y la de la persona
  /// que abrió la ventana. En una copia portable gana la primera, que es lo que
  /// mantiene la promesa de que todo lo que recuerda vive en su carpeta. En el
  /// producto instalado esa carpeta es `C:\ProgramData\Kanpachi`, donde los
  /// usuarios **leen y no escriben** por decisión del instalador, así que gana
  /// la segunda. Ver [UserDir], donde está escrita la regla.
  ///
  /// **Apuntar solo a `dir` no era guardar, era aparentar.** Medido el
  /// 2026-08-23 en el producto instalado: `ui-prefs.json` no existía en la
  /// carpeta del daemon y nunca había existido, así que el modo verboso se
  /// apagaba en cada arranque, la ventana no recordaba su tamaño, y el fallo lo
  /// tragaba un `debugPrint` que en una compilación de release no imprime. El
  /// mismo problema ya lo había resuelto [AppLog] con esta misma forma, y su
  /// registro cayendo a la carpeta de la persona era la prueba en vivo.
  ///
  /// # Por qué se PRUEBA a escribir en vez de mirar si existe
  ///
  /// Porque crear la carpeta sale bien en un sitio donde después no se puede
  /// crear un archivo dentro, que es exactamente la forma del permiso de
  /// `C:\ProgramData`. La prueba usa el mismo `.tmp` que usa [_save], así que
  /// comprueba la operación que de verdad importa y no una parecida.
  ///
  /// **A store that cannot be read opens EMPTY instead of failing.** What is
  /// lost then is a window size; what refusing would cost is the window, and
  /// with it the tray icon and the only way to close a room.
  static Future<AppPreferences> open({
    String? dir,
    bool defaultVerbose = kDebugMode,
  }) async {
    // Sin carpeta que probar no hay tienda, y ese caso se conserva a propósito:
    // es el de los tests, y caer al sitio de la persona los pondría a escribir
    // en el `%LOCALAPPDATA%` de quien los corre.
    if (dir == null || dir.trim().isEmpty) {
      return AppPreferences._(null, <String, Object?>{}, defaultVerbose);
    }
    final File? file = _primerSitioEscribible(<String?>[dir, UserDir.path]);
    if (file == null) {
      AppLog.warn('los ajustes de la ventana no se van a poder guardar');
      return AppPreferences._(null, <String, Object?>{}, defaultVerbose);
    }
    Map<String, Object?> values = <String, Object?>{};
    try {
      if (file.existsSync()) {
        final Object? read = jsonDecode(await file.readAsString());
        if (read is Map<String, dynamic>) {
          values = Map<String, Object?>.from(read);
          // La clave del nombre se retira al abrir, así el primer cambio de
          // cualquier ajuste deja el fichero sin ella. No se lee para nada: el
          // daemon ya la adoptó en su arranque, y dejarla acá sería dejar a la
          // vista un valor que no manda en nada. Ver [MachineProfile].
          values.remove('nickname');
        }
      }
    } on Object catch (e) {
      // Se dice y se sigue con lo que hay. Un JSON a medias es un arranque
      // interrumpido, y la respuesta correcta es empezar de cero: el fichero
      // se reescribe entero en el primer cambio.
      AppLog.warn('los ajustes de la ventana no se pudieron leer', '$e');
    }
    return AppPreferences._(file, values, defaultVerbose);
  }

  /// El primero de los candidatos donde se pueda escribir de verdad.
  ///
  /// Null cuando ninguno sirve, que es el caso de los tests: entonces todo
  /// contesta de memoria y guardar no hace nada, dicho en el registro.
  static File? _primerSitioEscribible(List<String?> candidatos) {
    for (final String? candidato in candidatos) {
      if (candidato == null || candidato.trim().isEmpty) continue;
      final String base = candidato.trim();
      try {
        Directory(base).createSync(recursive: true);
        final File file = File('$base${Platform.pathSeparator}$fileName');
        final File temp = File('${file.path}.tmp');
        temp.writeAsStringSync('', flush: true);
        temp.deleteSync();
        return file;
      } on Object {
        // Ese sitio no sirve y se prueba el siguiente. No se anota acá: fallar
        // en la carpeta del daemon es lo NORMAL en el producto instalado, y un
        // aviso por cada arranque sería ruido sobre algo que funciona.
      }
    }
    return null;
  }

  /// Vuelca el mapa entero, y por un fichero temporal.
  ///
  /// Escribir encima del bueno deja el fichero a medias si la máquina se apaga
  /// a mitad, y lo que se pierde entonces no es el cambio sino todo lo demás.
  /// Renombrar es la operación que Windows hace de una pieza.
  Future<void> _save() async {
    final File? file = _file;
    if (file == null) return;
    try {
      final File temp = File('${file.path}.tmp');
      await temp.writeAsString(jsonEncode(_values), flush: true);
      await temp.rename(file.path);
    } on Object catch (e) {
      // Al registro y no a `debugPrint`, que en release no imprime: así es como
      // el producto instalado se pasó meses sin guardar un ajuste y sin dejar
      // rastro de que lo intentaba.
      AppLog.warn('los ajustes de la ventana no se pudieron guardar', '$e');
    }
  }

  String _texto(String key) {
    final Object? v = _values[key];
    return v is String ? v.trim() : '';
  }

  /// Whether this window narrates, step by step, what the daemon is doing.
  ///
  /// # Why the default depends on the build and the setting does not
  ///
  /// **Debug builds start with it ON, release builds with it OFF, and either
  /// one can be changed from the settings screen.** It used to be `kDebugMode`
  /// and nothing else, which was a compile-time constant: the panel, the poll
  /// and the extra pipe connection were all tree-shaken out of a release
  /// build. Cheaper, and useless in the only situation that matters — a room
  /// that will not open on somebody else's machine, running the binary that
  /// ships, where the eight steps are the whole diagnosis and rebuilding is
  /// not on the table.
  ///
  /// What that costs, said plainly: a release build now carries the panel, and
  /// with the setting on it opens a second connection to the daemon and asks
  /// for the diary a couple of times a second while a room is opening. Off, it
  /// asks for nothing.
  /// A portable copy starts with it ON, and that is the third case rather than
  /// an exception. A portable Kanpachi is what somebody runs when the installed
  /// one is not an option — a friend trying it for ten minutes, or a machine
  /// where something is wrong — and in both the steps ARE the reason it is
  /// being run. It is still a default: the settings screen overrides it and
  /// the choice is remembered.
  bool get verbose {
    final Object? v = _values[_verboseKey];
    return v is bool ? v : _defaultVerbose;
  }

  Future<void> setVerbose({required bool enabled}) async {
    _values[_verboseKey] = enabled;
    await _save();
  }

  /// How big the window was when it was last closed, or null the first time.
  ///
  /// # Why the size is remembered and the POSITION is not
  ///
  /// They look like the same setting and they are not. Somebody who made the
  /// window taller wants it taller next time: it is a choice about the app.
  /// Where it happened to sit is a choice about that moment — which monitor
  /// was plugged in, what else was open — and restoring it is how a window
  /// comes back half off the screen after unplugging a second display, or on
  /// the monitor that is now switched off. Centred always is boring and always
  /// works.
  ///
  /// Read as two numbers rather than one string, because half a size is not a
  /// size: with one missing, this returns null and the default applies.
  ///
  /// `num` and not `double`, because JSON gives back `1024` as an int and
  /// `1024.0` as a double, and the two mean the same width.
  Size? get windowSize {
    final Object? w = _values[_windowWidthKey];
    final Object? h = _values[_windowHeightKey];
    if (w is! num || h is! num) return null;
    return Size(w.toDouble(), h.toDouble());
  }

  /// A published version newer than this build, found in an earlier run.
  ///
  /// Empty when there is none known. It is stored rather than asked for again
  /// every start for one reason: **once the answer is yes it cannot change
  /// back**. A version that was published stays published, so a second question
  /// can only get the same answer, and the notice has to survive a restart or
  /// it is a notice that only exists while the machine has network.
  ///
  /// What clears it is not a timer, it is arriving: the version stored stops
  /// being newer than the one running, and the cubit drops it. See
  /// `features/update`.
  String get pendingUpdate => _texto(_pendingUpdateKey);

  Future<void> setPendingUpdate(String version) async {
    _values[_pendingUpdateKey] = version.trim();
    await _save();
  }

  Future<void> clearPendingUpdate() async {
    _values.remove(_pendingUpdateKey);
    await _save();
  }

  /// Remembers the window size, clamped to what the app can actually draw.
  ///
  /// The clamp is not paranoia: a stored size below the minimum comes back as
  /// a window the layout cannot fit, and the overflow it produces is
  /// unreachable because the window cannot be made smaller than the minimum
  /// anyway. Storing something the app would refuse to honour just means the
  /// file disagrees with the screen.
  Future<void> setWindowSize(Size size) async {
    _values[_windowWidthKey] = size.width.clamp(
      AppSpacing.minWindow.width,
      10000.0,
    );
    _values[_windowHeightKey] = size.height.clamp(
      AppSpacing.minWindow.height,
      10000.0,
    );
    await _save();
  }
}
