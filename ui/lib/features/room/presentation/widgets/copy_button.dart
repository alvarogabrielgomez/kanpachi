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
    this.variant = AppButtonVariant.primaryFlat,
    this.height,
    this.horizontalPadding,
    super.key,
  });

  final String label;
  final String value;
  final AppButtonVariant variant;
  final double? height;
  final double? horizontalPadding;

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
      label: _copied ? 'Copiado' : widget.label,
      variant: widget.variant,
      height: widget.height,
      horizontalPadding: widget.horizontalPadding,
      onPressed: _copy,
    );
  }
}
