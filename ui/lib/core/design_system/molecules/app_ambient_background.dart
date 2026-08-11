import 'package:flutter/material.dart';
import 'package:kanpachi_ui/core/design_system/tokens/color_tokens.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/motion_tokens.dart';

/// Dos juegos de manchas distintos: las del panel y las de la pantalla entera.
///
/// No son el mismo dibujo escalado. El del panel es el de la bienvenida, con
/// cuatro manchas del tamaño de la tarjeta; el de pantalla es el de la espera,
/// con seis repartidas por los cuatro bordes de la ventana y dos de ellas en
/// anillo. Meter las del panel en una ventana entera las deja flotando en el
/// centro sin tocar ningún borde, que es justo lo que el diseño no hace.
enum AmbientLayout { panel, screen }

/// Las manchas que van a la deriva detrás de la ventana.
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
    this.layout = AmbientLayout.panel,
    super.key,
  });

  final bool enabled;

  /// Las manchas de la bienvenida van más subidas que las del lienzo.
  final double intensity;

  /// En la bienvenida el velo es la propia tarjeta a media opacidad, no el
  /// velo del lienzo.
  final bool veilOverSurface;

  /// Qué juego de manchas se dibuja. Ver [AmbientLayout].
  final AmbientLayout layout;

  @override
  State<AppAmbientBackground> createState() => _AppAmbientBackgroundState();
}

class _AppAmbientBackgroundState extends State<AppAmbientBackground>
    with TickerProviderStateMixin {
  /// Un controlador por mancha, y por eso cambiar de disposición los rehace:
  /// los dos juegos no tienen ni la misma cantidad ni los mismos periodos.
  List<AnimationController> _controllers = <AnimationController>[];

  static List<Duration> _periods(AmbientLayout layout) => switch (layout) {
    AmbientLayout.panel => AppMotion.drift,
    AmbientLayout.screen => AppMotion.driftScreen,
  };

  void _rebuildControllers() {
    for (final AnimationController c in _controllers) {
      c.dispose();
    }
    _controllers = <AnimationController>[
      for (final Duration d in _periods(widget.layout))
        AnimationController(vsync: this, duration: d),
    ];
  }

  @override
  void initState() {
    super.initState();
    _rebuildControllers();
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
    if (oldWidget.layout != widget.layout) _rebuildControllers();
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

    return Positioned.fill(
      child: IgnorePointer(
        child: ClipRect(
          child: LayoutBuilder(
            builder: (BuildContext context, BoxConstraints c) =>
                switch (widget.layout) {
                  AmbientLayout.panel => _PanelBlobs(
                    controllers: _controllers,
                    colors: colors,
                    opacity: opacity,
                    width: c.maxWidth,
                    veilOverSurface: widget.veilOverSurface,
                  ),
                  AmbientLayout.screen => _ScreenBlobs(
                    controllers: _controllers,
                    colors: colors,
                    opacity: opacity,
                    size: c.biggest,
                  ),
                },
          ),
        ),
      ),
    );
  }
}

/// La geometría del PANEL de bienvenida, no la del lienzo.
///
/// El diseño tiene dos juegos de manchas: uno detrás de la ventana flotando en
/// la captura, y otro dentro del panel de bienvenida. Estaba puesto el primero
/// — manchas de 460/330/420/280 en un panel que sólo es un 6% mayor que el del
/// diseño, o sea entre un 27% y un 53% más grandes de lo que toca. No se
/// compensa haciendo la ventana más grande. Estas son las del panel:
/// 300/240/280/220.
class _PanelBlobs extends StatelessWidget {
  const _PanelBlobs({
    required this.controllers,
    required this.colors,
    required this.opacity,
    required this.width,
    required this.veilOverSurface,
  });

  final List<AnimationController> controllers;
  final ColorTokens colors;
  final double opacity;
  final double width;
  final bool veilOverSurface;

  @override
  Widget build(BuildContext context) {
    return Stack(
      children: <Widget>[
        _Blob(
          controller: controllers[0],
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
          controller: controllers[1],
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
          controller: controllers[2],
          color: colors.shapeThree,
          opacity: opacity,
          // El diseño las coloca en porcentaje del ancho del panel, así que se
          // resuelven contra el ancho real y no contra el que tenía la maqueta.
          right: width * 0.06,
          bottom: -150,
          size: 280,
          borderRadius: 140,
          travel: const Offset(18, 18),
          scale: 0.07,
        ),
        _Blob(
          controller: controllers[3],
          color: colors.shapeFour,
          opacity: opacity,
          left: width * 0.04,
          bottom: -120,
          size: 220,
          borderRadius: 62,
          travel: const Offset(30, -22),
          spin: -9,
          baseRotation: 18,
        ),
        Positioned.fill(
          child: ColoredBox(
            color: veilOverSurface
                ? colors.surface.withValues(alpha: 0.5)
                : colors.veil,
          ),
        ),
      ],
    );
  }
}

/// Las seis de la pantalla de espera, pegadas a los cuatro bordes.
///
/// # Por qué son seis y no cuatro
///
/// Porque acá no hay tarjeta que mirar: la espera es una pantalla casi vacía y
/// el fondo es lo único que la llena. Con cuatro manchas quedan dos bordes
/// desnudos, y una ventana ancha lo enseña.
///
/// # Y por qué dos son anillos
///
/// Son las dos que caen más cerca del texto. Una mancha maciza detrás de una
/// frase de 26 px se le come el contraste; un anillo deja pasar el fondo por el
/// centro, que es justo por donde pasa la línea de lectura.
///
/// **Sin velo, a diferencia del panel.** El diseño no lo lleva acá: las manchas
/// van más apagadas de partida y el texto cae en el hueco que dejan.
class _ScreenBlobs extends StatelessWidget {
  const _ScreenBlobs({
    required this.controllers,
    required this.colors,
    required this.opacity,
    required this.size,
  });

  final List<AnimationController> controllers;
  final ColorTokens colors;

  /// La opacidad de partida. Cada mancha le aplica encima su propio factor, tal
  /// como el diseño, que las gradúa de .34 a .55 para que no pesen todas igual.
  final double opacity;

  /// El hueco real, para las que el diseño coloca en porcentaje.
  final Size size;

  @override
  Widget build(BuildContext context) {
    return Stack(
      children: <Widget>[
        _Blob(
          controller: controllers[0],
          color: colors.shapeThree,
          opacity: opacity * 0.55,
          left: -130,
          top: -120,
          size: 320,
          borderRadius: 160,
          travel: const Offset(-26, 30),
          spin: 7,
        ),
        _Blob(
          controller: controllers[1],
          color: colors.shapeTwo,
          opacity: opacity * 0.5,
          left: size.width * 0.3,
          top: -150,
          // 230 de hueco más 30 de trazo a cada lado: en CSS el borde crece
          // hacia fuera de la medida declarada y en Flutter hacia dentro, así
          // que el total se escribe acá.
          size: 290,
          borderRadius: 145,
          borderWidth: 30,
          travel: const Offset(22, 26),
          spin: 12,
        ),
        _Blob(
          controller: controllers[2],
          color: colors.shapeTwo,
          opacity: opacity * 0.48,
          right: -90,
          top: 20,
          size: 250,
          borderRadius: 56,
          travel: const Offset(30, -22),
          spin: -9,
        ),
        _Blob(
          controller: controllers[3],
          color: colors.shapeOne,
          opacity: opacity * 0.42,
          left: -80,
          bottom: -140,
          size: 270,
          borderRadius: 64,
          travel: const Offset(-24, -18),
          scale: 0.06,
          spin: -8,
        ),
        _Blob(
          controller: controllers[4],
          color: colors.shapeThree,
          opacity: opacity * 0.5,
          right: size.width * 0.16,
          bottom: -170,
          size: 340,
          borderRadius: 170,
          travel: const Offset(18, 18),
          scale: 0.07,
        ),
        _Blob(
          controller: controllers[5],
          color: colors.shapeOne,
          opacity: opacity * 0.34,
          right: -60,
          bottom: size.height * 0.22,
          size: 194,
          borderRadius: 97,
          borderWidth: 22,
          travel: const Offset(22, 26),
          spin: 12,
        ),
      ],
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
    this.borderWidth,
  });

  final AnimationController controller;
  final Color color;
  final double opacity;
  final double size;
  final double borderRadius;

  /// Grosor del trazo cuando la mancha es un ANILLO en vez de un macizo.
  ///
  /// Null es el caso normal: relleno del color. Con valor, el color se va al
  /// borde y el centro queda vacío — dos de las manchas de pantalla completa
  /// son así porque caen sobre la línea de lectura.
  final double? borderWidth;

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
        // **El alfa va en el COLOR, no en un `Opacity` alrededor.**
        //
        // Es la regla que la documentación de Flutter da con estas palabras:
        // en vez de envolver formas simples en `Opacity`, dibujarlas con un
        // color semitransparente. `Opacity` empuja una capa fuera de pantalla,
        // y acá eso pasaba CUATRO veces por fotograma, porque las manchas se
        // mueven: es el caso que la propia documentación señala como el peor
        // ("particularly in animations").
        //
        // Sale idéntico en pantalla. Cada mancha llevaba su propio `Opacity`,
        // así que nunca hubo opacidad de GRUPO que preservar: el alfa por
        // forma compone igual, y dos manchas que se solapen se ven como se
        // veían.
        child: Container(
          width: size,
          height: size,
          decoration: BoxDecoration(
            color: borderWidth == null
                ? color.withValues(alpha: color.a * opacity)
                : null,
            border: borderWidth == null
                ? null
                : Border.all(
                    color: color.withValues(alpha: color.a * opacity),
                    width: borderWidth!,
                  ),
            borderRadius: BorderRadius.circular(borderRadius),
          ),
        ),
      ),
    );
  }
}
