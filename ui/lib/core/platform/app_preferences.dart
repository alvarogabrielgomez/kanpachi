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
/// and nothing the daemon stores.
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
}
