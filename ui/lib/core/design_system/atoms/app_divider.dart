import 'package:flutter/material.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_kicker.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';

/// La línea con una palabra en medio: "O".
///
/// Separa dos caminos que son alternativos de verdad — unirse a una sala o
/// crear una — y no dos pasos de lo mismo. Una línea sin palabra dejaría eso
/// a interpretación.
class AppLabeledDivider extends StatelessWidget {
  const AppLabeledDivider(this.label, {super.key});

  final String label;

  @override
  Widget build(BuildContext context) {
    return Row(
      children: <Widget>[
        const Expanded(child: _Line()),
        Padding(
          padding: const EdgeInsets.symmetric(horizontal: AppSpacing.xxl),
          child: AppKicker(label),
        ),
        const Expanded(child: _Line()),
      ],
    );
  }
}

class _Line extends StatelessWidget {
  const _Line();

  @override
  Widget build(BuildContext context) {
    return Container(height: 1, color: context.colors.border);
  }
}
