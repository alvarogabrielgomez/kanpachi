import 'package:flutter/material.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/motion_tokens.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';

/// Botón de icono sin relleno: los de la barra de título y las cruces de
/// quitar. Se marca al pasar por encima con un fondo hundido, que es la única
/// señal que necesita algo tan pequeño.
class AppIconButton extends StatefulWidget {
  const AppIconButton({
    required this.icon,
    required this.onPressed,
    required this.tooltip,
    this.width = 42,
    this.height = 32,
    this.iconSize = 13,
    this.danger = false,
    super.key,
  });

  final IconData icon;
  final VoidCallback? onPressed;

  /// Obligatorio. Un icono suelto sin nombre es una adivinanza, y estos
  /// controles cierran ventanas y quitan cosas.
  final String tooltip;

  final double width;
  final double height;
  final double iconSize;

  /// Tiñe de acento al pasar por encima: quitar un rango de puertos, cerrar el
  /// juego de la sala.
  final bool danger;

  @override
  State<AppIconButton> createState() => _AppIconButtonState();
}

class _AppIconButtonState extends State<AppIconButton> {
  bool _hovered = false;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    return Tooltip(
      message: widget.tooltip,
      child: MouseRegion(
        cursor: widget.onPressed == null
            ? SystemMouseCursors.basic
            : SystemMouseCursors.click,
        onEnter: (_) => setState(() => _hovered = true),
        onExit: (_) => setState(() => _hovered = false),
        child: GestureDetector(
          onTap: widget.onPressed,
          child: AnimatedContainer(
            duration: AppMotion.hover,
            width: widget.width,
            height: widget.height,
            alignment: Alignment.center,
            decoration: BoxDecoration(
              color: _hovered
                  ? (widget.danger ? colors.chip : colors.surfaceSunken)
                  : Colors.transparent,
              borderRadius: AppRadius.allXs,
            ),
            child: Icon(
              widget.icon,
              size: widget.iconSize,
              color: _hovered && widget.danger ? colors.accent : colors.textMuted,
            ),
          ),
        ),
      ),
    );
  }
}

/// La flecha redonda de volver. Aparece en el selector de juego, en la
/// biblioteca y en el alta manual, siempre en la misma posición: arriba a la
/// izquierda, antes del título.
class AppBackButton extends StatefulWidget {
  const AppBackButton({required this.onPressed, super.key});

  final VoidCallback onPressed;

  @override
  State<AppBackButton> createState() => _AppBackButtonState();
}

class _AppBackButtonState extends State<AppBackButton> {
  bool _hovered = false;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    return Tooltip(
      message: 'Volver',
      child: MouseRegion(
        cursor: SystemMouseCursors.click,
        onEnter: (_) => setState(() => _hovered = true),
        onExit: (_) => setState(() => _hovered = false),
        child: GestureDetector(
          onTap: widget.onPressed,
          child: AnimatedContainer(
            duration: AppMotion.hover,
            width: 30,
            height: 30,
            alignment: Alignment.center,
            decoration: BoxDecoration(
              color: _hovered ? colors.chip : Colors.transparent,
              shape: BoxShape.circle,
              border: Border.all(
                color: _hovered ? colors.accent : colors.border,
                width: AppStroke.hairline,
              ),
            ),
            child: Icon(
              Icons.arrow_back,
              size: 15,
              color: _hovered ? colors.accent : colors.textMuted,
            ),
          ),
        ),
      ),
    );
  }
}
