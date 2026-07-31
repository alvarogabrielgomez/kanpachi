/// Numbering convention (informal, only meaningful to source readers):
///   1xxx → auth / credentials / tokens / invites / password-reset
///   2xxx → permission
///   3xxx → validation
///   4xxx → sync (schema mismatch, some other sync error, etc)
///   5xxx → generic / fallthrough
abstract final class AppErrorCode {
  /// Peer cannot enter to the room because code is invalid or expired.
    static const int loginFailed = 1001;

  /// Peer cannot connect because seed server is not reachable or is not responding.
  static const int seedServerUnreachable = 1002;
}
