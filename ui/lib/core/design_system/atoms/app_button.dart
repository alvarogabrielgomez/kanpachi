import 'package:flutter/material.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/motion_tokens.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';

/// Los arquetipos de botón. Uno por INTENCIÓN, no por apariencia: quien
/// escribe una pantalla elige "esta es la acción principal", y el design
/// system decide cómo se ve. Si mañana el relieve se cambia por un degradado,
/// se cambia acá y no en once pantallas.
enum AppButtonVariant {
  /// La acción principal de la pantalla. Lleva relieve y hunde al presionar.
  /// Sólo puede haber una por pantalla.
  primary,

  /// La misma acción principal pero dentro de una fila compacta, donde el
  /// relieve no cabe sin descuadrar la caja.
  primaryFlat,

  /// Acción secundaria: contorno, sin relleno.
  ghost,

  /// Acción terciaria, sobre fondo de chip. La usa "Salir de la sala": es
  /// destructiva pero no urgente, y un botón lleno la haría parecer el
  /// objetivo de la pantalla.
  quiet,
}

/// El botón de Kanpachi.
///
/// El relieve de la variante [AppButtonVariant.primary] no es decoración: es
/// lo que hace obvio dónde está la acción de la pantalla sin recurrir a otro
/// color, y el hundido al presionar confirma el clic antes de que haya llegado
/// ninguna respuesta del daemon.
class AppButton extends StatefulWidget {
  const AppButton({
    required this.label,
    required this.onPressed,
    this.variant = AppButtonVariant.primary,
    this.width,
    this.height,
    this.icon,
    super.key,
  });

  final String label;

  /// `null` deja el botón apagado. Es un estado real del diseño: "Unirse"
  /// nace apagado hasta que el código tiene la forma correcta.
  final VoidCallback? onPressed;

  final AppButtonVariant variant;
  final double? width;
  final double? height;
  final Widget? icon;

  @override
  State<AppButton> createState() => _AppButtonState();
}

class _AppButtonState extends State<AppButton> {
  bool _hovered = false;
  bool _pressed = false;

  /// Cuánto baja el botón. El relieve se acorta lo mismo que baja el cuerpo,
  /// así que el borde inferior se queda quieto y lo que se ve es la tapa
  /// hundiéndose, no el botón entero desplazándose.
  double get _sink {
    if (!_hasRelief) return 0;
    if (_pressed) return 3;
    if (_hovered) return -2;
    return 0;
  }

  double get _reliefHeight {
    if (!_hasRelief) return 0;
    if (_pressed) return 3;
    if (_hovered) return 8;
    return 6;
  }

  bool get _hasRelief =>
      widget.variant == AppButtonVariant.primary && _enabled;

  bool get _enabled => widget.onPressed != null;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final type = context.type;

    final (Color background, Color foreground, Color? border) = switch (widget.variant) {
      AppButtonVariant.primary || AppButtonVariant.primaryFlat => _enabled
          ? (colors.accent, colors.accentInk, null)
          : (colors.chip, colors.textMuted, null),
      AppButtonVariant.ghost => (
          _hovered ? colors.chip : Colors.transparent,
          _hovered && _enabled ? colors.accent : colors.text,
          _hovered && _enabled ? colors.accent : colors.border,
        ),
      AppButtonVariant.quiet => (
          colors.chip,
          _hovered && _enabled ? colors.accent : colors.text,
          _hovered && _enabled ? colors.accent : colors.border,
        ),
    };

    final TextStyle textStyle = switch (widget.variant) {
      AppButtonVariant.primary => type.buttonLg,
      AppButtonVariant.primaryFlat => type.button,
      AppButtonVariant.ghost || AppButtonVariant.quiet => type.label,
    };

    return MouseRegion(
      cursor: _enabled ? SystemMouseCursors.click : SystemMouseCursors.basic,
      onEnter: (_) => setState(() => _hovered = true),
      onExit: (_) => setState(() {
        _hovered = false;
        _pressed = false;
      }),
      child: GestureDetector(
        onTapDown: _enabled ? (_) => setState(() => _pressed = true) : null,
        onTapUp: _enabled ? (_) => setState(() => _pressed = false) : null,
        onTapCancel: _enabled ? () => setState(() => _pressed = false) : null,
        onTap: widget.onPressed,
        child: AnimatedContainer(
          duration: AppMotion.press,
          curve: AppMotion.standard,
          width: widget.width,
          height: widget.height,
          transform: Matrix4.translationValues(0, _sink, 0),
          padding: EdgeInsets.symmetric(
            horizontal: widget.variant == AppButtonVariant.primary
                ? AppSpacing.x7l
                : AppSpacing.x7l,
            vertical: widget.height == null ? AppSpacing.xxl : 0,
          ),
          decoration: BoxDecoration(
            color: background,
            borderRadius: AppRadius.pill,
            border: border == null
                ? null
                : Border.all(color: border, width: AppStroke.hairline),
            boxShadow: _reliefHeight == 0
                ? null
                : <BoxShadow>[
                    BoxShadow(
                      color: colors.accentShadow,
                      offset: Offset(0, _reliefHeight),
                    ),
                  ],
          ),
          // Sin `alignment`. Un Container con alignment envuelve al hijo en un
          // Align sin factor, y eso se come el ancho disponible entero: dentro
          // de un Wrap acotado el botón dejaba de abrazar su texto y se
          // estiraba a lo ancho del renglón, uno por línea. Sin alignment, el
          // Container encoge cuando las constraints son sueltas y se llena
          // cuando son tirantes — que es exactamente lo que se quiere: abraza
          // en una fila de acciones, y ocupa el ancho entero cuando la columna
          // que lo contiene lo estira.
          child: Row(
            mainAxisSize: MainAxisSize.min,
            mainAxisAlignment: MainAxisAlignment.center,
            children: <Widget>[
              if (widget.icon != null) ...<Widget>[
                IconTheme(
                  data: IconThemeData(color: foreground, size: 14),
                  child: widget.icon!,
                ),
                const SizedBox(width: AppSpacing.sm),
              ],
              Flexible(
                child: Text(
                  widget.label,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: textStyle.copyWith(color: foreground),
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}
