import 'dart:io';

/// The names and paths the daemon publishes, mirrored on this side.
///
/// They live in one file because they are a contract with `daemon/transport/
/// pipe/pipe.go`, not configuration. A typo here does not fail at build time;
/// it fails as "the daemon is not running" against a daemon that is running.
abstract final class PipeNames {
  /// The production name, which is what the service listens on.
  static const String production =
      r'\\.\pipe\ProtectedPrefix\Administrators\kanpachi';

  /// The `--console` name, which is what a development daemon listens on.
  ///
  /// Console mode uses a different name on purpose: with the same one, an
  /// unprivileged process would squat the production name by launching our own
  /// binary with `--console`.
  static const String console =
      r'\\.\pipe\ProtectedPrefix\Administrators\kanpachi-console';

  /// Talk to a `--console` daemon instead of the service.
  ///
  /// Compile time, and NOT read at runtime, same as the automatic cutoffs: a
  /// published binary has this branch pruned, so no file on the user's disk can
  /// point the app at a pipe any process is allowed to create.
  ///
  ///     flutter run -d windows --dart-define=KANPACHI_CONSOLE_PIPE=true
  static const bool _console = bool.fromEnvironment('KANPACHI_CONSOLE_PIPE');

  /// Which name this build talks to.
  ///
  /// **It used to default to [console], and that was a real bug rather than a
  /// stale comment.** The default was written while service mode was code
  /// nobody called, so it named the only pipe a daemon answered on back then.
  /// The day the service got wired, the app went on knocking at a door with
  /// nobody behind it: the daemon was running, the tray was up, and every
  /// screen said there was no service. Found by running it, not by reading it.
  static const String defaultName = _console ? console : production;

  /// The file holding the local API token.
  static const String tokenFile = 'api.token';

  /// The file that makes a folder a portable Kanpachi.
  ///
  /// A CONTRACT with `daemon/cmd/kanpachid/portable.go`, which owns the idea
  /// and the name. Both sides derive the data directory from the same file on
  /// disk rather than telling each other, and that is the whole point: this
  /// executable and the daemon are launched separately, so anything passed
  /// between them can go missing in one direction and not the other. The
  /// symptom of that would be the daemon writing its token beside its binary
  /// while this side reads ProgramData — "there is no service" with the
  /// service running, which is a bug this repository has already had four
  /// times over the pipe name alone.
  static const String portableMarker = 'kanpachi.portable';

  /// The data directory of a portable folder, relative to the executable.
  ///
  /// **Not `data`, and that is not a style choice.** Flutter's Windows bundle
  /// ships its own `data\` with `icudtl.dat`, `app.so` and `flutter_assets\`,
  /// and in a portable folder both executables live in the same directory. The
  /// first build measured the collision: the daemon would have written its
  /// token and its identity key in among the interface's own resources. Same
  /// name as `PortableDataDir` in `daemon/cmd/kanpachid/portable.go`.
  static const String portableData = 'kanpachi-data';

  /// Where the daemon keeps its data, and therefore the token.
  ///
  /// Portable first, because a portable folder can be copied onto a machine
  /// that ALSO has Kanpachi installed, and then both are true. The one that
  /// wins has to be the one this executable was launched from.
  static String get dataDir {
    final String junto = File(Platform.resolvedExecutable).parent.path;
    if (File('$junto\\$portableMarker').existsSync()) {
      return '$junto\\$portableData';
    }
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
