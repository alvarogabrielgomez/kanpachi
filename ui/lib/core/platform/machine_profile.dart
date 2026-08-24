import 'dart:convert';
import 'dart:io';

import 'package:flutter/foundation.dart';
import 'package:kanpachi_ui/core/platform/machine_dir.dart';

/// What the DAEMON remembers about the person, read from disk by this window.
///
/// # Why this is read and never written here
///
/// Because the name is one fact about the machine and there are three faces
/// asking about it — this window, the terminal and the wizard. While nobody
/// owned it, this window wrote its own copy into `ui-prefs.json` and the
/// terminal wrote another into `nickname.txt`, in the same folder, and the room
/// showed whichever face entered it: measured on 2026-08-18, a window saying
/// "Alvaro" and a room showing "AlvaroGDeskt".
///
/// And there is a harder reason than tidiness. In the installed product the
/// data directory leaves Users read-only and this window runs with the session
/// user's token, so it is the one process of the three that CANNOT write there.
/// A write that quietly fails is worse than no write: the name looks saved and
/// the next start asks for it again.
///
/// Changing the name goes over the pipe, through `nickname`, like every other
/// thing the daemon owns. See `SessionRepository.setNickname`.
///
/// # Why it reads the file instead of asking
///
/// Because the answer is needed BEFORE the first frame, to decide between the
/// sign-up screens and the home, and asking over the pipe would make that
/// decision wait for the daemon to be up. The screen would flicker from one to
/// the other on every start, or the window would sit there. Reading a file the
/// daemon left behind is not a new shape either: `readApiToken` already does
/// exactly this, in this same directory.
///
/// This is why `profile.json` is in the clear and not sealed like the room.
class MachineProfile {
  MachineProfile._(this._nickname);

  /// The file, beside the token, the identity key and the seed.
  static const String fileName = 'profile.json';

  final String _nickname;

  /// Opens the store. Call once, before the first frame.
  ///
  /// **Unreadable, absent or half written all open EMPTY**, same policy as
  /// `AppPreferences.open` and for the same reason: what is lost then is a
  /// name, and what refusing would cost is the window. An empty one reads as
  /// "nobody chose a name", which is exactly what a fresh install is.
  static Future<MachineProfile> open({String? dir}) async {
    if (dir == null || dir.trim().isEmpty) return MachineProfile._('');
    final File file = File(MachineDir.join(dir.trim(), fileName));
    try {
      if (!file.existsSync()) return MachineProfile._('');
      final Object? read = jsonDecode(await file.readAsString());
      if (read is Map<String, dynamic>) {
        final Object? nick = read['nickname'];
        if (nick is String) return MachineProfile._(nick.trim());
      }
    } on Object catch (e) {
      debugPrint('el perfil de esta máquina no se pudo leer: $e');
    }
    return MachineProfile._('');
  }

  /// Builds one from a value already known, for the tests and for the moment
  /// right after the daemon confirms a new name.
  factory MachineProfile.of(String nickname) =>
      MachineProfile._(nickname.trim());

  /// The name you are seen by, or empty if nobody chose one yet.
  String get nickname => _nickname;

  /// Whether the sign-up is done.
  ///
  /// It is the same question as having a name, and that is deliberate: signing
  /// up is two screens and the second one is the name, so a second flag would
  /// be a second source of truth for one fact. The day they disagreed, the app
  /// would either ask again for a name it already has or skip a step that
  /// never happened.
  bool get onboarded => _nickname.isNotEmpty;
}
