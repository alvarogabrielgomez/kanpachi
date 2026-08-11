import 'package:flutter/material.dart';
import 'package:kanpachi_ui/core/design_system/tokens/color_tokens.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/motion_tokens.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';

/// La barra de la pantalla de carga: un hilo que se llena con lo que YA pasó.
///
/// # Qué promete, exactamente
///
/// Que la operación avanza. Nada más. Cada salto es un paso que el daemon
/// terminó de verdad — nunca un reloj corriendo solo — y por eso la barra no
/// llega al final por su cuenta: se topa antes y el último tramo lo paga el fin
/// real de la operación. Ver `maxLoadingFraction`.
///
/// Es lo contrario de lo que decía `AppSpinner`, que existe justo para las
/// esperas sin final medible, y las dos conviven en la misma pantalla sin
/// contradecirse: el anillo dice "seguimos vivos" y la barra dice "vamos por
/// acá". Lo que no se puede prometer es CUÁNTO FALTA, y ninguna de las dos lo
/// hace.
///
/// # El brillo
///
/// Un reflejo que recorre el relleno cada segundo y medio. No es adorno de más:
/// entre paso y paso pueden pasar diez segundos, y sin él una barra quieta al
/// 30% es indistinguible de una barra colgada al 30%.
class AppProgressBar extends StatelessWidget {
  const AppProgressBar({
    required this.value,
    this.width = 300,
    this.thickness = 4,
    super.key,
  });

  /// Cuánto lleva hecho, de 0 a 1. Se recorta al rango; un valor fuera de él es
  /// un error de quien lo calcula, no algo que esta barra deba dibujar.
  final double value;

  /// El ancho al que aspira. Se encoge si la ventana no da para tanto.
  final double width;

  final double thickness;

  @override
  Widget build(BuildContext context) {
    final ColorTokens colors = context.colors;
    return SizedBox(
      width: width,
      height: thickness,
      child: DecoratedBox(
        decoration: BoxDecoration(
          color: colors.chip,
          borderRadius: AppRadius.pill,
        ),
        // La animación va en el ancho y no en un `LinearProgressIndicator`
        // porque el valor llega a saltos: tres pasos que aterrizan juntos son
        // un solo tramo largo, y la curva es la que lo convierte en un avance
        // en vez de un tirón.
        child: TweenAnimationBuilder<double>(
          tween: Tween<double>(begin: 0, end: value.clamp(0, 1)),
          duration: AppMotion.barFill,
          curve: AppMotion.fill,
          builder: (BuildContext context, double t, Widget? child) {
            return Align(
              alignment: Alignment.centerLeft,
              // Un `widthFactor` de 0 deja la caja sin ancho, que es
              // exactamente lo que hay que dibujar mientras no ha pasado nada.
              //
              // **El `heightFactor: 1` no es decorativo**, y costó una foto
              // descubrirlo: sin él el eje sin factor queda con constraints
              // FLOJAS, el relleno se encoge a lo que mida su hijo —un
              // `DecoratedBox` no mide nada— y la barra se pintaba de alto
              // cero. Se veía la pista vacía y nada más.
              child: FractionallySizedBox(
                widthFactor: t,
                heightFactor: 1,
                child: ClipRRect(
                  borderRadius: AppRadius.pill,
                  child: ColoredBox(color: colors.accent, child: child),
                ),
              ),
            );
          },
          // Fuera del builder: el brillo se anima solo y no tiene por qué
          // reconstruirse cada vez que el relleno cambia de ancho.
          child: _Sheen(color: colors.sheen),
        ),
      ),
    );
  }
}

/// El reflejo que viaja por el relleno.
///
/// Mide el 60% del relleno y se mueve en fracciones de sí mismo, igual que el
/// diseño: así entra desde fuera por la izquierda y sale por la derecha sea
/// cual sea el ancho que tenga el relleno en ese momento.
class _Sheen extends StatefulWidget {
  const _Sheen({required this.color});

  final Color color;

  @override
  State<_Sheen> createState() => _SheenState();
}

class _SheenState extends State<_Sheen> with SingleTickerProviderStateMixin {
  late final AnimationController _controller = AnimationController(
    vsync: this,
    duration: AppMotion.sheen,
  )..repeat();

  /// De dónde sale y hasta dónde llega, en anchos de sí mismo.
  static const double _from = -1.1;
  static const double _to = 2.2;

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return FractionallySizedBox(
      widthFactor: 0.6,
      // Igual que el relleno: sin esto el degradado no tiene alto que ocupar.
      heightFactor: 1,
      alignment: Alignment.centerLeft,
      child: AnimatedBuilder(
        animation: _controller,
        builder: (BuildContext context, Widget? band) {
          final double t = AppMotion.standard.transform(_controller.value);
          return FractionalTranslation(
            translation: Offset(_from + (_to - _from) * t, 0),
            child: band,
          );
        },
        child: DecoratedBox(
          decoration: BoxDecoration(
            gradient: LinearGradient(
              colors: <Color>[
                widget.color.withValues(alpha: 0),
                widget.color,
                widget.color.withValues(alpha: 0),
              ],
            ),
          ),
        ),
      ),
    );
  }
}
