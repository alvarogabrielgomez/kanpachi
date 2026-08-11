import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_button.dart';
import 'package:kanpachi_ui/core/design_system/tokens/motion_tokens.dart';

/// Copiar algo y decir que se copió.
///
/// El acuse no es cortesía: copiar al portapapeles no deja ninguna señal
/// visible, así que sin él la única forma de saber si funcionó es ir a pegar.
/// Vuelve solo a su texto porque un botón que se queda diciendo "Copiado" no
/// sirve para copiar otra vez.
class CopyButton extends StatefulWidget {
  const CopyButton({
    required this.label,
    required this.value,
    this.copiedLabel = 'Copiado',
    this.variant = AppButtonVariant.primaryFlat,
    this.height,
    this.horizontalPadding,
    this.textStyle,
    this.icon,
    this.iconGap,
    this.iconAlpha = 1,
    super.key,
  });

  final String label;
  final String value;

  /// Lo que dice mientras acusa recibo.
  ///
  /// Se puede cambiar porque no siempre cabe lo mismo ni se copia lo mismo: la
  /// píldora del código dice «Código copiado» porque al lado hay otro botón que
  /// copia el ENLACE, y dos «Copiado» seguidos no dirían cuál de los dos.
  final String copiedLabel;

  final AppButtonVariant variant;
  final double? height;
  final double? horizontalPadding;
  final TextStyle? textStyle;
  final Widget? icon;
  final double? iconGap;
  final double iconAlpha;

  @override
  State<CopyButton> createState() => _CopyButtonState();
}

class _CopyButtonState extends State<CopyButton> {
  bool _copied = false;
  Timer? _timer;

  @override
  void dispose() {
    _timer?.cancel();
    super.dispose();
  }

  Future<void> _copy() async {
    await Clipboard.setData(ClipboardData(text: widget.value));
    if (!mounted) return;
    setState(() => _copied = true);
    _timer?.cancel();
    _timer = Timer(AppMotion.copiedFeedback, () {
      if (mounted) setState(() => _copied = false);
    });
  }

  @override
  Widget build(BuildContext context) {
    return AppButton(
      label: _copied ? widget.copiedLabel : widget.label,
      variant: widget.variant,
      height: widget.height,
      horizontalPadding: widget.horizontalPadding,
      textStyle: widget.textStyle,
      icon: widget.icon,
      iconGap: widget.iconGap,
      iconAlpha: widget.iconAlpha,
      onPressed: _copy,
    );
  }
}
