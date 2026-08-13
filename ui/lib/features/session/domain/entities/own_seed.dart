import 'package:flutter/foundation.dart';

/// The registry this machine opens rooms on, and the one it could suggest.
///
/// # Why two fields and not one
///
/// Because they are put there by different people, and collapsing them is how
/// a stranger's server quietly becomes yours. [configured] is what somebody
/// typed on this machine, and it is the only one that decides anything.
/// [suggested] is the registry of the last room this machine ENTERED, which
/// came in a code somebody else wrote.
///
/// Entering a room does not adopt it. Adopting would mean the next room you
/// open is hosted on a stranger's server by default, with the confirmation
/// dialog already leaning that way before you read it. So the suggestion only
/// prefills the field on the settings screen, marked as what it is.
@immutable
class OwnSeed {
  const OwnSeed({this.configured = '', this.suggested = ''});

  /// Empty until somebody sets one. That is the normal state of an install
  /// that has never hosted, and it is not a failure.
  final String configured;

  /// The registry of the last room entered, or empty. Never used to connect.
  final String suggested;

  /// Whether this machine can open a room at all.
  bool get canHost => configured.isNotEmpty;

  @override
  bool operator ==(Object other) =>
      other is OwnSeed &&
      other.configured == configured &&
      other.suggested == suggested;

  @override
  int get hashCode => Object.hash(configured, suggested);
}
