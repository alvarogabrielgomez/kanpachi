import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:kanpachi_ui/core/platform/app_preferences.dart';
import 'package:kanpachi_ui/core/platform/app_version.dart';
import 'package:kanpachi_ui/features/update/domain/repositories/update_repository.dart';
import 'package:kanpachi_ui/features/update/domain/version.dart';

@immutable
class UpdateState {
  const UpdateState({this.available});

  /// The published version that is newer than this one, or null.
  ///
  /// One field, and it holds a tag rather than a flag, because the notice says
  /// WHICH version: "there is a new version" is a claim the user cannot check,
  /// and "0.1.5 is out" is one they can compare against the About of anything.
  final String? available;

  @override
  bool operator ==(Object other) =>
      other is UpdateState && other.available == available;

  @override
  int get hashCode => available.hashCode;
}

/// Whether a version newer than this one has been published.
///
/// # What it does NOT do
///
/// It does not download anything, does not install anything, and does not ask
/// permission to. Kanpachi has no auto-update — see `docs/07-futuro.md`, which
/// parks it behind code signing — so the whole feature is one line at the
/// bottom of the window and a link to the download page. Saying that plainly
/// matters, because the obvious next commit is to make this button do the
/// update, and that button would be a service running as SYSTEM replacing its
/// own binary with an unsigned download.
///
/// # Why nothing here asks on its own
///
/// It used to ask at three moments — the window starting, a room opening, a
/// room closing — and that was fine while the question went to the seed: one
/// compiled host, with the answer already cached for everyone.
///
/// The question now goes to the publishing channel, and there the arithmetic
/// flips: GitHub's unauthenticated quota is sixty per hour PER IP, shared by
/// everyone behind the same router, and it learns the IP of every machine that
/// starts Kanpachi. Several questions per session per machine does not fit in
/// that; one press of a button does. So the trigger is a button in settings,
/// and there is no timer anywhere.
///
/// # Why it stops asking for good
///
/// Once a newer version is known, the answer is stored and the question is
/// never asked again. A published version does not get unpublished, so the
/// second answer can only be the same as the first, and the notice has to
/// outlive the run it was found in. It clears itself the day the running
/// version catches up, which is the day somebody installed it.
class UpdateCubit extends Cubit<UpdateState> {
  UpdateCubit(
    this._repository, {
    AppPreferences? preferences,
    String running = kAppVersion,
  }) : _preferences = preferences,
       _running = running,
       super(
         UpdateState(
           available: _stillAhead(preferences?.pendingUpdate ?? '', running),
         ),
       ) {
    // Stored, but no longer ahead of us: this machine installed it. Dropped
    // here rather than left to rot, so that the next start does not have to
    // decide the same thing again — and so the file says what is true.
    if ((preferences?.pendingUpdate ?? '').isNotEmpty &&
        state.available == null) {
      unawaited(preferences!.clearPendingUpdate());
    }
  }

  final UpdateRepository _repository;
  final AppPreferences? _preferences;

  /// The version this build is. A parameter and not a direct read of
  /// [kAppVersion] so a test can pin it; the default is the real one.
  final String _running;

  /// Guards against two triggers landing at once — a room that opens while the
  /// start-up check is still in flight is not hypothetical, it is the fastest
  /// path through the app.
  bool _asking = false;

  static String? _stillAhead(String stored, String running) =>
      stored.isNotEmpty && isNewerVersion(stored, running) ? stored : null;

  /// Asks, unless there is nothing to gain by asking.
  ///
  /// Silent about everything: a failure leaves the state alone. Somebody asked,
  /// so a spinner that stops is the answer they get; inventing an error screen
  /// for "GitHub did not reply" would be a screen about somebody else's server.
  ///
  /// `force` skips the shortcut that stops once a newer version is known. It is
  /// what a human pressing the button means: the stored answer is exactly what
  /// they are asking to refresh, so honouring the shortcut would make the button
  /// do nothing at the only moment it is pressed twice.
  Future<void> check({bool force = false}) async {
    // A build with no version of its own has nothing to compare against, and
    // whoever compiled it does not need to be sent to a download page. Checked
    // BEFORE the request and not after: the point is not to make it.
    if (!isComparableVersion(_running)) return;
    if (state.available != null && !force) return;
    if (_asking) return;

    _asking = true;
    try {
      final String? ultima = await _repository.latestVersion();
      if (ultima == null || !isNewerVersion(ultima, _running)) return;
      await _preferences?.setPendingUpdate(ultima);
      // The window can be gone by now: closing it while a request is in flight
      // is one click away, and emitting on a closed cubit throws.
      if (!isClosed) emit(UpdateState(available: ultima));
    } finally {
      _asking = false;
    }
  }
}
