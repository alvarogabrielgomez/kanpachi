import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/motion_tokens.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';

/// Las cuatro formas de campo del diseño.
enum AppFieldShape {
  /// Píldora. La usan las filas que llevan un botón pegado a la derecha.
  pill,

  /// Rectángulo redondeado, para formularios en bloque.
  rounded,

  /// La caja que lleva controles pegados DENTRO: el rango de puertos con su
  /// selector de protocolo, el nombre de la sala con su botón de guardar.
  ///
  /// No es `rounded` con menos aire: el aire de `rounded` está pensado para un
  /// campo suelto, y aquí lo que va dentro no es texto sino otro control, que
  /// trae el suyo. Reutilizar `rounded` con un padding condicionado a
  /// `trailing` cambiaría también los campos de nombre y portada el día que
  /// alguno lleve algo pegado.
  inline,

  /// Rectángulo grande y centrado: el nombre en el alta.
  hero,
}

/// El campo de texto de Kanpachi.
///
/// El borde se pinta acá y no en el `InputDecoration` de cada pantalla: es el
/// único sitio donde se decide que un campo enfocado se marca con el acento, y
/// tenerlo repetido garantiza que un día uno de ellos se quede atrás.
class AppField extends StatefulWidget {
  const AppField({
    required this.controller,
    this.hint,
    this.shape = AppFieldShape.rounded,
    this.trailing,
    this.height,
    this.radius,
    this.maxLength,
    this.textStyle,
    this.mono = false,
    this.autofocus = false,
    this.onSubmitted,
    this.onChanged,
    this.inputFormatters,
    this.focusNode,
    super.key,
  });

  final TextEditingController controller;
  final String? hint;
  final AppFieldShape shape;

  /// Lo que va pegado a la derecha DENTRO del campo: casi siempre un botón.
  final Widget? trailing;

  /// Alto fijo, para las filas donde el campo comparte renglón con un botón y
  /// un conmutador. Sin él cada pieza mide lo que le sale de su contenido y la
  /// fila queda en escalera; el diseño las pone las tres del mismo alto.
  final double? height;

  /// Gana sobre el radio de la forma. Es para el caso suelto — el editor del
  /// nombre de la sala, que el diseño redondea a 10 — y no para abrirle la
  /// puerta a que cada pantalla elija el suyo.
  final BorderRadius? radius;

  final int? maxLength;
  final TextStyle? textStyle;

  /// Datos literales en monoespaciada: códigos, puertos, direcciones.
  final bool mono;

  final bool autofocus;
  final ValueChanged<String>? onSubmitted;
  final ValueChanged<String>? onChanged;
  final List<TextInputFormatter>? inputFormatters;
  final FocusNode? focusNode;

  @override
  State<AppField> createState() => _AppFieldState();
}

class _AppFieldState extends State<AppField> {
  late final FocusNode _focus = widget.focusNode ?? FocusNode();
  bool _focused = false;

  @override
  void initState() {
    super.initState();
    _focus.addListener(_onFocus);
  }

  void _onFocus() {
    if (_focus.hasFocus != _focused) {
      setState(() => _focused = _focus.hasFocus);
    }
  }

  @override
  void dispose() {
    _focus.removeListener(_onFocus);
    if (widget.focusNode == null) _focus.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final type = context.type;

    final EdgeInsets padding = switch (widget.shape) {
      // El inset derecho depende de si hay botón pegado dentro. Los 6 px están
      // para dejarle sitio a ese botón; cuando no lo hay, el texto queda a 6
      // del borde y a 18 del otro lado, descentrado a la vista.
      AppFieldShape.pill => EdgeInsets.fromLTRB(
          AppSpacing.x4l,
          AppSpacing.sm,
          widget.trailing == null ? AppSpacing.x4l : AppSpacing.sm,
          AppSpacing.sm,
        ),
      AppFieldShape.rounded => const EdgeInsets.symmetric(
          horizontal: AppSpacing.x3l, vertical: AppSpacing.xl),
      AppFieldShape.inline => const EdgeInsets.fromLTRB(
          AppSpacing.x3l, 5, 5, 5),
      AppFieldShape.hero => const EdgeInsets.symmetric(
          horizontal: AppSpacing.x4l, vertical: AppSpacing.x3l),
    };

    final BorderRadius radius = widget.radius ??
        switch (widget.shape) {
      AppFieldShape.pill => AppRadius.pill,
      AppFieldShape.rounded || AppFieldShape.inline => AppRadius.allMd,
      AppFieldShape.hero => AppRadius.allLg,
    };

    final TextStyle style = widget.textStyle ??
        switch (widget.shape) {
          AppFieldShape.pill || AppFieldShape.inline =>
            widget.mono ? type.mono.copyWith(letterSpacing: 0.75) : type.labelLg,
          AppFieldShape.rounded => type.bodyLg.copyWith(height: 1),
          AppFieldShape.hero =>
            type.labelLg.copyWith(fontSize: 18, height: 1),
        };

    return AnimatedContainer(
      duration: AppMotion.hover,
      height: widget.height,
      padding: padding,
      decoration: BoxDecoration(
        color: colors.surfaceSunken,
        borderRadius: radius,
        border: Border.all(
          color: _focused ? colors.accent : colors.border,
          width: AppStroke.hairline,
        ),
      ),
      child: Row(
        // El botón pegado dentro no va pegado del todo: el diseño deja 8 px
        // entre el texto y el control en las tres píldoras que los llevan.
        spacing: AppSpacing.md,
        children: <Widget>[
          Expanded(
            // TextField y no EditableText: el segundo no pinta placeholder ni
            // trae el menú contextual ni los tiradores de seleccion, y esta app
            // vive de pegar codigos con el raton.
            child: TextField(
              controller: widget.controller,
              focusNode: _focus,
              autofocus: widget.autofocus,
              maxLines: 1,
              style: style.copyWith(color: colors.text),
              cursorColor: colors.accent,
              textAlign: widget.shape == AppFieldShape.hero
                  ? TextAlign.center
                  : TextAlign.start,
              decoration: InputDecoration.collapsed(
                hintText: widget.hint,
                hintStyle: style.copyWith(color: colors.textMuted),
              ),
              inputFormatters: <TextInputFormatter>[
                if (widget.maxLength != null)
                  LengthLimitingTextInputFormatter(widget.maxLength),
                ...?widget.inputFormatters,
              ],
              onChanged: widget.onChanged,
              onSubmitted: widget.onSubmitted,
            ),
          ),
          if (widget.trailing != null) widget.trailing!,
        ],
      ),
    );
  }
}
