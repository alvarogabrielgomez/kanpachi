import 'dart:convert';
import 'dart:io';
import 'dart:ui' show Size;

import 'package:flutter/foundation.dart';
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
/// name you are seen by, which is an argument to `create_room` and `join_room`
/// and nothing the daemon stores, and whether this window narrates what the
/// daemon is doing.
///
/// **Autostart is deliberately NOT here.** Whether Kanpachi comes up with
/// Windows is the Service Control Manager's answer to give, and it is asked
/// over the pipe. A copy on this side would be a second truth for one fact,
/// and the one on screen would be the one that does not decide.
///
/// # Why the nickname doubles as the onboarding flag
///
/// Because onboarding is two screens and the second one is the nickname, so
/// "has a nickname" and "has been through onboarding" are the same fact. A
/// second key would be a second source of truth for one thing, and the day
/// they disagreed the app would either ask again for a name it already has or
/// skip a step that never happened.
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
/// The store goes where the daemon's data goes, which is what makes the
/// promise true: everything a portable copy remembers sits in the folder it
/// was run from. See `PipeNames.dataDir` for who decides that folder.
class AppPreferences {
  AppPreferences._(this._file, this._values, this._defaultVerbose);

  /// The file, beside the token and the identity key.
  static const String fileName = 'ui-prefs.json';

  static const String _nicknameKey = 'nickname';
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
  /// **A store that cannot be read opens EMPTY instead of failing.** What is
  /// lost then is a nickname and a window size; what refusing would cost is
  /// the window, and with it the tray icon and the only way to close a room.
  static Future<AppPreferences> open({
    String? dir,
    bool defaultVerbose = kDebugMode,
  }) async {
    if (dir == null || dir.trim().isEmpty) {
      return AppPreferences._(null, <String, Object?>{}, defaultVerbose);
    }
    final File file = File('${dir.trim()}\\$fileName');
    Map<String, Object?> values = <String, Object?>{};
    try {
      if (file.existsSync()) {
        final Object? read = jsonDecode(await file.readAsString());
        if (read is Map<String, dynamic>) {
          values = Map<String, Object?>.from(read);
        }
      }
    } on Object catch (e) {
      // Se dice y se sigue con lo que hay. Un JSON a medias es un arranque
      // interrumpido, y la respuesta correcta es empezar de cero: el fichero
      // se reescribe entero en el primer cambio.
      debugPrint('los ajustes de la ventana no se pudieron leer: $e');
    }
    return AppPreferences._(file, values, defaultVerbose);
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
      debugPrint('los ajustes de la ventana no se pudieron guardar: $e');
    }
  }

  String _texto(String key) {
    final Object? v = _values[key];
    return v is String ? v.trim() : '';
  }

  /// The name you are seen by, or empty if there is none yet.
  String get nickname => _texto(_nicknameKey);

  /// Whether onboarding is done. See the class doc for why this is the same
  /// question as having a nickname.
  bool get onboarded => nickname.isNotEmpty;

  /// Saves the nickname. An empty one is REMOVED rather than stored, so that
  /// "no name" is one state and not two.
  Future<void> setNickname(String value) async {
    final String clean = value.trim();
    if (clean.isEmpty) {
      _values.remove(_nicknameKey);
    } else {
      _values[_nicknameKey] = clean;
    }
    await _save();
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
