import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/motion_tokens.dart';

/// El indicador de "estamos en eso".
///
/// Un anillo completo apagado con un arco de acento encima que gira, como en
/// el diseño: un `border` de cuatro lados con el de arriba teñido. No es el
/// `CircularProgressIndicator` de Material, que además de girar alarga y
/// acorta el arco — ese vaivén dice "estoy calculando cuánto falta", y acá no
/// falta nada medible.
///
/// Se usa cuando la espera no tiene duración conocida — levantar la red de una
/// sala, presentar el equipo con los demás miembros.
///
/// Durante mucho tiempo fue lo ÚNICO que se veía en esas esperas, porque una
/// barra promete un final medible que nadie puede prometer acá. `AppProgressBar`
/// vino a acompañarlo con una condición que sostiene esa misma regla: se llena
/// con los pasos que el daemon ya terminó y no llega al final hasta que la
/// operación llega. El anillo sigue diciendo lo que la barra no puede decir —
/// que esto está vivo — mientras los pasos tardan.
class AppSpinner extends StatefulWidget {
  const AppSpinner({this.size = 44, this.stroke = 3, super.key});

  final double size;
  final double stroke;

  @override
  State<AppSpinner> createState() => _AppSpinnerState();
}

class _AppSpinnerState extends State<AppSpinner>
    with SingleTickerProviderStateMixin {
  late final AnimationController _controller = AnimationController(
    vsync: this,
    duration: AppMotion.spin,
  )..repeat();

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    return SizedBox(
      width: widget.size,
      height: widget.size,
      child: RotationTransition(
        turns: _controller,
        child: CustomPaint(
          painter: _RingPainter(
            track: colors.border,
            arc: colors.accent,
            stroke: widget.stroke,
          ),
        ),
      ),
    );
  }
}

class _RingPainter extends CustomPainter {
  const _RingPainter({
    required this.track,
    required this.arc,
    required this.stroke,
  });

  final Color track;
  final Color arc;
  final double stroke;

  /// Un cuarto de vuelta, que es lo que ocupa un solo lado del borde en el
  /// diseño. Arranca arriba porque ahí es donde CSS pone el `border-top`.
  static const double _sweep = math.pi / 2;
  static const double _start = -math.pi / 2;

  @override
  void paint(Canvas canvas, Size size) {
    // El rect se mete media pluma hacia dentro: un trazo se pinta centrado en
    // la línea, así que sin esto la mitad de fuera se sale de la caja y el
    // anillo se ve recortado.
    final Rect rect = Offset.zero & size;
    final Rect inner = rect.deflate(stroke / 2);
    final Paint pincel = Paint()
      ..style = PaintingStyle.stroke
      ..strokeWidth = stroke
      ..strokeCap = StrokeCap.butt
      ..color = track;
    canvas.drawArc(inner, 0, math.pi * 2, false, pincel);
    canvas.drawArc(inner, _start, _sweep, false, pincel..color = arc);
  }

  @override
  bool shouldRepaint(_RingPainter old) =>
      old.track != track || old.arc != arc || old.stroke != stroke;
}
