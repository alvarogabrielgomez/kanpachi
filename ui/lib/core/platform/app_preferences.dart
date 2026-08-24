import 'dart:ui' show Size;

import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';
import 'package:kanpachi_ui/core/platform/app_log.dart';
import 'package:kanpachi_ui/core/platform/machine_settings.dart';

/// What this machine remembers about how it presents itself.
///
/// # It is not "what the window remembers", and that changed on purpose
///
/// This used to write a file of its own, `ui-prefs.json`, and the line that
/// separated it from the daemon's was not a design decision: it was a
/// permission. The installed product leaves Users read-only on the data
/// directory, so anything the window wanted to keep BY ITSELF had to go
/// somewhere else.
///
/// Two of the three things in it were never about a window. Narration turns on
/// the DAEMON's step diary, published over `progress` for whoever asks, and the
/// newer-version answer is one the terminal asks the same rate-limited channel
/// for. The third, the window size, is genuinely about a pane of glass, and it
/// still belongs to the machine, because everything Kanpachi remembers does:
/// there is one room, one adapter, one identity. See decision 42.
///
/// # So the window reads, and asks to write
///
/// Reading is a disk read before the first frame, of the file the daemon left,
/// exactly as [MachineProfile] reads the name and `readApiToken` reads the
/// token. Writing goes over the pipe, exactly as the name, the registry and
/// starting with Windows already do.
///
/// **What that fixes is not tidiness.** In the installed product this window
/// could never write in the data directory, `_save` failed, and the failure
/// went into a `debugPrint` that a release build does not print: narration
/// switched itself off on every start and the window forgot its size, for
/// months, without leaving a line anywhere.
class AppPreferences {
  AppPreferences._(this._save, this._values);

  /// Opens the store with what the window already read off disk.
  ///
  /// **It costs no round trip**, and that is deliberate: this runs before the
  /// first frame, and making the first screen wait for the daemon is what would
  /// make the start-up flicker between onboarding and the home.
  ///
  /// `save` is how a change gets written. Null leaves every setter answering
  /// from memory and writing nowhere, which is what the tests want and what a
  /// window with no daemon behind it gets.
  static Future<AppPreferences> open({
    required MachineSettings initial,
    SaveSettings? save,
  }) async => AppPreferences._(save, initial);

  /// Null when nobody can write, which is the case in tests. Every setter is
  /// then a no-op that still answers from memory.
  final SaveSettings? _save;

  /// Lo último que se sabe. Se reemplaza con lo que conteste el daemon, que es
  /// quien manda, y con lo pedido cuando el daemon no contesta.
  MachineSettings _values;

  /// Whether this machine narrates, step by step, what the daemon is doing.
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
  ///
  /// A portable copy starts with it ON, and that is the third case rather than
  /// an exception. A portable Kanpachi is what somebody runs when the installed
  /// one is not an option — a friend trying it for ten minutes, or a machine
  /// where something is wrong — and in both the steps ARE the reason it is
  /// being run. It is still a default: the settings screen overrides it and
  /// the choice is remembered.
  ///
  /// The default is applied where the file is READ, by whoever knows whether
  /// this is a portable copy, so by the time it gets here the answer is already
  /// somebody's.
  bool get verbose => _values.verbose;

  Future<void> setVerbose({required bool enabled}) => _pedir(
    () => _save!(verbose: enabled),
    _values.copyWith(verbose: enabled),
  );

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
  /// Two numbers and not one, because half a size is not a size: with either
  /// missing this answers null and the default applies.
  Size? get windowSize => _values.hasWindowSize
      ? Size(_values.windowWidth.toDouble(), _values.windowHeight.toDouble())
      : null;

  /// A published version newer than this build, found in an earlier run.
  ///
  /// Empty when there is none known. It is stored rather than asked for again
  /// every start for one reason: **once the answer is yes it cannot change
  /// back**. A version that was published stays published, so a second question
  /// can only get the same answer, and the notice has to survive a restart or
  /// it is a notice that only exists while the machine has network.
  ///
  /// **And the terminal shares it**, since it moved into the daemon's file:
  /// `kanpachi upgrade --check` answers from here instead of asking a channel
  /// that allows sixty questions an hour per IP, and writes down what it finds
  /// so this window does not have to ask either.
  ///
  /// What clears it is not a timer, it is arriving: the version stored stops
  /// being newer than the one running, and the cubit drops it. See
  /// `features/update`.
  String get pendingUpdate => _values.pendingUpdate;

  Future<void> setPendingUpdate(String version) => _pedir(
    () => _save!(pendingUpdate: version.trim()),
    _values.copyWith(pendingUpdate: version.trim()),
  );

  Future<void> clearPendingUpdate() => setPendingUpdate('');

  /// Remembers the window size, clamped to what the app can actually draw.
  ///
  /// The clamp is not paranoia: a stored size below the minimum comes back as
  /// a window the layout cannot fit, and the overflow it produces is
  /// unreachable because the window cannot be made smaller than the minimum
  /// anyway. Storing something the app would refuse to honour just means the
  /// file disagrees with the screen.
  ///
  /// **It stays on this side and does not travel**, because `AppSpacing` is a
  /// token of the interface and the daemon has no business knowing what a
  /// screen gets drawn with.
  Future<void> setWindowSize(Size size) {
    final int w = size.width.clamp(AppSpacing.minWindow.width, 10000.0).round();
    final int h = size.height
        .clamp(AppSpacing.minWindow.height, 10000.0)
        .round();
    return _pedir(
      () => _save!(windowWidth: w, windowHeight: h),
      _values.copyWith(windowWidth: w, windowHeight: h),
    );
  }

  /// Pide el cambio y se queda con lo que conteste el daemon.
  ///
  /// **No lanza nunca.** Lo que se pierde cuando esto falla es un ajuste; lo
  /// que costaría negarse es la ventana, y con ella el icono de la bandeja y la
  /// única forma de cerrar una sala. Se anota, y al registro y no a un
  /// `debugPrint`: el fallo mudo es exactamente lo que dejó a este fichero sin
  /// guardar nada durante meses.
  Future<void> _pedir(
    Future<MachineSettings> Function() llamada,
    MachineSettings siFalla,
  ) async {
    if (_save == null) {
      _values = siFalla;
      return;
    }
    try {
      _values = await llamada();
    } on Object catch (e) {
      _values = siFalla;
      AppLog.warn('los ajustes de la ventana no se pudieron guardar', '$e');
    }
  }
}
