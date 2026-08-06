import 'package:flutter/material.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';
import 'package:kanpachi_ui/features/session/domain/entities/progress.dart';

/// What the daemon is doing, step by step.
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
/// # Who sees it
///
/// Whoever turned it on. It is on by default while developing and off by
/// default in the product, and either way it is a setting and not a build
/// flag — see [AppPreferences.verbose]. The reason it is off by default is not
/// secrecy: these lines name subnets, adapters, seeds and timings, which is
/// what somebody building Kanpachi needs and noise to somebody who wants to
/// play.
///
/// # Why the list scrolls inside itself
///
/// Because the number of steps is not known in advance and the panel shares a
/// screen with a spinner, a title and a note that must stay put. Measured: a
/// creation that reached the engine overflowed the wait screen by 116 px, and
/// what a bottom overflow hides is the END of the list, which is exactly the
/// part being waited on. The header stays outside the scroll so the operation
/// and its clock never leave.
class ProgressSteps extends StatelessWidget {
  const ProgressSteps({
    required this.progress,
    this.maxHeight = 240,
    super.key,
  });

  final Progress progress;

  /// How tall the list of steps gets before it starts scrolling.
  ///
  /// A cap and not a share of the window: this panel sits in two very
  /// different places — under the wait screen and inside the failure notice's
  /// details drawer — and in both the rest of the content has to remain
  /// reachable.
  final double maxHeight;

  @override
  Widget build(BuildContext context) {
    if (progress.isEmpty) return const SizedBox.shrink();
    final colors = context.colors;

    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      mainAxisSize: MainAxisSize.min,
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
        // Not wrapped in a `Flexible`, and that is load-bearing: the failure
        // notice hangs from a `Positioned` with no top, so its height arrives
        // UNBOUNDED, and a flex child under an unbounded main axis throws. The
        // cap lives inside the list itself, which works in both places.
        _StepList(progress: progress, maxHeight: maxHeight),
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

/// The scrolling part: the steps themselves, newest at the bottom.
///
/// # Why it follows the tail, and why only sometimes
///
/// Because this is a live log of something the reader is waiting on: pinned to
/// the top, a creation past the eighth step shows only the part that already
/// finished. So it jumps to the end when a step lands — **unless the reader
/// scrolled up**, because scrolling up is somebody reading an earlier step, and
/// yanking them back to the bottom every 400 ms would make that impossible.
class _StepList extends StatefulWidget {
  const _StepList({required this.progress, required this.maxHeight});

  final Progress progress;
  final double maxHeight;

  @override
  State<_StepList> createState() => _StepListState();
}

class _StepListState extends State<_StepList> {
  final ScrollController _scroll = ScrollController();

  /// How far off the bottom still counts as "at the bottom". One line of the
  /// mono type plus its gap: a couple of pixels of overscroll must not read as
  /// a deliberate scroll up.
  static const double _tailSlack = 24;

  @override
  void didUpdateWidget(_StepList old) {
    super.didUpdateWidget(old);
    if (widget.progress.steps.length == old.progress.steps.length) return;
    // Asked BEFORE the new step is laid out, which is why it is read here and
    // not in the callback: at this point the controller still holds the
    // metrics of the list the reader was looking at.
    if (!_pegadoAlFinal()) return;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!_scroll.hasClients) return;
      _scroll.jumpTo(_scroll.position.maxScrollExtent);
    });
  }

  bool _pegadoAlFinal() {
    if (!_scroll.hasClients) return true;
    final ScrollPosition p = _scroll.position;
    return p.pixels >= p.maxScrollExtent - _tailSlack;
  }

  @override
  void dispose() {
    _scroll.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return ConstrainedBox(
      constraints: BoxConstraints(maxHeight: widget.maxHeight),
      child: SingleChildScrollView(
        controller: _scroll,
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          mainAxisSize: MainAxisSize.min,
          children: <Widget>[
            for (final ProgressStep s in widget.progress.steps) _Linea(step: s),
          ],
        ),
      ),
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
