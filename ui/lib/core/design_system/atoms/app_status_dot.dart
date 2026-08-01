import 'package:flutter/material.dart';
import 'package:kanpachi_ui/core/design_system/tokens/motion_tokens.dart';

/// El punto de estado.
///
/// Aparece junto al servicio activo, junto a cada miembro de la sala y en los
/// avisos. El color lo elige quien lo usa porque el significado es del
/// contexto: el verde no quiere decir lo mismo junto a un peer que junto al
/// daemon.
class AppStatusDot extends StatefulWidget {
  const AppStatusDot({
    required this.color,
    this.size = 7,
    this.pulse = false,
    this.pulseDuration = AppMotion.pulseSlow,
    this.square = false,
    super.key,
  });

  final Color color;
  final double size;

  /// El latido dice "esto está vivo AHORA", no "esto está bien". Se reserva
  /// para lo que se está midiendo en tiempo real, porque si late todo, el
  /// movimiento deja de significar nada.
  final bool pulse;

  final Duration pulseDuration;

  /// Un cuadrado en vez de un círculo, para lo que ya dejó de latir: el aviso
  /// de que el host se fue de la sala.
  final bool square;

  @override
  State<AppStatusDot> createState() => _AppStatusDotState();
}

class _AppStatusDotState extends State<AppStatusDot>
    with SingleTickerProviderStateMixin {
  late final AnimationController _controller = AnimationController(
    vsync: this,
    duration: widget.pulseDuration,
  );

  @override
  void initState() {
    super.initState();
    if (widget.pulse) _controller.repeat(reverse: true);
  }

  @override
  void didUpdateWidget(AppStatusDot oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (widget.pulse && !_controller.isAnimating) {
      _controller.repeat(reverse: true);
    } else if (!widget.pulse && _controller.isAnimating) {
      _controller.stop();
      _controller.value = 0;
    }
  }

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final Widget dot = _Dot(
      color: widget.color,
      size: widget.size,
      square: widget.square,
    );
    if (!widget.pulse) return dot;
    return FadeTransition(
      opacity: Tween<double>(begin: 1, end: 0.3).animate(_controller),
      child: dot,
    );
  }
}

class _Dot extends StatelessWidget {
  const _Dot({required this.color, required this.size, required this.square});

  final Color color;
  final double size;
  final bool square;

  @override
  Widget build(BuildContext context) {
    return Container(
      width: size,
      height: size,
      decoration: BoxDecoration(
        color: color,
        shape: square ? BoxShape.rectangle : BoxShape.circle,
        borderRadius: square ? BorderRadius.circular(2) : null,
      ),
    );
  }
}
