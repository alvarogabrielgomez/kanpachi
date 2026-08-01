import 'package:flutter/material.dart';
import 'package:kanpachi_ui/core/design_system/tokens/color_tokens.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/motion_tokens.dart';

/// Las cuatro manchas que van a la deriva detrás de la ventana.
///
/// Se puede apagar entero, y no es un capricho: el movimiento continuo molesta
/// a quien es sensible al movimiento, y esto corre en segundo plano durante
/// horas mientras alguien juega. Respeta también `disableAnimations` del
/// sistema, que es la respuesta correcta cuando el usuario ya lo pidió a nivel
/// de sistema operativo y no debería tener que volver a pedirlo aquí.
class AppAmbientBackground extends StatefulWidget {
  const AppAmbientBackground({
    this.enabled = true,
    this.intensity = 1,
    this.veilOverSurface = false,
    super.key,
  });

  final bool enabled;

  /// Las manchas de la bienvenida van más subidas que las del lienzo.
  final double intensity;

  /// En la bienvenida el velo es la propia tarjeta a media opacidad, no el
  /// velo del lienzo.
  final bool veilOverSurface;

  @override
  State<AppAmbientBackground> createState() => _AppAmbientBackgroundState();
}

class _AppAmbientBackgroundState extends State<AppAmbientBackground>
    with TickerProviderStateMixin {
  late final List<AnimationController> _controllers = <AnimationController>[
    for (final Duration d in AppMotion.drift)
      AnimationController(vsync: this, duration: d),
  ];

  @override
  void initState() {
    super.initState();
    _sync();
  }

  void _sync() {
    for (final AnimationController c in _controllers) {
      if (widget.enabled && !c.isAnimating) {
        c.repeat(reverse: true);
      } else if (!widget.enabled && c.isAnimating) {
        c.stop();
      }
    }
  }

  @override
  void didUpdateWidget(AppAmbientBackground oldWidget) {
    super.didUpdateWidget(oldWidget);
    _sync();
  }

  @override
  void dispose() {
    for (final AnimationController c in _controllers) {
      c.dispose();
    }
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final ColorTokens colors = context.colors;
    if (!widget.enabled) return const SizedBox.shrink();

    final double opacity = (colors.shapeOpacity * widget.intensity).clamp(
      0.0,
      1.0,
    );

    // La geometría del PANEL de bienvenida, no la del lienzo.
    //
    // El diseño tiene dos juegos de manchas: uno detrás de la ventana flotando
    // en la captura, y otro dentro del panel de bienvenida. Estaba puesto el
    // primero — manchas de 460/330/420/280 en un panel que sólo es un 6% mayor
    // que el del diseño, o sea entre un 27% y un 53% más grandes de lo que
    // toca. No se compensa haciendo la ventana más grande. Estas son las del
    // panel: 300/240/280/220.
    return Positioned.fill(
      child: IgnorePointer(
        child: ClipRect(
          child: LayoutBuilder(
            builder: (BuildContext context, BoxConstraints c) => Stack(
              children: <Widget>[
                _Blob(
                  controller: _controllers[0],
                  color: colors.shapeOne,
                  opacity: opacity,
                  left: -120,
                  top: -110,
                  size: 300,
                  borderRadius: 150,
                  travel: const Offset(-26, 30),
                  spin: 7,
                ),
                _Blob(
                  controller: _controllers[1],
                  color: colors.shapeTwo,
                  opacity: opacity,
                  right: -90,
                  top: -40,
                  size: 240,
                  borderRadius: 70,
                  travel: const Offset(30, -22),
                  spin: -9,
                ),
                _Blob(
                  controller: _controllers[2],
                  color: colors.shapeThree,
                  opacity: opacity,
                  // El diseño las coloca en porcentaje del ancho del panel, así
                  // que se resuelven contra el ancho real y no contra el que
                  // tenía la maqueta.
                  right: c.maxWidth * 0.06,
                  bottom: -150,
                  size: 280,
                  borderRadius: 140,
                  travel: const Offset(18, 18),
                  scale: 0.07,
                ),
                _Blob(
                  controller: _controllers[3],
                  color: colors.shapeFour,
                  opacity: opacity,
                  left: c.maxWidth * 0.04,
                  bottom: -120,
                  size: 220,
                  borderRadius: 62,
                  travel: const Offset(30, -22),
                  spin: -9,
                  baseRotation: 18,
                ),
                Positioned.fill(
                  child: ColoredBox(
                    color: widget.veilOverSurface
                        ? colors.surface.withValues(alpha: 0.5)
                        : colors.veil,
                  ),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

class _Blob extends StatelessWidget {
  const _Blob({
    required this.controller,
    required this.color,
    required this.opacity,
    required this.size,
    required this.borderRadius,
    required this.travel,
    this.left,
    this.right,
    this.top,
    this.bottom,
    this.spin = 0,
    this.scale = 0,
    this.baseRotation = 0,
  });

  final AnimationController controller;
  final Color color;
  final double opacity;
  final double size;
  final double borderRadius;

  /// Cuánto se desplaza en el punto medio del ciclo.
  final Offset travel;

  final double? left;
  final double? right;
  final double? top;
  final double? bottom;

  /// Grados que gira en el punto medio.
  final double spin;

  /// Cuánto crece en el punto medio.
  final double scale;

  /// Inclinación fija de partida.
  final double baseRotation;

  static const double _degrees = 3.141592653589793 / 180;

  @override
  Widget build(BuildContext context) {
    return Positioned(
      left: left,
      right: right,
      top: top,
      bottom: bottom,
      child: AnimatedBuilder(
        animation: controller,
        builder: (BuildContext context, Widget? blob) {
          final double t = controller.value;
          return Transform.translate(
            offset: travel * t,
            child: Transform.rotate(
              angle: (baseRotation + spin * t) * _degrees,
              child: Transform.scale(scale: 1 + scale * t, child: blob),
            ),
          );
        },
        child: Opacity(
          opacity: opacity,
          child: Container(
            width: size,
            height: size,
            decoration: BoxDecoration(
              color: color,
              borderRadius: BorderRadius.circular(borderRadius),
            ),
          ),
        ),
      ),
    );
  }
}
