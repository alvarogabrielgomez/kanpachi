import 'package:flutter/material.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/motion_tokens.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';

/// A two-position switch: a pill with a knob that slides.
///
/// Not Material's `Switch`. That one brings its own colour scheme, its own
/// ripple and its own sizing, and next to the segmented control and the chips
/// of this app it reads as a control borrowed from another program. The shape
/// here is the same pill as [AppSegmented], which is the house's way of saying
/// "this has two positions".
///
/// It has no label of its own on purpose: what a switch means is a sentence,
/// and a sentence next to a control is a row, not an atom. See [AppSwitchRow].
class AppSwitch extends StatelessWidget {
  const AppSwitch({
    required this.value,
    required this.onChanged,
    this.enabled = true,
    super.key,
  });

  final bool value;
  final ValueChanged<bool> onChanged;

  /// Off while the answer is unknown or being written. A switch that accepts
  /// clicks while the machine has not answered yet invites two writes of the
  /// same setting.
  final bool enabled;

  static const double _width = 38;
  static const double _height = 22;
  static const double _knob = 16;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    return MouseRegion(
      cursor: enabled ? SystemMouseCursors.click : SystemMouseCursors.basic,
      child: GestureDetector(
        onTap: enabled ? () => onChanged(!value) : null,
        child: Opacity(
          opacity: enabled ? 1 : 0.45,
          child: AnimatedContainer(
            duration: AppMotion.hover,
            curve: AppMotion.enter,
            width: _width,
            height: _height,
            padding: const EdgeInsets.all(3),
            alignment: value ? Alignment.centerRight : Alignment.centerLeft,
            decoration: BoxDecoration(
              color: value ? colors.accent : colors.chip,
              borderRadius: AppRadius.pill,
            ),
            child: Container(
              width: _knob,
              height: _knob,
              decoration: BoxDecoration(
                color: value ? colors.accentInk : colors.textMuted,
                shape: BoxShape.circle,
              ),
            ),
          ),
        ),
      ),
    );
  }
}

/// A setting: what it does, what it costs, and the switch that changes it.
///
/// The explanation is required and not optional, and that is the point of the
/// row existing. Every setting in this app changes something with a
/// consequence — traffic to the daemon, whether Windows starts a service —
/// and a bare label leaves the reader to guess which one.
class AppSwitchRow extends StatelessWidget {
  const AppSwitchRow({
    required this.title,
    required this.note,
    required this.value,
    required this.onChanged,
    this.enabled = true,
    super.key,
  });

  final String title;
  final String note;
  final bool value;
  final ValueChanged<bool> onChanged;
  final bool enabled;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: <Widget>[
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: <Widget>[
              Text(
                title,
                style: context.type.strong.copyWith(color: colors.text),
              ),
              const SizedBox(height: AppSpacing.xs),
              Text(
                note,
                style: context.type.bodySm.copyWith(color: colors.textMuted),
              ),
            ],
          ),
        ),
        const SizedBox(width: AppSpacing.x7l),
        Padding(
          // Lines up with the title and not with the middle of the block: the
          // note can be one line or four, and a switch that drifts down the
          // longer the text gets stops looking attached to it.
          padding: const EdgeInsets.only(top: 2),
          child: AppSwitch(
            value: value,
            onChanged: onChanged,
            enabled: enabled,
          ),
        ),
      ],
    );
  }
}
