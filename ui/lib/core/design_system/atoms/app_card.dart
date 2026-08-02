import 'dart:ui' show PathMetric;

import 'package:flutter/material.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';

/// La caja con borde que agrupa un bloque.
///
/// El diseño usa dos variantes y la diferencia es semántica, no estética: el
/// borde continuo enmarca algo que EXISTE — el juego activo, la lista de
/// miembros — y el discontinuo algo que todavía no, como una sala sin juego o
/// el hueco de una portada que no se ha descargado.
class AppCard extends StatelessWidget {
  const AppCard({
    required this.child,
    this.padding,
    this.dashed = false,
    this.filled = false,
    this.radius = AppRadius.allXl,
    this.clip = false,
    super.key,
  });

  final Widget child;
  final EdgeInsets? padding;

  /// Borde discontinuo: lo que está por decidirse.
  final bool dashed;

  /// Relleno hundido, para un bloque dentro de otro bloque.
  final bool filled;

  final BorderRadius radius;

  /// Recorta el contenido al radio. Lo necesitan las listas de filas, cuyos
  /// separadores llegan hasta el borde.
  final bool clip;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final Widget body = Container(
      padding: padding,
      decoration: BoxDecoration(
        color: filled ? colors.surfaceSunken : null,
        borderRadius: radius,
        border: dashed
            ? null
            : Border.all(color: colors.border, width: AppStroke.hairline),
      ),
      child: child,
    );
    if (dashed) {
      return CustomPaint(
        painter: DashedBorderPainter(color: colors.border, radius: radius),
        child: body,
      );
    }
    return clip ? ClipRRect(borderRadius: radius, child: body) : body;
  }
}

/// Flutter no trae bordes discontinuos, así que hay que dibujarlos.
///
/// Público porque no lo usa sólo la tarjeta: el botón de añadir un rango de
/// puertos también va con trazo discontinuo, y es la misma línea — duplicarla
/// garantizaría que un día una de las dos tenga los guiones de otro tamaño.
class DashedBorderPainter extends CustomPainter {
  const DashedBorderPainter({required this.color, required this.radius});

  final Color color;
  final BorderRadius radius;

  static const double _dash = 5;
  static const double _gap = 4;

  @override
  void paint(Canvas canvas, Size size) {
    final Paint paint = Paint()
      ..color = color
      ..style = PaintingStyle.stroke
      ..strokeWidth = AppStroke.hairline;

    final Path path = Path()..addRRect(radius.toRRect(Offset.zero & size));

    for (final PathMetric metric in path.computeMetrics()) {
      double distance = 0;
      while (distance < metric.length) {
        final double next = distance + _dash;
        canvas.drawPath(
          metric.extractPath(distance, next.clamp(0, metric.length)),
          paint,
        );
        distance = next + _gap;
      }
    }
  }

  @override
  bool shouldRepaint(DashedBorderPainter oldDelegate) =>
      oldDelegate.color != color || oldDelegate.radius != radius;
}
