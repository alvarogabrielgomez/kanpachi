import 'dart:ui' show Size;

import 'package:flutter/foundation.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';
import 'package:shared_preferences/shared_preferences.dart';

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
class AppPreferences {
  const AppPreferences._(this._prefs);

  static const String _nicknameKey = 'nickname';
  static const String _verboseKey = 'verbose';
  static const String _windowWidthKey = 'window_width';
  static const String _windowHeightKey = 'window_height';
  static const String _pendingUpdateKey = 'pending_update';

  final SharedPreferences _prefs;

  /// Opens the store. Call once, before the first frame.
  static Future<AppPreferences> open() async =>
      AppPreferences._(await SharedPreferences.getInstance());

  /// The name you are seen by, or empty if there is none yet.
  String get nickname => _prefs.getString(_nicknameKey)?.trim() ?? '';

  /// Whether onboarding is done. See the class doc for why this is the same
  /// question as having a nickname.
  bool get onboarded => nickname.isNotEmpty;

  /// Saves the nickname. An empty one is REMOVED rather than stored, so that
  /// "no name" is one state and not two.
  Future<void> setNickname(String value) async {
    final String clean = value.trim();
    if (clean.isEmpty) {
      await _prefs.remove(_nicknameKey);
      return;
    }
    await _prefs.setString(_nicknameKey, clean);
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
  bool get verbose => _prefs.getBool(_verboseKey) ?? kDebugMode;

  Future<void> setVerbose({required bool enabled}) async =>
      _prefs.setBool(_verboseKey, enabled);

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
  /// Read as two doubles rather than one string, because that is what
  /// `shared_preferences` stores natively and because half a size is not a
  /// size: with one missing, this returns null and the default applies.
  Size? get windowSize {
    final double? w = _prefs.getDouble(_windowWidthKey);
    final double? h = _prefs.getDouble(_windowHeightKey);
    if (w == null || h == null) return null;
    return Size(w, h);
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
  String get pendingUpdate => _prefs.getString(_pendingUpdateKey)?.trim() ?? '';

  Future<void> setPendingUpdate(String version) async =>
      _prefs.setString(_pendingUpdateKey, version.trim());

  Future<void> clearPendingUpdate() async => _prefs.remove(_pendingUpdateKey);

  /// Remembers the window size, clamped to what the app can actually draw.
  ///
  /// The clamp is not paranoia: a stored size below the minimum comes back as
  /// a window the layout cannot fit, and the overflow it produces is
  /// unreachable because the window cannot be made smaller than the minimum
  /// anyway. Storing something the app would refuse to honour just means the
  /// file disagrees with the screen.
  Future<void> setWindowSize(Size size) async {
    final double w = size.width.clamp(AppSpacing.minWindow.width, 10000.0);
    final double h = size.height.clamp(AppSpacing.minWindow.height, 10000.0);
    await _prefs.setDouble(_windowWidthKey, w);
    await _prefs.setDouble(_windowHeightKey, h);
  }
}
