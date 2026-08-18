import 'package:flutter/material.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/motion_tokens.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';

/// A box that is ticked or not.
///
/// Not Material's `Checkbox`, for the same reason [AppSwitch] is not Material's
/// `Switch`: that one brings its own colour scheme, its own ripple and its own
/// sizing, and next to the chips and the segmented control of this app it
/// reads as a control borrowed from another program.
///
/// It is a different control from [AppSwitch] and the difference is what it
/// promises. A switch shows a state of the machine that it also changes, and
/// it can lie the moment the machine changes underneath it — which is why the
/// settings switch is drawn from the measurement. A checkbox is an ANSWER to
/// something that has not happened yet: what it shows is the intention with
/// which the button below it will be pressed, and nothing measures it because
/// there is nothing to measure until then.
///
/// Like the switch, it carries no label: what a tick means is a sentence, and
/// a sentence next to a control is a row.
class AppCheckbox extends StatelessWidget {
  const AppCheckbox({
    required this.value,
    required this.onChanged,
    this.enabled = true,
    super.key,
  });

  final bool value;
  final ValueChanged<bool> onChanged;
  final bool enabled;

  static const double _size = 22;

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
            width: _size,
            height: _size,
            decoration: BoxDecoration(
              color: value ? colors.accent : colors.chip,
              borderRadius: AppRadius.allSm,
              border: Border.all(
                color: value ? colors.accent : colors.border,
                width: 1.5,
              ),
            ),
            child: value
                ? Icon(Icons.check_rounded, size: 15, color: colors.accentInk)
                : null,
          ),
        ),
      ),
    );
  }
}
