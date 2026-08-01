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
  const AppRowList({required this.children, this.footer, super.key});

  final List<Widget> children;

  /// Una fila final con fondo hundido: "Ver toda la biblioteca (18)".
  final Widget? footer;

  @override
  Widget build(BuildContext context) {
    final Color line = context.colors.border;
    return AppCard(
      clip: true,
      radius: AppRadius.allLg,
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

/// La tarjeta que se resalta con el acento al pasar por encima: las de la
/// rejilla de portadas y las dos llamadas grandes ("Crear la sala sin juego",
/// "Ver toda la biblioteca").
class AppTappableCard extends StatefulWidget {
  const AppTappableCard({
    required this.child,
    required this.onTap,
    this.padding,
    this.selected = false,
    this.dashed = false,
    this.filled = false,
    super.key,
  });

  final Widget child;
  final VoidCallback onTap;
  final EdgeInsets? padding;

  /// El juego ya elegido queda marcado con el acento sin necesidad de hover.
  final bool selected;

  final bool dashed;
  final bool filled;

  @override
  State<AppTappableCard> createState() => _AppTappableCardState();
}

class _AppTappableCardState extends State<AppTappableCard> {
  bool _hovered = false;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final bool lit = _hovered || widget.selected;
    return MouseRegion(
      cursor: SystemMouseCursors.click,
      onEnter: (_) => setState(() => _hovered = true),
      onExit: (_) => setState(() => _hovered = false),
      child: GestureDetector(
        onTap: widget.onTap,
        child: AnimatedContainer(
          duration: AppMotion.hover,
          padding: widget.padding,
          decoration: BoxDecoration(
            color: lit
                ? colors.chip
                : (widget.filled ? colors.surfaceSunken : Colors.transparent),
            borderRadius: AppRadius.allLg,
            border: widget.dashed && !lit
                ? null
                : Border.all(
                    color: lit ? colors.accent : colors.border,
                    width: AppStroke.hairline,
                  ),
          ),
          child: widget.dashed && !lit
              ? _Dashed(child: widget.child)
              : widget.child,
        ),
      ),
    );
  }
}

class _Dashed extends StatelessWidget {
  const _Dashed({required this.child});

  final Widget child;

  @override
  Widget build(BuildContext context) {
    return AppCard(dashed: true, radius: AppRadius.allLg, child: child);
  }
}
