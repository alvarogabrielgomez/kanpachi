/// Who took a step. Mirrors `domain.ProgressScope` in Go.
enum ProgressScope {
  daemon('daemon'),
  seed('seed'),
  engine('engine'),
  network('red'),
  firewall('firewall'),

  /// A scope this build does not know.
  ///
  /// Exists so a newer daemon cannot make the panel drop steps in silence: an
  /// unknown scope loses its colour, never its line.
  unknown('');

  const ProgressScope(this.wire);

  final String wire;

  static ProgressScope fromWire(String? s) {
    for (final ProgressScope v in values) {
      if (v.wire == s) return v;
    }
    return unknown;
  }
}

/// One step that already happened.
class ProgressStep {
  const ProgressStep({
    required this.scope,
    required this.text,
    required this.since,
  });

  factory ProgressStep.fromJson(Map<String, Object?> json) => ProgressStep(
    scope: ProgressScope.fromWire(json['scope'] as String?),
    text: json['text'] as String? ?? '',
    since: Duration(milliseconds: (json['since_ms'] as num?)?.toInt() ?? 0),
  );

  final ProgressScope scope;
  final String text;

  /// How long the operation had been running when this happened.
  ///
  /// Comes computed from the daemon and is NOT derived here: between the two
  /// there is a pipe, not a shared clock, and subtracting two different clocks
  /// gives numbers that lie.
  final Duration since;
}

/// The long operation in flight, or the last one.
///
/// # What it is for
///
/// Creating a room takes tens of seconds with everything working. From the
/// outside, that wait and a hang look identical, and when it fails the only
/// thing that reaches the screen is the last error line, which is almost never
/// where the problem was.
///
/// # It is PULLED, never pushed
///
/// The local API is pure request and response, no server push. The daemon
/// piles the steps on its side and the screen asks for them. And it asks over
/// a SECOND connection: one connection's loop is sequential, so asking down
/// the same one would queue behind exactly what it wants to watch.
class Progress {
  const Progress({
    this.op = '',
    this.running = false,
    this.failure = '',
    this.elapsed = Duration.zero,
    this.steps = const <ProgressStep>[],
    this.dropped = 0,
  });

  factory Progress.fromJson(Map<String, Object?> json) => Progress(
    op: json['op'] as String? ?? '',
    running: json['running'] as bool? ?? false,
    failure: json['failure'] as String? ?? '',
    elapsed: Duration(milliseconds: (json['elapsed_ms'] as num?)?.toInt() ?? 0),
    steps: <ProgressStep>[
      for (final Object? s in (json['steps'] as List<Object?>?) ?? <Object?>[])
        if (s is Map<String, Object?>) ProgressStep.fromJson(s),
    ],
    dropped: (json['dropped'] as num?)?.toInt() ?? 0,
  );

  /// What is being done, in Spanish. Empty means nothing has happened yet.
  final String op;
  final bool running;

  /// The error it ended with. Empty if it went fine or is still alive.
  final String failure;
  final Duration elapsed;
  final List<ProgressStep> steps;

  /// Middle steps thrown away by the daemon's cap.
  ///
  /// Said out loud on purpose: a list cut in silence reads as a complete list,
  /// and then the hole where the problem was looks like it never happened.
  final int dropped;

  bool get isEmpty => op.isEmpty;
  bool get failed => failure.isNotEmpty;
}
