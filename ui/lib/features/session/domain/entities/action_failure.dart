import 'package:kanpachi_ui/core/messages/message_keys.dart';
import 'package:kanpachi_ui/features/session/domain/entities/progress.dart';

/// One action the user asked for, that did not happen.
///
/// # Why it is state and not an exception that escapes
///
/// Because a screen cannot catch an exception. Before this, a failing action
/// either threw into nowhere or was swallowed, and both look the same on
/// screen: the spinner stops and nothing else changes. The user cannot tell
/// "it failed" from "it worked and the screen is stale", so they press again.
///
/// Living in the state means the screen paints it, the user reads it, and it
/// clears when they retry or dismiss.
class ActionFailure {
  const ActionFailure({
    required this.action,
    required this.reason,
    this.code,
    this.progress,
  });

  final FailedAction action;

  /// Raw text from the daemon, or from the transport.
  ///
  /// **Never painted as the main message.** The sentence the user reads comes
  /// from the message catalog, keyed by [action] and [code]. This is the
  /// detail: it is written to diagnose, in daemon words, and often names
  /// things no user has heard of.
  final String reason;

  /// The closed error code, when the daemon answered with one.
  ///
  /// Null means the daemon never answered: the pipe was down, the write
  /// failed, the link died. That is a different sentence and a different fix.
  final String? code;

  /// Steps of the long operation that failed, when there were any.
  ///
  /// This is what "ver detalles" shows. It is kept in DEBUG builds only, and
  /// the reason is not secrecy: it names subnets, adapters and seeds, which
  /// help whoever is building Kanpachi and mean nothing to whoever is playing.
  final Progress? progress;

  /// Whether there is anything worth expanding.
  bool get hasDetails =>
      reason.isNotEmpty || (progress?.steps.isNotEmpty ?? false);

  ActionFailure withProgress(Progress? p) =>
      ActionFailure(action: action, reason: reason, code: code, progress: p);
}
