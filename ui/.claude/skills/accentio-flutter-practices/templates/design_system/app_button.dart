// lib/core/design_system/atoms/app_button.dart

import 'package:flutter/material.dart';

import '../tokens/color_tokens.dart';
import '../tokens/context_ext.dart';
import '../tokens/spacing_tokens.dart';

/// Peso visual. **No** es el tono semántico: es cuánto pesa en la jerarquía.
///
/// Mapean 1:1 a los botones de Material, que es lo que va por debajo. Eso trae
/// gratis cuatro cosas que un `GestureDetector` + `Container` pierde de una:
/// foco por teclado, cursor de mano en desktop, ripple, y el anuncio como
/// botón al screen reader.
enum ButtonVariant {
  /// Relleno con el acento. La acción principal. Una por pantalla.
  primary,

  /// Borde, sin relleno. Acción alternativa.
  ghost,

  /// Sin borde ni relleno. Terciaria — la que no querés que compita.
  quiet,
}

/// Tres tamaños, no trece. Un repo sin esta restricción termina con 13 alturas
/// para botones conceptualmente iguales, la mitad metidas en `SizedBox` del
/// call site.
enum ButtonSize {
  /// 36 — denso: barras, filas, chrome.
  sm,

  /// 48 — el default y el tap target mínimo de Material.
  md,

  /// 56 — CTA de página.
  lg,
}

enum _Shape { label, icon, link }

/// El botón del design system. Uno solo, para todas las superficies.
///
/// Las diferencias **estructurales** son constructores nombrados; las de
/// **estilo**, enums. Es lo que evita un constructor con 20 parámetros de los
/// cuales 15 no aplican.
///
/// ```dart
/// AppButton('Guardar', onPressed: _save, loading: _saving)
/// AppButton('Cancelar', variant: ButtonVariant.ghost, onPressed: _cancel)
/// AppButton.icon(Icons.refresh, tooltip: 'Actualizar', onPressed: _refresh)
/// AppButton.link('Ver más', onPressed: _open)
/// ```
///
/// ## Los tres apagados — significan cosas distintas
///
/// | Cómo | Se ve | Se toca |
/// |---|---|---|
/// | `onPressed: null` | apagado | **no** — inerte. El canónico de Material |
/// | `loading: true` | apagado + spinner | no |
/// | `disabled: true` (con `onPressed`) | apagado | **sí** — y el caller explica |
///
/// El tercero existe porque un botón gris y mudo no dice qué falta, justo
/// cuando más se necesita — y `Tooltip` es hover, o sea inútil en touch. El
/// caller muestra un toast con el motivo.
class AppButton extends StatelessWidget {
  const AppButton(
    this.label, {
    super.key,
    required this.onPressed,
    this.variant = ButtonVariant.primary,
    this.size = ButtonSize.md,
    this.icon,
    this.trailingIcon,
    this.loading = false,
    this.expand = false,
    this.disabled = false,
    this.tooltip,
  }) : _shape = _Shape.label;

  /// Sólo ícono, caja cuadrada.
  ///
  /// Es un constructor y no `AppButton('')` a propósito: el label vacío es el
  /// hack que obliga a cada caller de sólo-ícono a inventar un string.
  const AppButton.icon(
    IconData this.icon, {
    super.key,
    required this.onPressed,
    this.variant = ButtonVariant.quiet,
    this.size = ButtonSize.md,
    this.tooltip,
    this.disabled = false,
    this.loading = false,
  }) : _shape = _Shape.icon,
       label = '',
       trailingIcon = null,
       expand = false;

  /// Texto subrayable, sin caja. Para navegación inline ("ver más").
  const AppButton.link(
    this.label, {
    super.key,
    required this.onPressed,
    this.size = ButtonSize.sm,
    this.icon,
    this.tooltip,
  }) : _shape = _Shape.link,
       variant = ButtonVariant.quiet,
       trailingIcon = null,
       loading = false,
       expand = false,
       disabled = false;

  final String label;
  final VoidCallback? onPressed;
  final ButtonVariant variant;
  final ButtonSize size;
  final IconData? icon;
  final IconData? trailingIcon;
  final bool loading;
  final bool expand;

  /// Apagado PERO tocable: el caller explica por qué al tocarlo.
  final bool disabled;
  final String? tooltip;
  final _Shape _shape;

  double get _height => switch (size) {
    ButtonSize.sm => 36,
    ButtonSize.md => Spacing.minTapTarget,
    ButtonSize.lg => 56,
  };

  double get _fontSize => switch (size) {
    ButtonSize.sm => 12,
    ButtonSize.md => 13,
    ButtonSize.lg => 15,
  };

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final inert = onPressed == null || loading;
    final dimmed = inert || disabled;

    final fg = _foreground(colors, dimmed);
    final child = _shape == _Shape.icon
        ? Icon(icon, size: _fontSize + 5, color: fg)
        : _AppButtonBody(
            label: label,
            icon: icon,
            trailingIcon: trailingIcon,
            loading: loading,
            fontSize: _fontSize,
            color: fg,
            underline: _shape == _Shape.link,
          );

    final style = ButtonStyle(
      backgroundColor: WidgetStatePropertyAll(_background(colors, dimmed)),
      foregroundColor: WidgetStatePropertyAll(fg),
      overlayColor: WidgetStatePropertyAll(colors.primary.withValues(alpha: 0.10)),
      side: variant == ButtonVariant.ghost
          ? WidgetStatePropertyAll(
              BorderSide(
                color: dimmed ? colors.borderLow : colors.border,
                width: Spacing.borderHairline,
              ),
            )
          : const WidgetStatePropertyAll(BorderSide.none),
      minimumSize: WidgetStatePropertyAll(
        Size(_shape == _Shape.icon ? _height : 0, _height),
      ),
      padding: WidgetStatePropertyAll(
        EdgeInsets.symmetric(
          horizontal: _shape == _Shape.icon ? 0 : Spacing.md,
        ),
      ),
      shape: const WidgetStatePropertyAll(
        RoundedRectangleBorder(
          borderRadius: BorderRadius.all(Radius.circular(Spacing.radiusInput)),
        ),
      ),
      // El botón "apagado pero tocable" NO usa onPressed:null — Material lo
      // haría inerte y el caller no podría explicar nada.
      tapTargetSize: MaterialTapTargetSize.shrinkWrap,
    );

    final button = TextButton(
      style: style,
      onPressed: inert ? null : onPressed,
      child: child,
    );

    final sized = expand
        ? SizedBox(width: double.infinity, child: button)
        : button;

    return tooltip == null ? sized : Tooltip(message: tooltip!, child: sized);
  }

  Color _background(ColorTokens c, bool dimmed) => switch (variant) {
    ButtonVariant.primary => dimmed ? c.sunk : c.primary,
    ButtonVariant.ghost || ButtonVariant.quiet => Colors.transparent,
  };

  Color _foreground(ColorTokens c, bool dimmed) {
    if (dimmed) return c.textTertiary;
    return switch (variant) {
      ButtonVariant.primary => c.onPrimary,
      ButtonVariant.ghost => c.textPrimary,
      ButtonVariant.quiet => c.textSecondary,
    };
  }
}

/// El contenido del botón es una CLASE, no un `Widget _buildBody()`.
///
/// Un helper method cae siempre en la rama `update()` de
/// `Element.updateChild`: pierde el skip total que un `const` habilita.
class _AppButtonBody extends StatelessWidget {
  const _AppButtonBody({
    required this.label,
    required this.icon,
    required this.trailingIcon,
    required this.loading,
    required this.fontSize,
    required this.color,
    required this.underline,
  });

  final String label;
  final IconData? icon;
  final IconData? trailingIcon;
  final bool loading;
  final double fontSize;
  final Color color;
  final bool underline;

  @override
  Widget build(BuildContext context) {
    final text = Text(
      label,
      style: context.type.display(
        size: fontSize,
        color: color,
        trackEm: 0.12,
      ).copyWith(
        decoration: underline ? TextDecoration.underline : null,
        decorationColor: color,
      ),
    );

    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        // `loading` nunca oculta el label: ocultarlo mueve el layout y rompe
        // el anuncio al screen reader.
        if (loading)
          Padding(
            padding: const EdgeInsets.only(right: Spacing.xs),
            child: SizedBox(
              width: fontSize,
              height: fontSize,
              child: CircularProgressIndicator(strokeWidth: 2, color: color),
            ),
          )
        else if (icon != null)
          Padding(
            padding: const EdgeInsets.only(right: Spacing.xs),
            child: Icon(icon, size: fontSize + 3, color: color),
          ),
        Flexible(child: text),
        if (trailingIcon != null)
          Padding(
            padding: const EdgeInsets.only(left: Spacing.xs),
            child: Icon(trailingIcon, size: fontSize + 3, color: color),
          ),
      ],
    );
  }
}
