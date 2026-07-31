// lib/core/design_system/atoms/app_card.dart

import 'package:flutter/material.dart';

import '../tokens/context_ext.dart';
import '../tokens/spacing_tokens.dart';

/// Escalón de la rampa de superficies.
enum SurfaceLevel { surface, card, raised }

/// Peso del borde. La profundidad se hace con BORDES, no con sombras.
enum SurfaceEdge { none, low, standard, strong, accent }

/// El panel del design system.
///
/// La apariencia se dicta con [level] y [edge], **nunca pasando colores
/// sueltos**: así el panel no puede apartarse de la rampa.
///
/// El `Card` de Material no se usa: trae elevation, sombra y el radius de M3,
/// y se ve de otro sistema. Lo prohíbe `ds_purity_test.dart`.
class AppCard extends StatelessWidget {
  const AppCard({
    super.key,
    required this.child,
    this.level = SurfaceLevel.card,
    this.edge = SurfaceEdge.standard,
    this.padding = const EdgeInsets.all(Spacing.md),
    this.onTap,
  });

  final Widget child;
  final SurfaceLevel level;
  final SurfaceEdge edge;
  final EdgeInsets padding;
  final VoidCallback? onTap;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;

    final background = switch (level) {
      SurfaceLevel.surface => colors.surface,
      SurfaceLevel.card => colors.card,
      SurfaceLevel.raised => colors.cardRaised,
    };

    final borderColor = switch (edge) {
      SurfaceEdge.none => null,
      SurfaceEdge.low => colors.borderLow,
      SurfaceEdge.standard => colors.border,
      SurfaceEdge.strong => colors.borderStrong,
      SurfaceEdge.accent => colors.primary,
    };

    final decorated = Container(
      padding: padding,
      decoration: BoxDecoration(
        color: background,
        borderRadius: const BorderRadius.all(
          Radius.circular(Spacing.radiusCard),
        ),
        border: borderColor == null
            ? null
            : Border.all(color: borderColor, width: Spacing.borderHairline),
      ),
      child: child,
    );

    if (onTap == null) return decorated;

    // Material + InkWell: un card tocable conserva ripple y semántica.
    return Material(
      color: Colors.transparent,
      child: InkWell(onTap: onTap, child: decorated),
    );
  }
}
