import 'dart:io';

/// The names and paths the daemon publishes, mirrored on this side.
///
/// They live in one file because they are a contract with `daemon/transport/
/// pipe/pipe.go`, not configuration. A typo here does not fail at build time;
/// it fails as "the daemon is not running" against a daemon that is running.
abstract final class PipeNames {
  /// The production name. The daemon does NOT use it yet: `main.go` falls back
  /// to [console] unconditionally because service mode is written and wired to
  /// nothing. Kept here so the day service mode gets wired the client already
  /// knows the name.
  static const String production =
      r'\\.\pipe\ProtectedPrefix\Administrators\kanpachi';

  /// The `--console` name, which is what a development daemon listens on.
  ///
  /// Console mode uses a different name on purpose: with the same one, an
  /// unprivileged process would squat the production name by launching our own
  /// binary with `--console`.
  static const String console =
      r'\\.\pipe\ProtectedPrefix\Administrators\kanpachi-console';

  /// The file holding the local API token.
  static const String tokenFile = 'api.token';

  /// Where the daemon keeps its data, and therefore the token.
  static String get dataDir {
    final String programData =
        Platform.environment['ProgramData'] ?? r'C:\ProgramData';
    return '$programData\\Kanpachi';
  }

  static String get tokenPath => '$dataDir\\$tokenFile';
}

/// Reads the token the daemon left on disk.
///
/// Read on EVERY connection and never cached across them. The daemon rotates it
/// once per process lifetime and deletes the file on exit, so a remembered
/// token is always the wrong one after a restart, and the symptom would be an
/// `unauthorized` nobody can explain.
///
/// Returns null when the file is not there, which is not an error: it is what
/// "the daemon is not running" looks like from here.
Future<String?> readApiToken({String? path}) async {
  final File f = File(path ?? PipeNames.tokenPath);
  if (!await f.exists()) return null;
  final String raw = (await f.readAsString()).trim();
  return raw.isEmpty ? null : raw;
}
