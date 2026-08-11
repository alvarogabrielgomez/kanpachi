import 'package:flutter/material.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';

// Acá vivía `AppChip`, la píldora de SÓLO LECTURA del código de la sala. Su
// propia documentación decía «para llevárselo está el botón de copiar», y ese
// reparto era el problema: el gesto obvio —pulsar el código— no hacía nada, y
// el botón que sí lo hacía estaba en otro sitio. Ahora la píldora es el botón,
// ver `AppButtonVariant.data`.

/// El bloque de texto explicativo sobre fondo de chip.
///
/// Es el sitio donde la app explica QUÉ va a pasar antes de que pase — que el
/// nombre no se manda a ningún servidor, que entrar no expone la máquina. Se
/// distingue de un aviso ([AppNotice]) en que no pide nada: informa y ya.
class AppExplainer extends StatelessWidget {
  const AppExplainer(
    this.text, {
    this.rich,
    this.textAlign,
    this.compact = false,
    super.key,
  });

  final String text;

  /// Cuando parte del texto va en negrita. Si viene, manda sobre [text].
  final InlineSpan? rich;

  /// El diseño lo centra sólo en la pantalla del nombre, donde todo lo demás
  /// va centrado. En la invitación y en el diálogo va a la izquierda.
  final TextAlign? textAlign;

  /// Dentro de un diálogo: caja más chica y menos aire. Un diálogo ya es una
  /// caja, y la geometría de pantalla completa dentro de otra caja se lee como
  /// dos marcos.
  final bool compact;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final TextStyle base = context.type.body.copyWith(color: colors.textOnChip);
    return Container(
      width: double.infinity,
      padding: compact
          ? const EdgeInsets.symmetric(horizontal: 17, vertical: 15)
          : const EdgeInsets.symmetric(
              horizontal: AppSpacing.x4l,
              vertical: AppSpacing.x3l,
            ),
      decoration: BoxDecoration(
        color: colors.chip,
        borderRadius: compact ? AppRadius.allLg : AppRadius.allXl,
      ),
      child: rich == null
          ? Text(text, style: base, textAlign: textAlign)
          : Text.rich(rich!, style: base, textAlign: textAlign),
    );
  }
}
