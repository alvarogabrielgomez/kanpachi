import 'package:flutter/widgets.dart';

import '../../l10n/app_localizations.dart';

/// Resolves [AppLocalizations] without a BuildContext, for engines that run
/// from lifecycle/background callbacks (notification engine, timer-notif
/// service, bell scheduler, session coordinator) rather than the widget tree.
/// The app has no user-selected locale override, so the device locale is the
/// single source of truth. Replaces two byte-identical private resolvers that
/// had already been copy-pasted across engines.
///
/// The resolved instance is cached process-wide: the timer/notification
/// engines call this per state-tick, and the session coordinator needs a warm
/// instance for its SYNCHRONOUS foreground-title fallback (it can't await).
/// Device-locale changes are rare and picked up on the next cold start;
/// [invalidate] exists for tests.
abstract class BackgroundL10n {
  static AppLocalizations? _cached;

  /// The already-resolved instance, or null before the first [resolve]. Lets
  /// synchronous callers use a localized string when one is warm without an
  /// async hop, falling back to a literal otherwise.
  static AppLocalizations? get cached => _cached;

  /// Resolves (and caches) the localizations for the current device locale.
  static Future<AppLocalizations> resolve() async {
    final existing = _cached;
    if (existing != null) return existing;
    final candidate = WidgetsBinding.instance.platformDispatcher.locale;
    final supported = AppLocalizations.supportedLocales;
    final match = supported.firstWhere(
      (s) => s.languageCode == candidate.languageCode,
      orElse: () => supported.isNotEmpty ? supported.first : const Locale('en'),
    );
    final loaded = await AppLocalizations.delegate.load(match);
    _cached = loaded;
    return loaded;
  }

  /// Drops the cached instance (tests / a future locale-change hook).
  @visibleForTesting
  static void invalidate() => _cached = null;
}
