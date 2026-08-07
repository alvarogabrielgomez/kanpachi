/// Who knows which version is the latest one published.
///
/// One method, and a nullable answer that means "not right now". There is no
/// error type on purpose: nothing in this app changes behaviour depending on
/// WHY the version could not be read. No network, seed down, GitHub slow, a
/// body that is not JSON — all of them end the same way, with the window saying
/// nothing, and inventing three ways to say nothing would be three code paths
/// that all lead to the same silence.
abstract interface class UpdateRepository {
  /// The latest published version, as its tag, or null if it cannot be known
  /// right now. Never throws.
  Future<String?> latestVersion();
}
