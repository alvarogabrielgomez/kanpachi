import 'package:flutter/material.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';
import 'package:kanpachi_ui/features/session/domain/entities/progress.dart';

/// What the daemon is doing, step by step. **Debug builds only.**
///
/// # Why it exists
///
/// Opening a room takes tens of seconds with everything working: the routing
/// table is read, the registry hands out a code, the engine comes up, two
/// adapters have to take an address, the MTU gets probed, the gate is scoped
/// and the channel opens. From outside, that wait and a hang look identical,
/// and when it fails the only thing that reaches the screen is the last error
/// line, which is almost never where the problem was.
///
/// # Why only in debug
///
/// Not secrecy. These lines name subnets, adapters, seeds and timings: they
/// are how somebody building Kanpachi finds which of eight steps is slow, and
/// they are noise to somebody who wants to play. Whoever is playing gets one
/// spinner and, if it fails, one sentence that says what did not happen.
///
/// Nothing polls for this in a release build, so `Progress` is never even
/// fetched there.
class ProgressSteps extends StatelessWidget {
  const ProgressSteps({required this.progress, super.key});

  final Progress progress;

  @override
  Widget build(BuildContext context) {
    if (progress.isEmpty) return const SizedBox.shrink();
    final colors = context.colors;

    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: <Widget>[
        Row(
          children: <Widget>[
            Expanded(
              child: Text(
                progress.op,
                style: context.type.strong.copyWith(color: colors.text),
              ),
            ),
            Text(
              _tiempo(progress.elapsed),
              style: context.type.monoXs.copyWith(color: colors.textMuted),
            ),
          ],
        ),
        const SizedBox(height: AppSpacing.md),
        for (final ProgressStep s in progress.steps) _Linea(step: s),
        if (progress.dropped > 0) ...<Widget>[
          const SizedBox(height: AppSpacing.sm),
          // Said out loud on purpose. A list cut in silence reads as a
          // complete list, and then the hole where the problem was looks like
          // it never happened.
          Text(
            'se omitieron ${progress.dropped} pasos intermedios',
            style: context.type.monoXs.copyWith(color: colors.textMuted),
          ),
        ],
      ],
    );
  }
}

/// One step: elapsed time, who did it, what happened.
///
/// A class and not a `Widget _line()` helper, by the house rule: with a method
/// the parent's rebuild redoes every line, and this panel rebuilds every 400 ms
/// while a room is opening.
class _Linea extends StatelessWidget {
  const _Linea({required this.step});

  final ProgressStep step;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    return Padding(
      padding: const EdgeInsets.only(bottom: 3),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          SizedBox(
            width: 52,
            child: Text(
              _tiempo(step.since),
              textAlign: TextAlign.right,
              style: context.type.monoXs.copyWith(color: colors.textMuted),
            ),
          ),
          const SizedBox(width: AppSpacing.lg),
          SizedBox(
            width: 62,
            child: Text(
              _etiqueta(step.scope),
              style: context.type.monoXs.copyWith(
                color: _color(context, step.scope),
              ),
            ),
          ),
          const SizedBox(width: AppSpacing.lg),
          Expanded(
            child: Text(
              step.text,
              style: context.type.monoXs.copyWith(color: colors.textOnChip),
            ),
          ),
        ],
      ),
    );
  }

  static String _etiqueta(ProgressScope s) => switch (s) {
    ProgressScope.daemon => 'daemon',
    ProgressScope.seed => 'seed',
    ProgressScope.engine => 'engine',
    ProgressScope.network => 'red',
    ProgressScope.firewall => 'firewall',
    // A scope this build does not know loses its colour, never its line.
    ProgressScope.unknown => '?',
  };

  static Color _color(BuildContext context, ProgressScope s) {
    final colors = context.colors;
    return switch (s) {
      ProgressScope.seed => colors.accent,
      ProgressScope.engine => colors.ok,
      ProgressScope.firewall => colors.warn,
      _ => colors.textMuted,
    };
  }
}

/// Seconds with one decimal below a minute, `m:ss` above.
///
/// Milliseconds are noise here: what the reader is looking for is which step
/// ate twelve seconds, not whether one took 4 ms or 7 ms.
String _tiempo(Duration d) {
  if (d.inMinutes >= 1) {
    return '${d.inMinutes}:${(d.inSeconds % 60).toString().padLeft(2, '0')}';
  }
  return '${(d.inMilliseconds / 1000).toStringAsFixed(1)}s';
}
