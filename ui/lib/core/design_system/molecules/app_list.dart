import 'package:flutter/material.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_card.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/density_tokens.dart';
import 'package:kanpachi_ui/core/design_system/tokens/motion_tokens.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';

/// La lista enmarcada: filas separadas por una línea, dentro de una caja con
/// borde. La usan los juegos instalados, el catálogo en modo lista y los
/// miembros de la sala.
///
/// El separador lo pone la lista y no la fila, para que la última no arrastre
/// una línea suelta contra el borde de la caja. Es el error clásico de este
/// patrón y sólo se ve cuando ya está en pantalla.
class AppRowList extends StatelessWidget {
  const AppRowList({
    required this.children,
    this.footer,
    this.radius = AppRadius.allLg,
    super.key,
  });

  final List<Widget> children;

  /// Una fila final con fondo hundido: "Ver toda la biblioteca (18)".
  final Widget? footer;

  /// 14 por defecto, que es lo que el diseño usa en las listas de juegos. La
  /// de miembros de la sala pide 16: es la lista más grande de la app y con el
  /// radio chico se lee como una caja apretada.
  final BorderRadius radius;

  @override
  Widget build(BuildContext context) {
    final Color line = context.colors.border;
    return AppCard(
      clip: true,
      radius: radius,
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: <Widget>[
          for (int i = 0; i < children.length; i++) ...<Widget>[
            children[i],
            if (i != children.length - 1 || footer != null)
              Container(height: 1, color: line),
          ],
          ?footer,
        ],
      ),
    );
  }
}

/// Una fila de lista. Se ilumina al pasar por encima sólo si de verdad hace
/// algo: una fila que se resalta y no responde al clic es una promesa
/// incumplida.
class AppRow extends StatefulWidget {
  const AppRow({
    required this.child,
    this.onTap,
    this.padding,
    this.background,
    super.key,
  });

  final Widget child;
  final VoidCallback? onTap;
  final EdgeInsets? padding;

  /// Fondo fijo, para la fila destacada del footer.
  final Color? background;

  @override
  State<AppRow> createState() => _AppRowState();
}

class _AppRowState extends State<AppRow> {
  bool _hovered = false;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final bool interactive = widget.onTap != null;
    return MouseRegion(
      cursor: interactive ? SystemMouseCursors.click : SystemMouseCursors.basic,
      onEnter: (_) => setState(() => _hovered = true),
      onExit: (_) => setState(() => _hovered = false),
      child: GestureDetector(
        onTap: widget.onTap,
        child: AnimatedContainer(
          duration: AppMotion.hover,
          width: double.infinity,
          padding: widget.padding ?? context.density.rowPadding,
          color: _hovered && interactive
              ? colors.surfaceSunken
              : (widget.background ?? Colors.transparent),
          child: widget.child,
        ),
      ),
    );
  }
}

/// Cómo se ve una [AppTappableCard] EN REPOSO.
///
/// Encendida se ven todas igual —relleno de chip y línea continua con el
/// acento—, y por eso el estilo sólo describe el reposo. Es un estilo y no dos
/// banderas sueltas porque las banderas admitían combinaciones que no existen
/// en el diseño, como discontinua y hundida a la vez, y nadie sabía cuál de las
/// dos ganaba.
enum AppTappableCardStyle {
  /// Línea continua. Las portadas de la rejilla.
  outlined,

  /// Línea discontinua: lo que todavía no existe, como una sala sin juego.
  dashed,

  /// Relleno hundido, para una llamada que descansa sobre la propia página.
  sunken,
}

/// La tarjeta que se resalta con el acento al pasar por encima: las de la
/// rejilla de portadas y las dos llamadas grandes ("Crear la sala sin juego",
/// "Ver toda la biblioteca").
///
/// Las tres son ESTA, con estilos distintos. Una tarjeta propia por variante
/// tendría tres hover con tres velocidades, que es justo el defecto que se
/// midió acá: sólo se nota comparando dos en la misma pantalla.
class AppTappableCard extends StatefulWidget {
  const AppTappableCard({
    required this.child,
    required this.onTap,
    this.padding,
    this.selected = false,
    this.style = AppTappableCardStyle.outlined,
    super.key,
  });

  final Widget child;
  final VoidCallback onTap;
  final EdgeInsets? padding;

  /// El juego ya elegido queda marcado con el acento sin necesidad de hover.
  final bool selected;

  /// Cómo se ve en reposo. Ver [AppTappableCardStyle].
  final AppTappableCardStyle style;

  @override
  State<AppTappableCard> createState() => _AppTappableCardState();
}

class _AppTappableCardState extends State<AppTappableCard> {
  bool _hovered = false;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final bool lit = _hovered || widget.selected;

    // Discontinua sólo en reposo: iluminarse la pasa a línea continua con el
    // acento, que es lo que hace el diseño.
    final bool guiones = widget.style == AppTappableCardStyle.dashed && !lit;

    final Widget caja = AnimatedContainer(
      duration: AppMotion.hover,
      padding: widget.padding,
      decoration: BoxDecoration(
        color: lit
            ? colors.chip
            : (widget.style == AppTappableCardStyle.sunken
                  ? colors.surfaceSunken
                  : Colors.transparent),
        borderRadius: AppRadius.allLg,
        // **El borde se reserva SIEMPRE**, y con guiones va transparente.
        //
        // Un `Border` ocupa sitio DENTRO de la caja, así que quitarlo en reposo
        // dejaba la tarjeta dos píxeles más chica que al pasar por encima: el
        // texto saltaba y lo de debajo se movía con él. El trazo discontinuo lo
        // pinta el painter, que no ocupa nada, así que este hueco es lo único
        // que iguala los dos estados.
        border: Border.all(
          color: guiones
              ? Colors.transparent
              : (lit ? colors.accent : colors.border),
          width: AppStroke.hairline,
        ),
      ),
      child: widget.child,
    );

    return MouseRegion(
      cursor: SystemMouseCursors.click,
      onEnter: (_) => setState(() => _hovered = true),
      onExit: (_) => setState(() => _hovered = false),
      child: GestureDetector(
        onTap: widget.onTap,
        // Los guiones envuelven la CAJA, no al hijo. Envolvían al hijo, o sea
        // que caían por dentro del padding: el rectángulo discontinuo rodeaba
        // al texto, y la línea continua del hover aparecía en otro sitio.
        //
        // # Por qué el `CustomPaint` está SIEMPRE, incluso sin nada que pintar
        //
        // Porque la forma del árbol no puede cambiar entre los dos estados.
        // `Widget.canUpdate` compara el runtimeType, así que envolver sólo en
        // reposo hacía que al iluminarse el `AnimatedContainer` se desactivara
        // y naciera de cero, con los valores finales ya puestos: esta tarjeta
        // saltaba de golpe mientras la de al lado hacía sus 160 ms. Se ve sólo
        // comparando dos en la misma pantalla.
        //
        // Y los guiones se desvanecen con la MISMA duración, en vez de
        // desaparecer de un fotograma al siguiente: mientras el borde continuo
        // aparece, el discontinuo se va.
        child: TweenAnimationBuilder<double>(
          duration: AppMotion.hover,
          tween: Tween<double>(begin: 0, end: guiones ? 1 : 0),
          // Por acá y no dentro del builder: así la caja no se reconstruye en
          // cada fotograma de la transición.
          child: caja,
          builder: (BuildContext context, double t, Widget? child) =>
              CustomPaint(
                painter: t == 0
                    ? null
                    : DashedBorderPainter(
                        color: colors.border.withValues(
                          alpha: colors.border.a * t,
                        ),
                        radius: AppRadius.allLg,
                      ),
                child: child,
              ),
        ),
      ),
    );
  }
}
