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

  /// El mismo ghost pero sin contorno propio: lo pinta quien lo envuelve, con
  /// trazo discontinuo. Es "acá puede haber otra cosa y todavía no la hay",
  /// no una acción más del formulario.
  ghostDashed,

  /// Acción terciaria, sobre fondo de chip. La usa "Salir de la sala": es
  /// destructiva pero no urgente, y un botón lleno la haría parecer el
  /// objetivo de la pantalla.
  quiet,

  /// La misma acción terciaria, pero DENTRO de un panel flotante.
  ///
  /// Cambia el fondo de chip por el hundido: el chip se apoya en la superficie
  /// de una pantalla, y sobre la superficie elevada de un menú se pierde.
  quietSunken,
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
    this.horizontalPadding,
    this.textStyle,
    this.emphasis = false,
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

  /// El aire a los lados del texto. Va por llamada y no por variante porque el
  /// diseño lo elige por SITIO, no por arquetipo: el mismo ghost mide 11 en la
  /// fila de un miembro, 15 dentro de una tarjeta, 16 en la cabecera de la
  /// sala y 20 en un diálogo. Un valor por variante queda tan mal como la
  /// constante única que había antes.
  final double? horizontalPadding;

  /// La letra, cuando el sitio pide otra escala que la del arquetipo.
  ///
  /// El mismo `primaryFlat` es el CTA de 14,5 px en «Unirse» y de 12,5 en
  /// «Copiar enlace»; el mismo ghost va a 13,5 en un diálogo y a 11,5 en la
  /// fila de un miembro. Cambiar el mapeo de la variante arreglaría uno y
  /// rompería el otro.
  final TextStyle? textStyle;

  /// Un ghost que va en color de texto pleno en vez de apagado.
  ///
  /// Es la excepción y no la regla: el diseño pinta los ghost en `--kp-muted`
  /// en todas partes menos en el «Cancelar» del alta manual, donde compite con
  /// un «Guardar juego» al lado y tiene que pesar lo mismo.
  final bool emphasis;

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
      // Apagado en reposo. El diseño pinta TODOS los ghost en `--kp-muted`
      // menos uno, así que el default correcto es ese y la excepción se pide
      // por `emphasis`. Al revés, con siete sitios pidiendo lo apagado, la
      // excepción sería la regla.
      AppButtonVariant.ghost => (
          _hovered ? colors.chip : Colors.transparent,
          _hovered && _enabled
              ? colors.accent
              : (widget.emphasis ? colors.text : colors.textMuted),
          _hovered && _enabled ? colors.accent : colors.border,
        ),
      AppButtonVariant.quiet => (
          colors.chip,
          _hovered && _enabled ? colors.accent : colors.text,
          _hovered && _enabled ? colors.accent : colors.border,
        ),
      // Borde nulo: el discontinuo lo pinta el `CustomPaint` de fuera, y con
      // los dos se verían dos contornos.
      AppButtonVariant.ghostDashed => (
          _hovered ? colors.chip : Colors.transparent,
          _hovered && _enabled ? colors.accent : colors.textMuted,
          null,
        ),
      AppButtonVariant.quietSunken => (
          colors.surfaceSunken,
          _hovered && _enabled ? colors.accent : colors.text,
          _hovered && _enabled ? colors.accent : colors.border,
        ),
    };

    final TextStyle textStyle = widget.textStyle ??
        switch (widget.variant) {
          AppButtonVariant.primary => type.buttonLg,
          AppButtonVariant.primaryFlat => type.button,
          AppButtonVariant.ghost ||
          AppButtonVariant.ghostDashed ||
          AppButtonVariant.quiet ||
          AppButtonVariant.quietSunken =>
            type.label,
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
            horizontal: widget.horizontalPadding ?? AppSpacing.x7l,
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
